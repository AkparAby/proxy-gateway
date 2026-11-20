package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"local/proxy-gateway/internal/routing"
)

func TestAutomaticAccessRollbackPreservesExistingChoices(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows system and terminal integration")
	}
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new system", true: "existing system"}[existing], func(t *testing.T) {
			root := t.TempDir()
			os.Mkdir(filepath.Join(root, "runtime"), 0700)
			atomicJSON(filepath.Join(root, "runtime", "integrations-status.json"), map[string]bool{"system": existing})
			calls := []string{}
			m := &Manager{root: root, applyAppIntegration: func(_ context.Context, name string, enabled bool, port int) error {
				action := "disable "
				if enabled {
					action = "enable "
				}
				calls = append(calls, action+name)
				if enabled && name == "terminal" {
					return errors.New("simulated environment failure")
				}
				return nil
			}}
			c := defaults()
			c.AppRules = []routing.AppRule{{Match: "application", ApplicationID: "codex-desktop", Enabled: true}}
			undo, err := m.prepareApplicationAccess(context.Background(), c)
			if err == nil {
				t.Fatal("failure hidden")
			}
			if err = undo(context.Background()); err != nil {
				t.Fatal(err)
			}
			want := []string{"enable system", "enable terminal", "disable terminal", "disable system"}
			if existing {
				want = []string{"enable terminal", "disable terminal"}
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatal(calls, want)
			}
		})
	}
}

func TestAdvancedRulesDoNotChangeIntegrations(t *testing.T) {
	m := &Manager{root: t.TempDir(), applyAppIntegration: func(context.Context, string, bool, int) error {
		t.Fatal("advanced rules must not auto-enable integrations")
		return nil
	}}
	undo, err := m.prepareApplicationAccess(context.Background(), appConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(context.Background()); err != nil {
		t.Fatal(err)
	}
}
