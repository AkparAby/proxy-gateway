package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func installedApplicationLocations(ctx context.Context) (applicationLocations, error) {
	locations := applicationLocations{}
	local, roaming, home := os.Getenv("LOCALAPPDATA"), os.Getenv("APPDATA"), os.Getenv("USERPROFILE")
	if local != "" {
		locations.CodexDesktop = append(locations.CodexDesktop, filepath.Join(local, "Programs", "Codex"), filepath.Join(local, "Codex"))
		locations.ClaudeDesktop = append(locations.ClaudeDesktop, filepath.Join(local, "AnthropicClaude"), filepath.Join(local, "Programs", "Claude"))
	}
	if roaming != "" {
		locations.CodexCLI = append(locations.CodexCLI, filepath.Join(roaming, "npm", "node_modules", "@openai", "codex"))
		locations.ClaudeCLI = append(locations.ClaudeCLI, filepath.Join(roaming, "npm", "node_modules", "@anthropic-ai", "claude-code"))
	}
	if home != "" {
		locations.CodexCLI = append(locations.CodexCLI, filepath.Join(home, ".local", "bin", "codex.exe"))
		locations.ClaudeCLI = append(locations.ClaudeCLI, filepath.Join(home, ".local", "bin", "claude.exe"))
	}
	// Read installation metadata and executable paths only; never read process
	// command lines, app credentials, or run npm/shell wrappers discovered on PATH.
	const script = `[Console]::OutputEncoding=[Text.UTF8Encoding]::new($false)
$ErrorActionPreference='Stop'
$warnings=@();$packages=@();$processes=@()
try{$packages=@(Get-AppxPackage | Where-Object { $_.Name -eq 'OpenAI.Codex' -or $_.Name -match '^(Anthropic\.Claude|Claude)$' } | Select-Object Name,InstallLocation)}catch{$warnings+='Windows app package inventory unavailable'}
try{$processes=@(Get-CimInstance Win32_Process -Filter "Name = 'Codex.exe' OR Name = 'Claude.exe' OR Name = 'ChatGPT.exe'" | Select-Object Name,ExecutablePath)}catch{$warnings+='Running process inventory unavailable'}
$commands=@();foreach($name in @('codex','claude')){$command=Get-Command $name -ErrorAction SilentlyContinue;if($command){$commands+=@{Name=$name;Source=$command.Source}}}
@{packages=$packages;processes=$processes;commands=$commands;warnings=$warnings}|ConvertTo-Json -Depth 4 -Compress`
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	configureCommand(cmd)
	data, err := cmd.Output()
	if err != nil {
		return locations, fmt.Errorf("读取已安装应用失败：%w", err)
	}
	var inventory struct {
		Warnings  []string                                 `json:"warnings"`
		Packages  []struct{ Name, InstallLocation string } `json:"packages"`
		Processes []struct{ Name, ExecutablePath string }  `json:"processes"`
		Commands  []struct{ Name, Source string }          `json:"commands"`
	}
	if err = json.Unmarshal(data, &inventory); err != nil {
		return locations, fmt.Errorf("解析应用列表失败：%w", err)
	}
	for _, p := range inventory.Packages {
		if p.Name == "OpenAI.Codex" {
			locations.CodexDesktop = append(locations.CodexDesktop, filepath.Join(p.InstallLocation, "app"))
		} else {
			locations.ClaudeDesktop = append(locations.ClaudeDesktop, p.InstallLocation)
		}
	}
	for _, p := range inventory.Processes {
		if p.ExecutablePath == "" {
			continue
		}
		locations.RunningPaths = append(locations.RunningPaths, p.ExecutablePath)
		addRunningInstallation(&locations, p.ExecutablePath)
		lower := strings.ToLower(p.ExecutablePath)
		if strings.Contains(lower, `\resources\`) && strings.Contains(lower, "codex") {
			locations.CodexDesktop = append(locations.CodexDesktop, filepath.Dir(filepath.Dir(p.ExecutablePath)))
		}
		if strings.EqualFold(p.Name, "Claude.exe") && strings.Contains(lower, "anthropicclaude") {
			locations.ClaudeDesktop = append(locations.ClaudeDesktop, filepath.Dir(p.ExecutablePath))
		}
	}
	for _, command := range inventory.Commands {
		root := filepath.Dir(command.Source)
		if command.Name == "codex" {
			locations.CodexCLI = append(locations.CodexCLI, filepath.Join(root, "node_modules", "@openai", "codex"))
			if strings.HasSuffix(strings.ToLower(command.Source), ".exe") {
				locations.CodexCLI = append(locations.CodexCLI, command.Source)
			}
		}
		if command.Name == "claude" {
			locations.ClaudeCLI = append(locations.ClaudeCLI, filepath.Join(root, "node_modules", "@anthropic-ai", "claude-code"))
			if strings.HasSuffix(strings.ToLower(command.Source), ".exe") {
				locations.ClaudeCLI = append(locations.ClaudeCLI, command.Source)
			}
		}
	}
	if len(inventory.Warnings) > 0 {
		return locations, fmt.Errorf("部分安装或进程信息无法读取；已继续检查常见安装目录与命令行入口")
	}
	return locations, nil
}
