package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A separate listener on the WSL virtual adapter avoids exposing the proxy on
// Ethernet, Wi-Fi or Tailscale. Management and core APIs stay on loopback.
type Bridges struct {
	mu        sync.Mutex
	listeners map[string]net.Listener
}

func (b *Bridges) sync(root string, force bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.listeners == nil {
		b.listeners = map[string]net.Listener{}
	}
	var state map[string]bool
	if data, e := os.ReadFile(filepath.Join(root, "runtime", "integrations-status.json")); e == nil {
		_ = json.Unmarshal(data, &state)
	}
	wanted := force || state["wsl"]
	addresses := map[string]*net.IPNet{}
	if wanted {
		interfaces, e := net.Interfaces()
		if e != nil {
			return e
		}
		for _, iface := range interfaces {
			if !strings.Contains(strings.ToLower(iface.Name), "wsl") {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				ip, n, e := net.ParseCIDR(a.String())
				if e == nil && ip.To4() != nil && ip.IsPrivate() {
					n.IP = ip
					addresses[ip.String()] = n
				}
			}
		}
	}
	for ip, l := range b.listeners {
		if _, ok := addresses[ip]; !ok {
			l.Close()
			delete(b.listeners, ip)
		}
	}
	for ip, network := range addresses {
		if _, ok := b.listeners[ip]; ok {
			continue
		}
		l, e := net.Listen("tcp4", net.JoinHostPort(ip, "17890"))
		if e != nil {
			return fmt.Errorf("WSL 接入口 %s: %w", ip, e)
		}
		b.listeners[ip] = l
		go bridgeAccept(l, network)
	}
	if force && len(addresses) == 0 {
		return fmt.Errorf("没有找到 WSL 虚拟网卡；请先启动 Ubuntu")
	}
	return nil
}
func (b *Bridges) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range b.listeners {
		l.Close()
	}
}
func bridgeAccept(l net.Listener, network *net.IPNet) {
	for {
		conn, e := l.Accept()
		if e != nil {
			return
		}
		remote, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok || !network.Contains(remote.IP) {
			conn.Close()
			continue
		}
		go func() {
			defer conn.Close()
			upstream, e := net.DialTimeout("tcp4", "127.0.0.1:17890", 3*time.Second)
			if e != nil {
				return
			}
			defer upstream.Close()
			done := make(chan struct{})
			go func() {
				io.Copy(upstream, conn)
				if t, ok := upstream.(*net.TCPConn); ok {
					t.CloseWrite()
				}
				close(done)
			}()
			io.Copy(conn, upstream)
			if t, ok := conn.(*net.TCPConn); ok {
				t.CloseWrite()
			}
			<-done
		}()
	}
}
