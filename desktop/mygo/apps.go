package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	client "local/proxy-gateway/internal/gatewayclient"
	"local/proxy-gateway/internal/routing"
)

func (m *model) appsView(c *ui.Context) {
	ui.Row(c).FillWidth().Gap(10).Wrap().Children(func() {
		ui.PrimaryButton(c, "选择应用").Disabled(!m.editable() || len(m.draft.AppRules) >= 32).OnClick(func() { m.openRule(-1, false) })
		ui.Button(c, "刷新应用列表").Disabled(!m.editable()).OnClick(func() { m.action("/api/applications/refresh", nil, "应用识别结果已刷新") })
		ui.Button(c, "高级规则").Disabled(!m.editable() || len(m.draft.AppRules) >= 32).OnClick(func() { m.openRule(-1, true) })
	})
	muted(c, "选择应用和代理客户端后保存，网关会自动配置接入。应用固定规则优先于默认策略。")
	if m.status.ApplicationScanError != "" {
		ui.Text(c, m.status.ApplicationScanError).TextColor(palette.Warning)
	}
	if len(m.draft.AppRules) == 0 {
		card(c, "empty-apps", "为应用固定代理", func() {
			muted(c, "从本机识别的 Codex、Claude 桌面版或 CLI 中选择，无需填写 HTTP 地址或查找程序路径。")
		})
	}
	for i := range m.draft.AppRules {
		r := &m.draft.AppRules[i]
		card(c, r.ID, "", func() {
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(6).Children(func() {
					ui.Text(c, r.Name).FontSize(19).Bold()
					muted(c, "固定使用 · "+m.draft.Name(r.OutboundID))
				})
				label := "跟随默认"
				color := palette.TextMuted
				if r.Enabled {
					label = "固定代理"
					color = palette.Accent
				}
				badge(c, label, color)
			})
			ui.Text(c, m.ruleStatus(*r)).FontSize(13).TextColor(palette.TextMuted)
			ui.Row(c).Gap(8).Wrap().Children(func() {
				ui.Button(c, "编辑规则").Disabled(!m.editable()).OnClick(func() { m.openRule(i, r.Match != "application") })
				ui.Button(c, "删除规则").Disabled(!m.editable()).OnClick(func() { m.draft.AppRules = slices.Delete(m.draft.AppRules, i, i+1); m.changed() })
				if r.Match == "port" {
					ui.Button(c, "改为直接选择应用").Disabled(!m.editable()).OnClick(func() { m.openRule(i, false) })
					if ui.Button(c, "复制 HTTP 地址").Clicked() {
						c.WriteClipboard(fmt.Sprintf("http://127.0.0.1:%d", r.ListenPort))
						m.message("HTTP 地址已复制", false)
					}
					if ui.Button(c, "复制 SOCKS5 地址").Clicked() {
						c.WriteClipboard(fmt.Sprintf("socks5://127.0.0.1:%d", r.ListenPort))
						m.message("SOCKS5 地址已复制", false)
					}
				}
			})
		})
	}
	ui.Text(c, "系统与工具接入").FontSize(19).Bold().Margin(8, 0, 0, 0)
	muted(c, "启用时保存原值，停用时恢复。固定应用以外的接入流量继续使用默认策略。")
	for _, option := range m.status.IntegrationOptions {
		key := option[0]
		enabled := m.status.Integrations[key]
		card(c, "integration-"+key, "", func() {
			ui.Row(c).FillWidth().Gap(18).AlignItems(ui.Center).Children(func() {
				ui.Column(c).Grow(1).Gap(6).Children(func() { ui.Text(c, option[1]).Bold(); muted(c, option[2]) })
				label := "启用"
				if enabled {
					label = "停用并恢复"
				}
				ui.Button(c, label).Label(option[1] + label).Disabled(!m.editable()).OnClick(func() {
					m.action("/api/integration", map[string]any{"name": key, "enabled": !enabled}, "接入设置已更新")
				})
			})
		})
	}
}

func (m *model) ruleStatus(r routing.AppRule) string {
	if !r.Enabled {
		return "已停用固定规则，使用默认出口策略。"
	}
	if m.dirty {
		return "修改尚未保存，保存后生效。"
	}
	if !m.online {
		return "网关未连接，无法读取当前接入状态。"
	}
	if m.status.AppStatusError != "" {
		return m.status.AppStatusError
	}
	if h := m.status.Health[r.OutboundID]; h.Checked && !h.Available {
		return "固定出口当前不可用，不自动改道。"
	}
	if r.Match == "application" {
		found := false
		for _, app := range m.status.Applications {
			if app.ID == r.ApplicationID && app.Available {
				found = true
			}
		}
		if !found {
			return "尚未识别此应用，请安装或打开应用后刷新列表。"
		}
		access := m.status.Integrations["system"]
		if strings.HasSuffix(r.ApplicationID, "-cli") {
			access = m.status.Integrations["terminal"]
		} else if m.status.Platform == "windows" {
			access = access && m.status.Integrations["terminal"]
		}
		if m.status.Platform == "darwin" && strings.HasSuffix(r.ApplicationID, "-cli") {
			if m.status.AppStatus[r.ID].ActiveConnections == 0 {
				return "已识别 CLI；Mac 版尚未自动配置终端接入，流量进入网关后才会命中固定规则。"
			}
		} else if !access {
			return "接入尚未完成，保存此规则时会自动配置。"
		}
	}
	if s := m.status.AppStatus[r.ID]; s.ActiveConnections > 0 {
		return fmt.Sprintf("已命中固定代理 · %d 条活动连接 · 实际出口：%s", s.ActiveConnections, strings.Join(s.ActualOutbounds, "、"))
	}
	if r.Match == "application" {
		return "接入已设置，等待新连接；已打开的应用或终端请重启一次。"
	}
	if r.Match == "port" {
		return fmt.Sprintf("专属入口 http://127.0.0.1:%d · 暂无活动连接。", r.ListenPort)
	}
	return "按完整联网程序路径匹配进入网关的连接；暂无活动连接不代表规则失效。"
}

func (m *model) openRule(index int, advanced bool) {
	if !m.editable() || len(m.draft.Outbounds) == 0 || index < 0 && len(m.draft.AppRules) >= 32 {
		return
	}
	r := routing.AppRule{ID: newID(), Enabled: true, Match: "application", OutboundID: m.draft.Outbounds[0].ID, ListenPort: 17900}
	if index >= 0 {
		r = m.draft.AppRules[index]
		r.ProcessPaths = slices.Clone(r.ProcessPaths)
	}
	method := "自动识别应用"
	if advanced {
		method = "专属代理入口"
		if r.Match == "process" {
			method = "完整程序路径"
		}
	}
	if index < 0 {
		used := map[int]bool{m.draft.ProxyPort: true, 17880: true, 17881: true, 17882: true}
		for _, p := range m.ports {
			if n, err := strconv.Atoi(*p); err == nil {
				used[n] = true
			}
		}
		for _, existing := range m.draft.AppRules {
			if existing.Match == "port" {
				used[existing.ListenPort] = true
			}
		}
		for used[r.ListenPort] {
			r.ListenPort++
		}
	}
	application := r.ApplicationID
	if application == "" {
		words := strings.Fields(r.Name)
		for _, a := range m.status.Applications {
			if a.Available && (len(words) == 0 || strings.HasPrefix(strings.ToLower(a.Name), strings.ToLower(words[0]))) {
				application = a.ID
				break
			}
		}
		if application == "" && len(m.status.Applications) > 0 {
			application = m.status.Applications[0].ID
		}
	}
	m.rule = ruleDraft{value: r, index: index, method: method, application: application, outbound: r.OutboundID, paths: strings.Join(r.ProcessPaths, "\n"), port: strconv.Itoa(r.ListenPort)}
	m.ruleOpen = true
}

func (m *model) applyRule() {
	r := m.rule.value
	r.OutboundID = m.rule.outbound
	r.ProcessPaths = nil
	r.ListenPort = 0
	r.ApplicationID = ""
	fail := func(s string) { m.rule.error = s }
	switch m.rule.method {
	case "自动识别应用":
		var app routing.Application
		for _, candidate := range m.status.Applications {
			if candidate.ID == m.rule.application {
				app = candidate
				break
			}
		}
		if app.ID == "" {
			fail("请先刷新并选择应用")
			return
		}
		if r.Enabled && !app.Available {
			fail("尚未检测到此应用，请安装或打开后刷新列表。")
			return
		}
		for i, existing := range m.draft.AppRules {
			if i != m.rule.index && existing.Match == "application" && existing.ApplicationID == app.ID {
				fail("此应用已经有固定代理规则，请编辑已有规则。")
				return
			}
		}
		r.Match = "application"
		r.ApplicationID = app.ID
		r.Name = app.Name
		r.ProcessPaths = slices.Clone(app.ProcessPaths)
	case "完整程序路径":
		r.Match = "process"
		r.ProcessPaths = lines(m.rule.paths)
		if len(r.ProcessPaths) == 0 {
			fail("请填写至少一个完整联网程序路径。")
			return
		}
	default:
		r.Match = "port"
		port, err := strconv.Atoi(strings.TrimSpace(m.rule.port))
		if err != nil || port < 1024 || port > 65535 {
			fail("专属端口须为 1024–65535。")
			return
		}
		r.ListenPort = port
	}
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		fail("请填写应用名称。")
		return
	}
	if !slices.ContainsFunc(m.draft.Outbounds, func(o client.Outbound) bool { return o.ID == r.OutboundID }) {
		fail("请选择有效的代理客户端。")
		return
	}
	if m.rule.index < 0 {
		m.draft.AppRules = append(m.draft.AppRules, r)
	} else {
		m.draft.AppRules[m.rule.index] = r
	}
	m.ruleOpen = false
	m.changed()
	m.message("规则已加入设置。点击“保存全部设置”后生效。", false)
}

func (m *model) ruleDialog(c *ui.Context) {
	ui.Modal(c, &m.ruleOpen, func() {
		ui.Column(c).Width(560).MaxWidthPercent(90).Font(palette.Font).Gap(16).Children(func() {
			ui.Text(c, "应用固定代理").FontSize(22).Bold()
			if m.rule.method == "自动识别应用" {
				muted(c, "选择应用，然后指定它使用的代理客户端。")
				options, ids := []string{}, []string{}
				selected := ""
				for _, a := range m.status.Applications {
					label := a.Name + " · 未检测到"
					if a.Available {
						label = a.Name + " · 已安装"
						if a.Running {
							label += "（运行中）"
						}
					}
					options = append(options, label)
					ids = append(ids, a.ID)
					if a.ID == m.rule.application {
						selected = label
					}
				}
				ui.Select(c, &selected, options).FillWidth().Label("应用").OnChange(func() {
					for i, label := range options {
						if label == selected {
							m.rule.application = ids[i]
							m.rule.error = ""
							break
						}
					}
				})
				for _, a := range m.status.Applications {
					if a.ID == m.rule.application && a.Detail != "" {
						muted(c, a.Detail)
					}
				}
				if len(options) == 0 {
					ui.Text(c, "尚未取得应用列表。请关闭此窗口并刷新应用列表。").TextColor(palette.Warning)
				}
			} else {
				ui.TextInput(c, &m.rule.value.Name).Label("应用名称").Placeholder("应用名称").FillWidth()
				ui.Select(c, &m.rule.method, []string{"专属代理入口", "完整程序路径"}).Label("匹配方式").FillWidth()
				if m.rule.method == "完整程序路径" {
					ui.TextArea(c, &m.rule.paths).Label("完整程序路径").Placeholder("完整联网程序路径，每行一个").Height(110).FillWidth()
				} else {
					ui.TextInput(c, &m.rule.port).Label("专属端口").FillWidth()
				}
				muted(c, "高级规则只处理进入网关的连接。专属入口需在应用中配置，程序路径需填写实际联网进程。")
			}
			ui.Text(c, "代理客户端").Bold()
			m.outboundSelect(c, "固定代理客户端", &m.rule.outbound, false, false, nil)
			ui.Checkbox(c, &m.rule.value.Enabled, "固定使用指定代理客户端")
			muted(c, "固定出口故障时不自动改道。自动识别规则保存后会配置所需接入，已打开的应用可能需要重启一次。")
			if m.rule.error != "" {
				ui.Text(c, m.rule.error).TextColor(palette.Warning)
			}
			ui.Row(c).Justify(ui.End).Gap(10).Children(func() {
				ui.Button(c, "取消").OnClick(func() { m.ruleOpen = false })
				ui.PrimaryButton(c, "加入设置").Disabled(!m.editable()).OnClick(m.applyRule)
			})
		})
	})
}
