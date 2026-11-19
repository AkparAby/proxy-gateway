package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func freeProxyPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

func TestProxyPortValidationAndLegacyDefault(t *testing.T) {
	for _, port := range []int{0, 80, 1023, 65536, 17880, 17881, 17882, 7892, 17891} {
		c := defaults()
		c.ProxyPort = port
		if c.validate() == nil {
			t.Fatalf("accepted conflicting/invalid port %d", port)
		}
	}
	c := defaults()
	c.ProxyPort = 18890
	c.Outbounds[0].Port = 17890
	if err := c.validate(); err != nil {
		t.Fatal(err)
	}
	if coreConfig(c, "test")["mixed-port"] != 18890 {
		t.Fatal("core port ignored")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	var old map[string]any
	b, _ := json.Marshal(defaults())
	_ = json.Unmarshal(b, &old)
	delete(old, "proxyPort")
	_ = atomicJSON(path, old)
	loaded, err := readConfig(path)
	if err != nil || loaded.ProxyPort != 17890 {
		t.Fatal("legacy port lost", loaded, err)
	}
}

func TestOccupiedProxyPortRejectedBeforeMutation(t *testing.T) {
	for _, kind := range []string{"tcp4", "udp4"} {
		t.Run(kind, func(t *testing.T) {
			var port int
			if kind == "tcp4" {
				l, err := net.Listen(kind, "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				port = l.Addr().(*net.TCPAddr).Port
			} else {
				l, err := net.ListenPacket(kind, "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				port = l.LocalAddr().(*net.UDPAddr).Port
			}
			old := defaults()
			c := old
			c.ProxyPort = port
			m := &Manager{config: old} // A premature core/integration mutation would panic.
			if err := m.saveConfig(context.Background(), c); err == nil {
				t.Fatal("occupied port accepted")
			}
			if !reflect.DeepEqual(m.config, old) {
				t.Fatal("configuration mutated")
			}
		})
	}
}

func TestPortChangeCommitAndRollback(t *testing.T) {
	for _, failure := range []string{"", "bind", "integration", "persist"} {
		t.Run(failure, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "runtime")
			_ = os.Mkdir(dir, 0700)
			old := defaults()
			old.ProxyPort = freeProxyPort(t)
			wanted := old
			wanted.ProxyPort = freeProxyPort(t)
			for wanted.ProxyPort == old.ProxyPort {
				wanted.ProxyPort = freeProxyPort(t)
			}
			if failure == "persist" {
				_ = os.Mkdir(filepath.Join(root, "config.json"), 0700)
			} else {
				_ = atomicJSON(filepath.Join(root, "config.json"), old)
			}
			var listener net.Listener
			defer func() {
				if listener != nil {
					listener.Close()
				}
			}()
			active := old.ProxyPort
			selected := "upstream-proxy-1"
			core := &Core{dir: dir, secret: "test"}
			core.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				w := httptest.NewRecorder()
				switch {
				case r.Method == "GET" && r.URL.Path == "/configs":
					fmt.Fprintf(w, `{"mixed-port":%d}`, active)
				case r.Method == "PUT" && r.URL.Path == "/configs":
					var cfg struct {
						Port int `json:"mixed-port"`
					}
					b, _ := os.ReadFile(filepath.Join(dir, "config.json"))
					_ = json.Unmarshal(b, &cfg)
					active = cfg.Port
					if listener != nil {
						listener.Close()
						listener = nil
					}
					if !(failure == "bind" && active == wanted.ProxyPort) {
						var err error
						listener, err = net.Listen("tcp4", proxyAddress(active))
						if err != nil {
							t.Fatal(err)
						}
						l := listener
						go func() {
							for {
								conn, err := l.Accept()
								if err != nil {
									return
								}
								go func() {
									defer conn.Close()
									var hello [3]byte
									_, _ = io.ReadFull(conn, hello[:])
									_, _ = conn.Write([]byte{5, 0})
								}()
							}
						}()
					}
					w.WriteHeader(204)
				case r.URL.Path == "/proxies/Gateway":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					selected = body["name"]
					w.WriteHeader(204)
				default:
					t.Fatalf("unexpected request %s", r.URL)
				}
				return w.Result(), nil
			})}
			var rebound []int
			m := &Manager{root: root, config: old, core: core, selected: "proxy-1", health: old.initialHealth(), wake: make(chan struct{}, 1), rebind: func(ctx context.Context, _ string, port int) error {
				if ctx.Err() != nil {
					t.Fatal("rollback used expired context")
				}
				rebound = append(rebound, port)
				if failure == "integration" && port == wanted.ProxyPort {
					return errors.New("injected integration failure")
				}
				return nil
			}}
			err := m.saveConfig(context.Background(), wanted)
			if failure == "" {
				if err != nil || active != wanted.ProxyPort || m.config.ProxyPort != wanted.ProxyPort || m.status()["proxy"] != proxyAddress(wanted.ProxyPort) || selected != "REJECT" {
					t.Fatal("save failed", err, active, m.config.ProxyPort, selected)
				}
				saved, e := readConfig(filepath.Join(root, "config.json"))
				if e != nil || saved.ProxyPort != wanted.ProxyPort {
					t.Fatal("not persisted", e)
				}
				if !reflect.DeepEqual(rebound, []int{wanted.ProxyPort}) {
					t.Fatal(rebound)
				}
			} else {
				if err == nil || active != old.ProxyPort || m.config.ProxyPort != old.ProxyPort || selected != "upstream-proxy-1" {
					t.Fatal("rollback failed", err, active, m.config.ProxyPort, selected)
				}
				if failure != "bind" && !reflect.DeepEqual(rebound, []int{wanted.ProxyPort, old.ProxyPort}) {
					t.Fatal("integration rollback missing", rebound)
				}
				if failure != "persist" {
					saved, e := readConfig(filepath.Join(root, "config.json"))
					if e != nil || saved.ProxyPort != old.ProxyPort {
						t.Fatal("old config replaced", e)
					}
				}
			}
		})
	}
}
