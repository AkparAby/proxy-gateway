package main

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	client "local/proxy-gateway/internal/gatewayclient"
)

type checkAPI struct {
	fixtureAPI
	calls atomic.Int32
	err   error
}

func (a *checkAPI) Check(context.Context, string) (uint64, error) {
	a.calls.Add(1)
	return 5, a.err
}

func TestManualCheckShowsEachProxyUntilItsOwnRoundCompletes(t *testing.T) {
	m, tt, queue := testModel(t)
	api := &checkAPI{}
	m.api = api
	click(t, tt, "立即检查网络")
	for _, name := range []string{"代理 1", "代理 2"} {
		if _, ok := tt.AccessByRole(ui.RoleProgress, name+" 正在检测延迟"); !ok {
			t.Fatal("missing spinner", name)
		}
	}
	m.checkNetwork()
	complete(t, queue)
	if api.calls.Load() != 1 || !m.manualCheckActive() || m.busy {
		t.Fatal("check acknowledgement finished the check or locked editing")
	}
	s := fixtureStatus()
	for id, h := range s.Health {
		h.CheckID = 4
		s.Health[id] = h
	}
	m.receive(s, nil)
	if !m.outboundChecking("a") || !m.outboundChecking("b") {
		t.Fatal("old results completed a newer request")
	}
	h := s.Health["a"]
	h.CheckID, h.SmoothedLatency = 5, 31
	s.Health["a"] = h
	h = s.Health["b"]
	h.CheckID, h.Checking = 5, true
	s.Health["b"] = h
	m.receive(s, nil)
	tt.Frame()
	if m.outboundChecking("a") || !m.outboundChecking("b") || !tt.HasText("31 ms") {
		t.Fatal("fast result waited for the other proxy")
	}
	h = s.Health["b"]
	h.Checking, h.LastOK = false, false
	s.Health["b"] = h
	m.receive(s, nil)
	tt.Frame()
	if m.manualCheckActive() || m.fastPoll.Load() || !tt.HasText("检测失败") {
		t.Fatal("failed probe left the spinner running")
	}
	if tt.HasText("已开始检查网络") || tt.HasText("所有修改已保存") || tt.HasText("保存全部设置") {
		t.Fatal("overview footer still contains action feedback")
	}
	if !tt.HasText("网关运行中 · 代理 1 · 127.0.0.1:17890") {
		t.Fatal("missing live gateway footer")
	}
}

func TestAutomaticChecksKeepResultsVisible(t *testing.T) {
	m, tt, _ := testModel(t)
	s := fixtureStatus()
	for id, h := range s.Health {
		h.CheckID, h.Checking = 6, true
		s.Health[id] = h
	}
	m.receive(s, nil)
	tt.Frame()
	for _, name := range []string{"代理 1", "代理 2"} {
		if _, ok := tt.AccessByRole(ui.RoleProgress, name+" 正在检测延迟"); ok {
			t.Fatal("automatic check showed a spinner", name)
		}
	}
	if m.manualCheckActive() || tt.HasText("检测中") || tt.HasText("正在检查…") ||
		!tt.HasText("45 ms") || !tt.HasText("81 ms") || !tt.HasText("立即检查网络") {
		t.Fatal("automatic check replaced previous results or the manual check button")
	}
	h := s.Health["a"]
	h.Checking, h.SmoothedLatency = false, 32
	s.Health["a"] = h
	m.receive(s, nil)
	tt.Frame()
	if !tt.HasText("32 ms") || !tt.HasText("81 ms") {
		t.Fatal("automatic result did not refresh independently")
	}
}

func TestAutomaticRoundAfterManualCompletionDoesNotRestartSpinner(t *testing.T) {
	m, tt, queue := testModel(t)
	m.api = &checkAPI{}
	m.checkNetwork()
	complete(t, queue)
	s := fixtureStatus()
	// Polling may miss the end of the requested round and see the next one.
	for id, h := range s.Health {
		h.CheckID, h.Checking = 6, true
		s.Health[id] = h
	}
	m.receive(s, nil)
	tt.Frame()
	if m.manualCheckActive() || m.outboundChecking("a") || m.outboundChecking("b") ||
		tt.HasText("检测中") || !tt.HasText("立即检查网络") {
		t.Fatal("a later automatic round kept the manual check running")
	}
}

func TestCheckDisconnectFailureTimeoutAndRestart(t *testing.T) {
	m, _, queue := testModel(t)
	api := &checkAPI{}
	m.api = api
	m.checkNetwork()
	m.receive(client.Status{}, fmt.Errorf("offline"))
	complete(t, queue)
	if m.manualCheckActive() || m.outboundChecking("a") {
		t.Fatal("late reply revived disconnected spinner")
	}
	m.receive(fixtureStatus(), nil)
	api.err = fmt.Errorf("request rejected")
	m.checkNetwork()
	complete(t, queue)
	if m.manualCheckActive() || !m.noticeError || !strings.Contains(m.notice, "request rejected") {
		t.Fatal("failed request cannot be retried")
	}
	api.err = nil
	m.checkNetwork()
	complete(t, queue)
	m.checkDeadline = time.Now().Add(-time.Second)
	m.receive(fixtureStatus(), nil)
	if m.manualCheckActive() || !strings.Contains(m.notice, "超时") {
		t.Fatal("missing deadline recovery")
	}
	m.checkNetwork()
	complete(t, queue)
	s := fixtureStatus()
	s.Token = "restarted"
	m.receive(s, nil)
	if m.manualCheckActive() || !strings.Contains(m.notice, "重启") {
		t.Fatal("restart retained an obsolete check ID")
	}
}

func TestSaveActionsMoveToHeader(t *testing.T) {
	m, tt, _ := testModel(t)
	click(t, tt, "切换策略")
	bounds, ok := tt.Find("保存全部设置")
	if !ok || bounds.Y > 180 {
		t.Fatal("save button is not in header", bounds)
	}
	m.message("设置已保存", false)
	tt.Frame()
	tt.Advance(6 * time.Second)
	if tt.HasText("设置已保存") {
		t.Fatal("action feedback did not expire")
	}
	for range 2 {
		m.message("保存失败，请重试", true)
		tt.Frame()
		if !tt.HasText("保存失败，请重试") {
			t.Fatal("repeated action failure was hidden")
		}
		tt.Advance(6 * time.Second)
	}
}
