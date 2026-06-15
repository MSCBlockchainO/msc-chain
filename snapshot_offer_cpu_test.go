package main

import (
	"testing"
	"time"
)

func TestSnapshotOfferCooldownSkipsRepeatedLookupWindow(t *testing.T) {
	node := &Node{
		snapshotOfferSentAt: map[string]time.Time{
			"B": time.Now().Add(-time.Second),
		},
	}
	now := time.Now()
	if !node.snapshotOfferCooldownActive("B", now, 15*time.Second) {
		t.Fatal("expected recent snapshot offer to remain in cooldown")
	}
	if node.snapshotOfferCooldownActive("B", now.Add(20*time.Second), 15*time.Second) {
		t.Fatal("expected expired snapshot offer cooldown to clear")
	}
}
