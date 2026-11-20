// Package routing contains configuration shared by the manager and desktop UI.
package routing

type LatencyPolicy struct {
	Enabled        bool `json:"enabled"`
	Rounds         int  `json:"rounds"`
	MinImprovement int  `json:"minImprovement"`
	Cooldown       int  `json:"cooldown"`
}

func DefaultLatencyPolicy() LatencyPolicy {
	return LatencyPolicy{Rounds: 3, MinImprovement: 20, Cooldown: 30}
}

// AppRule binds either exact executable paths or a dedicated loopback listener.
// Disabled rules follow the default policy; dedicated listener addresses persist.
type AppRule struct {
	ApplicationID string   `json:"applicationId,omitempty"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Enabled       bool     `json:"enabled"`
	OutboundID    string   `json:"outboundId"`
	Match         string   `json:"match"`
	ProcessPaths  []string `json:"processPaths"`
	ListenPort    int      `json:"listenPort"`
}

// Application is a discovered, supported app, not a user-supplied executable.
type Application struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Available    bool     `json:"available"`
	Running      bool     `json:"running"`
	Detail       string   `json:"detail"`
	ProcessPaths []string `json:"processPaths"`
}

func KnownApplication(id string) bool {
	switch id {
	case "codex-desktop", "codex-cli", "claude-desktop", "claude-cli":
		return true
	}
	return false
}

type AppStatus struct {
	ActiveConnections int      `json:"activeConnections"`
	ActualOutbounds   []string `json:"actualOutbounds"`
}
