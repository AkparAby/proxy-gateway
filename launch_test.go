package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestLaunchOptions(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "proxy-gateway")
	wantRoot, _ := defaultDataRoot(executable)
	customRoot := t.TempDir()
	options, err := parseLaunchOptions(executable, nil)
	if err != nil || options.background || options.root != wantRoot {
		t.Fatal(options, err)
	}
	options, err = parseLaunchOptions(executable, []string{"--background", customRoot})
	if err != nil || !options.background || options.root != customRoot {
		t.Fatal(options, err)
	}
	options, err = parseLaunchOptions(executable, []string{customRoot})
	if err != nil || options.root != customRoot {
		t.Fatal("legacy root argument broke", err)
	}
	for _, args := range [][]string{{"--unknown"}, {"one", "two"}} {
		if _, err := parseLaunchOptions(executable, args); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
}

func launchTestClient(status int, body string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		w := httptest.NewRecorder()
		w.WriteHeader(status)
		_, _ = w.WriteString(body)
		return w.Result(), nil
	})}
}

func TestDashboardRequiresGatewayIdentity(t *testing.T) {
	for _, body := range []string{`not json`, `{}`, `{"proxy":"other","config":{}}`, `{"proxy":"127.0.0.1:17890","dashboard":"http://127.0.0.1:17880","config":null}`} {
		if dashboardReady(context.Background(), launchTestClient(200, body)) {
			t.Fatalf("non-gateway service accepted: %s", body)
		}
	}
	good := `{"proxy":"127.0.0.1:17890","dashboard":"http://127.0.0.1:17880","config":{}}`
	if dashboardReady(context.Background(), launchTestClient(503, good)) {
		t.Fatal("unready page accepted")
	}
	if !dashboardReady(context.Background(), launchTestClient(200, good)) {
		t.Fatal("gateway not recognized")
	}
}

func TestDashboardOpeningAndBackgroundLaunch(t *testing.T) {
	client := launchTestClient(200, `{"proxy":"127.0.0.1:17890","dashboard":"http://127.0.0.1:17880","config":{}}`)
	for _, background := range []bool{false, true} {
		calls := 0
		err := presentDashboard(context.Background(), client, time.Second, background, func() error { calls++; return nil })
		want := 1
		if background {
			want = 0
		}
		if err != nil || calls != want {
			t.Fatalf("background=%t calls=%d err=%v", background, calls, err)
		}
	}
	calls := 0
	err := presentDashboard(context.Background(), launchTestClient(200, `{}`), 10*time.Millisecond, false, func() error { calls++; return nil })
	if !errors.Is(err, context.DeadlineExceeded) || calls != 0 {
		t.Fatal("opened an unrelated or unready service")
	}
	want := errors.New("browser unavailable")
	if err := presentDashboard(context.Background(), client, time.Second, false, func() error { return want }); !errors.Is(err, want) {
		t.Fatal("browser failure swallowed")
	}
}

func TestDashboardWaitsForStartup(t *testing.T) {
	probes, opens := 0, 0
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		probes++
		w := httptest.NewRecorder()
		if probes < 2 {
			w.WriteHeader(503)
		} else {
			_, _ = w.WriteString(`{"proxy":"127.0.0.1:17890","dashboard":"http://127.0.0.1:17880","config":{}}`)
		}
		return w.Result(), nil
	})}
	if err := presentDashboard(context.Background(), client, time.Second, false, func() error { opens++; return nil }); err != nil || probes != 2 || opens != 1 {
		t.Fatal(probes, opens, err)
	}
}
