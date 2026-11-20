package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"local/proxy-gateway/internal/routing"
)

type applicationLocations struct {
	CodexDesktop  []string
	ClaudeDesktop []string
	CodexCLI      []string
	ClaudeCLI     []string
	RunningPaths  []string
}

func discoverApplications(ctx context.Context) ([]routing.Application, error) {
	locations, err := installedApplicationLocations(ctx)
	return applicationsAt(locations), err
}

func applicationsAt(locations applicationLocations) []routing.Application {
	apps := []routing.Application{
		{ID: "codex-desktop", Name: "Codex 桌面版", Kind: "desktop"},
		{ID: "codex-cli", Name: "Codex CLI", Kind: "cli"},
		{ID: "claude-desktop", Name: "Claude 桌面版", Kind: "desktop"},
		{ID: "claude-cli", Name: "Claude Code CLI", Kind: "cli"},
	}
	roots := [][]string{locations.CodexDesktop, locations.CodexCLI, locations.ClaudeDesktop, locations.ClaudeCLI}
	for i := range apps {
		app := &apps[i]
		paths := map[string]string{}
		visited := map[string]bool{}
		for _, root := range roots[i] {
			if root == "" || visited[processKey(root)] {
				continue
			}
			visited[processKey(root)] = true
			count := 0
			_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				count++
				if count > 6000 {
					return fs.SkipAll
				}
				if entry.IsDir() {
					rel, _ := filepath.Rel(root, path)
					if strings.Count(rel, string(filepath.Separator)) > 8 {
						return fs.SkipDir
					}
					return nil
				}
				if !appExecutable(app.ID, entry.Name()) {
					return nil
				}
				// Windows Store directories can deny traversal to EvalSymlinks
				// even though their installed executable is readable and runnable.
				resolved := path
				if target, err := filepath.EvalSymlinks(path); err == nil {
					resolved = target
				}
				info, err := os.Stat(resolved)
				if err != nil || !info.Mode().IsRegular() {
					return nil
				}
				absolute, err := filepath.Abs(resolved)
				if err == nil {
					paths[processKey(absolute)] = absolute
				}
				return nil
			})
		}
		for _, path := range paths {
			app.ProcessPaths = append(app.ProcessPaths, path)
		}
		slices.Sort(app.ProcessPaths)
		// Keep schema limits deterministic even if many old versions remain installed.
		if len(app.ProcessPaths) > 64 {
			app.ProcessPaths = app.ProcessPaths[len(app.ProcessPaths)-64:]
		}
		app.Available = len(app.ProcessPaths) > 0
		app.Detail = "未检测到安装，请安装或打开应用后刷新列表"
		if app.Available {
			app.Detail = "已自动识别，无需填写代理地址或程序路径"
		}
		for _, running := range locations.RunningPaths {
			if resolved, err := filepath.EvalSymlinks(running); err == nil {
				running = resolved
			}
			if _, ok := paths[processKey(running)]; ok {
				app.Running = true
			}
		}
	}
	return apps
}

func appExecutable(id, name string) bool {
	name = strings.ToLower(strings.TrimSuffix(strings.ToLower(name), ".exe"))
	if strings.HasPrefix(id, "codex") {
		return name == "codex" || name == "codex-code-mode-host" || name == "codex-voice-host" || (id == "codex-desktop" && (name == "chatgpt" || strings.HasPrefix(name, "codex helper")))
	}
	return name == "claude" || (id == "claude-desktop" && strings.HasPrefix(name, "claude helper"))
}

func addRunningInstallation(locations *applicationLocations, path string) {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(path)), ".exe")
	if base != "codex" && base != "claude" {
		return
	}
	dir := filepath.Dir(path)
	regular := func(path string) bool { info, err := os.Stat(path); return err == nil && info.Mode().IsRegular() }
	desktop := regular(filepath.Join(dir, "resources", "app.asar"))
	if strings.EqualFold(filepath.Base(dir), "resources") && regular(filepath.Join(dir, "app.asar")) {
		desktop = true
		dir = filepath.Dir(dir)
	}
	if base == "codex" {
		if desktop {
			locations.CodexDesktop = append(locations.CodexDesktop, dir)
		} else {
			locations.CodexCLI = append(locations.CodexCLI, path)
		}
	}
	if base == "claude" {
		if desktop {
			locations.ClaudeDesktop = append(locations.ClaudeDesktop, dir)
		} else {
			locations.ClaudeCLI = append(locations.ClaudeCLI, path)
		}
	}
}

func automaticApps(c Config) bool {
	for _, rule := range c.AppRules {
		if rule.Match == "application" {
			return true
		}
	}
	return false
}

func resolveApplicationRules(c Config, apps []routing.Application) (Config, error) {
	c.AppRules = slices.Clone(c.AppRules)
	for i, rule := range c.AppRules {
		if rule.Match != "application" {
			continue
		}
		if !routing.KnownApplication(rule.ApplicationID) {
			return c, fmt.Errorf("请选择支持的已安装应用")
		}
		if !rule.Enabled {
			continue
		}
		found := false
		for _, app := range apps {
			if app.ID != rule.ApplicationID {
				continue
			}
			if !app.Available {
				return c, fmt.Errorf("%s：%s", app.Name, app.Detail)
			}
			c.AppRules[i].Name = app.Name
			c.AppRules[i].ProcessPaths = slices.Clone(app.ProcessPaths)
			c.AppRules[i].ListenPort = 0
			found = true
			break
		}
		if !found {
			return c, fmt.Errorf("尚未识别应用，请刷新应用列表后重试")
		}
	}
	return c, nil
}
