//go:build !windows

package discovery

import "syscall"

func reusePort(network, address string, c syscall.RawConn) error {
	return nil
}
