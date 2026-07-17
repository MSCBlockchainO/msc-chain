package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type SnapshotMetaRecord struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash,omitempty"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root,omitempty"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash,omitempty"`
	// `ValidatorSetSource` stores whether the related condition is satisfied.
	ValidatorSetSource string `json:"validator_set_source,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string `json:"promotion_window_hash,omitempty"`
	// `NextValidatorSetHash` stores the digest used to identify or verify the related data.
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	// `NextValidatorSetSource` stores the value associated with this record.
	NextValidatorSetSource string `json:"next_validator_set_source,omitempty"`
	// `NextValidatorSetHeight` stores the value associated with this record.
	NextValidatorSetHeight uint64 `json:"next_validator_set_height,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `FinalizedHash` stores the digest used to identify or verify the related data.
	FinalizedHash string `json:"finalized_hash,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root,omitempty"`
	// `Timestamp` stores the value associated with this record.
	Timestamp int64 `json:"timestamp"`
	// `Source` stores the value associated with this record.
	Source string `json:"source,omitempty"`
	// `StateType` stores the value associated with this record.
	StateType string `json:"state_type,omitempty"`
	// `BaseHeight` stores the value associated with this record.
	BaseHeight uint64 `json:"base_height,omitempty"`
}

type TipSnapshotRecord struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `Registry` stores the value associated with this record.
	Registry map[string]ValidatorRecord `json:"registry,omitempty"`
	// `Snapshot` stores the value associated with this record.
	Snapshot *StateSnapshot `json:"snapshot,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string `json:"promotion_window_hash,omitempty"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root,omitempty"`
	// `Source` stores the value associated with this record.
	Source string `json:"source,omitempty"`
	// `UpdatedAt` stores the value associated with this record.
	UpdatedAt int64 `json:"updated_at"`
}

type StateDeltaSnapshot struct {
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `BaseHeight` stores the value associated with this record.
	BaseHeight uint64 `json:"base_height"`
	// `PrevSnapshotHash` stores the digest used to identify or verify the related data.
	PrevSnapshotHash string `json:"prev_snapshot_hash,omitempty"`
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string `json:"snapshot_hash,omitempty"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash,omitempty"`
	// `PrevHash` stores the digest used to identify or verify the related data.
	PrevHash string `json:"prev_hash,omitempty"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash,omitempty"`
	// `LedgerHash` stores the digest used to identify or verify the related data.
	LedgerHash string `json:"ledger_hash,omitempty"`
	// `LedgerStage` stores the value associated with this record.
	LedgerStage string `json:"ledger_stage,omitempty"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root,omitempty"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash,omitempty"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `PromotionWindowHash` stores the digest used to identify or verify the related data.
	PromotionWindowHash string `json:"promotion_window_hash,omitempty"`
	// `ValidatorSetHeight` stores whether the related condition is satisfied.
	ValidatorSetHeight uint64 `json:"validator_set_height,omitempty"`
	// `NextValidatorSetHash` stores the digest used to identify or verify the related data.
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	// `NextValidatorSetRoot` stores the digest used to identify or verify the related data.
	NextValidatorSetRoot string `json:"next_validator_set_root,omitempty"`
	// `NextValidatorSetHeight` stores the value associated with this record.
	NextValidatorSetHeight uint64 `json:"next_validator_set_height,omitempty"`
	// `ActivationHeight` stores the value associated with this record.
	ActivationHeight uint64 `json:"activation_height,omitempty"`
	// `CheckpointHeight` stores the value associated with this record.
	CheckpointHeight uint64 `json:"checkpoint_height,omitempty"`
	// `CheckpointDomain` stores the value associated with this record.
	CheckpointDomain string `json:"checkpoint_domain,omitempty"`
	// `CheckpointProof` stores the value associated with this record.
	CheckpointProof map[string]string `json:"checkpoint_proof,omitempty"`
	// `FinalizedEpoch` stores the value associated with this record.
	FinalizedEpoch uint64 `json:"finalized_epoch,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `FinalizedHash` stores the digest used to identify or verify the related data.
	FinalizedHash string `json:"finalized_hash,omitempty"`
	// `FinalizedStateRoot` stores the digest used to identify or verify the related data.
	FinalizedStateRoot string `json:"finalized_state_root,omitempty"`
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash,omitempty"`
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash,omitempty"`
	// `PreviousEpochAnchorHash` stores the digest used to identify or verify the related data.
	PreviousEpochAnchorHash string `json:"previous_epoch_anchor_hash,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root,omitempty"`
	// `FinalityCertificate` stores the value associated with this record.
	FinalityCertificate *FinalizedEpochCertificate `json:"finality_certificate,omitempty"`
	// `Timestamp` stores the value associated with this record.
	Timestamp int64 `json:"timestamp"`
	// `Validators` stores whether the related condition is satisfied.
	Validators map[string]bool `json:"validators,omitempty"`
	// `PendingValidators` stores the value associated with this record.
	PendingValidators map[string]uint64 `json:"pending_validators,omitempty"`
	// `PendingValidatorRemovals` stores the value associated with this record.
	PendingValidatorRemovals map[string]uint64 `json:"pending_validator_removals,omitempty"`
	// `ChangedBalances` stores the value associated with this record.
	ChangedBalances map[string]int `json:"changed_balances,omitempty"`
	// `DeletedBalances` stores the value associated with this record.
	DeletedBalances []string `json:"deleted_balances,omitempty"`
	// `ChangedNonces` stores the value associated with this record.
	ChangedNonces map[string]int `json:"changed_nonces,omitempty"`
	// `DeletedNonces` stores the value associated with this record.
	DeletedNonces []string `json:"deleted_nonces,omitempty"`
	// `ChangedStakes` stores the value associated with this record.
	ChangedStakes map[string]StakeLock `json:"changed_stakes,omitempty"`
	// `DeletedStakes` stores the value associated with this record.
	DeletedStakes []string `json:"deleted_stakes,omitempty"`
	// `ChangedRewardWallets` stores the value associated with this record.
	ChangedRewardWallets map[string]string `json:"changed_reward_wallets,omitempty"`
	// `DeletedRewardWallets` stores the value associated with this record.
	DeletedRewardWallets []string `json:"deleted_reward_wallets,omitempty"`
	// `ChangedUsedValidatorUpdateCerts` stores the value associated with this record.
	ChangedUsedValidatorUpdateCerts map[string]uint64 `json:"changed_used_validator_update_certs,omitempty"`
	// `DeletedUsedValidatorUpdateCerts` stores the value associated with this record.
	DeletedUsedValidatorUpdateCerts []string `json:"deleted_used_validator_update_certs,omitempty"`
	// `ChangedValidatorRegistry` stores the value associated with this record.
	ChangedValidatorRegistry map[string]ValidatorRecord `json:"changed_validator_registry,omitempty"`
	// `DeletedValidatorRegistry` stores the value associated with this record.
	DeletedValidatorRegistry []string `json:"deleted_validator_registry,omitempty"`
	// `ChangedPromotionWindowRecords` stores the value associated with this record.
	ChangedPromotionWindowRecords map[uint64]PromotionWindowRecord `json:"changed_promotion_window_records,omitempty"`
	// `ChangedPromotionWindowReplacements` stores the value associated with this record.
	ChangedPromotionWindowReplacements map[uint64][]PromotionWindowReplacementRecord `json:"changed_promotion_window_replacements,omitempty"`
	// `ChangedStateValidators` stores the value associated with this record.
	ChangedStateValidators map[string]Validator `json:"changed_state_validators,omitempty"`
	// `DeletedStateValidators` stores the value associated with this record.
	DeletedStateValidators []string `json:"deleted_state_validators,omitempty"`
	// `DTL` stores the value associated with this record.
	DTL *DTLState `json:"dtl,omitempty"`
}

// snapshotMetaKey implements the snapshot meta key helper.
func snapshotMetaKey(height uint64) []byte {
	return []byte(fmt.Sprintf("snapshot_meta:%d", height))
}

// snapshotDeltaKey implements the snapshot delta key helper.
func snapshotDeltaKey(height uint64) []byte {
	return []byte(fmt.Sprintf("snapshot_delta:%d", height))
}

// snapshotDeltaHeightKey implements the snapshot delta height key helper.
func snapshotDeltaHeightKey() []byte {
	return []byte("snapshot_delta_height")
}

// tipSnapshotRegistryKey implements the tip snapshot registry key helper.
func tipSnapshotRegistryKey() []byte {
	return []byte("tip_snapshot:registry")
}

// tipSnapshotStateKey implements the tip snapshot state key helper.
func tipSnapshotStateKey() []byte {
	return []byte("tip_snapshot:state")
}

// tipSnapshotMetaKey implements the tip snapshot meta key helper.
func tipSnapshotMetaKey() []byte {
	return []byte("tip_snapshot:meta")
}

// snapshotBaseHeight implements the snapshot base height helper.
func snapshotBaseHeight(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	return height - 1
}

// cloneStateSnapshot clones state snapshot.
func cloneStateSnapshot(snapshot *StateSnapshot) *StateSnapshot {
	if snapshot == nil {
		return nil
	}
	// `clone` stores the value produced by this operation.
	clone := *snapshot
	clone.Ledger = snapshot.Ledger.Clone()
	clone.Validators = copyBoolMap(snapshot.Validators)
	clone.PendingValidators = copySnapshotUint64Map(snapshot.PendingValidators)
	clone.PendingValidatorRemovals = copySnapshotUint64Map(snapshot.PendingValidatorRemovals)
	clone.ValidatorRegistry = copyValidatorRegistrySnapshot(snapshot.ValidatorRegistry)
	clone.StateValidators = copyValidatorMap(snapshot.StateValidators)
	clone.PromotionWindowRecords = copyPromotionWindowRecords(snapshot.PromotionWindowRecords)
	clone.PromotionWindowReplacements = copyPromotionWindowReplacements(snapshot.PromotionWindowReplacements)
	clone.CheckpointProof = copyStringMap(snapshot.CheckpointProof)
	clone.FinalityCertificate = copyFinalizedEpochCertificate(snapshot.FinalityCertificate)
	return &clone
}

// copyFinalizedEpochCertificate copies finalized epoch certificate.
func copyFinalizedEpochCertificate(src *FinalizedEpochCertificate) *FinalizedEpochCertificate {
	if src == nil {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := *src
	out.Signers = append([]string{}, src.Signers...)
	out.Signatures = append([]ValidatorSignature{}, src.Signatures...)
	out.ExecutionResultSignatures = copyStringMap(src.ExecutionResultSignatures)
	out.CommitVoteSignatures = copyStringMap(src.CommitVoteSignatures)
	return &out
}

// copyBoolMap copies bool map.
func copyBoolMap(src map[string]bool) map[string]bool {
	if len(src) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]bool, len(src))
	// `key` and `value` track the key used to access the related value.
	for key, value := range src {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// copyValidatorMap copies validator map.
func copyValidatorMap(src map[string]Validator) map[string]Validator {
	if len(src) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]Validator, len(src))
	// `key` and `value` track the key used to access the related value.
	for key, value := range src {
		value.PubKey = append([]byte{}, value.PubKey...)
		out[key] = value
	}
	return out
}

// copyNestedStringMap copies nested string map.
func copyNestedStringMap(src map[string]map[string]string) map[string]map[string]string {
	if len(src) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]map[string]string, len(src))
	// `key` and `value` track the key used to access the related value.
	for key, value := range src {
		out[key] = copyStringMap(value)
	}
	return out
}

// deepCopyDTLState implements the deep copy dtl state helper.
func deepCopyDTLState(src *DTLState) *DTLState {
	if src == nil {
		return nil
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	// `out` stores the result produced by this operation.
	var out DTLState
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return cloneDTLState(&out)
}

// snapshotMetaFromSnapshot implements the snapshot meta from snapshot helper.
func snapshotMetaFromSnapshot(snapshot *StateSnapshot, source string, stateType string, baseHeight uint64) *SnapshotMetaRecord {
	if snapshot == nil {
		return nil
	}
	populateSnapshotDerivedFields(snapshot)
	// `validatorSetSource` stores whether the related condition is satisfied.
	validatorSetSource := strings.TrimSpace(snapshot.ValidatorSetSource)
	if validatorSetSource == "" {
		validatorSetSource = strings.TrimSpace(normalizeCommittedValidatorAuthoritySource(source))
	}
	// `nextValidatorSetSource` stores the value produced by this operation.
	nextValidatorSetSource := strings.TrimSpace(snapshot.NextValidatorSetSource)
	if nextValidatorSetSource == "" {
		nextValidatorSetSource = validatorSetSource
	}
	return &SnapshotMetaRecord{
		Height:                 snapshot.Height,
		SnapshotHash:           strings.TrimSpace(snapshot.SnapshotHash),
		StateRoot:              strings.TrimSpace(snapshot.StateRoot),
		ValidatorSetHash:       strings.TrimSpace(snapshotValidatorSetHash(snapshot)),
		ValidatorSetSource:     validatorSetSource,
		ValidatorRegistryHash:  strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)),
		PromotionWindowHash:    strings.TrimSpace(snapshot.PromotionWindowHash),
		NextValidatorSetHash:   strings.TrimSpace(snapshot.NextValidatorSetHash),
		NextValidatorSetSource: nextValidatorSetSource,
		NextValidatorSetHeight: snapshot.NextValidatorSetHeight,
		FinalizedHeight:        snapshot.FinalizedHeight,
		FinalizedHash:          strings.TrimSpace(snapshot.FinalizedHash),
		EpochAnchorHash:        strings.TrimSpace(snapshot.EpochAnchorHash),
		FinalityRoot:           strings.TrimSpace(snapshot.FinalityRoot),
		Timestamp:              snapshot.Timestamp,
		Source:                 strings.TrimSpace(source),
		StateType:              strings.TrimSpace(stateType),
		BaseHeight:             baseHeight,
	}
}

// normalizeSnapshotMetaRecord normalizes snapshot meta record.
func normalizeSnapshotMetaRecord(record *SnapshotMetaRecord) {
	if record == nil {
		return
	}
	record.SnapshotHash = strings.TrimSpace(record.SnapshotHash)
	record.StateRoot = strings.TrimSpace(record.StateRoot)
	record.ValidatorSetHash = strings.TrimSpace(record.ValidatorSetHash)
	record.ValidatorSetSource = strings.TrimSpace(record.ValidatorSetSource)
	record.ValidatorRegistryHash = strings.TrimSpace(record.ValidatorRegistryHash)
	record.PromotionWindowHash = strings.TrimSpace(record.PromotionWindowHash)
	record.NextValidatorSetHash = strings.TrimSpace(record.NextValidatorSetHash)
	record.NextValidatorSetSource = strings.TrimSpace(record.NextValidatorSetSource)
	record.FinalizedHash = strings.TrimSpace(record.FinalizedHash)
	record.EpochAnchorHash = strings.TrimSpace(record.EpochAnchorHash)
	record.FinalityRoot = strings.TrimSpace(record.FinalityRoot)
	record.Source = strings.TrimSpace(record.Source)
	record.StateType = strings.TrimSpace(record.StateType)
}

// loadSnapshotMetaRecord implements the load snapshot meta record helper.
func (n *Node) loadSnapshotMetaRecord(height uint64) (*SnapshotMetaRecord, error) {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil, errors.New("snapshot_meta_unavailable")
	}
	// `record` stores the value used by this operation.
	var record SnapshotMetaRecord
	// `err` stores the error produced by this operation.
	var err error
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		err = store.View(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get(snapshotMetaKey(height))
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
				// `dec` and `err` store the error produced by this operation.
				dec, err := decryptDBValue(val)
				if err != nil {
					return err
				}
				return json.Unmarshal(dec, &record)
			})
		})
		if err == nil {
			break
		}
		if !errors.Is(err, ErrKeyNotFound) {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	normalizeSnapshotMetaRecord(&record)
	if record.Height != 0 && record.Height != height {
		return nil, fmt.Errorf("snapshot_meta_height_mismatch got=%d want=%d", record.Height, height)
	}
	if record.Height == 0 {
		record.Height = height
	}
	return &record, nil
}

// storeSnapshotMetaRecord implements the store snapshot meta record helper.
func (n *Node) storeSnapshotMetaRecord(height uint64, snapshot *StateSnapshot, source string, stateType string, baseHeight uint64) error {
	if n == nil || height == 0 || snapshot == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil
	}
	// `record` stores the value produced by this operation.
	record := snapshotMetaFromSnapshot(snapshot, source, stateType, baseHeight)
	if record == nil {
		return nil
	}
	normalizeSnapshotMetaRecord(record)
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return n.DB.SnapshotMetaStore().Update(func(txn *Txn) error {
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptDBValue(raw)
		if err != nil {
			return err
		}
		return txn.Set(snapshotMetaKey(height), enc)
	})
}

// ensureSnapshotMetaRecord implements the ensure snapshot meta record helper.
func (n *Node) ensureSnapshotMetaRecord(snapshot *StateSnapshot, source string) error {
	if snapshot == nil {
		return nil
	}
	// `record` and `err` store the error produced by this operation.
	record, err := n.loadSnapshotMetaRecord(snapshot.Height)
	if err == nil && record != nil {
		// `expected` stores the value produced by this operation.
		expected := snapshotMetaFromSnapshot(snapshot, record.Source, record.StateType, record.BaseHeight)
		normalizeSnapshotMetaRecord(expected)
		if strings.EqualFold(record.SnapshotHash, expected.SnapshotHash) &&
			strings.EqualFold(record.StateRoot, expected.StateRoot) &&
			strings.EqualFold(record.ValidatorSetHash, expected.ValidatorSetHash) &&
			strings.EqualFold(record.ValidatorSetSource, expected.ValidatorSetSource) &&
			strings.EqualFold(record.ValidatorRegistryHash, expected.ValidatorRegistryHash) &&
			strings.EqualFold(record.PromotionWindowHash, expected.PromotionWindowHash) &&
			strings.EqualFold(record.NextValidatorSetHash, expected.NextValidatorSetHash) &&
			strings.EqualFold(record.NextValidatorSetSource, expected.NextValidatorSetSource) &&
			record.NextValidatorSetHeight == expected.NextValidatorSetHeight {
			return nil
		}
	}
	return n.storeSnapshotMetaRecord(snapshot.Height, snapshot, source, "committed_full", snapshotBaseHeight(snapshot.Height))
}

// loadTipSnapshotRecord implements the load tip snapshot record helper.
func (n *Node) loadTipSnapshotRecord(key []byte) (*TipSnapshotRecord, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, errors.New("tip_snapshot_unavailable")
	}
	// `record` stores the value used by this operation.
	var record TipSnapshotRecord
	// `err` stores the error produced by this operation.
	var err error
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotStoresForRead() {
		err = store.View(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get(key)
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
				// `dec` and `err` store the error produced by this operation.
				dec, err := decryptDBValue(val)
				if err != nil {
					return err
				}
				return json.Unmarshal(dec, &record)
			})
		})
		if err == nil {
			break
		}
		if !errors.Is(err, ErrKeyNotFound) {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	if record.Snapshot != nil {
		populateSnapshotDerivedFields(record.Snapshot)
	}
	return &record, nil
}

// loadTipSnapshotState implements the load tip snapshot state helper.
func (n *Node) loadTipSnapshotState() (*TipSnapshotRecord, error) {
	return n.loadTipSnapshotRecord(tipSnapshotStateKey())
}

// loadTipSnapshotRegistry implements the load tip snapshot registry helper.
func (n *Node) loadTipSnapshotRegistry() (*TipSnapshotRecord, error) {
	return n.loadTipSnapshotRecord(tipSnapshotRegistryKey())
}

// loadTipSnapshotMeta implements the load tip snapshot meta helper.
func (n *Node) loadTipSnapshotMeta() (*TipSnapshotRecord, error) {
	return n.loadTipSnapshotRecord(tipSnapshotMetaKey())
}

// storeTipSnapshotRecords implements the store tip snapshot records helper.
func (n *Node) storeTipSnapshotRecords(snapshot *StateSnapshot, source string) error {
	if n == nil || snapshot == nil || snapshot.Height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	populateSnapshotDerivedFields(snapshot)
	// `updatedAt` stores the value produced by this operation.
	updatedAt := time.Now().Unix()
	// `stateRecord` stores the value produced by this operation.
	stateRecord := TipSnapshotRecord{
		Height:                snapshot.Height,
		Snapshot:              cloneStateSnapshot(snapshot),
		ValidatorRegistryHash: strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)),
		PromotionWindowHash:   strings.TrimSpace(snapshotPromotionWindowHash(snapshot)),
		StateRoot:             strings.TrimSpace(snapshot.StateRoot),
		Source:                strings.TrimSpace(source),
		UpdatedAt:             updatedAt,
	}
	// `registryRecord` stores the value produced by this operation.
	registryRecord := TipSnapshotRecord{
		Height:                snapshot.Height,
		Registry:              copyValidatorRegistrySnapshot(snapshot.ValidatorRegistry),
		ValidatorRegistryHash: strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)),
		PromotionWindowHash:   strings.TrimSpace(snapshotPromotionWindowHash(snapshot)),
		StateRoot:             strings.TrimSpace(snapshot.StateRoot),
		Source:                strings.TrimSpace(source),
		UpdatedAt:             updatedAt,
	}
	// `metaRecord` stores the value produced by this operation.
	metaRecord := TipSnapshotRecord{
		Height:                snapshot.Height,
		ValidatorRegistryHash: strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)),
		PromotionWindowHash:   strings.TrimSpace(snapshotPromotionWindowHash(snapshot)),
		StateRoot:             strings.TrimSpace(snapshot.StateRoot),
		Source:                strings.TrimSpace(source),
		UpdatedAt:             updatedAt,
	}
	return n.DB.SnapshotStore().Update(func(txn *Txn) error {
		// `write` stores the value produced by this operation.
		write := func(key []byte, value TipSnapshotRecord) error {
			// `raw` and `err` store the error produced by this operation.
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			// `enc` and `err` store the error produced by this operation.
			enc, err := encryptDBValue(raw)
			if err != nil {
				return err
			}
			return txn.Set(key, enc)
		}
		// `err` stores the error produced by this operation.
		if err := write(tipSnapshotStateKey(), stateRecord); err != nil {
			return err
		}
		// `err` stores the error produced by this operation.
		if err := write(tipSnapshotRegistryKey(), registryRecord); err != nil {
			return err
		}
		return write(tipSnapshotMetaKey(), metaRecord)
	})
}

// clearTipSnapshotRecords implements the clear tip snapshot records helper.
func (n *Node) clearTipSnapshotRecords() error {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotStoresForRead() {
		// `err` stores the error produced by this operation.
		if err := store.Update(func(txn *Txn) error {
			// `key` tracks the key used to access the related value.
			for _, key := range [][]byte{tipSnapshotStateKey(), tipSnapshotRegistryKey(), tipSnapshotMetaKey()} {
				// `err` stores the error produced by this operation.
				if err := txn.Delete(key); err != nil && !errors.Is(err, ErrKeyNotFound) {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// clearStaleTipSnapshotRecordsAboveHeight implements the clear stale tip snapshot records above height helper.
func (n *Node) clearStaleTipSnapshotRecordsAboveHeight(height uint64) error {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	// `load` tracks the current values while iterating.
	for _, load := range []func() (*TipSnapshotRecord, error){
		n.loadTipSnapshotState,
		n.loadTipSnapshotRegistry,
		n.loadTipSnapshotMeta,
	} {
		// `record` and `err` store the error produced by this operation.
		record, err := load()
		if err == nil && record != nil && record.Height > height {
			return n.clearTipSnapshotRecords()
		}
		if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return err
		}
	}
	return nil
}

// pruneSnapshotMetaAboveHeight implements the prune snapshot meta above height helper.
func (n *Node) pruneSnapshotMetaAboveHeight(height uint64) error {
	if n == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil
	}
	// `prefix` stores the value produced by this operation.
	prefix := []byte("snapshot_meta:")
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		// `err` stores the error produced by this operation.
		if err := store.Update(func(txn *Txn) error {
			// `it` stores the current position in the related collection.
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				// `key` stores the key used to access the related value.
				key := append([]byte(nil), it.Item().Key()...)
				// `parts` stores the value produced by this operation.
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				// `h` and `err` store the error produced by this operation.
				h, err := strconv.ParseUint(string(parts[1]), 10, 64)
				if err != nil || h <= height {
					continue
				}
				// `err` stores the error produced by this operation.
				if err := txn.Delete(key); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// pruneSnapshotMetaBelowHeight implements the prune snapshot meta below height helper.
func (n *Node) pruneSnapshotMetaBelowHeight(retainFromHeight uint64) error {
	if n == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil || retainFromHeight == 0 {
		return nil
	}
	// `prefix` stores the value produced by this operation.
	prefix := []byte("snapshot_meta:")
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		// `err` stores the error produced by this operation.
		if err := store.Update(func(txn *Txn) error {
			// `it` stores the current position in the related collection.
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				// `key` stores the key used to access the related value.
				key := append([]byte(nil), it.Item().Key()...)
				// `parts` stores the value produced by this operation.
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				// `h` and `err` store the error produced by this operation.
				h, err := strconv.ParseUint(string(parts[1]), 10, 64)
				if err != nil || h >= retainFromHeight {
					continue
				}
				// `err` stores the error produced by this operation.
				if err := txn.Delete(key); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// pruneSnapshotDeltasAboveHeight implements the prune snapshot deltas above height helper.
func (n *Node) pruneSnapshotDeltasAboveHeight(height uint64) error {
	if n == nil || n.DB == nil {
		return nil
	}
	if n.DB.SnapshotStore() != nil {
		// `prefix` stores the value produced by this operation.
		prefix := []byte("snapshot_delta:")
		// `store` tracks the current values while iterating.
		for _, store := range n.DB.SnapshotStoresForRead() {
			// `err` stores the error produced by this operation.
			if err := store.Update(func(txn *Txn) error {
				// `it` stores the current position in the related collection.
				it := txn.NewIterator(DefaultIteratorOptions)
				defer it.Close()
				for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
					// `key` stores the key used to access the related value.
					key := append([]byte(nil), it.Item().Key()...)
					// `parts` stores the value produced by this operation.
					parts := bytes.Split(key, []byte(":"))
					if len(parts) != 2 {
						continue
					}
					// `h` and `err` store the error produced by this operation.
					h, err := strconv.ParseUint(string(parts[1]), 10, 64)
					if err != nil || h <= height {
						continue
					}
					// `err` stores the error produced by this operation.
					if err := txn.Delete(key); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return err
			}
		}
	}
	if n.DB.SnapshotMetaStore() == nil {
		return nil
	}
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		// `err` stores the error produced by this operation.
		if err := store.Update(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get(snapshotDeltaHeightKey())
			if err != nil {
				if errors.Is(err, ErrKeyNotFound) {
					return nil
				}
				return err
			}
			// `latest` stores the value used by this operation.
			var latest uint64
			// `err` stores the error produced by this operation.
			if err := item.Value(func(val []byte) error {
				if len(val) != 8 {
					return nil
				}
				latest = binary.BigEndian.Uint64(val)
				return nil
			}); err != nil {
				return err
			}
			if latest > height {
				return txn.Delete(snapshotDeltaHeightKey())
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// pruneSnapshotDeltasBelowHeight implements the prune snapshot deltas below height helper.
func (n *Node) pruneSnapshotDeltasBelowHeight(retainFromHeight uint64) error {
	if n == nil || n.DB == nil || retainFromHeight == 0 {
		return nil
	}
	if n.DB.SnapshotStore() == nil {
		return nil
	}
	// `prefix` stores the value produced by this operation.
	prefix := []byte("snapshot_delta:")
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotStoresForRead() {
		// `err` stores the error produced by this operation.
		if err := store.Update(func(txn *Txn) error {
			// `it` stores the current position in the related collection.
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				// `key` stores the key used to access the related value.
				key := append([]byte(nil), it.Item().Key()...)
				// `parts` stores the value produced by this operation.
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				// `h` and `err` store the error produced by this operation.
				h, err := strconv.ParseUint(string(parts[1]), 10, 64)
				if err != nil || h >= retainFromHeight {
					continue
				}
				// `err` stores the error produced by this operation.
				if err := txn.Delete(key); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// storeCommittedStateSnapshotRecord implements the store committed state snapshot record helper.
func (n *Node) storeCommittedStateSnapshotRecord(snapshot *StateSnapshot, source string) error {
	if n == nil || snapshot == nil || snapshot.Height == 0 {
		return errors.New("snapshot record invalid")
	}
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return errors.New("snapshot db not initialized")
	}
	input := snapshot
	snapshot = cloneStateSnapshot(snapshot)
	if snapshot == nil {
		return errors.New("snapshot record clone failed")
	}
	// Persist the snapshot exactly as verified. Local snapshot creation attaches
	// promotion-window state before sealing the canonical hash; re-attaching
	// mutable local DB state here would rewrite downloaded/imported snapshots and
	// let different nodes persist different identities for the same payload.
	populateSnapshotDerivedFields(snapshot)
	if reason := snapshotIntrinsicMetadataRejectReason(snapshot); reason != "" {
		return fmt.Errorf("snapshot record metadata invalid: %s", reason)
	}
	if reason := snapshotExecutionAuthorityRejectReason(snapshot); reason != "" {
		return fmt.Errorf("snapshot record metadata invalid: %s", reason)
	}
	snapshot.SnapshotHash = snapshotCanonicalHash(snapshot)
	// `data` and `err` store the error produced by this operation.
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	// `key` stores the key used to access the related value.
	key := []byte(fmt.Sprintf("snapshot:%d", snapshot.Height))
	// `err` stores the error produced by this operation.
	if err := n.DB.SnapshotStore().Update(func(txn *Txn) error {
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set(key, enc)
	}); err != nil {
		return err
	}
	// `stored` and `err` store the error produced by this operation.
	stored, err := readSnapshotFromStores([]*DB{n.DB.SnapshotStore()}, key)
	if err != nil {
		return fmt.Errorf("snapshot write verification failed height=%d: %w", snapshot.Height, err)
	}
	if stored == nil ||
		stored.Height != snapshot.Height ||
		!strings.EqualFold(strings.TrimSpace(stored.SnapshotHash), strings.TrimSpace(snapshot.SnapshotHash)) {
		return fmt.Errorf("snapshot write verification mismatch height=%d", snapshot.Height)
	}
	if n.DB.SnapshotMetaStore() == nil {
		return errors.New("snapshot meta db not initialized")
	}
	// `err` stores the error produced by this operation.
	if err := n.DB.SnapshotMetaStore().Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:latest"), key)
	}); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := n.storeSnapshotMetaRecord(snapshot.Height, snapshot, source, "committed_full", snapshotBaseHeight(snapshot.Height)); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := n.storeTipSnapshotRecords(snapshot, source); err != nil {
		return err
	}
	n.exportSnapshotArtifactsBestEffort(snapshot, source)
	*input = *snapshot
	return nil
}

// committedStateSnapshotRecordExists implements the committed state snapshot record exists helper.
func (n *Node) committedStateSnapshotRecordExists(height uint64) bool {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return false
	}
	// `key` stores the key used to access the related value.
	key := []byte(fmt.Sprintf("snapshot:%d", height))
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotStoresForRead() {
		// `err` stores the error produced by this operation.
		err := store.View(func(txn *Txn) error {
			// `err` stores the error produced by this operation.
			_, err := txn.Get(key)
			return err
		})
		if err == nil {
			return true
		}
	}
	return false
}

// resolveCommittedStateSnapshotFromStorage implements the resolve committed state snapshot from storage helper.
func (n *Node) resolveCommittedStateSnapshotFromStorage(height uint64) (*StateSnapshot, string, bool) {
	if n == nil || height == 0 {
		return nil, "", false
	}
	// `snap` and `err` store the error produced by this operation.
	if snap, err := n.GetSnapshot(height); err == nil && snap != nil {
		// `ok` stores whether the related condition is satisfied.
		if ok, _ := n.verifySnapshotAgainstLocalBlockDetailed(snap); ok {
			_ = n.ensureSnapshotMetaRecord(snap, "snapshot_committed")
			return snap, strings.TrimSpace(snap.SnapshotHash), true
		}
	}
	return nil, "", false
}

// resolveTrustedExecutionSnapshotFromStorage implements the resolve trusted execution snapshot from storage helper.
func (n *Node) resolveTrustedExecutionSnapshotFromStorage(height uint64) (*StateSnapshot, string, bool) {
	if n == nil || height == 0 {
		return nil, "", false
	}
	// `snap` and `err` store the error produced by this operation.
	snap, err := n.GetSnapshot(height)
	if err != nil || snap == nil {
		return nil, "", false
	}
	// `ok` stores whether the related condition is satisfied.
	if ok, _ := n.verifySnapshotAgainstLocalBlockDetailed(snap); !ok {
		return nil, "", false
	}
	if !snapshotHasTrustedExecutionLedger(snap) {
		// Older snapshot versions did not identify whether their ledger was
		// captured before or after post-commit effects. Never promote that
		// ambiguous state into execution authority.
		if snap.Version < SnapshotVersion {
			return nil, "", false
		}
		// `upgraded` stores the value produced by this operation.
		upgraded := cloneStateSnapshot(snap)
		if upgraded == nil {
			return nil, "", false
		}
		upgraded.LedgerStage = snapshotLedgerStageExecution
		upgraded.SnapshotHash = ""
		populateSnapshotDerivedFields(upgraded)
		// `err` stores the error produced by this operation.
		if err := n.storeCommittedStateSnapshotRecord(upgraded, "snapshot_committed_upgrade"); err == nil {
			snap = upgraded
		} else {
			return nil, "", false
		}
	}
	_ = n.ensureSnapshotMetaRecord(snap, "snapshot_committed")
	return snap, strings.TrimSpace(snap.SnapshotHash), true
}

// resolveCommittedStateSnapshotFromTipRecord implements the resolve committed state snapshot from tip record helper.
func (n *Node) resolveCommittedStateSnapshotFromTipRecord(height uint64) (*StateSnapshot, string, bool, string) {
	if n == nil || height == 0 {
		return nil, "", false, "none"
	}
	// `record` and `err` store the error produced by this operation.
	record, err := n.loadTipSnapshotState()
	if err != nil || record == nil || record.Snapshot == nil || record.Height != height {
		return nil, "", false, "tip_snapshot_unavailable"
	}
	// `snapshot` stores the value produced by this operation.
	snapshot := cloneStateSnapshot(record.Snapshot)
	if snapshot == nil {
		return nil, "", false, "tip_snapshot_unavailable"
	}
	// `ok` and `reason` store whether the related condition is satisfied.
	if ok, reason := n.verifySnapshotAgainstLocalBlockDetailed(snapshot); !ok {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "tip_snapshot_invalid"
		}
		return nil, "", false, reason
	}
	if record.StateRoot != "" && !strings.EqualFold(strings.TrimSpace(record.StateRoot), strings.TrimSpace(snapshot.StateRoot)) {
		return nil, "", false, "tip_state_root_mismatch"
	}
	if record.ValidatorRegistryHash != "" && !strings.EqualFold(strings.TrimSpace(record.ValidatorRegistryHash), strings.TrimSpace(snapshotValidatorRegistryHash(snapshot))) {
		return nil, "", false, "tip_registry_hash_mismatch"
	}
	// `err` stores the error produced by this operation.
	if err := n.storeCommittedStateSnapshotRecord(snapshot, "tip_snapshot_repair"); err != nil {
		return nil, "", false, fmt.Sprintf("persist_failed err=%v", err)
	}
	if len(snapshot.ValidatorRegistry) > 0 {
		_ = n.storeValidatorRegistrySnapshotRecord(height, snapshot.ValidatorRegistry)
	}
	return snapshot, strings.TrimSpace(snapshot.SnapshotHash), true, ""
}

// materializeCommittedTipStateSnapshot implements the materialize committed tip state snapshot helper.
func (n *Node) materializeCommittedTipStateSnapshot(height uint64, reason string) error {
	if n == nil || height == 0 || n.Blockchain == nil {
		return errors.New("tip_snapshot_unavailable")
	}
	if n.Blockchain.Height() != height {
		return errors.New("not_chain_tip")
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.Blockchain.GetBlock(height)
	if !ok {
		return errors.New("tip_block_unavailable")
	}
	// `err` stores the error produced by this operation.
	if err := n.CreateSnapshot(height, strings.TrimSpace(block.BlockHash)); err != nil {
		return fmt.Errorf("create_snapshot_failed reason=%s err=%w", strings.TrimSpace(reason), err)
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.GetSnapshot(height)
	if err != nil || snapshot == nil {
		return fmt.Errorf("create_snapshot_load_failed reason=%s err=%v", strings.TrimSpace(reason), err)
	}
	// `ok` and `detail` store whether the related condition is satisfied.
	if ok, detail := n.snapshotMatchesLocalAnchorDetailed(snapshot); !ok {
		_ = n.deleteStoredSnapshotHeight(height)
		_ = n.refreshLatestSnapshotPointer()
		detail = strings.TrimSpace(detail)
		if detail == "" {
			detail = "anchor_verification_failed"
		}
		return fmt.Errorf("create_snapshot_anchor_verification_failed reason=%s detail=%s", strings.TrimSpace(reason), detail)
	}
	return nil
}

// shouldAutoCreateSnapshotAtHeight implements the should auto create snapshot at height helper.
func shouldAutoCreateSnapshotAtHeight(height uint64) bool {
	if height == 0 {
		return false
	}
	// `interval` stores the value currently being processed.
	interval := syncCheckpointIntervalBlocks()
	if interval <= 1 {
		return true
	}
	if height <= 2 {
		// Bootstrap compatibility for genesis/early chain startup.
		return true
	}
	return height%interval == 0
}

// shouldBypassSnapshotCheckpointDeferral implements the should bypass snapshot checkpoint deferral helper.
func shouldBypassSnapshotCheckpointDeferral(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "resolver_tip_missing", "integrity_monitor", "startup", "sync_complete":
		return true
	default:
		return false
	}
}

// shouldPeerFetchCommittedTipSnapshot implements the should peer fetch committed tip snapshot helper.
func shouldPeerFetchCommittedTipSnapshot(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "resolver_tip_missing", "integrity_monitor", "startup", "snapshot_create_worker", "sync_complete":
		return true
	default:
		return false
	}
}

// persistDurableSyncAnchorAsync implements the persist durable sync anchor async helper.
func (n *Node) persistDurableSyncAnchorAsync(height uint64, reason string) {
	if n == nil || height == 0 {
		return
	}
	n.SafeGo(fmt.Sprintf("durable_sync_anchor_%d", height), func() {
		// `source` and `ok` store whether the related condition is satisfied.
		source, ok := n.ensureCommittedTipStateSnapshot(height, "sync_complete")
		if !ok {
			// `key` stores the key used to access the related value.
			key := fmt.Sprintf("durable_sync_anchor_failed:%d", height)
			if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
				log.Printf("[SNAPSHOT-ANCHOR] status=failed height=%d reason=%s", height, strings.TrimSpace(reason))
			}
			return
		}
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("durable_sync_anchor_stored:%d", height)
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-ANCHOR] status=stored height=%d reason=%s source=%s",
				height, strings.TrimSpace(reason), strings.TrimSpace(source))
		}
	})
}

// fetchCommittedTipSnapshotFromPeers implements the fetch committed tip snapshot from peers helper.
func (n *Node) fetchCommittedTipSnapshotFromPeers(height uint64, reason string) (string, bool) {
	if n == nil || height == 0 || !shouldPeerFetchCommittedTipSnapshot(reason) {
		return "none", false
	}
	if n.Host == nil || len(n.Host.Network().Peers()) == 0 {
		return "none", false
	}
	if !n.shouldLogLivenessReason(fmt.Sprintf("snapshot_peer_fetch_attempt:%d", height), 5*time.Second) {
		return "none", false
	}
	if DebugSync || DebugConsensus {
		fmt.Printf("[SNAPSHOT-FETCH] height=%d reason=%s source=peers\n", height, strings.TrimSpace(reason))
	}
	// `result` and `err` store the error produced by this operation.
	result, err := n.downloadTrustedSnapshotAndStore(height, height, true, false, false, false)
	if err != nil {
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("snapshot_peer_fetch_failed:%d", height)
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-FETCH] height=%d reason=%s source=peers status=failed err=%v",
				height,
				strings.TrimSpace(reason),
				err,
			)
		}
		return "none", false
	}
	if result == nil || result.Snapshot == nil || result.Snapshot.Height != height {
		return "none", false
	}
	// `source` stores the value produced by this operation.
	source := strings.TrimSpace(result.Source)
	if source == "" {
		source = "trusted_snapshot_download"
	}
	// `displaySource` stores the value produced by this operation.
	displaySource := displaySnapshotAuthoritySource(source)
	if displaySource == "none" {
		displaySource = strings.TrimSpace(source)
	}
	// `key` stores the key used to access the related value.
	key := fmt.Sprintf("snapshot_peer_fetch_ok:%d", height)
	if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
		log.Printf("[SNAPSHOT-FETCH] height=%d reason=%s source=%s status=ok",
			height,
			strings.TrimSpace(reason),
			displaySource,
		)
	}
	return source, true
}

// ensureCommittedTipStateSnapshot implements the ensure committed tip state snapshot helper.
func (n *Node) ensureCommittedTipStateSnapshot(height uint64, reason string) (source string, ok bool) {
	if n == nil || height == 0 || n.Blockchain == nil || n.Blockchain.Height() != height {
		return "none", false
	}
	// `ok` stores whether the related condition is satisfied.
	if _, _, ok := n.resolveCommittedStateSnapshotFromStorage(height); ok {
		return "snapshot_committed", true
	}
	// `ok` stores whether the related condition is satisfied.
	if _, _, ok, _ := n.resolveCommittedStateSnapshotFromTipRecord(height); ok {
		return "tip_snapshot_repair", true
	}
	if !shouldAutoCreateSnapshotAtHeight(height) && !shouldBypassSnapshotCheckpointDeferral(reason) {
		return "checkpoint_interval_deferred", true
	}
	// `err` stores the error produced by this operation.
	if err := n.materializeCommittedTipStateSnapshot(height, reason); err != nil {
		// `source` and `fetched` store the value produced by this operation.
		if source, fetched := n.fetchCommittedTipSnapshotFromPeers(height, reason); fetched {
			return source, true
		}
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("snapshot_materialize_failed:%d", height)
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[WARN] committed tip snapshot materialization failed height=%d reason=%s err=%v",
				height,
				strings.TrimSpace(reason),
				err,
			)
		}
		return "none", false
	}
	// `ok` stores whether the related condition is satisfied.
	if _, _, ok := n.resolveCommittedStateSnapshotFromStorage(height); ok {
		return "tip_create_snapshot_repair", true
	}
	// `source` and `fetched` store the value produced by this operation.
	if source, fetched := n.fetchCommittedTipSnapshotFromPeers(height, reason); fetched {
		return source, true
	}
	return "none", false
}

// latestCommittedSnapshotMeta implements the latest committed snapshot meta helper.
func (n *Node) latestCommittedSnapshotMeta() (*StateSnapshot, *SnapshotMetaRecord, string, error) {
	if n == nil {
		return nil, nil, "", errors.New("node unavailable")
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.verifiedStoredSnapshotAtOrBelow(0)
	if err != nil {
		return nil, nil, "", err
	}
	if snapshot == nil {
		return nil, nil, "", errors.New("snapshot unavailable")
	}
	// `meta` and `metaErr` store the error produced by this operation.
	meta, metaErr := n.loadSnapshotMetaRecord(snapshot.Height)
	if metaErr != nil || meta == nil {
		// `source` stores the value produced by this operation.
		source := "snapshot_committed"
		// `err` stores the error produced by this operation.
		if err := n.ensureSnapshotMetaRecord(snapshot, source); err == nil {
			meta, _ = n.loadSnapshotMetaRecord(snapshot.Height)
		}
		if meta == nil {
			meta = snapshotMetaFromSnapshot(snapshot, source, "committed_full", snapshotBaseHeight(snapshot.Height))
		}
	}
	// `source` stores the value produced by this operation.
	source := "snapshot_committed"
	if meta != nil && strings.TrimSpace(meta.Source) != "" {
		source = strings.TrimSpace(meta.Source)
	}
	return snapshot, meta, source, nil
}

// createCommittedTipSnapshot implements the create committed tip snapshot helper.
func (n *Node) createCommittedTipSnapshot(_ string, force bool) (*StateSnapshot, *SnapshotMetaRecord, string, error) {
	if n == nil {
		return nil, nil, "", errors.New("node unavailable")
	}
	if n.Blockchain == nil {
		return nil, nil, "", errors.New("blockchain unavailable")
	}
	// `tip` stores the value produced by this operation.
	tip := n.Blockchain.Height()
	if tip == 0 {
		return nil, nil, "", errors.New("committed tip unavailable")
	}
	if !force {
		// `snapshot`, `source`, and `ok` store whether the related condition is satisfied.
		if snapshot, _, source, ok := n.ResolveCommittedStateSnapshot(tip); ok && snapshot != nil {
			// `meta` and `err` store the error produced by this operation.
			meta, err := n.loadSnapshotMetaRecord(snapshot.Height)
			if err != nil || meta == nil {
				_ = n.ensureSnapshotMetaRecord(snapshot, source)
				meta, _ = n.loadSnapshotMetaRecord(snapshot.Height)
			}
			if meta == nil {
				meta = snapshotMetaFromSnapshot(snapshot, source, "committed_full", snapshotBaseHeight(snapshot.Height))
			}
			return snapshot, meta, source, nil
		}
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := n.Blockchain.GetBlock(tip)
	if !ok {
		return nil, nil, "", errors.New("committed tip block unavailable")
	}
	// `err` stores the error produced by this operation.
	if err := n.CreateSnapshot(tip, strings.TrimSpace(block.BlockHash)); err != nil {
		return nil, nil, "", err
	}
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.GetSnapshot(tip)
	if err != nil {
		return nil, nil, "", err
	}
	if snapshot == nil {
		return nil, nil, "", errors.New("snapshot unavailable after create")
	}
	// `ok` and `detail` store whether the related condition is satisfied.
	if ok, detail := n.snapshotMatchesLocalAnchorDetailed(snapshot); !ok {
		_ = n.deleteStoredSnapshotHeight(tip)
		_ = n.refreshLatestSnapshotPointer()
		detail = strings.TrimSpace(detail)
		if detail == "" {
			detail = "anchor_verification_failed"
		}
		return nil, nil, "", fmt.Errorf("created snapshot failed local anchor verification: %s", detail)
	}
	// `source` stores the value produced by this operation.
	source := "create_snapshot"
	// `meta` and `metaErr` store the error produced by this operation.
	meta, metaErr := n.loadSnapshotMetaRecord(snapshot.Height)
	if metaErr != nil || meta == nil {
		_ = n.ensureSnapshotMetaRecord(snapshot, source)
		meta, _ = n.loadSnapshotMetaRecord(snapshot.Height)
	}
	if meta == nil {
		meta = snapshotMetaFromSnapshot(snapshot, source, "committed_full", snapshotBaseHeight(snapshot.Height))
	}
	return snapshot, meta, source, nil
}

// ResolveCommittedStateSnapshot resolves committed state snapshot.
func (n *Node) ResolveCommittedStateSnapshot(height uint64) (*StateSnapshot, string, string, bool) {
	if n == nil || height == 0 {
		return nil, "", "none", false
	}
	// `snap`, `hash`, and `ok` store whether the related condition is satisfied.
	if snap, hash, ok := n.resolveCommittedStateSnapshotFromStorage(height); ok {
		return snap, hash, "snapshot_committed", true
	}
	if n.Blockchain == nil || n.Blockchain.Height() != height {
		return nil, "", "none", false
	}
	// `failureReason` stores the value produced by this operation.
	failureReason := "tip_snapshot_unavailable"
	// `snap`, `hash`, `ok`, and `reason` store whether the related condition is satisfied.
	if snap, hash, ok, reason := n.resolveCommittedStateSnapshotFromTipRecord(height); ok {
		return snap, hash, "tip_snapshot_repair", true
	} else if strings.TrimSpace(reason) != "" {
		failureReason = strings.TrimSpace(reason)
	}
	// `source` and `repaired` store the value produced by this operation.
	if source, repaired := n.ensureCommittedTipStateSnapshot(height, "resolver_tip_missing"); repaired {
		// `snap`, `hash`, and `ok` store whether the related condition is satisfied.
		if snap, hash, ok := n.resolveCommittedStateSnapshotFromStorage(height); ok {
			return snap, hash, source, true
		}
		failureReason = "post_materialization_unavailable"
	}
	if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_missing:%d", height), livenessReasonLogCooldown) {
		log.Printf("[SNAPSHOT-MISSING] height=%d reason=%s", height, strings.TrimSpace(failureReason))
	}
	return nil, "", "none", false
}

// resolveCommittedStateSnapshot implements the resolve committed state snapshot helper.
func (n *Node) resolveCommittedStateSnapshot(height uint64) (*StateSnapshot, string, string, bool) {
	return n.ResolveCommittedStateSnapshot(height)
}

// ResolveCommittedRegistrySnapshot resolves committed registry snapshot.
func (n *Node) ResolveCommittedRegistrySnapshot(height uint64) (map[string]ValidatorRecord, string, string, bool) {
	return n.resolveCommittedValidatorRegistrySnapshot(height)
}

// diffIntMap implements the diff int map helper.
func diffIntMap(prev map[string]int, current map[string]int) (map[string]int, []string) {
	// `changed` stores the value produced by this operation.
	changed := make(map[string]int)
	// `deleted` stores the value produced by this operation.
	deleted := make([]string, 0)
	// `key` and `value` track the key used to access the related value.
	for key, value := range current {
		// `old` and `ok` store whether the related condition is satisfied.
		if old, ok := prev[key]; !ok || old != value {
			changed[key] = value
		}
	}
	// `key` tracks the key used to access the related value.
	for key := range prev {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := current[key]; !ok {
			deleted = append(deleted, key)
		}
	}
	if len(changed) == 0 {
		changed = nil
	}
	if len(deleted) == 0 {
		deleted = nil
	}
	return changed, deleted
}

// diffStringMap implements the diff string map helper.
func diffStringMap(prev map[string]string, current map[string]string) (map[string]string, []string) {
	// `changed` stores the value produced by this operation.
	changed := make(map[string]string)
	// `deleted` stores the value produced by this operation.
	deleted := make([]string, 0)
	// `key` and `value` track the key used to access the related value.
	for key, value := range current {
		// `old` and `ok` store whether the related condition is satisfied.
		if old, ok := prev[key]; !ok || old != value {
			changed[key] = value
		}
	}
	// `key` tracks the key used to access the related value.
	for key := range prev {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := current[key]; !ok {
			deleted = append(deleted, key)
		}
	}
	if len(changed) == 0 {
		changed = nil
	}
	if len(deleted) == 0 {
		deleted = nil
	}
	return changed, deleted
}

// diffUint64Map implements the diff uint64 map helper.
func diffUint64Map(prev map[string]uint64, current map[string]uint64) (map[string]uint64, []string) {
	// `changed` stores the value produced by this operation.
	changed := make(map[string]uint64)
	// `deleted` stores the value produced by this operation.
	deleted := make([]string, 0)
	// `key` and `value` track the key used to access the related value.
	for key, value := range current {
		// `old` and `ok` store whether the related condition is satisfied.
		if old, ok := prev[key]; !ok || old != value {
			changed[key] = value
		}
	}
	// `key` tracks the key used to access the related value.
	for key := range prev {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := current[key]; !ok {
			deleted = append(deleted, key)
		}
	}
	if len(changed) == 0 {
		changed = nil
	}
	if len(deleted) == 0 {
		deleted = nil
	}
	return changed, deleted
}

// diffStakeMap implements the diff stake map helper.
func diffStakeMap(prev map[string]StakeLock, current map[string]StakeLock) (map[string]StakeLock, []string) {
	// `changed` stores the value produced by this operation.
	changed := make(map[string]StakeLock)
	// `deleted` stores the value produced by this operation.
	deleted := make([]string, 0)
	// `key` and `value` track the key used to access the related value.
	for key, value := range current {
		// `old` and `ok` store whether the related condition is satisfied.
		if old, ok := prev[key]; !ok || !reflect.DeepEqual(old, value) {
			changed[key] = value
		}
	}
	// `key` tracks the key used to access the related value.
	for key := range prev {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := current[key]; !ok {
			deleted = append(deleted, key)
		}
	}
	if len(changed) == 0 {
		changed = nil
	}
	if len(deleted) == 0 {
		deleted = nil
	}
	return changed, deleted
}

// diffValidatorRegistryMap implements the diff validator registry map helper.
func diffValidatorRegistryMap(prev map[string]ValidatorRecord, current map[string]ValidatorRecord) (map[string]ValidatorRecord, []string) {
	// `changed` stores the value produced by this operation.
	changed := make(map[string]ValidatorRecord)
	// `deleted` stores the value produced by this operation.
	deleted := make([]string, 0)
	// `key` and `value` track the key used to access the related value.
	for key, value := range current {
		// `old` and `ok` store whether the related condition is satisfied.
		if old, ok := prev[key]; !ok || !reflect.DeepEqual(old, value) {
			changed[key] = value
		}
	}
	// `key` tracks the key used to access the related value.
	for key := range prev {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := current[key]; !ok {
			deleted = append(deleted, key)
		}
	}
	if len(changed) == 0 {
		changed = nil
	}
	if len(deleted) == 0 {
		deleted = nil
	}
	return changed, deleted
}

// diffValidatorMap implements the diff validator map helper.
func diffValidatorMap(prev map[string]Validator, current map[string]Validator) (map[string]Validator, []string) {
	// `changed` stores the value produced by this operation.
	changed := make(map[string]Validator)
	// `deleted` stores the value produced by this operation.
	deleted := make([]string, 0)
	// `key` and `value` track the key used to access the related value.
	for key, value := range current {
		// `old` and `ok` store whether the related condition is satisfied.
		if old, ok := prev[key]; !ok || !reflect.DeepEqual(old, value) {
			value.PubKey = append([]byte{}, value.PubKey...)
			changed[key] = value
		}
	}
	// `key` tracks the key used to access the related value.
	for key := range prev {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := current[key]; !ok {
			deleted = append(deleted, key)
		}
	}
	if len(changed) == 0 {
		changed = nil
	}
	if len(deleted) == 0 {
		deleted = nil
	}
	return changed, deleted
}

// diffNestedStringMap implements the diff nested string map helper.
func diffNestedStringMap(prev map[string]map[string]string, current map[string]map[string]string) (map[string]map[string]string, []string) {
	// `changed` stores the value produced by this operation.
	changed := make(map[string]map[string]string)
	// `deleted` stores the value produced by this operation.
	deleted := make([]string, 0)
	// `key` and `value` track the key used to access the related value.
	for key, value := range current {
		// `old` and `ok` store whether the related condition is satisfied.
		if old, ok := prev[key]; !ok || !reflect.DeepEqual(old, value) {
			changed[key] = copyStringMap(value)
		}
	}
	// `key` tracks the key used to access the related value.
	for key := range prev {
		// `ok` stores whether the related condition is satisfied.
		if _, ok := current[key]; !ok {
			deleted = append(deleted, key)
		}
	}
	if len(changed) == 0 {
		changed = nil
	}
	if len(deleted) == 0 {
		deleted = nil
	}
	return changed, deleted
}

// buildStateDeltaSnapshot builds state delta snapshot.
func buildStateDeltaSnapshot(base *StateSnapshot, current *StateSnapshot) (*StateDeltaSnapshot, error) {
	if base == nil || current == nil {
		return nil, fmt.Errorf("snapshot_delta_unavailable")
	}
	populateSnapshotDerivedFields(base)
	populateSnapshotDerivedFields(current)
	if current.Height == 0 || current.Height <= base.Height {
		return nil, fmt.Errorf("snapshot_delta_height_invalid base=%d current=%d", base.Height, current.Height)
	}
	if current.Height != base.Height+1 {
		return nil, fmt.Errorf("snapshot_delta_gap base=%d current=%d", base.Height, current.Height)
	}
	// `changedBalances` and `deletedBalances` store the value produced by this operation.
	changedBalances, deletedBalances := diffIntMap(base.Ledger.Balances, current.Ledger.Balances)
	// `changedNonces` and `deletedNonces` store the value produced by this operation.
	changedNonces, deletedNonces := diffIntMap(base.Ledger.Nonces, current.Ledger.Nonces)
	// `changedStakes` and `deletedStakes` store the value produced by this operation.
	changedStakes, deletedStakes := diffStakeMap(base.Ledger.Stakes, current.Ledger.Stakes)
	// `changedRewardWallets` and `deletedRewardWallets` store the value produced by this operation.
	changedRewardWallets, deletedRewardWallets := diffStringMap(base.Ledger.ValidatorRewardWallets, current.Ledger.ValidatorRewardWallets)
	// `changedCerts` and `deletedCerts` store the value produced by this operation.
	changedCerts, deletedCerts := diffUint64Map(base.Ledger.UsedValidatorUpdateCerts, current.Ledger.UsedValidatorUpdateCerts)
	// `changedRegistry` and `deletedRegistry` store the value produced by this operation.
	changedRegistry, deletedRegistry := diffValidatorRegistryMap(base.ValidatorRegistry, current.ValidatorRegistry)
	// `changedStateValidators` and `deletedStateValidators` store the value produced by this operation.
	changedStateValidators, deletedStateValidators := diffValidatorMap(base.StateValidators, current.StateValidators)
	// `changedPromotionRecords` stores the value used by this operation.
	var changedPromotionRecords map[uint64]PromotionWindowRecord
	if !reflect.DeepEqual(base.PromotionWindowRecords, current.PromotionWindowRecords) {
		changedPromotionRecords = copyPromotionWindowRecords(current.PromotionWindowRecords)
	}
	// `changedPromotionReplacements` stores the value used by this operation.
	var changedPromotionReplacements map[uint64][]PromotionWindowReplacementRecord
	if !reflect.DeepEqual(base.PromotionWindowReplacements, current.PromotionWindowReplacements) {
		changedPromotionReplacements = copyPromotionWindowReplacements(current.PromotionWindowReplacements)
	}
	// `dtlCopy` stores the value used by this operation.
	var dtlCopy *DTLState
	if !reflect.DeepEqual(base.Ledger.DTL, current.Ledger.DTL) {
		dtlCopy = deepCopyDTLState(current.Ledger.DTL)
	}
	return &StateDeltaSnapshot{
		Height:                             current.Height,
		BaseHeight:                         base.Height,
		PrevSnapshotHash:                   strings.TrimSpace(base.SnapshotHash),
		SnapshotHash:                       strings.TrimSpace(current.SnapshotHash),
		BlockHash:                          strings.TrimSpace(current.BlockHash),
		PrevHash:                           strings.TrimSpace(current.PrevHash),
		GenesisHash:                        strings.TrimSpace(current.GenesisHash),
		LedgerHash:                         strings.TrimSpace(current.LedgerHash),
		LedgerStage:                        strings.TrimSpace(current.LedgerStage),
		StateRoot:                          strings.TrimSpace(current.StateRoot),
		ValidatorSetHash:                   strings.TrimSpace(snapshotValidatorSetHash(current)),
		ValidatorSetRoot:                   strings.TrimSpace(snapshotValidatorSetRoot(current)),
		ValidatorRegistryHash:              strings.TrimSpace(snapshotValidatorRegistryHash(current)),
		PromotionWindowHash:                strings.TrimSpace(snapshotPromotionWindowHash(current)),
		ValidatorSetHeight:                 current.ValidatorSetHeight,
		NextValidatorSetHash:               strings.TrimSpace(current.NextValidatorSetHash),
		NextValidatorSetRoot:               strings.TrimSpace(current.NextValidatorSetRoot),
		NextValidatorSetHeight:             current.NextValidatorSetHeight,
		ActivationHeight:                   current.ActivationHeight,
		CheckpointHeight:                   current.CheckpointHeight,
		CheckpointDomain:                   strings.TrimSpace(current.CheckpointDomain),
		CheckpointProof:                    copyStringMap(current.CheckpointProof),
		FinalizedEpoch:                     current.FinalizedEpoch,
		FinalizedHeight:                    current.FinalizedHeight,
		FinalizedHash:                      strings.TrimSpace(current.FinalizedHash),
		FinalizedStateRoot:                 strings.TrimSpace(current.FinalizedStateRoot),
		FinalizedValidatorSetHash:          strings.TrimSpace(current.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot:          strings.TrimSpace(current.FinalizedValidatorSetRoot),
		EpochAnchorHash:                    strings.TrimSpace(current.EpochAnchorHash),
		PreviousEpochAnchorHash:            strings.TrimSpace(current.PreviousEpochAnchorHash),
		FinalityRoot:                       strings.TrimSpace(current.FinalityRoot),
		FinalityCertificate:                copyFinalizedEpochCertificate(current.FinalityCertificate),
		Timestamp:                          current.Timestamp,
		Validators:                         copyBoolMap(current.Validators),
		PendingValidators:                  copySnapshotUint64Map(current.PendingValidators),
		PendingValidatorRemovals:           copySnapshotUint64Map(current.PendingValidatorRemovals),
		ChangedBalances:                    changedBalances,
		DeletedBalances:                    deletedBalances,
		ChangedNonces:                      changedNonces,
		DeletedNonces:                      deletedNonces,
		ChangedStakes:                      changedStakes,
		DeletedStakes:                      deletedStakes,
		ChangedRewardWallets:               changedRewardWallets,
		DeletedRewardWallets:               deletedRewardWallets,
		ChangedUsedValidatorUpdateCerts:    changedCerts,
		DeletedUsedValidatorUpdateCerts:    deletedCerts,
		ChangedValidatorRegistry:           changedRegistry,
		DeletedValidatorRegistry:           deletedRegistry,
		ChangedPromotionWindowRecords:      changedPromotionRecords,
		ChangedPromotionWindowReplacements: changedPromotionReplacements,
		ChangedStateValidators:             changedStateValidators,
		DeletedStateValidators:             deletedStateValidators,
		DTL:                                dtlCopy,
	}, nil
}

// applyStateDeltaSnapshot applies state delta snapshot.
func applyStateDeltaSnapshot(base *StateSnapshot, delta *StateDeltaSnapshot) (*StateSnapshot, error) {
	if base == nil || delta == nil {
		return nil, fmt.Errorf("snapshot_delta_unavailable")
	}
	if delta.BaseHeight != base.Height {
		return nil, fmt.Errorf("snapshot_delta_base_mismatch base=%d delta=%d", base.Height, delta.BaseHeight)
	}
	// `next` stores the value produced by this operation.
	next := cloneStateSnapshot(base)
	if next == nil {
		return nil, fmt.Errorf("snapshot_delta_clone_failed")
	}
	next.Height = delta.Height
	next.BlockHash = delta.BlockHash
	next.PrevHash = delta.PrevHash
	next.GenesisHash = delta.GenesisHash
	next.LedgerHash = delta.LedgerHash
	next.LedgerStage = delta.LedgerStage
	next.StateRoot = delta.StateRoot
	next.ValidatorSetHash = delta.ValidatorSetHash
	next.ValidatorSetRoot = delta.ValidatorSetRoot
	next.ValidatorRegistryHash = delta.ValidatorRegistryHash
	next.PromotionWindowHash = delta.PromotionWindowHash
	next.ValidatorSetHeight = delta.ValidatorSetHeight
	next.NextValidatorSetHash = delta.NextValidatorSetHash
	next.NextValidatorSetRoot = delta.NextValidatorSetRoot
	next.NextValidatorSetHeight = delta.NextValidatorSetHeight
	next.ActivationHeight = delta.ActivationHeight
	next.CheckpointHeight = delta.CheckpointHeight
	next.CheckpointDomain = delta.CheckpointDomain
	next.CheckpointProof = copyStringMap(delta.CheckpointProof)
	next.FinalizedEpoch = delta.FinalizedEpoch
	next.FinalizedHeight = delta.FinalizedHeight
	next.FinalizedHash = delta.FinalizedHash
	next.FinalizedStateRoot = delta.FinalizedStateRoot
	next.FinalizedValidatorSetHash = delta.FinalizedValidatorSetHash
	next.FinalizedValidatorSetRoot = delta.FinalizedValidatorSetRoot
	next.EpochAnchorHash = delta.EpochAnchorHash
	next.PreviousEpochAnchorHash = delta.PreviousEpochAnchorHash
	next.FinalityRoot = delta.FinalityRoot
	next.FinalityCertificate = copyFinalizedEpochCertificate(delta.FinalityCertificate)
	next.Timestamp = delta.Timestamp
	next.SnapshotHash = ""
	next.Validators = copyBoolMap(delta.Validators)
	next.PendingValidators = copySnapshotUint64Map(delta.PendingValidators)
	next.PendingValidatorRemovals = copySnapshotUint64Map(delta.PendingValidatorRemovals)
	// `key` and `value` track the key used to access the related value.
	for key, value := range delta.ChangedBalances {
		if next.Ledger.Balances == nil {
			next.Ledger.Balances = make(map[string]int)
		}
		next.Ledger.Balances[key] = value
	}
	// `key` tracks the key used to access the related value.
	for _, key := range delta.DeletedBalances {
		delete(next.Ledger.Balances, key)
	}
	// `key` and `value` track the key used to access the related value.
	for key, value := range delta.ChangedNonces {
		if next.Ledger.Nonces == nil {
			next.Ledger.Nonces = make(map[string]int)
		}
		next.Ledger.Nonces[key] = value
	}
	// `key` tracks the key used to access the related value.
	for _, key := range delta.DeletedNonces {
		delete(next.Ledger.Nonces, key)
	}
	// `key` and `value` track the key used to access the related value.
	for key, value := range delta.ChangedStakes {
		if next.Ledger.Stakes == nil {
			next.Ledger.Stakes = make(map[string]StakeLock)
		}
		next.Ledger.Stakes[key] = value
	}
	// `key` tracks the key used to access the related value.
	for _, key := range delta.DeletedStakes {
		delete(next.Ledger.Stakes, key)
	}
	// `key` and `value` track the key used to access the related value.
	for key, value := range delta.ChangedRewardWallets {
		if next.Ledger.ValidatorRewardWallets == nil {
			next.Ledger.ValidatorRewardWallets = make(map[string]string)
		}
		next.Ledger.ValidatorRewardWallets[key] = value
	}
	// `key` tracks the key used to access the related value.
	for _, key := range delta.DeletedRewardWallets {
		delete(next.Ledger.ValidatorRewardWallets, key)
	}
	// `key` and `value` track the key used to access the related value.
	for key, value := range delta.ChangedUsedValidatorUpdateCerts {
		if next.Ledger.UsedValidatorUpdateCerts == nil {
			next.Ledger.UsedValidatorUpdateCerts = make(map[string]uint64)
		}
		next.Ledger.UsedValidatorUpdateCerts[key] = value
	}
	// `key` tracks the key used to access the related value.
	for _, key := range delta.DeletedUsedValidatorUpdateCerts {
		delete(next.Ledger.UsedValidatorUpdateCerts, key)
	}
	if delta.DTL != nil {
		next.Ledger.DTL = deepCopyDTLState(delta.DTL)
	}
	next.StateMerkleRoot = LedgerStateMerkleRoot(next.Ledger)
	// `key` and `value` track the key used to access the related value.
	for key, value := range delta.ChangedValidatorRegistry {
		if next.ValidatorRegistry == nil {
			next.ValidatorRegistry = make(map[string]ValidatorRecord)
		}
		next.ValidatorRegistry[key] = value
	}
	// `key` tracks the key used to access the related value.
	for _, key := range delta.DeletedValidatorRegistry {
		delete(next.ValidatorRegistry, key)
	}
	// `key` and `value` track the key used to access the related value.
	for key, value := range delta.ChangedStateValidators {
		if next.StateValidators == nil {
			next.StateValidators = make(map[string]Validator)
		}
		value.PubKey = append([]byte{}, value.PubKey...)
		next.StateValidators[key] = value
	}
	// `key` tracks the key used to access the related value.
	for _, key := range delta.DeletedStateValidators {
		delete(next.StateValidators, key)
	}
	if len(delta.ChangedPromotionWindowRecords) > 0 {
		if next.PromotionWindowRecords == nil {
			next.PromotionWindowRecords = make(map[uint64]PromotionWindowRecord)
		}
		// `window` and `record` track the current values while iterating.
		for window, record := range delta.ChangedPromotionWindowRecords {
			next.PromotionWindowRecords[window] = normalizePromotionWindowRecord(record)
		}
	}
	if len(delta.ChangedPromotionWindowReplacements) > 0 {
		if next.PromotionWindowReplacements == nil {
			next.PromotionWindowReplacements = make(map[uint64][]PromotionWindowReplacementRecord)
		}
		// `window` and `replacements` track the current values while iterating.
		for window, replacements := range delta.ChangedPromotionWindowReplacements {
			// `copied` stores the value produced by this operation.
			copied := make([]PromotionWindowReplacementRecord, 0, len(replacements))
			// `replacement` tracks the current values while iterating.
			for _, replacement := range replacements {
				copied = append(copied, normalizePromotionWindowReplacement(replacement))
			}
			next.PromotionWindowReplacements[window] = copied
		}
	}
	populateSnapshotDerivedFields(next)
	// `computedSnapshotHash` stores the digest used to identify or verify the related data.
	computedSnapshotHash := snapshotCanonicalHash(next)
	if strings.TrimSpace(delta.SnapshotHash) != "" && !strings.EqualFold(strings.TrimSpace(computedSnapshotHash), strings.TrimSpace(delta.SnapshotHash)) {
		return nil, fmt.Errorf("snapshot_delta_hash_mismatch got=%s want=%s", computedSnapshotHash, delta.SnapshotHash)
	}
	next.SnapshotHash = computedSnapshotHash
	if strings.TrimSpace(delta.ValidatorRegistryHash) != "" && !strings.EqualFold(strings.TrimSpace(snapshotValidatorRegistryHash(next)), strings.TrimSpace(delta.ValidatorRegistryHash)) {
		return nil, fmt.Errorf("snapshot_delta_registry_hash_mismatch got=%s want=%s", snapshotValidatorRegistryHash(next), delta.ValidatorRegistryHash)
	}
	if strings.TrimSpace(delta.PromotionWindowHash) != "" && !strings.EqualFold(strings.TrimSpace(snapshotPromotionWindowHash(next)), strings.TrimSpace(delta.PromotionWindowHash)) {
		return nil, fmt.Errorf("snapshot_delta_promotion_window_hash_mismatch got=%s want=%s", snapshotPromotionWindowHash(next), delta.PromotionWindowHash)
	}
	return next, nil
}

// loadStateDeltaSnapshot implements the load state delta snapshot helper.
func (n *Node) loadStateDeltaSnapshot(height uint64) (*StateDeltaSnapshot, error) {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, errors.New("snapshot_delta_unavailable")
	}
	// `delta` stores the value used by this operation.
	var delta StateDeltaSnapshot
	// `err` stores the error produced by this operation.
	var err error
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotStoresForRead() {
		err = store.View(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get(snapshotDeltaKey(height))
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
				// `dec` and `err` store the error produced by this operation.
				dec, err := decryptDBValue(val)
				if err != nil {
					return err
				}
				return json.Unmarshal(dec, &delta)
			})
		})
		if err == nil {
			break
		}
		if !errors.Is(err, ErrKeyNotFound) {
			return nil, err
		}
	}
	if err != nil {
		return nil, err
	}
	return &delta, nil
}

// itemKey implements the item key helper.
func itemKey(it *Iterator) []byte {
	if it == nil || it.iter == nil || !it.iter.Valid() {
		return nil
	}
	return append([]byte{}, it.iter.Key()...)
}

// parseUintBytes parses uint bytes.
func parseUintBytes(raw []byte) (uint64, error) {
	return strconv.ParseUint(string(raw), 10, 64)
}

// loadLatestSnapshotDeltaHeight implements the load latest snapshot delta height helper.
func (n *Node) loadLatestSnapshotDeltaHeight() (uint64, bool) {
	if n == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return 0, false
	}
	// `height` stores the value used by this operation.
	var height uint64
	// `err` stores the error produced by this operation.
	var err error
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		err = store.View(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get(snapshotDeltaHeightKey())
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
				if len(val) != 8 {
					return fmt.Errorf("invalid_snapshot_delta_height")
				}
				height = binary.BigEndian.Uint64(val)
				return nil
			})
		})
		if err == nil && height > 0 {
			return height, true
		}
		if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return 0, false
		}
	}
	if n.DB.SnapshotStore() == nil {
		return 0, false
	}
	// `best` stores the value used by this operation.
	var best uint64
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotStoresForRead() {
		_ = store.View(func(txn *Txn) error {
			// `it` stores the current position in the related collection.
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			// `prefix` stores the value produced by this operation.
			prefix := []byte("snapshot_delta:")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				// `key` stores the key used to access the related value.
				key := itemKey(it)
				// `parts` stores the value produced by this operation.
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				// `h` and `parseErr` store the error produced by this operation.
				h, parseErr := parseUintBytes(parts[1])
				if parseErr != nil || h == 0 {
					continue
				}
				if h > best {
					best = h
				}
			}
			return nil
		})
	}
	if best == 0 {
		return 0, false
	}
	return best, true
}

// storeLatestSnapshotDeltaHeight implements the store latest snapshot delta height helper.
func (n *Node) storeLatestSnapshotDeltaHeight(height uint64) error {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil
	}
	// `buf` stores the value used by this operation.
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], height)
	return n.DB.SnapshotMetaStore().Update(func(txn *Txn) error {
		return txn.Set(snapshotDeltaHeightKey(), buf[:])
	})
}

// storeStateDeltaSnapshot implements the store state delta snapshot helper.
func (n *Node) storeStateDeltaSnapshot(delta *StateDeltaSnapshot) error {
	if n == nil || delta == nil || delta.Height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(delta)
	if err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := n.DB.SnapshotStore().Update(func(txn *Txn) error {
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptDBValue(raw)
		if err != nil {
			return err
		}
		return txn.Set(snapshotDeltaKey(delta.Height), enc)
	}); err != nil {
		return err
	}
	return n.storeLatestSnapshotDeltaHeight(delta.Height)
}

// processPendingSnapshotDeltaWork implements the process pending snapshot delta work helper.
func (n *Node) processPendingSnapshotDeltaWork(limit int) (int, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return 0, nil
	}
	// `target` stores the value produced by this operation.
	target := n.getFinalizedHeight()
	if target == 0 && n.Blockchain != nil {
		target = n.Blockchain.Height()
	}
	if target < 2 {
		return 0, nil
	}
	// `latest` and `ok` store whether the related condition is satisfied.
	latest, ok := n.loadLatestSnapshotDeltaHeight()
	// `next` stores the value produced by this operation.
	next := uint64(2)
	if ok && latest >= 2 {
		next = latest + 1
	}
	if next > target {
		return 0, nil
	}
	if limit <= 0 {
		limit = 1
	}
	// `processed` stores the value produced by this operation.
	processed := 0
	for next <= target && processed < limit {
		// `base` and `okBase` store whether the related condition is satisfied.
		base, _, _, okBase := n.ResolveCommittedStateSnapshot(next - 1)
		// `current` and `okCurrent` store whether the related condition is satisfied.
		current, _, _, okCurrent := n.ResolveCommittedStateSnapshot(next)
		if !okBase || !okCurrent {
			break
		}
		// `delta` and `err` store the error produced by this operation.
		delta, err := buildStateDeltaSnapshot(base, current)
		if err != nil {
			return processed, err
		}
		// `err` stores the error produced by this operation.
		if err := n.storeStateDeltaSnapshot(delta); err != nil {
			return processed, err
		}
		processed++
		next++
	}
	return processed, nil
}

// startSnapshotDeltaWorker implements the start snapshot delta worker helper.
func (n *Node) startSnapshotDeltaWorker(ctx context.Context) {
	if n == nil {
		return
	}
	_, _ = n.processPendingSnapshotDeltaWork(8)
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_, _ = n.processPendingSnapshotDeltaWork(8)
		case <-n.shutdownCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// startSnapshotIntegrityMonitor implements the start snapshot integrity monitor helper.
func (n *Node) startSnapshotIntegrityMonitor(ctx context.Context) {
	if n == nil {
		return
	}
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			_ = n.verifySnapshotIntegrityDepth(syncCheckpointIntervalBlocks() * 2)
		case <-n.shutdownCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// startSnapshotCreateWorker implements the start snapshot create worker helper.
func (n *Node) startSnapshotCreateWorker(ctx context.Context) {
	if n == nil {
		return
	}
	// `run` stores the value produced by this operation.
	run := func() {
		if n == nil || n.Blockchain == nil {
			return
		}
		// `tip` stores the value produced by this operation.
		tip := n.Blockchain.Height()
		if tip == 0 {
			return
		}
		if n.shouldDeferNonConsensusCommitMaintenance() {
			return
		}
		// `source` and `ok` store whether the related condition is satisfied.
		source, ok := n.ensureCommittedTipStateSnapshot(tip, "snapshot_create_worker")
		if !ok || source == "snapshot_committed" || source == "checkpoint_interval_deferred" {
			return
		}
		// `key` stores the key used to access the related value.
		key := fmt.Sprintf("snapshot_create_worker:%d:%s", tip, strings.TrimSpace(source))
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-CREATE] height=%d source=%s", tip, strings.TrimSpace(source))
		}
	}
	run()
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			run()
		case <-n.shutdownCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// snapshotIntegrityScanHeight implements the snapshot integrity scan height helper.
func snapshotIntegrityScanHeight(height uint64, tip uint64, interval uint64) bool {
	if height == 0 || tip == 0 || height > tip {
		return false
	}
	if height == tip {
		return true
	}
	if interval == 0 {
		interval = 32
	}
	return height%interval == 0
}

// verifySnapshotIntegrityDepth verifies snapshot integrity depth.
func (n *Node) verifySnapshotIntegrityDepth(depth uint64) error {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil || n.Blockchain == nil {
		return nil
	}
	// `tip` stores the value produced by this operation.
	tip := n.Blockchain.Height()
	if tip == 0 {
		return nil
	}
	if depth == 0 {
		depth = syncCheckpointIntervalBlocks() * 2
	}
	if depth == 0 {
		depth = 64
	}
	// `start` stores the value produced by this operation.
	start := uint64(1)
	if tip > depth {
		start = tip - depth + 1
	}
	// `interval` stores the value currently being processed.
	interval := syncCheckpointIntervalBlocks()
	// `height` stores the value produced by this operation.
	for height := start; height <= tip; height++ {
		if !snapshotIntegrityScanHeight(height, tip, interval) {
			continue
		}
		// `recordExists` stores whether the related condition is satisfied.
		recordExists := n.committedStateSnapshotRecordExists(height)
		if !recordExists && height < tip {
			// Checkpoint-only snapshot policies intentionally leave most historical
			// heights without a committed snapshot record. Avoid loading and
			// verifying snapshot state for those expected gaps; on small nodes that
			// work can otherwise monopolize CPU during every integrity pass.
			if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_materializing:%d", height), livenessReasonLogCooldown) {
				log.Printf("[SNAPSHOT-MATERIALIZING] height=%d chain_tip=%d kind=missing_committed", height, tip)
			}
			continue
		}
		// `snap` and `ok` store whether the related condition is satisfied.
		snap, _, _, ok := n.ResolveCommittedStateSnapshot(height)
		if ok && snap != nil {
			// `meta` and `metaErr` store the error produced by this operation.
			meta, metaErr := n.loadSnapshotMetaRecord(height)
			if metaErr == nil && meta != nil {
				// `expected` stores the value produced by this operation.
				expected := snapshotMetaFromSnapshot(snap, meta.Source, meta.StateType, meta.BaseHeight)
				normalizeSnapshotMetaRecord(expected)
				if !strings.EqualFold(meta.SnapshotHash, expected.SnapshotHash) ||
					!strings.EqualFold(meta.StateRoot, expected.StateRoot) ||
					!strings.EqualFold(meta.ValidatorSetHash, expected.ValidatorSetHash) ||
					!strings.EqualFold(meta.ValidatorRegistryHash, expected.ValidatorRegistryHash) ||
					!strings.EqualFold(meta.PromotionWindowHash, expected.PromotionWindowHash) {
					if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_meta:%d", height), livenessReasonLogCooldown) {
						log.Printf("[SNAPSHOT-INTEGRITY] height=%d kind=meta_mismatch", height)
					}
					// `err` stores the error produced by this operation.
					if err := n.storeSnapshotMetaRecord(height, snap, "integrity_repair", "committed_full", snapshotBaseHeight(height)); err != nil {
						return err
					}
				}
			} else if metaErr != nil {
				// `err` stores the error produced by this operation.
				if err := n.storeSnapshotMetaRecord(height, snap, "integrity_backfill", "committed_full", snapshotBaseHeight(height)); err != nil {
					return err
				}
			}
			continue
		}
		if !recordExists && height == tip {
			// `repaired` stores the value produced by this operation.
			if _, repaired := n.ensureCommittedTipStateSnapshot(height, "integrity_monitor"); repaired {
				continue
			}
		}
		if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_missing:%d", height), livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-INTEGRITY] height=%d kind=missing_or_invalid", height)
		}
		if recordExists {
			// `err` stores the error produced by this operation.
			if err := n.quarantineCommittedSnapshotHeight(height); err != nil {
				return err
			}
		}
		// `rebuilt` and `rebuildErr` store the error produced by this operation.
		if rebuilt, rebuildErr := n.rebuildCommittedSnapshotHeight(height); rebuildErr == nil && rebuilt {
			continue
		} else if rebuildErr != nil && n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_rebuild_error:%d", height), livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-INTEGRITY] height=%d kind=rebuild_error err=%v", height, rebuildErr)
		}
	}
	// `tipMeta` and `err` store the error produced by this operation.
	if tipMeta, err := n.loadTipSnapshotMeta(); err == nil && tipMeta != nil && tipMeta.Height > 0 {
		// `snap` and `ok` store whether the related condition is satisfied.
		if snap, _, _, ok := n.ResolveCommittedStateSnapshot(tipMeta.Height); ok && snap != nil {
			if !strings.EqualFold(strings.TrimSpace(tipMeta.StateRoot), strings.TrimSpace(snap.StateRoot)) ||
				!strings.EqualFold(strings.TrimSpace(tipMeta.ValidatorRegistryHash), strings.TrimSpace(snapshotValidatorRegistryHash(snap))) ||
				!strings.EqualFold(strings.TrimSpace(tipMeta.PromotionWindowHash), strings.TrimSpace(snapshotPromotionWindowHash(snap))) {
				_ = n.clearTipSnapshotRecords()
			}
		}
	}
	return nil
}

// quarantineCommittedSnapshotHeight implements the quarantine committed snapshot height helper.
func (n *Node) quarantineCommittedSnapshotHeight(height uint64) error {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	// `snapshotKey` stores the key used to access the related value.
	snapshotKey := []byte(fmt.Sprintf("snapshot:%d", height))
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotStoresForRead() {
		// `err` stores the error produced by this operation.
		if err := store.Update(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get(snapshotKey)
			if err != nil {
				if errors.Is(err, ErrKeyNotFound) {
					return nil
				}
				return err
			}
			// `raw` stores the value used by this operation.
			var raw []byte
			// `err` stores the error produced by this operation.
			if err := item.Value(func(val []byte) error {
				raw = append([]byte{}, val...)
				return nil
			}); err != nil {
				return err
			}
			// `quarantineKey` stores the key used to access the related value.
			quarantineKey := []byte(fmt.Sprintf("quarantine:snapshot:%d:%d", height, time.Now().UnixNano()))
			// `err` stores the error produced by this operation.
			if err := txn.Set(quarantineKey, raw); err != nil {
				return err
			}
			return txn.Delete(snapshotKey)
		}); err != nil {
			return err
		}
	}
	return n.refreshLatestSnapshotPointer()
}

// rebuildCommittedSnapshotHeight implements the rebuild committed snapshot height helper.
func (n *Node) rebuildCommittedSnapshotHeight(height uint64) (bool, error) {
	if n == nil || height < 2 {
		return false, nil
	}
	// `base` stores the value used by this operation.
	var base *StateSnapshot
	// `prev` stores the value produced by this operation.
	for prev := height - 1; prev >= 1; prev-- {
		// `snap` and `ok` store whether the related condition is satisfied.
		if snap, _, _, ok := n.ResolveCommittedStateSnapshot(prev); ok && snap != nil {
			base = snap
			break
		}
		// `raw` and `err` store the error produced by this operation.
		if raw, err := n.GetSnapshot(prev); err == nil && raw != nil {
			// `reason` stores the value produced by this operation.
			if _, reason := n.verifySnapshotAgainstLocalBlockDetailed(raw); reason != "" &&
				n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_base_verify:%d:%s", prev, reason), livenessReasonLogCooldown) {
				// `detail` stores the value produced by this operation.
				detail := ""
				if strings.TrimSpace(reason) == "validator_set_root_mismatch" {
					detail = fmt.Sprintf(" got=%s want=%s", ShortHash(raw.ValidatorSetRoot), ShortHash(ValidatorSetMerkleRoot(raw.Height, validatorsFromSnapshot(raw), raw.ValidatorRegistry)))
				}
				log.Printf("[SNAPSHOT-INTEGRITY] height=%d kind=rebuild_base_untrusted base=%d reason=%s%s", height, prev, strings.TrimSpace(reason), detail)
			}
		}
		if prev == 1 {
			break
		}
	}
	if base == nil {
		return false, nil
	}
	// `working` stores the value produced by this operation.
	working := cloneStateSnapshot(base)
	if working == nil {
		return false, nil
	}
	// `next` stores the value produced by this operation.
	for next := working.Height + 1; next <= height; next++ {
		// `delta` and `err` store the error produced by this operation.
		delta, err := n.loadStateDeltaSnapshot(next)
		if err != nil {
			return false, err
		}
		working, err = applyStateDeltaSnapshot(working, delta)
		if err != nil {
			return false, err
		}
	}
	// `ok` and `reason` store whether the related condition is satisfied.
	if ok, reason := n.verifySnapshotAgainstLocalBlockDetailed(working); !ok {
		if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_rebuild_verify:%d:%s", height, reason), livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-INTEGRITY] height=%d kind=rebuild_verify_failed reason=%s", height, strings.TrimSpace(reason))
		}
		return false, nil
	}
	// `err` stores the error produced by this operation.
	if err := n.storeCommittedStateSnapshotRecord(working, "integrity_rebuild"); err != nil {
		return false, err
	}
	if len(working.ValidatorRegistry) > 0 {
		// `err` stores the error produced by this operation.
		if err := n.storeValidatorRegistrySnapshotRecord(height, working.ValidatorRegistry); err != nil {
			return false, err
		}
	}
	return true, nil
}
