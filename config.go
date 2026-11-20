package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"local/proxy-gateway/internal/routing"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Endpoint struct {
	Type string `json:"type"`
	Host string `json:"host"`
	Port int    `json:"port"`
}
type Outbound struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Endpoint
}
type Config struct {
	LatencyPolicy routing.LatencyPolicy `json:"latencyPolicy"`
	AppRules      []routing.AppRule     `json:"appRules"`
	SchemaVersion int                   `json:"schemaVersion"`
	ProxyPort     int                   `json:"proxyPort"`
	Outbounds     []Outbound            `json:"outbounds"`
	Mode          string                `json:"mode"`
	AutoReturn    bool                  `json:"autoReturn"`
	Interval      int                   `json:"interval"`
	Timeout       int                   `json:"timeout"`
	Failures      int                   `json:"failures"`
	Recovery      int                   `json:"recovery"`
	Targets       []string              `json:"targets"`
	DirectDomains []string              `json:"directDomains"`
}

func defaults() Config {
	return Config{LatencyPolicy: routing.DefaultLatencyPolicy(), SchemaVersion: 2, ProxyPort: 17890, Outbounds: []Outbound{
		{"proxy-1", "代理 1", Endpoint{"http", "127.0.0.1", 17891}},
		{"proxy-2", "代理 2", Endpoint{"http", "127.0.0.1", 7892}},
	}, Mode: "auto", AutoReturn: true, Interval: 10, Timeout: 3000, Failures: 3, Recovery: 60,
		Targets: []string{"https://www.gstatic.com/generate_204", "https://cp.cloudflare.com/generate_204"}, DirectDomains: []string{"localhost", "local"}}
}

const maxOutbounds = 16

var outboundID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func (c Config) index(id string) int {
	for i, o := range c.Outbounds {
		if o.ID == id {
			return i
		}
	}
	return -1
}
func (c Config) primary() string {
	if len(c.Outbounds) == 0 {
		return "REJECT"
	}
	return c.Outbounds[0].ID
}
func (c Config) name(id string) string {
	if i := c.index(id); i >= 0 {
		return c.Outbounds[i].Name
	}
	if id == "REJECT" {
		return "暂无可用出口"
	}
	return id
}
func (c Config) initialHealth() map[string]Health {
	h := make(map[string]Health, len(c.Outbounds))
	for _, o := range c.Outbounds {
		h[o.ID] = Health{}
	}
	return h
}

// Display names never become Mihomo identifiers, so renaming is safe and names
// such as DIRECT or Gateway cannot collide with built-in routes.
func coreName(id string) string {
	if id == "REJECT" {
		return id
	}
	return "upstream-" + id
}
func (c Config) validate() error {
	if c.ProxyPort < 1024 || c.ProxyPort > 65535 || c.ProxyPort == 17880 || c.ProxyPort == 17881 || c.ProxyPort == 17882 {
		return errors.New("网关端口须为 1024–65535，不能使用管理端口 17880、17881、17882")
	}
	if c.SchemaVersion != 2 {
		return errors.New("配置版本不支持，请刷新管理页或更新程序")
	}
	if len(c.Outbounds) < 1 || len(c.Outbounds) > maxOutbounds {
		return fmt.Errorf("请配置 1–%d 个代理出口", maxOutbounds)
	}
	if c.Mode != "auto" && c.index(c.Mode) < 0 {
		return errors.New("模式无效")
	}
	if c.Interval < 3 || c.Interval > 300 || c.Timeout < 500 || c.Timeout > 10000 || c.Failures < 1 || c.Failures > 10 || c.Recovery < 0 || c.Recovery > 3600 {
		return errors.New("检测参数超出允许范围")
	}
	if len(c.Targets) < 1 || len(c.Targets) > 5 {
		return errors.New("需要 1–5 个 HTTPS 检测地址")
	}
	for _, s := range c.Targets {
		u, e := url.Parse(s)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return errors.New("检测地址必须为不含凭据的 HTTPS URL")
		}
	}
	ids, names, addresses := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, o := range c.Outbounds {
		if !outboundID.MatchString(o.ID) || o.ID == "auto" || o.ID == "REJECT" || ids[o.ID] {
			return errors.New("出口标识无效或重复")
		}
		ids[o.ID] = true
		name := strings.TrimSpace(o.Name)
		if name == "" || name != o.Name || utf8.RuneCountInString(name) > 60 || strings.ContainsFunc(name, unicode.IsControl) || names[strings.ToLower(name)] {
			return errors.New("出口名称须为不重复的 1–60 个字符，不能包含控制字符或首尾空格")
		}
		names[strings.ToLower(name)] = true
		e := o.Endpoint
		ip := net.ParseIP(e.Host)
		if ip == nil || !ip.IsLoopback() || (e.Type != "http" && e.Type != "socks5") || e.Port < 1 || e.Port > 65535 || e.Port == c.ProxyPort || e.Port == 17880 || e.Port == 17881 || e.Port == 17882 {
			return errors.New("出口必须使用独立的本地 HTTP/SOCKS5 端口")
		}
		address := net.JoinHostPort(ip.String(), fmt.Sprint(e.Port))
		if addresses[address] {
			return errors.New("不同出口不能使用同一地址和端口")
		}
		addresses[address] = true
	}
	for _, d := range c.DirectDomains {
		if len(d) == 0 || len(d) > 253 {
			return errors.New("直连域名无效")
		}
		for _, r := range d {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
				return errors.New("直连域名只接受域名后缀，不含协议或通配符")
			}
		}
	}
	return c.validateRouting()
}
func atomicJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return atomicWrite(path, append(b, '\n'))
}
func atomicWrite(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".gateway-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(name, path)
}
func readConfig(path string) (Config, error) {
	c := defaults()
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return c, atomicJSON(path, c)
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, e
	}
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(b, &fields); e != nil {
		return c, e
	}
	_, hasOutbounds := fields["outbounds"]
	legacy := !hasOutbounds && (fields["primary"] != nil || fields["speedcat"] != nil || fields["clash"] != nil)
	if legacy {
		old := struct {
			Primary  string   `json:"primary"`
			SpeedCat Endpoint `json:"speedcat"`
			Clash    Endpoint `json:"clash"`
		}{"SpeedCat", Endpoint{"http", "127.0.0.1", 17891}, Endpoint{"http", "127.0.0.1", 7892}}
		if e = json.Unmarshal(b, &old); e != nil {
			return c, e
		}
		if old.Primary != "SpeedCat" && old.Primary != "Clash" {
			return c, errors.New("旧配置主出口无效")
		}
		c.Outbounds = []Outbound{{"SpeedCat", "SpeedCat", old.SpeedCat}, {"Clash", "Clash Verge", old.Clash}}
		if old.Primary == "Clash" {
			c.Outbounds[0], c.Outbounds[1] = c.Outbounds[1], c.Outbounds[0]
		}
	}
	if e = c.validate(); e != nil {
		return c, e
	}
	if legacy {
		backupDir := filepath.Join(filepath.Dir(path), "backups")
		if e = os.MkdirAll(backupDir, 0700); e != nil {
			return c, e
		}
		backup, err := os.CreateTemp(backupDir, "config-v1-*.json")
		if err != nil {
			return c, err
		}
		_, err = backup.Write(b)
		closeErr := backup.Close()
		if err != nil {
			return c, err
		}
		if closeErr != nil {
			return c, closeErr
		}
		if e = atomicJSON(path, c); e != nil {
			return c, e
		}
	}
	return c, nil
}
func coreConfig(c Config, secret string) map[string]any {
	rules := []string{"DOMAIN,localhost,DIRECT", "DOMAIN-SUFFIX,local,DIRECT", "IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,172.16.0.0/12,DIRECT,no-resolve", "IP-CIDR,192.168.0.0/16,DIRECT,no-resolve", "IP-CIDR,100.64.0.0/10,DIRECT,no-resolve", "IP-CIDR6,::1/128,DIRECT,no-resolve", "IP-CIDR6,fc00::/7,DIRECT,no-resolve", "IP-CIDR6,fe80::/10,DIRECT,no-resolve"}
	localRules := append([]string(nil), rules...)
	listeners := []any{}
	subRules := map[string][]string{}
	for _, app := range c.AppRules {
		if !app.Enabled && app.Match != "port" {
			continue
		}
		target := coreName(app.OutboundID)
		if app.Match == "process" || app.Match == "application" {
			for _, path := range app.ProcessPaths {
				rules = append(rules, "PROCESS-PATH,"+processKey(path)+","+target)
			}
		} else {
			name := "app-" + app.ID
			rule := ""
			if app.Enabled {
				rule = name
				subRules[name] = append(append([]string(nil), localRules...), "MATCH,"+target)
			}
			listeners = append(listeners, map[string]any{"name": name, "type": "mixed", "listen": "127.0.0.1", "port": app.ListenPort, "udp": false, "rule": rule})
		}
	}
	for _, d := range c.DirectDomains {
		rules = append(rules, "DOMAIN-SUFFIX,"+d+",DIRECT")
	}
	rules = append(rules, "MATCH,Gateway")
	proxies := make([]any, 0, len(c.Outbounds))
	choices := []string{"REJECT"}
	for _, o := range c.Outbounds {
		name := coreName(o.ID)
		proxies = append(proxies, map[string]any{"name": name, "type": o.Type, "server": o.Host, "port": o.Port, "udp": false})
		choices = append(choices, name)
	}
	return map[string]any{"listeners": listeners, "sub-rules": subRules, "find-process-mode": "strict", "mixed-port": c.ProxyPort, "bind-address": "127.0.0.1", "allow-lan": false, "mode": "rule", "log-level": "warning", "ipv6": true, "external-controller": "127.0.0.1:17881", "secret": secret, "tun": map[string]any{"enable": false}, "dns": map[string]any{"enable": false}, "profile": map[string]any{"store-selected": false}, "proxies": proxies, "proxy-groups": []any{map[string]any{"name": "Gateway", "type": "select", "proxies": choices}}, "rules": rules}
}
