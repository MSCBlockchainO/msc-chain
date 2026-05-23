package main

import (
	"strings"
	"testing"
)

func governanceTestRegistry() map[string]ValidatorRecord {
	return map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive, GovernanceSigner: true},
		"B": {ID: "B", Stake: 100, Status: ValidatorActive, GovernanceSigner: true},
		"C": {ID: "C", Stake: 100, Status: ValidatorActive, GovernanceSigner: true},
		"D": {ID: "D", Stake: 100, Status: ValidatorActive, GovernanceSigner: true},
		"E": {ID: "E", Stake: 100, Status: ValidatorActive, GovernanceSigner: false},
	}
}

func approveGovernanceProposal(t *testing.T, state *GovernanceState, id string, registry map[string]ValidatorRecord) {
	t.Helper()
	voteHeight := uint64(2)
	if proposal := state.Proposals[id]; proposal != nil && proposal.VotingStartHeight > voteHeight {
		voteHeight = proposal.VotingStartHeight
	}
	for _, voter := range []string{"A", "B", "C"} {
		if err := state.CastVote(id, voter, GovernanceVoteYes, voteHeight, registry); err != nil {
			t.Fatalf("cast vote %s: %v", voter, err)
		}
	}
	tally, err := state.FinalizeProposal(id, voteHeight, registry)
	if err != nil {
		t.Fatalf("finalize proposal: %v", err)
	}
	if tally.Yes != 3 || tally.Required != 3 {
		t.Fatalf("unexpected tally: %+v", tally)
	}
}

func TestGovernanceValidatorVotingRequiresStrictSignerQuorum(t *testing.T) {
	registry := governanceTestRegistry()
	state := NewGovernanceState()
	id, err := state.SubmitProposal(GovernanceProposal{
		Kind:              GovernanceProposalValidatorUpgrade,
		Title:             "add validator H",
		Proposer:          "A",
		CreatedHeight:     1,
		VotingStartHeight: 1,
		VotingEndHeight:   20,
		ActivationHeight:  30,
		Target:            "add:H",
	})
	if err != nil {
		t.Fatalf("submit proposal: %v", err)
	}
	if err := state.CastVote(id, "E", GovernanceVoteYes, 2, registry); err == nil || !strings.Contains(err.Error(), "not authorized") {
		t.Fatalf("expected non-governance signer rejection, got %v", err)
	}
	if err := state.CastVote(id, "A", GovernanceVoteYes, 2, registry); err != nil {
		t.Fatalf("cast vote A: %v", err)
	}
	if err := state.CastVote(id, "A", GovernanceVoteNo, 2, registry); err == nil || !strings.Contains(err.Error(), "already voted") {
		t.Fatalf("expected duplicate vote rejection, got %v", err)
	}
	if err := state.CastVote(id, "B", GovernanceVoteYes, 2, registry); err != nil {
		t.Fatalf("cast vote B: %v", err)
	}
	tally, err := state.FinalizeProposal(id, 2, registry)
	if err != nil {
		t.Fatalf("finalize before quorum: %v", err)
	}
	if state.Proposals[id].Status == GovernanceProposalApproved {
		t.Fatalf("proposal approved before strict quorum: %+v", tally)
	}
	if err := state.CastVote(id, "C", GovernanceVoteYes, 3, registry); err != nil {
		t.Fatalf("cast vote C: %v", err)
	}
	tally, err = state.FinalizeProposal(id, 3, registry)
	if err != nil {
		t.Fatalf("finalize at quorum: %v", err)
	}
	if state.Proposals[id].Status != GovernanceProposalApproved {
		t.Fatalf("proposal status = %s, want approved", state.Proposals[id].Status)
	}
	if tally.Yes != 3 || tally.Required != 3 {
		t.Fatalf("unexpected strict quorum tally: %+v", tally)
	}
}

func TestTreasuryGovernanceAppliesOnlyAfterApproval(t *testing.T) {
	registry := governanceTestRegistry()
	state := NewGovernanceState()
	state.TreasuryBalance = 1000
	id, err := state.SubmitProposal(GovernanceProposal{
		Kind:              GovernanceProposalTreasury,
		Title:             "fund client tooling",
		Proposer:          "A",
		CreatedHeight:     1,
		VotingStartHeight: 1,
		VotingEndHeight:   20,
		ActivationHeight:  3,
		TreasuryRecipient: "builder-fund",
		TreasuryAmount:    250,
	})
	if err != nil {
		t.Fatalf("submit treasury proposal: %v", err)
	}
	if err := state.ApplyApprovedProposal(id, 3); err == nil {
		t.Fatalf("expected unapproved treasury proposal to be rejected")
	}
	approveGovernanceProposal(t, state, id, registry)
	if err := state.ApplyApprovedProposal(id, 3); err != nil {
		t.Fatalf("apply treasury proposal: %v", err)
	}
	if state.TreasuryBalance != 750 {
		t.Fatalf("treasury balance = %d, want 750", state.TreasuryBalance)
	}
	if state.Proposals[id].Status != GovernanceProposalApplied {
		t.Fatalf("proposal status = %s, want applied", state.Proposals[id].Status)
	}
}

func TestProtocolUpgradeSchedulingActivationAndRollbackProtection(t *testing.T) {
	oldCheckpoint := SyncSnapshotCheckpointV2Height
	oldHashV3 := ValidatorSetHashV3Height
	defer func() {
		SyncSnapshotCheckpointV2Height = oldCheckpoint
		ValidatorSetHashV3Height = oldHashV3
	}()
	SyncSnapshotCheckpointV2Height = 0
	ValidatorSetHashV3Height = 100

	registry := governanceTestRegistry()
	state := NewGovernanceState()
	id, err := state.SubmitProposal(GovernanceProposal{
		Kind:              GovernanceProposalProtocolUpgrade,
		Title:             "activate checkpoint v2",
		Proposer:          "A",
		CreatedHeight:     10,
		VotingStartHeight: 10,
		VotingEndHeight:   20,
		ActivationHeight:  25,
		UpgradeName:       "checkpoint-v2",
		UpgradeVersion:    "0.2.0",
		ProtocolChanges: map[string]uint64{
			ProtocolGateSyncSnapshotCheckpointV2: 25,
			ProtocolGateValidatorSetHashV3:       120,
		},
	})
	if err != nil {
		t.Fatalf("submit protocol upgrade: %v", err)
	}
	approveGovernanceProposal(t, state, id, registry)
	if err := state.ApplyApprovedProposal(id, 12); err != nil {
		t.Fatalf("schedule protocol upgrade: %v", err)
	}
	if SyncSnapshotCheckpointV2Height != 0 {
		t.Fatalf("upgrade activated early at checkpoint height %d", SyncSnapshotCheckpointV2Height)
	}
	if _, err := state.UpgradeManager.ActivateDue(24); err != nil {
		t.Fatalf("activate due before height: %v", err)
	}
	if SyncSnapshotCheckpointV2Height != 0 {
		t.Fatalf("upgrade activated before activation height")
	}
	activated, err := state.UpgradeManager.ActivateDue(25)
	if err != nil {
		t.Fatalf("activate due at height: %v", err)
	}
	if len(activated) != 1 {
		t.Fatalf("activated upgrades = %d, want 1", len(activated))
	}
	if SyncSnapshotCheckpointV2Height != 25 || ValidatorSetHashV3Height != 120 {
		t.Fatalf("protocol gates not applied: checkpoint=%d hashv3=%d", SyncSnapshotCheckpointV2Height, ValidatorSetHashV3Height)
	}

	_, err = state.SubmitProposal(GovernanceProposal{
		Kind:              GovernanceProposalProtocolUpgrade,
		Title:             "unsafe rollback",
		Proposer:          "A",
		CreatedHeight:     30,
		VotingStartHeight: 30,
		VotingEndHeight:   40,
		ActivationHeight:  50,
		UpgradeName:       "rollback",
		ProtocolChanges: map[string]uint64{
			ProtocolGateValidatorSetHashV3: 60,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "rollback rejected") {
		t.Fatalf("expected rollback protection, got %v", err)
	}
}

func TestEmergencyGovernanceCanScheduleShortActivationWithStrictQuorum(t *testing.T) {
	oldDTL := ConfigDTLV2ActivationHeight
	defer func() { ConfigDTLV2ActivationHeight = oldDTL }()
	ConfigDTLV2ActivationHeight = 0

	registry := governanceTestRegistry()
	state := NewGovernanceState()
	id, err := state.SubmitProposal(GovernanceProposal{
		Kind:              GovernanceProposalEmergency,
		Title:             "emergency dtl gate",
		Proposer:          "A",
		CreatedHeight:     100,
		VotingStartHeight: 100,
		VotingEndHeight:   110,
		ActivationHeight:  101,
		UpgradeName:       "dtl-emergency",
		UpgradeVersion:    "0.1.1",
		ProtocolChanges: map[string]uint64{
			ProtocolGateDTLV2: 101,
		},
	})
	if err != nil {
		t.Fatalf("submit emergency proposal: %v", err)
	}
	approveGovernanceProposal(t, state, id, registry)
	if err := state.ApplyApprovedProposal(id, 100); err != nil {
		t.Fatalf("schedule emergency proposal: %v", err)
	}
	if ConfigDTLV2ActivationHeight != 0 {
		t.Fatalf("emergency activated before height")
	}
	if _, err := state.UpgradeManager.ActivateDue(101); err != nil {
		t.Fatalf("activate emergency: %v", err)
	}
	if ConfigDTLV2ActivationHeight != 101 {
		t.Fatalf("dtl activation height = %d, want 101", ConfigDTLV2ActivationHeight)
	}
}

func TestGovernanceStatePersistsInNodeDB(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	node := &Node{DB: db}
	state := NewGovernanceState()
	state.TreasuryBalance = 777
	id, err := state.SubmitProposal(GovernanceProposal{
		Kind:              GovernanceProposalTreasury,
		Title:             "persisted treasury proposal",
		Proposer:          "A",
		CreatedHeight:     1,
		VotingStartHeight: 1,
		VotingEndHeight:   5,
		TreasuryRecipient: "ops",
		TreasuryAmount:    10,
	})
	if err != nil {
		t.Fatalf("submit persisted proposal: %v", err)
	}
	if err := node.PersistGovernanceState(state); err != nil {
		t.Fatalf("persist governance state: %v", err)
	}
	loaded, err := node.LoadGovernanceState()
	if err != nil {
		t.Fatalf("load governance state: %v", err)
	}
	if loaded.TreasuryBalance != 777 {
		t.Fatalf("loaded treasury = %d, want 777", loaded.TreasuryBalance)
	}
	if loaded.Proposals[id] == nil {
		t.Fatalf("loaded proposal %s missing", id)
	}
	if loaded.Hash() != state.Hash() {
		t.Fatalf("loaded governance hash mismatch: got %s want %s", loaded.Hash(), state.Hash())
	}
}
