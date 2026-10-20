// Package safefile replaces files without following symbolic links and
// without leaving a torn file behind.
package safefile

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"strconv"
)

// Write replaces path with data. Project folders are untrusted input, so a
// symbolic link at path is refused rather than followed. The data goes to a
// temp file in the same directory, is synced, and is renamed over path, so a
// reader sees the old or the new content, never a mix. Like os.WriteFile, an
// existing file keeps its mode and perm applies only to a new one.
func Write(path string, data []byte, perm os.FileMode) error {
	fi, err := os.Lstat(path)
	switch {
	case err == nil && fi.Mode()&fs.ModeSymlink != 0:
		return fmt.Errorf("refusing to write %s: it is a symbolic link", path)
	case err == nil:
		perm = fi.Mode().Perm()
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	tmp := path + "." + strconv.FormatUint(uint64(rand.Uint32()), 36) + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if fi != nil {
		if err := os.Chmod(tmp, perm); err != nil {
			return err
		}
	}
	return os.Rename(tmp, path)
}
