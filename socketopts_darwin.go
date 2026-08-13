//go:build darwin || freebsd || netbsd || openbsd

package goserver

import "syscall"

func setSocketOptions(fd uintptr) error {
	sock := int(fd)
	_ = syscall.SetsockoptInt(sock, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	_ = syscall.SetsockoptInt(sock, syscall.IPPROTO_TCP, syscall.TCP_NODELAY, 1)
	_ = syscall.SetsockoptInt(sock, syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
	return nil
}
