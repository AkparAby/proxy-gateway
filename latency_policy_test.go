package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLatencyDebounceCooldownAndFailure(t *testing.T) {
	c := defaults()
	c.LatencyPolicy.Enabled = true
	now := time.Now()
	s := latencySelector{}
	h := c.initialHealth()
	record := func(at time.Time, a, b int) {
		for i, value := range []int{a, b} {
			id := c.Outbounds[i].ID
			entry := h[id]
			entry.record(value >= 0, value, "", at, c.Failures)
			h[id] = entry
		}
	}
	record(now, 150, 200)
	current, _ := s.choose(c, "REJECT", h, now)
	if current != "proxy-1" {
		t.Fatal(current)
	}
	s.switched(now)
	// Ignore the rolling average warm-up, then require three consecutive wins.
	for i := 1; i <= 4; i++ {
		at := now.Add(time.Duration(i) * 5 * time.Second)
		record(at, 150, 50)
		if got, _ := s.choose(c, current, h, at); got != current {
			t.Fatalf("switched before cooldown at round %d", i)
		}
	}
	at := now.Add(30 * time.Second)
	record(at, 150, 50)
	if got, _ := s.choose(c, current, h, at); got != "proxy-2" {
		t.Fatal("did not select faster exit", got)
	}
	s.switched(at)
	current = "proxy-2"
	for i := 1; i <= 3; i++ {
		at = at.Add(time.Second)
		record(at, 150, -1)
		got, _ := s.choose(c, current, h, at)
		if i < 3 && got != current {
			t.Fatal("isolated failure switched", got)
		}
		if i == 3 && got != "proxy-1" {
			t.Fatal("failed exit did not bypass cooldown", got)
		}
	}
}

func TestLatencyDoesNotUseStaleFailedOrMissingMeasurements(t *testing.T) {
	c := defaults()
	c.LatencyPolicy.Enabled = true
	c.LatencyPolicy.Rounds = 1
	c.LatencyPolicy.Cooldown = 0
	now := time.Now()
	valid := Health{}
	valid.record(true, 100, "", now, c.Failures)
	for name, bad := range map[string]Health{
		"unmeasured":  {Available: true},
		"stale":       {Available: true, Checked: true, LastOK: true, CheckedAt: now.Add(-time.Hour), SmoothedLatency: 1},
		"failed zero": {Available: true, Checked: true, LastOK: false, CheckedAt: now},
	} {
		t.Run(name, func(t *testing.T) {
			s := latencySelector{}
			h := map[string]Health{"proxy-1": valid, "proxy-2": bad}
			if got, _ := s.choose(c, "proxy-1", h, now); got != "proxy-1" {
				t.Fatal(got)
			}
			if got, _ := s.choose(c, "REJECT", h, now); got != "proxy-1" {
				t.Fatal(got)
			}
		})
	}
	s := latencySelector{}
	if got, _ := s.choose(c, "REJECT", map[string]Health{}, now); got != "REJECT" {
		t.Fatal(got)
	}
}

func TestLatencyMinimumImprovementTiesAndManualMode(t *testing.T) {
	c := defaults()
	c.LatencyPolicy.Enabled = true
	c.LatencyPolicy.Rounds = 1
	c.LatencyPolicy.Cooldown = 0
	now := time.Now()
	h := c.initialHealth()
	for id := range h {
		entry := Health{}
		entry.record(true, 100, "", now, c.Failures)
		h[id] = entry
	}
	s := latencySelector{}
	if got, _ := s.choose(c, "proxy-2", h, now); got != "proxy-2" {
		t.Fatal("tie must keep current")
	}
	entry := h["proxy-2"]
	entry.SmoothedLatency = 81
	h["proxy-2"] = entry
	if got, _ := s.choose(c, "proxy-1", h, now); got != "proxy-1" {
		t.Fatal("small difference switched")
	}
	entry.SmoothedLatency = 80
	h["proxy-2"] = entry
	if got, _ := s.choose(c, "proxy-1", h, now); got != "proxy-2" {
		t.Fatal("threshold did not switch")
	}
	c.Mode = "proxy-1"
	if got, _ := s.choose(c, "proxy-2", h, now); got != "proxy-1" {
		t.Fatal("manual ignored")
	}
	delete(h, "proxy-1")
	if got, _ := s.choose(c, "proxy-2", h, now); got != "REJECT" {
		t.Fatal("manual failed open")
	}
}

func TestLatencyCandidateMustWinConsecutiveRounds(t *testing.T) {
	c := threeOutbounds()
	c.LatencyPolicy.Enabled = true
	c.LatencyPolicy.Cooldown = 0
	now := time.Now()
	s := latencySelector{}
	for i, best := range []string{"proxy-2", "proxy-3", "proxy-2", "proxy-2", "proxy-2"} {
		at := now.Add(time.Duration(i) * 10 * time.Second)
		h := c.initialHealth()
		for id := range h {
			v := Health{}
			delay := 200
			if id == best {
				delay = 50
			}
			v.record(true, delay, "", at, c.Failures)
			h[id] = v
		}
		got, _ := s.choose(c, "proxy-1", h, at)
		if i < 4 && got != "proxy-1" {
			t.Fatal("nonconsecutive wins switched")
		}
		if i == 4 && got != "proxy-2" {
			t.Fatal("consecutive wins did not switch")
		}
	}
}

func TestProbeAveragesOnlySuccessfulTargets(t *testing.T) {
	c := defaults()
	c.Timeout = 1000
	for _, tc := range []struct {
		name      string
		responses []string
		successes int
		latency   int
	}{
		{"both succeed", []string{`{"delay":100}`, `{"delay":300}`}, 2, 200},
		{"one fails", []string{`{"delay":100}`, "failure"}, 1, 100},
		{"one missing delay", []string{`{"delay":100}`, `{}`}, 1, 100},
		{"one negative delay", []string{`{"delay":100}`, `{"delay":-1}`}, 1, 100},
		{"one over timeout", []string{`{"delay":100}`, `{"delay":1001}`}, 1, 100},
		{"zero is valid", []string{`{"delay":0}`, "failure"}, 1, 0},
		{"two successes and one failure", []string{`{"delay":100}`, "failure", `{"delay":300}`}, 2, 200},
		{"all fail", []string{"failure", `{}`}, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			responses := make(map[string]string, len(tc.responses))
			c.Targets = nil
			for i, body := range tc.responses {
				target := fmt.Sprintf("https://target-%d.example/", i)
				c.Targets = append(c.Targets, target)
				responses[target] = body
			}
			core := &Core{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				body := responses[r.URL.Query().Get("url")]
				if body == "failure" {
					w.WriteHeader(503)
				} else {
					fmt.Fprint(w, body)
				}
				return w.Result(), nil
			})}}
			ok, latency, detail := core.probe(context.Background(), "proxy-1", c)
			if ok != (tc.successes > 0) || latency != tc.latency {
				t.Fatalf("got %v %d, want successes=%d latency=%d", ok, latency, tc.successes, tc.latency)
			}
			if ok && !strings.Contains(detail, fmt.Sprintf("%d/%d 成功", tc.successes, len(tc.responses))) {
				t.Fatal("incorrect successful target count", detail)
			}
		})
	}
}

func TestProbeExcludesUnfinishedTargetsAtDeadline(t *testing.T) {
	c := defaults()
	c.Timeout = 1000
	c.Targets = []string{"https://fast.example/", "https://timeout.example/"}
	core := &Core{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Query().Get("url"), "timeout") {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		w := httptest.NewRecorder()
		fmt.Fprint(w, `{"delay":100}`)
		return w.Result(), nil
	})}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	ok, latency, detail := core.probe(ctx, "proxy-1", c)
	if !ok || latency != 100 || !strings.Contains(detail, "1/2 成功") {
		t.Fatalf("timed out target affected successful latency: %v %d %s", ok, latency, detail)
	}
}
