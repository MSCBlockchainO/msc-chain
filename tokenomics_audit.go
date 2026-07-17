package main

import (
	"encoding/json"
	"net/http"
)

func tokenomicsStakeLocked(ledger *Ledger) int64 {
	if ledger == nil {
		return 0
	}
	var total int64
	for _, stake := range ledger.Stakes {
		if stake.Amount > 0 {
			total += int64(stake.Amount)
		}
	}
	return total
}

func (s *Server) tokenomicsAuditSnapshot() map[string]any {
	ledger := Ledger{}
	var audit SupplyAuditState
	var height uint64
	var genesis *Genesis
	if s != nil && s.Node != nil {
		ledger = s.Node.currentExecutionLedgerClone()
		audit = s.Node.supplyAuditSnapshot()
		if s.Node.Blockchain != nil {
			height = s.Node.Blockchain.Height()
		}
		if loaded, err := loadGenesisFromDisk(resolveGenesisPath(s.Node.DataDir)); err == nil {
			genesis = loaded
		}
	}
	breakdown, err := genesisSupplyBreakdown(&ledger, genesis)
	invariantOK := err == nil
	currentSupply := currentCoinSupply(&ledger, CoinSymbol)
	if currentSupply > FixedTotalSupply {
		invariantOK = false
	}
	validatorLocked := tokenomicsStakeLocked(&ledger)
	circulating := currentSupply - breakdown.Treasury - breakdown.Foundation -
		breakdown.Community - breakdown.Ecosystem - validatorLocked
	if circulating < 0 {
		circulating = 0
	}
	remainingMintable := FixedTotalSupply - currentSupply
	if remainingMintable < 0 {
		remainingMintable = 0
	}
	lastAuditHeight := audit.LastAuditHeight
	if height > lastAuditHeight {
		lastAuditHeight = height
	}
	genesisSupply := audit.GenesisSupply
	if genesisSupply == 0 {
		genesisSupply = FixedTotalSupply
	}
	invariantStatus := "ok"
	if !invariantOK {
		invariantStatus = "failed"
	}
	response := map[string]any{
		"max_supply":         FixedTotalSupply,
		"current_supply":     currentSupply,
		"genesis_supply":     genesisSupply,
		"minted":             audit.TotalMinted,
		"burned":             audit.TotalBurned,
		"circulating":        circulating,
		"treasury":           breakdown.Treasury,
		"foundation":         breakdown.Foundation,
		"validator_locked":   validatorLocked,
		"community":          breakdown.Community,
		"ecosystem":          breakdown.Ecosystem,
		"remaining_mintable": remainingMintable,
		"last_audit_height":  lastAuditHeight,
		"invariant_ok":       invariantOK,
		"invariant_status":   invariantStatus,
		"supply_cap_surplus": supplyCapSurplus(&ledger),
		"genesis_breakdown":  breakdown,
		"last_transition":    audit.LastTransition,
	}
	if err != nil {
		response["invariant_error"] = err.Error()
	}
	if !invariantOK {
		response["alert"] = "Supply Audit Failed"
	}
	return response
}

func (s *Server) handleTokenomicsAudit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.tokenomicsAuditSnapshot())
}
