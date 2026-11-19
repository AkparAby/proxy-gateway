package main

import (
	"net/http/httptest"
	"testing"
)

func TestAPIRejectsCrossOriginAndMissingToken(t *testing.T) {
	m := &Manager{token: "test-token"}
	for _, tc := range []struct{ host, origin, token string }{{"evil.example", "", "test-token"}, {"127.0.0.1:17880", "https://evil.example", "test-token"}, {"127.0.0.1:17880", "", ""}} {
		r := httptest.NewRequest("POST", "http://127.0.0.1:17880/api/check", nil)
		r.Host = tc.host
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("X-Gateway-Token", tc.token)
		w := httptest.NewRecorder()
		m.serve(w, r)
		if w.Code != 403 {
			t.Fatalf("accepted unauthorized mutation: %+v status %d", tc, w.Code)
		}
	}
}
func TestCoreConfigNeverHasDirectFallback(t *testing.T) {
	c := coreConfig(defaults(), "secret")
	groups := c["proxy-groups"].([]any)
	choices := groups[0].(map[string]any)["proxies"].([]string)
	if choices[0] != "REJECT" {
		t.Fatal("startup must reject until health check")
	}
	for _, name := range choices {
		if name == "DIRECT" {
			t.Fatal("direct fallback allowed")
		}
	}
	if c["bind-address"] != "127.0.0.1" || c["allow-lan"] != false {
		t.Fatal("proxy unintentionally exposed")
	}
}
