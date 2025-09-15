//go:build !windows
// +build !windows

package system

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// lockFile applies POSIX fcntl lock to a file descriptor (cross-process safe)
func lockFile(file *os.File) error {
	flock := &syscall.Flock_t{
		Type:   syscall.F_WRLCK, // Exclusive write lock
		Whence: int16(io.SeekStart),
		Start:  0,
		Len:    0,
	}
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, flock); err != nil {
		return fmt.Errorf("failed to acquire file lock: %w", err)
	}
	return nil
}

// unlockFile removes POSIX fcntl lock from a file descriptor
func unlockFile(file *os.File) error {
	flock := &syscall.Flock_t{
		Type:   syscall.F_UNLCK,
		Whence: int16(io.SeekStart),
		Start:  0,
		Len:    0,
	}
	if err := syscall.FcntlFlock(file.Fd(), syscall.F_SETLK, flock); err != nil {
		return fmt.Errorf("failed to acquire file lock: %w", err)
	}
	return nil
}
