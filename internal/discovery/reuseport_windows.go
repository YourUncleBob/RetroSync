//go:build windows

package discovery

import "syscall"

func reusePort(network, address string, c syscall.RawConn) error {
	var setsockoptErr error
	err := c.Control(func(fd uintptr) {
		setsockoptErr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	})
	if err != nil {
		return err
	}
	return setsockoptErr
}
