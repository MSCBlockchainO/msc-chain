package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	consensusSafetyDBKey               = "consensus:safety:v1"
	consensusSafetyJournalPrefix       = "consensus:safety:v2:"
	consensusSafetyJournalLatestKey    = consensusSafetyJournalPrefix + "latest"
	consensusSafetyJournalRecordPrefix = consensusSafetyJournalPrefix + "record:"
	consensusSafetyVersion             = 1
	consensusSafetyJournalVersion      = 1
	consensusSafetyJournalKeepRecords  = 16
	finalizedHashDBPrefix              = "finalized_hash:"
	consensusEvidenceDBPrefix          = "consensus_evidence:"
	consensusTelemetryJSONL            = "consensus_events.jsonl"
	consensusEvidenceVersion           = 1
	consensusTelemetryFileMode         = 0600
)

var consensusTelemetryMu sync.Mutex

type consensusSafetySnapshot struct {
	Version       int    `json:"version"`
	NodeID        string `json:"node_id"`
	SavedAtUnix   int64  `json:"saved_at_unix"`
	Reason        string `json:"reason,omitempty"`
	Height        uint64 `json:"height"`
	Round         uint32 `json:"round"`
	Phase         ConsensusPhase
	RoundStart    int64 `json:"round_start_unix,omitempty"`
	LastFinalized uint64

	LockedBlock        string `json:"locked_block,omitempty"`
	LockedBlockHash    string `json:"locked_block_hash,omitempty"`
	LockedRound        uint32 `json:"locked_round,omitempty"`
	Committed          bool   `json:"committed"`
	LastProposedHeight uint64 `json:"last_proposed_height,omitempty"`
	LastProposedRound  uint32 `json:"last_proposed_round,omitempty"`

	Votes     map[uint64]map[string]BlockVote       `json:"votes,omitempty"`
	Proposals map[uint64]Block                      `json:"proposals,omitempty"`
	ExecVotes map[string]map[string]ExecutionResult `json:"exec_votes,omitempty"`

	AcceptedProposal       map[string]string            `json:"accepted_proposal,omitempty"`
	QuorumLockedProposal   map[string]string            `json:"quorum_locked_proposal,omitempty"`
	AcceptedProposalBlocks map[string]Block             `json:"accepted_proposal_blocks,omitempty"`
	LocalExecVoteByRound   map[uint64]map[uint32]string `json:"local_exec_vote_by_round,omitempty"`

	CommitVotes      map[uint64]map[string][]string `json:"commit_votes,omitempty"`
	CommitVoted      map[uint64]map[string]string   `json:"commit_voted,omitempty"`
	CommittedHashes  map[uint64]string              `json:"committed_hashes,omitempty"`
	CommittedHeight  uint64                         `json:"committed_height,omitempty"`
	FinalizedHeight  uint64                         `json:"finalized_height,omitempty"`
	LastCommitHeight uint64                         `json:"last_commit_height,omitempty"`
	LastCommitAtUnix int64                          `json:"last_commit_at_unix,omitempty"`
}

type consensusSafetyJournalRecord struct {
	Version     int    `json:"version"`
	Seq         uint64 `json:"seq"`
	SavedAtUnix int64  `json:"saved_at_unix"`
	SHA256      string `json:"sha256"`
	Payload     []byte `json:"payload"`
}

type consensusEvidenceRecord struct {
	Version    int    `json:"version"`
	Type       string `json:"type"`
	Key        string `json:"key"`
	Height     uint64 `json:"height,omitempty"`
	Round      uint32 `json:"round,omitempty"`
	Validator  string `json:"validator,omitempty"`
	Expected   string `json:"expected,omitempty"`
	Got        string `json:"got,omitempty"`
	BlockHash  string `json:"block_hash,omitempty"`
	PrevHash   string `json:"prev_hash,omitempty"`
	SeenAtUnix int64  `json:"seen_at_unix"`
}

type consensusTelemetryEvent struct {
	At                  string                 `json:"at"`
	UnixMillis          int64                  `json:"unix_ms"`
	Node                string                 `json:"node,omitempty"`
	Type                string                 `json:"type"`
	Reason              string                 `json:"reason,omitempty"`
	Height              uint64                 `json:"height,omitempty"`
	Round               uint32                 `json:"round,omitempty"`
	BlockHash           string                 `json:"block_hash,omitempty"`
	ConsensusMode       string                 `json:"consensus_mode,omitempty"`
	QuorumPolicyVersion string                 `json:"quorum_policy_version,omitempty"`
	Required            int                    `json:"required,omitempty"`
	ActiveReady         int                    `json:"active_ready,omitempty"`
	Fields              map[string]interface{} `json:"fields,omitempty"`
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneBlockMap(in map[uint64]Block) map[uint64]Block {
	if len(in) == 0 {
		return nil
	}
	out := make(map[uint64]Block, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneStringBlockMap(in map[string]Block) map[string]Block {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]Block, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneBlockVoteMap(in map[uint64]map[string]BlockVote) map[uint64]map[string]BlockVote {
	if len(in) == 0 {
		return nil
	}
	out := make(map[uint64]map[string]BlockVote, len(in))
	for h, votes := range in {
		if len(votes) == 0 {
			continue
		}
		dst := make(map[string]BlockVote, len(votes))
		for signer, vote := range votes {
			dst[signer] = vote
		}
		out[h] = dst
	}
	return out
}

func cloneExecResultVoteMap(in map[string]map[string]ExecutionResult) map[string]map[string]ExecutionResult {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]map[string]ExecutionResult, len(in))
	for blockHash, votes := range in {
		if len(votes) == 0 {
			continue
		}
		dst := make(map[string]ExecutionResult, len(votes))
		for signer, vote := range votes {
			dst[signer] = vote
		}
		out[blockHash] = dst
	}
	return out
}

func cloneLocalExecVoteByRound(in map[uint64]map[uint32]string) map[uint64]map[uint32]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[uint64]map[uint32]string, len(in))
	for height, rounds := range in {
		if len(rounds) == 0 {
			continue
		}
		dst := make(map[uint32]string, len(rounds))
		for round, key := range rounds {
			dst[round] = key
		}
		out[height] = dst
	}
	return out
}

func proposalKeyEvidenceBlockHash(height uint64, proposalKey string, blocks map[string]Block, proposals map[uint64]Block) string {
	proposalKey = strings.TrimSpace(proposalKey)
	if height == 0 || proposalKey == "" {
		return ""
	}
	if blocks != nil {
		if block, ok := blocks[proposalKey]; ok && block.ID == height {
			return strings.TrimSpace(block.BlockHash)
		}
	}
	keyHeight, _, keyBlockHash, _, _, ok := proposalVoteKeyParts(proposalKey)
	if ok && keyHeight == height && keyBlockHash != "" {
		return strings.TrimSpace(keyBlockHash)
	}
	if proposals != nil {
		if block, ok := proposals[height]; ok && block.ID == height {
			return strings.TrimSpace(block.BlockHash)
		}
	}
	return ""
}

func proposalKeyHasExecVoteEvidence(height uint64, proposalKey string, blocks map[string]Block, proposals map[uint64]Block, execVotes map[string]map[string]ExecutionResult) bool {
	if height == 0 || strings.TrimSpace(proposalKey) == "" || len(execVotes) == 0 {
		return false
	}
	blockHash := proposalKeyEvidenceBlockHash(height, proposalKey, blocks, proposals)
	if blockHash == "" {
		return false
	}
	votes := execVotes[blockHash]
	if len(votes) == 0 {
		return false
	}
	for _, vote := range votes {
		if vote.Height == 0 || vote.Height == height {
			return true
		}
	}
	return false
}

func safetyProposalKeyBacked(height uint64, proposalKey string, accepted map[string]string, quorum map[string]string, blocks map[string]Block, proposals map[uint64]Block, execVotes map[string]map[string]ExecutionResult) bool {
	proposalKey = strings.TrimSpace(proposalKey)
	if height == 0 || proposalKey == "" {
		return false
	}
	heightKey := acceptedProposalHeightKey(height)
	if quorum != nil && strings.TrimSpace(quorum[heightKey]) == proposalKey {
		if proposalKeyHasExecVoteEvidence(height, proposalKey, blocks, proposals, execVotes) {
			return true
		}
		return strings.TrimSpace(proposalKeyEvidenceBlockHash(height, proposalKey, blocks, proposals)) != ""
	}
	if accepted != nil && strings.TrimSpace(accepted[heightKey]) == proposalKey {
		return proposalKeyHasExecVoteEvidence(height, proposalKey, blocks, proposals, execVotes)
	}
	if blocks != nil {
		if block, ok := blocks[proposalKey]; ok && block.ID == height {
			return proposalKeyHasExecVoteEvidence(height, proposalKey, blocks, proposals, execVotes)
		}
	}
	keyHeight, _, keyBlockHash, _, _, ok := proposalVoteKeyParts(proposalKey)
	if !ok || keyHeight != height || keyBlockHash == "" {
		return false
	}
	if proposals != nil {
		if block, ok := proposals[height]; ok && block.ID == height &&
			strings.EqualFold(strings.TrimSpace(block.BlockHash), keyBlockHash) {
			return proposalKeyHasExecVoteEvidence(height, proposalKey, blocks, proposals, execVotes)
		}
	}
	return false
}

func pruneUnbackedLocalExecVoteByRound(in map[uint64]map[uint32]string, accepted map[string]string, quorum map[string]string, blocks map[string]Block, proposals map[uint64]Block, execVotes map[string]map[string]ExecutionResult, activeHeight uint64) map[uint64]map[uint32]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[uint64]map[uint32]string, len(in))
	for height, rounds := range in {
		if len(rounds) == 0 {
			continue
		}
		if activeHeight > 0 && height != activeHeight {
			continue
		}
		dst := make(map[uint32]string, len(rounds))
		for round, proposalKey := range rounds {
			if safetyProposalKeyBacked(height, proposalKey, accepted, quorum, blocks, proposals, execVotes) {
				dst[round] = strings.TrimSpace(proposalKey)
			}
		}
		if len(dst) > 0 {
			out[height] = dst
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func flattenCommitVotes(in map[uint64]map[string]map[string]struct{}) map[uint64]map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[uint64]map[string][]string, len(in))
	for height, byHash := range in {
		if len(byHash) == 0 {
			continue
		}
		hashes := make(map[string][]string, len(byHash))
		for hash, signers := range byHash {
			if len(signers) == 0 {
				continue
			}
			list := make([]string, 0, len(signers))
			for signer := range signers {
				list = append(list, signer)
			}
			sort.Strings(list)
			hashes[hash] = list
		}
		out[height] = hashes
	}
	return out
}

func inflateCommitVotes(in map[uint64]map[string][]string) map[uint64]map[string]map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[uint64]map[string]map[string]struct{}, len(in))
	for height, byHash := range in {
		if len(byHash) == 0 {
			continue
		}
		hashes := make(map[string]map[string]struct{}, len(byHash))
		for hash, signers := range byHash {
			if len(signers) == 0 {
				continue
			}
			set := make(map[string]struct{}, len(signers))
			for _, signer := range signers {
				signer = normalizeValidatorID(signer)
				if signer != "" {
					set[signer] = struct{}{}
				}
			}
			hashes[hash] = set
		}
		out[height] = hashes
	}
	return out
}

func cloneCommitVoted(in map[uint64]map[string]string) map[uint64]map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[uint64]map[string]string, len(in))
	for height, votes := range in {
		if len(votes) == 0 {
			continue
		}
		dst := make(map[string]string, len(votes))
		for signer, hash := range votes {
			dst[signer] = hash
		}
		out[height] = dst
	}
	return out
}

func cloneCommittedHashes(in map[uint64]string) map[uint64]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[uint64]string, len(in))
	for height, hash := range in {
		out[height] = hash
	}
	return out
}

func (n *Node) snapshotConsensusSafetyState(reason string) consensusSafetySnapshot {
	snap := consensusSafetySnapshot{
		Version:     consensusSafetyVersion,
		NodeID:      strings.TrimSpace(n.ID),
		SavedAtUnix: time.Now().Unix(),
		Reason:      strings.TrimSpace(reason),
	}
	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		snap.Height = n.Consensus.Height
		snap.Round = n.Consensus.Round
		snap.Phase = n.Consensus.Phase
		if !n.Consensus.RoundStart.IsZero() {
			snap.RoundStart = n.Consensus.RoundStart.Unix()
		}
		snap.LastFinalized = n.Consensus.LastFinalized
		snap.LockedBlock = n.Consensus.LockedBlock
		snap.LockedBlockHash = n.Consensus.LockedBlockHash
		snap.LockedRound = n.Consensus.LockedRound
		snap.Committed = n.Consensus.Committed
		snap.LastProposedHeight = n.Consensus.LastProposedHeight
		snap.LastProposedRound = n.Consensus.LastProposedRound
		snap.Votes = cloneBlockVoteMap(n.Consensus.Votes)
		snap.Proposals = cloneBlockMap(n.Consensus.Proposals)
		snap.ExecVotes = cloneExecResultVoteMap(n.Consensus.ExecVotes)
		n.Consensus.mu.Unlock()
	}

	n.execResultsMu.Lock()
	snap.AcceptedProposal = cloneStringMap(n.acceptedProposal)
	snap.QuorumLockedProposal = cloneStringMap(n.quorumLockedProposal)
	snap.AcceptedProposalBlocks = cloneStringBlockMap(n.acceptedProposalBlocks)
	snap.LocalExecVoteByRound = cloneLocalExecVoteByRound(n.localExecVoteByRound)
	n.execResultsMu.Unlock()
	snap.LocalExecVoteByRound = pruneUnbackedLocalExecVoteByRound(
		snap.LocalExecVoteByRound,
		snap.AcceptedProposal,
		snap.QuorumLockedProposal,
		snap.AcceptedProposalBlocks,
		snap.Proposals,
		snap.ExecVotes,
		snap.Height,
	)

	n.commitMu.Lock()
	snap.CommitVotes = flattenCommitVotes(n.commitVotes)
	snap.CommitVoted = cloneCommitVoted(n.commitVoted)
	snap.CommittedHashes = cloneCommittedHashes(n.committed)
	snap.CommittedHeight = n.committedHeight
	snap.FinalizedHeight = n.finalizedHeight
	snap.LastCommitHeight = n.lastCommitHeight
	if !n.lastCommitAt.IsZero() {
		snap.LastCommitAtUnix = n.lastCommitAt.Unix()
	}
	n.commitMu.Unlock()
	return snap
}

func consensusSafetyJournalRecordKey(seq uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", consensusSafetyJournalRecordPrefix, seq))
}

func consensusSafetyJournalSeqFromKey(key []byte) (uint64, bool) {
	raw := strings.TrimPrefix(string(key), consensusSafetyJournalRecordPrefix)
	if raw == string(key) || raw == "" {
		return 0, false
	}
	seq, err := strconv.ParseUint(raw, 10, 64)
	return seq, err == nil && seq > 0
}

func encodeConsensusSafetyJournalRecord(seq uint64, payload []byte) ([]byte, error) {
	if seq == 0 {
		return nil, errors.New("consensus_safety_journal_seq_missing")
	}
	if len(payload) == 0 {
		return nil, errors.New("consensus_safety_journal_payload_missing")
	}
	sum := sha256.Sum256(payload)
	rec := consensusSafetyJournalRecord{
		Version:     consensusSafetyJournalVersion,
		Seq:         seq,
		SavedAtUnix: time.Now().Unix(),
		SHA256:      hex.EncodeToString(sum[:]),
		Payload:     append([]byte{}, payload...),
	}
	return json.Marshal(rec)
}

func decodeConsensusSafetyJournalRecord(raw []byte) (consensusSafetySnapshot, uint64, error) {
	var snap consensusSafetySnapshot
	plain, err := decryptDBValue(raw)
	if err != nil {
		return snap, 0, err
	}
	var rec consensusSafetyJournalRecord
	if err := json.Unmarshal(plain, &rec); err != nil {
		return snap, 0, err
	}
	if rec.Version != consensusSafetyJournalVersion || rec.Seq == 0 || len(rec.Payload) == 0 {
		return snap, 0, errors.New("consensus_safety_journal_record_invalid")
	}
	sum := sha256.Sum256(rec.Payload)
	if !strings.EqualFold(strings.TrimSpace(rec.SHA256), hex.EncodeToString(sum[:])) {
		return snap, 0, errors.New("consensus_safety_journal_checksum_mismatch")
	}
	if err := json.Unmarshal(rec.Payload, &snap); err != nil {
		return snap, 0, err
	}
	if snap.Version != consensusSafetyVersion {
		return snap, 0, errors.New("consensus_safety_snapshot_version_invalid")
	}
	return snap, rec.Seq, nil
}

func decodeConsensusSafetyJournalLatest(raw []byte) (uint64, error) {
	plain, err := decryptDBValue(raw)
	if err != nil {
		return 0, err
	}
	seq, err := strconv.ParseUint(strings.TrimSpace(string(plain)), 10, 64)
	if err != nil || seq == 0 {
		return 0, errors.New("consensus_safety_journal_latest_invalid")
	}
	return seq, nil
}

func (n *Node) latestConsensusSafetyJournalSeq() uint64 {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return 0
	}
	seq := uint64(0)
	_ = n.DB.Meta.View(func(txn *Txn) error {
		item, err := txn.Get([]byte(consensusSafetyJournalLatestKey))
		if err == nil && item != nil {
			_ = item.Value(func(val []byte) error {
				decoded, derr := decodeConsensusSafetyJournalLatest(val)
				if derr == nil {
					seq = decoded
				}
				return nil
			})
		}
		return nil
	})
	return seq
}

func consensusSafetyJournalNextSeq(txn *Txn) uint64 {
	seq := uint64(time.Now().UnixNano())
	if txn != nil {
		if item, err := txn.Get([]byte(consensusSafetyJournalLatestKey)); err == nil && item != nil {
			_ = item.Value(func(val []byte) error {
				prev, derr := decodeConsensusSafetyJournalLatest(val)
				if derr == nil && prev >= seq {
					seq = prev + 1
				}
				return nil
			})
		}
	}
	if seq == 0 {
		seq = 1
	}
	return seq
}

func consensusSafetyJournalPruneLocked(txn *Txn) error {
	if txn == nil || consensusSafetyJournalKeepRecords <= 0 {
		return nil
	}
	prefix := []byte(consensusSafetyJournalRecordPrefix)
	seqs := make([]uint64, 0)
	it := txn.NewIterator(IteratorOptions{Prefix: prefix})
	defer it.Close()
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		if seq, ok := consensusSafetyJournalSeqFromKey(it.Item().Key()); ok {
			seqs = append(seqs, seq)
		}
	}
	if len(seqs) <= consensusSafetyJournalKeepRecords {
		return nil
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	for _, seq := range seqs[:len(seqs)-consensusSafetyJournalKeepRecords] {
		if err := txn.Delete(consensusSafetyJournalRecordKey(seq)); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) loadConsensusSafetySnapshotFromJournal() (consensusSafetySnapshot, bool, error) {
	var best consensusSafetySnapshot
	bestSeq := uint64(0)
	var lastErr error
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return best, false, nil
	}
	err := n.DB.Meta.View(func(txn *Txn) error {
		prefix := []byte(consensusSafetyJournalRecordPrefix)
		it := txn.NewIterator(IteratorOptions{Prefix: prefix})
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			if item == nil {
				continue
			}
			keySeq, ok := consensusSafetyJournalSeqFromKey(item.Key())
			if !ok {
				continue
			}
			_ = item.Value(func(val []byte) error {
				snap, recSeq, derr := decodeConsensusSafetyJournalRecord(val)
				if derr != nil {
					lastErr = derr
					return nil
				}
				if recSeq != keySeq {
					lastErr = errors.New("consensus_safety_journal_key_seq_mismatch")
					return nil
				}
				if recSeq > bestSeq {
					bestSeq = recSeq
					best = snap
				}
				return nil
			})
		}
		return nil
	})
	if err != nil {
		return best, false, err
	}
	if bestSeq > 0 {
		return best, true, nil
	}
	return best, false, lastErr
}

func (n *Node) persistConsensusSafetyState(reason string) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return nil
	}
	snap := n.snapshotConsensusSafetyState(reason)
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if err := n.DB.Meta.Update(func(txn *Txn) error {
		seq := consensusSafetyJournalNextSeq(txn)
		record, err := encodeConsensusSafetyJournalRecord(seq, data)
		if err != nil {
			return err
		}
		encRecord, err := encryptDBValue(record)
		if err != nil {
			return err
		}
		if err := txn.Set(consensusSafetyJournalRecordKey(seq), encRecord); err != nil {
			return err
		}
		latest, err := encryptDBValue([]byte(fmt.Sprintf("%020d", seq)))
		if err != nil {
			return err
		}
		if err := txn.Set([]byte(consensusSafetyJournalLatestKey), latest); err != nil {
			return err
		}
		if err := consensusSafetyJournalPruneLocked(txn); err != nil {
			return err
		}
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set([]byte(consensusSafetyDBKey), enc)
	}); err != nil {
		return err
	}
	n.emitConsensusTelemetry(consensusTelemetryEvent{
		Type:      "consensus_safety_persisted",
		Reason:    reason,
		Height:    snap.Height,
		Round:     snap.Round,
		BlockHash: snap.LockedBlockHash,
		Fields: map[string]interface{}{
			"committed_height": snap.CommittedHeight,
			"finalized_height": snap.FinalizedHeight,
		},
	})
	return nil
}

func (n *Node) persistConsensusSafetyStateAsync(reason string) {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return
	}
	go func() {
		_ = n.persistConsensusSafetyState(reason)
	}()
}

func (n *Node) restoreConsensusSafetyState() error {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return nil
	}
	var snap consensusSafetySnapshot
	snap, foundJournal, journalErr := n.loadConsensusSafetySnapshotFromJournal()
	err := journalErr
	if !foundJournal {
		legacyFound := false
		err = n.DB.Meta.View(func(txn *Txn) error {
			item, err := txn.Get([]byte(consensusSafetyDBKey))
			if err != nil {
				if errors.Is(err, ErrKeyNotFound) {
					return nil
				}
				return err
			}
			legacyFound = true
			return item.Value(func(val []byte) error {
				plain, err := decryptDBValue(val)
				if err != nil {
					return err
				}
				return json.Unmarshal(plain, &snap)
			})
		})
		if err == nil && !legacyFound && journalErr != nil {
			err = journalErr
		}
	}
	if err != nil || snap.Version == 0 {
		return err
	}
	chainHeight := uint64(0)
	if n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}
	restoreActiveRound := snap.Height <= chainHeight
	if !restoreActiveRound &&
		chainHeight > 0 &&
		snap.Height == chainHeight+1 &&
		snap.CommittedHeight <= chainHeight &&
		!snap.Committed {
		restoreActiveRound = true
	}

	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		if restoreActiveRound && snap.Height > n.Consensus.Height {
			n.Consensus.Height = snap.Height
		}
		if chainHeight > n.Consensus.Height {
			n.Consensus.Height = chainHeight
		}
		if restoreActiveRound {
			n.Consensus.Round = snap.Round
			n.Consensus.Phase = snap.Phase
			if snap.RoundStart > 0 {
				n.Consensus.RoundStart = time.Unix(snap.RoundStart, 0)
			}
			n.Consensus.LastFinalized = snap.LastFinalized
			n.Consensus.LockedBlock = strings.TrimSpace(snap.LockedBlock)
			n.Consensus.LockedBlockHash = strings.TrimSpace(snap.LockedBlockHash)
			n.Consensus.LockedRound = snap.LockedRound
			n.Consensus.Committed = snap.Committed
			n.Consensus.LastProposedHeight = snap.LastProposedHeight
			n.Consensus.LastProposedRound = snap.LastProposedRound
			if snap.Votes != nil {
				n.Consensus.Votes = cloneBlockVoteMap(snap.Votes)
			}
			if snap.Proposals != nil {
				n.Consensus.Proposals = cloneBlockMap(snap.Proposals)
			}
			if snap.ExecVotes != nil {
				n.Consensus.ExecVotes = cloneExecResultVoteMap(snap.ExecVotes)
			}
		} else {
			n.Consensus.Round = 0
			n.Consensus.Phase = PhasePropose
			n.Consensus.RoundStart = time.Now()
			n.Consensus.LockedBlock = ""
			n.Consensus.LockedBlockHash = ""
			n.Consensus.LockedRound = 0
			n.Consensus.Committed = false
			if n.Consensus.Votes == nil {
				n.Consensus.Votes = make(map[uint64]map[string]BlockVote)
			}
			if n.Consensus.Proposals == nil {
				n.Consensus.Proposals = make(map[uint64]Block)
			}
			if n.Consensus.ExecVotes == nil {
				n.Consensus.ExecVotes = make(map[string]map[string]ExecutionResult)
			}
		}
		n.Consensus.Syncing = false
		n.Consensus.Paused = false
		n.Consensus.mu.Unlock()
	}

	if restoreActiveRound {
		n.execResultsMu.Lock()
		if snap.AcceptedProposal != nil {
			n.acceptedProposal = cloneStringMap(snap.AcceptedProposal)
		}
		if snap.QuorumLockedProposal != nil {
			n.quorumLockedProposal = cloneStringMap(snap.QuorumLockedProposal)
		}
		if snap.AcceptedProposalBlocks != nil {
			n.acceptedProposalBlocks = cloneStringBlockMap(snap.AcceptedProposalBlocks)
		}
		if snap.LocalExecVoteByRound != nil {
			n.localExecVoteByRound = pruneUnbackedLocalExecVoteByRound(
				cloneLocalExecVoteByRound(snap.LocalExecVoteByRound),
				n.acceptedProposal,
				n.quorumLockedProposal,
				n.acceptedProposalBlocks,
				snap.Proposals,
				snap.ExecVotes,
				snap.Height,
			)
		} else {
			n.localExecVoteByRound = make(map[uint64]map[uint32]string)
		}
		n.execResultsMu.Unlock()
	} else {
		n.execResultsMu.Lock()
		n.acceptedProposal = make(map[string]string)
		n.quorumLockedProposal = make(map[string]string)
		n.acceptedProposalBlocks = make(map[string]Block)
		if n.localExecVoteByRound == nil {
			n.localExecVoteByRound = make(map[uint64]map[uint32]string)
		}
		n.execResultsMu.Unlock()
	}

	n.commitMu.Lock()
	if chainHeight == 0 {
		n.committedHeight = 0
		n.finalizedHeight = 0
		n.lastCommitHeight = 0
	} else {
		if n.committedHeight > chainHeight {
			n.committedHeight = chainHeight
		}
		if n.finalizedHeight > chainHeight {
			n.finalizedHeight = chainHeight
		}
		if n.lastCommitHeight > chainHeight {
			n.lastCommitHeight = chainHeight
		}
	}
	if snap.CommitVotes != nil {
		n.commitVotes = inflateCommitVotes(snap.CommitVotes)
		for height := range n.commitVotes {
			if chainHeight == 0 || height > chainHeight {
				delete(n.commitVotes, height)
			}
		}
	}
	if snap.CommitVoted != nil {
		n.commitVoted = cloneCommitVoted(snap.CommitVoted)
		for height := range n.commitVoted {
			if chainHeight == 0 || height > chainHeight {
				delete(n.commitVoted, height)
			}
		}
	}
	if n.committed == nil {
		n.committed = make(map[uint64]string)
	}
	for height, hash := range snap.CommittedHashes {
		if strings.TrimSpace(hash) != "" && chainHeight > 0 && height <= chainHeight {
			n.committed[height] = hash
		}
	}
	for height := range n.committed {
		if chainHeight == 0 || height > chainHeight {
			delete(n.committed, height)
		}
	}
	restoredCommittedHeight := snap.CommittedHeight
	if chainHeight == 0 {
		restoredCommittedHeight = 0
	} else if restoredCommittedHeight == 0 || restoredCommittedHeight > chainHeight {
		restoredCommittedHeight = chainHeight
	}
	if restoredCommittedHeight > n.committedHeight {
		n.committedHeight = restoredCommittedHeight
	}
	restoredFinalizedHeight := snap.FinalizedHeight
	if chainHeight == 0 {
		restoredFinalizedHeight = 0
	} else if restoredFinalizedHeight == 0 || restoredFinalizedHeight > chainHeight {
		restoredFinalizedHeight = chainHeight
	}
	if restoredFinalizedHeight > n.finalizedHeight {
		n.finalizedHeight = restoredFinalizedHeight
	}
	restoredLastCommitHeight := snap.LastCommitHeight
	if chainHeight == 0 {
		restoredLastCommitHeight = 0
	} else if restoredLastCommitHeight == 0 || restoredLastCommitHeight > chainHeight {
		restoredLastCommitHeight = restoredCommittedHeight
		if restoredLastCommitHeight == 0 || restoredLastCommitHeight > chainHeight {
			restoredLastCommitHeight = chainHeight
		}
	}
	if restoredLastCommitHeight > n.lastCommitHeight {
		n.lastCommitHeight = restoredLastCommitHeight
	}
	if snap.LastCommitAtUnix > 0 && n.lastCommitAt.IsZero() {
		n.lastCommitAt = time.Unix(snap.LastCommitAtUnix, 0)
	}
	n.commitMu.Unlock()

	n.emitConsensusTelemetry(consensusTelemetryEvent{
		Type:   "consensus_safety_restored",
		Reason: snap.Reason,
		Height: snap.Height,
		Round:  snap.Round,
		Fields: map[string]interface{}{
			"chain_height":         chainHeight,
			"restore_active_round": restoreActiveRound,
			"committed_height":     snap.CommittedHeight,
			"restored_committed":   restoredCommittedHeight,
			"finalized_height":     snap.FinalizedHeight,
			"restored_finalized":   restoredFinalizedHeight,
			"last_commit_height":   snap.LastCommitHeight,
			"restored_last_commit": restoredLastCommitHeight,
		},
	})
	return nil
}

func finalizedHashDBKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", finalizedHashDBPrefix, height))
}

func (n *Node) loadFinalizedHashInvariant(height uint64) (string, bool, error) {
	if n == nil || n.DB == nil || n.DB.Meta == nil || height == 0 {
		return "", false, nil
	}
	var out string
	found := false
	err := n.DB.Meta.View(func(txn *Txn) error {
		item, err := txn.Get(finalizedHashDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			plain, err := decryptDBValue(val)
			if err != nil {
				return err
			}
			out = strings.TrimSpace(string(plain))
			return nil
		})
	})
	return out, found, err
}

func (n *Node) persistFinalizedHashInvariant(block Block) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil || block.ID == 0 {
		return nil
	}
	hash := strings.TrimSpace(block.BlockHash)
	if hash == "" {
		return nil
	}
	return n.DB.Meta.Update(func(txn *Txn) error {
		key := finalizedHashDBKey(block.ID)
		item, err := txn.Get(key)
		if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return err
		}
		if err == nil && item != nil {
			var existing string
			if vErr := item.Value(func(val []byte) error {
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				existing = strings.TrimSpace(string(plain))
				return nil
			}); vErr != nil {
				return vErr
			}
			if existing != "" && !strings.EqualFold(existing, hash) {
				return fmt.Errorf("finalized hash immutable violation height=%d existing=%s got=%s", block.ID, existing, hash)
			}
			return nil
		}
		enc, err := encryptDBValue([]byte(hash))
		if err != nil {
			return err
		}
		return txn.Set(key, enc)
	})
}

type finalizedHashBackfillConflict struct {
	Height   uint64
	Round    uint32
	Expected string
	Got      string
}

func (n *Node) backfillFinalizedHashInvariants(blocks []Block, reason string) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil || len(blocks) == 0 {
		return nil
	}
	writes := 0
	conflicts := make([]finalizedHashBackfillConflict, 0)
	err := n.DB.Meta.Update(func(txn *Txn) error {
		for _, block := range blocks {
			if block.ID == 0 {
				continue
			}
			hash := strings.TrimSpace(block.BlockHash)
			if hash == "" {
				continue
			}
			key := finalizedHashDBKey(block.ID)
			item, err := txn.Get(key)
			if err != nil && !errors.Is(err, ErrKeyNotFound) {
				return err
			}
			if err == nil && item != nil {
				existing := ""
				if vErr := item.Value(func(val []byte) error {
					plain, derr := decryptDBValue(val)
					if derr != nil {
						return derr
					}
					existing = strings.TrimSpace(string(plain))
					return nil
				}); vErr != nil {
					return vErr
				}
				if existing != "" && !strings.EqualFold(existing, hash) {
					conflicts = append(conflicts, finalizedHashBackfillConflict{
						Height:   block.ID,
						Round:    block.Round,
						Expected: existing,
						Got:      hash,
					})
				}
				continue
			}
			enc, err := encryptDBValue([]byte(hash))
			if err != nil {
				return err
			}
			if err := txn.Set(key, enc); err != nil {
				return err
			}
			writes++
		}
		return nil
	})
	for _, conflict := range conflicts {
		n.recordFinalizedHashConflictEvidence(conflict.Height, conflict.Round, conflict.Expected, conflict.Got, "startup_chain_invariant_conflict")
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:      "finalized_hash_conflict",
			Reason:    "startup_chain_invariant_conflict",
			Height:    conflict.Height,
			Round:     conflict.Round,
			BlockHash: conflict.Got,
			Fields: map[string]interface{}{
				"expected": conflict.Expected,
				"got":      conflict.Got,
			},
		})
	}
	if err == nil && writes > 0 {
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:   "finalized_hash_backfilled",
			Reason: strings.TrimSpace(reason),
			Fields: map[string]interface{}{
				"count": writes,
			},
		})
	}
	return err
}

func (n *Node) persistSnapshotCommitSafety(anchor Block, reason string) {
	if n == nil || anchor.ID == 0 || strings.TrimSpace(anchor.BlockHash) == "" {
		return
	}
	if err := n.persistFinalizedHashInvariant(anchor); err != nil {
		n.recordFinalizedHashConflictEvidence(anchor.ID, anchor.Round, "", anchor.BlockHash, strings.TrimSpace(reason)+"_invariant")
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:      "finalized_hash_conflict",
			Reason:    strings.TrimSpace(reason) + "_invariant",
			Height:    anchor.ID,
			Round:     anchor.Round,
			BlockHash: anchor.BlockHash,
			Fields: map[string]interface{}{
				"error": err.Error(),
			},
		})
		return
	}
	if err := n.persistConsensusSafetyState(reason); err != nil {
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:      "consensus_safety_persist_failed",
			Reason:    strings.TrimSpace(reason),
			Height:    anchor.ID,
			Round:     anchor.Round,
			BlockHash: anchor.BlockHash,
			Fields: map[string]interface{}{
				"error": err.Error(),
			},
		})
	}
}

func finalizedHashConflictEvidenceKey(height uint64, round uint32, expectedHash, gotHash, reason string) string {
	if height == 0 {
		return ""
	}
	expectedHash = strings.ToLower(strings.TrimSpace(expectedHash))
	gotHash = strings.ToLower(strings.TrimSpace(gotHash))
	reason = strings.ToLower(strings.TrimSpace(reason))
	if expectedHash == "" {
		expectedHash = "unknown"
	}
	if gotHash == "" {
		gotHash = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}
	return fmt.Sprintf("%d|%d|%s|%s|%s", height, round, expectedHash, gotHash, reason)
}

func (n *Node) committedHashForEvidence(height uint64) string {
	if n == nil || height == 0 {
		return ""
	}
	if committedHash, ok := n.getCommittedHash(height); ok {
		return strings.TrimSpace(committedHash)
	}
	persistedHash, found, err := n.loadFinalizedHashInvariant(height)
	if err == nil && found {
		return strings.TrimSpace(persistedHash)
	}
	return ""
}

func (n *Node) recordFinalizedHashConflictEvidence(height uint64, round uint32, expectedHash, gotHash, reason string) {
	if n == nil || height == 0 {
		return
	}
	gotHash = strings.TrimSpace(gotHash)
	if gotHash == "" {
		return
	}
	expectedHash = strings.TrimSpace(expectedHash)
	if expectedHash == "" {
		expectedHash = n.committedHashForEvidence(height)
	}
	key := finalizedHashConflictEvidenceKey(height, round, expectedHash, gotHash, reason)
	if key == "" {
		return
	}
	now := time.Now()
	n.persistConsensusEvidenceRecord(consensusEvidenceRecord{
		Type:       "finalized_hash_conflict",
		Key:        key,
		Height:     height,
		Round:      round,
		Expected:   strings.ToLower(strings.TrimSpace(expectedHash)),
		Got:        strings.ToLower(strings.TrimSpace(gotHash)),
		BlockHash:  strings.ToLower(strings.TrimSpace(gotHash)),
		PrevHash:   strings.ToLower(strings.TrimSpace(expectedHash)),
		SeenAtUnix: now.Unix(),
	})
}

func (n *Node) pruneFinalizedHashInvariantsAboveHeight(height uint64) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return nil
	}
	prefix := []byte(finalizedHashDBPrefix)
	return n.DB.Meta.Update(func(txn *Txn) error {
		it := txn.NewIterator(DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := append([]byte(nil), it.Item().Key()...)
			rawHeight := strings.TrimPrefix(string(key), finalizedHashDBPrefix)
			h, err := strconv.ParseUint(rawHeight, 10, 64)
			if err != nil || h <= height {
				continue
			}
			if err := txn.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
}

func consensusEvidenceDBKey(ev consensusEvidenceRecord) []byte {
	sum := sha256.Sum256([]byte(strings.TrimSpace(ev.Type) + "|" + strings.TrimSpace(ev.Key)))
	return []byte(consensusEvidenceDBPrefix + hex.EncodeToString(sum[:]))
}

func (n *Node) persistConsensusEvidenceRecord(ev consensusEvidenceRecord) {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return
	}
	ev.Type = strings.TrimSpace(ev.Type)
	ev.Key = strings.TrimSpace(ev.Key)
	if ev.Type == "" || ev.Key == "" {
		return
	}
	if ev.Version == 0 {
		ev.Version = consensusEvidenceVersion
	}
	if ev.SeenAtUnix <= 0 {
		ev.SeenAtUnix = time.Now().Unix()
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
		return txn.Set(consensusEvidenceDBKey(ev), enc)
	})
	n.emitConsensusTelemetry(consensusTelemetryEvent{
		Type:      "consensus_evidence_persisted",
		Reason:    ev.Type,
		Height:    ev.Height,
		Round:     ev.Round,
		BlockHash: ev.BlockHash,
		Fields: map[string]interface{}{
			"validator": ev.Validator,
			"expected":  ev.Expected,
			"got":       ev.Got,
			"key":       ev.Key,
		},
	})
}

func (n *Node) loadConsensusEvidenceSeenFromDB() error {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil
	}
	records := make([]consensusEvidenceRecord, 0)
	err := n.DB.State.View(func(txn *Txn) error {
		opts := DefaultIteratorOptions
		opts.Prefix = []byte(consensusEvidenceDBPrefix)
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			if item == nil {
				continue
			}
			if err := item.Value(func(val []byte) error {
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				var ev consensusEvidenceRecord
				if uerr := json.Unmarshal(plain, &ev); uerr != nil {
					return nil
				}
				if ev.Key != "" && ev.Type != "" {
					records = append(records, ev)
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	for _, ev := range records {
		seenAt := time.Unix(ev.SeenAtUnix, 0)
		if ev.SeenAtUnix <= 0 {
			seenAt = time.Now()
		}
		switch strings.TrimSpace(ev.Type) {
		case "double_proposal":
			n.doubleProposalMu.Lock()
			if n.doubleProposalEvidenceSeen == nil {
				n.doubleProposalEvidenceSeen = make(map[string]time.Time)
			}
			n.doubleProposalEvidenceSeen[ev.Key] = seenAt
			n.doubleProposalMu.Unlock()
		case "invalid_proposer":
			n.invalidProposerMu.Lock()
			if n.invalidProposerEvidenceSeen == nil {
				n.invalidProposerEvidenceSeen = make(map[string]time.Time)
			}
			n.invalidProposerEvidenceSeen[ev.Key] = seenAt
			n.invalidProposerMu.Unlock()
		}
	}
	return nil
}

func (n *Node) emitConsensusTelemetry(ev consensusTelemetryEvent) {
	if n == nil || strings.TrimSpace(ev.Type) == "" {
		return
	}
	now := time.Now().UTC()
	ev.At = now.Format(time.RFC3339Nano)
	ev.UnixMillis = now.UnixMilli()
	if ev.Node == "" {
		ev.Node = strings.TrimSpace(n.ID)
	}
	dir := nodeDataPath(n.DataDir, n.ID)
	if strings.TrimSpace(n.DataDir) == "" || strings.TrimSpace(n.ID) == "" {
		return
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	path := filepath.Join(dir, consensusTelemetryJSONL)
	consensusTelemetryMu.Lock()
	defer consensusTelemetryMu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, consensusTelemetryFileMode)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}
