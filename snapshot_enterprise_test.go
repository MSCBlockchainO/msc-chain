package main

import "testing"

func TestSnapshotCatalogAvailabilityRatio(t *testing.T) {
	if got := snapshotCatalogAvailabilityRatio(0, 3); got != 0 {
		t.Fatalf("unexpected ratio for zero providers: got=%f want=0", got)
	}
	if got := snapshotCatalogAvailabilityRatio(1, 3); got <= 0 || got >= 1 {
		t.Fatalf("unexpected ratio for one provider: got=%f", got)
	}
	if got := snapshotCatalogAvailabilityRatio(5, 3); got != 1 {
		t.Fatalf("unexpected ratio clamp: got=%f want=1", got)
	}
}

func TestUpdateSnapshotCatalogProvidersMergesAndRaisesAvailability(t *testing.T) {
	n := &Node{}
	n.updateSnapshotCatalogProviders(448, []string{"A"})
	first, ok := n.snapshotCatalogEntry(448)
	if !ok {
		t.Fatalf("expected catalog entry to exist")
	}
	if len(first.ProviderSet) != 1 || first.ProviderSet[0] != "A" {
		t.Fatalf("unexpected provider set after first update: %+v", first.ProviderSet)
	}
	firstRatio := first.AvailabilityRatio
	if firstRatio <= 0 {
		t.Fatalf("expected positive availability ratio")
	}

	n.updateSnapshotCatalogProviders(448, []string{"B"})
	second, ok := n.snapshotCatalogEntry(448)
	if !ok {
		t.Fatalf("expected catalog entry to exist")
	}
	if len(second.ProviderSet) != 2 {
		t.Fatalf("expected merged providers, got=%+v", second.ProviderSet)
	}
	if second.AvailabilityRatio < firstRatio {
		t.Fatalf("expected non-decreasing availability ratio: first=%f second=%f", firstRatio, second.AvailabilityRatio)
	}
}

func TestValidatorSyncIsolationAllowsNearTipSyncInFlight(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{ID: 10}}},
		Consensus:  &ConsensusState{SyncTarget: 11, Syncing: true, Paused: true, syncInFlight: true},
	}

	isolated, reason := n.validatorSyncIsolationState(11)
	if isolated {
		t.Fatalf("expected near-tip sync in-flight to allow next-height consensus, reason=%s", reason)
	}
}

func TestValidatorSyncIsolationBlocksFarSyncInFlight(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{Blocks: []Block{{ID: 10}}},
		Consensus:  &ConsensusState{SyncTarget: 20, syncInFlight: true},
	}

	isolated, reason := n.validatorSyncIsolationState(11)
	if !isolated || reason != "syncing" {
		t.Fatalf("expected far sync in-flight to isolate consensus, isolated=%t reason=%s", isolated, reason)
	}
}
