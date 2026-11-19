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
	"reflect"
	"strconv"
	"strings"
)

func (u *Window) markDirty() {
	if u.loading {
		return
	}
	u.dirty = true
	u.saved.SetText("有未保存的修改")
	u.controls()
}
func (u *Window) buildEditor() {
	u.list = widget.NewList(func() int { return len(u.draft.Outbounds) }, func() fyne.CanvasObject { return widget.NewLabel("代理出口") }, func(id widget.ListItemID, obj fyne.CanvasObject) {
		o := u.draft.Outbounds[id]
		obj.(*widget.Label).SetText(fmt.Sprintf("%d. %s", id+1, o.Name))
	})
	u.list.OnSelected = func(id widget.ListItemID) { u.selected = id; u.loadSelected(); u.controls() }
	entry := func() *widget.Entry { e := widget.NewEntry(); u.editables = append(u.editables, e); return e }
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
	right := container.NewVScroll(container.NewVBox(widget.NewCard("编辑代理出口", "", form), wrap("列表从上到下排列优先级，第一个为首选。支持 1–16 个本机 HTTP / SOCKS5 出口。"), wrap("地址使用 127.0.0.1 或 ::1。客户端须已连接，端口不可指回网关。修改后点击下方“保存全部设置”。")))
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
	used := map[int]bool{17880: true, 17881: true, 17882: true, 17890: true}
	for _, o := range u.draft.Outbounds {
		used[o.Port] = true
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
	u.mode = widget.NewSelect([]string{"自动按优先级切换"}, func(_ string) {
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
	entry := func(multiline bool) *widget.Entry {
		e := widget.NewEntry()
		if multiline {
			e = widget.NewMultiLineEntry()
			e.SetMinRowsVisible(3)
			e.Wrapping = fyne.TextWrapWord
		}
		e.OnChanged = func(string) { u.markDirty() }
		u.editables = append(u.editables, e)
		return e
	}
	u.interval, u.timeout, u.failures, u.recovery = entry(false), entry(false), entry(false), entry(false)
	u.targets, u.domains = entry(true), entry(true)
	u.editables = append(u.editables, u.mode, u.autoReturn)
}
func (u *Window) policyContent() fyne.CanvasObject {
	form := widget.NewForm(widget.NewFormItem("运行模式", u.mode), widget.NewFormItem("恢复行为", u.autoReturn), widget.NewFormItem("恢复观察（秒）", u.recovery), widget.NewFormItem("检查间隔（秒）", u.interval), widget.NewFormItem("单次超时（毫秒）", u.timeout), widget.NewFormItem("连续失败次数", u.failures), widget.NewFormItem("HTTPS 检测地址", u.targets), widget.NewFormItem("直连域名后缀", u.domains))
	return container.NewVScroll(container.NewVBox(form, wrap("检测地址、直连域名每行一个。任一检测地址返回成功即视为出口健康。已有连接不会自动迁移；全部出口故障时拒绝代理请求。")))
}
func (u *Window) syncModes() {
	if u.mode == nil {
		return
	}
	was := u.loading
	u.loading = true
	defer func() { u.loading = was }()
	options := []string{"自动按优先级切换"}
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
}
func (u *Window) fill(c client.Config) {
	u.loading = true
	u.draft, u.base = c.Clone(), c.Clone()
	u.selected = 0
	u.interval.SetText(strconv.Itoa(c.Interval))
	u.timeout.SetText(strconv.Itoa(c.Timeout))
	u.failures.SetText(strconv.Itoa(c.Failures))
	u.recovery.SetText(strconv.Itoa(c.Recovery))
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
		entry *widget.Entry
		value *int
		name  string
	}{{u.interval, &c.Interval, "检查间隔"}, {u.timeout, &c.Timeout, "超时"}, {u.failures, &c.Failures, "失败次数"}, {u.recovery, &c.Recovery, "观察时间"}} {
		value, err := strconv.Atoi(strings.TrimSpace(pair.entry.Text))
		if err != nil {
			return c, fmt.Errorf("%s必须为整数", pair.name)
		}
		*pair.value = value
	}
	c.AutoReturn = u.autoReturn.SelectedIndex() == 0
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
		return "设置已保存，正在重新检测出口", err
	}, func() { u.status.Config = c.Clone(); u.fill(c) })
}
