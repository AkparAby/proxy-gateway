//go:build desktop

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"image/png"
	ui "local/proxy-gateway/internal/desktop"
	"local/proxy-gateway/internal/desktopinstance"
	"local/proxy-gateway/internal/gatewayclient"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var version = "dev"
var commit = "unknown"

func main() {
	if err := run(); err != nil {
		log.Print(err)
		if smoke := flag.Lookup("smoke-test"); smoke == nil || smoke.Value.String() == "" {
			showError(err)
		}
		os.Exit(1)
	}
}
func run() error {
	background := flag.Bool("background", false, "start in system tray")
	restore := flag.Bool("restore", false, "restore managed integration settings")
	smoke := flag.String("smoke-test", "", "write this app's canvas and smoke results, then close the desktop")
	flag.Parse()
	if flag.NArg() > 1 {
		return fmt.Errorf("只接受一个可选的配置目录")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	root, manager, err := paths(exe)
	if err != nil {
		return err
	}
	if flag.NArg() == 1 {
		root, err = filepath.Abs(flag.Arg(0))
		if err != nil {
			return err
		}
	}
	if err = os.MkdirAll(filepath.Join(root, "runtime"), 0700); err != nil {
		return err
	}
	lf, err := os.OpenFile(filepath.Join(root, "runtime", "desktop.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { log.SetOutput(os.Stderr); lf.Close() }()
	log.SetOutput(lf)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *restore {
		c := exec.Command(manager, "--restore", root)
		configure(c)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return c.Run()
	}
	instance, err := desktopinstance.Acquire(ctx, *background)
	if err != nil {
		return err
	}
	if instance == nil {
		return nil
	}
	defer instance.Close()
	api := gatewayclient.New()
	a := app.NewWithID("io.github.AkparAby.proxy-gateway.desktop")
	w := ui.New(a, api, ctx)
	w.RestoreOffline = func(ctx context.Context) error {
		if _, err := os.Stat(filepath.Join(root, "runtime", "integrations-state.json")); os.IsNotExist(err) {
			return nil
		}
		cmd := exec.CommandContext(ctx, manager, "--restore", root)
		configure(cmd)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("恢复接入失败：%s (%w)", output, err)
		}
		return nil
	}
	var startup sync.Mutex
	w.OnReconnect = func() {
		go func() {
			startup.Lock()
			defer startup.Unlock()
			if err := ensureGateway(ctx, api, manager, root, lf); err != nil {
				log.Print(err)
				fyne.Do(func() { w.SetNotice(err.Error()); w.Show() })
			}
		}()
	}
	w.OnReconnect()
	w.Start()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-instance.Show:
				fyne.Do(w.Show)
			}
		}
	}()
	if !*background || *smoke != "" {
		w.Show()
	}
	var smokeErr error
	if *smoke != "" {
		go func() {
			select {
			case <-w.Ready:
			case <-time.After(40 * time.Second):
				smokeErr = fmt.Errorf("desktop readiness timeout")
				fyne.Do(a.Quit)
				return
			}
			time.Sleep(2 * time.Second)
			fyne.DoAndWait(func() {
				smokeErr = capture(w, *smoke)
				w.Window.Hide()
				w.Show()
			})
			time.Sleep(2 * time.Second)
			fyne.DoAndWait(func() {
				result := map[string]any{"version": version, "commit": commit, "pid": os.Getpid(), "ready": w.IsReady(), "rendered": smokeErr == nil, "error": ""}
				if smokeErr != nil {
					result["error"] = smokeErr.Error()
				}
				b, _ := json.MarshalIndent(result, "", "  ")
				if err := os.WriteFile(*smoke+".json", b, 0600); err != nil {
					smokeErr = err
				}
				a.Quit()
			})
		}()
	}
	a.Run()
	return smokeErr
}
func ensureGateway(ctx context.Context, api *gatewayclient.Client, manager, root string, lf *os.File) error {
	probe, done := context.WithTimeout(ctx, time.Second)
	_, err := api.Status(probe)
	done()
	if err == nil {
		return nil
	}
	if _, err = os.Stat(manager); err != nil {
		return fmt.Errorf("缺少后台程序，请完整解压运行包：%w", err)
	}
	cmd := exec.Command(manager, "--background", root)
	configure(cmd)
	cmd.Stdout = lf
	cmd.Stderr = lf
	if err = cmd.Start(); err != nil {
		return err
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		probe, done = context.WithTimeout(ctx, time.Second)
		_, err = api.Status(probe)
		done()
		if err == nil {
			return nil
		}
		select {
		case err = <-finished:
			return fmt.Errorf("网关启动失败，请检查 runtime/gateway.log：%v", err)
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("网关启动超时，请检查管理端口 17880 / 17881、配置中的网关端口与运行日志")
}
func capture(w *ui.Window, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, w.Window.Canvas().Capture())
}
