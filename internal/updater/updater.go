// Package updater finds the latest GitHub release and replaces the running
// binary with its archive after checking the release's checksums.txt.
package updater

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	latestURL   = "https://api.github.com/repos/n0nuser/decathlon-cli/releases/latest"
	downloadURL = "https://github.com/n0nuser/decathlon-cli/releases/download/"
	maxArchive  = 64 << 20
)

var semver = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

type Release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// Stable is true for plain vX.Y.Z versions; dev and prerelease builds are
// never replaced.
func Stable(v string) bool { return semver.MatchString(v) }

// Newer reports whether latest is a higher X.Y.Z than current.
func Newer(latest, current string) bool {
	l, c := semver.FindStringSubmatch(latest), semver.FindStringSubmatch(current)
	if l == nil || c == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, _ := strconv.Atoi(l[i])
		y, _ := strconv.Atoi(c[i])
		if x != y {
			return x > y
		}
	}
	return false
}

func Client() *http.Client { return &http.Client{Timeout: 60 * time.Second} }

func get(ctx context.Context, hc *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "decathlon-cli-updater")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than expected", url)
	}
	return b, err
}

func Latest(ctx context.Context, hc *http.Client) (Release, error) {
	var r Release
	raw, err := get(ctx, hc, latestURL, 1<<20)
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, err
	}
	if r.Draft || r.Prerelease || !Stable(r.Tag) {
		return r, fmt.Errorf("latest release %q is not a stable version", r.Tag)
	}
	return r, nil
}

// Archive is the goreleaser archive name for this platform.
func Archive(tag string) string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("decathlon_%s_%s_%s.%s", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH, ext)
}

// Install downloads the archive, verifies its sha256 against checksums.txt,
// and atomically replaces exe.
func Install(ctx context.Context, hc *http.Client, r Release, exe string) error {
	name := Archive(r.Tag)
	sums, err := get(ctx, hc, downloadURL+r.Tag+"/checksums.txt", 1<<20)
	if err != nil {
		return err
	}
	want := ""
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && f[1] == name {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt for %s lists no %s", r.Tag, name)
	}
	archive, err := get(ctx, hc, downloadURL+r.Tag+"/"+name, maxArchive)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != want {
		return errors.New("downloaded archive does not match checksums.txt; nothing was replaced")
	}
	bin, err := extract(archive, runtime.GOOS == "windows")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(exe), ".decathlon-update-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", exe, err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		// A running .exe cannot be overwritten, only renamed away.
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), exe)
}

func extract(archive []byte, isZip bool) ([]byte, error) {
	want := "decathlon"
	if isZip {
		want = "decathlon.exe"
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == want {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(io.LimitReader(rc, maxArchive))
			}
		}
		return nil, errors.New("archive holds no decathlon.exe")
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, errors.New("archive holds no decathlon binary")
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == want {
			return io.ReadAll(io.LimitReader(tr, maxArchive))
		}
	}
}

// ManagedBy names the package manager that owns exe, if any: those
// installs update through it, not by replacing the file.
func ManagedBy(exe string) string {
	switch {
	case strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/homebrew/"):
		return "homebrew"
	case strings.Contains(exe, `\scoop\`):
		return "scoop"
	case strings.HasPrefix(exe, "/usr/bin/"):
		return "your system package manager"
	}
	return ""
}
