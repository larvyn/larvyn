//go:build !windows

package safefile

import "syscall"

// oNonblock makes opening a FIFO return at once instead of waiting for a
// writer. Regular files ignore it.
const oNonblock = syscall.O_NONBLOCK
