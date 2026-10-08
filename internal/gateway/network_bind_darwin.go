package gateway

import (
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func bindIPInterface(dialer *net.Dialer, iface net.Interface) {
	dialer.Control = func(network, _ string, raw syscall.RawConn) error {
		level, option := unix.IPPROTO_IP, unix.IP_BOUND_IF
		if network == "tcp6" {
			level, option = unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF
		}
		var bindErr error
		if err := raw.Control(func(fd uintptr) {
			bindErr = unix.SetsockoptInt(int(fd), level, option, iface.Index)
		}); err != nil {
			return err
		}
		return bindErr
	}
}
