// Package atomicfile writes a file through a temporary file and a rename, so
// a reader sees the old contents or the new ones, never a half-written file.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write replaces path with data, forces mode and creates the parent directory
// private to the user. The temporary file lives next to the target so the
// rename stays on one filesystem.
func Write(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
