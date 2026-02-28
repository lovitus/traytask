package main

import (
	_ "embed"
	"runtime"
)

var (
	//go:embed assets/tray_icon.png
	trayIconPNG []byte

	//go:embed assets/tray_icon_windows.ico
	trayIconWindowsICO []byte
)

func trayIconBytes() []byte {
	if runtime.GOOS == "windows" {
		return trayIconWindowsICO
	}
	return trayIconPNG
}
