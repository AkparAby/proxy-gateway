package desktopinstance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestActivationRequiresLocalHostAndSecret(t *testing.T) {
	i := &Instance{token: strings.Repeat("a", 64), Show: make(chan struct{}, 1)}
	for _, tc := range []struct {
		host, token, origin, method string
		code                        int
	}{{Address, i.token, "", "POST", 200}, {"attacker.example", i.token, "", "POST", 403}, {Address, "wrong", "", "POST", 403}, {Address, i.token, "https://attacker.example", "POST", 403}, {Address, i.token, "", "GET", 403}} {
		r := httptest.NewRequest(tc.method, "http://"+tc.host+"/activate", strings.NewReader(`{"show":true}`))
		r.Header.Set("X-Desktop-Token", tc.token)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		i.handle(w, r)
		if w.Code != tc.code {
			t.Fatal(tc, w.Code)
		}
	}
	if len(i.Show) != 1 {
		t.Fatal("activation was not delivered exactly once")
	}
	<-i.Show
	r := httptest.NewRequest(http.MethodPost, "http://"+Address+"/activate", strings.NewReader(`{"show":false}`))
	r.Header.Set("X-Desktop-Token", i.token)
	i.handle(httptest.NewRecorder(), r)
	if len(i.Show) != 0 {
		t.Fatal("background launch opened window")
	}
}
