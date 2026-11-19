package main

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func defaultDataRoot(executable string) (string, error) { return filepath.Dir(executable), nil }
func reportLaunchError(err error)                       {}
func coreExecutable(executable, root string) string {
	return filepath.Join(root, "runtime", "mihomo.exe")
}
func configureCommand(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func integrationOptions() [][3]string {
	return [][3]string{
		{"system", "Windows 系统代理", "浏览器、桌面应用及 Docker 的系统代理模式。"},
		{"terminal", "终端与新启动的子进程", "用户级代理环境变量，重新打开程序生效。"},
		{"git", "Git HTTPS", "配置当前用户的 Git HTTP 代理，私有 IP 仓库直连。"},
		{"docker", "Docker 构建与容器", "配置构建及新容器代理；镜像拉取仍需 Desktop 系统代理。"},
		{"wsl", "WSL Ubuntu", "安装可启停的 shell 接入文件，自动发现 Windows 主机地址。"},
		{"autostart", "登录后启动网关", "仅启动网关，不自动打开页面；代理客户端仍需保持运行。"},
	}
}

func runIntegration(ctx context.Context, root, name string, enabled bool) (string, error) {
	mode := "disable"
	if enabled {
		mode = "enable"
	}
	shell, err := exec.LookPath("pwsh.exe")
	if err != nil {
		shell = "powershell.exe"
	}
	cmd := exec.CommandContext(ctx, shell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(root, "scripts", "integrations.ps1"), "-Action", name, "-Mode", mode, "-Root", root)
	configureCommand(cmd)
	b, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(b))
	if err != nil {
		return output, fmt.Errorf("接入设置失败：%s (%w)", output, err)
	}
	return output, nil
}
