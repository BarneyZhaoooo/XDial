package mobile

import (
	"encoding/json"
	"testing"
)

func connectionSnapshot(c *ConnectionController) connectionState {
	var s connectionState
	_ = json.Unmarshal([]byte(c.Snapshot()), &s)
	return s
}
func TestConnectionIntentEpochAndLateCallbacks(t *testing.T) {
	c := NewConnectionController()
	c.RequestConnection(true)
	if c.NextAttempt(0) != 0 || connectionSnapshot(c).Phase != "waiting_network" {
		t.Fatal(c.Snapshot())
	}
	c.ObserveUnderlay("network-a")
	first := c.NextAttempt(0)
	if first == 0 || c.NextAttempt(0) != 0 {
		t.Fatal("attempt must be leased once")
	}
	c.ObserveUnderlay("network-b")
	second := c.NextAttempt(0)
	c.CompleteAttempt(first, true, false, 0)
	if connectionSnapshot(c).Phase != "connecting" {
		t.Fatal("stale completion applied")
	}
	c.CompleteAttempt(second, true, false, 0)
	if connectionSnapshot(c).Phase != "connected" {
		t.Fatal(c.Snapshot())
	}
	c.ObserveUnderlay("")
	if connectionSnapshot(c).Phase != "waiting_network" {
		t.Fatal(c.Snapshot())
	}
	c.RequestConnection(false)
	c.ObserveUnderlay("network-c")
	c.CompleteAttempt(second, true, false, 0)
	if c.NextAttempt(0) != 0 || connectionSnapshot(c).Phase != "disconnected" {
		t.Fatal("user disconnect must win", c.Snapshot())
	}
}
func TestConnectionBoundedBackoffAndFatalFailure(t *testing.T) {
	c := NewConnectionController()
	c.ObserveUnderlay("a")
	c.RequestConnection(true)
	now := int64(0)
	for attempt := 0; attempt < 5; attempt++ {
		epoch := c.NextAttempt(now)
		if epoch == 0 {
			t.Fatal(c.Snapshot())
		}
		c.CompleteAttempt(epoch, false, true, now)
		s := connectionSnapshot(c)
		if attempt == 4 {
			if s.Phase != "failed" {
				t.Fatal(s)
			}
			break
		}
		if s.RetryAt-now != int64(1000<<attempt) || c.NextAttempt(s.RetryAt-1) != 0 {
			t.Fatal("unbounded or early retry", s)
		}
		now = s.RetryAt
	}
	c.ObserveUnderlay("b")
	epoch := c.NextAttempt(now)
	if epoch == 0 {
		t.Fatal("new OS network should reset recovery budget")
	}
	c.CompleteAttempt(epoch, false, false, now)
	if c.NextAttempt(now+100000) != 0 || connectionSnapshot(c).Phase != "failed" {
		t.Fatal("fatal error retried")
	}
	c.ObserveUnderlay("c")
	if c.NextAttempt(now+100000) != 0 {
		t.Fatal("new network retried terminal failure")
	}
	c.RequestConnection(false)
	c.RequestConnection(true)
	if c.NextAttempt(now) == 0 {
		t.Fatal("explicit retry should reset budget")
	}
}

func TestRetryAttemptRejectsPreviousCoreCallback(t *testing.T) {
	c := NewConnectionController()
	c.ObserveUnderlay("a")
	c.RequestConnection(true)
	first := c.NextAttempt(0)
	c.CompleteAttempt(first, false, true, 0)
	second := c.NextAttempt(1000)
	if second == first || second == 0 {
		t.Fatal("retry needs a distinct callback epoch")
	}
	c.CompleteAttempt(first, false, false, 1000)
	if connectionSnapshot(c).Phase != "connecting" {
		t.Fatal("old core killed retry")
	}
	c.CompleteAttempt(second, true, false, 1000)
	if connectionSnapshot(c).Phase != "connected" {
		t.Fatal(c.Snapshot())
	}
}
