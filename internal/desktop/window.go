//go:build desktop

package desktop

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	desktopdriver "fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"local/proxy-gateway/assets"
	client "local/proxy-gateway/internal/gatewayclient"
	"reflect"
	"strings"
	"time"
)

type API interface {
	Status(context.Context) (client.Status, error)
	Post(context.Context, string, string, any) (string, error)
}
type disableable interface {
	Disable()
	Enable()
}
type Window struct {
	App                                                        fyne.App
	Window                                                     fyne.Window
	api                                                        API
	ctx                                                        context.Context
	status                                                     client.Status
	draft, base                                                client.Config
	loaded, dirty, busy, loading, online                       bool
	selected                                                   int
	header, current, reason, notice, saved                     *widget.Label
	health, integrations                                       *fyne.Container
	events                                                     *widget.Label
	list                                                       *widget.List
	name, host, port                                           *widget.Entry
	protocol, mode, autoReturn                                 *widget.Select
	interval, timeout, failures, recovery, targets, domains    *widget.Entry
	check, returnPrimary, save, discard, add, remove, up, down *widget.Button
	editables                                                  []disableable
	integrationButtons                                         map[string]*widget.Button
	integrationSignature                                       string
	tray                                                       *fyne.Menu
	trayStatus                                                 *fyne.MenuItem
	trayCheck                                                  *fyne.MenuItem
	Ready                                                      chan struct{}
	OnReconnect                                                func()
	RestoreOffline                                             func(context.Context) error
}

func New(app fyne.App, api API, ctx context.Context) *Window {
	u := &Window{App: app, api: api, ctx: ctx, selected: -1, Ready: make(chan struct{}), integrationButtons: map[string]*widget.Button{}}
	app.Settings().SetTheme(NewTheme())
	icon := fyne.NewStaticResource("gateway.png", assets.Icon)
	app.SetIcon(icon)
	u.Window = app.NewWindow("Proxy Gateway · 本地代理网关")
	u.Window.SetIcon(icon)
	u.Window.Resize(fyne.NewSize(1080, 760))
	u.Window.CenterOnScreen()
	u.Window.SetCloseIntercept(u.Window.Hide)
	u.header = widget.NewLabel("正在连接本地网关…")
	u.current = widget.NewLabelWithStyle("等待出口检测", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	u.current.Wrapping = fyne.TextWrapWord
	u.reason = wrap("应用统一连接 127.0.0.1:17890，网关按优先级选择健康出口。")
	u.notice = wrap("")
	u.saved = widget.NewLabel("正在读取配置")
	u.health = container.NewVBox()
	u.integrations = container.NewVBox()
	u.events = wrap("暂无事件")
	u.check = widget.NewButtonWithIcon("立即检查", theme.ViewRefreshIcon(), func() { u.action("/api/check", nil, "已开始检查所有出口") })
	u.returnPrimary = widget.NewButton("切回首选", func() { u.action("/api/return", nil, "已切回首选出口") })
	u.save = widget.NewButtonWithIcon("保存全部设置", theme.DocumentSaveIcon(), u.saveConfig)
	u.save.Importance = widget.HighImportance
	u.discard = widget.NewButton("撤销修改", func() {
		if !u.busy && u.loaded {
			u.fill(u.status.Config)
		}
	})
	u.buildEditor()
	u.buildPolicy()
	logo := canvas.NewImageFromResource(icon)
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(56, 56))
	heading := container.NewBorder(nil, nil, logo, nil, container.NewVBox(widget.NewLabelWithStyle("PROXY GATEWAY", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), u.header))
	overview := container.NewVScroll(container.NewVBox(widget.NewCard("", "", container.NewVBox(container.NewHBox(widget.NewLabel("当前出口"), u.current), u.reason, container.NewHBox(u.check, u.returnPrimary))), u.health))
	apps := container.NewVScroll(container.NewVBox(wrap("按需启用接入。每项保存原值，停用时恢复；代理客户端仍需保持连接。"), u.integrations))
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("概览", theme.HomeIcon(), overview),
		container.NewTabItemWithIcon("代理出口", theme.ComputerIcon(), u.editorContent()),
		container.NewTabItemWithIcon("切换策略", theme.SettingsIcon(), u.policyContent()),
		container.NewTabItemWithIcon("应用接入", theme.LoginIcon(), apps),
		container.NewTabItemWithIcon("事件", theme.HistoryIcon(), container.NewVScroll(u.events)),
	)
	quit := widget.NewButton("恢复接入并退出", u.ConfirmExit)
	retry := widget.NewButton("启动网关", func() {
		if u.OnReconnect != nil {
			u.OnReconnect()
		}
	})
	footer := container.NewVBox(u.notice, container.NewHBox(u.saved, layout.NewSpacer(), u.discard, u.save), widget.NewSeparator(), container.NewHBox(widget.NewLabel("关闭窗口后继续在托盘运行"), layout.NewSpacer(), retry, quit))
	u.Window.SetContent(container.NewBorder(container.NewVBox(heading, widget.NewSeparator()), footer, nil, nil, tabs))
	u.setupMenus(icon)
	u.controls()
	return u
}
func wrap(text string) *widget.Label {
	l := widget.NewLabel(text)
	l.Wrapping = fyne.TextWrapWord
	return l
}
func (u *Window) Show()                 { u.Window.Show(); u.Window.RequestFocus() }
func (u *Window) SetNotice(text string) { u.notice.SetText(text) }
func (u *Window) IsReady() bool         { return u.loaded && u.online }
func (u *Window) Start() {
	go func() {
		for {
			ctx, cancel := context.WithTimeout(u.ctx, 3*time.Second)
			s, err := u.api.Status(ctx)
			cancel()
			fyne.Do(func() {
				if err != nil {
					u.offline(err)
				} else {
					u.display(s)
				}
			})
			select {
			case <-u.ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
}
func (u *Window) offline(err error) {
	u.online = false
	u.header.SetText("网关未连接 · 可点击“启动网关”重试")
	u.reason.SetText(err.Error())
	u.controls()
	u.updateTray("网关未连接")
}
func (u *Window) display(s client.Status) {
	healthChanged := !reflect.DeepEqual(u.status.Health, s.Health) || !reflect.DeepEqual(u.status.Config.Outbounds, s.Config.Outbounds) || u.status.Selected != s.Selected
	u.status = s
	u.online = true
	u.header.SetText("网关运行中 · " + s.Version + " · HTTP / SOCKS5 127.0.0.1:17890")
	u.current.SetText(s.Config.Name(s.Selected))
	reason := s.Reason
	if s.Error != "" {
		reason = s.Error
	}
	u.reason.SetText(reason)
	if !u.loaded {
		u.loaded = true
		u.fill(s.Config)
		close(u.Ready)
	} else if !u.dirty && !u.busy && !reflect.DeepEqual(u.base, s.Config) {
		u.fill(s.Config)
	}
	if u.dirty && !reflect.DeepEqual(u.base, s.Config) {
		u.saved.SetText("配置已在其他窗口更新，请撤销修改后重新编辑")
	}
	if healthChanged {
		u.health.RemoveAll()
		for i, o := range s.Config.Outbounds {
			h := s.Health[o.ID]
			state := "待检测"
			if h.Checked {
				state = "不可用"
				if h.Available {
					state = "可用"
					if !h.LastOK {
						state = "观察中"
					}
				}
			}
			active := ""
			if o.ID == s.Selected {
				active = " · 当前使用"
			}
			detail := fmt.Sprintf("%s://%s:%d · %s · %d ms · 连续失败 %d 次", o.Type, o.Host, o.Port, state, h.Latency, h.Failures)
			name := wrap(fmt.Sprintf("%d. %s%s", i+1, o.Name, active))
			name.TextStyle = fyne.TextStyle{Bold: true}
			u.health.Add(widget.NewCard("", "", container.NewVBox(name, wrap(detail), wrap(h.Detail))))
		}
	}
	u.renderIntegrations(s)
	var events strings.Builder
	for _, event := range s.Events {
		fmt.Fprintf(&events, "%s  %s\n\n", event.Time.Local().Format("15:04:05"), event.Message)
	}
	u.events.SetText(events.String())
	u.updateTray("当前：" + s.Config.Name(s.Selected))
	u.controls()
}
func (u *Window) controls() {
	enabled := u.online && u.loaded && !u.busy
	set := func(x disableable, yes bool) {
		if yes {
			x.Enable()
		} else {
			x.Disable()
		}
	}
	for _, x := range u.editables {
		set(x, enabled)
	}
	for _, b := range []*widget.Button{u.check, u.save, u.discard, u.add} {
		set(b, enabled)
	}
	set(u.save, enabled && u.dirty)
	set(u.discard, enabled && u.dirty)
	set(u.add, enabled && len(u.draft.Outbounds) < 16)
	set(u.remove, enabled && len(u.draft.Outbounds) > 1 && u.selected >= 0)
	set(u.up, enabled && u.selected > 0)
	set(u.down, enabled && u.selected >= 0 && u.selected < len(u.draft.Outbounds)-1)
	primaryHealthy := len(u.status.Config.Outbounds) > 0 && u.status.Health[u.status.Config.Outbounds[0].ID].Available
	set(u.returnPrimary, enabled && u.status.Config.Mode == "auto" && primaryHealthy)
	for _, button := range u.integrationButtons {
		set(button, enabled)
	}
	if u.trayCheck != nil && u.trayCheck.Disabled != !enabled {
		u.trayCheck.Disabled = !enabled
		u.tray.Refresh()
	}
}
func (u *Window) perform(work func(context.Context) (string, error), done func()) {
	if u.busy {
		return
	}
	u.busy = true
	u.controls()
	go func() {
		ctx, cancel := context.WithTimeout(u.ctx, 150*time.Second)
		message, err := work(ctx)
		cancel()
		fyne.Do(func() {
			u.busy = false
			if err != nil {
				u.SetNotice(err.Error())
				u.Show()
				dialog.ShowError(err, u.Window)
			} else {
				u.SetNotice(message)
				if done != nil {
					done()
				}
			}
			u.controls()
		})
	}()
}
func (u *Window) action(path string, body any, message string) {
	token := u.status.Token
	u.perform(func(ctx context.Context) (string, error) {
		result, err := u.api.Post(ctx, path, token, body)
		if result != "" {
			message = result
		}
		return message, err
	}, nil)
}
func (u *Window) ConfirmExit() {
	if u.busy {
		return
	}
	u.Show()
	dialog.ShowConfirm("恢复接入并退出", "将恢复由网关管理的接入设置（包括登录自启动）并停止代理转发。", func(yes bool) {
		if !yes {
			return
		}
		token := u.status.Token
		if !u.online {
			if u.RestoreOffline != nil {
				u.perform(func(ctx context.Context) (string, error) { return "接入已恢复", u.RestoreOffline(ctx) }, u.App.Quit)
			} else {
				u.SetNotice("请使用包内恢复工具恢复接入后退出")
			}
			return
		}
		u.perform(func(ctx context.Context) (string, error) { return u.api.Post(ctx, "/api/stop", token, nil) }, u.App.Quit)
	}, u.Window)
}
func (u *Window) setupMenus(icon fyne.Resource) {
	open := fyne.NewMenuItem("打开管理窗口", u.Show)
	u.trayStatus = fyne.NewMenuItem("正在连接网关", nil)
	u.trayStatus.Disabled = true
	u.trayCheck = fyne.NewMenuItem("立即检查", func() { u.action("/api/check", nil, "已开始检查") })
	quit := fyne.NewMenuItem("恢复接入并退出", u.ConfirmExit)
	quit.IsQuit = true
	u.tray = fyne.NewMenu("Proxy Gateway", open, u.trayStatus, u.trayCheck, fyne.NewMenuItemSeparator(), quit)
	u.Window.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu("网关", fyne.NewMenuItem("显示窗口", u.Show), fyne.NewMenuItem("隐藏到托盘", u.Window.Hide), quit)))
	if desk, ok := u.App.(desktopdriver.App); ok {
		desk.SetSystemTrayMenu(u.tray)
		desk.SetSystemTrayIcon(icon)
	}
}
func (u *Window) updateTray(text string) {
	if u.trayStatus.Label != text {
		u.trayStatus.Label = text
		u.tray.Refresh()
	}
}
func (u *Window) renderIntegrations(s client.Status) {
	signature := fmt.Sprint(s.IntegrationOptions)
	if signature != u.integrationSignature {
		u.integrationSignature = signature
		u.integrations.RemoveAll()
		u.integrationButtons = map[string]*widget.Button{}
		for _, item := range s.IntegrationOptions {
			key := item[0]
			button := widget.NewButton("启用", func() {
				u.action("/api/integration", map[string]any{"name": key, "enabled": !u.status.Integrations[key]}, "接入设置已更新")
			})
			u.integrationButtons[key] = button
			u.integrations.Add(widget.NewCard(item[1], "", container.NewBorder(nil, nil, nil, button, wrap(item[2]))))
		}
	}
	for key, button := range u.integrationButtons {
		text := "启用"
		if s.Integrations[key] {
			text = "停用并恢复"
		}
		button.SetText(text)
	}
}
