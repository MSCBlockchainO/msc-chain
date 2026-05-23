package main

import "testing"

func resetSlashEvidenceGlobalsForTest(t *testing.T) {
	t.Helper()

	oldRegistry := GlobalValidatorRegistry.Snapshot()
	participationMu.Lock()
	oldParticipation := make(map[string]*ParticipationScore, len(Participation))
	for id, score := range Participation {
		if score == nil {
			continue
		}
		cloned := *score
		oldParticipation[id] = &cloned
	}
	participationMu.Unlock()
	oldCooldown := make(map[string]int, len(ValidatorCooldown))
	for id, height := range ValidatorCooldown {
		oldCooldown[id] = height
	}

	t.Cleanup(func() {
		GlobalValidatorRegistry.Load(oldRegistry)
		participationMu.Lock()
		Participation = oldParticipation
		participationMu.Unlock()
		ValidatorCooldown = oldCooldown
	})
}

func seedSlashEvidenceValidator(id string) {
	id = normalizeValidatorID(id)
	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		id: {
			ID:     id,
			Status: ValidatorActive,
			Stake:  ValidatorMinStake,
		},
	})
}

func slashEvidenceEntriesForTest(t *testing.T, node *Node, validator string) []SlashEvidence {
	t.Helper()
	validator = normalizeValidatorID(validator)
	node.misbehaviorMu.Lock()
	defer node.misbehaviorMu.Unlock()
	entries := append([]SlashEvidence(nil), node.MisbehaviorLog[validator]...)
	return entries
}

func TestSlashEvidencePersistsEquivocationAndReloadsWithDedupe(t *testing.T) {
	resetSlashEvidenceGlobalsForTest(t)
	seedSlashEvidenceValidator("B")
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	node.RecordMisbehavior("b", "exec_equivocation_signed", 12, "EXEC-A")
	node.RecordMisbehavior("B", "exec_equivocation", 12, "exec-a")

	entries := slashEvidenceEntriesForTest(t, node, "B")
	if len(entries) != 1 {
		t.Fatalf("expected duplicate equivocation evidence to dedupe, got %d entries: %#v", len(entries), entries)
	}
	if entries[0].Reason != "exec_equivocation" {
		t.Fatalf("expected canonical reason exec_equivocation, got %q", entries[0].Reason)
	}
	if entries[0].BlockHash != "exec-a" {
		t.Fatalf("expected normalized block hash exec-a, got %q", entries[0].BlockHash)
	}

	rec, _ := GlobalValidatorRegistry.Get("B")
	if rec == nil || rec.TotalSlashes != 1 {
		t.Fatalf("expected one slash from first unique equivocation, got record %#v", rec)
	}

	node.misbehaviorMu.Lock()
	node.MisbehaviorLog = make(map[string][]SlashEvidence)
	node.misbehaviorMu.Unlock()
	if err := node.loadMisbehaviorEvidenceFromDB(); err != nil {
		t.Fatalf("reload slash evidence: %v", err)
	}
	reloaded := slashEvidenceEntriesForTest(t, node, "B")
	if len(reloaded) != 1 {
		t.Fatalf("expected one durable equivocation record after reload, got %d: %#v", len(reloaded), reloaded)
	}
	if reloaded[0].Reason != "exec_equivocation" || reloaded[0].BlockHash != "exec-a" {
		t.Fatalf("unexpected reloaded evidence: %#v", reloaded[0])
	}
}

func TestSlashEvidencePersistenceKeepsDistinctReasonsAtSameHeightHash(t *testing.T) {
	resetSlashEvidenceGlobalsForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	node.persistMisbehaviorEvidence(SlashEvidence{
		Validator: "B",
		Reason:    "double_proposal",
		Height:    9,
		BlockHash: "same-fork",
		Timestamp: 100,
	})
	node.persistMisbehaviorEvidence(SlashEvidence{
		Validator: "B",
		Reason:    "invalid_proposer",
		Height:    9,
		BlockHash: "same-fork",
		Timestamp: 100,
	})

	if err := node.loadMisbehaviorEvidenceFromDB(); err != nil {
		t.Fatalf("reload slash evidence: %v", err)
	}
	entries := slashEvidenceEntriesForTest(t, node, "B")
	if len(entries) != 2 {
		t.Fatalf("expected two distinct evidence records at same height/hash, got %d: %#v", len(entries), entries)
	}
	reasons := map[string]bool{}
	for _, ev := range entries {
		reasons[ev.Reason] = true
	}
	if !reasons["double_proposal"] || !reasons["invalid_proposer"] {
		t.Fatalf("missing distinct persisted reasons: %#v", reasons)
	}
}

func TestSlashEvidenceReplayAppliesCrashPersistedEvidenceOnce(t *testing.T) {
	resetSlashEvidenceGlobalsForTest(t)
	seedSlashEvidenceValidator("B")
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	crashPersisted := SlashEvidence{
		Validator: "B",
		Reason:    "double_proposal",
		Height:    15,
		BlockHash: "fork-b",
		Timestamp: 200,
	}
	node.persistMisbehaviorEvidence(crashPersisted)
	node.misbehaviorMu.Lock()
	node.MisbehaviorLog = make(map[string][]SlashEvidence)
	node.misbehaviorMu.Unlock()
	if err := node.loadMisbehaviorEvidenceFromDB(); err != nil {
		t.Fatalf("reload slash evidence: %v", err)
	}

	node.replayMisbehaviorSlashingThresholds("test_crash_replay")
	rec, _ := GlobalValidatorRegistry.Get("B")
	if rec == nil || rec.TotalSlashes != 1 {
		t.Fatalf("expected crash-persisted evidence to replay exactly one slash, got record %#v", rec)
	}

	entries := slashEvidenceEntriesForTest(t, node, "B")
	if len(entries) != 1 {
		t.Fatalf("expected one restored evidence entry, got %d", len(entries))
	}
	actionKey := slashActionKey("B", "double_proposal", entries[0])
	if actionKey == "" || !node.slashActionApplied(actionKey) {
		t.Fatalf("expected persisted slash action marker for %q", actionKey)
	}

	node.replayMisbehaviorSlashingThresholds("test_crash_replay_again")
	rec, _ = GlobalValidatorRegistry.Get("B")
	if rec == nil || rec.TotalSlashes != 1 {
		t.Fatalf("expected replay dedupe to prevent second slash, got record %#v", rec)
	}
}
