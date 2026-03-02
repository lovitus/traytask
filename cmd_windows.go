//go:build windows

package main

import (
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

const windowsCreateNoWindow = 0x08000000

func configureCommandForPlatform(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: windowsCreateNoWindow,
	}
}

const (
	processSetQuota              = 0x0100
	processTerminate             = 0x0001
	jobObjectLimitKillOnJobClose = 0x00002000
	jobObjectInfoClassExtLimit   = 9
	windowsLifecycleJobName      = "Local\\TrayTaskProcessJob"
)

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

var (
	windowsLifecycleKernel32            = syscall.NewLazyDLL("kernel32.dll")
	windowsProcCreateJobObjectW         = windowsLifecycleKernel32.NewProc("CreateJobObjectW")
	windowsProcSetInformationJobObject  = windowsLifecycleKernel32.NewProc("SetInformationJobObject")
	windowsProcAssignProcessToJobObject = windowsLifecycleKernel32.NewProc("AssignProcessToJobObject")
	windowsProcOpenProcess              = windowsLifecycleKernel32.NewProc("OpenProcess")
	windowsProcCloseHandle              = windowsLifecycleKernel32.NewProc("CloseHandle")
	windowsLifecycleJobInitOnce         sync.Once
	windowsLifecycleJobHandle           syscall.Handle
	windowsLifecycleJobInitErr          error
)

func ensureLifecycleJobObject() (syscall.Handle, error) {
	windowsLifecycleJobInitOnce.Do(func() {
		name := syscall.StringToUTF16Ptr(windowsLifecycleJobName)
		h, _, err := windowsProcCreateJobObjectW.Call(0, uintptr(unsafe.Pointer(name)))
		if h == 0 {
			if err != syscall.Errno(0) {
				windowsLifecycleJobInitErr = err
			} else {
				windowsLifecycleJobInitErr = syscall.EINVAL
			}
			return
		}
		windowsLifecycleJobHandle = syscall.Handle(h)

		info := jobObjectExtendedLimitInformation{}
		info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
		r, _, setErr := windowsProcSetInformationJobObject.Call(
			uintptr(windowsLifecycleJobHandle),
			uintptr(jobObjectInfoClassExtLimit),
			uintptr(unsafe.Pointer(&info)),
			uintptr(uint32(unsafe.Sizeof(info))),
		)
		if r == 0 {
			_, _, _ = windowsProcCloseHandle.Call(uintptr(windowsLifecycleJobHandle))
			windowsLifecycleJobHandle = 0
			if setErr != syscall.Errno(0) {
				windowsLifecycleJobInitErr = setErr
			} else {
				windowsLifecycleJobInitErr = syscall.EINVAL
			}
		}
	})
	return windowsLifecycleJobHandle, windowsLifecycleJobInitErr
}

func attachProcessForLifecycle(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	job, err := ensureLifecycleJobObject()
	if err != nil {
		return err
	}
	ph, _, openErr := windowsProcOpenProcess.Call(
		uintptr(processSetQuota|processTerminate),
		0,
		uintptr(uint32(cmd.Process.Pid)),
	)
	if ph == 0 {
		if openErr != syscall.Errno(0) {
			return openErr
		}
		return syscall.EINVAL
	}
	defer windowsProcCloseHandle.Call(ph)

	r, _, assignErr := windowsProcAssignProcessToJobObject.Call(uintptr(job), ph)
	if r == 0 {
		if assignErr != syscall.Errno(0) {
			// In restricted environments the process may already belong to another job.
			if assignErr == syscall.ERROR_ACCESS_DENIED {
				return nil
			}
			return assignErr
		}
		return syscall.EINVAL
	}
	return nil
}
