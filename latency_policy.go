package main

import (
	"fmt"
	"time"
)

type latencySelector struct {
	candidate  string
	rounds     int
	lastSwitch time.Time
}

func (s *latencySelector) resetCandidate()        { s.candidate = ""; s.rounds = 0 }
func (s *latencySelector) switched(now time.Time) { s.lastSwitch = now; s.resetCandidate() }

func (s *latencySelector) choose(c Config, current string, health map[string]Health, now time.Time) (string, string) {
	if c.Mode != "auto" || !c.LatencyPolicy.Enabled {
		s.resetCandidate()
		return choose(c, current, health, now)
	}
	fresh := func(h Health) bool {
		age := now.Sub(h.CheckedAt)
		return h.Available && h.Checked && h.LastOK && age >= 0 && age <= time.Duration(c.Interval*1000+c.Timeout+1200)*time.Millisecond
	}
	best := ""
	for _, o := range c.Outbounds {
		h := health[o.ID]
		if fresh(h) && (best == "" || h.SmoothedLatency < health[best].SmoothedLatency || (h.SmoothedLatency == health[best].SmoothedLatency && o.ID == current)) {
			best = o.ID
		}
	}
	active := health[current]
	if c.index(current) < 0 || !active.Available || (active.LastOK && !fresh(active)) {
		s.resetCandidate()
		if best == "" {
			return "REJECT", "自动择优：没有检测有效的健康出口"
		}
		return best, fmt.Sprintf("自动择优：选择健康的最低延迟出口（%dms）", health[best].SmoothedLatency)
	}
	if best == "" || best == current || !fresh(active) || active.SmoothedLatency <= health[best].SmoothedLatency || active.SmoothedLatency-health[best].SmoothedLatency < c.LatencyPolicy.MinImprovement {
		s.resetCandidate()
		return current, "自动择优：保持当前出口，暂无持续更快的候选"
	}
	if s.candidate != best {
		s.candidate = best
		s.rounds = 0
	}
	s.rounds++
	if s.rounds < c.LatencyPolicy.Rounds {
		return current, fmt.Sprintf("自动择优：%s 更快，观察 %d/%d 轮", c.name(best), s.rounds, c.LatencyPolicy.Rounds)
	}
	if now.Sub(s.lastSwitch) < time.Duration(c.LatencyPolicy.Cooldown)*time.Second {
		return current, "自动择优：候选已稳定，等待切换冷却结束"
	}
	return best, fmt.Sprintf("自动择优：%s 连续 %d 轮更快，%dms → %dms", c.name(best), s.rounds, active.SmoothedLatency, health[best].SmoothedLatency)
}
