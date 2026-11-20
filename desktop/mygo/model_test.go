package main

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	client "local/proxy-gateway/internal/gatewayclient"
)

func testModel(t *testing.T) (*model, *ui.Tester, chan func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := newModel(ctx, fixtureAPI{})
	m.receive(fixtureStatus(), nil)
	queue := make(chan func(), 8)
	m.dispatch = func(fn func()) { queue <- fn }
	tt := ui.NewTester(m.view, 1160, 800)
	return m, tt, queue
}
func complete(t *testing.T, queue chan func()) {
	t.Helper()
	select {
	case fn := <-queue:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("background operation did not complete")
	}
}
func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func TestEmbeddedChineseFont(t *testing.T) {
	for _, family := range ui.FontFamilies() {
		if family.Name == "Noto Sans CJK SC" && family.Registered {
			return
		}
	}
	t.Fatal("embedded font is not registered")
}

func TestSidebarAndGatewayStartStates(t *testing.T) {
	m, tt, queue := testModel(t)
	if tt.HasText("启动网关") {
		t.Fatal("running service offers start")
	}
	for _, p := range pages {
		bounds, ok := tt.Find(p.label)
		if !ok || bounds.X >= 210 {
			t.Fatalf("%s is not in left navigation: %+v", p.label, bounds)
		}
	}
	m.receive(client.Status{}, fmt.Errorf("offline"))
	tt.Frame()
	if !tt.HasText("启动网关") {
		t.Fatal("offline service cannot start")
	}
	gate := make(chan struct{})
	calls := 0
	m.ensure = func(context.Context) error { <-gate; return nil }
	start := m.ensure
	m.ensure = func(ctx context.Context) error { calls++; return start(ctx) }
	click(t, tt, "启动网关")
	if !m.starting || !tt.HasText("正在启动…") {
		t.Fatal("missing startup feedback")
	}
	m.startGateway()
	close(gate)
	complete(t, queue)
	tt.Frame()
	if calls != 1 || !m.online || m.starting || tt.HasText("启动网关") {
		t.Fatal("startup duplicated or did not recover")
	}
	m.receive(client.Status{}, fmt.Errorf("offline"))
	m.ensure = func(context.Context) error { return fmt.Errorf("cannot bind port") }
	tt.Frame()
	click(t, tt, "启动网关")
	complete(t, queue)
	tt.Frame()
	if m.starting || m.online || !tt.HasText("启动网关") || !strings.Contains(m.notice, "cannot bind") {
		t.Fatal("startup failure cannot be retried")
	}
}

func TestExternalUIUpdate(t *testing.T) {
	m, tt, _ := testModel(t)
	m.page = "apps"
	tt.Frame()
	if m.page != "apps" || !tt.HasText("选择应用") {
		t.Fatal("programmatic page change was lost", m.page)
	}
	m.openRule(-1, false)
	tt.Frame()
	if !m.ruleOpen || !tt.HasText("加入设置") {
		t.Fatal("programmatic dialog was lost")
	}
}

func TestOpenRuleSurvivesExternalConfigChange(t *testing.T) {
	m, _, _ := testModel(t)
	m.openRule(0, false)
	updated := fixtureStatus()
	updated.Config.AppRules = nil
	m.receive(updated, nil)
	if len(m.draft.AppRules) != 1 || len(m.base.AppRules) != 1 || !m.noticeError {
		t.Fatal("poll replaced the rule being edited or lost the conflict warning")
	}
	m.applyRule()
	if !m.dirty || len(m.draft.AppRules) != 1 {
		t.Fatal("dialog could not preserve its edits for conflict checking")
	}
}

func TestPolicyScrollAndDraftSurvivesRefresh(t *testing.T) {
	m, tt, _ := testModel(t)
	click(t, tt, "切换策略")
	if m.page != "policy" {
		t.Fatal("sidebar did not change page")
	}
	switchInfo, ok := tt.AccessByRole(ui.RoleSwitch, "自动切换最优网络")
	if !ok {
		t.Fatal("missing latency switch")
	}
	tt.ClickAt(switchInfo.Bounds.X+switchInfo.Bounds.W/2, switchInfo.Bounds.Y+switchInfo.Bounds.H/2)
	if m.draft.LatencyPolicy.Enabled || !m.dirty {
		t.Fatal("toggle did not update draft")
	}
	s := fixtureStatus()
	s.Health["a"] = client.Health{Checked: true, Available: false}
	m.receive(s, nil)
	tt.Frame()
	if m.draft.LatencyPolicy.Enabled || !m.dirty {
		t.Fatal("poll discarded unsaved policy")
	}
	page := m.scroll("policy")
	page.Y = 1e7
	tt.Frame()
	bottom := page.Y
	if bottom <= 0 {
		t.Fatal("policy page does not overflow")
	}
	field, ok := tt.AccessByRole(ui.RoleTextField, "直连域名后缀")
	if !ok || field.Bounds.H <= 0 {
		t.Fatal("domain editor is not visible at bottom", field)
	}
	pos := field.Bounds
	tt.Wheel(pos.X+pos.W/2, pos.Y+pos.H/2, 0, -80)
	tt.Advance(time.Second)
	if page.Y >= bottom {
		t.Fatal("wheel stuck over multiline entry at page bottom", page.Y, bottom)
	}
	click(t, tt, "概览")
	click(t, tt, "切换策略")
	if page.Y >= bottom {
		t.Fatal("page navigation lost scroll state")
	}
}

func TestApplicationPickerAndExitFlow(t *testing.T) {
	m, tt, _ := testModel(t)
	click(t, tt, "应用接入")
	click(t, tt, "选择应用")
	if !m.ruleOpen {
		t.Fatal("application picker did not open")
	}
	click(t, tt, "应用")
	click(t, tt, "Claude Code CLI · 已安装")
	click(t, tt, "加入设置")
	if m.ruleOpen || len(m.draft.AppRules) != 2 {
		t.Fatal("application was not added", m.rule.error)
	}
	r := m.draft.AppRules[1]
	if r.Match != "application" || r.ApplicationID != "claude-cli" || r.OutboundID != "a" || len(r.ProcessPaths) == 0 {
		t.Fatal("application identity was lost", r)
	}
	if len(m.base.AppRules) != 1 {
		t.Fatal("draft mutated saved configuration")
	}
	click(t, tt, "恢复接入并退出")
	if !m.exitOpen {
		t.Fatal("missing restore confirmation")
	}
	click(t, tt, "继续运行")
	if m.exitOpen {
		t.Fatal("cancel did not close confirmation")
	}
}

func TestMacCLIRuleReportsObservedConnectionsWithoutPromisingAutoAccess(t *testing.T) {
	m, _, _ := testModel(t)
	m.status.Platform = "darwin"
	delete(m.status.Integrations, "terminal")
	rule := m.draft.AppRules[0]
	rule.ApplicationID = "claude-cli"
	observed := m.status.AppStatus[rule.ID]
	observed.ActiveConnections = 0
	m.status.AppStatus[rule.ID] = observed
	if status := m.ruleStatus(rule); !strings.Contains(status, "尚未自动配置终端接入") || strings.Contains(status, "保存此规则时会自动配置") {
		t.Fatal("Mac CLI discovery must not promise automatic terminal configuration", status)
	}
	observed.ActiveConnections = 2
	m.status.AppStatus[rule.ID] = observed
	if status := m.ruleStatus(rule); !strings.Contains(status, "已命中固定代理") {
		t.Fatal("actual CLI traffic was hidden by missing terminal integration state", status)
	}
}

type saveAPI struct {
	status client.Status
	posts  int
	fail   bool
}

func (a *saveAPI) Check(context.Context, string) (uint64, error) {
	return 0, fmt.Errorf("check not configured")
}

func (a *saveAPI) Status(context.Context) (client.Status, error) {
	s := a.status
	s.Config = s.Config.Clone()
	return s, nil
}
func (a *saveAPI) Post(_ context.Context, path, _ string, body any) (string, error) {
	a.posts++
	if a.fail {
		return "", fmt.Errorf("config rejected")
	}
	if path == "/api/config" {
		a.status.Config = body.(client.Config).Clone()
	}
	return "saved", nil
}

func TestSaveConflictFailureAndSuccess(t *testing.T) {
	m, _, queue := testModel(t)
	api := &saveAPI{status: fixtureStatus()}
	m.api = api
	*m.fields["proxyPort"] = "18890"
	m.changed()
	api.status.Config.Interval = 12
	m.save()
	complete(t, queue)
	if api.posts != 0 || !m.dirty || !m.noticeError {
		t.Fatal("external configuration was overwritten")
	}
	api.status = fixtureStatus()
	api.fail = true
	m.save()
	complete(t, queue)
	if !m.dirty || m.base.ProxyPort != 17890 {
		t.Fatal("failed save discarded edits")
	}
	api.fail = false
	m.save()
	complete(t, queue)
	if m.dirty || m.base.ProxyPort != 18890 || m.status.Config.ProxyPort != 18890 {
		t.Fatal("save did not accept server configuration")
	}
}

func TestOutboundRulesAndValidation(t *testing.T) {
	m, _, _ := testModel(t)
	m.removeOutbound(0)
	if len(m.draft.Outbounds) != 2 {
		t.Fatal("deleted a bound proxy")
	}
	m.moveOutbound(0, 1)
	if m.draft.AppRules[0].OutboundID != "a" {
		t.Fatal("reorder changed stable outbound ID")
	}
	*m.fields["proxyPort"] = "7890"
	if _, err := m.collect(); err == nil {
		t.Fatal("gateway loop accepted")
	}
	*m.fields["proxyPort"] = "17890"
	*m.fields["rounds"] = "invalid"
	if _, err := m.collect(); err == nil {
		t.Fatal("invalid numeric policy accepted")
	}
}

func TestRenderedPages(t *testing.T) {
	dir := os.Getenv("GATEWAY_MYGO_SCREENSHOTS")
	if dir == "" {
		t.Skip("optional screenshots")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	_, tt, _ := testModel(t)
	for _, p := range pages {
		click(t, tt, p.label)
		f, err := os.Create(filepath.Join(dir, "test-"+p.id+".png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
