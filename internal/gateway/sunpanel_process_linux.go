package gateway

import (
	"os/exec"
	"syscall"
)

func configureSunPanelProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
