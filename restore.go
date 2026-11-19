package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
)

// Serialize restoration through the running manager. An unrelated listener is
// never given the settings operation or treated as a successful restore.
func restoreFromCLI(ctx context.Context, root string) (string, error) {
	l, err := net.Listen("tcp4", "127.0.0.1:17880")
	if err == nil {
		defer l.Close()
		return runIntegration(ctx, root, "restore", false)
	}
	client := dashboardClient()
	defer client.CloseIdleConnections()
	if !dashboardReady(ctx, client) {
		return "", fmt.Errorf("管理端口被其他程序占用，请先释放 17880")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", dashboardURL+"/api/status", nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	var status struct {
		Token string `json:"token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&status)
	resp.Body.Close()
	if err != nil || status.Token == "" {
		return "", fmt.Errorf("无法读取网关状态")
	}
	client.Timeout = 0 // Native authorization is bounded by the caller's context.
	req, _ = http.NewRequestWithContext(ctx, "POST", dashboardURL+"/api/integration", bytes.NewBufferString(`{"name":"restore","enabled":false}`))
	req.Header.Set("X-Gateway-Token", status.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s", result.Error)
	}
	return result.Message, nil
}
