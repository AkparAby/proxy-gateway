package main

import (
	"testing"
	"time"
)

func TestFailoverHysteresisAndRecovery(t *testing.T) {
	c := defaults()
	now := time.Now()
	h := map[string]Health{"proxy-1": {}, "proxy-2": {}}
	record := func(name string, ok bool, at time.Time) {
		v := h[name]
		v.record(ok, 50, "test", at, c.Failures)
		h[name] = v
	}
	record("proxy-1", true, now)
	record("proxy-2", true, now)
	if got, _ := choose(c, "REJECT", h, now); got != "proxy-1" {
		t.Fatal(got)
	}
	for i := 0; i < 2; i++ {
		record("proxy-1", false, now)
		if got, _ := choose(c, "proxy-1", h, now); got != "proxy-1" {
			t.Fatal("isolated failures must not switch", got)
		}
	}
	record("proxy-1", false, now)
	if got, _ := choose(c, "proxy-1", h, now); got != "proxy-2" {
		t.Fatal(got)
	}
	record("proxy-1", true, now)
	if got, _ := choose(c, "proxy-2", h, now.Add(59*time.Second)); got != "proxy-2" {
		t.Fatal("returned too early")
	}
	if got, _ := choose(c, "proxy-2", h, now.Add(61*time.Second)); got != "proxy-1" {
		t.Fatal("did not return")
	}
	c.AutoReturn = false
	if got, _ := choose(c, "proxy-2", h, now.Add(time.Hour)); got != "proxy-2" {
		t.Fatal("sticky mode broken")
	}
}
func TestFailureResetsRecoveryWindow(t *testing.T) {
	c := defaults()
	n := time.Now()
	h := Health{}
	h.record(true, 1, "", n, c.Failures)
	h.record(false, 0, "", n.Add(50*time.Second), c.Failures)
	h.record(true, 1, "", n.Add(55*time.Second), c.Failures)
	state := map[string]Health{"proxy-1": h, "proxy-2": {Available: true}}
	if got, _ := choose(c, "proxy-2", state, n.Add(70*time.Second)); got != "proxy-2" {
		t.Fatal("failed probe must reset stable recovery timer")
	}
}
func TestBothDownNeverDirect(t *testing.T) {
	for _, mode := range []string{"auto", "proxy-1", "proxy-2"} {
		c := defaults()
		c.Mode = mode
		if got, _ := choose(c, "proxy-1", map[string]Health{}, time.Now()); got != "REJECT" {
			t.Fatalf("mode %s got %s", mode, got)
		}
	}
}
func TestBackupFailureReturnsToHealthyPrimaryEvenSticky(t *testing.T) {
	c := defaults()
	c.AutoReturn = false
	h := map[string]Health{"proxy-1": {Available: true}}
	if got, _ := choose(c, "proxy-2", h, time.Now()); got != "proxy-1" {
		t.Fatal(got)
	}
}
func TestConfigRejectsLoops(t *testing.T) {
	for _, port := range []int{17890, 17880, 17881, 7892} {
		c := defaults()
		c.Outbounds[0].Port = port
		if c.validate() == nil {
			t.Fatalf("accepted looping/conflicting port %d", port)
		}
	}
	c := defaults()
	c.Outbounds[0].Host = "0.0.0.0"
	if c.validate() == nil {
		t.Fatal("accepted non-local upstream")
	}
}

func TestMultipleOutboundsFailoverAndPriorityRecovery(t *testing.T) {
	c := threeOutbounds()
	now := time.Now()
	h := map[string]Health{"proxy-3": {Available: true, HealthySince: now}}
	if got, _ := choose(c, "proxy-1", h, now); got != "proxy-3" {
		t.Fatal("did not skip both failed exits", got)
	}
	h["proxy-2"] = Health{Available: true, HealthySince: now}
	if got, _ := choose(c, "proxy-3", h, now.Add(59*time.Second)); got != "proxy-3" {
		t.Fatal("returned before stable recovery")
	}
	if got, _ := choose(c, "proxy-3", h, now.Add(61*time.Second)); got != "proxy-2" {
		t.Fatal("did not recover to second priority")
	}
	h["proxy-1"] = Health{Available: true, HealthySince: now.Add(61 * time.Second)}
	if got, _ := choose(c, "proxy-2", h, now.Add(70*time.Second)); got != "proxy-2" {
		t.Fatal("returned to primary too early")
	}
	if got, _ := choose(c, "proxy-2", h, now.Add(122*time.Second)); got != "proxy-1" {
		t.Fatal("did not restore primary")
	}
	c.AutoReturn = false
	if got, _ := choose(c, "proxy-3", h, now.Add(time.Hour)); got != "proxy-3" {
		t.Fatal("sticky multi-exit mode broken")
	}
	h["proxy-3"] = Health{}
	if got, _ := choose(c, "proxy-3", h, now.Add(time.Hour)); got != "proxy-1" {
		t.Fatal("sticky mode did not fail over")
	}
}

func TestRemovedAndFixedOutboundsNeverLeakThroughStaleHealth(t *testing.T) {
	c := threeOutbounds()
	h := map[string]Health{"deleted": {Available: true}, "proxy-2": {Available: true}}
	if got, _ := choose(c, "deleted", h, time.Now()); got != "proxy-2" {
		t.Fatal("stale selected exit was reused")
	}
	c.Mode = "proxy-3"
	if got, _ := choose(c, "proxy-2", h, time.Now()); got != "REJECT" {
		t.Fatal("fixed mode used a different exit")
	}
	c.Mode = "deleted"
	if got, _ := choose(c, "deleted", h, time.Now()); got != "REJECT" {
		t.Fatal("deleted fixed exit used stale health")
	}
	c.Mode = "auto"
	c.Outbounds = c.Outbounds[1:2]
	if got, _ := choose(c, "REJECT", h, time.Now()); got != "proxy-2" {
		t.Fatal("single exit configuration failed")
	}
	delete(h, "proxy-2")
	if got, _ := choose(c, "proxy-2", h, time.Now()); got != "REJECT" {
		t.Fatal("single failed exit did not reject")
	}
}
