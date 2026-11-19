package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

func proxyAddress(port int) string { return fmt.Sprintf("127.0.0.1:%d", port) }

func (m *Manager) rebindIntegrations(ctx context.Context, port int) error {
	if m.rebind != nil {
		return m.rebind(ctx, m.root, port)
	}
	_, err := runIntegrationAtPort(ctx, m.root, "rebind", true, port)
	return err
}

// Check both sockets before changing configuration or managed application settings.
func checkProxyPort(port int) error {
	address := proxyAddress(port)
	tcp, err := net.Listen("tcp4", address)
	if err != nil {
		return fmt.Errorf("网关端口 %d 已占用或不可用：%w", port, err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp4", address)
	if err != nil {
		return fmt.Errorf("网关 UDP 端口 %d 已占用或不可用：%w", port, err)
	}
	return udp.Close()
}

// Mihomo can accept a reload even when a new listener cannot bind. Verify its
// active configuration and the SOCKS5 handshake before updating any integrations.
func (c *Core) verifyPort(ctx context.Context, port int) error {
	var active struct {
		MixedPort int `json:"mixed-port"`
	}
	if err := c.request(ctx, "GET", "/configs", nil, &active); err != nil {
		return err
	}
	if active.MixedPort != port {
		return fmt.Errorf("内核没有应用网关端口 %d", port)
	}
	d := net.Dialer{Timeout: 2 * time.Second}
	conn, err := d.DialContext(ctx, "tcp4", proxyAddress(port))
	if err != nil {
		return fmt.Errorf("网关端口 %d 未成功监听：%w", port, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err = conn.Write([]byte{5, 1, 0}); err != nil {
		return err
	}
	var reply [2]byte
	if _, err = io.ReadFull(conn, reply[:]); err != nil {
		return err
	}
	if reply != [2]byte{5, 0} {
		return fmt.Errorf("端口 %d 未返回预期的网关握手", port)
	}
	return nil
}

func configuredProxyPort(root string) (int, error) {
	c := struct {
		ProxyPort int `json:"proxyPort"`
	}{17890}
	b, err := os.ReadFile(filepath.Join(root, "config.json"))
	if os.IsNotExist(err) {
		return c.ProxyPort, nil
	}
	if err != nil {
		return 0, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return 0, err
	}
	if c.ProxyPort < 1024 || c.ProxyPort > 65535 {
		return 0, fmt.Errorf("网关端口无效")
	}
	return c.ProxyPort, nil
}
