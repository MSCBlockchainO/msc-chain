package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// `misbehaviorDBPrefix` defines the constant value used by this package.
const misbehaviorDBPrefix = "misbehavior:"
// `slashActionDBPrefix` defines the constant value used by this package.
const slashActionDBPrefix = "slash_action:v1:"

// `slashActionApplyMu` stores the synchronization state protecting shared data.
var slashActionApplyMu sync.Mutex

type slashActionRecord struct {
	// `Version` stores the value associated with this record.
	Version       int    `json:"version"`
	// `Validator` stores whether the related condition is satisfied.
	Validator     string `json:"validator"`
	// `Reason` stores the value associated with this record.
	Reason        string `json:"reason"`
	// `Height` stores the value associated with this record.
	Height        uint64 `json:"height"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash     string `json:"block_hash,omitempty"`
	// `EvidenceKey` stores the key used to access the related value.
	EvidenceKey   string `json:"evidence_key"`
	// `AppliedAtUnix` stores the value associated with this record.
	AppliedAtUnix int64  `json:"applied_at_unix"`
}

// misbehaviorDBKey implements the misbehavior db key helper.
func misbehaviorDBKey(ev SlashEvidence) []byte {
	// `ev` and `evidenceKey` store the key used to access the related value.
	ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
	// `validator` stores whether the related condition is satisfied.
	validator := ev.Validator
	// `reason` stores the value produced by this operation.
	reason := canonicalMisbehaviorReason(ev.Reason)
	if reason == "" {
		reason = "unknown"
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(evidenceKey))
	return []byte(fmt.Sprintf("%s%s:%020d:%020d:%s",
		misbehaviorDBPrefix,
		validator,
		ev.Height,
		ev.Timestamp,
		reason+":"+hex.EncodeToString(sum[:]),
	))
}

// normalizeSlashEvidenceForStore normalizes slash evidence for store.
func normalizeSlashEvidenceForStore(ev SlashEvidence) (SlashEvidence, string) {
	// `validator` stores whether the related condition is satisfied.
	validator := normalizeValidatorID(ev.Validator)
	if validator == "" {
		validator = normalizeValidatorID(ev.ValidatorID)
	}
	// `reason` stores the value produced by this operation.
	reason := canonicalMisbehaviorReason(ev.Reason)
	ev.Validator = validator
	ev.ValidatorID = validator
	ev.Reason = reason
	ev.BlockHash = strings.ToLower(strings.TrimSpace(ev.BlockHash))
	if ev.Timestamp <= 0 {
		ev.Timestamp = time.Now().Unix()
	}
	return ev, slashEvidenceKey(ev)
}

// slashEvidenceKey implements the slash evidence key helper.
func slashEvidenceKey(ev SlashEvidence) string {
	// `validator` stores whether the related condition is satisfied.
	validator := normalizeValidatorID(ev.Validator)
	if validator == "" {
		validator = normalizeValidatorID(ev.ValidatorID)
	}
	// `reason` stores the value produced by this operation.
	reason := canonicalMisbehaviorReason(ev.Reason)
	if validator == "" || reason == "" || ev.Height == 0 {
		return ""
	}
	return fmt.Sprintf("%s|%s|%020d|%s",
		validator,
		reason,
		ev.Height,
		strings.ToLower(strings.TrimSpace(ev.BlockHash)),
	)
}

// persistMisbehaviorEvidence implements the persist misbehavior evidence helper.
func (n *Node) persistMisbehaviorEvidence(ev SlashEvidence) {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return
	}
	// `ev` and `evidenceKey` store the key used to access the related value.
	ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
	if ev.Validator == "" || evidenceKey == "" {
		return
	}
	// `data` and `err` store the error produced by this operation.
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_ = n.DB.State.Update(func(txn *Txn) error {
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set(misbehaviorDBKey(ev), enc)
	})
}

// loadMisbehaviorEvidenceFromDB implements the load misbehavior evidence from db helper.
func (n *Node) loadMisbehaviorEvidenceFromDB() error {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil
	}
	// `prefix` stores the value produced by this operation.
	prefix := []byte(misbehaviorDBPrefix)
	// `loaded` stores the value produced by this operation.
	loaded := make(map[string][]SlashEvidence)
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{})
	// `err` stores the error produced by this operation.
	err := n.DB.State.View(func(txn *Txn) error {
		// `opts` stores the value produced by this operation.
		opts := DefaultIteratorOptions
		opts.Prefix = prefix
		// `it` stores the current position in the related collection.
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			// `item` stores the current position in the related collection.
			item := it.Item()
			if item == nil {
				continue
			}
			// `err` stores the error produced by this operation.
			err := item.Value(func(val []byte) error {
				// `plain` and `derr` store the error produced by this operation.
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				// `ev` stores the value used by this operation.
				var ev SlashEvidence
				// `uerr` stores the error produced by this operation.
				if uerr := json.Unmarshal(plain, &ev); uerr != nil {
					return nil
				}
				// `ev` and `evidenceKey` store the key used to access the related value.
				ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
				if ev.Validator == "" || evidenceKey == "" {
					return nil
				}
				// `ok` stores whether the related condition is satisfied.
				if _, ok := seen[evidenceKey]; ok {
					return nil
				}
				seen[evidenceKey] = struct{}{}
				loaded[ev.Validator] = append(loaded[ev.Validator], ev)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	n.misbehaviorMu.Lock()
	if n.MisbehaviorLog == nil {
		n.MisbehaviorLog = make(map[string][]SlashEvidence)
	}
	// `validator` and `entries` track whether the related condition is satisfied.
	for validator, entries := range loaded {
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Height == entries[j].Height {
				if entries[i].Timestamp == entries[j].Timestamp {
					return slashEvidenceKey(entries[i]) < slashEvidenceKey(entries[j])
				}
				return entries[i].Timestamp < entries[j].Timestamp
			}
			return entries[i].Height < entries[j].Height
		})
		// `existing` stores the value produced by this operation.
		existing := n.MisbehaviorLog[validator]
		// `existingSeen` stores the value produced by this operation.
		existingSeen := make(map[string]struct{}, len(existing)+len(entries))
		// `merged` stores the value produced by this operation.
		merged := make([]SlashEvidence, 0, len(existing)+len(entries))
		// `ev` tracks the current values while iterating.
		for _, ev := range existing {
			// `ev` and `evidenceKey` store the key used to access the related value.
			ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
			if evidenceKey == "" {
				continue
			}
			// `ok` stores whether the related condition is satisfied.
			if _, ok := existingSeen[evidenceKey]; ok {
				continue
			}
			existingSeen[evidenceKey] = struct{}{}
			merged = append(merged, ev)
		}
		// `ev` tracks the current values while iterating.
		for _, ev := range entries {
			// `ev` and `evidenceKey` store the key used to access the related value.
			ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
			if evidenceKey == "" {
				continue
			}
			// `ok` stores whether the related condition is satisfied.
			if _, ok := existingSeen[evidenceKey]; ok {
				continue
			}
			existingSeen[evidenceKey] = struct{}{}
			merged = append(merged, ev)
		}
		sort.Slice(merged, func(i, j int) bool {
			if merged[i].Height == merged[j].Height {
				if merged[i].Timestamp == merged[j].Timestamp {
					return slashEvidenceKey(merged[i]) < slashEvidenceKey(merged[j])
				}
				return merged[i].Timestamp < merged[j].Timestamp
			}
			return merged[i].Height < merged[j].Height
		})
		n.MisbehaviorLog[validator] = merged
	}
	n.misbehaviorMu.Unlock()
	return nil
}

// slashActionKey implements the slash action key helper.
func slashActionKey(validator, reason string, ev SlashEvidence) string {
	validator = normalizeValidatorID(validator)
	reason = canonicalMisbehaviorReason(reason)
	// `ev` and `evidenceKey` store the key used to access the related value.
	ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
	if validator == "" {
		validator = ev.Validator
	}
	if validator == "" || reason == "" || evidenceKey == "" {
		return ""
	}
	return validator + "|" + reason + "|" + evidenceKey
}

// slashActionDBKey implements the slash action db key helper.
func slashActionDBKey(actionKey string) []byte {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(strings.TrimSpace(actionKey)))
	return []byte(slashActionDBPrefix + hex.EncodeToString(sum[:]))
}

// slashActionApplied implements the slash action applied helper.
func (n *Node) slashActionApplied(actionKey string) bool {
	actionKey = strings.TrimSpace(actionKey)
	if actionKey == "" || n == nil || n.DB == nil || n.DB.State == nil {
		return false
	}
	// `found` stores whether the related condition is satisfied.
	found := false
	_ = n.DB.State.View(func(txn *Txn) error {
		// `err` stores the error produced by this operation.
		if _, err := txn.Get(slashActionDBKey(actionKey)); err == nil {
			found = true
		}
		return nil
	})
	return found
}

// persistSlashAction implements the persist slash action helper.
func (n *Node) persistSlashAction(actionKey, validator, reason string, ev SlashEvidence) {
	actionKey = strings.TrimSpace(actionKey)
	if actionKey == "" || n == nil || n.DB == nil || n.DB.State == nil {
		return
	}
	ev, _ = normalizeSlashEvidenceForStore(ev)
	// `rec` stores the value produced by this operation.
	rec := slashActionRecord{
		Version:       1,
		Validator:     normalizeValidatorID(validator),
		Reason:        canonicalMisbehaviorReason(reason),
		Height:        ev.Height,
		BlockHash:     ev.BlockHash,
		EvidenceKey:   actionKey,
		AppliedAtUnix: time.Now().Unix(),
	}
	// `data` and `err` store the error produced by this operation.
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = n.DB.State.Update(func(txn *Txn) error {
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set(slashActionDBKey(actionKey), enc)
	})
}

// replayMisbehaviorSlashingThresholds implements the replay misbehavior slashing thresholds helper.
func (n *Node) replayMisbehaviorSlashingThresholds(source string) {
	if n == nil {
		return
	}
	type pair struct {
		// `validator` stores whether the related condition is satisfied.
		validator string
		// `reason` stores the value associated with this record.
		reason    string
	}
	// `pairs` stores the value produced by this operation.
	pairs := make([]pair, 0)
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{})
	n.misbehaviorMu.Lock()
	// `validator` and `entries` track whether the related condition is satisfied.
	for validator, entries := range n.MisbehaviorLog {
		validator = normalizeValidatorID(validator)
		if validator == "" {
			continue
		}
		// `ev` tracks the current values while iterating.
		for _, ev := range entries {
			// `reason` stores the value produced by this operation.
			reason := canonicalMisbehaviorReason(ev.Reason)
			if reason == "" {
				continue
			}
			// `key` stores the key used to access the related value.
			key := validator + "|" + reason
			// `ok` stores whether the related condition is satisfied.
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			pairs = append(pairs, pair{validator: validator, reason: reason})
		}
	}
	n.misbehaviorMu.Unlock()
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].validator == pairs[j].validator {
			return pairs[i].reason < pairs[j].reason
		}
		return pairs[i].validator < pairs[j].validator
	})
	// `p` tracks the current values while iterating.
	for _, p := range pairs {
		n.CheckSlashingThreshold(p.validator, p.reason)
	}
	if len(pairs) > 0 {
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:   "slash_evidence_replay",
			Reason: strings.TrimSpace(source),
			Fields: map[string]interface{}{
				"validator_reason_pairs": len(pairs),
			},
		})
	}
}
