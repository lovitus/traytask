//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var singleInstanceHandle syscall.Handle

func ensureSingleInstance() (bool, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW := kernel32.NewProc("CreateMutexW")
	name := syscall.StringToUTF16Ptr("Local\\TrayTaskSingleton")

	h, _, callErr := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		if callErr != syscall.Errno(0) {
			return false, callErr
		}
		return false, syscall.EINVAL
	}
	singleInstanceHandle = syscall.Handle(h)

	if callErr == syscall.ERROR_ALREADY_EXISTS {
		showInfo("TrayTask 已经在运行，请在托盘中操作。", "TrayTask")
		return true, nil
	}
	return false, nil
}

func releaseSingleInstance() {
	if singleInstanceHandle != 0 {
		_ = syscall.CloseHandle(singleInstanceHandle)
		singleInstanceHandle = 0
	}
}
