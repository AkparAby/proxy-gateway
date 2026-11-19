package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const dashboardURL = "http://127.0.0.1:17880"

type launchOptions struct {
	root       string
	background bool
}

func parseLaunchOptions(executable string, args []string) (launchOptions, error) {
	options := launchOptions{root: filepath.Dir(executable)}
	flags := flag.NewFlagSet("proxy-gateway", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.background, "background", false, "start without opening the dashboard")
	if err := flags.Parse(args); err != nil {
		return options, err
	}
	if flags.NArg() > 1 {
		return options, errors.New("只接受一个可选的配置目录")
	}
	if flags.NArg() == 1 {
		root, err := filepath.Abs(flags.Arg(0))
		if err != nil {
			return options, err
		}
		options.root = root
	}
	return options, nil
}

func dashboardClient() *http.Client {
	return &http.Client{
		Transport:     &http.Transport{Proxy: nil},
		Timeout:       400 * time.Millisecond,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func dashboardReady(ctx context.Context, client *http.Client) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardURL+"/api/status", nil)
	if err != nil {
		return false
	}
	response, err := client.Do(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var status struct {
		Proxy     string          `json:"proxy"`
		Dashboard string          `json:"dashboard"`
		Config    json.RawMessage `json:"config"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(&status) != nil {
		return false
	}
	return status.Proxy == "127.0.0.1:17890" && status.Dashboard == dashboardURL && len(status.Config) > 0 && string(status.Config) != "null"
}

// Probe readiness instead of sleeping for a fixed time or opening a different
// application's page when the management port is already occupied.
func presentDashboard(ctx context.Context, client *http.Client, timeout time.Duration, background bool, open func() error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if dashboardReady(ctx, client) {
			if background {
				return nil
			}
			return open()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("管理页尚未就绪：%w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Use the user's default HTTP handler without invoking a command shell.
func openDashboard() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ole := syscall.NewLazyDLL("ole32.dll")
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 0x2|0x4)
	if hr == 0 || hr == 1 {
		defer ole.NewProc("CoUninitialize").Call()
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	url, _ := syscall.UTF16PtrFromString(dashboardURL)
	result, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW").Call(
		0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(url)), 0, 0, 1,
	)
	if result <= 32 {
		return fmt.Errorf("无法打开默认浏览器（Windows 错误 %d），请访问 %s", result, dashboardURL)
	}
	return nil
}
