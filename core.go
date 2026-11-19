package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

type Core struct {
	secret string
	dir    string
	client *http.Client
	mu     sync.Mutex
	cmd    *exec.Cmd
	done   chan error
	log    *os.File
}

func (c *Core) request(ctx context.Context, method, path string, data any, out any) error {
	var b []byte
	var e error
	if data != nil {
		b, e = json.Marshal(data)
		if e != nil {
			return e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, "http://127.0.0.1:17881"+path, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+c.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.client.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("内核 HTTP %d: %s", resp.StatusCode, body)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
	}
	io.Copy(io.Discard, resp.Body)
	return nil
}
func (c *Core) start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	logfile, e := os.OpenFile(filepath.Join(c.dir, "mihomo.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	cmd := exec.Command(filepath.Join(c.dir, "mihomo.exe"), "-d", c.dir, "-f", filepath.Join(c.dir, "config.json"))
	cmd.Stdout = logfile
	cmd.Stderr = logfile
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if e = cmd.Start(); e != nil {
		logfile.Close()
		return e
	}
	c.cmd = cmd
	c.log = logfile
	c.done = make(chan error, 1)
	done := c.done
	go func() { done <- cmd.Wait(); logfile.Close() }()
	for i := 0; i < 50; i++ {
		check, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		e = c.request(check, "GET", "/version", nil, nil)
		cancel()
		if e == nil {
			return nil
		}
		select {
		case e = <-c.done:
			return fmt.Errorf("内核启动失败: %v", e)
		case <-ctx.Done():
			cmd.Process.Kill()
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	cmd.Process.Kill()
	return fmt.Errorf("内核启动超时: %w", e)
}
func (c *Core) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
}
func (c *Core) selectProxy(ctx context.Context, name string) error {
	return c.request(ctx, "PUT", "/proxies/Gateway", map[string]string{"name": name}, nil)
}
func (c *Core) reload(ctx context.Context) error {
	return c.request(ctx, "PUT", "/configs?force=true", map[string]string{"path": filepath.Join(c.dir, "config.json")}, nil)
}
func (c *Core) probe(ctx context.Context, name string, conf Config) (bool, int, string) {
	type result struct {
		ok      bool
		latency int
	}
	results := make(chan result, len(conf.Targets))
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(conf.Timeout+1200)*time.Millisecond)
	defer cancel()
	for _, target := range conf.Targets {
		go func(target string) {
			var data struct {
				Delay int `json:"delay"`
			}
			path := "/proxies/" + url.PathEscape(name) + "/delay?url=" + url.QueryEscape(target) + "&timeout=" + fmt.Sprint(conf.Timeout) + "&expected=200-299"
			err := c.request(probeCtx, "GET", path, nil, &data)
			results <- result{err == nil, data.Delay}
		}(target)
	}
	for range conf.Targets {
		select {
		case r := <-results:
			if r.ok {
				return true, r.latency, "HTTPS 检测通过"
			}
		case <-probeCtx.Done():
			return false, 0, "HTTPS 检测超时"
		}
	}
	return false, 0, "所有检测地址均失败"
}
