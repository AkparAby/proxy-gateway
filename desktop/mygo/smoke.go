package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	client "local/proxy-gateway/internal/gatewayclient"
	"local/proxy-gateway/internal/routing"
)

type fixtureAPI struct{}

func (fixtureAPI) Status(context.Context) (client.Status, error) { return fixtureStatus(), nil }
func (fixtureAPI) Check(context.Context, string) (uint64, error) {
	return 0, fmt.Errorf("UI 验证模式不会修改系统设置")
}
func (fixtureAPI) Post(context.Context, string, string, any) (string, error) {
	return "", fmt.Errorf("UI 验证模式不会修改系统设置")
}
func fixtureStatus() client.Status {
	return client.Status{Version: version, Platform: "windows", Token: "fixture", Proxy: "127.0.0.1:17890", Selected: "a", Reason: "代理 1 的延迟更低，已连续满足切换条件。", Config: client.Config{SchemaVersion: 2, ProxyPort: 17890, Mode: "auto", AutoReturn: true, Interval: 10, Timeout: 3000, Failures: 3, Recovery: 60, Targets: []string{"https://www.gstatic.com/generate_204", "https://cp.cloudflare.com/generate_204"}, DirectDomains: []string{"localhost", "local"}, LatencyPolicy: routing.LatencyPolicy{Enabled: true, Rounds: 3, MinImprovement: 20, Cooldown: 30}, Outbounds: []client.Outbound{{ID: "a", Name: "代理 1", Type: "http", Host: "127.0.0.1", Port: 7890}, {ID: "b", Name: "代理 2", Type: "socks5", Host: "127.0.0.1", Port: 1080}}, AppRules: []routing.AppRule{{ID: "codex", Name: "Codex 桌面版", ApplicationID: "codex-desktop", Enabled: true, Match: "application", ProcessPaths: []string{`C:\Apps\Codex.exe`}, OutboundID: "a"}}}, Health: map[string]client.Health{"a": {Available: true, Checked: true, LastOK: true, Latency: 42, SmoothedLatency: 45, Detail: "HTTPS 检测通过"}, "b": {Available: true, Checked: true, LastOK: true, Latency: 86, SmoothedLatency: 81, Detail: "HTTPS 检测通过"}}, Applications: []routing.Application{{ID: "codex-desktop", Name: "Codex 桌面版", Available: true, Running: true, Kind: "desktop", ProcessPaths: []string{`C:\Apps\Codex.exe`}, Detail: "已识别桌面应用，保存后自动配置接入。"}, {ID: "claude-cli", Name: "Claude Code CLI", Available: true, Kind: "cli", ProcessPaths: []string{`C:\Tools\claude.exe`}, Detail: "已识别 CLI，保存后请重开终端。"}}, AppStatus: map[string]routing.AppStatus{"codex": {ActiveConnections: 2, ActualOutbounds: []string{"代理 1"}}}, Integrations: map[string]bool{"system": true, "terminal": true}, IntegrationOptions: [][3]string{{"system", "系统代理", "保存原设置，可随时恢复"}, {"terminal", "终端环境变量", "重开终端后生效"}, {"autostart", "登录后启动", "在后台托盘中运行"}}, Events: []client.Event{{Time: time.Date(2026, 11, 20, 16, 12, 8, 0, time.Local), Message: "代理 1 检测成功，平滑延迟 45 ms"}, {Time: time.Date(2026, 11, 20, 16, 12, 5, 0, time.Local), Message: "Codex 桌面版已固定使用代理 1"}}}
}

func runSmoke(ctx context.Context, output string) error {
	if err := os.MkdirAll(output, 0700); output != "" && err != nil {
		return err
	}
	m := newModel(ctx, fixtureAPI{})
	m.receive(fixtureStatus(), nil)
	var failure error
	mygo.App.SetName("Proxy Gateway UI 验证")
	mygo.App.WhenReady(func() {
		frames := 0
		title := "Proxy Gateway · 界面验证（自动关闭）"
		if output == "" {
			title = "Proxy Gateway · 界面预览（示例数据）"
		}
		win := mygo.NewWindow(mygo.WindowOptions{Title: title, Width: 1160, Height: 820, MinWidth: 900, MinHeight: 660, Content: ui.View(func(c *ui.Context) { frames++; m.view(c) })})
		if err := win.SetIcon(icon); err != nil {
			failure = fmt.Errorf("set window icon: %w", err)
			mygo.App.Quit()
			return
		}
		win.Show()
		m.dispatch = win.Update
		if output == "" {
			time.AfterFunc(100*time.Millisecond, win.Show)
			win.OnClosed(func() { mygo.App.Quit() })
			return
		}
		go func() {
			time.Sleep(100 * time.Millisecond)
			win.Show()
			var diagnostics []byte
			seen := map[[32]byte]bool{}
			lastFrame := 0
			for _, page := range pages {
				mygo.RunOnMain(func() { m.page = page.id; win.Invalidate() })
				time.Sleep(250 * time.Millisecond)
				mygo.RunOnMain(func() {
					if m.page != page.id {
						failure = fmt.Errorf("page update lost: want %s, got %s", page.id, m.page)
					}
				})
				if failure != nil {
					break
				}
				data, err := win.CapturePage()
				mygo.RunOnMain(func() {
					diagnostics = fmt.Appendf(diagnostics, "page=%s frames=%d handle=%x visible=%v\n", m.page, frames, win.NativeHandle(), win.IsVisible())
					if !win.IsVisible() || frames <= lastFrame {
						err = fmt.Errorf("page %s did not render a visible frame", page.id)
					}
					lastFrame = frames
				})
				hash := sha256.Sum256(data)
				if err == nil && seen[hash] {
					err = fmt.Errorf("page %s repeated a previous screenshot", page.id)
				}
				seen[hash] = true
				if err == nil {
					err = os.WriteFile(filepath.Join(output, "mygo-"+page.id+".png"), data, 0600)
				}
				if err != nil {
					failure = err
					break
				}
			}
			mygo.RunOnMain(func() {
				m.page = "overview"
				m.checkPending = true
				for id, h := range m.status.Health {
					h.Checking = true
					m.status.Health[id] = h
				}
				win.Invalidate()
			})
			var previous [32]byte
			for frame := range 2 {
				time.Sleep(180 * time.Millisecond)
				data, err := win.CapturePage()
				if err == nil {
					err = os.WriteFile(filepath.Join(output, fmt.Sprintf("mygo-checking-%d.png", frame)), data, 0600)
				}
				hash := sha256.Sum256(data)
				if err == nil && frame > 0 && hash == previous {
					err = fmt.Errorf("network checking spinner did not animate")
				}
				previous = hash
				if err != nil {
					failure = err
					break
				}
			}
			mygo.RunOnMain(func() {
				m.resetCheck()
				m.status = fixtureStatus()
				m.page = "apps"
				m.openRule(-1, false)
				win.Invalidate()
			})
			time.Sleep(250 * time.Millisecond)
			data, err := win.CapturePage()
			if err == nil {
				err = os.WriteFile(filepath.Join(output, "mygo-app-picker.png"), data, 0600)
			}
			if err != nil {
				failure = err
			}
			_ = os.WriteFile(filepath.Join(output, "native-diagnostics.txt"), diagnostics, 0600)
			mygo.App.Quit()
		}()
	})
	if err := mygo.App.Run(); err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	if output == "" {
		return nil
	}
	return os.WriteFile(filepath.Join(output, "native-result.txt"), []byte("PASS: rendered all six native MyGo pages, animated check progress and application picker; no system changes"), 0600)
}
