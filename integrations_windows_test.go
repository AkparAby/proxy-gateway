package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Exercise the real PowerShell adapter using disposable Git and Docker homes.
// No system proxy, user environment, WSL distribution or login entry is touched.
func TestWindowsPortRebindPreservesOriginals(t *testing.T) {
	root := t.TempDir()
	profile := t.TempDir()
	appdata := t.TempDir()
	for _, dir := range []string{filepath.Join(root, "scripts"), filepath.Join(root, "runtime"), filepath.Join(profile, ".docker"), filepath.Join(appdata, "Docker")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	script, err := os.ReadFile("scripts/integrations.ps1")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "scripts", "integrations.ps1"), script, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USERPROFILE", profile)
	t.Setenv("APPDATA", appdata)
	gitPath := filepath.Join(profile, "test.gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", gitPath)
	gitOriginal := "[http]\n\tproxy = http://original.example:8080\n"
	_ = os.WriteFile(gitPath, []byte(gitOriginal), 0600)
	dockerPath := filepath.Join(profile, ".docker", "config.json")
	desktopPath := filepath.Join(appdata, "Docker", "settings-store.json")
	dockerOriginal := map[string]any{"proxies": map[string]any{"default": map[string]any{"httpProxy": "http://old.example:8080"}}, "credsStore": "test"}
	desktopOriginal := map[string]any{"ProxyHTTPMode": "manual", "ContainersProxyHTTPMode": "manual", "unrelated": true}
	_ = atomicJSON(dockerPath, dockerOriginal)
	_ = atomicJSON(desktopPath, desktopOriginal)
	for _, name := range []string{"git", "docker"} {
		if _, err := runIntegrationAtPort(context.Background(), root, name, true, 17890); err != nil {
			t.Fatal(err)
		}
	}
	read := func(path string) map[string]any {
		t.Helper()
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]any
		if e = json.Unmarshal(b, &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	statePath := filepath.Join(root, "runtime", "integrations-state.json")
	before := read(statePath)
	if _, err := runIntegrationAtPort(context.Background(), root, "rebind", true, 18890); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{gitPath, dockerPath} {
		b, _ := os.ReadFile(path)
		if !strings.Contains(string(b), ":18890") || strings.Contains(string(b), ":17890") {
			t.Fatal("stale integration port", path)
		}
	}
	if after := read(statePath); !reflect.DeepEqual(before, after) {
		t.Fatal("rebind overwrote restore snapshots")
	}
	for _, name := range []string{"git", "docker"} {
		if _, err := runIntegrationAtPort(context.Background(), root, name, false, 18890); err != nil {
			t.Fatal(err)
		}
	}
	out, err := exec.Command("git", "config", "--global", "--get", "http.proxy").Output()
	if err != nil || strings.TrimSpace(string(out)) != "http://original.example:8080" {
		t.Fatal("git restore failed", string(out), err)
	}
	if !reflect.DeepEqual(read(dockerPath), dockerOriginal) || !reflect.DeepEqual(read(desktopPath), desktopOriginal) {
		t.Fatal("docker originals lost")
	}
}
