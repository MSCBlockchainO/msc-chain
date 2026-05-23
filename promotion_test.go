package main

import "testing"

func TestPromotionRateLimiter(t *testing.T) {
	oldWindow := PromotionWindowSize
	oldMax := MaxPromotionsPerWindow
	PromotionWindowSize = 100
	MaxPromotionsPerWindow = 1
	defer func() {
		PromotionWindowSize = oldWindow
		MaxPromotionsPerWindow = oldMax
	}()

	n := &Node{}
	if !n.allowPromotionAtEpoch(10) {
		t.Fatalf("expected first promotion allowed")
	}
	if n.allowPromotionAtEpoch(10) {
		t.Fatalf("expected rate limit to block second promotion in same window")
	}
	if n.allowPromotionAtEpoch(99) {
		t.Fatalf("expected rate limit to block within same window")
	}
	if !n.allowPromotionAtEpoch(100) {
		t.Fatalf("expected promotion allowed in next window")
	}
}

func TestApplyPromotionConfig(t *testing.T) {
	oldObs := CandidateObservationEpochs
	oldDCS := CandidateDCSMin
	oldUptime := CandidateUptimeMin
	oldDivPct := CandidateDiversityPctMin
	oldDivEpochs := CandidateDiversityEpochs
	oldMax := MaxPromotionsPerWindow
	oldWindow := PromotionWindowSize
	oldSRPMin := CandidateSRPMin
	oldSRPAlpha := CandidateSRPAlpha
	defer func() {
		CandidateObservationEpochs = oldObs
		CandidateDCSMin = oldDCS
		CandidateUptimeMin = oldUptime
		CandidateDiversityPctMin = oldDivPct
		CandidateDiversityEpochs = oldDivEpochs
		MaxPromotionsPerWindow = oldMax
		PromotionWindowSize = oldWindow
		CandidateSRPMin = oldSRPMin
		CandidateSRPAlpha = oldSRPAlpha
	}()

	cfg := PromotionConfig{
		CandidateObservationEpochs: 55,
		CandidateDCSMin:            0.998,
		CandidateUptimeMin:         0.985,
		CandidateDiversityPctMin:   0.65,
		CandidateDiversityEpochs:   55,
		MaxPromotionsPerWindow:     2,
		PromotionWindowSize:        120,
		CandidateSRPMin:            0.997,
		CandidateSRPAlpha:          0.15,
	}
	if !applyPromotionConfig(cfg) {
		t.Fatalf("expected config to apply changes")
	}
	if CandidateObservationEpochs != 55 {
		t.Fatalf("unexpected CandidateObservationEpochs=%d", CandidateObservationEpochs)
	}
	if CandidateDCSMin != 0.998 {
		t.Fatalf("unexpected CandidateDCSMin=%f", CandidateDCSMin)
	}
	if CandidateUptimeMin != 0.985 {
		t.Fatalf("unexpected CandidateUptimeMin=%f", CandidateUptimeMin)
	}
	if CandidateDiversityPctMin != 0.65 {
		t.Fatalf("unexpected CandidateDiversityPctMin=%f", CandidateDiversityPctMin)
	}
	if CandidateDiversityEpochs != 55 {
		t.Fatalf("unexpected CandidateDiversityEpochs=%d", CandidateDiversityEpochs)
	}
	if MaxPromotionsPerWindow != 2 {
		t.Fatalf("unexpected MaxPromotionsPerWindow=%d", MaxPromotionsPerWindow)
	}
	if PromotionWindowSize != 120 {
		t.Fatalf("unexpected PromotionWindowSize=%d", PromotionWindowSize)
	}
	if CandidateSRPMin != 0.997 {
		t.Fatalf("unexpected CandidateSRPMin=%f", CandidateSRPMin)
	}
	if CandidateSRPAlpha != 0.15 {
		t.Fatalf("unexpected CandidateSRPAlpha=%f", CandidateSRPAlpha)
	}
}
