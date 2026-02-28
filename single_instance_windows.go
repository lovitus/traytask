//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	singleInstanceHandle syscall.Handle
	shutdownEventHandle  syscall.Handle
)

const (
	singleInstanceMutexName = "Local\\TrayTaskSingleton"
	shutdownEventName       = "Local\\TrayTaskQuitSignal"

	eventModifyState = 0x0002
	synchronizePerm  = 0x00100000
	waitObject0      = 0
	waitTimeout      = 0x00000102
)

func ensureSingleInstance() (bool, error) {
	h, alreadyExists, err := createSingleInstanceMutex()
	if err != nil {
		return false, err
	}
	singleInstanceHandle = h

	eh, err := createOrOpenShutdownEvent()
	if err == nil {
		shutdownEventHandle = eh
	}

	if alreadyExists {
		showInfo("TrayTask 已经在运行，请在托盘中操作。", "TrayTask")
		return true, nil
	}
	return false, nil
}

func releaseSingleInstance() {
	if shutdownEventHandle != 0 {
		_ = syscall.CloseHandle(shutdownEventHandle)
		shutdownEventHandle = 0
	}
	if singleInstanceHandle != 0 {
		_ = syscall.CloseHandle(singleInstanceHandle)
		singleInstanceHandle = 0
	}
}

func startExternalShutdownWatcher(onShutdown func()) {
	if shutdownEventHandle == 0 {
		return
	}
	go func(h syscall.Handle) {
		for {
			r, _ := waitForSingleObject(h, syscall.INFINITE)
			if r == waitObject0 {
				onShutdown()
				return
			}
			return
		}
	}(shutdownEventHandle)
}

func requestExistingInstanceExitAndWait(timeout time.Duration) bool {
	if !isExistingInstanceRunning() {
		return true
	}
	_ = signalShutdownEvent()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isExistingInstanceRunning() {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return !isExistingInstanceRunning()
}

func isExistingInstanceRunning() bool {
	h, err := openSingleInstanceMutex()
	if err != nil {
		return false
	}
	_ = syscall.CloseHandle(h)
	return true
}

func createSingleInstanceMutex() (syscall.Handle, bool, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW := kernel32.NewProc("CreateMutexW")
	name := syscall.StringToUTF16Ptr(singleInstanceMutexName)

	h, _, callErr := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		if callErr != syscall.Errno(0) {
			return 0, false, callErr
		}
		return 0, false, syscall.EINVAL
	}
	alreadyExists := callErr == syscall.ERROR_ALREADY_EXISTS
	return syscall.Handle(h), alreadyExists, nil
}

func openSingleInstanceMutex() (syscall.Handle, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procOpenMutexW := kernel32.NewProc("OpenMutexW")
	name := syscall.StringToUTF16Ptr(singleInstanceMutexName)

	h, _, callErr := procOpenMutexW.Call(uintptr(synchronizePerm), 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		if callErr == syscall.ERROR_FILE_NOT_FOUND {
			return 0, callErr
		}
		if callErr != syscall.Errno(0) {
			return 0, callErr
		}
		return 0, syscall.EINVAL
	}
	return syscall.Handle(h), nil
}

func createOrOpenShutdownEvent() (syscall.Handle, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procCreateEventW := kernel32.NewProc("CreateEventW")
	name := syscall.StringToUTF16Ptr(shutdownEventName)

	h, _, callErr := procCreateEventW.Call(0, 0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		if callErr != syscall.Errno(0) {
			return 0, callErr
		}
		return 0, syscall.EINVAL
	}
	return syscall.Handle(h), nil
}

func signalShutdownEvent() error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procOpenEventW := kernel32.NewProc("OpenEventW")
	procSetEvent := kernel32.NewProc("SetEvent")
	name := syscall.StringToUTF16Ptr(shutdownEventName)

	h, _, callErr := procOpenEventW.Call(uintptr(eventModifyState), 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		if callErr == syscall.ERROR_FILE_NOT_FOUND {
			return nil
		}
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return syscall.EINVAL
	}
	defer syscall.CloseHandle(syscall.Handle(h))

	r, _, setErr := procSetEvent.Call(h)
	if r == 0 {
		if setErr != syscall.Errno(0) {
			return setErr
		}
		return syscall.EINVAL
	}
	return nil
}

func waitForSingleObject(h syscall.Handle, timeout uint32) (uint32, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procWaitForSingleObject := kernel32.NewProc("WaitForSingleObject")
	r, _, callErr := procWaitForSingleObject.Call(uintptr(h), uintptr(timeout))
	if r == 0xFFFFFFFF {
		if callErr != syscall.Errno(0) {
			return uint32(r), callErr
		}
		return uint32(r), syscall.EINVAL
	}
	if uint32(r) == waitTimeout {
		return uint32(r), nil
	}
	return uint32(r), nil
}
