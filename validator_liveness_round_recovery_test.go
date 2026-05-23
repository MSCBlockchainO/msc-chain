package main

import (
	"testing"
	"time"
)

func TestGetConsensusLeaderForRoundUsesHeightPlusRound(t *testing.T) {
	validators := []string{"C", "A", "B", "D"}
	cases := []struct {
		height int
		round  uint32
	}{
		{height: 1, round: 0},
		{height: 1, round: 1},
		{height: 5, round: 2},
		{height: 99, round: 7},
	}

	for _, tc := range cases {
		got := GetConsensusLeaderForRound(validators, tc.height, tc.round)
		want := LeaderForHeight(uint64(tc.height)+uint64(tc.round), validators)
		if got != want {
			t.Fatalf("leader mismatch at height=%d round=%d: got=%s want=%s", tc.height, tc.round, got, want)
		}
	}
}

func TestValidatorRejoinRequiresHeartbeatsAndSignedEvidence(t *testing.T) {
	oldHB := ValidatorRejoinRequiredHeartbeats
	oldSigned := ValidatorRejoinRequiredSignedBlocks
	oldWindow := ValidatorRejoinWindowBlocks
	ValidatorRejoinRequiredHeartbeats = 3
	ValidatorRejoinRequiredSignedBlocks = 1
	ValidatorRejoinWindowBlocks = 16
	t.Cleanup(func() {
		ValidatorRejoinRequiredHeartbeats = oldHB
		ValidatorRejoinRequiredSignedBlocks = oldSigned
		ValidatorRejoinWindowBlocks = oldWindow
	})

	n := &Node{
		validatorStatus:       map[string]*ValidatorStatus{"A": {LastSeen: time.Now(), Active: true, ReportedHeight: 120, FinalizedHeight: 120}},
		validatorOfflineSince: make(map[string]time.Time),
		validatorRejoin:       make(map[string]ValidatorRejoinState),
	}
	n.commitMu.Lock()
	n.finalizedHeight = 120
	n.committedHeight = 120
	n.commitMu.Unlock()

	n.markValidatorOffline("A", "test")
	if _, ok := n.validatorOfflineSince["A"]; !ok {
		t.Fatalf("expected validator A to be marked offline")
	}

	n.recordValidatorRejoinHeartbeat("A")
	n.recordValidatorRejoinHeartbeat("A")
	if _, ok := n.validatorOfflineSince["A"]; !ok {
		t.Fatalf("validator A should remain offline before signed evidence + required heartbeats")
	}

	n.recordValidatorRejoinSigned("A", 120)
	n.recordValidatorRejoinHeartbeat("A")

	if _, ok := n.validatorOfflineSince["A"]; ok {
		t.Fatalf("validator A should become live after rejoin criteria")
	}
	if _, ok := n.validatorRejoin["A"]; ok {
		t.Fatalf("validator A rejoin state should be cleared after completion")
	}
	if st := n.validatorStatus["A"]; st == nil || !st.Active {
		t.Fatalf("validator A status should be active after rejoin")
	}
}

func TestExecutionQuorumEmergencyDoesNotRelaxFinalityQuorum(t *testing.T) {
	oldEmergency := ExecQuorumEmergencyEnabled
	oldTimeout := execQuorumEmergencyStallTimeout
	oldDrop := execQuorumEmergencyMaxDrop
	oldPct := GlobalConfig.ExecQuorumPct
	ExecQuorumEmergencyEnabled = true
	execQuorumEmergencyStallTimeout = 20 * time.Second
	execQuorumEmergencyMaxDrop = 1
	GlobalConfig.ExecQuorumPct = 80
	t.Cleanup(func() {
		ExecQuorumEmergencyEnabled = oldEmergency
		execQuorumEmergencyStallTimeout = oldTimeout
		execQuorumEmergencyMaxDrop = oldDrop
		GlobalConfig.ExecQuorumPct = oldPct
	})

	bc := NewBlockchain()
	n := &Node{
		Blockchain:        &bc,
		GenesisValidators: []string{"A", "B", "C", "D"},
		validatorStatus:   make(map[string]*ValidatorStatus),
		validatorOfflineSince: map[string]time.Time{
			"C": time.Now(),
			"D": time.Now(),
		},
		validatorRejoin:    make(map[string]ValidatorRejoinState),
		validatorSetHeight: 10,
	}
	now := time.Now()
	for _, id := range []string{"A", "B", "C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             true,
			ConsensusReadyKnown: true,
			ReportedHeight:      10,
			FinalizedHeight:     10,
			ExecEpoch:           10,
			ValidatorSetHeight:  10,
		}
	}
	n.commitMu.Lock()
	n.finalizedHeight = 10
	n.committedHeight = 10
	n.lastCommitAt = time.Now().Add(-25 * time.Second)
	n.commitMu.Unlock()

	stalled := n.executionQuorumRequired(10)
	if stalled != 3 {
		t.Fatalf("expected strict quorum 3 for 4 validators under stall, got %d", stalled)
	}

	n.commitMu.Lock()
	n.lastCommitAt = time.Now()
	n.commitMu.Unlock()
	strict := n.executionQuorumRequired(10)
	if strict != 3 {
		t.Fatalf("expected strict quorum 3 with no stall, got %d", strict)
	}
}

func TestMainnetFourValidatorEmergencyDoesNotDropBelowThree(t *testing.T) {
	oldEmergency := ExecQuorumEmergencyEnabled
	oldTimeout := execQuorumEmergencyStallTimeout
	oldDrop := execQuorumEmergencyMaxDrop
	oldPct := GlobalConfig.ExecQuorumPct
	oldTestnet := IsTestnet
	ExecQuorumEmergencyEnabled = true
	execQuorumEmergencyStallTimeout = 20 * time.Second
	execQuorumEmergencyMaxDrop = 2
	GlobalConfig.ExecQuorumPct = 75
	IsTestnet = false
	t.Cleanup(func() {
		ExecQuorumEmergencyEnabled = oldEmergency
		execQuorumEmergencyStallTimeout = oldTimeout
		execQuorumEmergencyMaxDrop = oldDrop
		GlobalConfig.ExecQuorumPct = oldPct
		IsTestnet = oldTestnet
	})

	bc := NewBlockchain()
	n := &Node{
		Blockchain:         &bc,
		GenesisValidators:  []string{"A", "B", "C", "D"},
		validatorStatus:    make(map[string]*ValidatorStatus),
		validatorSetHeight: 10,
	}
	now := time.Now()
	for _, id := range []string{"A", "B"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             true,
			ConsensusReadyKnown: true,
			ReportedHeight:      10,
			FinalizedHeight:     10,
			ExecEpoch:           10,
			ValidatorSetHeight:  10,
		}
	}
	for _, id := range []string{"C", "D"} {
		n.validatorStatus[id] = &ValidatorStatus{
			LastSeen:            now,
			Active:              true,
			Enabled:             false,
			ConsensusReadyKnown: true,
			ReportedHeight:      10,
			FinalizedHeight:     10,
			ExecEpoch:           10,
			ValidatorSetHeight:  10,
		}
	}
	n.commitMu.Lock()
	n.finalizedHeight = 10
	n.committedHeight = 10
	n.lastCommitAt = time.Now().Add(-2 * mainnetDegradedGraceWindow())
	n.commitMu.Unlock()

	policy := n.executionQuorumPolicy(10)
	if policy.Required != 3 {
		t.Fatalf("mainnet 4-validator emergency quorum = %d, want 3", policy.Required)
	}
	if policy.Relaxed {
		t.Fatalf("mainnet 4-validator quorum must not relax to 2-of-4: %+v", policy)
	}
}
