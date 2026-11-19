//go:build desktop

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func paths(exe string) (string, string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "Proxy Gateway"), filepath.Join(filepath.Dir(exe), "..", "Resources", "proxy-gateway"), err
}
func configure(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
func showError(err error) {
	quote := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(err.Error()) + `"`
	_ = exec.Command("/usr/bin/osascript", "-e", `display alert "Proxy Gateway" message `+quote).Run()
}
