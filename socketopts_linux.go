//go:build linux

package goserver

import "golang.org/x/sys/unix"

func setSocketOptions(fd uintptr) error {
	sock := int(fd)
	_ = unix.SetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
	_ = unix.SetsockoptInt(sock, unix.IPPROTO_TCP, unix.TCP_NODELAY, 1)
	_ = unix.SetsockoptInt(sock, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	_ = unix.SetsockoptInt(sock, unix.IPPROTO_TCP, unix.TCP_QUICKACK, 1)
	_ = unix.SetsockoptInt(sock, unix.IPPROTO_TCP, unix.TCP_FASTOPEN, 5)
	return nil
}
