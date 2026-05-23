package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
)

func copyPubKeyMap(in map[string]ed25519.PublicKey) map[string]ed25519.PublicKey {
	out := make(map[string]ed25519.PublicKey, len(in))
	for k, v := range in {
		out[k] = append(ed25519.PublicKey(nil), v...)
	}
	return out
}

func TestOnChainValidatorsFromRegistrySnapshotBuildsCanonicalState(t *testing.T) {
	validatorPubKeysMu.Lock()
	oldGenesis := copyPubKeyMap(GenesisValidatorPubKeys)
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey)
	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
	for i := range pub {
		pub[i] = byte(i + 1)
	}
	GenesisValidatorPubKeys["A"] = append(ed25519.PublicKey(nil), pub...)
	validatorPubKeysMu.Unlock()
	defer func() {
		validatorPubKeysMu.Lock()
		GenesisValidatorPubKeys = oldGenesis
		validatorPubKeysMu.Unlock()
	}()

	got := onChainValidatorsFromRegistrySnapshot(map[string]ValidatorRecord{
		"A": {
			ID:         "A",
			Stake:      150,
			Status:     ValidatorActive,
			JoinHeight: 10,
		},
	}, map[string]uint64{
		"A": 21,
	}, 20)

	v, ok := got["A"]
	if !ok {
		t.Fatalf("expected validator A in on-chain snapshot")
	}
	if v.Stake != 150 || v.VotingPower != 150 {
		t.Fatalf("unexpected stake/voting power: got stake=%d voting=%d", v.Stake, v.VotingPower)
	}
	if v.Status != string(ValidatorActive) {
		t.Fatalf("unexpected status: got=%s want=%s", v.Status, ValidatorActive)
	}
	if v.JoinHeight != 10 {
		t.Fatalf("unexpected join height: got=%d want=10", v.JoinHeight)
	}
	if v.ActivationHeight != 21 {
		t.Fatalf("unexpected activation height: got=%d want=21", v.ActivationHeight)
	}
	wantAddr := strings.ToLower(hex.EncodeToString(pub))
	if v.Address != wantAddr {
		t.Fatalf("unexpected canonical address: got=%s want=%s", v.Address, wantAddr)
	}
	if !bytes.Equal(v.PubKey, pub) {
		t.Fatalf("unexpected pubkey bytes in on-chain validator state")
	}
}

func TestPopulateSnapshotDerivedFieldsBackfillsStateValidatorsFromRegistry(t *testing.T) {
	snap := &StateSnapshot{
		Height: 42,
		ValidatorRegistry: map[string]ValidatorRecord{
			"A": {
				ID:         "A",
				Stake:      100,
				Status:     ValidatorActive,
				JoinHeight: 7,
			},
		},
	}

	populateSnapshotDerivedFields(snap)

	if len(snap.StateValidators) != 1 {
		t.Fatalf("expected one state validator, got=%d", len(snap.StateValidators))
	}
	if _, ok := snap.StateValidators["A"]; !ok {
		t.Fatalf("expected validator A state entry")
	}
}

func TestPopulateSnapshotDerivedFieldsBackfillsRegistryFromStateValidators(t *testing.T) {
	snap := &StateSnapshot{
		Height: 99,
		StateValidators: map[string]Validator{
			"A": {
				Address:          "a",
				Stake:            200,
				VotingPower:      200,
				Status:           string(ValidatorActive),
				JoinHeight:       5,
				ActivationHeight: 9,
			},
		},
	}

	populateSnapshotDerivedFields(snap)

	rec, ok := snap.ValidatorRegistry["A"]
	if !ok {
		t.Fatalf("expected registry entry for A")
	}
	if rec.Stake != 200 {
		t.Fatalf("unexpected reconstructed stake: got=%d want=200", rec.Stake)
	}
	if rec.Status != ValidatorActive {
		t.Fatalf("unexpected reconstructed status: got=%s want=%s", rec.Status, ValidatorActive)
	}
	if rec.JoinHeight != 5 {
		t.Fatalf("unexpected reconstructed join height: got=%d want=5", rec.JoinHeight)
	}
}

func TestApplySnapshotValidatorRegistryUsesOnChainStateFallback(t *testing.T) {
	old := GlobalValidatorRegistry.Snapshot()
	defer GlobalValidatorRegistry.Load(old)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{})

	n := &Node{}
	n.applySnapshotValidatorRegistry(StateSnapshot{
		Height: 10,
		StateValidators: map[string]Validator{
			"A": {
				Address:     "a",
				Stake:       123,
				VotingPower: 123,
				Status:      string(ValidatorActive),
				JoinHeight:  4,
			},
		},
	})

	rec, ok := GlobalValidatorRegistry.Get("A")
	if !ok || rec == nil {
		t.Fatalf("expected registry entry reconstructed from on-chain state")
	}
	if rec.Stake != 123 {
		t.Fatalf("unexpected reconstructed stake: got=%d want=123", rec.Stake)
	}
	if rec.Status != ValidatorActive {
		t.Fatalf("unexpected reconstructed status: got=%s want=%s", rec.Status, ValidatorActive)
	}
}
