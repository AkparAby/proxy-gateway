// Package gatewayclient is the typed, loopback-only API used by the desktop UI.
package gatewayclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"local/proxy-gateway/internal/routing"
	"net/http"
	"slices"
	"time"
)

const URL = "http://127.0.0.1:17880"

type Outbound struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	Host string `json:"host"`
	Port int    `json:"port"`
}
type Config struct {
	LatencyPolicy routing.LatencyPolicy `json:"latencyPolicy"`
	AppRules      []routing.AppRule     `json:"appRules"`
	SchemaVersion int                   `json:"schemaVersion"`
	ProxyPort     int                   `json:"proxyPort"`
	Outbounds     []Outbound            `json:"outbounds"`
	Mode          string                `json:"mode"`
	AutoReturn    bool                  `json:"autoReturn"`
	Interval      int                   `json:"interval"`
	Timeout       int                   `json:"timeout"`
	Failures      int                   `json:"failures"`
	Recovery      int                   `json:"recovery"`
	Targets       []string              `json:"targets"`
	DirectDomains []string              `json:"directDomains"`
}

func (c Config) Clone() Config {
	c.AppRules = slices.Clone(c.AppRules)
	for i := range c.AppRules {
		c.AppRules[i].ProcessPaths = slices.Clone(c.AppRules[i].ProcessPaths)
	}
	c.Outbounds = slices.Clone(c.Outbounds)
	c.Targets = slices.Clone(c.Targets)
	c.DirectDomains = slices.Clone(c.DirectDomains)
	return c
}
func (c Config) Name(id string) string {
	for _, o := range c.Outbounds {
		if o.ID == id {
			return o.Name
		}
	}
	return "暂无可用出口"
}

type Health struct {
	CheckID         uint64 `json:"checkId"`
	Checking        bool   `json:"checking"`
	SmoothedLatency int    `json:"smoothedLatency"`
	Available       bool   `json:"available"`
	Checked         bool   `json:"checked"`
	LastOK          bool   `json:"lastOK"`
	Latency         int    `json:"latency"`
	Failures        int    `json:"failures"`
	Detail          string `json:"detail"`
}
type Event struct {
	Time    time.Time `json:"time"`
	Message string    `json:"message"`
}
type Status struct {
	Applications         []routing.Application        `json:"applications"`
	ApplicationScanError string                       `json:"applicationScanError"`
	AppStatus            map[string]routing.AppStatus `json:"appStatus"`
	AppStatusError       string                       `json:"appStatusError"`
	Version              string                       `json:"version"`
	Config               Config                       `json:"config"`
	Health               map[string]Health            `json:"health"`
	Selected             string                       `json:"selected"`
	Reason               string                       `json:"reason"`
	Error                string                       `json:"error"`
	Events               []Event                      `json:"events"`
	Token                string                       `json:"token"`
	Proxy                string                       `json:"proxy"`
	Dashboard            string                       `json:"dashboard"`
	Platform             string                       `json:"platform"`
	Integrations         map[string]bool              `json:"integrations"`
	IntegrationOptions   [][3]string                  `json:"integrationOptions"`
}
type Client struct{ HTTP *http.Client }

func New() *Client {
	return &Client{HTTP: &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	s.Config.LatencyPolicy = routing.DefaultLatencyPolicy()
	err := c.request(ctx, "GET", "/api/status", "", nil, &s)
	if s.Config.ProxyPort == 0 {
		s.Config.ProxyPort = 17890
	} // Older managers omit the optional field.
	if err == nil && (s.Dashboard != URL || s.Config.ProxyPort < 1024 || s.Config.ProxyPort > 65535 || s.Proxy != fmt.Sprintf("127.0.0.1:%d", s.Config.ProxyPort) || s.Config.SchemaVersion != 2 || len(s.Config.Outbounds) == 0 || s.Token == "") {
		err = fmt.Errorf("管理端口上的服务不是兼容的 Proxy Gateway")
	}
	return s, err
}
func (c *Client) Post(ctx context.Context, path, token string, body any) (string, error) {
	var result struct {
		Message string `json:"message"`
	}
	err := c.request(ctx, "POST", path, token, body, &result)
	return result.Message, err
}
func (c *Client) Check(ctx context.Context, token string) (uint64, error) {
	var result struct {
		ID uint64 `json:"checkId"`
	}
	err := c.request(ctx, "POST", "/api/check", token, nil, &result)
	if err == nil && result.ID == 0 {
		err = fmt.Errorf("网关后台版本不支持检测进度，请更新后台后重试")
	}
	return result.ID, err
}
func (c *Client) request(ctx context.Context, method, path, token string, body, out any) error {
	var b []byte
	if body != nil {
		var err error
		b, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, URL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Gateway-Token", token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("无法连接本地网关：%w", err)
	}
	defer resp.Body.Close()
	b, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		var message struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &message)
		if message.Error != "" {
			return fmt.Errorf("%s", message.Error)
		}
		return fmt.Errorf("网关返回 HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
