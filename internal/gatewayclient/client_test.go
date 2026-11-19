package gatewayclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestStatusRejectsUnrelatedService(t *testing.T) {
	c := New()
	c.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"config":{"schemaVersion":2}}`)), Header: make(http.Header)}, nil
	})
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("unrelated listener accepted")
	}
}
func TestAuthenticatedPostAndErrors(t *testing.T) {
	c := New()
	c.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != URL+"/api/config" || r.Header.Get("X-Gateway-Token") != "secret" {
			t.Fatal("wrong request", r.URL)
		}
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(`{"error":"端口无效"}`)), Header: make(http.Header)}, nil
	})
	if _, err := c.Post(context.Background(), "/api/config", "secret", Config{}); err == nil || err.Error() != "端口无效" {
		t.Fatal(err)
	}
}
func TestCloneDoesNotShareEditableSlices(t *testing.T) {
	original := Config{Outbounds: []Outbound{{Name: "old"}}, Targets: []string{"old"}, DirectDomains: []string{"old"}}
	copy := original.Clone()
	copy.Outbounds[0].Name = "new"
	copy.Targets[0] = "new"
	copy.DirectDomains[0] = "new"
	if original.Outbounds[0].Name != "old" || original.Targets[0] != "old" || original.DirectDomains[0] != "old" {
		t.Fatal("draft shares slices")
	}
}

func TestClonePreservesEmptyDomainList(t *testing.T) {
	c := Config{DirectDomains: []string{}}
	if c.Clone().DirectDomains == nil {
		t.Fatal("empty JSON array changed to null, causing false config conflict")
	}
}
