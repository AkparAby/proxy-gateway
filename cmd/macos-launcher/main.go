//go:build darwin

// Finder launches this short-lived helper. Keeping it separate lets another
// double-click reopen the dashboard while the manager runs without a Dock icon.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func run() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	data = filepath.Join(data, "Proxy Gateway", "runtime")
	if err = os.MkdirAll(data, 0700); err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(data, "launcher.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer log.Close()
	manager := filepath.Join(filepath.Dir(executable), "..", "Resources", "proxy-gateway")
	cmd := exec.Command(manager)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
func main() {
	if err := run(); err != nil {
		message := fmt.Sprintf("启动失败：%v", err)
		quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(message) + `"`
		_ = exec.Command("/usr/bin/osascript", "-e", `display alert "Proxy Gateway" message `+quoted).Run()
		os.Exit(1)
	}
}
