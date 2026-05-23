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
	Height                 uint64 `json:"height"`
	SnapshotHash           string `json:"snapshot_hash,omitempty"`
	StateRoot              string `json:"state_root,omitempty"`
	ValidatorSetHash       string `json:"validator_set_hash,omitempty"`
	ValidatorSetSource     string `json:"validator_set_source,omitempty"`
	ValidatorRegistryHash  string `json:"validator_registry_hash,omitempty"`
	NextValidatorSetHash   string `json:"next_validator_set_hash,omitempty"`
	NextValidatorSetSource string `json:"next_validator_set_source,omitempty"`
	NextValidatorSetHeight uint64 `json:"next_validator_set_height,omitempty"`
	FinalizedHeight        uint64 `json:"finalized_height,omitempty"`
	FinalizedHash          string `json:"finalized_hash,omitempty"`
	EpochAnchorHash        string `json:"epoch_anchor_hash,omitempty"`
	FinalityRoot           string `json:"finality_root,omitempty"`
	Timestamp              int64  `json:"timestamp"`
	Source                 string `json:"source,omitempty"`
	StateType              string `json:"state_type,omitempty"`
	BaseHeight             uint64 `json:"base_height,omitempty"`
}

type TipSnapshotRecord struct {
	Height                uint64                     `json:"height"`
	Registry              map[string]ValidatorRecord `json:"registry,omitempty"`
	Snapshot              *StateSnapshot             `json:"snapshot,omitempty"`
	ValidatorRegistryHash string                     `json:"validator_registry_hash,omitempty"`
	StateRoot             string                     `json:"state_root,omitempty"`
	Source                string                     `json:"source,omitempty"`
	UpdatedAt             int64                      `json:"updated_at"`
}

type StateDeltaSnapshot struct {
	Height                          uint64                       `json:"height"`
	BaseHeight                      uint64                       `json:"base_height"`
	PrevSnapshotHash                string                       `json:"prev_snapshot_hash,omitempty"`
	SnapshotHash                    string                       `json:"snapshot_hash,omitempty"`
	BlockHash                       string                       `json:"block_hash,omitempty"`
	PrevHash                        string                       `json:"prev_hash,omitempty"`
	GenesisHash                     string                       `json:"genesis_hash,omitempty"`
	LedgerHash                      string                       `json:"ledger_hash,omitempty"`
	LedgerStage                     string                       `json:"ledger_stage,omitempty"`
	StateRoot                       string                       `json:"state_root,omitempty"`
	ValidatorSetHash                string                       `json:"validator_set_hash,omitempty"`
	ValidatorSetRoot                string                       `json:"validator_set_root,omitempty"`
	ValidatorRegistryHash           string                       `json:"validator_registry_hash,omitempty"`
	ValidatorSetHeight              uint64                       `json:"validator_set_height,omitempty"`
	NextValidatorSetHash            string                       `json:"next_validator_set_hash,omitempty"`
	NextValidatorSetRoot            string                       `json:"next_validator_set_root,omitempty"`
	NextValidatorSetHeight          uint64                       `json:"next_validator_set_height,omitempty"`
	ActivationHeight                uint64                       `json:"activation_height,omitempty"`
	CheckpointHeight                uint64                       `json:"checkpoint_height,omitempty"`
	CheckpointDomain                string                       `json:"checkpoint_domain,omitempty"`
	CheckpointProof                 map[string]string            `json:"checkpoint_proof,omitempty"`
	FinalizedEpoch                  uint64                       `json:"finalized_epoch,omitempty"`
	FinalizedHeight                 uint64                       `json:"finalized_height,omitempty"`
	FinalizedHash                   string                       `json:"finalized_hash,omitempty"`
	FinalizedStateRoot              string                       `json:"finalized_state_root,omitempty"`
	FinalizedValidatorSetHash       string                       `json:"finalized_validator_set_hash,omitempty"`
	FinalizedValidatorSetRoot       string                       `json:"finalized_validator_set_root,omitempty"`
	EpochAnchorHash                 string                       `json:"epoch_anchor_hash,omitempty"`
	PreviousEpochAnchorHash         string                       `json:"previous_epoch_anchor_hash,omitempty"`
	FinalityRoot                    string                       `json:"finality_root,omitempty"`
	FinalityCertificate             *FinalizedEpochCertificate   `json:"finality_certificate,omitempty"`
	Timestamp                       int64                        `json:"timestamp"`
	Validators                      map[string]bool              `json:"validators,omitempty"`
	PendingValidators               map[string]uint64            `json:"pending_validators,omitempty"`
	PendingValidatorRemovals        map[string]uint64            `json:"pending_validator_removals,omitempty"`
	ChangedBalances                 map[string]int               `json:"changed_balances,omitempty"`
	DeletedBalances                 []string                     `json:"deleted_balances,omitempty"`
	ChangedNonces                   map[string]int               `json:"changed_nonces,omitempty"`
	DeletedNonces                   []string                     `json:"deleted_nonces,omitempty"`
	ChangedStakes                   map[string]StakeLock         `json:"changed_stakes,omitempty"`
	DeletedStakes                   []string                     `json:"deleted_stakes,omitempty"`
	ChangedRewardWallets            map[string]string            `json:"changed_reward_wallets,omitempty"`
	DeletedRewardWallets            []string                     `json:"deleted_reward_wallets,omitempty"`
	ChangedEVMState                 map[string]string            `json:"changed_evm_state,omitempty"`
	DeletedEVMState                 []string                     `json:"deleted_evm_state,omitempty"`
	ChangedEVMCode                  map[string]string            `json:"changed_evm_code,omitempty"`
	DeletedEVMCode                  []string                     `json:"deleted_evm_code,omitempty"`
	ChangedEVMStorage               map[string]map[string]string `json:"changed_evm_storage,omitempty"`
	DeletedEVMStorage               []string                     `json:"deleted_evm_storage,omitempty"`
	ChangedUsedValidatorUpdateCerts map[string]uint64            `json:"changed_used_validator_update_certs,omitempty"`
	DeletedUsedValidatorUpdateCerts []string                     `json:"deleted_used_validator_update_certs,omitempty"`
	ChangedValidatorRegistry        map[string]ValidatorRecord   `json:"changed_validator_registry,omitempty"`
	DeletedValidatorRegistry        []string                     `json:"deleted_validator_registry,omitempty"`
	ChangedStateValidators          map[string]Validator         `json:"changed_state_validators,omitempty"`
	DeletedStateValidators          []string                     `json:"deleted_state_validators,omitempty"`
	DTL                             *DTLState                    `json:"dtl,omitempty"`
}

func snapshotMetaKey(height uint64) []byte {
	return []byte(fmt.Sprintf("snapshot_meta:%d", height))
}

func snapshotDeltaKey(height uint64) []byte {
	return []byte(fmt.Sprintf("snapshot_delta:%d", height))
}

func snapshotDeltaHeightKey() []byte {
	return []byte("snapshot_delta_height")
}

func tipSnapshotRegistryKey() []byte {
	return []byte("tip_snapshot:registry")
}

func tipSnapshotStateKey() []byte {
	return []byte("tip_snapshot:state")
}

func tipSnapshotMetaKey() []byte {
	return []byte("tip_snapshot:meta")
}

func snapshotBaseHeight(height uint64) uint64 {
	if height == 0 {
		return 0
	}
	return height - 1
}

func cloneStateSnapshot(snapshot *StateSnapshot) *StateSnapshot {
	if snapshot == nil {
		return nil
	}
	clone := *snapshot
	clone.Ledger = snapshot.Ledger.Clone()
	clone.Validators = copyBoolMap(snapshot.Validators)
	clone.PendingValidators = copySnapshotUint64Map(snapshot.PendingValidators)
	clone.PendingValidatorRemovals = copySnapshotUint64Map(snapshot.PendingValidatorRemovals)
	clone.ValidatorRegistry = copyValidatorRegistrySnapshot(snapshot.ValidatorRegistry)
	clone.StateValidators = copyValidatorMap(snapshot.StateValidators)
	clone.CheckpointProof = copyStringMap(snapshot.CheckpointProof)
	clone.FinalityCertificate = copyFinalizedEpochCertificate(snapshot.FinalityCertificate)
	return &clone
}

func copyFinalizedEpochCertificate(src *FinalizedEpochCertificate) *FinalizedEpochCertificate {
	if src == nil {
		return nil
	}
	out := *src
	out.Signers = append([]string{}, src.Signers...)
	out.Signatures = append([]ValidatorSignature{}, src.Signatures...)
	out.ExecutionResultSignatures = copyStringMap(src.ExecutionResultSignatures)
	return &out
}

func copyBoolMap(src map[string]bool) map[string]bool {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]bool, len(src))
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

func copyValidatorMap(src map[string]Validator) map[string]Validator {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]Validator, len(src))
	for key, value := range src {
		value.PubKey = append([]byte{}, value.PubKey...)
		out[key] = value
	}
	return out
}

func copyNestedStringMap(src map[string]map[string]string) map[string]map[string]string {
	if len(src) == 0 {
		return nil
	}
	out := make(map[string]map[string]string, len(src))
	for key, value := range src {
		out[key] = copyStringMap(value)
	}
	return out
}

func deepCopyDTLState(src *DTLState) *DTLState {
	if src == nil {
		return nil
	}
	raw, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	var out DTLState
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return &out
}

func snapshotMetaFromSnapshot(snapshot *StateSnapshot, source string, stateType string, baseHeight uint64) *SnapshotMetaRecord {
	if snapshot == nil {
		return nil
	}
	populateSnapshotDerivedFields(snapshot)
	validatorSetSource := strings.TrimSpace(snapshot.ValidatorSetSource)
	if validatorSetSource == "" {
		validatorSetSource = strings.TrimSpace(normalizeCommittedValidatorAuthoritySource(source))
	}
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

func normalizeSnapshotMetaRecord(record *SnapshotMetaRecord) {
	if record == nil {
		return
	}
	record.SnapshotHash = strings.TrimSpace(record.SnapshotHash)
	record.StateRoot = strings.TrimSpace(record.StateRoot)
	record.ValidatorSetHash = strings.TrimSpace(record.ValidatorSetHash)
	record.ValidatorSetSource = strings.TrimSpace(record.ValidatorSetSource)
	record.ValidatorRegistryHash = strings.TrimSpace(record.ValidatorRegistryHash)
	record.NextValidatorSetHash = strings.TrimSpace(record.NextValidatorSetHash)
	record.NextValidatorSetSource = strings.TrimSpace(record.NextValidatorSetSource)
	record.FinalizedHash = strings.TrimSpace(record.FinalizedHash)
	record.EpochAnchorHash = strings.TrimSpace(record.EpochAnchorHash)
	record.FinalityRoot = strings.TrimSpace(record.FinalityRoot)
	record.Source = strings.TrimSpace(record.Source)
	record.StateType = strings.TrimSpace(record.StateType)
}

func (n *Node) loadSnapshotMetaRecord(height uint64) (*SnapshotMetaRecord, error) {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil, errors.New("snapshot_meta_unavailable")
	}
	var record SnapshotMetaRecord
	var err error
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		err = store.View(func(txn *Txn) error {
			item, err := txn.Get(snapshotMetaKey(height))
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
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

func (n *Node) storeSnapshotMetaRecord(height uint64, snapshot *StateSnapshot, source string, stateType string, baseHeight uint64) error {
	if n == nil || height == 0 || snapshot == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil
	}
	record := snapshotMetaFromSnapshot(snapshot, source, stateType, baseHeight)
	if record == nil {
		return nil
	}
	normalizeSnapshotMetaRecord(record)
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return n.DB.SnapshotMetaStore().Update(func(txn *Txn) error {
		enc, err := encryptDBValue(raw)
		if err != nil {
			return err
		}
		return txn.Set(snapshotMetaKey(height), enc)
	})
}

func (n *Node) ensureSnapshotMetaRecord(snapshot *StateSnapshot, source string) error {
	if snapshot == nil {
		return nil
	}
	record, err := n.loadSnapshotMetaRecord(snapshot.Height)
	if err == nil && record != nil {
		expected := snapshotMetaFromSnapshot(snapshot, record.Source, record.StateType, record.BaseHeight)
		normalizeSnapshotMetaRecord(expected)
		if strings.EqualFold(record.SnapshotHash, expected.SnapshotHash) &&
			strings.EqualFold(record.StateRoot, expected.StateRoot) &&
			strings.EqualFold(record.ValidatorSetHash, expected.ValidatorSetHash) &&
			strings.EqualFold(record.ValidatorSetSource, expected.ValidatorSetSource) &&
			strings.EqualFold(record.ValidatorRegistryHash, expected.ValidatorRegistryHash) &&
			strings.EqualFold(record.NextValidatorSetHash, expected.NextValidatorSetHash) &&
			strings.EqualFold(record.NextValidatorSetSource, expected.NextValidatorSetSource) &&
			record.NextValidatorSetHeight == expected.NextValidatorSetHeight {
			return nil
		}
	}
	return n.storeSnapshotMetaRecord(snapshot.Height, snapshot, source, "committed_full", snapshotBaseHeight(snapshot.Height))
}

func (n *Node) loadTipSnapshotRecord(key []byte) (*TipSnapshotRecord, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, errors.New("tip_snapshot_unavailable")
	}
	var record TipSnapshotRecord
	var err error
	for _, store := range n.DB.SnapshotStoresForRead() {
		err = store.View(func(txn *Txn) error {
			item, err := txn.Get(key)
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
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

func (n *Node) loadTipSnapshotState() (*TipSnapshotRecord, error) {
	return n.loadTipSnapshotRecord(tipSnapshotStateKey())
}

func (n *Node) loadTipSnapshotRegistry() (*TipSnapshotRecord, error) {
	return n.loadTipSnapshotRecord(tipSnapshotRegistryKey())
}

func (n *Node) loadTipSnapshotMeta() (*TipSnapshotRecord, error) {
	return n.loadTipSnapshotRecord(tipSnapshotMetaKey())
}

func (n *Node) storeTipSnapshotRecords(snapshot *StateSnapshot, source string) error {
	if n == nil || snapshot == nil || snapshot.Height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	populateSnapshotDerivedFields(snapshot)
	updatedAt := time.Now().Unix()
	stateRecord := TipSnapshotRecord{
		Height:                snapshot.Height,
		Snapshot:              cloneStateSnapshot(snapshot),
		ValidatorRegistryHash: strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)),
		StateRoot:             strings.TrimSpace(snapshot.StateRoot),
		Source:                strings.TrimSpace(source),
		UpdatedAt:             updatedAt,
	}
	registryRecord := TipSnapshotRecord{
		Height:                snapshot.Height,
		Registry:              copyValidatorRegistrySnapshot(snapshot.ValidatorRegistry),
		ValidatorRegistryHash: strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)),
		StateRoot:             strings.TrimSpace(snapshot.StateRoot),
		Source:                strings.TrimSpace(source),
		UpdatedAt:             updatedAt,
	}
	metaRecord := TipSnapshotRecord{
		Height:                snapshot.Height,
		ValidatorRegistryHash: strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)),
		StateRoot:             strings.TrimSpace(snapshot.StateRoot),
		Source:                strings.TrimSpace(source),
		UpdatedAt:             updatedAt,
	}
	return n.DB.SnapshotStore().Update(func(txn *Txn) error {
		write := func(key []byte, value TipSnapshotRecord) error {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			enc, err := encryptDBValue(raw)
			if err != nil {
				return err
			}
			return txn.Set(key, enc)
		}
		if err := write(tipSnapshotStateKey(), stateRecord); err != nil {
			return err
		}
		if err := write(tipSnapshotRegistryKey(), registryRecord); err != nil {
			return err
		}
		return write(tipSnapshotMetaKey(), metaRecord)
	})
}

func (n *Node) clearTipSnapshotRecords() error {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	for _, store := range n.DB.SnapshotStoresForRead() {
		if err := store.Update(func(txn *Txn) error {
			for _, key := range [][]byte{tipSnapshotStateKey(), tipSnapshotRegistryKey(), tipSnapshotMetaKey()} {
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

func (n *Node) clearStaleTipSnapshotRecordsAboveHeight(height uint64) error {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	for _, load := range []func() (*TipSnapshotRecord, error){
		n.loadTipSnapshotState,
		n.loadTipSnapshotRegistry,
		n.loadTipSnapshotMeta,
	} {
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

func (n *Node) pruneSnapshotMetaAboveHeight(height uint64) error {
	if n == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil
	}
	prefix := []byte("snapshot_meta:")
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		if err := store.Update(func(txn *Txn) error {
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := append([]byte(nil), it.Item().Key()...)
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				h, err := strconv.ParseUint(string(parts[1]), 10, 64)
				if err != nil || h <= height {
					continue
				}
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

func (n *Node) pruneSnapshotMetaBelowHeight(retainFromHeight uint64) error {
	if n == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil || retainFromHeight == 0 {
		return nil
	}
	prefix := []byte("snapshot_meta:")
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		if err := store.Update(func(txn *Txn) error {
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := append([]byte(nil), it.Item().Key()...)
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				h, err := strconv.ParseUint(string(parts[1]), 10, 64)
				if err != nil || h >= retainFromHeight {
					continue
				}
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

func (n *Node) pruneSnapshotDeltasAboveHeight(height uint64) error {
	if n == nil || n.DB == nil {
		return nil
	}
	if n.DB.SnapshotStore() != nil {
		prefix := []byte("snapshot_delta:")
		for _, store := range n.DB.SnapshotStoresForRead() {
			if err := store.Update(func(txn *Txn) error {
				it := txn.NewIterator(DefaultIteratorOptions)
				defer it.Close()
				for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
					key := append([]byte(nil), it.Item().Key()...)
					parts := bytes.Split(key, []byte(":"))
					if len(parts) != 2 {
						continue
					}
					h, err := strconv.ParseUint(string(parts[1]), 10, 64)
					if err != nil || h <= height {
						continue
					}
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
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		if err := store.Update(func(txn *Txn) error {
			item, err := txn.Get(snapshotDeltaHeightKey())
			if err != nil {
				if errors.Is(err, ErrKeyNotFound) {
					return nil
				}
				return err
			}
			var latest uint64
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

func (n *Node) pruneSnapshotDeltasBelowHeight(retainFromHeight uint64) error {
	if n == nil || n.DB == nil || retainFromHeight == 0 {
		return nil
	}
	if n.DB.SnapshotStore() == nil {
		return nil
	}
	prefix := []byte("snapshot_delta:")
	for _, store := range n.DB.SnapshotStoresForRead() {
		if err := store.Update(func(txn *Txn) error {
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := append([]byte(nil), it.Item().Key()...)
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				h, err := strconv.ParseUint(string(parts[1]), 10, 64)
				if err != nil || h >= retainFromHeight {
					continue
				}
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

func (n *Node) storeCommittedStateSnapshotRecord(snapshot *StateSnapshot, source string) error {
	if n == nil || snapshot == nil || snapshot.Height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	populateSnapshotDerivedFields(snapshot)
	snapshot.SnapshotHash = snapshotCanonicalHash(snapshot)
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	key := []byte(fmt.Sprintf("snapshot:%d", snapshot.Height))
	if err := n.DB.SnapshotStore().Update(func(txn *Txn) error {
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set(key, enc)
	}); err != nil {
		return err
	}
	if n.DB.SnapshotMetaStore() == nil {
		return errors.New("snapshot meta db not initialized")
	}
	if err := n.DB.SnapshotMetaStore().Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:latest"), key)
	}); err != nil {
		return err
	}
	if err := n.storeSnapshotMetaRecord(snapshot.Height, snapshot, source, "committed_full", snapshotBaseHeight(snapshot.Height)); err != nil {
		return err
	}
	if err := n.storeTipSnapshotRecords(snapshot, source); err != nil {
		return err
	}
	n.exportSnapshotArtifactsBestEffort(snapshot, source)
	return nil
}

func (n *Node) committedStateSnapshotRecordExists(height uint64) bool {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return false
	}
	key := []byte(fmt.Sprintf("snapshot:%d", height))
	for _, store := range n.DB.SnapshotStoresForRead() {
		err := store.View(func(txn *Txn) error {
			_, err := txn.Get(key)
			return err
		})
		if err == nil {
			return true
		}
	}
	return false
}

func (n *Node) resolveCommittedStateSnapshotFromStorage(height uint64) (*StateSnapshot, string, bool) {
	if n == nil || height == 0 {
		return nil, "", false
	}
	if snap, err := n.GetSnapshot(height); err == nil && snap != nil {
		if ok, _ := n.verifySnapshotAgainstLocalBlockDetailed(snap); ok {
			_ = n.ensureSnapshotMetaRecord(snap, "snapshot_committed")
			return snap, strings.TrimSpace(snap.SnapshotHash), true
		}
	}
	return nil, "", false
}

func (n *Node) resolveTrustedExecutionSnapshotFromStorage(height uint64) (*StateSnapshot, string, bool) {
	if n == nil || height == 0 {
		return nil, "", false
	}
	snap, err := n.GetSnapshot(height)
	if err != nil || snap == nil {
		return nil, "", false
	}
	if ok, _ := n.verifySnapshotAgainstLocalBlockDetailed(snap); !ok {
		return nil, "", false
	}
	if !snapshotHasTrustedExecutionLedger(snap) {
		upgraded := cloneStateSnapshot(snap)
		if upgraded == nil {
			return nil, "", false
		}
		upgraded.LedgerStage = snapshotLedgerStageExecution
		populateSnapshotDerivedFields(upgraded)
		if err := n.storeCommittedStateSnapshotRecord(upgraded, "snapshot_committed_upgrade"); err == nil {
			snap = upgraded
		} else {
			return nil, "", false
		}
	}
	_ = n.ensureSnapshotMetaRecord(snap, "snapshot_committed")
	return snap, strings.TrimSpace(snap.SnapshotHash), true
}

func (n *Node) resolveCommittedStateSnapshotFromTipRecord(height uint64) (*StateSnapshot, string, bool, string) {
	if n == nil || height == 0 {
		return nil, "", false, "none"
	}
	record, err := n.loadTipSnapshotState()
	if err != nil || record == nil || record.Snapshot == nil || record.Height != height {
		return nil, "", false, "tip_snapshot_unavailable"
	}
	snapshot := cloneStateSnapshot(record.Snapshot)
	if snapshot == nil {
		return nil, "", false, "tip_snapshot_unavailable"
	}
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
	if err := n.storeCommittedStateSnapshotRecord(snapshot, "tip_snapshot_repair"); err != nil {
		return nil, "", false, fmt.Sprintf("persist_failed err=%v", err)
	}
	if len(snapshot.ValidatorRegistry) > 0 {
		_ = n.storeValidatorRegistrySnapshotRecord(height, snapshot.ValidatorRegistry)
	}
	return snapshot, strings.TrimSpace(snapshot.SnapshotHash), true, ""
}

func (n *Node) materializeCommittedTipStateSnapshot(height uint64, reason string) error {
	if n == nil || height == 0 || n.Blockchain == nil {
		return errors.New("tip_snapshot_unavailable")
	}
	if n.Blockchain.Height() != height {
		return errors.New("not_chain_tip")
	}
	block, ok := n.Blockchain.GetBlock(height)
	if !ok {
		return errors.New("tip_block_unavailable")
	}
	if err := n.CreateSnapshot(height, strings.TrimSpace(block.BlockHash)); err != nil {
		return fmt.Errorf("create_snapshot_failed reason=%s err=%w", strings.TrimSpace(reason), err)
	}
	snapshot, err := n.GetSnapshot(height)
	if err != nil || snapshot == nil {
		return fmt.Errorf("create_snapshot_load_failed reason=%s err=%v", strings.TrimSpace(reason), err)
	}
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

func shouldAutoCreateSnapshotAtHeight(height uint64) bool {
	if height == 0 {
		return false
	}
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

func shouldBypassSnapshotCheckpointDeferral(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "resolver_tip_missing", "integrity_monitor", "startup":
		return true
	default:
		return false
	}
}

func shouldPeerFetchCommittedTipSnapshot(reason string) bool {
	switch strings.TrimSpace(reason) {
	case "resolver_tip_missing", "integrity_monitor", "startup", "snapshot_create_worker":
		return true
	default:
		return false
	}
}

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
	result, err := n.downloadTrustedSnapshotAndStore(height, height, true, false, false)
	if err != nil {
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
	source := strings.TrimSpace(result.Source)
	if source == "" {
		source = "trusted_snapshot_download"
	}
	displaySource := displaySnapshotAuthoritySource(source)
	if displaySource == "none" {
		displaySource = strings.TrimSpace(source)
	}
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

func (n *Node) ensureCommittedTipStateSnapshot(height uint64, reason string) (source string, ok bool) {
	if n == nil || height == 0 || n.Blockchain == nil || n.Blockchain.Height() != height {
		return "none", false
	}
	if _, _, ok := n.resolveCommittedStateSnapshotFromStorage(height); ok {
		return "snapshot_committed", true
	}
	if _, _, ok, _ := n.resolveCommittedStateSnapshotFromTipRecord(height); ok {
		return "tip_snapshot_repair", true
	}
	if !shouldAutoCreateSnapshotAtHeight(height) && !shouldBypassSnapshotCheckpointDeferral(reason) {
		return "checkpoint_interval_deferred", true
	}
	if err := n.materializeCommittedTipStateSnapshot(height, reason); err != nil {
		if source, fetched := n.fetchCommittedTipSnapshotFromPeers(height, reason); fetched {
			return source, true
		}
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
	if _, _, ok := n.resolveCommittedStateSnapshotFromStorage(height); ok {
		return "tip_create_snapshot_repair", true
	}
	if source, fetched := n.fetchCommittedTipSnapshotFromPeers(height, reason); fetched {
		return source, true
	}
	return "none", false
}

func (n *Node) latestCommittedSnapshotMeta() (*StateSnapshot, *SnapshotMetaRecord, string, error) {
	if n == nil {
		return nil, nil, "", errors.New("node unavailable")
	}
	snapshot, err := n.verifiedStoredSnapshotAtOrBelow(0)
	if err != nil {
		return nil, nil, "", err
	}
	if snapshot == nil {
		return nil, nil, "", errors.New("snapshot unavailable")
	}
	meta, metaErr := n.loadSnapshotMetaRecord(snapshot.Height)
	if metaErr != nil || meta == nil {
		source := "snapshot_committed"
		if err := n.ensureSnapshotMetaRecord(snapshot, source); err == nil {
			meta, _ = n.loadSnapshotMetaRecord(snapshot.Height)
		}
		if meta == nil {
			meta = snapshotMetaFromSnapshot(snapshot, source, "committed_full", snapshotBaseHeight(snapshot.Height))
		}
	}
	source := "snapshot_committed"
	if meta != nil && strings.TrimSpace(meta.Source) != "" {
		source = strings.TrimSpace(meta.Source)
	}
	return snapshot, meta, source, nil
}

func (n *Node) createCommittedTipSnapshot(_ string, force bool) (*StateSnapshot, *SnapshotMetaRecord, string, error) {
	if n == nil {
		return nil, nil, "", errors.New("node unavailable")
	}
	if n.Blockchain == nil {
		return nil, nil, "", errors.New("blockchain unavailable")
	}
	tip := n.Blockchain.Height()
	if tip == 0 {
		return nil, nil, "", errors.New("committed tip unavailable")
	}
	if !force {
		if snapshot, _, source, ok := n.ResolveCommittedStateSnapshot(tip); ok && snapshot != nil {
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
	block, ok := n.Blockchain.GetBlock(tip)
	if !ok {
		return nil, nil, "", errors.New("committed tip block unavailable")
	}
	if err := n.CreateSnapshot(tip, strings.TrimSpace(block.BlockHash)); err != nil {
		return nil, nil, "", err
	}
	snapshot, err := n.GetSnapshot(tip)
	if err != nil {
		return nil, nil, "", err
	}
	if snapshot == nil {
		return nil, nil, "", errors.New("snapshot unavailable after create")
	}
	if ok, detail := n.snapshotMatchesLocalAnchorDetailed(snapshot); !ok {
		_ = n.deleteStoredSnapshotHeight(tip)
		_ = n.refreshLatestSnapshotPointer()
		detail = strings.TrimSpace(detail)
		if detail == "" {
			detail = "anchor_verification_failed"
		}
		return nil, nil, "", fmt.Errorf("created snapshot failed local anchor verification: %s", detail)
	}
	source := "create_snapshot"
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

func (n *Node) ResolveCommittedStateSnapshot(height uint64) (*StateSnapshot, string, string, bool) {
	if n == nil || height == 0 {
		return nil, "", "none", false
	}
	if snap, hash, ok := n.resolveCommittedStateSnapshotFromStorage(height); ok {
		return snap, hash, "snapshot_committed", true
	}
	if n.Blockchain == nil || n.Blockchain.Height() != height {
		return nil, "", "none", false
	}
	failureReason := "tip_snapshot_unavailable"
	if snap, hash, ok, reason := n.resolveCommittedStateSnapshotFromTipRecord(height); ok {
		return snap, hash, "tip_snapshot_repair", true
	} else if strings.TrimSpace(reason) != "" {
		failureReason = strings.TrimSpace(reason)
	}
	if source, repaired := n.ensureCommittedTipStateSnapshot(height, "resolver_tip_missing"); repaired {
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

func (n *Node) resolveCommittedStateSnapshot(height uint64) (*StateSnapshot, string, string, bool) {
	return n.ResolveCommittedStateSnapshot(height)
}

func (n *Node) ResolveCommittedRegistrySnapshot(height uint64) (map[string]ValidatorRecord, string, string, bool) {
	return n.resolveCommittedValidatorRegistrySnapshot(height)
}

func diffIntMap(prev map[string]int, current map[string]int) (map[string]int, []string) {
	changed := make(map[string]int)
	deleted := make([]string, 0)
	for key, value := range current {
		if old, ok := prev[key]; !ok || old != value {
			changed[key] = value
		}
	}
	for key := range prev {
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

func diffStringMap(prev map[string]string, current map[string]string) (map[string]string, []string) {
	changed := make(map[string]string)
	deleted := make([]string, 0)
	for key, value := range current {
		if old, ok := prev[key]; !ok || old != value {
			changed[key] = value
		}
	}
	for key := range prev {
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

func diffUint64Map(prev map[string]uint64, current map[string]uint64) (map[string]uint64, []string) {
	changed := make(map[string]uint64)
	deleted := make([]string, 0)
	for key, value := range current {
		if old, ok := prev[key]; !ok || old != value {
			changed[key] = value
		}
	}
	for key := range prev {
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

func diffStakeMap(prev map[string]StakeLock, current map[string]StakeLock) (map[string]StakeLock, []string) {
	changed := make(map[string]StakeLock)
	deleted := make([]string, 0)
	for key, value := range current {
		if old, ok := prev[key]; !ok || !reflect.DeepEqual(old, value) {
			changed[key] = value
		}
	}
	for key := range prev {
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

func diffValidatorRegistryMap(prev map[string]ValidatorRecord, current map[string]ValidatorRecord) (map[string]ValidatorRecord, []string) {
	changed := make(map[string]ValidatorRecord)
	deleted := make([]string, 0)
	for key, value := range current {
		if old, ok := prev[key]; !ok || !reflect.DeepEqual(old, value) {
			changed[key] = value
		}
	}
	for key := range prev {
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

func diffValidatorMap(prev map[string]Validator, current map[string]Validator) (map[string]Validator, []string) {
	changed := make(map[string]Validator)
	deleted := make([]string, 0)
	for key, value := range current {
		if old, ok := prev[key]; !ok || !reflect.DeepEqual(old, value) {
			value.PubKey = append([]byte{}, value.PubKey...)
			changed[key] = value
		}
	}
	for key := range prev {
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

func diffNestedStringMap(prev map[string]map[string]string, current map[string]map[string]string) (map[string]map[string]string, []string) {
	changed := make(map[string]map[string]string)
	deleted := make([]string, 0)
	for key, value := range current {
		if old, ok := prev[key]; !ok || !reflect.DeepEqual(old, value) {
			changed[key] = copyStringMap(value)
		}
	}
	for key := range prev {
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
	changedBalances, deletedBalances := diffIntMap(base.Ledger.Balances, current.Ledger.Balances)
	changedNonces, deletedNonces := diffIntMap(base.Ledger.Nonces, current.Ledger.Nonces)
	changedStakes, deletedStakes := diffStakeMap(base.Ledger.Stakes, current.Ledger.Stakes)
	changedRewardWallets, deletedRewardWallets := diffStringMap(base.Ledger.ValidatorRewardWallets, current.Ledger.ValidatorRewardWallets)
	changedEVMState, deletedEVMState := diffStringMap(base.Ledger.EVMState, current.Ledger.EVMState)
	changedEVMCode, deletedEVMCode := diffStringMap(base.Ledger.EVMCode, current.Ledger.EVMCode)
	changedEVMStorage, deletedEVMStorage := diffNestedStringMap(base.Ledger.EVMStorage, current.Ledger.EVMStorage)
	changedCerts, deletedCerts := diffUint64Map(base.Ledger.UsedValidatorUpdateCerts, current.Ledger.UsedValidatorUpdateCerts)
	changedRegistry, deletedRegistry := diffValidatorRegistryMap(base.ValidatorRegistry, current.ValidatorRegistry)
	changedStateValidators, deletedStateValidators := diffValidatorMap(base.StateValidators, current.StateValidators)
	var dtlCopy *DTLState
	if !reflect.DeepEqual(base.Ledger.DTL, current.Ledger.DTL) {
		dtlCopy = deepCopyDTLState(current.Ledger.DTL)
	}
	return &StateDeltaSnapshot{
		Height:                          current.Height,
		BaseHeight:                      base.Height,
		PrevSnapshotHash:                strings.TrimSpace(base.SnapshotHash),
		SnapshotHash:                    strings.TrimSpace(current.SnapshotHash),
		BlockHash:                       strings.TrimSpace(current.BlockHash),
		PrevHash:                        strings.TrimSpace(current.PrevHash),
		GenesisHash:                     strings.TrimSpace(current.GenesisHash),
		LedgerHash:                      strings.TrimSpace(current.LedgerHash),
		LedgerStage:                     strings.TrimSpace(current.LedgerStage),
		StateRoot:                       strings.TrimSpace(current.StateRoot),
		ValidatorSetHash:                strings.TrimSpace(snapshotValidatorSetHash(current)),
		ValidatorSetRoot:                strings.TrimSpace(snapshotValidatorSetRoot(current)),
		ValidatorRegistryHash:           strings.TrimSpace(snapshotValidatorRegistryHash(current)),
		ValidatorSetHeight:              current.ValidatorSetHeight,
		NextValidatorSetHash:            strings.TrimSpace(current.NextValidatorSetHash),
		NextValidatorSetRoot:            strings.TrimSpace(current.NextValidatorSetRoot),
		NextValidatorSetHeight:          current.NextValidatorSetHeight,
		ActivationHeight:                current.ActivationHeight,
		CheckpointHeight:                current.CheckpointHeight,
		CheckpointDomain:                strings.TrimSpace(current.CheckpointDomain),
		CheckpointProof:                 copyStringMap(current.CheckpointProof),
		FinalizedEpoch:                  current.FinalizedEpoch,
		FinalizedHeight:                 current.FinalizedHeight,
		FinalizedHash:                   strings.TrimSpace(current.FinalizedHash),
		FinalizedStateRoot:              strings.TrimSpace(current.FinalizedStateRoot),
		FinalizedValidatorSetHash:       strings.TrimSpace(current.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot:       strings.TrimSpace(current.FinalizedValidatorSetRoot),
		EpochAnchorHash:                 strings.TrimSpace(current.EpochAnchorHash),
		PreviousEpochAnchorHash:         strings.TrimSpace(current.PreviousEpochAnchorHash),
		FinalityRoot:                    strings.TrimSpace(current.FinalityRoot),
		FinalityCertificate:             copyFinalizedEpochCertificate(current.FinalityCertificate),
		Timestamp:                       current.Timestamp,
		Validators:                      copyBoolMap(current.Validators),
		PendingValidators:               copySnapshotUint64Map(current.PendingValidators),
		PendingValidatorRemovals:        copySnapshotUint64Map(current.PendingValidatorRemovals),
		ChangedBalances:                 changedBalances,
		DeletedBalances:                 deletedBalances,
		ChangedNonces:                   changedNonces,
		DeletedNonces:                   deletedNonces,
		ChangedStakes:                   changedStakes,
		DeletedStakes:                   deletedStakes,
		ChangedRewardWallets:            changedRewardWallets,
		DeletedRewardWallets:            deletedRewardWallets,
		ChangedEVMState:                 changedEVMState,
		DeletedEVMState:                 deletedEVMState,
		ChangedEVMCode:                  changedEVMCode,
		DeletedEVMCode:                  deletedEVMCode,
		ChangedEVMStorage:               changedEVMStorage,
		DeletedEVMStorage:               deletedEVMStorage,
		ChangedUsedValidatorUpdateCerts: changedCerts,
		DeletedUsedValidatorUpdateCerts: deletedCerts,
		ChangedValidatorRegistry:        changedRegistry,
		DeletedValidatorRegistry:        deletedRegistry,
		ChangedStateValidators:          changedStateValidators,
		DeletedStateValidators:          deletedStateValidators,
		DTL:                             dtlCopy,
	}, nil
}

func applyStateDeltaSnapshot(base *StateSnapshot, delta *StateDeltaSnapshot) (*StateSnapshot, error) {
	if base == nil || delta == nil {
		return nil, fmt.Errorf("snapshot_delta_unavailable")
	}
	if delta.BaseHeight != base.Height {
		return nil, fmt.Errorf("snapshot_delta_base_mismatch base=%d delta=%d", base.Height, delta.BaseHeight)
	}
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
	for key, value := range delta.ChangedBalances {
		if next.Ledger.Balances == nil {
			next.Ledger.Balances = make(map[string]int)
		}
		next.Ledger.Balances[key] = value
	}
	for _, key := range delta.DeletedBalances {
		delete(next.Ledger.Balances, key)
	}
	for key, value := range delta.ChangedNonces {
		if next.Ledger.Nonces == nil {
			next.Ledger.Nonces = make(map[string]int)
		}
		next.Ledger.Nonces[key] = value
	}
	for _, key := range delta.DeletedNonces {
		delete(next.Ledger.Nonces, key)
	}
	for key, value := range delta.ChangedStakes {
		if next.Ledger.Stakes == nil {
			next.Ledger.Stakes = make(map[string]StakeLock)
		}
		next.Ledger.Stakes[key] = value
	}
	for _, key := range delta.DeletedStakes {
		delete(next.Ledger.Stakes, key)
	}
	for key, value := range delta.ChangedRewardWallets {
		if next.Ledger.ValidatorRewardWallets == nil {
			next.Ledger.ValidatorRewardWallets = make(map[string]string)
		}
		next.Ledger.ValidatorRewardWallets[key] = value
	}
	for _, key := range delta.DeletedRewardWallets {
		delete(next.Ledger.ValidatorRewardWallets, key)
	}
	for key, value := range delta.ChangedEVMState {
		if next.Ledger.EVMState == nil {
			next.Ledger.EVMState = make(map[string]string)
		}
		next.Ledger.EVMState[key] = value
	}
	for _, key := range delta.DeletedEVMState {
		delete(next.Ledger.EVMState, key)
	}
	for key, value := range delta.ChangedEVMCode {
		if next.Ledger.EVMCode == nil {
			next.Ledger.EVMCode = make(map[string]string)
		}
		next.Ledger.EVMCode[key] = value
	}
	for _, key := range delta.DeletedEVMCode {
		delete(next.Ledger.EVMCode, key)
	}
	for key, value := range delta.ChangedEVMStorage {
		if next.Ledger.EVMStorage == nil {
			next.Ledger.EVMStorage = make(map[string]map[string]string)
		}
		next.Ledger.EVMStorage[key] = copyStringMap(value)
	}
	for _, key := range delta.DeletedEVMStorage {
		delete(next.Ledger.EVMStorage, key)
	}
	for key, value := range delta.ChangedUsedValidatorUpdateCerts {
		if next.Ledger.UsedValidatorUpdateCerts == nil {
			next.Ledger.UsedValidatorUpdateCerts = make(map[string]uint64)
		}
		next.Ledger.UsedValidatorUpdateCerts[key] = value
	}
	for _, key := range delta.DeletedUsedValidatorUpdateCerts {
		delete(next.Ledger.UsedValidatorUpdateCerts, key)
	}
	if delta.DTL != nil {
		next.Ledger.DTL = deepCopyDTLState(delta.DTL)
	}
	next.StateMerkleRoot = LedgerStateMerkleRoot(next.Ledger)
	for key, value := range delta.ChangedValidatorRegistry {
		if next.ValidatorRegistry == nil {
			next.ValidatorRegistry = make(map[string]ValidatorRecord)
		}
		next.ValidatorRegistry[key] = value
	}
	for _, key := range delta.DeletedValidatorRegistry {
		delete(next.ValidatorRegistry, key)
	}
	for key, value := range delta.ChangedStateValidators {
		if next.StateValidators == nil {
			next.StateValidators = make(map[string]Validator)
		}
		value.PubKey = append([]byte{}, value.PubKey...)
		next.StateValidators[key] = value
	}
	for _, key := range delta.DeletedStateValidators {
		delete(next.StateValidators, key)
	}
	populateSnapshotDerivedFields(next)
	computedSnapshotHash := snapshotCanonicalHash(next)
	if strings.TrimSpace(delta.SnapshotHash) != "" && !strings.EqualFold(strings.TrimSpace(computedSnapshotHash), strings.TrimSpace(delta.SnapshotHash)) {
		return nil, fmt.Errorf("snapshot_delta_hash_mismatch got=%s want=%s", computedSnapshotHash, delta.SnapshotHash)
	}
	next.SnapshotHash = computedSnapshotHash
	if strings.TrimSpace(delta.ValidatorRegistryHash) != "" && !strings.EqualFold(strings.TrimSpace(snapshotValidatorRegistryHash(next)), strings.TrimSpace(delta.ValidatorRegistryHash)) {
		return nil, fmt.Errorf("snapshot_delta_registry_hash_mismatch got=%s want=%s", snapshotValidatorRegistryHash(next), delta.ValidatorRegistryHash)
	}
	return next, nil
}

func (n *Node) loadStateDeltaSnapshot(height uint64) (*StateDeltaSnapshot, error) {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, errors.New("snapshot_delta_unavailable")
	}
	var delta StateDeltaSnapshot
	var err error
	for _, store := range n.DB.SnapshotStoresForRead() {
		err = store.View(func(txn *Txn) error {
			item, err := txn.Get(snapshotDeltaKey(height))
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
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

func itemKey(it *Iterator) []byte {
	if it == nil || it.iter == nil || !it.iter.Valid() {
		return nil
	}
	return append([]byte{}, it.iter.Key()...)
}

func parseUintBytes(raw []byte) (uint64, error) {
	return strconv.ParseUint(string(raw), 10, 64)
}

func (n *Node) loadLatestSnapshotDeltaHeight() (uint64, bool) {
	if n == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return 0, false
	}
	var height uint64
	var err error
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		err = store.View(func(txn *Txn) error {
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
	var best uint64
	for _, store := range n.DB.SnapshotStoresForRead() {
		_ = store.View(func(txn *Txn) error {
			it := txn.NewIterator(DefaultIteratorOptions)
			defer it.Close()
			prefix := []byte("snapshot_delta:")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				key := itemKey(it)
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
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

func (n *Node) storeLatestSnapshotDeltaHeight(height uint64) error {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], height)
	return n.DB.SnapshotMetaStore().Update(func(txn *Txn) error {
		return txn.Set(snapshotDeltaHeightKey(), buf[:])
	})
}

func (n *Node) storeStateDeltaSnapshot(delta *StateDeltaSnapshot) error {
	if n == nil || delta == nil || delta.Height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	raw, err := json.Marshal(delta)
	if err != nil {
		return err
	}
	if err := n.DB.SnapshotStore().Update(func(txn *Txn) error {
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

func (n *Node) processPendingSnapshotDeltaWork(limit int) (int, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return 0, nil
	}
	target := n.getFinalizedHeight()
	if target == 0 && n.Blockchain != nil {
		target = n.Blockchain.Height()
	}
	if target < 2 {
		return 0, nil
	}
	latest, ok := n.loadLatestSnapshotDeltaHeight()
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
	processed := 0
	for next <= target && processed < limit {
		base, _, _, okBase := n.ResolveCommittedStateSnapshot(next - 1)
		current, _, _, okCurrent := n.ResolveCommittedStateSnapshot(next)
		if !okBase || !okCurrent {
			break
		}
		delta, err := buildStateDeltaSnapshot(base, current)
		if err != nil {
			return processed, err
		}
		if err := n.storeStateDeltaSnapshot(delta); err != nil {
			return processed, err
		}
		processed++
		next++
	}
	return processed, nil
}

func (n *Node) startSnapshotDeltaWorker(ctx context.Context) {
	if n == nil {
		return
	}
	_, _ = n.processPendingSnapshotDeltaWork(8)
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

func (n *Node) startSnapshotIntegrityMonitor(ctx context.Context) {
	if n == nil {
		return
	}
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

func (n *Node) startSnapshotCreateWorker(ctx context.Context) {
	if n == nil {
		return
	}
	run := func() {
		if n == nil || n.Blockchain == nil {
			return
		}
		tip := n.Blockchain.Height()
		if tip == 0 {
			return
		}
		source, ok := n.ensureCommittedTipStateSnapshot(tip, "snapshot_create_worker")
		if !ok || source == "snapshot_committed" || source == "checkpoint_interval_deferred" {
			return
		}
		key := fmt.Sprintf("snapshot_create_worker:%d:%s", tip, strings.TrimSpace(source))
		if n.shouldLogLivenessReason(key, livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-CREATE] height=%d source=%s", tip, strings.TrimSpace(source))
		}
	}
	run()
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

func (n *Node) verifySnapshotIntegrityDepth(depth uint64) error {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil || n.Blockchain == nil {
		return nil
	}
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
	start := uint64(1)
	if tip > depth {
		start = tip - depth + 1
	}
	for height := start; height <= tip; height++ {
		snap, _, _, ok := n.ResolveCommittedStateSnapshot(height)
		if ok && snap != nil {
			meta, metaErr := n.loadSnapshotMetaRecord(height)
			if metaErr == nil && meta != nil {
				expected := snapshotMetaFromSnapshot(snap, meta.Source, meta.StateType, meta.BaseHeight)
				normalizeSnapshotMetaRecord(expected)
				if !strings.EqualFold(meta.SnapshotHash, expected.SnapshotHash) ||
					!strings.EqualFold(meta.StateRoot, expected.StateRoot) ||
					!strings.EqualFold(meta.ValidatorSetHash, expected.ValidatorSetHash) ||
					!strings.EqualFold(meta.ValidatorRegistryHash, expected.ValidatorRegistryHash) {
					if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_meta:%d", height), livenessReasonLogCooldown) {
						log.Printf("[SNAPSHOT-INTEGRITY] height=%d kind=meta_mismatch", height)
					}
					if err := n.storeSnapshotMetaRecord(height, snap, "integrity_repair", "committed_full", snapshotBaseHeight(height)); err != nil {
						return err
					}
				}
			} else if metaErr != nil {
				if err := n.storeSnapshotMetaRecord(height, snap, "integrity_backfill", "committed_full", snapshotBaseHeight(height)); err != nil {
					return err
				}
			}
			continue
		}
		recordExists := n.committedStateSnapshotRecordExists(height)
		if !recordExists && height < tip {
			if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_materializing:%d", height), livenessReasonLogCooldown) {
				log.Printf("[SNAPSHOT-MATERIALIZING] height=%d chain_tip=%d kind=missing_committed", height, tip)
			}
			continue
		}
		if !recordExists && height == tip {
			if _, repaired := n.ensureCommittedTipStateSnapshot(height, "integrity_monitor"); repaired {
				continue
			}
		}
		if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_missing:%d", height), livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-INTEGRITY] height=%d kind=missing_or_invalid", height)
		}
		if recordExists {
			if err := n.quarantineCommittedSnapshotHeight(height); err != nil {
				return err
			}
		}
		if rebuilt, rebuildErr := n.rebuildCommittedSnapshotHeight(height); rebuildErr == nil && rebuilt {
			continue
		} else if rebuildErr != nil && n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_rebuild_error:%d", height), livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-INTEGRITY] height=%d kind=rebuild_error err=%v", height, rebuildErr)
		}
	}
	if tipMeta, err := n.loadTipSnapshotMeta(); err == nil && tipMeta != nil && tipMeta.Height > 0 {
		if snap, _, _, ok := n.ResolveCommittedStateSnapshot(tipMeta.Height); ok && snap != nil {
			if !strings.EqualFold(strings.TrimSpace(tipMeta.StateRoot), strings.TrimSpace(snap.StateRoot)) ||
				!strings.EqualFold(strings.TrimSpace(tipMeta.ValidatorRegistryHash), strings.TrimSpace(snapshotValidatorRegistryHash(snap))) {
				_ = n.clearTipSnapshotRecords()
			}
		}
	}
	return nil
}

func (n *Node) quarantineCommittedSnapshotHeight(height uint64) error {
	if n == nil || height == 0 || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	snapshotKey := []byte(fmt.Sprintf("snapshot:%d", height))
	for _, store := range n.DB.SnapshotStoresForRead() {
		if err := store.Update(func(txn *Txn) error {
			item, err := txn.Get(snapshotKey)
			if err != nil {
				if errors.Is(err, ErrKeyNotFound) {
					return nil
				}
				return err
			}
			var raw []byte
			if err := item.Value(func(val []byte) error {
				raw = append([]byte{}, val...)
				return nil
			}); err != nil {
				return err
			}
			quarantineKey := []byte(fmt.Sprintf("quarantine:snapshot:%d:%d", height, time.Now().UnixNano()))
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

func (n *Node) rebuildCommittedSnapshotHeight(height uint64) (bool, error) {
	if n == nil || height < 2 {
		return false, nil
	}
	var base *StateSnapshot
	for prev := height - 1; prev >= 1; prev-- {
		if snap, _, _, ok := n.ResolveCommittedStateSnapshot(prev); ok && snap != nil {
			base = snap
			break
		}
		if raw, err := n.GetSnapshot(prev); err == nil && raw != nil {
			if _, reason := n.verifySnapshotAgainstLocalBlockDetailed(raw); reason != "" &&
				n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_base_verify:%d:%s", prev, reason), livenessReasonLogCooldown) {
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
	working := cloneStateSnapshot(base)
	if working == nil {
		return false, nil
	}
	for next := working.Height + 1; next <= height; next++ {
		delta, err := n.loadStateDeltaSnapshot(next)
		if err != nil {
			return false, err
		}
		working, err = applyStateDeltaSnapshot(working, delta)
		if err != nil {
			return false, err
		}
	}
	if ok, reason := n.verifySnapshotAgainstLocalBlockDetailed(working); !ok {
		if n.shouldLogLivenessReason(fmt.Sprintf("snapshot_integrity_rebuild_verify:%d:%s", height, reason), livenessReasonLogCooldown) {
			log.Printf("[SNAPSHOT-INTEGRITY] height=%d kind=rebuild_verify_failed reason=%s", height, strings.TrimSpace(reason))
		}
		return false, nil
	}
	if err := n.storeCommittedStateSnapshotRecord(working, "integrity_rebuild"); err != nil {
		return false, err
	}
	if len(working.ValidatorRegistry) > 0 {
		if err := n.storeValidatorRegistrySnapshotRecord(height, working.ValidatorRegistry); err != nil {
			return false, err
		}
	}
	return true, nil
}
