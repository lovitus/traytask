//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

const windowsCreateNoWindow = 0x08000000

func configureCommandForPlatform(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windowsCreateNoWindow,
	}
}
