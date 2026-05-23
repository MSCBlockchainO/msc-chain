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

const misbehaviorDBPrefix = "misbehavior:"
const slashActionDBPrefix = "slash_action:v1:"

var slashActionApplyMu sync.Mutex

type slashActionRecord struct {
	Version       int    `json:"version"`
	Validator     string `json:"validator"`
	Reason        string `json:"reason"`
	Height        uint64 `json:"height"`
	BlockHash     string `json:"block_hash,omitempty"`
	EvidenceKey   string `json:"evidence_key"`
	AppliedAtUnix int64  `json:"applied_at_unix"`
}

func misbehaviorDBKey(ev SlashEvidence) []byte {
	ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
	validator := ev.Validator
	reason := canonicalMisbehaviorReason(ev.Reason)
	if reason == "" {
		reason = "unknown"
	}
	sum := sha256.Sum256([]byte(evidenceKey))
	return []byte(fmt.Sprintf("%s%s:%020d:%020d:%s",
		misbehaviorDBPrefix,
		validator,
		ev.Height,
		ev.Timestamp,
		reason+":"+hex.EncodeToString(sum[:]),
	))
}

func normalizeSlashEvidenceForStore(ev SlashEvidence) (SlashEvidence, string) {
	validator := normalizeValidatorID(ev.Validator)
	if validator == "" {
		validator = normalizeValidatorID(ev.ValidatorID)
	}
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

func slashEvidenceKey(ev SlashEvidence) string {
	validator := normalizeValidatorID(ev.Validator)
	if validator == "" {
		validator = normalizeValidatorID(ev.ValidatorID)
	}
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

func (n *Node) persistMisbehaviorEvidence(ev SlashEvidence) {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return
	}
	ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
	if ev.Validator == "" || evidenceKey == "" {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_ = n.DB.State.Update(func(txn *Txn) error {
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set(misbehaviorDBKey(ev), enc)
	})
}

func (n *Node) loadMisbehaviorEvidenceFromDB() error {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil
	}
	prefix := []byte(misbehaviorDBPrefix)
	loaded := make(map[string][]SlashEvidence)
	seen := make(map[string]struct{})
	err := n.DB.State.View(func(txn *Txn) error {
		opts := DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			if item == nil {
				continue
			}
			err := item.Value(func(val []byte) error {
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				var ev SlashEvidence
				if uerr := json.Unmarshal(plain, &ev); uerr != nil {
					return nil
				}
				ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
				if ev.Validator == "" || evidenceKey == "" {
					return nil
				}
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
		existing := n.MisbehaviorLog[validator]
		existingSeen := make(map[string]struct{}, len(existing)+len(entries))
		merged := make([]SlashEvidence, 0, len(existing)+len(entries))
		for _, ev := range existing {
			ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
			if evidenceKey == "" {
				continue
			}
			if _, ok := existingSeen[evidenceKey]; ok {
				continue
			}
			existingSeen[evidenceKey] = struct{}{}
			merged = append(merged, ev)
		}
		for _, ev := range entries {
			ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
			if evidenceKey == "" {
				continue
			}
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

func slashActionKey(validator, reason string, ev SlashEvidence) string {
	validator = normalizeValidatorID(validator)
	reason = canonicalMisbehaviorReason(reason)
	ev, evidenceKey := normalizeSlashEvidenceForStore(ev)
	if validator == "" {
		validator = ev.Validator
	}
	if validator == "" || reason == "" || evidenceKey == "" {
		return ""
	}
	return validator + "|" + reason + "|" + evidenceKey
}

func slashActionDBKey(actionKey string) []byte {
	sum := sha256.Sum256([]byte(strings.TrimSpace(actionKey)))
	return []byte(slashActionDBPrefix + hex.EncodeToString(sum[:]))
}

func (n *Node) slashActionApplied(actionKey string) bool {
	actionKey = strings.TrimSpace(actionKey)
	if actionKey == "" || n == nil || n.DB == nil || n.DB.State == nil {
		return false
	}
	found := false
	_ = n.DB.State.View(func(txn *Txn) error {
		if _, err := txn.Get(slashActionDBKey(actionKey)); err == nil {
			found = true
		}
		return nil
	})
	return found
}

func (n *Node) persistSlashAction(actionKey, validator, reason string, ev SlashEvidence) {
	actionKey = strings.TrimSpace(actionKey)
	if actionKey == "" || n == nil || n.DB == nil || n.DB.State == nil {
		return
	}
	ev, _ = normalizeSlashEvidenceForStore(ev)
	rec := slashActionRecord{
		Version:       1,
		Validator:     normalizeValidatorID(validator),
		Reason:        canonicalMisbehaviorReason(reason),
		Height:        ev.Height,
		BlockHash:     ev.BlockHash,
		EvidenceKey:   actionKey,
		AppliedAtUnix: time.Now().Unix(),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_ = n.DB.State.Update(func(txn *Txn) error {
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set(slashActionDBKey(actionKey), enc)
	})
}

func (n *Node) replayMisbehaviorSlashingThresholds(source string) {
	if n == nil {
		return
	}
	type pair struct {
		validator string
		reason    string
	}
	pairs := make([]pair, 0)
	seen := make(map[string]struct{})
	n.misbehaviorMu.Lock()
	for validator, entries := range n.MisbehaviorLog {
		validator = normalizeValidatorID(validator)
		if validator == "" {
			continue
		}
		for _, ev := range entries {
			reason := canonicalMisbehaviorReason(ev.Reason)
			if reason == "" {
				continue
			}
			key := validator + "|" + reason
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
