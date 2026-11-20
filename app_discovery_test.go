package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"local/proxy-gateway/internal/routing"
)

func installFixture(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, file := range files {
		path := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0700); err != nil {
			t.Fatal(err)
		}
	}
}

func TestApplicationDiscoveryAcrossMachines(t *testing.T) {
	var first Config
	for _, machine := range []string{"Alice computer", "Bob portable drive"} {
		root := filepath.Join(t.TempDir(), machine)
		codex := filepath.Join(root, "WindowsApps", "OpenAI.Codex_version", "app")
		claude := filepath.Join(root, "npm", "node_modules", "@anthropic-ai", "claude-code")
		installFixture(t, codex, "Codex.exe", filepath.Join("resources", "codex.exe"), filepath.Join("resources", "app.asar"), "node.exe", "rg.exe")
		installFixture(t, claude, filepath.Join("bin", "claude.exe"), "node.exe")
		apps := applicationsAt(applicationLocations{CodexDesktop: []string{codex}, ClaudeCLI: []string{claude}, RunningPaths: []string{filepath.Join(codex, "Codex.exe")}})
		if !apps[0].Available || !apps[0].Running || len(apps[0].ProcessPaths) != 2 || apps[2].Available || !apps[3].Available {
			t.Fatal(apps)
		}
		c := defaults()
		if len(first.AppRules) > 0 {
			c = first
		}
		c.AppRules = []routing.AppRule{{ID: "codex", Name: "Codex", Enabled: true, Match: "application", ApplicationID: "codex-desktop", OutboundID: "proxy-1", ProcessPaths: []string{`C:\OtherComputer\Codex.exe`}}}
		resolved, err := resolveApplicationRules(c, apps)
		if err != nil {
			t.Fatal(err)
		}
		if err = resolved.validate(); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(resolved.AppRules[0].ProcessPaths, filepath.Join(codex, "Codex.exe")) {
			t.Fatal("kept another computer's path")
		}
		if c.AppRules[0].ProcessPaths[0] != `C:\OtherComputer\Codex.exe` {
			t.Fatal("mutated input config")
		}
		first = resolved
	}
}

func TestPortableRunningApplicationDiscovery(t *testing.T) {
	root := t.TempDir()
	desktop := filepath.Join(root, "arbitrary portable folder")
	installFixture(t, desktop, "Claude.exe", filepath.Join("resources", "app.asar"))
	cli := filepath.Join(root, "tools")
	installFixture(t, cli, "codex.exe")
	locations := applicationLocations{}
	addRunningInstallation(&locations, filepath.Join(desktop, "Claude.exe"))
	addRunningInstallation(&locations, filepath.Join(cli, "codex.exe"))
	apps := applicationsAt(locations)
	if !apps[2].Available || !apps[1].Available || apps[0].Available || apps[3].Available {
		t.Fatal(apps)
	}
}

func TestUnknownApplicationCannotSilentlyUseCachedPath(t *testing.T) {
	c := defaults()
	c.AppRules = []routing.AppRule{{ID: "claude", Name: "Claude", Enabled: true, Match: "application", ApplicationID: "claude-cli", OutboundID: "proxy-1", ProcessPaths: []string{`C:\cached\claude.exe`}}}
	if _, err := resolveApplicationRules(c, applicationsAt(applicationLocations{})); err == nil {
		t.Fatal("missing app reported as bound")
	}
	c.AppRules[0].Enabled = false
	resolved, err := resolveApplicationRules(c, nil)
	if err != nil || resolved.validate() != nil {
		t.Fatal("cannot disable an uninstalled application", err)
	}
}

func TestRealLocalApplicationDiscovery(t *testing.T) {
	if os.Getenv("GATEWAY_TEST_DISCOVERY") == "" {
		t.Skip("optional read-only host inventory")
	}
	apps, err := discoverApplications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	locations, _ := installedApplicationLocations(context.Background())
	for _, root := range locations.CodexDesktop {
		if _, err := os.Stat(filepath.Join(root, "Codex.exe")); err == nil && !apps[0].Available {
			t.Fatal("installed Store executable was not discovered")
		}
	}
	for _, app := range apps {
		t.Logf("%s: installed=%v, running=%v, detected executables=%d", app.Name, app.Available, app.Running, len(app.ProcessPaths))
	}
	if !reflect.DeepEqual(apps[0].ID, "codex-desktop") {
		t.Fatal(apps)
	}
}
