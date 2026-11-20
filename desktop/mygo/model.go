package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
	client "local/proxy-gateway/internal/gatewayclient"
	"local/proxy-gateway/internal/routing"
)

type gatewayAPI interface {
	Status(context.Context) (client.Status, error)
	Post(context.Context, string, string, any) (string, error)
	Check(context.Context, string) (uint64, error)
}

type model struct {
	api                                   gatewayAPI
	ctx                                   context.Context
	dispatch                              func(func())
	ensure                                func(context.Context) error
	restore                               func(context.Context) error
	quit                                  func()
	onStatus                              func()
	status                                client.Status
	base, draft                           client.Config
	online, loaded, busy, starting, dirty bool
	epoch                                 atomic.Uint64
	page, selected                        string
	notice                                string
	noticeError                           bool
	noticeSerial, shownNoticeSerial       uint64
	checkPending                          bool
	checkID, checkTicket                  uint64
	checkToken                            string
	checkDeadline                         time.Time
	fastPoll                              atomic.Bool
	pollWake                              chan struct{}
	fields                                map[string]*string
	ports                                 map[string]*string
	scrolls                               map[string]*ui.ScrollState
	rule                                  ruleDraft
	ruleOpen, exitOpen                    bool
}

type ruleDraft struct {
	value                                             routing.AppRule
	index                                             int
	application, outbound, method, paths, port, error string
}

func newModel(ctx context.Context, api gatewayAPI) *model {
	return &model{api: api, ctx: ctx, page: "overview", fields: map[string]*string{}, ports: map[string]*string{}, scrolls: map[string]*ui.ScrollState{}, pollWake: make(chan struct{}, 1)}
}

func (m *model) message(text string, failure bool) {
	m.notice, m.noticeError = text, failure
	m.noticeSerial++
}
func (m *model) editable() bool { return m.online && m.loaded && !m.busy && !m.starting }
func (m *model) changed()       { m.dirty = true }
func (m *model) scroll(page string) *ui.ScrollState {
	if m.scrolls[page] == nil {
		m.scrolls[page] = &ui.ScrollState{}
	}
	return m.scrolls[page]
}
func stringValue(s string) *string { return &s }
func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func (m *model) fill(c client.Config) {
	m.base, m.draft = c.Clone(), c.Clone()
	for key, value := range map[string]int{"proxyPort": c.ProxyPort, "interval": c.Interval, "timeout": c.Timeout, "failures": c.Failures, "recovery": c.Recovery, "rounds": c.LatencyPolicy.Rounds, "improvement": c.LatencyPolicy.MinImprovement, "cooldown": c.LatencyPolicy.Cooldown} {
		m.fields[key] = stringValue(strconv.Itoa(value))
	}
	m.fields["targets"], m.fields["domains"] = stringValue(strings.Join(c.Targets, "\n")), stringValue(strings.Join(c.DirectDomains, "\n"))
	m.ports = map[string]*string{}
	selectedExists := false
	for _, o := range c.Outbounds {
		m.ports[o.ID] = stringValue(strconv.Itoa(o.Port))
		selectedExists = selectedExists || o.ID == m.selected
	}
	if !selectedExists && len(c.Outbounds) > 0 {
		m.selected = c.Outbounds[0].ID
	}
	m.dirty = false
}

func (m *model) receive(s client.Status, err error) {
	if err != nil {
		m.online = false
		m.status.Error = err.Error()
	} else {
		m.online = true
		if !m.loaded || (!m.dirty && !m.busy && !m.ruleOpen && !reflect.DeepEqual(m.base, s.Config)) {
			m.fill(s.Config)
		}
		m.loaded = true
		m.status = s
		if (m.dirty || m.ruleOpen) && !reflect.DeepEqual(m.base, s.Config) {
			const warning = "设置已在其他窗口更新，请撤销修改后重新编辑。"
			if m.notice != warning || !m.noticeError {
				m.message(warning, true)
			}
		}
	}
	m.updateCheckState()
	if m.onStatus != nil {
		m.onStatus()
	}
}

func (m *model) poll() {
	go func() {
		for {
			epoch := m.epoch.Load()
			ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
			s, err := m.api.Status(ctx)
			cancel()
			m.dispatch(func() {
				if m.ctx.Err() == nil && epoch == m.epoch.Load() {
					m.receive(s, err)
				}
			})
			delay := 2 * time.Second
			if m.fastPoll.Load() {
				delay = 250 * time.Millisecond
			}
			select {
			case <-m.ctx.Done():
				return
			case <-m.pollWake:
			case <-time.After(delay):
			}
		}
	}()
}

func (m *model) startGateway() {
	if m.online || m.starting || m.busy || m.ensure == nil {
		return
	}
	m.starting = true
	m.message("", false)
	m.epoch.Add(1)
	go func() {
		err := m.ensure(m.ctx)
		var s client.Status
		if err == nil {
			ctx, cancel := context.WithTimeout(m.ctx, 3*time.Second)
			s, err = m.api.Status(ctx)
			cancel()
		}
		m.dispatch(func() {
			m.epoch.Add(1)
			m.starting = false
			m.receive(s, err)
			if err != nil {
				m.message(err.Error(), true)
			} else {
				m.message("", false)
			}
		})
	}()
}

func (m *model) perform(work func(context.Context) (string, error), done func()) {
	if m.busy || m.starting {
		return
	}
	m.busy = true
	m.epoch.Add(1)
	go func() {
		ctx, cancel := context.WithTimeout(m.ctx, 150*time.Second)
		message, err := work(ctx)
		cancel()
		m.dispatch(func() {
			m.epoch.Add(1)
			m.busy = false
			if err != nil {
				m.message(err.Error(), true)
				return
			}
			m.message(message, false)
			if done != nil {
				done()
			}
		})
	}()
}

func (m *model) action(path string, body any, success string) {
	if !m.editable() {
		return
	}
	token := m.status.Token
	m.perform(func(ctx context.Context) (string, error) {
		message, err := m.api.Post(ctx, path, token, body)
		if message == "" {
			message = success
		}
		return message, err
	}, nil)
}

func lines(s string) []string {
	result := []string{}
	for _, value := range strings.Split(s, "\n") {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func (m *model) collect() (client.Config, error) {
	c := m.draft.Clone()
	for _, f := range []struct {
		key, label string
		dest       *int
	}{
		{"proxyPort", "网关端口", &c.ProxyPort}, {"interval", "检查间隔", &c.Interval}, {"timeout", "检测超时", &c.Timeout}, {"failures", "连续失败次数", &c.Failures}, {"recovery", "恢复观察", &c.Recovery}, {"rounds", "连续更快轮数", &c.LatencyPolicy.Rounds}, {"improvement", "延迟差", &c.LatencyPolicy.MinImprovement}, {"cooldown", "切换冷却", &c.LatencyPolicy.Cooldown},
	} {
		v, err := strconv.Atoi(strings.TrimSpace(*m.fields[f.key]))
		if err != nil {
			return c, fmt.Errorf("%s必须为整数", f.label)
		}
		*f.dest = v
	}
	if c.ProxyPort < 1024 || c.ProxyPort > 65535 || c.ProxyPort >= 17880 && c.ProxyPort <= 17882 {
		return c, fmt.Errorf("网关端口须为 1024–65535，且不能占用管理端口 17880–17882")
	}
	for i := range c.Outbounds {
		o := &c.Outbounds[i]
		port, err := strconv.Atoi(strings.TrimSpace(*m.ports[o.ID]))
		if err != nil || port < 1 || port > 65535 {
			return c, fmt.Errorf("%s 的端口无效", o.Name)
		}
		if port == c.ProxyPort {
			return c, fmt.Errorf("网关端口不能与 %s 相同", o.Name)
		}
		o.Port = port
		o.Name = strings.TrimSpace(o.Name)
		o.Host = strings.TrimSpace(o.Host)
	}
	c.Targets, c.DirectDomains = lines(*m.fields["targets"]), lines(*m.fields["domains"])
	return c, nil
}

func (m *model) save() {
	if !m.editable() || !m.dirty {
		return
	}
	c, err := m.collect()
	if err != nil {
		m.message(err.Error(), true)
		return
	}
	base := m.base.Clone()
	var saved client.Status
	m.perform(func(ctx context.Context) (string, error) {
		latest, err := m.api.Status(ctx)
		if err != nil {
			return "", err
		}
		if !reflect.DeepEqual(latest.Config, base) {
			return "", fmt.Errorf("设置已在其他窗口更新，请撤销修改后重新编辑")
		}
		message, err := m.api.Post(ctx, "/api/config", latest.Token, c)
		if err != nil {
			return "", err
		}
		saved, err = m.api.Status(ctx)
		if err != nil {
			saved = latest
			saved.Config = c
		}
		if message == "" {
			message = "设置已保存，新的连接将使用新策略。"
		}
		return message, nil
	}, func() { m.fill(saved.Config); m.receive(saved, nil) })
}

func (m *model) addOutbound() {
	if !m.editable() || len(m.draft.Outbounds) >= 16 {
		return
	}
	used := map[int]bool{m.draft.ProxyPort: true, 17880: true, 17881: true, 17882: true}
	for _, p := range m.ports {
		if n, err := strconv.Atoi(*p); err == nil {
			used[n] = true
		}
	}
	for _, r := range m.draft.AppRules {
		if r.Match == "port" {
			used[r.ListenPort] = true
		}
	}
	port := 7890
	for used[port] {
		port++
	}
	name := ""
	for n := 1; name == ""; n++ {
		candidate := fmt.Sprintf("代理 %d", n)
		if !slices.ContainsFunc(m.draft.Outbounds, func(o client.Outbound) bool { return o.Name == candidate }) {
			name = candidate
		}
	}
	o := client.Outbound{ID: newID(), Name: name, Type: "http", Host: "127.0.0.1", Port: port}
	m.draft.Outbounds = append(m.draft.Outbounds, o)
	m.ports[o.ID] = stringValue(strconv.Itoa(port))
	m.selected = o.ID
	m.changed()
}

func (m *model) removeOutbound(index int) {
	if !m.editable() || index < 0 || index >= len(m.draft.Outbounds) || len(m.draft.Outbounds) <= 1 {
		return
	}
	id := m.draft.Outbounds[index].ID
	for _, r := range m.draft.AppRules {
		if r.OutboundID == id {
			m.message("请先修改或删除应用 “"+r.Name+"” 的出口绑定。", true)
			return
		}
	}
	m.draft.Outbounds = slices.Delete(m.draft.Outbounds, index, index+1)
	delete(m.ports, id)
	if m.draft.Mode == id {
		m.draft.Mode = "auto"
	}
	m.selected = m.draft.Outbounds[0].ID
	m.changed()
}

func (m *model) moveOutbound(index, delta int) {
	next := index + delta
	if !m.editable() || index < 0 || next < 0 || next >= len(m.draft.Outbounds) {
		return
	}
	m.draft.Outbounds[index], m.draft.Outbounds[next] = m.draft.Outbounds[next], m.draft.Outbounds[index]
	m.changed()
}

func (m *model) restoreAndQuit() {
	if m.busy || m.starting {
		return
	}
	token, online := m.status.Token, m.online
	m.perform(func(ctx context.Context) (string, error) {
		if online {
			return m.api.Post(ctx, "/api/stop", token, nil)
		}
		if m.restore != nil {
			return "接入已恢复", m.restore(ctx)
		}
		return "", fmt.Errorf("无法恢复接入，请使用随包提供的恢复工具")
	}, func() {
		if m.quit != nil {
			m.quit()
		}
	})
}
