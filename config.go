package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
)

type Endpoint struct {
	Type string `json:"type"`
	Host string `json:"host"`
	Port int    `json:"port"`
}
type Config struct {
	Primary       string   `json:"primary"`
	Mode          string   `json:"mode"`
	AutoReturn    bool     `json:"autoReturn"`
	Interval      int      `json:"interval"`
	Timeout       int      `json:"timeout"`
	Failures      int      `json:"failures"`
	Recovery      int      `json:"recovery"`
	Targets       []string `json:"targets"`
	SpeedCat      Endpoint `json:"speedcat"`
	Clash         Endpoint `json:"clash"`
	DirectDomains []string `json:"directDomains"`
}

func defaults() Config {
	return Config{Primary: "SpeedCat", Mode: "auto", AutoReturn: true, Interval: 10, Timeout: 3000, Failures: 3, Recovery: 60, Targets: []string{"https://www.gstatic.com/generate_204", "https://cp.cloudflare.com/generate_204"}, SpeedCat: Endpoint{"http", "127.0.0.1", 17891}, Clash: Endpoint{"http", "127.0.0.1", 7892}, DirectDomains: []string{"localhost", "local"}}
}
func (c Config) validate() error {
	if c.Primary != "SpeedCat" && c.Primary != "Clash" {
		return errors.New("主出口无效")
	}
	if c.Mode != "auto" && c.Mode != "SpeedCat" && c.Mode != "Clash" {
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
	for _, e := range []Endpoint{c.SpeedCat, c.Clash} {
		ip := net.ParseIP(e.Host)
		if ip == nil || !ip.IsLoopback() || (e.Type != "http" && e.Type != "socks5") || e.Port < 1 || e.Port > 65535 || e.Port == 17890 || e.Port == 17880 || e.Port == 17881 {
			return errors.New("出口必须使用独立的本地 HTTP/SOCKS5 端口")
		}
	}
	if c.SpeedCat.Host == c.Clash.Host && c.SpeedCat.Port == c.Clash.Port {
		return errors.New("两个出口不能使用同一地址")
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
	return nil
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
	return c, c.validate()
}
func coreConfig(c Config, secret string) map[string]any {
	rules := []string{"DOMAIN,localhost,DIRECT", "DOMAIN-SUFFIX,local,DIRECT", "IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,172.16.0.0/12,DIRECT,no-resolve", "IP-CIDR,192.168.0.0/16,DIRECT,no-resolve", "IP-CIDR,100.64.0.0/10,DIRECT,no-resolve", "IP-CIDR6,::1/128,DIRECT,no-resolve", "IP-CIDR6,fc00::/7,DIRECT,no-resolve", "IP-CIDR6,fe80::/10,DIRECT,no-resolve"}
	for _, d := range c.DirectDomains {
		rules = append(rules, "DOMAIN-SUFFIX,"+d+",DIRECT")
	}
	rules = append(rules, "MATCH,Gateway")
	proxy := func(name string, e Endpoint) map[string]any {
		return map[string]any{"name": name, "type": e.Type, "server": e.Host, "port": e.Port, "udp": false}
	}
	return map[string]any{"mixed-port": 17890, "bind-address": "127.0.0.1", "allow-lan": false, "mode": "rule", "log-level": "warning", "ipv6": true, "external-controller": "127.0.0.1:17881", "secret": secret, "tun": map[string]any{"enable": false}, "dns": map[string]any{"enable": false}, "profile": map[string]any{"store-selected": false}, "proxies": []any{proxy("SpeedCat", c.SpeedCat), proxy("Clash", c.Clash)}, "proxy-groups": []any{map[string]any{"name": "Gateway", "type": "select", "proxies": []string{"REJECT", "SpeedCat", "Clash"}}}, "rules": rules}
}
func endpointURL(e Endpoint) string {
	return fmt.Sprintf("%s://%s", e.Type, net.JoinHostPort(e.Host, fmt.Sprint(e.Port)))
}
