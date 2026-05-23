package main

import (
	"testing"
	"time"
)

func TestRecordProposedBlockRoundAware(t *testing.T) {
	proposedBlocksMu.Lock()
	old := ProposedBlocks
	ProposedBlocks = make(map[uint64]map[uint32]map[string]string)
	proposedBlocksMu.Unlock()
	t.Cleanup(func() {
		proposedBlocksMu.Lock()
		ProposedBlocks = old
		proposedBlocksMu.Unlock()
	})

	added, equivocated, prev := recordProposedBlock(61, 7, "A", "hash-a")
	if !added || equivocated || prev != "" {
		t.Fatalf("first proposal unexpected result: added=%t equivocated=%t prev=%q", added, equivocated, prev)
	}

	added, equivocated, prev = recordProposedBlock(61, 7, "A", "hash-a")
	if added || equivocated || prev != "hash-a" {
		t.Fatalf("same round same hash should be duplicate only: added=%t equivocated=%t prev=%q", added, equivocated, prev)
	}

	added, equivocated, prev = recordProposedBlock(61, 7, "A", "hash-b")
	if added || !equivocated || prev != "hash-a" {
		t.Fatalf("same round conflicting hash should equivocate: added=%t equivocated=%t prev=%q", added, equivocated, prev)
	}

	added, equivocated, prev = recordProposedBlock(61, 8, "A", "hash-c")
	if !added || equivocated || prev != "" {
		t.Fatalf("new round proposal should be accepted: added=%t equivocated=%t prev=%q", added, equivocated, prev)
	}
}

func TestShouldCountDoubleProposalEvidenceDedupes(t *testing.T) {
	n := &Node{}

	if !n.shouldCountDoubleProposalEvidence(61, 7, "A", "hash-a", "hash-b") {
		t.Fatal("first evidence should be counted")
	}
	if n.shouldCountDoubleProposalEvidence(61, 7, "A", "hash-a", "hash-b") {
		t.Fatal("duplicate evidence should be ignored inside TTL")
	}
	if !n.shouldCountDoubleProposalEvidence(61, 7, "A", "hash-a", "hash-c") {
		t.Fatal("new evidence tuple should be counted")
	}

	key := doubleProposalEvidenceKey(61, 7, "A", "hash-a", "hash-b")
	n.doubleProposalMu.Lock()
	n.doubleProposalEvidenceSeen[key] = time.Now().Add(-4 * time.Minute)
	n.doubleProposalMu.Unlock()
	if !n.shouldCountDoubleProposalEvidence(61, 7, "A", "hash-a", "hash-b") {
		t.Fatal("expired evidence should be countable again")
	}
}
