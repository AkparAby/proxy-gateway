//go:build desktop

package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	client "local/proxy-gateway/internal/gatewayclient"
	"local/proxy-gateway/internal/routing"
	"reflect"
	"strconv"
	"strings"
)

func (u *Window) markDirty() {
	if u.loading {
		return
	}
	u.dirty = true
	setLabelText(u.saved, "有未保存的修改")
	u.controls()
}
func (u *Window) buildEditor() {
	u.list = widget.NewList(func() int { return len(u.draft.Outbounds) }, func() fyne.CanvasObject { return widget.NewLabel("代理出口") }, func(id widget.ListItemID, obj fyne.CanvasObject) {
		o := u.draft.Outbounds[id]
		obj.(*widget.Label).SetText(fmt.Sprintf("%d. %s", id+1, o.Name))
	})
	u.list.OnSelected = func(id widget.ListItemID) { u.selected = id; u.loadSelected(); u.controls() }
	entry := func() *formEntry { e := newFormEntry(false); u.editables = append(u.editables, e); return e }
	u.name, u.host, u.port = entry(), entry(), entry()
	u.protocol = widget.NewSelect([]string{"HTTP", "SOCKS5"}, func(value string) { u.editSelected(func(o *client.Outbound) { o.Type = strings.ToLower(value) }) })
	u.editables = append(u.editables, u.protocol)
	u.name.OnChanged = func(text string) {
		u.editSelected(func(o *client.Outbound) { o.Name = text })
		u.list.Refresh()
		u.syncModes()
	}
	u.host.OnChanged = func(text string) { u.editSelected(func(o *client.Outbound) { o.Host = text }) }
	u.port.OnChanged = func(text string) { u.editSelected(func(o *client.Outbound) { o.Port, _ = strconv.Atoi(text) }) }
	u.add = widget.NewButtonWithIcon("添加", theme.ContentAddIcon(), func() {
		if u.busy || len(u.draft.Outbounds) >= 16 {
			return
		}
		u.addOutbound()
	})
	u.remove = widget.NewButtonWithIcon("删除", theme.DeleteIcon(), func() {
		if u.busy || len(u.draft.Outbounds) <= 1 || u.selected < 0 {
			return
		}
		u.removeOutbound()
	})
	u.up = widget.NewButton("上移", func() { u.moveOutbound(-1) })
	u.down = widget.NewButton("下移", func() { u.moveOutbound(1) })
}
func (u *Window) editorContent() fyne.CanvasObject {
	left := container.NewBorder(nil, container.NewVBox(container.NewGridWithColumns(2, u.add, u.remove), container.NewGridWithColumns(2, u.up, u.down)), nil, nil, u.list)
	form := widget.NewForm(widget.NewFormItem("名称", u.name), widget.NewFormItem("协议", u.protocol), widget.NewFormItem("回环地址", u.host), widget.NewFormItem("端口", u.port))
	right := newFormScroll(container.NewVBox(widget.NewCard("编辑代理出口", "", form), wrap("列表从上到下排列优先级，第一个为首选。支持 1–16 个本机 HTTP / SOCKS5 出口。"), wrap("地址使用 127.0.0.1 或 ::1。客户端须已连接，端口不可指回网关。修改后点击下方“保存全部设置”。")), u.name, u.host, u.port)
	split := container.NewHSplit(left, right)
	split.Offset = 0.32
	return split
}
func (u *Window) editSelected(change func(*client.Outbound)) {
	if u.loading || u.selected < 0 || u.selected >= len(u.draft.Outbounds) {
		return
	}
	change(&u.draft.Outbounds[u.selected])
	u.markDirty()
}
func (u *Window) loadSelected() {
	if u.selected < 0 || u.selected >= len(u.draft.Outbounds) {
		return
	}
	u.loading = true
	defer func() { u.loading = false }()
	o := u.draft.Outbounds[u.selected]
	u.name.SetText(o.Name)
	u.host.SetText(o.Host)
	u.port.SetText(strconv.Itoa(o.Port))
	u.protocol.SetSelected(strings.ToUpper(o.Type))
}
func (u *Window) addOutbound() {
	used := map[int]bool{17880: true, 17881: true, 17882: true, u.draft.ProxyPort: true}
	for _, o := range u.draft.Outbounds {
		used[o.Port] = true
	}
	for _, a := range u.draft.AppRules {
		if a.Match == "port" {
			used[a.ListenPort] = true
		}
	}
	port := 7890
	for used[port] {
		port++
	}
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		u.SetNotice(err.Error())
		return
	}
	name := ""
	for number := 1; ; number++ {
		candidate := fmt.Sprintf("代理 %d", number)
		exists := false
		for _, o := range u.draft.Outbounds {
			if o.Name == candidate {
				exists = true
			}
		}
		if !exists {
			name = candidate
			break
		}
	}
	u.draft.Outbounds = append(u.draft.Outbounds, client.Outbound{ID: hex.EncodeToString(b), Name: name, Type: "http", Host: "127.0.0.1", Port: port})
	u.list.Refresh()
	u.list.Select(len(u.draft.Outbounds) - 1)
	u.syncModes()
	u.markDirty()
}
func (u *Window) removeOutbound() {
	id := u.draft.Outbounds[u.selected].ID
	for _, a := range u.draft.AppRules {
		if a.OutboundID == id {
			u.SetNotice("请先修改或删除应用 “" + a.Name + "” 的出口绑定")
			return
		}
	}
	u.draft.Outbounds = append(u.draft.Outbounds[:u.selected], u.draft.Outbounds[u.selected+1:]...)
	if u.draft.Mode == id {
		u.draft.Mode = "auto"
		u.SetNotice("已删除固定出口，保存后改用自动模式")
	}
	if u.selected >= len(u.draft.Outbounds) {
		u.selected = len(u.draft.Outbounds) - 1
	}
	u.list.Refresh()
	u.list.Select(u.selected)
	u.loadSelected()
	u.syncModes()
	u.markDirty()
}
func (u *Window) moveOutbound(delta int) {
	next := u.selected + delta
	if u.busy || u.selected < 0 || next < 0 || next >= len(u.draft.Outbounds) {
		return
	}
	u.draft.Outbounds[next], u.draft.Outbounds[u.selected] = u.draft.Outbounds[u.selected], u.draft.Outbounds[next]
	u.list.Refresh()
	u.list.Select(next)
	u.loadSelected()
	u.syncModes()
	u.markDirty()
}
func (u *Window) buildPolicy() {
	u.mode = widget.NewSelect([]string{"自动选择出口"}, func(_ string) {
		if u.loading {
			return
		}
		i := u.mode.SelectedIndex()
		u.draft.Mode = "auto"
		if i > 0 && i <= len(u.draft.Outbounds) {
			u.draft.Mode = u.draft.Outbounds[i-1].ID
		}
		u.markDirty()
	})
	u.autoReturn = widget.NewSelect([]string{"稳定后自动切回", "保持当前出口，手动切回"}, func(_ string) { u.markDirty() })
	entry := func(multiline bool) *formEntry {
		e := newFormEntry(multiline)
		e.OnChanged = func(string) { u.markDirty() }
		u.editables = append(u.editables, e)
		return e
	}
	u.interval, u.timeout, u.failures, u.recovery = entry(false), entry(false), entry(false), entry(false)
	u.targets, u.domains = entry(true), entry(true)
	u.latencyRounds, u.latencyDifference, u.latencyCooldown = entry(false), entry(false), entry(false)
	u.optimal = widget.NewCheck("自动切换最优网络（低延迟）", func(bool) { u.markDirty() })
	u.editables = append(u.editables, u.optimal)
	u.editables = append(u.editables, u.mode, u.autoReturn)
}
func (u *Window) policyContent() fyne.CanvasObject {
	form := widget.NewForm(widget.NewFormItem("运行模式", u.mode), widget.NewFormItem("恢复行为", u.autoReturn), widget.NewFormItem("恢复观察（秒）", u.recovery), widget.NewFormItem("检查间隔（秒）", u.interval), widget.NewFormItem("单次超时（毫秒）", u.timeout), widget.NewFormItem("连续失败次数", u.failures), widget.NewFormItem("HTTPS 检测地址", u.targets), widget.NewFormItem("直连域名后缀", u.domains))
	latency := widget.NewForm(widget.NewFormItem("连续更快轮数", u.latencyRounds), widget.NewFormItem("至少快（毫秒）", u.latencyDifference), widget.NewFormItem("切换冷却（秒）", u.latencyCooldown))
	return newFormScroll(container.NewVBox(u.optimal, wrap("开启后按最近 3 轮的平滑延迟择优，暂停优先级恢复回切。当前出口故障时跳过防抖等待。应用固定规则始终优先。"), latency, form, wrap("检测地址、直连域名每行一个。任一检测地址成功即视为健康；延迟只对成功目标取平均。只切换新连接，不主动断开已有连接。")), u.latencyRounds, u.latencyDifference, u.latencyCooldown, u.recovery, u.interval, u.timeout, u.failures, u.targets, u.domains)
}
func (u *Window) syncModes() {
	if u.mode == nil {
		return
	}
	was := u.loading
	u.loading = true
	defer func() { u.loading = was }()
	options := []string{"自动选择出口"}
	selected := 0
	for i, o := range u.draft.Outbounds {
		options = append(options, fmt.Sprintf("固定 %d：%s", i+1, o.Name))
		if u.draft.Mode == o.ID {
			selected = i + 1
		}
	}
	u.mode.Options = options
	u.mode.SetSelectedIndex(selected)
	u.mode.Refresh()
	if u.appRules != nil {
		u.renderAppRules()
	}
}
func (u *Window) fill(c client.Config) {
	u.loading = true
	u.draft, u.base = c.Clone(), c.Clone()
	u.selected = 0
	u.proxyPort.SetText(strconv.Itoa(c.ProxyPort))
	u.interval.SetText(strconv.Itoa(c.Interval))
	u.timeout.SetText(strconv.Itoa(c.Timeout))
	u.failures.SetText(strconv.Itoa(c.Failures))
	u.recovery.SetText(strconv.Itoa(c.Recovery))
	p := c.LatencyPolicy
	if p.Rounds == 0 {
		p = routing.DefaultLatencyPolicy()
	}
	u.optimal.SetChecked(p.Enabled)
	u.latencyRounds.SetText(strconv.Itoa(p.Rounds))
	u.latencyDifference.SetText(strconv.Itoa(p.MinImprovement))
	u.latencyCooldown.SetText(strconv.Itoa(p.Cooldown))
	u.targets.SetText(strings.Join(c.Targets, "\n"))
	u.domains.SetText(strings.Join(c.DirectDomains, "\n"))
	i := 1
	if c.AutoReturn {
		i = 0
	}
	u.autoReturn.SetSelectedIndex(i)
	u.syncModes()
	u.list.Refresh()
	u.list.Select(0)
	u.loadSelected()
	u.loading = false
	u.dirty = false
	u.saved.SetText("配置已保存")
	u.controls()
}
func (u *Window) collect() (client.Config, error) {
	c := u.draft.Clone()
	for _, pair := range []struct {
		entry *formEntry
		value *int
		name  string
	}{{u.latencyRounds, &c.LatencyPolicy.Rounds, "择优轮数"}, {u.latencyDifference, &c.LatencyPolicy.MinImprovement, "延迟差"}, {u.latencyCooldown, &c.LatencyPolicy.Cooldown, "冷却时间"}, {u.proxyPort, &c.ProxyPort, "网关端口"}, {u.interval, &c.Interval, "检查间隔"}, {u.timeout, &c.Timeout, "超时"}, {u.failures, &c.Failures, "失败次数"}, {u.recovery, &c.Recovery, "观察时间"}} {
		value, err := strconv.Atoi(strings.TrimSpace(pair.entry.Text))
		if err != nil {
			return c, fmt.Errorf("%s必须为整数", pair.name)
		}
		*pair.value = value
	}
	if c.ProxyPort < 1024 || c.ProxyPort > 65535 || c.ProxyPort == 17880 || c.ProxyPort == 17881 || c.ProxyPort == 17882 {
		return c, fmt.Errorf("网关端口须为 1024–65535，且不能占用管理端口 17880、17881、17882")
	}
	for _, o := range c.Outbounds {
		if o.Port == c.ProxyPort {
			return c, fmt.Errorf("网关端口不能与代理出口 %s 的端口相同", o.Name)
		}
	}
	c.AutoReturn = u.autoReturn.SelectedIndex() == 0
	c.LatencyPolicy.Enabled = u.optimal.Checked
	lines := func(text string) []string {
		result := []string{}
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				result = append(result, line)
			}
		}
		return result
	}
	c.Targets = lines(u.targets.Text)
	c.DirectDomains = lines(u.domains.Text)
	for i := range c.Outbounds {
		c.Outbounds[i].Name = strings.TrimSpace(c.Outbounds[i].Name)
		c.Outbounds[i].Host = strings.TrimSpace(c.Outbounds[i].Host)
	}
	return c, nil
}
func (u *Window) saveConfig() {
	if u.busy || !u.dirty {
		return
	}
	c, err := u.collect()
	if err != nil {
		u.SetNotice(err.Error())
		return
	}
	base := u.base.Clone()
	u.perform(func(ctx context.Context) (string, error) {
		latest, err := u.api.Status(ctx)
		if err != nil {
			return "", err
		}
		if !reflect.DeepEqual(base, latest.Config) {
			return "", fmt.Errorf("配置已在其他窗口更新，请先撤销修改，重新读取后再编辑")
		}
		_, err = u.api.Post(ctx, "/api/config", latest.Token, c)
		message := "设置已保存，正在重新检测出口"
		if c.ProxyPort != base.ProxyPort {
			message = fmt.Sprintf("网关端口已改为 %d。已同步启用的接入；请重开终端、重建容器，并更新手动填写的代理地址。", c.ProxyPort)
		}
		return message, err
	}, func() { u.status.Config = c.Clone(); u.fill(c) })
}

func (u *Window) gatewayContent() fyne.CanvasObject {
	return newFormScroll(container.NewVBox(
		widget.NewCard("本地网关入口", "HTTP / HTTPS CONNECT / SOCKS5 共用一个端口", widget.NewForm(widget.NewFormItem("监听端口", u.proxyPort))),
		wrap("默认 17890，可设置为 1024–65535。仅监听 127.0.0.1，不能与代理出口或管理端口冲突。"),
		wrap("保存后自动重新监听并同步已启用的系统代理、Git、终端、Docker 和 WSL 接入。现有连接可能需要重试；已打开的终端和已创建的容器需重新打开或创建。macOS 更新系统代理时会请求管理员授权。"),
		wrap("在其他应用中手动填写的代理地址需自行修改。管理端口 17880、17881、17882 保持固定。")), u.proxyPort)
}
