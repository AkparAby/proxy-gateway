//go:build desktop

package desktop

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"local/proxy-gateway/internal/routing"
)

func (u *Window) chooseAppRule(index int, preset string) {
	if u.busy || len(u.draft.Outbounds) == 0 {
		return
	}
	if index < 0 && len(u.draft.AppRules) >= 32 {
		u.SetNotice("最多配置 32 条规则")
		return
	}
	apps := slices.Clone(u.status.Applications)
	if len(apps) == 0 {
		dialog.ShowInformation("选择应用", "尚未读取到应用列表，请先点击“刷新应用列表”。", u.Window)
		return
	}
	rule := routing.AppRule{ID: fmt.Sprintf("app-%d", time.Now().UnixNano()), Enabled: true, Match: "application", OutboundID: u.draft.Outbounds[0].ID}
	if index >= 0 {
		rule = u.draft.AppRules[index]
	}
	options := []string{}
	selected := -1
	for i, app := range apps {
		label := app.Name + " · 未检测到"
		if app.Available {
			label = app.Name + " · 已安装"
			if app.Running {
				label += "（运行中）"
			}
		}
		options = append(options, label)
		if app.ID == rule.ApplicationID {
			selected = i
		}
	}
	if selected < 0 && preset != "" {
		for i, app := range apps {
			if app.Available && strings.HasPrefix(strings.ToLower(app.ID), strings.ToLower(strings.Fields(preset)[0])) {
				selected = i
				break
			}
		}
		if selected < 0 {
			for i, app := range apps {
				if strings.HasPrefix(strings.ToLower(app.ID), strings.ToLower(strings.Fields(preset)[0])) {
					selected = i
					break
				}
			}
		}
	}
	if selected < 0 {
		for i, app := range apps {
			if app.Available {
				selected = i
				break
			}
		}
	}
	if selected < 0 {
		selected = 0
	}
	message := wrap("")
	appSelect := widget.NewSelect(options, nil)
	enabled := widget.NewCheck("固定使用指定代理客户端", nil)
	enabled.SetChecked(rule.Enabled)
	outboundNames := []string{}
	outboundIndex := 0
	for i, outbound := range u.draft.Outbounds {
		outboundNames = append(outboundNames, outbound.Name)
		if outbound.ID == rule.OutboundID {
			outboundIndex = i
		}
	}
	outbound := widget.NewSelect(outboundNames, nil)
	outbound.SetSelectedIndex(outboundIndex)
	var popup *dialog.CustomDialog
	save := widget.NewButton("添加到设置", func() {
		app := apps[appSelect.SelectedIndex()]
		if enabled.Checked && !app.Available {
			message.SetText(app.Detail)
			return
		}
		for i, existing := range u.draft.AppRules {
			if i != index && existing.Match == "application" && existing.ApplicationID == app.ID {
				message.SetText("此应用已有绑定，请编辑已有规则")
				return
			}
		}
		rule.ApplicationID = app.ID
		rule.Name = app.Name
		rule.Match = "application"
		rule.Enabled = enabled.Checked
		rule.ListenPort = 0
		rule.ProcessPaths = slices.Clone(app.ProcessPaths)
		rule.OutboundID = u.draft.Outbounds[outbound.SelectedIndex()].ID
		if index < 0 {
			u.draft.AppRules = append(u.draft.AppRules, rule)
		} else {
			u.draft.AppRules[index] = rule
		}
		u.renderAppRules()
		u.markDirty()
		u.SetNotice("应用已选择。点击“保存全部设置”后自动配置接入；已打开的应用或终端可能需要重启一次。")
		popup.Hide()
	})
	update := func() {
		i := appSelect.SelectedIndex()
		if i < 0 {
			return
		}
		app := apps[i]
		message.SetText(app.Detail)
		if app.Available || !enabled.Checked {
			save.Enable()
		} else {
			save.Disable()
		}
	}
	appSelect.OnChanged = func(string) { update() }
	enabled.OnChanged = func(bool) { update() }
	appSelect.SetSelectedIndex(selected)
	form := widget.NewForm(widget.NewFormItem("应用", appSelect), widget.NewFormItem("代理客户端", outbound), widget.NewFormItem("策略", enabled))
	note := "保存后由网关自动启用所需的系统代理和终端接入。其他接入应用继续使用默认策略。接入原值会保存，退出网关时可恢复。"
	if index >= 0 && rule.Match == "port" {
		note += " 此次修改会移除旧专属入口。"
	}
	popup = dialog.NewCustom("选择应用并固定代理", "取消", container.NewVBox(form, message, wrap(note), save), u.Window)
	popup.Resize(fyne.NewSize(620, 360))
	popup.Show()
}
