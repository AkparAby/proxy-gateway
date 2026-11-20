package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"local/proxy-gateway/internal/routing"
)

func appConfig() Config {
	c := defaults()
	c.AppRules = []routing.AppRule{
		{ID: "codex", Name: "Codex", Enabled: true, OutboundID: "proxy-1", Match: "process", ProcessPaths: []string{`C:\Program Files (x86)\Codex\Codex.exe`}},
		{ID: "claude", Name: "Claude", Enabled: true, OutboundID: "proxy-2", Match: "port", ListenPort: 17900},
	}
	return c
}

func TestAppRulesValidation(t *testing.T) {
	if err := appConfig().validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Config){
		"missing outbound":  func(c *Config) { c.Outbounds = c.Outbounds[1:] },
		"injection":         func(c *Config) { c.AppRules[0].ProcessPaths = []string{`C:\app.exe,DIRECT`} },
		"generic runtime":   func(c *Config) { c.AppRules[0].ProcessPaths = []string{"node.exe"} },
		"duplicate ID":      func(c *Config) { c.AppRules[1].ID = c.AppRules[0].ID },
		"duplicate process": func(c *Config) { c.AppRules = append(c.AppRules, c.AppRules[0]); c.AppRules[2].ID = "duplicate" },
		"entry collision":   func(c *Config) { c.AppRules[1].ListenPort = c.ProxyPort },
		"outbound loop":     func(c *Config) { c.AppRules[1].ListenPort = c.Outbounds[0].Port },
		"admin collision":   func(c *Config) { c.AppRules[1].ListenPort = 17882 },
		"duplicate port":    func(c *Config) { c.AppRules = append(c.AppRules, c.AppRules[1]); c.AppRules[2].ID = "duplicate" },
		"unknown kind":      func(c *Config) { c.AppRules[0].Match = "whatever" },
		"zero rounds":       func(c *Config) { c.LatencyPolicy.Rounds = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			c := appConfig()
			mutate(&c)
			if c.validate() == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestAppCoreRoutingIsIndependentOfDefaultSelection(t *testing.T) {
	for _, mode := range []string{"auto", "proxy-1", "proxy-2"} {
		c := appConfig()
		c.Mode = mode
		c.LatencyPolicy.Enabled = true
		core := coreConfig(c, "test")
		rules := core["rules"].([]string)
		if rules[len(rules)-1] != "MATCH,Gateway" {
			t.Fatal(rules)
		}
		found := false
		for _, rule := range rules {
			if strings.HasPrefix(rule, "PROCESS-PATH,") {
				found = true
				if !strings.HasSuffix(rule, ",upstream-proxy-1") {
					t.Fatal(rule)
				}
			}
		}
		if !found {
			t.Fatal("missing process rule")
		}
		sub := core["sub-rules"].(map[string][]string)["app-claude"]
		if sub[len(sub)-1] != "MATCH,upstream-proxy-2" {
			t.Fatal(sub)
		}
		listener := core["listeners"].([]any)[0].(map[string]any)
		if listener["listen"] != "127.0.0.1" || listener["rule"] != "app-claude" {
			t.Fatal(listener)
		}
		c.AppRules[1].Enabled = false
		core = coreConfig(c, "test")
		if core["listeners"].([]any)[0].(map[string]any)["rule"] != "" {
			t.Fatal("disabled dedicated address must follow default rules")
		}
	}
}

func TestOccupiedApplicationPortDoesNotMutateConfiguration(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c := appConfig()
	c.AppRules[1].ListenPort = l.Addr().(*net.TCPAddr).Port
	m := &Manager{config: defaults()}
	if err = m.saveConfig(context.Background(), c); err == nil {
		t.Fatal("occupied app port accepted")
	}
}

func TestOldConfigDefaultsAndAppPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	b, _ := json.Marshal(defaults())
	var old map[string]any
	json.Unmarshal(b, &old)
	delete(old, "latencyPolicy")
	delete(old, "appRules")
	if err := atomicJSON(path, old); err != nil {
		t.Fatal(err)
	}
	c, err := readConfig(path)
	if err != nil || c.LatencyPolicy.Enabled || c.LatencyPolicy.Rounds != 3 || len(c.AppRules) != 0 {
		t.Fatal(c, err)
	}
	c = appConfig()
	c.LatencyPolicy.Enabled = true
	if err := atomicJSON(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := readConfig(path)
	if err != nil || !loaded.LatencyPolicy.Enabled || len(loaded.AppRules) != 2 {
		t.Fatal(loaded, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestFailedApplicationListenerReloadRollsBack(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "runtime")
	os.Mkdir(dir, 0700)
	old := defaults()
	wanted := appConfig()
	wanted.AppRules[1].ListenPort = freeProxyPort(t)
	if err := atomicJSON(filepath.Join(root, "config.json"), old); err != nil {
		t.Fatal(err)
	}
	reloads := 0
	selected := ""
	core := &Core{dir: dir, secret: "test", client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		switch r.URL.Path {
		case "/configs":
			reloads++
			w.WriteHeader(204) // Simulate a kernel accepting config but failing to bind.
		case "/proxies/Gateway":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			selected = body["name"]
			w.WriteHeader(204)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(500)
		}
		return w.Result(), nil
	})}}
	m := &Manager{config: old, root: root, core: core, selected: "proxy-1", health: old.initialHealth()}
	if err := m.saveConfig(context.Background(), wanted); err == nil {
		t.Fatal("unbound dedicated port accepted")
	}
	if reloads != 2 || selected != "upstream-proxy-1" || !reflect.DeepEqual(m.config, old) {
		t.Fatal("previous routing not restored", reloads, selected)
	}
	persisted, err := readConfig(filepath.Join(root, "config.json"))
	if err != nil || !reflect.DeepEqual(persisted, old) {
		t.Fatal("failed save changed user config", err)
	}
	var restored map[string]json.RawMessage
	b, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	json.Unmarshal(b, &restored)
	if string(restored["listeners"]) != "[]" {
		t.Fatal("new app listener survived rollback")
	}
}
