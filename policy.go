package main

import "time"

type Health struct {
	Available    bool      `json:"available"`
	Checked      bool      `json:"checked"`
	LastOK       bool      `json:"lastOK"`
	Failures     int       `json:"failures"`
	Latency      int       `json:"latency"`
	CheckedAt    time.Time `json:"checkedAt"`
	HealthySince time.Time `json:"healthySince"`
	Detail       string    `json:"detail"`
}

func (h *Health) record(ok bool, latency int, detail string, now time.Time, limit int) {
	h.Checked = true
	h.CheckedAt = now
	h.LastOK = ok
	h.Detail = detail
	h.Latency = latency
	if ok {
		if h.HealthySince.IsZero() {
			h.HealthySince = now
		}
		h.Available = true
		h.Failures = 0
	} else {
		h.HealthySince = time.Time{}
		h.Failures++
		if h.Failures >= limit {
			h.Available = false
		}
	}
}
func choose(c Config, current string, h map[string]Health, now time.Time) (string, string) {
	if c.Mode != "auto" {
		if h[c.Mode].Available {
			return c.Mode, "手动固定出口"
		}
		return "REJECT", "手动指定的出口不可用"
	}
	primary := c.Primary
	secondary := "Clash"
	if primary == "Clash" {
		secondary = "SpeedCat"
	}
	if current == secondary && h[secondary].Available {
		p := h[primary]
		if c.AutoReturn && p.Available && !p.HealthySince.IsZero() && now.Sub(p.HealthySince) >= time.Duration(c.Recovery)*time.Second {
			return primary, "主出口已持续恢复，自动切回"
		}
		return secondary, "继续使用备用出口"
	}
	if h[primary].Available {
		return primary, "使用健康的主出口"
	}
	if h[secondary].Available {
		return secondary, "主出口不可用，切换备用"
	}
	return "REJECT", "两个出口均不可用，暂停代理请求"
}
