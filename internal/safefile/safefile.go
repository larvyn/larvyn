// Package safefile reads project files that Larvyn opens on its own, so a
// file planted in a project can't hang or exhaust the app.
package safefile

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrTooLarge is returned for a file bigger than the limit.
var ErrTooLarge = errors.New("file is too large")

// ReadRegular reads a regular file of at most max bytes. A symlink is
// followed only to a regular file: a device (/dev/zero), FIFO or socket is
// refused. It's opened non-blocking where the OS has that, so a FIFO swapped
// in after the check can't block the open either; the type and size are
// checked again on the opened file, and the read itself is bounded.
func ReadRegular(path string, max int64) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.OpenFile(path, os.O_RDONLY|oNonblock, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readOpened(f, path, max)
}

// ReadRegularIn reads the regular file rel inside dir, of at most max bytes.
// rel itself may not be a symlink, and it's opened through os.Root, so
// even a symlink swapped in after the check can't lead out of dir.
func ReadRegularIn(dir, rel string, max int64) ([]byte, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	st, err := root.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", rel)
	}
	f, err := root.OpenFile(rel, os.O_RDONLY|oNonblock, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readOpened(f, rel, max)
}

func readOpened(f *os.File, path string, max int64) ([]byte, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if st.Size() > max {
		return nil, ErrTooLarge
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, ErrTooLarge
	}
	return b, nil
}
