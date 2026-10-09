//go:build !linux

package gateway

import "os/exec"

func configureSunPanelProcess(cmd *exec.Cmd) {}
