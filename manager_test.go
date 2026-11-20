package main

import (
	"context"
	"encoding/json"
	"fmt"
	"local/proxy-gateway/internal/routing"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestStatusHealthIsASnapshot(t *testing.T) {
	m := &Manager{root: t.TempDir(), health: map[string]Health{"proxy-1": {Available: true}}}
	snapshot := m.status()["health"].(map[string]Health)
	m.health["proxy-1"] = Health{}
	delete(m.health, "proxy-1")
	if !snapshot["proxy-1"].Available {
		t.Fatal("status shares the mutable monitor health map")
	}
}

func TestManagerLatencySelectionDoesNotReloadFixedRoutes(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "runtime"), 0700)
	c := defaults()
	c.LatencyPolicy.Enabled = true
	c.LatencyPolicy.Rounds = 1
	c.LatencyPolicy.MinImprovement = 0
	c.LatencyPolicy.Cooldown = 0
	c.AppRules = []routing.AppRule{{ID: "fixed", Name: "Fixed", Enabled: true, Match: "process", ProcessPaths: []string{`C:\Apps\fixed.exe`}, OutboundID: "proxy-1"}}
	phase := 0
	selected := ""
	core := &Core{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		if strings.HasSuffix(r.URL.Path, "/delay") {
			delay := 100
			if strings.Contains(r.URL.Path, "upstream-proxy-1") {
				delay = 50
			}
			if phase == 1 {
				delay = 10
				if strings.Contains(r.URL.Path, "upstream-proxy-1") {
					delay = 500
				}
			}
			fmt.Fprintf(w, `{"delay":%d}`, delay)
		} else if r.URL.Path == "/proxies/Gateway" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			selected = body["name"]
			w.WriteHeader(204)
		} else {
			t.Errorf("automatic selection must not reload configuration: %s", r.URL.Path)
			w.WriteHeader(500)
		}
		return w.Result(), nil
	})}}
	m := &Manager{config: c, core: core, root: root, selected: "REJECT", health: c.initialHealth()}
	before := coreConfig(c, "test")
	m.cycle(context.Background())
	if m.selected != "proxy-1" {
		t.Fatal(m.selected)
	}
	phase = 1
	m.cycle(context.Background())
	if m.selected != "proxy-2" || selected != "upstream-proxy-2" {
		t.Fatal(m.selected, selected)
	}
	if !reflect.DeepEqual(before, coreConfig(m.config, "test")) {
		t.Fatal("automatic selection rewrote fixed rules")
	}
	if err := m.returnPrimary(context.Background()); err == nil {
		t.Fatal("manual priority return must be suspended during latency optimization")
	}
}

func TestSaveSelectedApplicationResolvesAndPreparesAccess(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "runtime"), 0700)
	path := filepath.Join(t.TempDir(), "Codex.exe")
	apps := []routing.Application{{ID: "codex-desktop", Name: "Codex 桌面版", Available: true, ProcessPaths: []string{path}}}
	core := &Core{dir: filepath.Join(root, "runtime"), client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		if r.URL.Path != "/configs" && r.URL.Path != "/proxies/Gateway" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		w.WriteHeader(204)
		return w.Result(), nil
	})}}
	access := []string{}
	m := &Manager{config: defaults(), root: root, core: core, selected: "proxy-1", discoverApps: func(context.Context) ([]routing.Application, error) { return apps, nil }, applyAppIntegration: func(_ context.Context, name string, enabled bool, port int) error {
		if !enabled || port != 17890 {
			t.Fatal("unexpected integration action")
		}
		access = append(access, name)
		return nil
	}}
	c := defaults()
	c.AppRules = []routing.AppRule{{ID: "selected", Match: "application", ApplicationID: "codex-desktop", Enabled: true, OutboundID: "proxy-2", ProcessPaths: []string{`C:\UntrustedOldMachine\ignored.exe`}}}
	if err := m.saveConfig(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.config.AppRules[0].ProcessPaths, []string{path}) || m.config.AppRules[0].OutboundID != "proxy-2" || m.config.AppRules[0].Name != "Codex 桌面版" {
		t.Fatal(m.config.AppRules)
	}
	if !reflect.DeepEqual(access, applicationIntegrations(m.config)) {
		t.Fatal(access)
	}
	persisted, err := readConfig(filepath.Join(root, "config.json"))
	if err != nil || !reflect.DeepEqual(persisted, m.config) {
		t.Fatal("binding was not saved", err)
	}
}

func TestManagerReloadsAndChecksEveryConfiguredOutbound(t *testing.T) {
	root := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, "runtime"), 0700)
	var mu sync.Mutex
	probed := map[string]bool{}
	selected := ""
	core := &Core{dir: filepath.Join(root, "runtime"), secret: "test", client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		switch {
		case strings.HasSuffix(r.URL.Path, "/delay"):
			mu.Lock()
			probed[r.URL.Path] = true
			mu.Unlock()
			if strings.Contains(r.URL.Path, "upstream-proxy-1/") {
				w.WriteHeader(503)
			} else {
				_, _ = w.WriteString(`{"delay":10}`)
			}
		case r.URL.Path == "/proxies/Gateway":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			selected = body["name"]
			w.WriteHeader(204)
		case r.URL.Path == "/configs":
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			w.WriteHeader(404)
		}
		return w.Result(), nil
	})}}
	c := threeOutbounds()
	m := &Manager{config: c, root: root, core: core, health: c.initialHealth(), selected: "REJECT", wake: make(chan struct{}, 1)}
	m.cycle(context.Background())
	mu.Lock()
	count := len(probed)
	mu.Unlock()
	if count != 3 || m.selected != "proxy-2" || selected != "upstream-proxy-2" {
		t.Fatalf("checks=%d selected=%s core=%s", count, m.selected, selected)
	}
	// Delete the currently selected exit, rename the third, and make it first.
	c.Outbounds = []Outbound{c.Outbounds[2], c.Outbounds[0]}
	c.Outbounds[0].Name = "DIRECT"
	if err := m.saveConfig(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if m.selected != "REJECT" || selected != "REJECT" || len(m.health) != 2 || m.health["proxy-2"].Available {
		t.Fatal("stale selection/health survived reload")
	}
	m.cycle(context.Background())
	if m.selected != "proxy-3" || selected != "upstream-proxy-3" {
		t.Fatal("new priority not selected")
	}
	saved, err := readConfig(filepath.Join(root, "config.json"))
	if err != nil || saved.Outbounds[0].Name != "DIRECT" {
		t.Fatal("new configuration was not persisted", err)
	}
	if err := m.returnPrimary(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.config.Mode = "proxy-1"
	if err := m.returnPrimary(context.Background()); err == nil {
		t.Fatal("manual return ignored fixed mode")
	}
}
