//go:build !windows

package main

import "time"

func ensureSingleInstance() (bool, error) {
	return false, nil
}

func releaseSingleInstance() {}

func startExternalShutdownWatcher(onShutdown func()) {
	_ = onShutdown
}

func requestExistingInstanceExitAndWait(timeout time.Duration) bool {
	_ = timeout
	return true
}
