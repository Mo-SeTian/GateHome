//go:build !linux && !darwin

package gateway

import (
	"errors"
	"net"
	"syscall"
)

func bindIPInterface(dialer *net.Dialer, iface net.Interface) {
	dialer.Control = func(_, _ string, _ syscall.RawConn) error {
		return errors.New("当前平台不支持绑定网卡进行公网查询")
	}
}
