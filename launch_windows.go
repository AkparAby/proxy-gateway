package main

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// Use the user's default HTTP handler without invoking a command shell.
func openDashboard() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole := syscall.NewLazyDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 0x2|0x4)
	if hr == 0 || hr == 1 {
		defer ole.NewProc("CoUninitialize").Call()
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	url, _ := syscall.UTF16PtrFromString(dashboardURL)
	result, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW").Call(
		0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(url)), 0, 0, 1,
	)
	if result <= 32 {
		return fmt.Errorf("无法打开默认浏览器（Windows 错误 %d），请访问 %s", result, dashboardURL)
	}
	return nil
}
