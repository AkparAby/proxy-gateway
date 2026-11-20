//go:build desktop

package desktop

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"image/png"
	client "local/proxy-gateway/internal/gatewayclient"
	"local/proxy-gateway/internal/routing"
	"os"
	"path/filepath"
	"testing"
)

func TestStartGatewayOnlyOfferedWhenOffline(t *testing.T) {
	u := testWindow(t)
	if u.retry.Visible() {
		t.Fatal("running gateway still offers start")
	}
	u.offline(fmt.Errorf("offline"))
	if !u.retry.Visible() || u.retry.Disabled() {
		t.Fatal("offline gateway cannot be started")
	}
	calls := 0
	u.OnReconnect = func() { calls++ }
	u.Reconnect()
	u.Reconnect()
	if calls != 1 || !u.retry.Disabled() || u.retry.Text != "正在启动…" {
		t.Fatal("duplicate startup allowed or startup feedback missing")
	}
	u.ReconnectFinished(fmt.Errorf("startup failed"))
	if u.retry.Disabled() || u.retry.Text != "启动网关" {
		t.Fatal("failed startup cannot be retried")
	}
	u.display(exampleStatus())
	if u.retry.Visible() {
		t.Fatal("start action remains after recovery")
	}
}

type fakeAPI struct{}

func (fakeAPI) Status(context.Context) (client.Status, error)             { return exampleStatus(), nil }
func (fakeAPI) Post(context.Context, string, string, any) (string, error) { return "ok", nil }
func exampleStatus() client.Status {
	return client.Status{Version: "v0.4.0", Selected: "a", Token: "test", Proxy: "127.0.0.1:17890", Config: client.Config{SchemaVersion: 2, ProxyPort: 17890, Mode: "auto", AutoReturn: true, Interval: 10, Timeout: 3000, Failures: 3, Recovery: 60, Targets: []string{"https://www.gstatic.com/generate_204"}, DirectDomains: []string{"localhost", "local"}, Outbounds: []client.Outbound{{ID: "a", Name: "主代理", Type: "http", Host: "127.0.0.1", Port: 7890}, {ID: "b", Name: "备用代理", Type: "socks5", Host: "::1", Port: 1080}}}, Health: map[string]client.Health{"a": {Available: true, Checked: true, LastOK: true, Latency: 42, Detail: "HTTPS 检测通过"}}, IntegrationOptions: [][3]string{{"system", "系统代理", "保存原设置，可随时恢复"}, {"autostart", "登录后启动", "后台托盘运行"}}}
}
func testWindow(t *testing.T) *Window {
	t.Helper()
	a := test.NewApp()
	t.Cleanup(a.Quit)
	u := New(a, fakeAPI{}, context.Background())
	u.Window.Resize(fyne.NewSize(1080, 760))
	u.display(exampleStatus())
	return u
}
func TestDraftSurvivesHealthRefresh(t *testing.T) {
	u := testWindow(t)
	u.name.SetText("修改中的名称")
	u.host.SetText("::1")
	u.port.SetText("19080")
	u.proxyPort.SetText("18890")
	u.protocol.SetSelected("SOCKS5")
	u.mode.SetSelectedIndex(1)
	s := exampleStatus()
	s.Health["a"] = client.Health{Available: false, Checked: true}
	u.display(s)
	if u.proxyPort.Text != "18890" {
		t.Fatal("refresh discarded gateway port")
	}
	if c, err := u.collect(); err != nil || c.ProxyPort != 18890 {
		t.Fatal(c, err)
	}
	if u.name.Text != "修改中的名称" || u.draft.Outbounds[0].Port != 19080 || u.draft.Outbounds[0].Type != "socks5" || u.draft.Mode != "a" || !u.dirty {
		t.Fatal("refresh discarded edits", u.draft)
	}
	if u.save.Disabled() {
		t.Fatal("dirty configuration cannot be saved")
	}
	u.fill(s.Config)
	if u.dirty || u.name.Text != "主代理" || !u.save.Disabled() {
		t.Fatal("discard did not restore saved config")
	}
}

func TestGatewayPortEditorRejectsConflicts(t *testing.T) {
	u := testWindow(t)
	for _, port := range []string{"text", "0", "80", "65536", "17880", "17881", "17882", "7890"} {
		u.proxyPort.SetText(port)
		if _, err := u.collect(); err == nil {
			t.Fatal("invalid port accepted", port)
		}
	}
	u.fill(exampleStatus().Config)
	if u.proxyPort.Text != "17890" || u.dirty {
		t.Fatal("port discard failed")
	}
}
func TestEditingPriorityBoundsAndFixedDeletion(t *testing.T) {
	u := testWindow(t)
	u.mode.SetSelectedIndex(2)
	u.list.Select(1)
	u.moveOutbound(-1)
	if u.draft.Outbounds[0].ID != "b" || u.draft.Mode != "b" {
		t.Fatal("reorder lost stable identity")
	}
	u.removeOutbound()
	if u.draft.Mode != "auto" || len(u.draft.Outbounds) != 1 {
		t.Fatal("fixed deletion did not choose auto")
	}
	if !u.remove.Disabled() {
		t.Fatal("must keep one exit")
	}
	for len(u.draft.Outbounds) < 16 {
		u.addOutbound()
	}
	if !u.add.Disabled() {
		t.Fatal("maximum not enforced")
	}
	seen := map[string]bool{}
	for _, o := range u.draft.Outbounds {
		if seen[o.ID] {
			t.Fatal("duplicate ID")
		}
		seen[o.ID] = true
	}
	u.interval.SetText("wrong")
	if _, err := u.collect(); err == nil {
		t.Fatal("bad numeric value accepted")
	}
}
func TestExternalConfigChangeAndPlatformIntegrations(t *testing.T) {
	u := testWindow(t)
	u.name.SetText("本地草稿")
	s := exampleStatus()
	s.Config.Outbounds[0].Port = 29999
	u.display(s)
	if u.draft.Outbounds[0].Port == 29999 {
		t.Fatal("external configuration overwrote draft")
	}
	if u.saved.Text != "配置已在其他窗口更新，请撤销修改后重新编辑" {
		t.Fatal(u.saved.Text)
	}
	if len(u.integrationButtons) != 2 || u.integrationButtons["wsl"] != nil {
		t.Fatal("wrong platform controls")
	}
	s.Integrations = map[string]bool{"system": true}
	u.display(s)
	if u.integrationButtons["system"].Text != "停用并恢复" {
		t.Fatal("integration state not reflected")
	}
	u.busy = true
	u.controls()
	if !u.save.Disabled() || !u.name.Disabled() || !u.integrationButtons["system"].Disabled() {
		t.Fatal("busy edits not blocked")
	}
}
func TestCanvasCapture(t *testing.T) {
	dir := os.Getenv("GATEWAY_UI_SCREENSHOT_DIR")
	if dir == "" {
		t.Skip("optional rendered artifact")
	}
	u := testWindow(t)
	u.Show()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "fyne-preview.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = png.Encode(f, u.Window.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
	var findTabs func(fyne.CanvasObject) *container.AppTabs
	findTabs = func(obj fyne.CanvasObject) *container.AppTabs {
		if tabs, ok := obj.(*container.AppTabs); ok {
			return tabs
		}
		if c, ok := obj.(*fyne.Container); ok {
			for _, child := range c.Objects {
				if tabs := findTabs(child); tabs != nil {
					return tabs
				}
			}
		}
		return nil
	}
	tabs := findTabs(u.Window.Content())
	if tabs == nil {
		t.Fatal("missing desktop tabs")
	}
	tabs.SelectIndex(2)
	portFile, err := os.Create(filepath.Join(dir, "fyne-port-settings.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer portFile.Close()
	if err = png.Encode(portFile, u.Window.Canvas().Capture()); err != nil {
		t.Fatal(err)
	}
	u.optimal.SetChecked(true)
	u.draft.AppRules = []routing.AppRule{{ID: "codex", Name: "Codex", Enabled: true, Match: "port", ListenPort: 17900, OutboundID: "a"}, {ID: "claude", Name: "Claude", Enabled: true, Match: "process", ProcessPaths: []string{`C:\Apps\Claude\Claude.exe`}, OutboundID: "b"}}
	u.renderAppRules()
	for _, item := range []struct {
		index  int
		name   string
		bottom bool
	}{{3, "fyne-latency-settings.png", false}, {3, "fyne-policy-bottom.png", true}, {4, "fyne-app-rules.png", false}} {
		tabs.SelectIndex(item.index)
		if item.bottom {
			tabs.Items[item.index].Content.(*container.Scroll).ScrollToBottom()
		}
		file, err := os.Create(filepath.Join(dir, item.name))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, u.Window.Canvas().Capture())
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	u.status.Applications = []routing.Application{{ID: "codex-desktop", Name: "Codex 桌面版", Available: true, Kind: "desktop", ProcessPaths: []string{`C:\Apps\Codex.exe`}}, {ID: "claude-cli", Name: "Claude Code CLI", Available: true, Kind: "cli", ProcessPaths: []string{`C:\Tools\claude.exe`}}}
	u.editAppRule(-1, "Codex")
	file, err := os.Create(filepath.Join(dir, "fyne-app-dialog.png"))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(file, u.Window.Canvas().Capture())
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestLatencyControlsAndAppDraftIsolation(t *testing.T) {
	u := testWindow(t)
	u.optimal.SetChecked(true)
	if !u.autoReturn.Disabled() || !u.recovery.Disabled() || u.latencyRounds.Disabled() {
		t.Fatal("conflicting policy controls enabled")
	}
	u.latencyDifference.SetText("35")
	c, err := u.collect()
	if err != nil || !c.LatencyPolicy.Enabled || c.LatencyPolicy.MinImprovement != 35 || c.LatencyPolicy.Rounds != 3 {
		t.Fatal(c, err)
	}
	u.mode.SetSelectedIndex(1)
	if !u.optimal.Disabled() || !u.latencyRounds.Disabled() {
		t.Fatal("fixed mode must suspend optimization")
	}
	s := exampleStatus()
	s.Config.AppRules = []routing.AppRule{{ID: "codex", Name: "Codex", Enabled: true, Match: "process", ProcessPaths: []string{`C:\Apps\Codex.exe`}, OutboundID: "a"}}
	u.fill(s.Config)
	u.removeOutbound()
	if len(u.draft.Outbounds) != 2 {
		t.Fatal("deleted referenced outbound")
	}
	u.draft.AppRules[0].ProcessPaths[0] = `C:\Changed.exe`
	u.markDirty()
	u.display(s)
	if s.Config.AppRules[0].ProcessPaths[0] != `C:\Apps\Codex.exe` || u.base.AppRules[0].ProcessPaths[0] != `C:\Apps\Codex.exe` {
		t.Fatal("draft mutated saved paths")
	}
	if u.draft.AppRules[0].ProcessPaths[0] != `C:\Changed.exe` {
		t.Fatal("polling discarded unsaved application edits")
	}
}
