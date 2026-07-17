package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var supplyAuditStateDBKey = []byte("tokenomics:supply-audit:v1")

type SupplyChange struct {
	Minted int64 `json:"minted"`
	Burned int64 `json:"burned"`
}

type SupplyDelta struct {
	Coin       string `json:"coin,omitempty"`
	MintTo     string `json:"mint_to,omitempty"`
	MintAmount int64  `json:"mint_amount,omitempty"`
	BurnFrom   string `json:"burn_from,omitempty"`
	BurnAmount int64  `json:"burn_amount,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// ApplySupplyDelta is the strict production gate for non-consensus MSC supply
// mutations. Consensus replay should use ApplySupplyDeltaAtHeight so historical
// blocks before SupplyCapV2 activation keep their original state root.
func ApplySupplyDelta(ledger *Ledger, delta SupplyDelta) (SupplyChange, error) {
	return applySupplyDelta(ledger, delta, 0, true)
}

func ApplySupplyDeltaAtHeight(ledger *Ledger, delta SupplyDelta, height uint64) (SupplyChange, error) {
	return applySupplyDelta(ledger, delta, height, supplyCapConsensusActive(height))
}

func applySupplyDelta(ledger *Ledger, delta SupplyDelta, height uint64, enforceMaxSupply bool) (SupplyChange, error) {
	var change SupplyChange
	if ledger == nil {
		return change, errors.New("supply delta: nil ledger")
	}
	if delta.MintAmount < 0 || delta.BurnAmount < 0 {
		return change, fmt.Errorf("supply delta: negative amount mint=%d burn=%d", delta.MintAmount, delta.BurnAmount)
	}
	coin := normalizeCoin(delta.Coin)
	if coin == "" {
		coin = CoinSymbol
	}
	if coin != CoinSymbol {
		return change, fmt.Errorf("supply delta: unsupported supply coin %s", coin)
	}
	if delta.MintAmount > 0 && strings.TrimSpace(delta.MintTo) == "" {
		return change, errors.New("supply delta: mint recipient required")
	}
	if delta.BurnAmount > 0 && strings.TrimSpace(delta.BurnFrom) == "" {
		return change, errors.New("supply delta: burn source required")
	}

	previous := ledger.Clone()
	if delta.BurnAmount > 0 {
		change.Burned = burnCoinsFromAddress(ledger, coin, delta.BurnFrom, delta.BurnAmount)
	}
	if delta.MintAmount > 0 {
		current := currentCoinSupply(ledger, coin)
		if enforceMaxSupply && current+delta.MintAmount > FixedTotalSupply {
			*ledger = previous.Clone()
			return change, fmt.Errorf(
				"supply delta: mint exceeds max supply reason=%s current=%d mint=%d max=%d",
				strings.TrimSpace(delta.Reason),
				current,
				delta.MintAmount,
				FixedTotalSupply,
			)
		}
		addBalance(ledger, coin, delta.MintTo, int(delta.MintAmount))
		change.Minted = delta.MintAmount
	}
	if _, err := validateSupplyTransition(height, &previous, ledger, change); err != nil {
		if change.Minted == 0 && change.Burned > 0 &&
			currentCoinSupply(ledger, CoinSymbol) < currentCoinSupply(&previous, CoinSymbol) &&
			strings.Contains(err.Error(), "max supply exceeded") {
			return change, nil
		}
		*ledger = previous.Clone()
		return change, err
	}
	return change, nil
}

func (c *SupplyChange) Mint(ledger *Ledger, to string, amount int64) int64 {
	applied, err := ApplySupplyDelta(ledger, SupplyDelta{
		Coin:       CoinSymbol,
		MintTo:     to,
		MintAmount: amount,
		Reason:     "tracked_mint",
	})
	if err != nil {
		return 0
	}
	if c != nil {
		c.Minted += applied.Minted
		c.Burned += applied.Burned
	}
	return applied.Minted
}

func (c *SupplyChange) MintAtHeight(ledger *Ledger, to string, amount int64, height uint64) int64 {
	applied, err := ApplySupplyDeltaAtHeight(ledger, SupplyDelta{
		Coin:       CoinSymbol,
		MintTo:     to,
		MintAmount: amount,
		Reason:     "tracked_consensus_mint",
	}, height)
	if err != nil {
		return 0
	}
	if c != nil {
		c.Minted += applied.Minted
		c.Burned += applied.Burned
	}
	return applied.Minted
}

func (c *SupplyChange) Burn(ledger *Ledger, from string, amount int64) int64 {
	applied, err := ApplySupplyDelta(ledger, SupplyDelta{
		Coin:       CoinSymbol,
		BurnFrom:   from,
		BurnAmount: amount,
		Reason:     "tracked_burn",
	})
	if err != nil {
		return 0
	}
	if c != nil {
		c.Minted += applied.Minted
		c.Burned += applied.Burned
	}
	return applied.Burned
}

func (c *SupplyChange) BurnAtHeight(ledger *Ledger, from string, amount int64, height uint64) int64 {
	applied, err := ApplySupplyDeltaAtHeight(ledger, SupplyDelta{
		Coin:       CoinSymbol,
		BurnFrom:   from,
		BurnAmount: amount,
		Reason:     "tracked_consensus_burn",
	}, height)
	if err != nil {
		return 0
	}
	if c != nil {
		c.Minted += applied.Minted
		c.Burned += applied.Burned
	}
	return applied.Burned
}

func (c *SupplyChange) RouteBlockedReward(ledger *Ledger, amount int64) {
	if amount <= 0 {
		return
	}
	minted := c.Mint(ledger, TREASURY_ADDRESS, amount)
	if minted <= 0 {
		return
	}
	c.Burn(ledger, TREASURY_ADDRESS, minted)
}

func (c *SupplyChange) RouteBlockedRewardAtHeight(ledger *Ledger, amount int64, height uint64) {
	if amount <= 0 {
		return
	}
	minted := c.MintAtHeight(ledger, TREASURY_ADDRESS, amount, height)
	if minted <= 0 {
		return
	}
	c.BurnAtHeight(ledger, TREASURY_ADDRESS, minted, height)
}

func (c *SupplyChange) Add(other SupplyChange) {
	if c == nil {
		return
	}
	c.Minted += other.Minted
	c.Burned += other.Burned
}

type SupplyTransitionAudit struct {
	Height         uint64 `json:"height"`
	BlockHash      string `json:"block_hash,omitempty"`
	PreviousSupply int64  `json:"previous_supply"`
	Minted         int64  `json:"minted"`
	Burned         int64  `json:"burned"`
	NewSupply      int64  `json:"new_supply"`
	MaxSupply      int64  `json:"max_supply"`
	InvariantOK    bool   `json:"invariant_ok"`
}

type SupplyAuditState struct {
	GenesisSupply   int64                 `json:"genesis_supply"`
	TotalMinted     int64                 `json:"total_minted"`
	TotalBurned     int64                 `json:"total_burned"`
	LastAuditHeight uint64                `json:"last_audit_height"`
	LastTransition  SupplyTransitionAudit `json:"last_transition"`
}

func validateSupplyState(ledger *Ledger) error {
	if ledger == nil {
		return errors.New("supply invariant: nil ledger")
	}
	for key, amount := range ledger.Balances {
		if strings.HasPrefix(key, normalizeCoin(CoinSymbol)+"|") && amount < 0 {
			return fmt.Errorf("supply invariant: negative balance key=%s amount=%d", key, amount)
		}
	}
	for key, stake := range ledger.Stakes {
		if stake.Amount < 0 {
			return fmt.Errorf("supply invariant: negative stake key=%s amount=%d", key, stake.Amount)
		}
	}
	return nil
}

func supplyCapConsensusActive(height uint64) bool {
	if SupplyCapV2ActivationHeight == 0 {
		return false
	}
	return height == 0 || height >= SupplyCapV2ActivationHeight
}

func validateRestoredLedgerSupplyAtHeight(ledger *Ledger, source string, height uint64) error {
	if err := validateSupplyState(ledger); err != nil {
		return err
	}
	current := currentCoinSupply(ledger, CoinSymbol)
	if supplyCapConsensusActive(height) && current > FixedTotalSupply {
		return fmt.Errorf(
			"supply invariant: restored ledger exceeds max supply source=%s supply=%d max=%d",
			strings.TrimSpace(source),
			current,
			FixedTotalSupply,
		)
	}
	return nil
}

func validateRestoredLedgerSupply(ledger *Ledger, source string) error {
	return validateRestoredLedgerSupplyAtHeight(ledger, source, 0)
}

func validateSupplyTransition(
	height uint64,
	previous *Ledger,
	next *Ledger,
	change SupplyChange,
) (SupplyTransitionAudit, error) {
	audit := SupplyTransitionAudit{
		Height:      height,
		Minted:      change.Minted,
		Burned:      change.Burned,
		MaxSupply:   FixedTotalSupply,
		InvariantOK: false,
	}
	if err := validateSupplyState(previous); err != nil {
		return audit, err
	}
	if err := validateSupplyState(next); err != nil {
		return audit, err
	}
	if change.Minted < 0 || change.Burned < 0 {
		return audit, fmt.Errorf(
			"supply invariant: invalid accounting height=%d minted=%d burned=%d",
			height,
			change.Minted,
			change.Burned,
		)
	}

	audit.PreviousSupply = currentCoinSupply(previous, CoinSymbol)
	audit.NewSupply = currentCoinSupply(next, CoinSymbol)
	expected := audit.PreviousSupply + change.Minted - change.Burned
	if audit.NewSupply != expected {
		return audit, fmt.Errorf(
			"supply invariant: transition mismatch height=%d previous=%d minted=%d burned=%d expected=%d got=%d",
			height,
			audit.PreviousSupply,
			change.Minted,
			change.Burned,
			expected,
			audit.NewSupply,
		)
	}
	if supplyCapConsensusActive(height) && audit.NewSupply > FixedTotalSupply {
		return audit, fmt.Errorf(
			"supply invariant: max supply exceeded height=%d supply=%d max=%d",
			height,
			audit.NewSupply,
			FixedTotalSupply,
		)
	}
	audit.InvariantOK = true
	return audit, nil
}

func (n *Node) recordSupplyTransition(audit SupplyTransitionAudit) {
	if n == nil || !audit.InvariantOK || audit.Height == 0 {
		return
	}
	n.supplyAuditMu.Lock()
	if !n.supplyAuditLoaded {
		n.loadSupplyAuditStateLocked()
	}
	if n.supplyAudit.GenesisSupply == 0 {
		n.supplyAudit.GenesisSupply = audit.PreviousSupply
	}
	if audit.Height > n.supplyAudit.LastAuditHeight {
		n.supplyAudit.TotalMinted += audit.Minted
		n.supplyAudit.TotalBurned += audit.Burned
		n.supplyAudit.LastAuditHeight = audit.Height
		n.supplyAudit.LastTransition = audit
	}
	state := n.supplyAudit
	n.supplyAuditMu.Unlock()
	_ = n.persistSupplyAuditState(state)
}

func (n *Node) supplyAuditSnapshot() SupplyAuditState {
	if n == nil {
		return SupplyAuditState{}
	}
	n.supplyAuditMu.Lock()
	defer n.supplyAuditMu.Unlock()
	if !n.supplyAuditLoaded {
		n.loadSupplyAuditStateLocked()
	}
	if n.supplyAudit.GenesisSupply == 0 {
		n.supplyAudit.GenesisSupply = FixedTotalSupply
	}
	return n.supplyAudit
}

func (n *Node) loadSupplyAuditStateLocked() {
	n.supplyAuditLoaded = true
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return
	}
	_ = n.DB.Meta.View(func(txn *Txn) error {
		item, err := txn.Get(supplyAuditStateDBKey)
		if err != nil {
			return nil
		}
		return item.Value(func(value []byte) error {
			return json.Unmarshal(value, &n.supplyAudit)
		})
	})
}

func (n *Node) persistSupplyAuditState(state SupplyAuditState) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return n.DB.Meta.Update(func(txn *Txn) error {
		return txn.Set(supplyAuditStateDBKey, raw)
	})
}
