package main

import "testing"

func resetValidatorRegistryForTest() {
	GlobalValidatorRegistry.mu.Lock()
	GlobalValidatorRegistry.records = make(map[string]*ValidatorRecord)
	GlobalValidatorRegistry.mu.Unlock()
}

func TestMarkValidatorInactivityPenaltyCooldown(t *testing.T) {
	prevEnabled := ValidatorInactivityPenaltyEnabled
	prevCooldown := ValidatorInactivityPenaltyCooldownBlocks
	defer func() {
		ValidatorInactivityPenaltyEnabled = prevEnabled
		ValidatorInactivityPenaltyCooldownBlocks = prevCooldown
	}()

	resetValidatorRegistryForTest()
	defer resetValidatorRegistryForTest()
	ValidatorInactivityPenaltyEnabled = true
	ValidatorInactivityPenaltyCooldownBlocks = 5

	if ok := MarkValidatorInactivityPenalty("A", 100); !ok {
		t.Fatalf("expected first inactivity penalty marker to succeed")
	}
	if ok := MarkValidatorInactivityPenalty("A", 102); ok {
		t.Fatalf("expected cooldown to block repeated inactivity penalty")
	}
	if ok := MarkValidatorInactivityPenalty("A", 105); !ok {
		t.Fatalf("expected inactivity penalty marker after cooldown")
	}

	rec, ok := GlobalValidatorRegistry.Get("A")
	if !ok || rec == nil {
		t.Fatalf("expected validator record for A")
	}
	if rec.InactivityPenalties != 2 {
		t.Fatalf("expected inactivity penalties=2, got=%d", rec.InactivityPenalties)
	}
	if rec.LastInactivityPenaltyHeight != 105 {
		t.Fatalf("expected last inactivity penalty height=105, got=%d", rec.LastInactivityPenaltyHeight)
	}
}

func TestApplyOfflineInactivityPenaltyBurnsStakeAndJails(t *testing.T) {
	prevEnabled := ValidatorInactivityPenaltyEnabled
	prevBurnBPS := ValidatorInactivityPenaltyBurnBPS
	prevJail := ValidatorInactivityPenaltyJailBlocks
	prevCooldown := ValidatorInactivityPenaltyCooldownBlocks
	defer func() {
		ValidatorInactivityPenaltyEnabled = prevEnabled
		ValidatorInactivityPenaltyBurnBPS = prevBurnBPS
		ValidatorInactivityPenaltyJailBlocks = prevJail
		ValidatorInactivityPenaltyCooldownBlocks = prevCooldown
	}()

	resetValidatorRegistryForTest()
	defer resetValidatorRegistryForTest()
	ValidatorInactivityPenaltyEnabled = true
	ValidatorInactivityPenaltyBurnBPS = 100
	ValidatorInactivityPenaltyJailBlocks = 20
	ValidatorInactivityPenaltyCooldownBlocks = 50

	n := &Node{
		Ledger: Ledger{
			Stakes: map[string]StakeLock{
				"wallet1|A": {ValidatorID: "A", Amount: 1000},
			},
		},
	}

	ApplyValidatorStake("A", 1000, 100)
	n.applyOfflineInactivityPenalty("A", 200)

	rec, ok := GlobalValidatorRegistry.Get("A")
	if !ok || rec == nil {
		t.Fatalf("expected validator record for A")
	}
	if rec.TotalSlashes != 1 {
		t.Fatalf("expected total slashes=1, got=%d", rec.TotalSlashes)
	}
	if rec.DisconnectPattern != 1 {
		t.Fatalf("expected disconnect pattern=1, got=%d", rec.DisconnectPattern)
	}
	if rec.Status != ValidatorJailed {
		t.Fatalf("expected status JAILED, got=%s", rec.Status)
	}
	if rec.JailUntilHeight < 220 {
		t.Fatalf("expected jail until at least 220, got=%d", rec.JailUntilHeight)
	}
	if rec.Stake != 995 {
		t.Fatalf("expected stake=995 after first tier burn, got=%d", rec.Stake)
	}

	gotStake := n.Ledger.Stakes["wallet1|A"].Amount
	if gotStake != 995 {
		t.Fatalf("expected ledger stake=995 after first tier burn, got=%d", gotStake)
	}

	// Cooldown should block repeat penalty at nearby heights.
	n.applyOfflineInactivityPenalty("A", 210)
	if rec.TotalSlashes != 1 {
		t.Fatalf("expected no extra slash during cooldown, got=%d", rec.TotalSlashes)
	}
	if n.Ledger.Stakes["wallet1|A"].Amount != 995 {
		t.Fatalf("expected no extra burn during cooldown, got=%d", n.Ledger.Stakes["wallet1|A"].Amount)
	}
}

func TestApplyOfflineInactivityPenaltyTierProgression(t *testing.T) {
	prevEnabled := ValidatorInactivityPenaltyEnabled
	prevBurnBPS := ValidatorInactivityPenaltyBurnBPS
	prevJail := ValidatorInactivityPenaltyJailBlocks
	prevCooldown := ValidatorInactivityPenaltyCooldownBlocks
	defer func() {
		ValidatorInactivityPenaltyEnabled = prevEnabled
		ValidatorInactivityPenaltyBurnBPS = prevBurnBPS
		ValidatorInactivityPenaltyJailBlocks = prevJail
		ValidatorInactivityPenaltyCooldownBlocks = prevCooldown
	}()

	resetValidatorRegistryForTest()
	defer resetValidatorRegistryForTest()
	ValidatorInactivityPenaltyEnabled = true
	ValidatorInactivityPenaltyBurnBPS = 100
	ValidatorInactivityPenaltyJailBlocks = 20
	ValidatorInactivityPenaltyCooldownBlocks = 1

	n := &Node{
		Ledger: Ledger{
			Stakes: map[string]StakeLock{
				"wallet1|A": {ValidatorID: "A", Amount: 1000},
			},
		},
	}

	ApplyValidatorStake("A", 1000, 100)

	n.applyOfflineInactivityPenalty("A", 200) // tier-1 => 50 bps
	if got := n.Ledger.Stakes["wallet1|A"].Amount; got != 995 {
		t.Fatalf("tier-1 expected stake=995, got=%d", got)
	}

	n.applyOfflineInactivityPenalty("A", 201) // tier-2 => 100 bps
	if got := n.Ledger.Stakes["wallet1|A"].Amount; got != 986 {
		t.Fatalf("tier-2 expected stake=986, got=%d", got)
	}

	n.applyOfflineInactivityPenalty("A", 202) // tier-3 => 200 bps
	if got := n.Ledger.Stakes["wallet1|A"].Amount; got != 967 {
		t.Fatalf("tier-3 expected stake=967, got=%d", got)
	}

	rec, ok := GlobalValidatorRegistry.Get("A")
	if !ok || rec == nil {
		t.Fatalf("expected validator record for A")
	}
	if rec.InactivityPenalties != 3 {
		t.Fatalf("expected inactivity penalties=3, got=%d", rec.InactivityPenalties)
	}
	if rec.TotalSlashes != 3 {
		t.Fatalf("expected total slashes=3, got=%d", rec.TotalSlashes)
	}
	if rec.JailUntilHeight < 262 {
		t.Fatalf("expected jail until at least 262 after tier-3, got=%d", rec.JailUntilHeight)
	}
}
