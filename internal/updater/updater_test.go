package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		l, c string
		want bool
	}{{"v0.2.0", "0.1.9", true}, {"v0.10.0", "0.9.0", true}, {"v1.0.0", "1.0.0", false}, {"v1.0.0", "dev", false}, {"v1.0.0-rc1", "0.1.0", false}} {
		if Newer(c.l, c.c) != c.want {
			t.Errorf("Newer(%s, %s) != %v", c.l, c.c, c.want)
		}
	}
	if Stable("dev") || !Stable("1.2.3") {
		t.Fatal("Stable")
	}
}

func TestExtractTarGz(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"README.md": "x", "decathlon": "BINARY"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	got, err := extract(buf.Bytes(), false)
	if err != nil || string(got) != "BINARY" {
		t.Fatalf("%q %v", got, err)
	}
}
