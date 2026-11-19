package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func threeOutbounds() Config {
	c := defaults()
	c.Outbounds = append(c.Outbounds, Outbound{"proxy-3", "第三个代理", Endpoint{"socks5", "127.0.0.1", 1080}})
	return c
}

func TestGenericConfigValidation(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"empty":               func(c *Config) { c.Outbounds = nil },
		"too many":            func(c *Config) { c.Outbounds = make([]Outbound, maxOutbounds+1) },
		"duplicate ID":        func(c *Config) { c.Outbounds[1].ID = c.Outbounds[0].ID },
		"reserved ID":         func(c *Config) { c.Outbounds[1].ID = "REJECT" },
		"mode ID":             func(c *Config) { c.Outbounds[1].ID = "auto" },
		"path ID":             func(c *Config) { c.Outbounds[1].ID = "../Gateway" },
		"duplicate name":      func(c *Config) { c.Outbounds[1].Name = c.Outbounds[0].Name },
		"case duplicate":      func(c *Config) { c.Outbounds[0].Name = "Proxy"; c.Outbounds[1].Name = "PROXY" },
		"empty name":          func(c *Config) { c.Outbounds[0].Name = "  " },
		"control name":        func(c *Config) { c.Outbounds[0].Name = "proxy\nline" },
		"long name":           func(c *Config) { c.Outbounds[0].Name = strings.Repeat("名", 61) },
		"canonical duplicate": func(c *Config) { c.Outbounds[1].Host = "::ffff:127.0.0.1"; c.Outbounds[1].Port = c.Outbounds[0].Port },
		"remote host":         func(c *Config) { c.Outbounds[0].Host = "192.168.1.2" },
		"desktop port":        func(c *Config) { c.Outbounds[0].Port = 17882 },
		"missing fixed":       func(c *Config) { c.Mode = "deleted" },
		"future schema":       func(c *Config) { c.SchemaVersion = 3 },
	} {
		t.Run(name, func(t *testing.T) {
			c := threeOutbounds()
			mutate(&c)
			if c.validate() == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	c := threeOutbounds()
	c.Outbounds[0].Name = "DIRECT"
	c.Outbounds[1].Name = "Gateway"
	c.Outbounds[2].Host = "::1"
	c.Mode = c.Outbounds[2].ID
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	core := coreConfig(c, "test-secret")
	proxies := core["proxies"].([]any)
	choices := core["proxy-groups"].([]any)[0].(map[string]any)["proxies"].([]string)
	if len(proxies) != 3 || len(choices) != 4 || choices[0] != "REJECT" {
		t.Fatal("missing dynamic proxies")
	}
	for i, p := range proxies {
		if p.(map[string]any)["name"] != coreName(c.Outbounds[i].ID) || choices[i+1] != coreName(c.Outbounds[i].ID) {
			t.Fatal("display name used as core identifier")
		}
	}
}

func TestLegacyConfigMigrationPreservesSettingsAndBackup(t *testing.T) {
	for _, mode := range []string{"auto", "SpeedCat", "Clash"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			original := []byte(`{"primary":"Clash","mode":"` + mode + `","autoReturn":false,"interval":7,"timeout":2200,"failures":2,"recovery":43,"speedcat":{"type":"socks5","host":"127.0.0.1","port":1081},"clash":{"type":"http","host":"::1","port":1082},"targets":["https://example.com/health"],"directDomains":["private.example"]}`)
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			c, err := readConfig(path)
			if err != nil {
				t.Fatal(err)
			}
			if c.primary() != "Clash" || c.Mode != mode || c.AutoReturn || c.Interval != 7 || c.Timeout != 2200 || c.Failures != 2 || c.Recovery != 43 {
				t.Fatalf("lost policy: %+v", c)
			}
			if c.Outbounds[0].Port != 1082 || c.Outbounds[1].Type != "socks5" || c.Outbounds[1].Port != 1081 || c.Targets[0] != "https://example.com/health" || c.DirectDomains[0] != "private.example" {
				t.Fatal("lost endpoint or routing settings")
			}
			files, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "config-v1-*.json"))
			if len(files) != 1 {
				t.Fatal("missing migration backup")
			}
			backup, _ := os.ReadFile(files[0])
			if string(backup) != string(original) {
				t.Fatal("backup is not exact original")
			}
			var migrated map[string]any
			data, _ := os.ReadFile(path)
			if err := json.Unmarshal(data, &migrated); err != nil {
				t.Fatal(err)
			}
			if migrated["speedcat"] != nil || migrated["primary"] != nil || migrated["outbounds"] == nil {
				t.Fatal("legacy fields retained")
			}
			if _, err := readConfig(path); err != nil {
				t.Fatal(err)
			}
			files, _ = filepath.Glob(filepath.Join(filepath.Dir(path), "backups", "config-v1-*.json"))
			if len(files) != 1 {
				t.Fatal("migration repeated")
			}
		})
	}
}

func TestInvalidConfigIsNeverMigratedOrReplaced(t *testing.T) {
	for _, data := range []string{
		`{"primary":"wrong","speedcat":{"type":"http","host":"127.0.0.1","port":1081}}`,
		`{"outbounds":[],"primary":"SpeedCat"}`,
		`{"schemaVersion":3,"primary":"SpeedCat"}`,
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		_ = os.WriteFile(path, []byte(data), 0600)
		if _, err := readConfig(path); err == nil {
			t.Fatal("invalid config accepted")
		}
		after, _ := os.ReadFile(path)
		if string(after) != data {
			t.Fatal("invalid config overwritten")
		}
	}
}

func TestFreshConfigUsesGenericDefaults(t *testing.T) {
	c, err := readConfig(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	if c.Outbounds[0].Name != "代理 1" || c.Outbounds[1].Name != "代理 2" {
		t.Fatal("defaults are tied to software names")
	}
}
