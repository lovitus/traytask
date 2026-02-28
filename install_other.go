//go:build !windows

package main

func ensureInstalledAndRelaunch() (bool, error) {
	return false, nil
}
