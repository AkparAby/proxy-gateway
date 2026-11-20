package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"local/proxy-gateway/assets"
)

var palette = func() *ui.Theme {
	t := ui.DarkTheme()
	t.Background = ui.Hex("#0b121b")
	t.Surface = ui.Hex("#192a3b")
	t.SurfaceHover = ui.Hex("#233b50")
	t.SurfacePressed = ui.Hex("#2a465c")
	t.Border = ui.Hex("#293c4d")
	t.Text = ui.Hex("#edf4fa")
	t.TextMuted = ui.Hex("#8fa6b9")
	t.Accent = ui.Hex("#46d6ad")
	t.AccentHover = ui.Hex("#6ce5c1")
	t.AccentPressed = ui.Hex("#2eb38f")
	t.AccentText = ui.Hex("#08241d")
	t.InactiveAccent = t.Accent
	t.FontSize = 14
	t.Font = "Microsoft YaHei UI, Segoe UI"
	if err := ui.RegisterFont(assets.Font, ""); err == nil {
		t.Font = "Microsoft YaHei UI, Noto Sans CJK SC"
	}
	return t
}()

var pages = []struct{ id, label, title, description string }{
	{"overview", "概览", "网络概览", "所有应用的默认出口与网络健康状态"},
	{"outbounds", "代理出口", "代理出口", "连接本机代理客户端，按顺序设置优先级"},
	{"gateway", "网关设置", "网关设置", "统一的本地入口，管理端口保持独立"},
	{"policy", "切换策略", "切换策略", "在稳定连接与更低延迟之间，选择适合你的策略"},
	{"apps", "应用接入", "应用接入", "为 Codex、Claude 等应用指定固定代理客户端"},
	{"events", "事件", "运行事件", "查看检测、出口切换和配置变更记录"},
}

func card(c *ui.Context, key, title string, content func()) {
	ui.Column(c.Key(key)).FillWidth().Padding(20).Gap(14).Radius(12).Background(ui.Hex("#12202e")).Border(1, palette.Border).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(16).Bold()
		}
		content()
	})
}
func muted(c *ui.Context, text string) { ui.Text(c, text).TextColor(palette.TextMuted).FontSize(13) }
func badge(c *ui.Context, text string, color ui.Color) {
	ui.Text(c, text).AlignSelf(ui.Start).FontSize(12).Bold().TextColor(color).Padding(5, 10).Radius(20).Background(color.Alpha(.1))
}

func (m *model) view(c *ui.Context) {
	c.SetTheme(palette)
	if m.noticeSerial != m.shownNoticeSerial {
		m.shownNoticeSerial = m.noticeSerial
		if m.notice != "" {
			kind := "success"
			if m.noticeError {
				kind = "error"
			}
			c.AddToast(ui.Toast{ID: "gateway-action", Title: m.notice, Type: kind})
		} else {
			c.CloseToast("gateway-action")
		}
	}
	ui.Row(c).Fill().Font(palette.Font).Background(palette.Background).Children(func() {
		ui.Column(c.Key("navigation")).Width(210).FillHeight().Padding(16, 12).Gap(18).Background(ui.Hex("#0e1925")).BorderWidth(0, 1, 0, 0).BorderColor(palette.Border).Children(func() {
			ui.Row(c).Padding(12, 8).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "PG").Bold().FontSize(16).Padding(9).Radius(10).Background(palette.Accent).TextColor(palette.AccentText)
				ui.Column(c).Gap(3).Children(func() { ui.Text(c, "Proxy Gateway").Bold(); muted(c, "本地代理网关") })
			})
			ui.Sidebar(c, &m.page, func() {
				ui.SidebarSection(c, "工作空间", nil, func() {
					for _, p := range pages {
						ui.SidebarItem(c, p.id, nil, p.label)
					}
				})
			}).FillWidth().Grow(1)
			ui.Column(c).Gap(8).Padding(10).Children(func() {
				status := "未连接"
				color := palette.Warning
				if m.starting {
					status = "正在启动…"
				} else if m.online {
					status = "网关运行中"
					color = palette.Success
				}
				badge(c, status, color)
				muted(c, "关闭窗口后继续在托盘运行")
			})
			ui.Button(c, "恢复接入并退出").FillWidth().Disabled(m.busy || m.starting).OnClick(func() { m.exitOpen = true })
		})
		ui.Column(c.Key("main")).Grow(1).MinWidth(0).FillHeight().Children(func() {
			m.header(c)
			ui.Scroll(c.Key(m.page)).TrackScroll(m.scroll(m.page)).Grow(1).MinHeight(0).FillWidth().Padding(24).Gap(18).Children(func() {
				if !m.loaded {
					card(c, "loading", "连接本地网关", func() {
						if m.starting {
							muted(c, "正在启动并读取配置，请稍候…")
						} else {
							muted(c, "尚未连接到本地服务，请点击右上角“启动网关”。")
						}
						if m.status.Error != "" {
							ui.Text(c, m.status.Error).TextColor(palette.Warning)
						}
					})
					return
				}
				switch m.page {
				case "outbounds":
					m.outboundsView(c)
				case "gateway":
					m.gatewayView(c)
				case "policy":
					m.policyView(c)
				case "apps":
					m.appsView(c)
				case "events":
					m.eventsView(c)
				default:
					m.overviewView(c)
				}
			})
			m.footer(c)
		})
	})
	m.ruleDialog(c)
	ui.Modal(c, &m.exitOpen, func() {
		ui.Column(c).Width(440).Font(palette.Font).Gap(18).Children(func() {
			ui.Text(c, "恢复接入并退出？").FontSize(20).Bold()
			ui.Text(c, "将恢复网关管理的系统代理和工具接入设置，停止代理转发。")
			if m.dirty {
				ui.Text(c, "尚未保存的修改将被放弃。").TextColor(palette.Warning)
			}
			ui.Row(c).Gap(10).Justify(ui.End).Children(func() {
				ui.Button(c, "继续运行").OnClick(func() { m.exitOpen = false })
				ui.PrimaryButton(c, "恢复并退出").Disabled(m.busy || m.starting).OnClick(func() { m.exitOpen = false; m.restoreAndQuit() })
			})
		})
	})
}

func (m *model) header(c *ui.Context) {
	ui.Column(c).FillWidth().Padding(22, 26).Gap(12).BorderWidth(0, 0, 1, 0).BorderColor(palette.Border).Children(func() {
		ui.Row(c).FillWidth().Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				for _, p := range pages {
					if p.id == m.page {
						ui.Text(c, p.title).Font(palette.Font).FontSize(25).Bold()
						muted(c, p.description)
						break
					}
				}
			})
			if m.online {
				text := "服务在线"
				color := palette.Success
				if m.status.Error != "" {
					text = "服务异常"
					color = palette.Warning
				}
				badge(c, text, color)
			} else if m.starting {
				ui.Button(c, "正在启动…").Disabled(true)
			} else {
				ui.PrimaryButton(c, "启动网关").Disabled(m.busy).OnClick(m.startGateway)
			}
		})
		if m.dirty || m.page != "overview" && m.page != "events" {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				if m.dirty {
					ui.Text(c, "有未保存的修改").TextColor(palette.Warning)
				}
				ui.Spacer(c)
				ui.Button(c, "撤销修改").Disabled(!m.editable() || !m.dirty).OnClick(func() { m.fill(m.status.Config); m.message("已恢复已保存的设置", false) })
				label := "保存全部设置"
				if m.busy {
					label = "正在处理…"
				}
				ui.PrimaryButton(c, label).Label("保存全部设置").Disabled(!m.editable() || !m.dirty).OnClick(m.save)
			})
		}
	})
}

func (m *model) footer(c *ui.Context) {
	ui.Row(c.Key("gateway-status")).FillWidth().Padding(16, 24).BorderWidth(1, 0, 0, 0).BorderColor(palette.Border).Background(ui.Hex("#0e1925")).Children(func() {
		label, color := "网关未连接", palette.Warning
		if m.starting {
			label = "网关正在启动…"
		} else if m.online {
			label, color = "网关运行中", palette.Success
			if m.status.Error != "" {
				label, color = "网关异常", palette.Warning
			}
			label += " · " + m.status.Config.Name(m.status.Selected) + " · " + m.status.Proxy
		}
		ui.Text(c, label).TextColor(color).FontSize(13)
	})
}

func (m *model) overviewView(c *ui.Context) {
	strategy := "优先级故障切换"
	if m.status.Config.LatencyPolicy.Enabled {
		strategy = "低延迟自动择优"
	}
	if m.status.Config.Mode != "auto" {
		strategy = "固定默认出口"
	}
	card(c, "active", "当前连接", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(18).Children(func() {
			ui.Column(c).Grow(1).Gap(8).Children(func() {
				ui.Text(c, m.status.Config.Name(m.status.Selected)).FontSize(30).Bold()
				badge(c, strategy, palette.Accent)
			})
			ui.Column(c).Gap(6).Children(func() { muted(c, "本地网关入口"); ui.Text(c, m.status.Proxy).FontSize(17) })
		})
		if m.status.Reason != "" {
			muted(c, m.status.Reason)
		}
		if m.status.Error != "" {
			ui.Text(c, m.status.Error).TextColor(palette.Warning)
		}
		ui.Row(c).Gap(10).Children(func() {
			label := "立即检查网络"
			if m.manualCheckActive() {
				label = "正在检查…"
			}
			ui.Button(c, label).Label("立即检查网络").Disabled(!m.editable() || m.manualCheckActive()).OnClick(m.checkNetwork)
			healthy := len(m.status.Config.Outbounds) > 0 && m.status.Health[m.status.Config.Outbounds[0].ID].Available
			ui.Button(c, "切回首选出口").Disabled(!m.editable() || m.status.Config.Mode != "auto" || m.status.Config.LatencyPolicy.Enabled || !healthy).OnClick(func() { m.action("/api/return", nil, "已切回首选出口") })
		})
	})
	for i, o := range m.status.Config.Outbounds {
		h := m.status.Health[o.ID]
		checking := m.outboundChecking(o.ID)
		card(c, o.ID, "", func() {
			ui.Row(c).FillWidth().Gap(14).AlignItems(ui.Center).Children(func() {
				ui.Text(c, fmt.Sprintf("%02d", i+1)).TextColor(palette.TextMuted).FontSize(20)
				ui.Column(c).Grow(1).Gap(5).Children(func() {
					ui.Text(c, o.Name).Bold().FontSize(17)
					muted(c, fmt.Sprintf("%s://%s:%d", strings.ToUpper(o.Type), o.Host, o.Port))
				})
				if o.ID == m.status.Selected {
					badge(c, "当前使用", palette.Accent)
				}
				state := "等待检测"
				color := palette.TextMuted
				if h.Checked {
					state = "不可用"
					color = palette.Warning
					if h.Available {
						state = "可用"
						color = palette.Success
						if !h.LastOK {
							state = "观察中"
							color = palette.Warning
						}
					}
				}
				badge(c, state, color)
				ui.Row(c.Key("latency")).Width(120).Height(34).Gap(8).AlignItems(ui.Center).Justify(ui.End).Children(func() {
					switch {
					case checking:
						ui.Spinner(c).Width(22).Height(22).TextColor(palette.Accent).Label(o.Name + " 正在检测延迟")
						ui.Text(c, "检测中").FontSize(13).TextColor(palette.TextMuted)
					case h.LastOK:
						ui.Text(c, fmt.Sprintf("%d ms", h.SmoothedLatency)).FontSize(24).Bold()
					case h.Checked:
						ui.Text(c, "检测失败").FontSize(13).TextColor(palette.Warning)
					default:
						ui.Text(c, "— ms").TextColor(palette.TextMuted)
					}
				})
			})
			if checking {
				muted(c, "正在检测 HTTPS 连通性与延迟…")
			} else if h.Detail != "" {
				muted(c, h.Detail)
			}
		})
	}
}

func (m *model) textField(c *ui.Context, key, label, hint string, disabled, multiline bool) {
	ui.Column(c.Key(key)).FillWidth().Gap(7).Children(func() {
		ui.Text(c, label).Bold()
		var input ui.Element
		if multiline {
			input = ui.TextArea(c, m.fields[key]).Height(94)
		} else {
			input = ui.TextInput(c, m.fields[key]).Height(38)
		}
		input.FillWidth().Label(label).Disabled(disabled).OnChange(m.changed)
		if hint != "" {
			muted(c, hint)
		}
	})
}

func (m *model) gatewayView(c *ui.Context) {
	card(c, "gateway-port", "本地代理入口", func() {
		m.textField(c, "proxyPort", "监听端口", "HTTP / HTTPS CONNECT / SOCKS5 共用一个端口。默认 17890，仅监听本机。", !m.editable(), false)
		muted(c, "保存后同步更新已启用的系统代理、终端等接入。已打开的应用或终端可能需要重新启动。")
	})
	card(c, "gateway-info", "管理服务", func() {
		ui.Text(c, "管理 API  127.0.0.1:17880")
		ui.Text(c, "内核控制  127.0.0.1:17881")
		muted(c, "监听端口不能与代理客户端或管理端口冲突。")
	})
}

func (m *model) policyView(c *ui.Context) {
	disabled := !m.editable()
	card(c, "default-mode", "默认出口", func() {
		m.outboundSelect(c, "默认出口策略", &m.draft.Mode, true, disabled, m.changed)
	})
	card(c, "latency", "自动切换最优网络", func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(16).Children(func() {
			ui.Column(c).Grow(1).Gap(6).Children(func() {
				ui.Text(c, "优先使用延迟更低的网络").Bold()
				muted(c, "仅在自动模式生效，固定应用始终使用指定客户端。")
			})
			ui.Switch(c, &m.draft.LatencyPolicy.Enabled).Label("自动切换最优网络").Disabled(disabled || m.draft.Mode != "auto").OnChange(m.changed)
		})
		for _, f := range []struct{ key, label, hint string }{{"rounds", "连续更快轮数", "连续满足条件才切换，减少频繁跳动。"}, {"improvement", "至少快（毫秒）", "使用最近 3 轮的平滑延迟比较。"}, {"cooldown", "切换冷却（秒）", "当前出口故障时跳过等待，立即尝试健康出口。"}} {
			m.textField(c, f.key, f.label, f.hint, disabled || m.draft.Mode != "auto" || !m.draft.LatencyPolicy.Enabled, false)
		}
	})
	card(c, "recovery", "优先级恢复", func() {
		ui.Checkbox(c, &m.draft.AutoReturn, "首选代理恢复稳定后自动切回").Disabled(disabled || m.draft.Mode != "auto" || m.draft.LatencyPolicy.Enabled).OnChange(m.changed)
		m.textField(c, "recovery", "恢复观察（秒）", "低延迟自动选择开启时，暂停优先级回切。", disabled || m.draft.Mode != "auto" || m.draft.LatencyPolicy.Enabled, false)
	})
	card(c, "detection", "网络检测", func() {
		m.textField(c, "interval", "检查间隔（秒）", "", disabled, false)
		m.textField(c, "timeout", "单次超时（毫秒）", "", disabled, false)
		m.textField(c, "failures", "连续失败次数", "", disabled, false)
		m.textField(c, "targets", "HTTPS 检测地址", "每行一个。任一地址成功即视为健康；延迟只对成功目标取平均。", disabled, true)
		m.textField(c, "domains", "直连域名后缀", "每行一个。切换只影响新连接，不主动断开已有连接。", disabled, true)
	})
}

func (m *model) outboundSelect(c *ui.Context, label string, id *string, automatic, disabled bool, changed func()) {
	options, ids := []string{}, []string{}
	if automatic {
		options = append(options, "自动选择出口")
		ids = append(ids, "auto")
	}
	selected := ""
	for i, o := range m.draft.Outbounds {
		options = append(options, fmt.Sprintf("%d · %s", i+1, o.Name))
		ids = append(ids, o.ID)
	}
	for i, v := range ids {
		if v == *id {
			selected = options[i]
		}
	}
	ui.Select(c, &selected, options).FillWidth().Label(label).Disabled(disabled).OnChange(func() {
		for i, option := range options {
			if option == selected {
				*id = ids[i]
				if changed != nil {
					changed()
				}
				break
			}
		}
	})
}

func (m *model) outboundsView(c *ui.Context) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		muted(c, fmt.Sprintf("%d / 16 个出口 · 第一个为首选", len(m.draft.Outbounds)))
		ui.Spacer(c)
		ui.PrimaryButton(c, "添加代理出口").Disabled(!m.editable() || len(m.draft.Outbounds) >= 16).OnClick(m.addOutbound)
	})
	for i := range m.draft.Outbounds {
		o := &m.draft.Outbounds[i]
		card(c, o.ID, fmt.Sprintf("%02d  %s", i+1, o.Name), func() {
			ui.Column(c).Gap(12).Children(func() {
				ui.TextInput(c, &o.Name).Label("名称").Placeholder("代理名称").FillWidth().Disabled(!m.editable()).OnChange(m.changed)
				protocol := strings.ToUpper(o.Type)
				ui.Select(c, &protocol, []string{"HTTP", "SOCKS5"}).Label("协议").FillWidth().Disabled(!m.editable()).OnChange(func() { o.Type = strings.ToLower(protocol); m.changed() })
				ui.Row(c).Gap(12).Children(func() {
					ui.Column(c).Grow(1).Gap(6).Children(func() {
						muted(c, "本机地址")
						ui.TextInput(c, &o.Host).Label("本机地址").FillWidth().Disabled(!m.editable()).OnChange(m.changed)
					})
					ui.Column(c).Width(170).Gap(6).Children(func() {
						muted(c, "代理端口")
						ui.TextInput(c, m.ports[o.ID]).Label("代理端口").FillWidth().Disabled(!m.editable()).OnChange(m.changed)
					})
				})
				ui.Row(c).Gap(8).Children(func() {
					ui.Button(c, "上移").Disabled(!m.editable() || i == 0).OnClick(func() { m.moveOutbound(i, -1) })
					ui.Button(c, "下移").Disabled(!m.editable() || i == len(m.draft.Outbounds)-1).OnClick(func() { m.moveOutbound(i, 1) })
					ui.Spacer(c)
					ui.Button(c, "删除出口").Disabled(!m.editable() || len(m.draft.Outbounds) <= 1).OnClick(func() { m.removeOutbound(i) })
				})
			})
		})
	}
	muted(c, "代理客户端需要保持连接；地址使用 127.0.0.1 或 ::1。修改后保存全部设置。")
}

func (m *model) eventsView(c *ui.Context) {
	if len(m.status.Events) == 0 {
		card(c, "empty-events", "暂无运行事件", func() { muted(c, "检测、切换或修改设置后，记录会显示在这里。") })
		return
	}
	for i, e := range m.status.Events {
		ui.Row(c.Key(fmt.Sprintf("event-%d", i))).FillWidth().Padding(14, 0).Gap(18).BorderWidth(0, 0, 1, 0).BorderColor(palette.Border).Children(func() {
			ui.Text(c, e.Time.Local().Format("15:04:05")).Width(75).TextColor(palette.TextMuted)
			ui.Text(c, e.Message).Grow(1)
		})
	}
}
