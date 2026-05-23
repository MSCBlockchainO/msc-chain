package main

import (
	"strings"
	"testing"
)

func TestSanitizePeerListWithPreferredDropsConflictingPeerID(t *testing.T) {
	peers := []string{
		"/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWOLDOLDOLDOLDOLDOLDOLDOLDOLDOLDOLDOLD1",
		"/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWNEWNEWNEWNEWNEWNEWNEWNEWNEWNEWNEWNEW2",
		"/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
	}
	preferred := []string{
		"/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWNEWNEWNEWNEWNEWNEWNEWNEWNEWNEWNEWNEW2",
	}

	got := sanitizePeerListWithPreferred(peers, preferred)

	if len(got) != 2 {
		t.Fatalf("expected 2 peers after sanitize, got %d: %#v", len(got), got)
	}
	want := preferred[0]
	found := false
	for _, p := range got {
		if p == want {
			found = true
		}
		if p == peers[0] {
			t.Fatalf("stale peer id was not removed: %s", p)
		}
	}
	if !found {
		t.Fatalf("preferred peer missing, got %#v", got)
	}
}

func TestSanitizePeerListWithPreferredKeepsFirstWhenNoPreference(t *testing.T) {
	peers := []string{
		"/ip4/127.0.0.1/tcp/7003/p2p/12D3KooWCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC1",
		"/ip4/127.0.0.1/tcp/7003/p2p/12D3KooWCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC2",
	}

	got := sanitizePeerListWithPreferred(peers, nil)
	if len(got) != 1 {
		t.Fatalf("expected single peer for same endpoint, got %d: %#v", len(got), got)
	}
	if got[0] != peers[0] {
		t.Fatalf("expected first peer to be retained, got %s", got[0])
	}
}

func TestReconcileLocalhostPeerIDsWithPersistedPrefersPersistedLocalhostPeerID(t *testing.T) {
	peers := []string{
		"/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWOLDA",
		"/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWOLDB",
	}
	persisted := []string{
		"/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWNEWB",
	}

	got, events := reconcileLocalhostPeerIDsWithPersisted(peers, persisted)

	if len(events) != 1 {
		t.Fatalf("expected one reconcile event, got %d", len(events))
	}
	if events[0].Base != "/ip4/127.0.0.1/tcp/7002" {
		t.Fatalf("unexpected reconcile base: %q", events[0].Base)
	}
	if events[0].OldPeerID != "12D3KooWOLDB" || events[0].NewPeerID != "12D3KooWNEWB" {
		t.Fatalf("unexpected reconcile event: %#v", events[0])
	}
	if len(got) != 2 {
		t.Fatalf("expected two peers after reconcile, got %d: %#v", len(got), got)
	}
	for _, addr := range got {
		if addr == "/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWOLDB" {
			t.Fatalf("stale localhost peer id not replaced: %#v", got)
		}
	}
	want := "/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWNEWB"
	found := false
	for _, addr := range got {
		if addr == want {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected reconciled peer %q in %#v", want, got)
	}
}

func TestReconcileLocalhostPeerIDsWithPersistedKeepsNonLocalhostConfigAuthoritative(t *testing.T) {
	peers := []string{
		"/dns4/node.example.com/tcp/7001/p2p/12D3KooWOLDREMOTE",
	}
	persisted := []string{
		"/dns4/node.example.com/tcp/7001/p2p/12D3KooWNEWREMOTE",
	}

	got, events := reconcileLocalhostPeerIDsWithPersisted(peers, persisted)

	if len(events) != 0 {
		t.Fatalf("expected no reconcile events, got %#v", events)
	}
	if len(got) != 1 || got[0] != peers[0] {
		t.Fatalf("expected non-localhost config peer to stay authoritative, got %#v", got)
	}
}

func TestReconcileLocalhostPeerIDsWithPersistedPreservesConfigWithoutSameBasePersistedEntry(t *testing.T) {
	peers := []string{
		"/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWOLDA",
	}
	persisted := []string{
		"/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWNEWB",
	}

	got, events := reconcileLocalhostPeerIDsWithPersisted(peers, persisted)

	if len(events) != 0 {
		t.Fatalf("expected no reconcile events, got %#v", events)
	}
	if len(got) != 1 || got[0] != peers[0] {
		t.Fatalf("expected config peer to be preserved, got %#v", got)
	}
}

func TestStartupSelfPeerFilteringStillWorksAfterReconcile(t *testing.T) {
	peers := []string{
		"/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWOLDA",
		"/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWOLDB",
	}
	persisted := []string{
		"/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWNEWA",
		"/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWNEWB",
	}

	reconciled, _ := reconcileLocalhostPeerIDsWithPersisted(peers, persisted)
	selfBase := "/ip4/127.0.0.1/tcp/7001"
	filtered := make([]string, 0, len(reconciled))
	for _, addr := range reconciled {
		if stripP2PComponent(addr) == selfBase {
			continue
		}
		filtered = append(filtered, addr)
	}

	if len(filtered) != 1 {
		t.Fatalf("expected one non-self peer after filtering, got %#v", filtered)
	}
	if stripP2PComponent(filtered[0]) != "/ip4/127.0.0.1/tcp/7002" {
		t.Fatalf("unexpected remaining peer after filtering: %#v", filtered)
	}
}

func TestReconcileLocalhostPeerIDsWithPersistedACrossCluster(t *testing.T) {
	peers := []string{
		"/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWOLDA",
		"/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWOLDB",
		"/ip4/127.0.0.1/tcp/7003/p2p/12D3KooWOLDC",
		"/ip4/127.0.0.1/tcp/7004/p2p/12D3KooWOLDD",
	}
	persisted := []string{
		"/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWF9bfGknPNtbKz9UwvUMwn6L83zotcPLb7PzmHkgp34QJ",
		"/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWGhsa4XsXQiq1TRtLRzAgbJsJzvmJpttcM97DMY3NBNea",
		"/ip4/127.0.0.1/tcp/7003/p2p/12D3KooWNShYtVRE9TF7pVndtC97acZYy3vF2necGAnqJX8q5Jng",
		"/ip4/127.0.0.1/tcp/7004/p2p/12D3KooWA5xVniTmmLVmN864jLMAiJXGRQi5KdXHt9wv5GgpHEBz",
	}

	got, events := reconcileLocalhostPeerIDsWithPersisted(peers, persisted)

	if len(events) != 4 {
		t.Fatalf("expected four reconcile events, got %d: %#v", len(events), events)
	}
	if len(got) != 4 {
		t.Fatalf("expected four peers after reconcile, got %d: %#v", len(got), got)
	}
	for _, addr := range persisted {
		found := false
		for _, gotAddr := range got {
			if gotAddr == addr {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected reconciled peer %q in %#v", addr, got)
		}
	}
}

func TestReplayValidatorFreezeJournalSkipsEntryOnChainHashMismatch(t *testing.T) {
	dataDir := t.TempDir()
	nodeID := "T"

	journalVals := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	journalHash := ValidatorSetHash(journalVals)
	chainVals := canonicalValidatorIDs([]string{"A", "B", "C", "D", "F"})
	chainHash := ValidatorSetHash(chainVals)
	if chainHash == journalHash {
		t.Fatalf("test setup invalid: expected different hashes")
	}

	entry := ValidatorFreezeJournalEntry{
		Height:           1,
		ValidatorSetHash: journalHash,
		Validators:       journalVals,
	}
	if err := appendValidatorFreezeJournal(dataDir, nodeID, entry); err != nil {
		t.Fatalf("append journal entry: %v", err)
	}

	n := &Node{
		DataDir:    dataDir,
		ID:         nodeID,
		Blockchain: &Blockchain{Blocks: []Block{{ID: 1, ValidatorSetHash: chainHash}}},
	}
	applied := n.replayValidatorFreezeJournal()
	if applied != 0 {
		t.Fatalf("expected no replay apply on chain hash mismatch, got %d", applied)
	}
	if hash, ok := n.frozenValidatorSetHash(1); ok || hash != "" {
		t.Fatalf("expected frozen hash to remain empty, got ok=%v hash=%q", ok, hash)
	}
}

func TestReplayValidatorFreezeJournalAppliesEntryWhenChainHashMatches(t *testing.T) {
	dataDir := t.TempDir()
	nodeID := "U"

	vals := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	hash := ValidatorSetHash(vals)
	entry := ValidatorFreezeJournalEntry{
		Height:           1,
		ValidatorSetHash: hash,
		Validators:       vals,
	}
	if err := appendValidatorFreezeJournal(dataDir, nodeID, entry); err != nil {
		t.Fatalf("append journal entry: %v", err)
	}

	n := &Node{
		DataDir:    dataDir,
		ID:         nodeID,
		Blockchain: &Blockchain{Blocks: []Block{{ID: 1, ValidatorSetHash: hash}}},
	}
	applied := n.replayValidatorFreezeJournal()
	if applied != 1 {
		t.Fatalf("expected replay apply count=1, got %d", applied)
	}
	got, ok := n.frozenValidatorSetHash(1)
	if !ok || got != hash {
		t.Fatalf("expected frozen hash %q, got ok=%v hash=%q", hash, ok, got)
	}
}

func TestSyncFrozenValidatorSetHashesFromChainOverwritesStaleEntries(t *testing.T) {
	chainVals := canonicalValidatorIDs([]string{"A", "B", "C", "D", "F"})
	chainHash := ValidatorSetHash(chainVals)
	staleVals := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	staleHash := ValidatorSetHash(staleVals)
	if chainHash == staleHash {
		t.Fatalf("test setup invalid: expected different hashes")
	}

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 1, ValidatorSetHash: chainHash},
			},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			1: staleVals,
		},
		frozenValidatorHashByHeight: map[uint64]string{
			1: staleHash,
		},
	}

	applied := n.syncFrozenValidatorSetHashesFromChain()
	if applied != 1 {
		t.Fatalf("expected chain sync apply count=1, got %d", applied)
	}
	gotHash, ok := n.frozenValidatorSetHash(1)
	if !ok || gotHash != chainHash {
		t.Fatalf("expected chain hash %q, got ok=%v hash=%q", chainHash, ok, gotHash)
	}
	if vals := n.frozenValidatorsForHeight(1); len(vals) != 0 {
		t.Fatalf("expected stale validator list to be dropped, got %#v", vals)
	}
}

func TestSyncFrozenValidatorSetHashesFromChainKeepsMatchingList(t *testing.T) {
	vals := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	hash := ValidatorSetHash(vals)

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 1, ValidatorSetHash: hash},
			},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			1: vals,
		},
		frozenValidatorHashByHeight: map[uint64]string{
			1: hash,
		},
	}

	applied := n.syncFrozenValidatorSetHashesFromChain()
	if applied != 0 {
		t.Fatalf("expected no updates when already matching, got %d", applied)
	}
	gotVals := n.frozenValidatorsForHeight(1)
	if len(gotVals) != len(vals) {
		t.Fatalf("expected validator list to stay intact, got %#v", gotVals)
	}
}

func TestSyncFrozenValidatorSetHashesFromChainMaterializesRegistrySubset(t *testing.T) {
	active := canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	registry := testValidatorSetMaterializationRegistry()
	targetHash := validatorSetHashFromSnapshotForHeight(1, active, registry)

	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{
					ID:               1,
					BlockHash:        "block-1",
					Signatures:       []string{"A", "B", "C"},
					ValidatorSetHash: targetHash,
				},
			},
		},
	}
	installValidatorSetMaterializationRegistry(t, n, 1, registry)

	applied := n.syncFrozenValidatorSetHashesFromChain()
	if applied != 1 {
		t.Fatalf("expected hash sync apply count=1, got %d", applied)
	}
	gotHash, ok := n.frozenValidatorSetHash(1)
	if !ok || !strings.EqualFold(strings.TrimSpace(gotHash), targetHash) {
		t.Fatalf("expected chain hash %q, got ok=%v hash=%q", targetHash, ok, gotHash)
	}
	gotVals := n.frozenValidatorsForHeight(1)
	if !sameStringSlice(gotVals, active) {
		t.Fatalf("expected validator materialization from registry subset, got=%v want=%v", gotVals, active)
	}
}
