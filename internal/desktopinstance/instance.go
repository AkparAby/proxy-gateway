// Package desktopinstance coordinates a single per-user desktop window. The
// gateway has its own independent singleton on its management port.
package desktopinstance

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const Address = "127.0.0.1:17882"

type Instance struct {
	listener    net.Listener
	server      *http.Server
	path, token string
	Show        chan struct{}
}
type record struct {
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func Acquire(ctx context.Context, background bool) (*Instance, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(cache, "Proxy Gateway", "desktop-session.json")
	l, err := net.Listen("tcp4", Address)
	if err != nil {
		return nil, notify(ctx, path, background)
	}
	i := &Instance{listener: l, path: path, Show: make(chan struct{}, 1)}
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		l.Close()
		return nil, err
	}
	i.token = hex.EncodeToString(b)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		l.Close()
		return nil, err
	}
	b, _ = json.Marshal(record{i.token, os.Getpid()})
	if err = os.WriteFile(path, b, 0600); err != nil {
		l.Close()
		return nil, err
	}
	i.server = &http.Server{Handler: http.HandlerFunc(i.handle), ReadHeaderTimeout: time.Second, IdleTimeout: time.Second}
	go i.server.Serve(l)
	return i, nil
}
func (i *Instance) handle(w http.ResponseWriter, r *http.Request) {
	if r.Host != Address || r.Method != "POST" || r.URL.Path != "/activate" || r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Desktop-Token")), []byte(i.token)) != 1 {
		http.Error(w, "Forbidden", 403)
		return
	}
	var body struct {
		Show bool `json:"show"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body) != nil {
		http.Error(w, "Bad request", 400)
		return
	}
	if body.Show {
		select {
		case i.Show <- struct{}{}:
		default:
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"application":"proxy-gateway-desktop"}`))
}
func notify(ctx context.Context, path string, background bool) error {
	c := http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer c.CloseIdleConnections()
	for n := 0; n < 20; n++ {
		var session record
		b, err := os.ReadFile(path)
		if err == nil && json.Unmarshal(b, &session) == nil && len(session.Token) == 64 {
			body, _ := json.Marshal(map[string]bool{"show": !background})
			req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+Address+"/activate", bytes.NewReader(body))
			req.Header.Set("X-Desktop-Token", session.Token)
			resp, err := c.Do(req)
			if err == nil {
				var value struct {
					Application string `json:"application"`
				}
				_ = json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&value)
				resp.Body.Close()
				if resp.StatusCode == 200 && value.Application == "proxy-gateway-desktop" {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("桌面端口 17882 已被占用，无法唤起现有窗口")
}
func (i *Instance) Close() {
	_ = i.server.Close()
	var current record
	if b, err := os.ReadFile(i.path); err == nil && json.Unmarshal(b, &current) == nil && current.Token == i.token {
		_ = os.Remove(i.path)
	}
}
