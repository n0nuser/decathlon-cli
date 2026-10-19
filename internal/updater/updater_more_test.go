package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type route struct {
	status int
	body   []byte
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fakeClient(routes map[string]route) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, ok := routes[r.URL.String()]
		if !ok {
			return nil, fmt.Errorf("unexpected request %s", r.URL)
		}
		return &http.Response{
			StatusCode: resp.status,
			Status:     fmt.Sprintf("%d %s", resp.status, http.StatusText(resp.status)),
			Body:       io.NopCloser(bytes.NewReader(resp.body)),
			Header:     make(http.Header),
			Request:    r,
		}, nil
	})}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipFile(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestLatest(t *testing.T) {
	ctx := context.Background()
	stable := fakeClient(map[string]route{latestURL: {200, []byte(`{"tag_name":"v0.2.0"}`)}})
	r, err := Latest(ctx, stable)
	if err != nil || r.Tag != "v0.2.0" {
		t.Fatalf("%+v %v", r, err)
	}
	for name, body := range map[string]string{
		"draft":      `{"tag_name":"v0.2.0","draft":true}`,
		"prerelease": `{"tag_name":"v0.2.0","prerelease":true}`,
		"not semver": `{"tag_name":"nightly"}`,
	} {
		if _, err := Latest(ctx, fakeClient(map[string]route{latestURL: {200, []byte(body)}})); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := Latest(ctx, fakeClient(map[string]route{latestURL: {500, []byte("nope")}})); err == nil {
		t.Error("500 accepted")
	}
	if _, err := Latest(ctx, fakeClient(map[string]route{latestURL: {200, []byte("{")}})); err == nil {
		t.Error("bad JSON accepted")
	}
}

func TestGetLimitsAndStatus(t *testing.T) {
	ctx := context.Background()
	if _, err := get(ctx, fakeClient(map[string]route{"https://x": {404, nil}}), "https://x", 10); err == nil {
		t.Error("404 accepted")
	}
	if _, err := get(ctx, fakeClient(map[string]route{"https://x": {200, []byte("0123456789")}}), "https://x", 5); err == nil || !strings.Contains(err.Error(), "larger") {
		t.Errorf("limit: %v", err)
	}
	body, err := get(ctx, fakeClient(map[string]route{"https://x": {200, []byte("ok")}}), "https://x", 10)
	if err != nil || string(body) != "ok" {
		t.Fatalf("%q %v", body, err)
	}
}

func TestInstallReplacesTheBinary(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "decathlon")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := tarGz(t, map[string]string{"README.md": "x", "decathlon": "NEW"})
	sum := sha256.Sum256(archive)
	tag := "v0.2.0"
	name := Archive(tag)
	routes := map[string]route{
		downloadURL + tag + "/checksums.txt": {200, []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")},
		downloadURL + tag + "/" + name:       {200, archive},
	}
	if err := Install(context.Background(), fakeClient(routes), Release{Tag: tag}, exe); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "NEW" {
		t.Fatalf("%q %v", got, err)
	}
	if fi, err := os.Stat(exe); err != nil || fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v %v", fi.Mode(), err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("left temporary files: %v", entries)
	}
}

func TestInstallRefusesBadArchives(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "decathlon")
	if err := os.WriteFile(exe, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	tag := "v0.2.0"
	name := Archive(tag)
	archive := tarGz(t, map[string]string{"decathlon": "NEW"})
	var junk bytes.Buffer
	junkGz := gzip.NewWriter(&junk)
	junkGz.Write([]byte("junk without a tar header"))
	junkGz.Close()
	badArchive := junk.Bytes()
	badSum := sha256.Sum256(badArchive)

	cases := []struct {
		name     string
		checksum string
		archive  []byte
		want     string
	}{
		{"mismatch", strings.Repeat("0", 64) + "  " + name + "\n", archive, "does not match"},
		{"missing entry", strings.Repeat("0", 64) + "  other.tar.gz\n", archive, "lists no"},
		{"bad archive", hex.EncodeToString(badSum[:]) + "  " + name + "\n", badArchive, "no decathlon binary"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			routes := map[string]route{
				downloadURL + tag + "/checksums.txt": {200, []byte(c.checksum)},
				downloadURL + tag + "/" + name:       {200, c.archive},
			}
			err := Install(context.Background(), fakeClient(routes), Release{Tag: tag}, exe)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("%v", err)
			}
			if got, _ := os.ReadFile(exe); string(got) != "OLD" {
				t.Fatalf("binary replaced anyway: %q", got)
			}
		})
	}
	if err := Install(context.Background(), fakeClient(map[string]route{}), Release{Tag: tag}, exe); err == nil {
		t.Fatal("missing checksums.txt accepted")
	}
}

func TestExtractZipAndFailures(t *testing.T) {
	zipBytes := zipFile(t, map[string]string{"decathlon.exe": "WIN"})
	got, err := extract(zipBytes, true)
	if err != nil || string(got) != "WIN" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := extract(zipFile(t, map[string]string{"other": "x"}), true); err == nil {
		t.Error("zip without the binary accepted")
	}
	if _, err := extract([]byte("not a zip"), true); err == nil {
		t.Error("bad zip accepted")
	}
	if _, err := extract([]byte("not gzip"), false); err == nil {
		t.Error("bad gzip accepted")
	}
}

func TestArchiveAndManagedBy(t *testing.T) {
	got := Archive("v1.2.3")
	if !strings.HasPrefix(got, "decathlon_1.2.3_"+runtime.GOOS+"_"+runtime.GOARCH+".") {
		t.Fatalf("%q", got)
	}
	for exe, want := range map[string]string{
		"/opt/homebrew/bin/decathlon":                    "homebrew",
		"/usr/local/Cellar/decathlon/1":                  "homebrew",
		`C:\Users\me\scoop\apps\decathlon\decathlon.exe`: "scoop",
		"/usr/bin/decathlon":                             "your system package manager",
		"/home/me/.local/bin/decathlon":                  "",
	} {
		if got := ManagedBy(exe); got != want {
			t.Errorf("ManagedBy(%q) = %q, want %q", exe, got, want)
		}
	}
}

func TestReleaseJSON(t *testing.T) {
	raw, err := json.Marshal(Release{Tag: "v1.0.0", Draft: false})
	if err != nil || !strings.Contains(string(raw), `"tag_name":"v1.0.0"`) {
		t.Fatalf("%s %v", raw, err)
	}
}
