package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func macNativeFixture(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte{0xcf, 0xfa, 0xed, 0xfe, 0, 0, 0, 0}, 0700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func macBundleFixture(t *testing.T, root, id, executable string) string {
	t.Helper()
	main := macNativeFixture(t, filepath.Join(root, "Contents", "MacOS", executable))
	plist := fmt.Sprintf(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>%s</string><key>CFBundleExecutable</key><string>%s</string></dict></plist>`, id, executable)
	if err := os.WriteFile(filepath.Join(root, "Contents", "Info.plist"), []byte(plist), 0600); err != nil {
		t.Fatal(err)
	}
	return main
}

// The real macOS plist parser is exercised, while Spotlight/process inventory
// is deterministic and independent of what is installed on the CI host.
func macFixtureInventory(t *testing.T, fixtureRoot, indexed, running string) macInventoryRunner {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(fixtureRoot)
	if err != nil {
		t.Fatal(err)
	}
	return func(ctx context.Context, command string, args ...string) ([]byte, error) {
		switch command {
		case "/usr/bin/mdfind":
			return []byte(indexed), nil
		case "/bin/ps":
			if strings.Join(args, " ") != "-axww -o comm=" {
				t.Fatal("process discovery must not collect command arguments", args)
			}
			return []byte(running), nil
		case "/usr/bin/plutil":
			if strings.HasPrefix(args[len(args)-1], canonical+string(filepath.Separator)) {
				return macInventoryCommand(ctx, command, args...)
			}
			return nil, os.ErrNotExist
		default:
			t.Fatalf("discovery attempted to execute %s", command)
			return nil, nil
		}
	}
}

func TestMacDesktopBundleIdentityAndHelpers(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Applications", "ChatGPT.app")
	main := macBundleFixture(t, root, "com.openai.codex", "ChatGPT")
	helper := macNativeFixture(t, filepath.Join(root, "Contents", "Frameworks", "ChatGPT Helper (Renderer).app", "Contents", "MacOS", "ChatGPT Helper (Renderer)"))
	cli := macNativeFixture(t, filepath.Join(root, "Contents", "Resources", "codex"))
	macNativeFixture(t, filepath.Join(root, "Contents", "Resources", "node"))
	// Binary Info.plist is common in shipped .app bundles.
	if _, err := macInventoryCommand(context.Background(), "/usr/bin/plutil", "-convert", "binary1", filepath.Join(root, "Contents", "Info.plist")); err != nil {
		t.Fatal(err)
	}
	run := macFixtureInventory(t, home, root+"\x00", main+"\n"+cli)
	locations, err := discoverMacApplications(context.Background(), home, "/usr/bin:/bin", nil, run)
	if err != nil {
		t.Fatal(err)
	}
	apps := applicationsAt(locations)
	if !apps[0].Available || !apps[0].Running || apps[1].Available || len(apps[0].ProcessPaths) != 3 {
		t.Fatal("renamed Codex bundle/helpers not discovered, or bundled CLI misclassified", apps)
	}
	for _, path := range []string{main, helper, cli} {
		if !slices.Contains(apps[0].ProcessPaths, path) {
			t.Fatal("missing executable", path, apps[0])
		}
	}
}

func TestMacDoesNotClassifyUnrelatedChatGPTAsCodex(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Applications", "ChatGPT.app")
	main := macBundleFixture(t, root, "com.openai.chat", "ChatGPT")
	locations, err := discoverMacApplications(context.Background(), home, "", nil, macFixtureInventory(t, home, root+"\x00", main))
	if err != nil || applicationsAt(locations)[0].Available {
		t.Fatal("unrelated bundle was classified as Codex", locations, err)
	}
}

func TestMacStandaloneDownloadsAndFinderPATH(t *testing.T) {
	for _, name := range []string{"codex", "codex-aarch64-apple-darwin", "codex-x86_64-apple-darwin"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			path := macNativeFixture(t, filepath.Join(home, "Downloads", "codex download", name))
			locations, err := discoverMacApplications(context.Background(), home, "/usr/bin:/bin", nil, macFixtureInventory(t, home, "", ""))
			apps := applicationsAt(locations)
			if err != nil || !apps[1].Available || apps[0].Available || !slices.Contains(apps[1].ProcessPaths, path) {
				t.Fatal("standalone download missing with Finder's limited PATH", apps, err)
			}
		})
	}
}

func TestMacCLISymlinksAndNpmNativeDependency(t *testing.T) {
	home := t.TempDir()
	prefix := filepath.Join(home, "homebrew")
	codex := macNativeFixture(t, filepath.Join(prefix, "Caskroom", "codex", "1.0", "codex-aarch64-apple-darwin"))
	claude := macNativeFixture(t, filepath.Join(home, ".local", "share", "claude", "versions", "2.0.0"))
	for alias, target := range map[string]string{filepath.Join(prefix, "bin", "codex"): codex, filepath.Join(home, ".local", "bin", "claude"): claude} {
		if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, alias); err != nil {
			t.Fatal(err)
		}
	}
	modules := filepath.Join(home, ".nvm", "versions", "node", "v22", "lib", "node_modules")
	npm := macNativeFixture(t, filepath.Join(modules, "@openai", "codex-darwin-arm64", "vendor", "aarch64-apple-darwin", "bin", "codex"))
	installFixture(t, filepath.Join(home, ".nvm", "versions", "node", "v22", "bin"), "codex")
	locations, err := discoverMacApplications(context.Background(), home, "/usr/bin:/bin", []string{prefix}, macFixtureInventory(t, home, "", claude))
	apps := applicationsAt(locations)
	if err != nil || !apps[1].Available || !apps[3].Available || !apps[3].Running || len(apps[1].ProcessPaths) != 2 {
		t.Fatal(apps, err)
	}
	for _, path := range []string{codex, npm} {
		if !slices.Contains(apps[1].ProcessPaths, path) {
			t.Fatal("missing real CLI binary", path, apps[1])
		}
	}
}

func TestMacInventoryDoesNotExecuteWrappersOrBindSharedRuntime(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	marker := filepath.Join(home, "wrapper-ran")
	installFixture(t, bin, "codex")
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	node := macNativeFixture(t, filepath.Join(bin, "node"))
	if err := os.Symlink(node, filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	locations, err := discoverMacApplications(context.Background(), home, bin, nil, macFixtureInventory(t, home, "", node))
	apps := applicationsAt(locations)
	if err != nil || apps[1].Available || apps[3].Available {
		t.Fatal("wrapper or shared runtime reported as native CLI", apps, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("discovery executed a wrapper")
	}
}

func TestMacRunningPortableAppsWithSpotlightUnavailable(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "private", "AppTranslocation", "random", "d", "Renamed App.app")
	desktop := macBundleFixture(t, root, "com.openai.codex", "Codex")
	cli := macNativeFixture(t, filepath.Join(home, "arbitrary tools", "codex-aarch64-apple-darwin"))
	run := macFixtureInventory(t, home, "", desktop+"\n"+cli)
	locations, err := discoverMacApplications(context.Background(), home, "", nil, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if name == "/usr/bin/mdfind" {
			return nil, fmt.Errorf("Spotlight disabled")
		}
		return run(ctx, name, args...)
	})
	apps := applicationsAt(locations)
	if err != nil || !apps[0].Available || !apps[0].Running || !apps[1].Available || !apps[1].Running {
		t.Fatal("portable/translocated processes missing", apps, err)
	}
}

func TestMacIndexedRenamedBundleAndCancellation(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "external drive", "Work Assistant.app")
	macBundleFixture(t, root, "com.openai.codex", "WorkAssistant")
	run := macFixtureInventory(t, home, root+"\x00", "")
	locations, err := discoverMacApplications(context.Background(), home, "", nil, run)
	if err != nil || !applicationsAt(locations)[0].Available {
		t.Fatal("bundle identity or declared executable was ignored", locations, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = discoverMacApplications(ctx, home, "", nil, run)
	if err == nil {
		t.Fatal("canceled scan did not report cancellation")
	}
}

func TestMacSymlinkedLargeBundleKeepsMainExecutable(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "mounted volume", "Codex.app")
	main := macBundleFixture(t, root, "com.openai.codex", "Codex")
	frameworks := filepath.Join(root, "Contents", "Frameworks")
	if err := os.MkdirAll(frameworks, 0700); err != nil {
		t.Fatal(err)
	}
	// The previous whole-bundle walk exhausted its 6,000-entry allowance in
	// Frameworks (which sorts before MacOS), then missed the main executable.
	for i := 0; i < 6010; i++ {
		if err := os.WriteFile(filepath.Join(frameworks, fmt.Sprintf("resource-%04d", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(home, "Applications", "Codex.app")
	if err := os.MkdirAll(filepath.Dir(alias), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	locations, err := discoverMacApplications(context.Background(), home, "", nil, macFixtureInventory(t, home, "", ""))
	apps := applicationsAt(locations)
	if err != nil || !slices.Contains(apps[0].ProcessPaths, main) {
		t.Fatal("large or symlinked bundle lost main executable", apps, err)
	}
}
