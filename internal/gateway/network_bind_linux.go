package gateway

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func bindIPInterface(dialer *net.Dialer, iface net.Interface) {
	dialer.Control = func(_, _ string, raw syscall.RawConn) error {
		var bindErr error
		if err := raw.Control(func(fd uintptr) {
			bindErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, iface.Name)
		}); err != nil {
			return err
		}
		return bindErr
	}
}
