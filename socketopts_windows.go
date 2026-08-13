//go:build windows

package goserver

import "syscall"

func setSocketOptions(fd uintptr) error {
	handle := syscall.Handle(fd)

	_ = syscall.SetsockoptInt(handle, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)

	_ = syscall.SetsockoptInt(handle, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)

	return nil
}
