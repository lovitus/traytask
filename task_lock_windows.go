//go:build windows

package main

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

var (
	taskLockKernel32         = syscall.NewLazyDLL("kernel32.dll")
	taskLockCreateMutexW     = taskLockKernel32.NewProc("CreateMutexW")
	taskLockReleaseMutexProc = taskLockKernel32.NewProc("ReleaseMutex")
	taskLockCloseHandleProc  = taskLockKernel32.NewProc("CloseHandle")
)

func acquireTaskRunLock(lockKey string) (func(), bool, error) {
	name := fmt.Sprintf("Local\\TrayTaskTaskRun_%s", lockKey)
	namePtr := syscall.StringToUTF16Ptr(name)

	h, _, callErr := taskLockCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		if callErr != syscall.Errno(0) {
			return nil, false, callErr
		}
		return nil, false, syscall.EINVAL
	}
	handle := syscall.Handle(h)
	if callErr == syscall.ERROR_ALREADY_EXISTS {
		_, _, _ = taskLockCloseHandleProc.Call(uintptr(handle))
		return nil, false, nil
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			_, _, _ = taskLockReleaseMutexProc.Call(uintptr(handle))
			_, _, _ = taskLockCloseHandleProc.Call(uintptr(handle))
		})
	}
	return release, true, nil
}
