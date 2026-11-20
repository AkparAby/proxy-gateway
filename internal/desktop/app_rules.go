//go:build desktop

package desktop

import (
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"local/proxy-gateway/internal/routing"
)

func (u *Window) renderAppRules() {
	u.appRules.RemoveAll()
	u.appLabels = map[string]*widget.Label{}
	u.appButtons = nil
	button := func(name string, action func()) *widget.Button {
		b := widget.NewButton(name, action)
		u.appButtons = append(u.appButtons, b)
		return b
	}
	u.appRules.Add(widget.NewSeparator())
	u.appRules.Add(wrap("应用固定代理 · 优先于默认策略，固定出口故障时不改道"))
	u.appRules.Add(wrap("选择应用和代理客户端，网关自动识别并配置接入，无需在应用里填写代理地址或寻找程序文件。已打开的应用或终端可能需要重启一次。"))
	u.appRules.Add(container.NewHBox(button("选择应用", func() { u.editAppRule(-1, "") }), button("绑定 Codex", func() { u.editAppRule(-1, "Codex") }), button("绑定 Claude", func() { u.editAppRule(-1, "Claude") }), button("刷新应用列表", func() { u.action("/api/applications/refresh", nil, "已刷新应用列表") }), button("高级规则", func() { u.editAppRule(-1, "__advanced__") })))
	if u.status.ApplicationScanError != "" {
		u.appRules.Add(wrap(u.status.ApplicationScanError))
	}
	for index, rule := range u.draft.AppRules {
		mode := "程序路径：" + strings.Join(rule.ProcessPaths, "；")
		if rule.Match == "application" {
			mode = "自动识别与接入 · 无需单独配置应用代理"
		}
		if rule.Match == "port" {
			mode = fmt.Sprintf("专属入口：http://127.0.0.1:%d（同端口支持 SOCKS5）", rule.ListenPort)
		}
		strategy := "固定 → " + u.draft.Name(rule.OutboundID)
		if !rule.Enabled {
			strategy = "跟随默认策略（固定规则已关闭）"
		}
		status := wrap("")
		u.appLabels[rule.ID] = status
		actions := container.NewHBox(button("编辑", func() { u.editAppRule(index, "") }), button("删除规则", func() {
			u.draft.AppRules = append(u.draft.AppRules[:index], u.draft.AppRules[index+1:]...)
			u.renderAppRules()
			u.markDirty()
		}))
		if rule.Match == "port" {
			actions.Add(button("复制 HTTP 地址", func() { u.Window.Clipboard().SetContent(fmt.Sprintf("http://127.0.0.1:%d", rule.ListenPort)) }))
			actions.Add(button("复制 SOCKS5 地址", func() { u.Window.Clipboard().SetContent(fmt.Sprintf("socks5h://127.0.0.1:%d", rule.ListenPort)) }))
		}
		if rule.Match != "application" {
			actions.Add(button("改为直接选择应用", func() { u.chooseAppRule(index, rule.Name) }))
		}
		u.appRules.Add(widget.NewCard(rule.Name, strategy, container.NewVBox(wrap(mode), status, actions)))
	}
	u.updateAppStatus()
}

func (u *Window) updateAppStatus() {
	for _, rule := range u.draft.AppRules {
		label := u.appLabels[rule.ID]
		if label == nil {
			continue
		}
		saved := false
		for _, active := range u.status.Config.AppRules {
			if reflect.DeepEqual(rule, active) {
				saved = true
			}
		}
		text := "未保存：点击“保存全部设置”后生效"
		if saved {
			text = "跟随默认策略"
			if rule.Enabled {
				h := u.status.Health[rule.OutboundID]
				text = "固定出口待检测"
				if h.Checked {
					text = "固定出口不可用，不自动改道"
					if h.Available {
						text = "固定出口可用"
					}
				}
				s := u.status.AppStatus[rule.ID]
				if s.ActiveConnections > 0 {
					text += fmt.Sprintf(" · 当前命中 %d 条连接 · 实际出口：%s", s.ActiveConnections, strings.Join(s.ActualOutbounds, "、"))
				} else {
					text += " · 暂无活动连接，不代表规则未生效"
				}
				if u.status.AppStatusError != "" {
					text = u.status.AppStatusError
				}
				if rule.Match == "application" {
					identified := false
					for _, app := range u.status.Applications {
						if app.ID == rule.ApplicationID && app.Available {
							identified = true
						}
					}
					access := u.status.Integrations["system"]
					if strings.HasSuffix(rule.ApplicationID, "-cli") {
						access = u.status.Integrations["terminal"]
					} else if u.status.Platform == "windows" {
						access = access && u.status.Integrations["terminal"]
					}
					switch {
					case !identified:
						text = "尚未识别此应用，请安装或打开应用后刷新列表"
					case !access:
						text = "尚未完成接入，保存规则时网关会自动配置"
					case s.ActiveConnections > 0:
						text = fmt.Sprintf("已命中固定代理 · %d 条活动连接 · 实际出口：%s", s.ActiveConnections, strings.Join(s.ActualOutbounds, "、"))
					default:
						text = "接入已设置，等待应用的新连接；已打开的应用或终端请重启一次"
					}
					if h.Checked && !h.Available {
						text += " · 固定出口当前不可用，不自动改道"
					}
				}
			}
		}
		setLabelText(label, text)
	}
}

func (u *Window) editAppRule(index int, preset string) {
	if u.busy || len(u.draft.Outbounds) == 0 {
		return
	}
	if index < 0 && len(u.draft.AppRules) >= 32 {
		u.SetNotice("最多配置 32 个应用规则")
		return
	}
	if preset != "__advanced__" && (index < 0 || u.draft.AppRules[index].Match == "application") {
		u.chooseAppRule(index, preset)
		return
	}
	if preset == "__advanced__" {
		preset = ""
	}
	rule := routing.AppRule{ID: fmt.Sprintf("app-%d", time.Now().UnixNano()), Name: preset, Enabled: true, Match: "port", OutboundID: u.draft.Outbounds[0].ID, ListenPort: 17900}
	if index >= 0 {
		rule = u.draft.AppRules[index]
	} else {
		used := map[int]bool{u.draft.ProxyPort: true, 17880: true, 17881: true, 17882: true}
		for _, o := range u.draft.Outbounds {
			used[o.Port] = true
		}
		for _, a := range u.draft.AppRules {
			if a.Match == "port" {
				used[a.ListenPort] = true
			}
		}
		for used[rule.ListenPort] {
			rule.ListenPort++
		}
	}
	name, port := widget.NewEntry(), widget.NewEntry()
	name.SetText(rule.Name)
	port.SetText(strconv.Itoa(rule.ListenPort))
	paths := widget.NewMultiLineEntry()
	paths.SetMinRowsVisible(3)
	paths.SetText(strings.Join(rule.ProcessPaths, "\n"))
	paths.SetPlaceHolder("联网程序的完整路径，每行一个")
	enabled := widget.NewCheck("固定使用指定客户端", nil)
	enabled.SetChecked(rule.Enabled)
	options, selected := []string{}, 0
	for i, o := range u.draft.Outbounds {
		options = append(options, o.Name)
		if o.ID == rule.OutboundID {
			selected = i
		}
	}
	outbound := widget.NewSelect(options, nil)
	outbound.SetSelectedIndex(selected)
	browse := widget.NewButton("选择程序并添加路径", func() {
		dialog.ShowFileOpen(func(file fyne.URIReadCloser, err error) {
			if err != nil {
				u.SetNotice(err.Error())
				return
			}
			if file == nil {
				return
			}
			path := file.URI().Path()
			file.Close()
			if runtime.GOOS == "windows" && len(path) > 3 && path[0] == '/' && path[2] == ':' {
				path = path[1:]
			}
			path = filepath.FromSlash(path)
			if strings.TrimSpace(paths.Text) == "" {
				paths.SetText(path)
			} else {
				paths.SetText(paths.Text + "\n" + path)
			}
		}, u.Window)
	})
	match := widget.NewSelect([]string{"专属代理入口", "完整程序路径"}, func(value string) {
		port.Enable()
		paths.Disable()
		browse.Disable()
		if value == "完整程序路径" {
			port.Disable()
			paths.Enable()
			browse.Enable()
		}
	})
	if rule.Match == "process" {
		match.SetSelectedIndex(1)
	} else {
		match.SetSelectedIndex(0)
	}
	message := wrap("专属入口：保存后将地址配置到应用中。关闭固定规则后，该地址继续跟随默认策略。程序路径：仅匹配进入网关的实际联网进程，可填写多个路径。")
	form := widget.NewForm(widget.NewFormItem("应用名称", name), widget.NewFormItem("策略", enabled), widget.NewFormItem("代理客户端", outbound), widget.NewFormItem("匹配方式", match), widget.NewFormItem("专属端口", port), widget.NewFormItem("程序路径", paths))
	var popup *dialog.CustomDialog
	save := widget.NewButton("应用到草稿", func() {
		rule.Name = strings.TrimSpace(name.Text)
		if rule.Name == "" {
			message.SetText("请填写应用名称")
			return
		}
		rule.Enabled = enabled.Checked
		rule.OutboundID = u.draft.Outbounds[outbound.SelectedIndex()].ID
		rule.ProcessPaths = nil
		rule.Match = "port"
		if match.SelectedIndex() == 1 {
			rule.Match = "process"
			rule.ListenPort = 0
			for _, line := range strings.Split(paths.Text, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					rule.ProcessPaths = append(rule.ProcessPaths, line)
				}
			}
			if len(rule.ProcessPaths) == 0 {
				message.SetText("请选择或填写至少一个完整程序路径")
				return
			}
		} else {
			value, err := strconv.Atoi(strings.TrimSpace(port.Text))
			if err != nil || value < 1024 || value > 65535 {
				message.SetText("专属端口须为 1024–65535")
				return
			}
			rule.ListenPort = value
		}
		if index < 0 {
			u.draft.AppRules = append(u.draft.AppRules, rule)
		} else {
			u.draft.AppRules[index] = rule
		}
		u.renderAppRules()
		u.markDirty()
		popup.Hide()
	})
	popup = dialog.NewCustom("应用固定代理", "取消", container.NewVBox(form, browse, message, save), u.Window)
	popup.Resize(fyne.NewSize(680, 560))
	popup.Show()
}
