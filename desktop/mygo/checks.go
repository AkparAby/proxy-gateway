package main

import (
	"context"
	"time"
)

func (m *model) manualCheckActive() bool { return m.checkPending || m.checkID != 0 }

func (m *model) outboundChecking(id string) bool {
	h := m.status.Health[id]
	return m.online && m.manualCheckActive() && (m.checkPending ||
		h.CheckID < m.checkID || h.CheckID == m.checkID && h.Checking)
}

func (m *model) resetCheck() {
	m.checkPending, m.checkID, m.checkToken = false, 0, ""
	m.checkTicket++ // Ignore a late acknowledgement after disconnection or restart.
}

func (m *model) updateCheckState() {
	if m.manualCheckActive() {
		switch {
		case !m.online:
			m.resetCheck()
		case m.status.Token != m.checkToken:
			m.resetCheck()
			m.message("网关已重启，请重新检查网络。", true)
		case time.Now().After(m.checkDeadline):
			m.resetCheck()
			m.message("等待检测结果超时，请稍后重试。", true)
		case !m.checkPending:
			complete := true
			for _, outbound := range m.status.Config.Outbounds {
				complete = complete && !m.outboundChecking(outbound.ID)
			}
			if complete {
				m.resetCheck()
			}
		}
	}
	checking := m.manualCheckActive()
	if m.online {
		for _, h := range m.status.Health {
			checking = checking || h.Checking
		}
	}
	m.fastPoll.Store(checking)
}

func (m *model) checkNetwork() {
	if !m.editable() || m.manualCheckActive() {
		return
	}
	m.checkPending = true
	m.checkToken = m.status.Token
	m.checkDeadline = time.Now().Add(2 * time.Minute)
	m.checkTicket++
	ticket, token := m.checkTicket, m.checkToken
	m.fastPoll.Store(true)
	go func() {
		ctx, cancel := context.WithTimeout(m.ctx, 8*time.Second)
		id, err := m.api.Check(ctx, token)
		cancel()
		m.dispatch(func() {
			if m.ctx.Err() != nil || ticket != m.checkTicket {
				return
			}
			if err != nil {
				m.resetCheck()
				m.message("无法开始网络检查："+err.Error(), true)
			} else {
				m.checkPending, m.checkID = false, id
			}
			m.updateCheckState()
			select {
			case m.pollWake <- struct{}{}:
			default:
			}
		})
	}()
}
