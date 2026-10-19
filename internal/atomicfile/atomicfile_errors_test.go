package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFailsWhenTheParentIsAFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(filepath.Join(file, "out"), []byte("x"), 0o600); err == nil {
		t.Fatal("write under a regular file succeeded")
	}
}

func TestWriteFailsWhenTheTargetIsADirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(target, []byte("x"), 0o600); err == nil {
		t.Fatal("write over a non-empty directory succeeded")
	}
}
