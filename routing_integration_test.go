package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"local/proxy-gateway/internal/routing"
)

// Run with GATEWAY_TEST_MIHOMO pointing to the pinned kernel. All listeners and
// upstreams are isolated loopback fixtures; this never changes system proxy or
// talks to the user's running manager, apps, or real proxy clients.
func TestMihomoFixedRoutingAndSelection(t *testing.T) {
	executable := os.Getenv("GATEWAY_TEST_MIHOMO")
	if executable == "" {
		t.Skip("set GATEWAY_TEST_MIHOMO to exercise the real kernel")
	}
	executable, err := filepath.Abs(executable)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	proxy := func(label byte) (int, func()) {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		connections := []net.Conn{}
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				mu.Lock()
				connections = append(connections, conn)
				mu.Unlock()
				go func() {
					defer conn.Close()
					reader := bufio.NewReader(conn)
					req, err := http.ReadRequest(reader)
					if err != nil {
						return
					}
					if req.Method != "CONNECT" {
						return
					}
					fmt.Fprint(conn, "HTTP/1.1 200 Connection Established\r\n\r\n")
					var b [1]byte
					for {
						if _, err := io.ReadFull(reader, b[:]); err != nil {
							return
						}
						if _, err := conn.Write([]byte{label}); err != nil {
							return
						}
					}
				}()
			}
		}()
		t.Cleanup(func() {
			listener.Close()
			mu.Lock()
			defer mu.Unlock()
			for _, conn := range connections {
				conn.Close()
			}
		})
		return listener.Addr().(*net.TCPAddr).Port, func() { listener.Close() }
	}
	portA, stopA := proxy('A')
	portB, _ := proxy('B')
	c := defaults()
	c.ProxyPort = freeProxyPort(t)
	appPort := freeProxyPort(t)
	controller := proxyAddress(freeProxyPort(t))
	c.Outbounds[0].Port = portA
	c.Outbounds[1].Port = portB
	c.AppRules = []routing.AppRule{{ID: "pinned", Name: "Pinned fixture", Enabled: true, OutboundID: "proxy-1", Match: "port", ListenPort: appPort}}
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	core := &Core{executable: executable, dir: dir, secret: "isolated-test", client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		clone.URL.Host = controller
		return transport.RoundTrip(clone)
	})}}
	writeConfig := func() {
		cfg := coreConfig(c, core.secret)
		cfg["external-controller"] = controller
		if err := atomicJSON(filepath.Join(dir, "config.json"), cfg); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig()
	if err := core.start(ctx); err != nil {
		data, _ := os.ReadFile(filepath.Join(dir, "mihomo.log"))
		t.Fatalf("%v\n%s", err, data)
	}
	defer core.stop()
	defer func() {
		if t.Failed() {
			data, _ := os.ReadFile(filepath.Join(dir, "mihomo.log"))
			t.Log(string(data))
		}
	}()
	if err := core.verifyAppPorts(ctx, c); err != nil {
		t.Fatal(err)
	}
	selectExit := func(id string) {
		t.Helper()
		if err := core.selectProxy(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	dial := func(port int, socks bool) net.Conn {
		t.Helper()
		conn, err := net.DialTimeout("tcp4", proxyAddress(port), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		conn.SetDeadline(time.Now().Add(4 * time.Second))
		if socks {
			conn.Write([]byte{5, 1, 0})
			var hello [2]byte
			if _, err := io.ReadFull(conn, hello[:]); err != nil || hello != [2]byte{5, 0} {
				t.Fatal("SOCKS greeting", hello, err)
			}
			conn.Write([]byte{5, 1, 0, 1, 198, 51, 100, 1, 1, 187})
			var reply [10]byte
			if _, err := io.ReadFull(conn, reply[:]); err != nil || reply[1] != 0 {
				t.Fatal("SOCKS CONNECT", reply, err)
			}
		} else {
			fmt.Fprint(conn, "CONNECT 198.51.100.1:443 HTTP/1.1\r\nHost: 198.51.100.1:443\r\n\r\n")
			resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
			if err != nil || resp.StatusCode != 200 {
				t.Fatal("CONNECT", resp, err)
			}
		}
		return conn
	}
	readLabel := func(conn net.Conn) (byte, error) {
		conn.SetDeadline(time.Now().Add(4 * time.Second))
		if _, err := conn.Write([]byte{'?'}); err != nil {
			return 0, err
		}
		var b [1]byte
		_, err := io.ReadFull(conn, b[:])
		return b[0], err
	}
	expect := func(conn net.Conn, want byte) {
		t.Helper()
		got, err := readLabel(conn)
		if err != nil || got != want {
			t.Fatalf("route got %q (%v), want %q", got, err, want)
		}
	}
	selectExit("proxy-1")
	existing := dial(c.ProxyPort, false)
	expect(existing, 'A')
	selectExit("proxy-2")
	expect(dial(c.ProxyPort, false), 'B')
	expect(dial(appPort, false), 'A')
	expect(dial(appPort, true), 'A')
	expect(existing, 'A')
	selectExit("REJECT")
	expect(dial(appPort, false), 'A')
	observed, err := core.appConnections(ctx, c)
	if err != nil || observed["pinned"].ActiveConnections < 1 {
		t.Fatal("missing dedicated route observation", observed, err)
	}
	if len(observed["pinned"].ActualOutbounds) != 1 || observed["pinned"].ActualOutbounds[0] != c.Outbounds[0].Name {
		t.Fatal("wrong observed exit", observed)
	}
	// Turning off a rule preserves its address and follows the default policy.
	c.AppRules[0].Enabled = false
	writeConfig()
	if err := core.reload(ctx); err != nil {
		t.Fatal(err)
	}
	selectExit("proxy-2")
	expect(dial(appPort, false), 'B')
	// Match the real test process, while the default remains B.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c.AppRules[0].Enabled = true
	c.AppRules = append(c.AppRules, routing.AppRule{ID: "process", Name: "Application fixture", Enabled: true, OutboundID: "proxy-1", Match: "application", ApplicationID: "codex-cli", ProcessPaths: []string{self}})
	writeConfig()
	if err := core.reload(ctx); err != nil {
		t.Fatal(err)
	}
	selectExit("proxy-2")
	expect(dial(c.ProxyPort, false), 'A')
	observed, err = core.appConnections(ctx, c)
	if err != nil || observed["process"].ActiveConnections < 1 {
		t.Fatal("missing process route observation", observed, err)
	}
	// A pinned upstream failure must not fall through to the healthy B route.
	stopA()
	blocked := dial(appPort, false)
	if got, err := readLabel(blocked); err == nil {
		t.Fatalf("fixed route did not fail closed: %q", got)
	}
	blocked = dial(c.ProxyPort, false)
	if got, err := readLabel(blocked); err == nil {
		t.Fatalf("fixed process did not fail closed: %q", got)
	}
}
