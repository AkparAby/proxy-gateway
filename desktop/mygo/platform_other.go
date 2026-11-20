//go:build !windows && !darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func paths(exe string) (string, string, error) {
	root := filepath.Dir(exe)
	return root, filepath.Join(root, "runtime", "gateway-manager"), nil
}
func configure(cmd *exec.Cmd) {}
func showError(err error)     { fmt.Fprintln(os.Stderr, err) }
