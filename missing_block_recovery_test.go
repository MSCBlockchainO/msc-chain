package main

import (
	"testing"
	"time"
)

func TestShouldStartMissingBlockRecoveryRejectsDuplicateInFlight(t *testing.T) {
	now := time.Now()
	if shouldStartMissingBlockRecovery(now, now.Add(-time.Second), true, 106, 106) {
		t.Fatalf("expected in-flight recovery for same height to be rejected")
	}
}

func TestShouldStartMissingBlockRecoveryRejectsRecentSameHeight(t *testing.T) {
	now := time.Now()
	if shouldStartMissingBlockRecovery(now, now.Add(-500*time.Millisecond), false, 106, 106) {
		t.Fatalf("expected recent same-height recovery request to be throttled")
	}
}

func TestShouldStartMissingBlockRecoveryAllowsAfterCooldownOrNewHeight(t *testing.T) {
	now := time.Now()
	if !shouldStartMissingBlockRecovery(now, now.Add(-2*time.Second), false, 106, 106) {
		t.Fatalf("expected same-height recovery after cooldown to be allowed")
	}
	if !shouldStartMissingBlockRecovery(now, now.Add(-100*time.Millisecond), false, 106, 107) {
		t.Fatalf("expected different-height recovery to be allowed")
	}
}
