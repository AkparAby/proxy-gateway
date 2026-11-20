package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"local/proxy-gateway/internal/desktopinstance"
	client "local/proxy-gateway/internal/gatewayclient"
)

var version = "dev-mygo"
var commit = "unknown"

//go:embed icon.png
var icon []byte

func main() {
	if err := run(); err != nil {
		log.Print(err)
		if smoke := flag.Lookup("ui-smoke"); smoke != nil && smoke.Value.String() != "" {
			_ = os.WriteFile(filepath.Join(smoke.Value.String(), "native-result.txt"), []byte("FAIL: "+err.Error()), 0600)
		} else {
			showError(err)
		}
		os.Exit(1)
	}
}

func run() error {
	background := flag.Bool("background", false, "start in system tray")
	restore := flag.Bool("restore", false, "restore managed integration settings")
	smoke := flag.String("ui-smoke", "", "render fixture pages without starting or changing the gateway")
	preview := flag.Bool("ui-preview", false, "interactive fixture UI without starting or changing the gateway")
	flag.Parse()
	if flag.NArg() > 1 {
		return fmt.Errorf("只接受一个可选的配置目录")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if *preview {
		return runSmoke(ctx, "")
	}
	if *smoke != "" {
		return runSmoke(ctx, *smoke)
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
	lf, err := os.OpenFile(filepath.Join(root, "runtime", "desktop-mygo.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer lf.Close()
	log.SetOutput(lf)
	defer log.SetOutput(os.Stderr)
	restoreLocal := func(ctx context.Context) error {
		cmd := exec.CommandContext(ctx, manager, "--restore", root)
		configure(cmd)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("恢复接入失败：%s (%w)", output, err)
		}
		return nil
	}
	if *restore {
		return restoreLocal(ctx)
	}
	instance, err := desktopinstance.Acquire(ctx, *background)
	if err != nil {
		return err
	}
	if instance == nil {
		return nil
	}
	defer instance.Close()
	api := client.New()
	m := newModel(ctx, api)
	m.ensure = func(ctx context.Context) error { return ensureGateway(ctx, api, manager, root, lf) }
	m.restore = func(ctx context.Context) error {
		if _, err := os.Stat(filepath.Join(root, "runtime", "integrations-state.json")); os.IsNotExist(err) {
			return nil
		}
		return restoreLocal(ctx)
	}
	mygo.App.SetName("Proxy Gateway")
	var win *mygo.Window
	quitting := false
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if quitting || e.SessionEnding {
			return
		}
		e.PreventDefault()
		if win != nil {
			win.Show()
			win.Focus()
			win.Update(func() {
				if !m.busy && !m.starting {
					m.exitOpen = true
				}
			})
		}
	})
	mygo.App.WhenReady(func() {
		win = mygo.NewWindow(mygo.WindowOptions{Title: "Proxy Gateway · 本地代理网关", Width: 1160, Height: 820, MinWidth: 900, MinHeight: 660, Hidden: *background, Content: ui.View(m.view)})
		// Windows does not pick up our EXE resource as the window-class icon.
		if err := win.SetIcon(icon); err != nil {
			log.Printf("设置窗口图标失败：%v", err)
		}
		m.dispatch = win.Update
		mygo.App.OnActivate(func(visible bool) {
			if !visible {
				win.Show()
				win.Focus()
			}
		})
		m.quit = func() { quitting = true; cancel(); mygo.App.Quit() }
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				win.Hide()
			}
		})
		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: "打开管理窗口", Click: func(*mygo.MenuItem, *mygo.Window) { win.Show(); win.Focus() }},
			{Label: "立即检查网络", Click: func(*mygo.MenuItem, *mygo.Window) {
				win.Update(m.checkNetwork)
			}},
			mygo.Separator(),
			{Label: "恢复接入并退出", Click: func(*mygo.MenuItem, *mygo.Window) { win.Show(); win.Focus(); win.Update(func() { m.exitOpen = true }) }},
		})
		tray, err := mygo.NewTray(mygo.TrayOptions{ID: "proxy-gateway", Icon: icon, ToolTip: "Proxy Gateway", Menu: menu})
		if err != nil {
			m.message("托盘不可用，关闭窗口将提示恢复并退出："+err.Error(), true)
			win.Show()
			win.OnClose(func(e *mygo.CloseEvent) { e.PreventDefault(); win.Show(); win.Update(func() { m.exitOpen = true }) })
		} else {
			tray.OnClick(func() { win.Show(); win.Focus() })
			last := ""
			m.onStatus = func() {
				text := "Proxy Gateway · 未连接"
				if m.online {
					text = "Proxy Gateway · " + m.status.Config.Name(m.status.Selected)
				}
				if last != text {
					tray.SetToolTip(text)
					last = text
				}
			}
		}
		m.startGateway()
		m.poll()
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-instance.Show:
					win.Show()
					win.Focus()
				}
			}
		}()
	})
	return mygo.App.Run()
}

func ensureGateway(ctx context.Context, api gatewayAPI, manager, root string, lf *os.File) error {
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
	cmd.Stdout, cmd.Stderr = lf, lf
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
	return fmt.Errorf("网关启动超时，请检查管理端口和 runtime/gateway.log")
}
