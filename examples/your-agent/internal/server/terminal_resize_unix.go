//go:build !windows

package server

import (
	"os"
	"syscall"
	"unsafe"
)

func setWinsize(file *os.File, cols, rows uint16) error {
	size := struct {
		Row, Col, Xpixel, Ypixel uint16
	}{Row: rows, Col: cols}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, file.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&size)))
	if errno != 0 {
		return errno
	}
	return nil
}
