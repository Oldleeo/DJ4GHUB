//go:build darwin

package main

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func bindDialerToInterface(dialer *net.Dialer, interfaceIndex int) {
	dialer.Control = func(_, _ string, connection syscall.RawConn) error {
		var socketErr error
		if err := connection.Control(func(fd uintptr) {
			socketErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, interfaceIndex)
		}); err != nil {
			return err
		}
		return socketErr
	}
}
