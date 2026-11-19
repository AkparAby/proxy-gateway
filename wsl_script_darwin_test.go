package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWSLScriptCustomPortAndIdempotence(t *testing.T) {
	taskHome := t.TempDir()
	bin := t.TempDir()
	_ = os.WriteFile(filepath.Join(bin, "ip"), []byte("#!/bin/sh\nprintf 'default via 172.20.0.1 dev eth0\\n'\n"), 0700)
	_ = os.WriteFile(filepath.Join(taskHome, ".profile"), []byte("# original profile\n"), 0600)
	_ = os.WriteFile(filepath.Join(taskHome, ".bashrc"), []byte("# original bashrc\n"), 0600)
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("/bin/sh", args...)
		cmd.Env = append(os.Environ(), "HOME="+taskHome, "PATH="+bin+":/usr/bin:/bin")
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatal(string(b), err)
		}
		return string(b)
	}
	run("scripts/wsl-integration.sh", "enable", "18890")
	run("scripts/wsl-integration.sh", "enable", "18990")
	out := run("-c", `. "$HOME/.config/local-proxy-gateway/env.sh"; printf '%s %s' "$HTTP_PROXY" "$ALL_PROXY"`)
	if out != "http://172.20.0.1:18990 socks5h://172.20.0.1:18990" {
		t.Fatal(out)
	}
	b, _ := os.ReadFile(filepath.Join(taskHome, ".profile"))
	if strings.Count(string(b), "# Local proxy gateway") != 1 {
		t.Fatal("duplicated shell hook")
	}
	b, _ = os.ReadFile(filepath.Join(taskHome, ".config", "local-proxy-gateway", ".profile.before"))
	if string(b) != "# original profile\n" {
		t.Fatal("shell original replaced")
	}
	run("scripts/wsl-integration.sh", "disable", "18990")
	if _, err := os.Stat(filepath.Join(taskHome, ".config", "local-proxy-gateway", "enabled")); !os.IsNotExist(err) {
		t.Fatal("disable failed")
	}
}
