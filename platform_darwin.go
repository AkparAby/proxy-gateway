package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func defaultDataRoot(executable string) (string, error) {
	dir, err := os.UserConfigDir()
	return filepath.Join(dir, "Proxy Gateway"), err
}
func coreExecutable(executable, root string) string {
	return filepath.Join(filepath.Dir(executable), "mihomo")
}
func configureCommand(cmd *exec.Cmd) {}
func openDashboard() error           { return exec.Command("/usr/bin/open", dashboardURL).Run() }
func reportLaunchError(err error) {
	for _, arg := range os.Args[1:] {
		if arg == "--background" || arg == "--restore" {
			return
		}
	}
	_ = exec.Command("/usr/bin/osascript", "-e", `display alert "Proxy Gateway" message `+appleString("启动失败："+err.Error()+"。日志位于 ~/Library/Application Support/Proxy Gateway/runtime/")).Run()
}

type Bridges struct{}

func (b *Bridges) sync(root string, force bool) error {
	if force {
		return fmt.Errorf("macOS 不支持 WSL 接入")
	}
	return nil
}
func (b *Bridges) close() {}

func integrationOptions() [][3]string {
	return [][3]string{
		{"system", "macOS 系统代理", "设置当前启用的网络服务（Wi-Fi / 以太网等）；修改时 macOS 会请求管理员授权。新增网络服务需停用再启用。"},
		{"git", "Git HTTPS", "设置当前用户的 Git HTTP 代理；需要已安装 Git。Git SSH 不受影响。"},
		{"autostart", "登录后启动网关", "请先将应用放入应用程序文件夹。仅启动网关，不自动打开页面或连接代理客户端。"},
	}
}
