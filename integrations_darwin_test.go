package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMacProxyParsing(t *testing.T) {
	p, err := parseProxy("Enabled: Yes\nServer: ::1\nPort: 7890\nAuthenticated Proxy Enabled: 0\n")
	if err != nil || p.Server != "::1" || !p.Enabled || p.Port != 7890 {
		t.Fatal(p, err)
	}
	for _, value := range []string{
		"Enabled: Yes\nServer: old\nPort: 8080\nAuthenticated Proxy Enabled: 1",
		"Enabled: unknown\nPort: 0\nAuthenticated Proxy Enabled: 0",
		"Enabled: Yes\nServer: \nPort: 0\nAuthenticated Proxy Enabled: 0",
	} {
		if _, err := parseProxy(value); err == nil {
			t.Fatal("unsafe original accepted", value)
		}
	}
}

func TestMacCustomPortRebindPreservesOriginals(t *testing.T) {
	m := macIntegrator{root: t.TempDir(), home: t.TempDir(), port: 18890}
	original := macIntegrationState{Enabled: map[string]bool{"system": true, "git": true, "autostart": true}, Services: []macService{{Name: "Wi-Fi", HTTP: macProxy{true, "old.example", 8080}}}, GitProxy: []string{"http://old.example:8080"}}
	if err := m.save(&original); err != nil {
		t.Fatal(err)
	}
	before := string(mustRead(t, m.statePath()))
	m.admin = func(_ context.Context, script string) error {
		if !strings.Contains(script, "'127.0.0.1' '18890'") {
			t.Fatal(script)
		}
		return nil
	}
	m.command = func(_ context.Context, name string, args ...string) (string, error) {
		if name != "/usr/bin/git" || args[len(args)-1] != "http://127.0.0.1:18890" {
			t.Fatal(name, args)
		}
		return "", nil
	}
	if _, err := m.run(context.Background(), "rebind", true); err != nil {
		t.Fatal(err)
	}
	if before != string(mustRead(t, m.statePath())) {
		t.Fatal("originals replaced")
	}
	if _, err := os.Stat(m.agentPath()); !os.IsNotExist(err) {
		t.Fatal("rebind modified login startup")
	}
	restored := networkScript(original.Services, false, 18890)
	if !strings.Contains(restored, "'old.example' '8080'") || strings.Contains(restored, "'18890'") {
		t.Fatal("restore used new port", restored)
	}
}

func TestMacNetworkSnapshotAndRestore(t *testing.T) {
	name := "Wi-Fi '$(touch /tmp/should-not-exist)'"
	reads := 0
	m := macIntegrator{root: t.TempDir(), home: t.TempDir()}
	m.command = func(ctx context.Context, cmd string, args ...string) (string, error) {
		if cmd != networkSetup {
			t.Fatal(cmd)
		}
		if args[0] == "-listallnetworkservices" {
			reads++
			return "An asterisk (*) denotes that a network service is disabled.\n" + name + "\n*Disabled\n", nil
		}
		if args[1] != name {
			t.Fatal("service name lost", args)
		}
		switch args[0] {
		case "-getautoproxyurl":
			return "URL: https://example.com/a:b.pac\nEnabled: Yes\n", nil
		case "-getproxyautodiscovery":
			return "Auto Proxy Discovery: On\n", nil
		default:
			return "Enabled: No\nServer: old.example\nPort: 8080\nAuthenticated Proxy Enabled: 0\n", nil
		}
	}
	var scripts []string
	fail := true
	m.admin = func(ctx context.Context, script string) error {
		scripts = append(scripts, script)
		if fail {
			return errors.New("authorization canceled or partial write")
		}
		return nil
	}
	if _, err := m.run(context.Background(), "system", true); err == nil {
		t.Fatal("failure swallowed")
	}
	if !strings.Contains(string(mustRead(t, m.statePath())), `"system": true`) {
		t.Fatal("lost restore record")
	}
	// Retrying enable must not replace the real originals with managed values.
	if _, err := m.run(context.Background(), "system", true); err != nil {
		t.Fatal(err)
	}
	if reads != 1 {
		t.Fatal("originals recaptured")
	}
	fail = false
	if _, err := m.run(context.Background(), "restore", false); err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 2 || !strings.Contains(scripts[1], "'old.example' '8080'") || !strings.Contains(scripts[1], "'-setautoproxystate' "+shellQuote(name)+" 'on'") {
		t.Fatal(scripts)
	}
	if strings.Contains(scripts[1], "setproxybypassdomains") || strings.Contains(scripts[1], "setautoproxyurl") {
		t.Fatal("untouched settings overwritten")
	}
	// Run the generated quoting through the real shell using a harmless function.
	script := strings.ReplaceAll(scripts[0], networkSetup, "probe")
	arguments := filepath.Join(t.TempDir(), "arguments")
	script = "probe() { printf '%s\\n' \"$@\" >> " + shellQuote(arguments) + "; }\n" + script
	out, err := exec.Command("/bin/sh", "-c", script).CombinedOutput()
	if err != nil || !strings.Contains(string(mustRead(t, arguments)), name) {
		t.Fatal(string(out), err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMacAutostartPlistAndOriginalRestore(t *testing.T) {
	m := macIntegrator{root: t.TempDir(), home: t.TempDir(), executable: "/Applications/Proxy & Gateway.app/Contents/Resources/proxy-gateway"}
	if err := os.MkdirAll(filepath.Dir(m.agentPath()), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("original launch agent\n")
	if err := os.WriteFile(m.agentPath(), original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run(context.Background(), "autostart", true); err != nil {
		t.Fatal(err)
	}
	plist := string(mustRead(t, m.agentPath()))
	if !strings.Contains(plist, "&amp;") || !strings.Contains(plist, "--background") || !strings.Contains(plist, "<key>RunAtLoad</key><true/>") {
		t.Fatal(plist)
	}
	if out, err := exec.Command("/usr/bin/plutil", "-lint", m.agentPath()).CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	if _, err := m.run(context.Background(), "autostart", false); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, m.agentPath())) != string(original) {
		t.Fatal("original agent lost")
	}
	if err := os.Remove(m.agentPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run(context.Background(), "autostart", true); err != nil {
		t.Fatal(err)
	}
	if _, err := m.run(context.Background(), "restore", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.agentPath()); !os.IsNotExist(err) {
		t.Fatal("new agent not removed", err)
	}
}

func TestMacGitPreservesAllGlobalValues(t *testing.T) {
	config := filepath.Join(t.TempDir(), "gitconfig")
	m := macIntegrator{root: t.TempDir(), home: t.TempDir()}
	m.command = func(ctx context.Context, name string, args ...string) (string, error) {
		c := exec.CommandContext(ctx, name, args...)
		c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+config)
		out, err := c.CombinedOutput()
		return string(out), err
	}
	for _, v := range []string{"http://old-one:8080", "http://old-two:8081"} {
		if _, err := m.command(context.Background(), "/usr/bin/git", "config", "--global", "--add", "http.proxy", v); err != nil {
			t.Fatal(err)
		}
	}
	original := string(mustRead(t, config))
	if _, err := m.run(context.Background(), "git", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mustRead(t, config)), "127.0.0.1:17890") {
		t.Fatal("not enabled")
	}
	if _, err := m.run(context.Background(), "git", false); err != nil {
		t.Fatal(err)
	}
	if string(mustRead(t, config)) != original {
		t.Fatal("original git settings lost")
	}
}

func TestMacNativeNetworkRead(t *testing.T) {
	if os.Getenv("CI") != "true" {
		t.Skip("native service inventory is checked on the disposable CI Mac")
	}
	m := macIntegrator{command: macCommand}
	services, err := m.snapshotServices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(services) == 0 {
		t.Fatal("no services")
	}
	t.Logf("parsed %d native macOS network services", len(services))
}
