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
// refused before it's opened, as opening a FIFO blocks. The size is checked
// on the opened file and the read itself is bounded too, in case the file
// grows or is swapped in between.
func ReadRegular(path string, max int64) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readOpened(f, path, max)
}

// ReadRegularNoFollow is ReadRegular without following a symlink at the
// last path element: a symlink is refused.
func ReadRegularNoFollow(path string, max int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return readOpened(f, path, max)
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
