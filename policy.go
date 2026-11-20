package main

import "time"

type Health struct {
	CheckID         uint64 `json:"checkId"`
	Checking        bool   `json:"checking"`
	SmoothedLatency int    `json:"smoothedLatency"`
	samples         [3]int
	sampleCount     int
	Available       bool      `json:"available"`
	Checked         bool      `json:"checked"`
	LastOK          bool      `json:"lastOK"`
	Failures        int       `json:"failures"`
	Latency         int       `json:"latency"`
	CheckedAt       time.Time `json:"checkedAt"`
	HealthySince    time.Time `json:"healthySince"`
	Detail          string    `json:"detail"`
}

func (h *Health) record(ok bool, latency int, detail string, now time.Time, limit int) {
	h.Checked = true
	h.CheckedAt = now
	h.LastOK = ok
	h.Detail = detail
	h.Latency = latency
	if ok {
		h.samples[h.sampleCount%len(h.samples)] = latency
		h.sampleCount++
		count := min(h.sampleCount, len(h.samples))
		sum := 0
		for _, value := range h.samples[:count] {
			sum += value
		}
		h.SmoothedLatency = sum / count
		if h.HealthySince.IsZero() {
			h.HealthySince = now
		}
		h.Available = true
		h.Failures = 0
	} else {
		h.sampleCount = 0
		h.samples = [3]int{}
		h.SmoothedLatency = 0
		h.HealthySince = time.Time{}
		h.Failures++
		if h.Failures >= limit {
			h.Available = false
		}
	}
}
func choose(c Config, current string, h map[string]Health, now time.Time) (string, string) {
	if c.Mode != "auto" {
		if c.index(c.Mode) >= 0 && h[c.Mode].Available {
			return c.Mode, "手动固定出口"
		}
		return "REJECT", "手动指定的出口不可用"
	}
	if index := c.index(current); index >= 0 && h[current].Available {
		if c.AutoReturn {
			for _, o := range c.Outbounds[:index] {
				candidate := h[o.ID]
				if candidate.Available && !candidate.HealthySince.IsZero() && now.Sub(candidate.HealthySince) >= time.Duration(c.Recovery)*time.Second {
					return o.ID, "更高优先级出口已稳定恢复，自动切回"
				}
			}
		}
		if index == 0 {
			return current, "使用健康的首选出口"
		}
		return current, "继续使用当前备用出口"
	}
	for _, o := range c.Outbounds {
		if h[o.ID].Available {
			return o.ID, "按优先级选择可用出口"
		}
	}
	return "REJECT", "所有出口均不可用，暂停代理请求"
}
