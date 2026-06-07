package main

import "testing"

func TestFounderBadgeEligibility(t *testing.T) {
	oldEnabled := FounderValidatorBadgeEnabled
	oldCutoff := FounderValidatorCutoffHeight
	oldMax := FounderValidatorMaxCount
	oldMin := FounderValidatorMinSignedBPS
	oldCollection := FounderValidatorNFTCollection
	t.Cleanup(func() {
		FounderValidatorBadgeEnabled = oldEnabled
		FounderValidatorCutoffHeight = oldCutoff
		FounderValidatorMaxCount = oldMax
		FounderValidatorMinSignedBPS = oldMin
		FounderValidatorNFTCollection = oldCollection
	})

	FounderValidatorBadgeEnabled = true
	FounderValidatorCutoffHeight = 1000
	FounderValidatorMaxCount = 10
	FounderValidatorMinSignedBPS = 9500
	FounderValidatorNFTCollection = "founder"

	entry := ValidatorPoolEntry{ID: "HOME1", SignedRatioBPS: 9800}
	rec := ValidatorRecord{ID: "HOME1", Status: ValidatorActive, JoinHeight: 10}
	badge := founderBadgeForValidator(entry, rec, 1, 100, nil)
	if !badge.Eligible || !badge.Badge {
		t.Fatalf("expected founder badge eligibility, got %+v", badge)
	}
	if badge.NFTTokenID == 0 || badge.NFTCollection != "founder" {
		t.Fatalf("expected deterministic founder NFT metadata, got %+v", badge)
	}
}

func TestFounderBadgeRejectsSevereFaultAndLowUptime(t *testing.T) {
	oldEnabled := FounderValidatorBadgeEnabled
	oldMin := FounderValidatorMinSignedBPS
	oldCutoff := FounderValidatorCutoffHeight
	t.Cleanup(func() {
		FounderValidatorBadgeEnabled = oldEnabled
		FounderValidatorMinSignedBPS = oldMin
		FounderValidatorCutoffHeight = oldCutoff
	})

	FounderValidatorBadgeEnabled = true
	FounderValidatorMinSignedBPS = 9500
	FounderValidatorCutoffHeight = 1000

	lowSigned := founderBadgeForValidator(
		ValidatorPoolEntry{ID: "HOME2", SignedRatioBPS: 9000},
		ValidatorRecord{ID: "HOME2", Status: ValidatorActive},
		1,
		100,
		nil,
	)
	if lowSigned.Badge || lowSigned.Reason != "signed_ratio_below_founder_minimum" {
		t.Fatalf("expected low signed ratio rejection, got %+v", lowSigned)
	}

	slashed := founderBadgeForValidator(
		ValidatorPoolEntry{ID: "HOME3", SignedRatioBPS: 9900},
		ValidatorRecord{ID: "HOME3", Status: ValidatorActive, TotalSlashes: 1},
		1,
		100,
		nil,
	)
	if slashed.Badge || slashed.Reason != "severe_fault" {
		t.Fatalf("expected severe fault rejection, got %+v", slashed)
	}
}

func TestFounderBadgeRequiresConfiguredCutoff(t *testing.T) {
	oldEnabled := FounderValidatorBadgeEnabled
	oldCutoff := FounderValidatorCutoffHeight
	t.Cleanup(func() {
		FounderValidatorBadgeEnabled = oldEnabled
		FounderValidatorCutoffHeight = oldCutoff
	})

	FounderValidatorBadgeEnabled = true
	FounderValidatorCutoffHeight = 0
	badge := founderBadgeForValidator(
		ValidatorPoolEntry{ID: "HOME4", SignedRatioBPS: 9900},
		ValidatorRecord{ID: "HOME4", Status: ValidatorActive},
		1,
		100,
		nil,
	)
	if badge.Badge || badge.Reason != "founder_cutoff_not_configured" {
		t.Fatalf("expected unconfigured cutoff rejection, got %+v", badge)
	}
}

func TestValidatorLeaderboardSort(t *testing.T) {
	entries := []validatorsLeaderboardEntry{
		{ValidatorID: "B", SlotType: "standby", Active: false, FinalScore: 0.99, SignedRatioBPS: 10000, EffectiveStake: 100},
		{ValidatorID: "A", SlotType: "performance", Active: true, FinalScore: 0.90, SignedRatioBPS: 9500, EffectiveStake: 100},
		{ValidatorID: "C", SlotType: "rotation", Active: true, FinalScore: 0.95, SignedRatioBPS: 9400, EffectiveStake: 100},
	}
	sortValidatorLeaderboardEntries(entries)
	if entries[0].ValidatorID != "A" || entries[1].ValidatorID != "C" || entries[2].ValidatorID != "B" {
		t.Fatalf("unexpected leaderboard order: %+v", entries)
	}
	if entries[0].Rank != 1 || entries[1].Rank != 2 || entries[2].Rank != 3 {
		t.Fatalf("unexpected ranks: %+v", entries)
	}
}
