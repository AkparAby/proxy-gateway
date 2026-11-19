package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
