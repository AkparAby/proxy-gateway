package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"local/proxy-gateway/internal/routing"
)

func applicationIntegrations(c Config) []string {
	required := []string{}
	for _, rule := range c.AppRules {
		if !rule.Enabled || rule.Match != "application" {
			continue
		}
		if rule.ApplicationID == "codex-desktop" || rule.ApplicationID == "claude-desktop" {
			if !slices.Contains(required, "system") {
				required = append(required, "system")
			}
		}
		// Native CLI processes and desktop network helpers consume environment
		// variables on Windows. Never bind a shared node.exe by its process name.
		if runtime.GOOS == "windows" && !slices.Contains(required, "terminal") {
			required = append(required, "terminal")
		}
	}
	return required
}

func (m *Manager) refreshApplications(ctx context.Context) ([]routing.Application, error) {
	scan := m.discoverApps
	if scan == nil {
		scan = discoverApplications
	}
	apps, err := scan(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applicationScanError = ""
	if len(apps) > 0 {
		m.applications = apps
	}
	if err != nil {
		m.applicationScanError = err.Error()
		return apps, err
	}
	m.applications = apps
	return apps, nil
}

func (m *Manager) appIntegration(ctx context.Context, name string, enabled bool, port int) error {
	if m.applyAppIntegration != nil {
		return m.applyAppIntegration(ctx, name, enabled, port)
	}
	_, err := runIntegrationAtPort(ctx, m.root, name, enabled, port)
	return err
}

// Return an undo function even on partial failure. Only integrations newly
// enabled by this transaction are restored; existing user choices are retained.
func (m *Manager) prepareApplicationAccess(ctx context.Context, c Config) (func(context.Context) error, error) {
	changed := []string{}
	undo := func(ctx context.Context) error {
		failures := []error{}
		for i := len(changed) - 1; i >= 0; i-- {
			if err := m.appIntegration(ctx, changed[i], false, c.ProxyPort); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}
	required := applicationIntegrations(c)
	if len(required) == 0 {
		return undo, nil
	}
	state := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(m.root, "runtime", "integrations-status.json"))
	if err != nil && !os.IsNotExist(err) {
		return undo, err
	}
	if err == nil {
		if err = json.Unmarshal(data, &state); err != nil {
			return undo, fmt.Errorf("无法读取接入恢复状态：%w", err)
		}
	}
	for _, name := range required {
		if state[name] {
			continue
		}
		changed = append(changed, name)
		if err := m.appIntegration(ctx, name, true, c.ProxyPort); err != nil {
			return undo, fmt.Errorf("应用自动接入失败：%w", err)
		}
	}
	return undo, nil
}

func (m *Manager) enableSavedApplicationAccess(ctx context.Context, c Config) error {
	undo, err := m.prepareApplicationAccess(ctx, c)
	if err == nil {
		return nil
	}
	restore, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if rollback := undo(restore); rollback != nil {
		return fmt.Errorf("%w；恢复接入失败：%v", err, rollback)
	}
	return err
}
