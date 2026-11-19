package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const launchAgentName = "io.github.AkparAby.proxy-gateway"
const networkSetup = "/usr/sbin/networksetup"

type macProxy struct {
	Enabled bool   `json:"enabled"`
	Server  string `json:"server"`
	Port    int    `json:"port"`
}
type macService struct {
	Name      string   `json:"name"`
	HTTP      macProxy `json:"http"`
	HTTPS     macProxy `json:"https"`
	SOCKS     macProxy `json:"socks"`
	PAC       bool     `json:"pac"`
	Discovery bool     `json:"discovery"`
}
type macIntegrationState struct {
	Enabled       map[string]bool `json:"enabled"`
	Services      []macService    `json:"services,omitempty"`
	GitProxy      []string        `json:"gitProxy,omitempty"`
	AgentOriginal []byte          `json:"agentOriginal,omitempty"`
	AgentExisted  bool            `json:"agentExisted"`
}
type macIntegrator struct {
	root, home, executable string
	command                func(context.Context, string, ...string) (string, error)
	admin                  func(context.Context, string) error
}

func macCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	b, err := cmd.CombinedOutput()
	if err != nil {
		return string(b), fmt.Errorf("%s: %s: %w", filepath.Base(name), strings.TrimSpace(string(b)), err)
	}
	return string(b), nil
}

func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func privilegedNetworkSetup(ctx context.Context, script string) error {
	_, err := macCommand(ctx, "/usr/bin/osascript", "-e", "do shell script "+appleString(script)+" with administrator privileges")
	return err
}

func runIntegration(ctx context.Context, root, name string, enabled bool) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	m := macIntegrator{root, home, executable, macCommand, privilegedNetworkSetup}
	return m.run(ctx, name, enabled)
}

func (m macIntegrator) statePath() string {
	return filepath.Join(m.root, "runtime", "integrations-state.json")
}
func (m macIntegrator) save(s *macIntegrationState) error {
	if err := os.MkdirAll(filepath.Dir(m.statePath()), 0700); err != nil {
		return err
	}
	if err := atomicJSON(m.statePath(), s); err != nil {
		return err
	}
	return atomicJSON(filepath.Join(m.root, "runtime", "integrations-status.json"), s.Enabled)
}
func (m macIntegrator) run(ctx context.Context, name string, enabled bool) (string, error) {
	if name != "system" && name != "git" && name != "autostart" && name != "restore" && name != "status" {
		return "", fmt.Errorf("macOS 暂不支持此接入项：%s；可在应用中手动设置 127.0.0.1:17890", name)
	}
	s := macIntegrationState{Enabled: map[string]bool{}}
	b, err := os.ReadFile(m.statePath())
	if err == nil {
		if err = json.Unmarshal(b, &s); err != nil {
			return "", fmt.Errorf("恢复记录无法读取，请保留原文件：%w", err)
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	if s.Enabled == nil {
		s.Enabled = map[string]bool{}
	}
	if name == "status" {
		return "接入状态已更新", m.save(&s)
	}
	names := []string{name}
	if name == "restore" {
		names = []string{"system", "git", "autostart"}
		enabled = false
	}
	for _, item := range names {
		if s.Enabled[item] == enabled {
			continue
		}
		if enabled {
			if err := m.capture(ctx, item, &s); err != nil {
				return "", err
			}
			// Persist originals before any external mutation. A partial failure
			// remains restorable, including after a crash or canceled prompt.
			s.Enabled[item] = true
			if err := m.save(&s); err != nil {
				return "", err
			}
		}
		if err := m.apply(ctx, item, enabled, &s); err != nil {
			return "", fmt.Errorf("%s 操作未完成，恢复记录已保留；请点击“停用并恢复”后重试：%w", item, err)
		}
		s.Enabled[item] = enabled
		if err := m.save(&s); err != nil {
			return "", err
		}
	}
	return "接入设置已更新；登录自启动在下次登录时生效", nil
}

func (m macIntegrator) agentPath() string {
	return filepath.Join(m.home, "Library", "LaunchAgents", launchAgentName+".plist")
}
func (m macIntegrator) capture(ctx context.Context, name string, s *macIntegrationState) error {
	switch name {
	case "system":
		services, err := m.snapshotServices(ctx)
		if err != nil {
			return err
		}
		s.Services = services
	case "git":
		out, err := m.command(ctx, "/usr/bin/git", "config", "--global", "--get-all", "http.proxy")
		var exit *exec.ExitError
		if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 1) {
			return err
		}
		s.GitProxy = nil
		if err == nil {
			s.GitProxy = strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		}
	case "autostart":
		if strings.Contains(m.executable, "/AppTranslocation/") || strings.HasPrefix(m.executable, "/Volumes/") {
			return fmt.Errorf("请先将 Proxy Gateway.app 移入应用程序文件夹，重新打开后再启用自启动")
		}
		b, err := os.ReadFile(m.agentPath())
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		s.AgentOriginal, s.AgentExisted = b, err == nil
	}
	return nil
}

func (m macIntegrator) apply(ctx context.Context, name string, enabled bool, s *macIntegrationState) error {
	switch name {
	case "system":
		return m.admin(ctx, networkScript(s.Services, enabled))
	case "git":
		if enabled {
			_, err := m.command(ctx, "/usr/bin/git", "config", "--global", "--replace-all", "http.proxy", "http://127.0.0.1:17890")
			return err
		}
		_, err := m.command(ctx, "/usr/bin/git", "config", "--global", "--unset-all", "http.proxy")
		var exit *exec.ExitError
		if err != nil && !(errors.As(err, &exit) && exit.ExitCode() == 5) {
			return err
		}
		for _, value := range s.GitProxy {
			if _, err := m.command(ctx, "/usr/bin/git", "config", "--global", "--add", "http.proxy", value); err != nil {
				return err
			}
		}
	case "autostart":
		if enabled {
			if err := os.MkdirAll(filepath.Dir(m.agentPath()), 0700); err != nil {
				return err
			}
			return atomicFile(m.agentPath(), []byte(agentPlist(m.executable, m.root)), 0600)
		}
		if s.AgentExisted {
			return atomicFile(m.agentPath(), s.AgentOriginal, 0600)
		}
		if err := os.Remove(m.agentPath()); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func atomicFile(path string, b []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".gateway-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func agentPlist(executable, root string) string {
	escape := func(s string) string { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>` + launchAgentName + `</string>
<key>ProgramArguments</key><array><string>` + escape(executable) + `</string><string>--background</string><string>` + escape(root) + `</string></array>
<key>RunAtLoad</key><true/>
<key>KeepAlive</key><false/>
<key>StandardErrorPath</key><string>` + escape(filepath.Join(root, "runtime", "launchd.log")) + `</string>
</dict></plist>
`
}

func parseFields(text string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(line, ":"); ok {
			fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return fields
}
func enabledField(text string) (bool, error) {
	switch parseFields(text)["Enabled"] {
	case "Yes":
		return true, nil
	case "No":
		return false, nil
	default:
		return false, fmt.Errorf("无法识别 networksetup 的启用状态：%s", strings.TrimSpace(text))
	}
}
func parseProxy(text string) (macProxy, error) {
	fields := parseFields(text)
	enabled, err := enabledField(text)
	if err != nil {
		return macProxy{}, err
	}
	if fields["Authenticated Proxy Enabled"] != "0" {
		return macProxy{}, fmt.Errorf("检测到带认证的代理或未知认证状态；请在系统设置中手动接入，以保留原凭据")
	}
	port, err := strconv.Atoi(fields["Port"])
	if err != nil || port < 0 || port > 65535 {
		return macProxy{}, fmt.Errorf("无法识别代理端口")
	}
	if enabled && (port == 0 || fields["Server"] == "") {
		return macProxy{}, fmt.Errorf("现有系统代理配置不完整")
	}
	return macProxy{enabled, fields["Server"], port}, nil
}

func (m macIntegrator) snapshotServices(ctx context.Context) ([]macService, error) {
	out, err := m.command(ctx, networkSetup, "-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "asterisk") {
		return nil, fmt.Errorf("无法识别 macOS 网络服务列表")
	}
	var services []macService
	for _, line := range lines[1:] {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "*") {
			continue
		}
		s := macService{Name: name}
		for i, option := range []string{"-getwebproxy", "-getsecurewebproxy", "-getsocksfirewallproxy"} {
			out, err := m.command(ctx, networkSetup, option, name)
			if err != nil {
				return nil, err
			}
			p, err := parseProxy(out)
			if err != nil {
				return nil, fmt.Errorf("%s：%w", name, err)
			}
			*[]*macProxy{&s.HTTP, &s.HTTPS, &s.SOCKS}[i] = p
		}
		out, err = m.command(ctx, networkSetup, "-getautoproxyurl", name)
		if err != nil {
			return nil, err
		}
		if s.PAC, err = enabledField(out); err != nil {
			return nil, err
		}
		out, err = m.command(ctx, networkSetup, "-getproxyautodiscovery", name)
		if err != nil {
			return nil, err
		}
		switch strings.TrimSpace(out) {
		case "Auto Proxy Discovery: On":
			s.Discovery = true
		case "Auto Proxy Discovery: Off":
			s.Discovery = false
		default:
			return nil, fmt.Errorf("无法识别代理自动发现状态：%s", out)
		}
		services = append(services, s)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("没有可配置的网络服务")
	}
	return services, nil
}

func networkScript(services []macService, enabled bool) string {
	var b strings.Builder
	b.WriteString("set -eu\napply() {\n  output=$(/usr/sbin/networksetup \"$@\" 2>&1) || { printf '%s\\n' \"$output\"; return 1; }\n  case \"$output\" in *Error*|*error*|*'not a recognized'*) printf '%s\\n' \"$output\"; return 1;; esac\n}\n")
	write := func(args ...string) {
		b.WriteString("apply")
		for _, arg := range args {
			b.WriteByte(' ')
			b.WriteString(shellQuote(arg))
		}
		b.WriteByte('\n')
	}
	onOff := func(on bool) string {
		if on {
			return "on"
		}
		return "off"
	}
	for _, s := range services {
		for i, kind := range []string{"webproxy", "securewebproxy", "socksfirewallproxy"} {
			p := []macProxy{s.HTTP, s.HTTPS, s.SOCKS}[i]
			if enabled {
				p = macProxy{true, "127.0.0.1", 17890}
			}
			// Empty original endpoints only require disabling the proxy. No
			// credentials, bypass domains or PAC URL are ever overwritten.
			if p.Server != "" && p.Port > 0 {
				write("-set"+kind, s.Name, p.Server, strconv.Itoa(p.Port), "off")
			}
			write("-set"+kind+"state", s.Name, onOff(p.Enabled))
		}
		write("-setautoproxystate", s.Name, onOff(!enabled && s.PAC))
		write("-setproxyautodiscovery", s.Name, onOff(!enabled && s.Discovery))
	}
	return b.String()
}
