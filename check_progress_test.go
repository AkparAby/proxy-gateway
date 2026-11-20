package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCheckProgressPublishesEachResultAndIdentifiesQueuedRound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conf := defaults()
	conf.Targets = []string{"https://example.com"}
	root := t.TempDir()
	_ = os.Mkdir(filepath.Join(root, "runtime"), 0700)
	fast, slow := make(chan struct{}), make(chan struct{})
	started := make(chan struct{}, 4)
	var selected atomic.Bool
	core := &Core{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		if strings.HasSuffix(r.URL.Path, "/delay") {
			gate := slow
			if strings.Contains(r.URL.Path, "upstream-proxy-1/") {
				gate = fast
			}
			started <- struct{}{}
			select {
			case <-gate:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			fmt.Fprint(w, `{"delay":42}`)
		} else {
			selected.Store(true)
			w.WriteHeader(204)
		}
		return w.Result(), nil
	})}}
	m := &Manager{config: conf, core: core, root: root, health: conf.initialHealth(), selected: "REJECT", wake: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() { m.cycle(ctx); close(done) }()
	for range conf.Outbounds {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("probe did not start")
		}
	}
	for _, h := range m.status()["health"].(map[string]Health) {
		if h.CheckID != 1 || !h.Checking {
			t.Fatal("missing running state", h)
		}
	}
	if m.requestCheck() != 2 || m.requestCheck() != 2 || len(m.wake) != 1 {
		t.Fatal("requests during a probe must identify and coalesce the next round")
	}
	close(fast)
	deadline := time.Now().Add(3 * time.Second)
	for {
		health := m.status()["health"].(map[string]Health)
		if !health["proxy-1"].Checking {
			if !health["proxy-1"].LastOK || !health["proxy-2"].Checking || selected.Load() {
				t.Fatal("partial results were not published independently", health)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fast result waited for slow result")
		}
		time.Sleep(time.Millisecond)
	}
	close(slow)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cycle did not finish")
	}
	<-m.wake
	m.cycle(ctx)
	for _, h := range m.status()["health"].(map[string]Health) {
		if h.CheckID != 2 || h.Checking {
			t.Fatal("queued round did not complete", h)
		}
	}
}
