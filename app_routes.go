package main

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"local/proxy-gateway/internal/routing"
)

func processKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(strings.ReplaceAll(path, "/", `\`))
	}
	return strings.ToLower(path)
}

func (c Config) validateRouting() error {
	p := c.LatencyPolicy
	if p.Rounds < 1 || p.Rounds > 10 || p.MinImprovement < 0 || p.MinImprovement > 10000 || p.Cooldown < 0 || p.Cooldown > 3600 {
		return fmt.Errorf("择优参数无效：连续轮数 1–10、延迟差 0–10000ms、冷却时间 0–3600 秒")
	}
	if len(c.AppRules) > 32 {
		return fmt.Errorf("最多配置 32 个应用固定规则")
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	applications := map[string]bool{}
	ports := map[int]bool{c.ProxyPort: true, 17880: true, 17881: true, 17882: true}
	for _, o := range c.Outbounds {
		ports[o.Port] = true
	}
	for _, a := range c.AppRules {
		if !outboundID.MatchString(a.ID) || ids[a.ID] {
			return fmt.Errorf("应用规则标识无效或重复")
		}
		ids[a.ID] = true
		if a.Name == "" || strings.TrimSpace(a.Name) != a.Name || utf8.RuneCountInString(a.Name) > 60 || strings.ContainsFunc(a.Name, unicode.IsControl) {
			return fmt.Errorf("应用名称须为 1–60 个字符")
		}
		if c.index(a.OutboundID) < 0 {
			return fmt.Errorf("应用 %s 引用的出口不存在，请先修改绑定或删除该规则", a.Name)
		}
		switch a.Match {
		case "application", "process":
			limit := 16
			if a.Match == "application" {
				if !routing.KnownApplication(a.ApplicationID) {
					return fmt.Errorf("请选择支持的应用")
				}
				limit = 64
				if applications[a.ApplicationID] {
					return fmt.Errorf("同一应用只能配置一条自动绑定规则")
				}
				applications[a.ApplicationID] = true
				if !a.Enabled {
					continue
				}
			}
			if len(a.ProcessPaths) < 1 || len(a.ProcessPaths) > limit {
				return fmt.Errorf("应用 %s 需要 1–16 个完整程序路径", a.Name)
			}
			for _, path := range a.ProcessPaths {
				absolute := strings.HasPrefix(path, "/") || (len(path) > 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/'))
				if !absolute || len(path) > 1024 || strings.TrimSpace(path) != path || strings.ContainsAny(path, ",*?") || strings.ContainsFunc(path, unicode.IsControl) {
					return fmt.Errorf("应用 %s 的程序路径无效：需完整路径，不能包含逗号、通配符或控制字符", a.Name)
				}
				key := processKey(path)
				if a.Enabled && paths[key] {
					return fmt.Errorf("程序路径重复绑定：%s", path)
				}
				if a.Enabled {
					paths[key] = true
				}
			}
		case "port":
			if a.ListenPort < 1024 || a.ListenPort > 65535 || ports[a.ListenPort] {
				return fmt.Errorf("应用 %s 的专属端口无效或与其他入口/出口冲突", a.Name)
			}
			ports[a.ListenPort] = true
		default:
			return fmt.Errorf("应用 %s 的匹配方式无效", a.Name)
		}
	}
	return nil
}

func (c Config) appPorts() map[int]bool {
	ports := map[int]bool{}
	for _, a := range c.AppRules {
		if a.Match == "port" {
			ports[a.ListenPort] = true
		}
	}
	return ports
}

func checkNewAppPorts(old, next Config) error {
	previous := old.appPorts()
	for port := range next.appPorts() {
		if !previous[port] {
			if err := checkProxyPort(port); err != nil {
				return fmt.Errorf("应用专属入口：%w", err)
			}
		}
	}
	return nil
}

func (c *Core) verifyAppPorts(ctx context.Context, config Config) error {
	for port := range config.appPorts() {
		if err := verifySOCKSPort(ctx, port); err != nil {
			return err
		}
	}
	return nil
}

// This reports current connections, not a cumulative hit count. Empty results
// must never be presented as proof that an application is bypassing the gateway.
func (c *Core) appConnections(ctx context.Context, config Config) (map[string]routing.AppStatus, error) {
	var data struct {
		Connections []struct {
			Metadata struct {
				ProcessPath string `json:"processPath"`
				InboundName string `json:"inboundName"`
			} `json:"metadata"`
			Chains      []string `json:"chains"`
			Rule        string   `json:"rule"`
			RulePayload string   `json:"rulePayload"`
		} `json:"connections"`
	}
	status := map[string]routing.AppStatus{}
	if len(config.AppRules) == 0 {
		return status, nil
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := c.request(ctx, "GET", "/connections", nil, &data); err != nil {
		return status, err
	}
	for _, a := range config.AppRules {
		if !a.Enabled {
			continue
		}
		s := routing.AppStatus{ActualOutbounds: []string{}}
		for _, conn := range data.Connections {
			matched := a.Match == "port" && conn.Metadata.InboundName == "app-"+a.ID
			if (a.Match == "process" || a.Match == "application") && conn.Rule == "ProcessPath" {
				for _, path := range a.ProcessPaths {
					if processKey(path) == processKey(conn.RulePayload) {
						matched = true
					}
				}
			}
			if !matched {
				continue
			}
			s.ActiveConnections++
			for _, o := range config.Outbounds {
				if slices.Contains(conn.Chains, coreName(o.ID)) && !slices.Contains(s.ActualOutbounds, o.Name) {
					s.ActualOutbounds = append(s.ActualOutbounds, o.Name)
				}
			}
			if slices.Contains(conn.Chains, "DIRECT") && !slices.Contains(s.ActualOutbounds, "本地直连") {
				s.ActualOutbounds = append(s.ActualOutbounds, "本地直连")
			}
		}
		status[a.ID] = s
	}
	return status, nil
}
