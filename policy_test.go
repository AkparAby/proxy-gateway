package main

import (
	"testing"
	"time"
)

func TestFailoverHysteresisAndRecovery(t *testing.T) {
	c := defaults()
	now := time.Now()
	h := map[string]Health{"SpeedCat": {}, "Clash": {}}
	record := func(name string, ok bool, at time.Time) {
		v := h[name]
		v.record(ok, 50, "test", at, c.Failures)
		h[name] = v
	}
	record("SpeedCat", true, now)
	record("Clash", true, now)
	if got, _ := choose(c, "REJECT", h, now); got != "SpeedCat" {
		t.Fatal(got)
	}
	for i := 0; i < 2; i++ {
		record("SpeedCat", false, now)
		if got, _ := choose(c, "SpeedCat", h, now); got != "SpeedCat" {
			t.Fatal("isolated failures must not switch", got)
		}
	}
	record("SpeedCat", false, now)
	if got, _ := choose(c, "SpeedCat", h, now); got != "Clash" {
		t.Fatal(got)
	}
	record("SpeedCat", true, now)
	if got, _ := choose(c, "Clash", h, now.Add(59*time.Second)); got != "Clash" {
		t.Fatal("returned too early")
	}
	if got, _ := choose(c, "Clash", h, now.Add(61*time.Second)); got != "SpeedCat" {
		t.Fatal("did not return")
	}
	c.AutoReturn = false
	if got, _ := choose(c, "Clash", h, now.Add(time.Hour)); got != "Clash" {
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
	state := map[string]Health{"SpeedCat": h, "Clash": {Available: true}}
	if got, _ := choose(c, "Clash", state, n.Add(70*time.Second)); got != "Clash" {
		t.Fatal("failed probe must reset stable recovery timer")
	}
}
func TestBothDownNeverDirect(t *testing.T) {
	for _, mode := range []string{"auto", "SpeedCat", "Clash"} {
		c := defaults()
		c.Mode = mode
		if got, _ := choose(c, "SpeedCat", map[string]Health{}, time.Now()); got != "REJECT" {
			t.Fatalf("mode %s got %s", mode, got)
		}
	}
}
func TestBackupFailureReturnsToHealthyPrimaryEvenSticky(t *testing.T) {
	c := defaults()
	c.AutoReturn = false
	h := map[string]Health{"SpeedCat": {Available: true}}
	if got, _ := choose(c, "Clash", h, time.Now()); got != "SpeedCat" {
		t.Fatal(got)
	}
}
func TestConfigRejectsLoops(t *testing.T) {
	for _, port := range []int{17890, 17880, 17881, 7892} {
		c := defaults()
		c.SpeedCat.Port = port
		if c.validate() == nil {
			t.Fatalf("accepted looping/conflicting port %d", port)
		}
	}
	c := defaults()
	c.SpeedCat.Host = "0.0.0.0"
	if c.validate() == nil {
		t.Fatal("accepted non-local upstream")
	}
}
