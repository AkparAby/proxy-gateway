//go:build desktop

package main

import (
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

func paths(exe string) (string, string, error) {
	root := filepath.Dir(exe)
	return root, filepath.Join(root, "runtime", "gateway-manager.exe"), nil
}
func configure(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
func showError(err error) {
	message, _ := syscall.UTF16PtrFromString(err.Error())
	title, _ := syscall.UTF16PtrFromString("Proxy Gateway")
	syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(message)), uintptr(unsafe.Pointer(title)), 0x10)
}
