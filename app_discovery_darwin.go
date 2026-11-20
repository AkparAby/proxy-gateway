package main

import (
	"context"
	"os"
	"os/exec"
	"time"
)

func installedApplicationLocations(ctx context.Context) (applicationLocations, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return applicationLocations{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return discoverMacApplications(ctx, home, os.Getenv("PATH"), []string{"/opt/homebrew", "/usr/local"}, macInventoryCommand)
}

func macInventoryCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 200 * time.Millisecond
	return cmd.Output()
}
