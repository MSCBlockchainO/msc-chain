package main

import (
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"testing"
)

func signCensorshipEvidenceForTest(t *testing.T, observer, leader, txID string) CensorshipEvidence {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	observer = normalizeValidatorID(observer)
	ValidatorPubKeys[observer] = pub
	ev := CensorshipEvidence{
		Height:      10,
		BlockHash:   "blk10",
		Leader:      normalizeValidatorID(leader),
		TxID:        txID,
		Fee:         1,
		MempoolRoot: "root10",
		Observer:    observer,
		ObservedAt:  10,
	}
	ev.ObserverSig = ed25519.Sign(priv, censorshipEvidenceSignBytes(ev))
	return ev
}

func TestApplyCensorshipEvidenceRequiresValidObserverSignature(t *testing.T) {
	prevPool := CensorshipEvidencePool
	prevPub := ValidatorPubKeys
	CensorshipEvidencePool = map[EvidenceKey][]CensorshipEvidence{}
	ValidatorPubKeys = map[string]ed25519.PublicKey{}
	defer func() {
		CensorshipEvidencePool = prevPool
		ValidatorPubKeys = prevPub
	}()

	block := Block{ID: 10, BlockHash: "blk10", Proposer: "LEAD", MempoolRoot: "root10"}
	ev := signCensorshipEvidenceForTest(t, "OBS", "LEAD", "tx-1")
	if !ApplyCensorshipEvidence(nil, ev, block) {
		t.Fatalf("valid signed evidence should apply")
	}
	key := EvidenceKey{Leader: "LEAD", Height: 10}
	if got := len(CensorshipEvidencePool[key]); got != 1 {
		t.Fatalf("expected one evidence entry, got=%d", got)
	}

	evBad := ev
	evBad.TxID = "tx-2"
	evBad.ObserverSig = []byte("bad")
	if ApplyCensorshipEvidence(nil, evBad, block) {
		t.Fatalf("invalid signature evidence must be rejected")
	}
}

func TestApplyCensorshipEvidenceAllowsMultiWitnessSameTx(t *testing.T) {
	prevPool := CensorshipEvidencePool
	prevPub := ValidatorPubKeys
	CensorshipEvidencePool = map[EvidenceKey][]CensorshipEvidence{}
	ValidatorPubKeys = map[string]ed25519.PublicKey{}
	defer func() {
		CensorshipEvidencePool = prevPool
		ValidatorPubKeys = prevPub
	}()

	block := Block{ID: 10, BlockHash: "blk10", Proposer: "LEAD", MempoolRoot: "root10"}
	ev1 := signCensorshipEvidenceForTest(t, "OBS1", "LEAD", "tx-1")
	ev2 := signCensorshipEvidenceForTest(t, "OBS2", "LEAD", "tx-1")
	if !ApplyCensorshipEvidence(nil, ev1, block) {
		t.Fatalf("first evidence should apply")
	}
	if !ApplyCensorshipEvidence(nil, ev2, block) {
		t.Fatalf("second observer evidence should apply for same tx")
	}

	key := EvidenceKey{Leader: "LEAD", Height: 10}
	if got := len(CensorshipEvidencePool[key]); got != 2 {
		t.Fatalf("expected 2 witness entries, got=%d", got)
	}
}
