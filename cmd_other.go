//go:build !windows

package main

import "os/exec"

func configureCommandForPlatform(cmd *exec.Cmd) {
	_ = cmd
}

func attachProcessForLifecycle(cmd *exec.Cmd) error {
	_ = cmd
	return nil
}
