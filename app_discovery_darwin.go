package main

import (
	"context"
	"os"
	"path/filepath"
)

func installedApplicationLocations(context.Context) (applicationLocations, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return applicationLocations{}, err
	}
	return applicationLocations{
		CodexDesktop:  []string{"/Applications/Codex.app", filepath.Join(home, "Applications", "Codex.app")},
		ClaudeDesktop: []string{"/Applications/Claude.app", filepath.Join(home, "Applications", "Claude.app")},
	}, nil
}
