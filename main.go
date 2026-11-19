package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"
)

//go:embed web/*
var web embed.FS

// Set by the release build; visible in /api/status for support and verification.
var version = "dev"
var commit = "unknown"

type Event struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}
type Manager struct {
	mu        sync.Mutex
	op        sync.Mutex
	config    Config
	health    map[string]Health
	selected  string
	reason    string
	lastError string
	events    []Event
	core      *Core
	root      string
	token     string
	wake      chan struct{}
	cancel    context.CancelFunc
	bridges   *Bridges
	rebind    func(context.Context, string, int) error
}

func nonce() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (m *Manager) event(msg string) {
	m.events = append([]Event{{time.Now(), msg}}, m.events...)
	if len(m.events) > 100 {
		m.events = m.events[:100]
	}
	log.Println(msg)
}
func (m *Manager) kick() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) cycle(ctx context.Context) {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	conf := m.config
	m.mu.Unlock()
	if m.bridges != nil {
		if err := m.bridges.sync(m.root, false, conf.ProxyPort); err != nil {
			log.Println(err)
		}
	}
	type outcome struct {
		name    string
		ok      bool
		latency int
		detail  string
	}
	ch := make(chan outcome, len(conf.Outbounds))
	for _, outbound := range conf.Outbounds {
		go func(name string) {
			ok, latency, detail := m.core.probe(ctx, name, conf)
			ch <- outcome{name, ok, latency, detail}
		}(outbound.ID)
	}
	outcomes := make([]outcome, 0, len(conf.Outbounds))
	for range conf.Outbounds {
		outcomes = append(outcomes, <-ch)
	}
	now := time.Now()
	m.mu.Lock()
	for _, r := range outcomes {
		h := m.health[r.name]
		was := h.Available
		h.record(r.ok, r.latency, r.detail, now, conf.Failures)
		m.health[r.name] = h
		if was != h.Available {
			m.event(fmt.Sprintf("%s 状态：可用=%t", conf.name(r.name), h.Available))
		}
	}
	next, reason := choose(conf, m.selected, m.health, now)
	m.mu.Unlock()
	call, cancel := context.WithTimeout(ctx, 3*time.Second)
	err := m.core.selectProxy(call, next)
	cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.lastError = "网关内核控制失败：" + err.Error()
		return
	}
	m.lastError = ""
	if next != m.selected {
		m.event(fmt.Sprintf("出口 %s → %s：%s", conf.name(m.selected), conf.name(next), reason))
		m.selected = next
		_ = atomicJSON(filepath.Join(m.root, "runtime", "selection.json"), map[string]string{"selected": next})
	}
	m.reason = reason
}
func (m *Manager) monitor(ctx context.Context) {
	for {
		m.cycle(ctx)
		m.mu.Lock()
		interval := m.config.Interval
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-time.After(time.Duration(interval) * time.Second):
		}
	}
}
func (m *Manager) status() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	var integrations any
	data, err := os.ReadFile(filepath.Join(m.root, "runtime", "integrations-status.json"))
	if err == nil {
		_ = json.Unmarshal(data, &integrations)
	}
	health := make(map[string]Health, len(m.health))
	for id, value := range m.health {
		health[id] = value
	}
	return map[string]any{"version": version, "commit": commit, "config": m.config, "health": health, "selected": m.selected, "reason": m.reason, "error": m.lastError, "events": append([]Event(nil), m.events...), "token": m.token, "proxy": proxyAddress(m.config.ProxyPort), "dashboard": "http://127.0.0.1:17880", "integrations": integrations, "platform": runtime.GOOS, "integrationOptions": integrationOptions()}
}
func (m *Manager) saveConfig(ctx context.Context, c Config) error {
	if e := c.validate(); e != nil {
		return e
	}
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	old := m.config
	oldSelected := m.selected
	m.mu.Unlock()
	portChanged := c.ProxyPort != old.ProxyPort
	if portChanged {
		if err := checkProxyPort(c.ProxyPort); err != nil {
			return err
		}
	}
	path := filepath.Join(m.core.dir, "config.json")
	rebound := false
	rollback := func(cause error) error {
		// A canceled save must still be able to restore the previous listener.
		restoreCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var failures []error
		if err := atomicJSON(path, coreConfig(old, m.core.secret)); err != nil {
			failures = append(failures, err)
		}
		if err := m.core.reload(restoreCtx); err != nil {
			failures = append(failures, err)
		}
		if err := m.core.selectProxy(restoreCtx, oldSelected); err != nil {
			failures = append(failures, err)
		}
		if rebound {
			if err := m.rebindIntegrations(restoreCtx, old.ProxyPort); err != nil {
				failures = append(failures, err)
			}
			if m.bridges != nil {
				if err := m.bridges.sync(m.root, false, old.ProxyPort); err != nil {
					failures = append(failures, err)
				}
			}
		}
		if len(failures) > 0 {
			cause = fmt.Errorf("%w；回退未完成，请检查网关与接入设置：%v", cause, errors.Join(failures...))
			m.mu.Lock()
			m.lastError = cause.Error()
			m.event(m.lastError)
			m.mu.Unlock()
		}
		return cause
	}
	if e := atomicJSON(path, coreConfig(c, m.core.secret)); e != nil {
		return e
	}
	if e := m.core.reload(ctx); e != nil {
		return rollback(e)
	}
	if portChanged {
		if err := m.core.verifyPort(ctx, c.ProxyPort); err != nil {
			return rollback(err)
		}
	}
	if e := m.core.selectProxy(ctx, "REJECT"); e != nil {
		return rollback(e)
	}
	if portChanged {
		rebound = true
		if err := m.rebindIntegrations(ctx, c.ProxyPort); err != nil {
			return rollback(err)
		}
		if m.bridges != nil {
			if err := m.bridges.sync(m.root, false, c.ProxyPort); err != nil {
				return rollback(err)
			}
		}
	}
	if e := atomicJSON(filepath.Join(m.root, "config.json"), c); e != nil {
		return rollback(e)
	}
	m.mu.Lock()
	m.config = c
	m.health = c.initialHealth()
	m.selected = "REJECT"
	m.reason = "配置已保存，重新检测出口"
	m.event("已更新网关配置")
	if portChanged {
		m.event(fmt.Sprintf("网关端口 %d → %d，已同步启用的接入", old.ProxyPort, c.ProxyPort))
	}
	m.mu.Unlock()
	_ = atomicJSON(filepath.Join(m.root, "runtime", "selection.json"), map[string]string{"selected": "REJECT"})
	m.kick()
	return nil
}
func (m *Manager) returnPrimary(ctx context.Context) error {
	m.op.Lock()
	defer m.op.Unlock()
	m.mu.Lock()
	primary := m.config.primary()
	auto := m.config.Mode == "auto"
	healthy := m.health[primary].Available
	m.mu.Unlock()
	if !auto {
		return errors.New("请先切换到自动模式，再手动切回首选出口")
	}
	if !healthy {
		return errors.New("首选出口尚未恢复，请先检查连接")
	}
	if err := m.core.selectProxy(ctx, primary); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.selected = primary
	m.reason = "已手动切回首选出口，保留自动切换模式"
	m.event(m.reason)
	return atomicJSON(filepath.Join(m.root, "runtime", "selection.json"), map[string]string{"selected": primary})
}
func (m *Manager) integrate(ctx context.Context, name string, enabled bool) (string, error) {
	valid := map[string]bool{"system": true, "terminal": true, "git": true, "docker": true, "wsl": true, "autostart": true, "restore": true, "status": true}
	if !valid[name] {
		return "", errors.New("未知接入项")
	}
	m.op.Lock()
	defer m.op.Unlock()
	mode := "disable"
	if enabled {
		mode = "enable"
	}
	if name == "wsl" && enabled {
		if err := m.bridges.sync(m.root, true, m.config.ProxyPort); err != nil {
			return err.Error(), err
		}
	}
	text, err := runIntegration(ctx, m.root, name, enabled)
	_ = m.bridges.sync(m.root, false, m.config.ProxyPort)
	if err != nil {
		return err.Error(), err
	}
	m.mu.Lock()
	m.event(fmt.Sprintf("应用接入 %s：%s", name, mode))
	m.mu.Unlock()
	return text, nil
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func (m *Manager) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; frame-ancestors 'none'")
	if r.Host != "127.0.0.1:17880" && r.Host != "localhost:17880" {
		http.Error(w, "Invalid host", 403)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		http.Error(w, "Invalid origin", 403)
		return
	}
	if r.Method != "GET" {
		if r.Header.Get("X-Gateway-Token") != m.token {
			http.Error(w, "Invalid token", 403)
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	switch {
	case r.URL.Path == "/api/return" && r.Method == "POST":
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		if e := m.returnPrimary(ctx); e != nil {
			writeJSON(w, 400, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case r.URL.Path == "/api/status" && r.Method == "GET":
		writeJSON(w, 200, m.status())
	case r.URL.Path == "/api/check" && r.Method == "POST":
		m.kick()
		writeJSON(w, 200, map[string]bool{"ok": true})
	case r.URL.Path == "/api/config" && r.Method == "POST":
		m.mu.Lock()
		c := Config{ProxyPort: m.config.ProxyPort}
		m.mu.Unlock()
		if e := json.NewDecoder(r.Body).Decode(&c); e != nil {
			writeJSON(w, 400, map[string]string{"error": e.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if e := m.saveConfig(ctx, c); e != nil {
			writeJSON(w, 400, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	case r.URL.Path == "/api/integration" && r.Method == "POST":
		var data struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		}
		if e := json.NewDecoder(r.Body).Decode(&data); e != nil {
			writeJSON(w, 400, map[string]string{"error": e.Error()})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		output, e := m.integrate(ctx, data.Name, data.Enabled)
		if e != nil {
			writeJSON(w, 400, map[string]string{"error": output})
			return
		}
		writeJSON(w, 200, map[string]string{"message": output})
	case r.URL.Path == "/api/stop" && r.Method == "POST":
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		_, e := m.integrate(ctx, "restore", false)
		cancel()
		if e != nil {
			writeJSON(w, 500, map[string]string{"error": e.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"message": "接入设置已恢复，网关正在停止"})
		go func() { time.Sleep(300 * time.Millisecond); m.cancel() }()
	case r.Method == "GET":
		name := r.URL.Path
		if name == "/" {
			name = "/index.html"
		}
		if name != "/index.html" && name != "/app.js" && name != "/style.css" {
			http.NotFound(w, r)
			return
		}
		b, e := web.ReadFile("web" + name)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		types := map[string]string{"/index.html": "text/html; charset=utf-8", "/app.js": "text/javascript; charset=utf-8", "/style.css": "text/css; charset=utf-8"}
		w.Header().Set("Content-Type", types[name])
		w.Write(b)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}
func run() error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	options, e := parseLaunchOptions(exe, os.Args[1:])
	if e != nil {
		return e
	}
	root := options.root
	if options.restore {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		output, err := restoreFromCLI(ctx, root)
		fmt.Println(output)
		return err
	}
	listener, e := net.Listen("tcp4", "127.0.0.1:17880")
	if e != nil {
		client := dashboardClient()
		defer client.CloseIdleConnections()
		if err := presentDashboard(context.Background(), client, 10*time.Second, options.background, openDashboard); err != nil {
			return fmt.Errorf("管理端口已占用或管理页无法打开：%w", err)
		}
		return nil
	}
	defer listener.Close()
	for _, port := range []string{"17881"} {
		l, e := net.Listen("tcp4", "127.0.0.1:"+port)
		if e != nil {
			return fmt.Errorf("端口 %s 已占用：%w", port, e)
		}
		l.Close()
	}
	if e = os.MkdirAll(filepath.Join(root, "runtime"), 0700); e != nil {
		return e
	}
	lf, e := os.OpenFile(filepath.Join(root, "runtime", "gateway.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer func() { log.SetOutput(os.Stderr); lf.Close() }()
	log.SetOutput(lf)
	conf, e := readConfig(filepath.Join(root, "config.json"))
	if e != nil {
		return e
	}
	if err := checkProxyPort(conf.ProxyPort); err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	core := &Core{executable: coreExecutable(exe, root), secret: nonce(), dir: filepath.Join(root, "runtime"), client: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 15 * time.Second}}
	if e = atomicJSON(filepath.Join(core.dir, "config.json"), coreConfig(conf, core.secret)); e != nil {
		return e
	}
	if e = core.start(ctx); e != nil {
		return e
	}
	defer core.stop()
	if err := core.verifyPort(ctx, conf.ProxyPort); err != nil {
		return err
	}
	selected := "REJECT"
	var last map[string]string
	if b, e := os.ReadFile(filepath.Join(core.dir, "selection.json")); e == nil {
		if json.Unmarshal(b, &last) == nil && conf.index(last["selected"]) >= 0 {
			selected = last["selected"]
		}
	}
	bridges := &Bridges{}
	if e := bridges.sync(root, false, conf.ProxyPort); e != nil {
		log.Println(e)
	}
	defer bridges.close()
	m := &Manager{config: conf, health: conf.initialHealth(), selected: selected, root: root, core: core, token: nonce(), wake: make(chan struct{}, 1), cancel: cancel, bridges: bridges}
	m.event("本地代理网关已启动")
	go m.monitor(ctx)
	server := &http.Server{Handler: http.HandlerFunc(m.serve), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if e := server.Serve(listener); e != nil && e != http.ErrServerClosed {
			log.Println(e)
			cancel()
		}
	}()
	if !options.background {
		go func() {
			client := dashboardClient()
			defer client.CloseIdleConnections()
			if err := presentDashboard(ctx, client, 10*time.Second, false, openDashboard); err != nil {
				log.Println(err)
			}
		}()
	}
	// A dead core is restarted with REJECT selected until fresh health checks succeed.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case err := <-core.done:
				m.op.Lock()
				m.mu.Lock()
				m.lastError = fmt.Sprintf("内核退出，正在重启：%v", err)
				m.health = m.config.initialHealth()
				m.event(m.lastError)
				m.mu.Unlock()
				for ctx.Err() == nil {
					if err = core.start(ctx); err == nil {
						break
					}
					select {
					case <-ctx.Done():
					case <-time.After(3 * time.Second):
					}
				}
				m.op.Unlock()
				m.kick()
			}
		}
	}()
	<-ctx.Done()
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_ = server.Shutdown(shutdown)
	return nil
}
func main() {
	if e := run(); e != nil {
		log.Print(e)
		reportLaunchError(e)
		os.Exit(1)
	}
}
