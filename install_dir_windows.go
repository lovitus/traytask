//go:build windows

package main

func installedAppDirForUI() string {
	d, err := trayTaskInstallDir()
	if err != nil {
		return ""
	}
	return d
}
