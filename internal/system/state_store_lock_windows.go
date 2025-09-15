//go:build windows
// +build windows

package system

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	lockFileEx   = kernel32.NewProc("LockFileEx")
	unlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const (
	LOCKFILE_EXCLUSIVE_LOCK   = 0x00000002
	LOCKFILE_FAIL_IMMEDIATELY = 0x00000001
)

// lockFile applies Windows file lock (cross-process safe)
func lockFile(file *os.File) error {
	handle := syscall.Handle(file.Fd())
	var overlapped syscall.Overlapped

	ret, _, err := lockFileEx.Call(
		uintptr(handle),
		uintptr(LOCKFILE_EXCLUSIVE_LOCK|LOCKFILE_FAIL_IMMEDIATELY),
		uintptr(0),
		uintptr(0xFFFFFFFF),
		uintptr(0xFFFFFFFF),
		uintptr(unsafe.Pointer(&overlapped)),
	)

	if ret == 0 {
		return err
	}
	return nil
}

// unlockFile removes Windows file lock
func unlockFile(file *os.File) error {
	handle := syscall.Handle(file.Fd())
	var overlapped syscall.Overlapped

	ret, _, err := unlockFileEx.Call(
		uintptr(handle),
		uintptr(0),
		uintptr(0xFFFFFFFF),
		uintptr(0xFFFFFFFF),
		uintptr(unsafe.Pointer(&overlapped)),
	)

	if ret == 0 {
		return err
	}
	return nil
}
