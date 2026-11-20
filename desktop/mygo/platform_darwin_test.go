package main

import (
	"path/filepath"
	"testing"
)

func TestMacBundleKeepsRuntimeOutsideSignedApplication(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root, manager, err := paths("/Applications/Proxy Gateway.app/Contents/MacOS/ProxyGateway")
	if err != nil || root != filepath.Join(home, "Library", "Application Support", "Proxy Gateway") ||
		manager != "/Applications/Proxy Gateway.app/Contents/Resources/proxy-gateway" {
		t.Fatalf("unexpected bundle paths: %q %q %v", root, manager, err)
	}
}
