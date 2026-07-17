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
	// `consensusSafetyDBKey` defines the key used to access the related value.
	consensusSafetyDBKey = "consensus:safety:v1"
	// `consensusSafetyJournalPrefix` defines the constant value used by this package.
	consensusSafetyJournalPrefix = "consensus:safety:v2:"
	// `consensusSafetyJournalLatestKey` defines the key used to access the related value.
	consensusSafetyJournalLatestKey = consensusSafetyJournalPrefix + "latest"
	// `consensusSafetyJournalRecordPrefix` defines the constant value used by this package.
	consensusSafetyJournalRecordPrefix = consensusSafetyJournalPrefix + "record:"
	// `consensusSafetyVersion` defines the constant value used by this package.
	consensusSafetyVersion = 1
	// `consensusSafetyJournalVersion` defines the constant value used by this package.
	consensusSafetyJournalVersion = 1
	// `consensusSafetyJournalKeepRecords` defines the constant value used by this package.
	consensusSafetyJournalKeepRecords = 16
	// `consensusSafetyJournalPruneEvery` defines the constant value used by this package.
	consensusSafetyJournalPruneEvery = consensusSafetyJournalKeepRecords
	// `finalizedHashDBPrefix` defines the constant value used by this package.
	finalizedHashDBPrefix = "finalized_hash:"
	// `consensusEvidenceDBPrefix` defines the constant value used by this package.
	consensusEvidenceDBPrefix = "consensus_evidence:"
	// `consensusTelemetryJSONL` defines the constant value used by this package.
	consensusTelemetryJSONL = "consensus_events.jsonl"
	// `consensusEvidenceVersion` defines the constant value used by this package.
	consensusEvidenceVersion = 1
	// `consensusTelemetryFileMode` defines the constant value used by this package.
	consensusTelemetryFileMode = 0600
)

// `consensusTelemetryMu` stores the synchronization state protecting shared data.
var consensusTelemetryMu sync.Mutex

type consensusSafetySnapshot struct {
	// `Version` stores the value associated with this record.
	Version int `json:"version"`
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id"`
	// `SavedAtUnix` stores the value associated with this record.
	SavedAtUnix int64 `json:"saved_at_unix"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason,omitempty"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `Round` stores the value associated with this record.
	Round uint32 `json:"round"`
	// `Phase` stores the value associated with this record.
	Phase ConsensusPhase
	// `RoundStart` stores the value associated with this record.
	RoundStart int64 `json:"round_start_unix,omitempty"`
	// `LastFinalized` stores the value associated with this record.
	LastFinalized uint64

	// `LockedBlock` stores the synchronization state protecting shared data.
	LockedBlock string `json:"locked_block,omitempty"`
	// `LockedBlockHash` stores the synchronization state protecting shared data.
	LockedBlockHash string `json:"locked_block_hash,omitempty"`
	// `LockedRound` stores the synchronization state protecting shared data.
	LockedRound uint32 `json:"locked_round,omitempty"`
	// `Committed` stores the value associated with this record.
	Committed bool `json:"committed"`
	// `LastProposedHeight` stores the value associated with this record.
	LastProposedHeight uint64 `json:"last_proposed_height,omitempty"`
	// `LastProposedRound` stores the value associated with this record.
	LastProposedRound uint32 `json:"last_proposed_round,omitempty"`

	// `Votes` stores the value associated with this record.
	Votes map[uint64]map[string]BlockVote `json:"votes,omitempty"`
	// `Proposals` stores the value associated with this record.
	Proposals map[uint64]Block `json:"proposals,omitempty"`
	// `ExecVotes` stores the value associated with this record.
	ExecVotes map[string]map[string]ExecutionResult `json:"exec_votes,omitempty"`

	// `AcceptedProposal` stores the value associated with this record.
	AcceptedProposal map[string]string `json:"accepted_proposal,omitempty"`
	// `QuorumLockedProposal` stores the value associated with this record.
	QuorumLockedProposal map[string]string `json:"quorum_locked_proposal,omitempty"`
	// `AcceptedProposalBlocks` stores the value associated with this record.
	AcceptedProposalBlocks map[string]Block `json:"accepted_proposal_blocks,omitempty"`
	// `LocalExecVoteByRound` stores the value associated with this record.
	LocalExecVoteByRound map[uint64]map[uint32]string `json:"local_exec_vote_by_round,omitempty"`

	// `CommitVotes` stores the value associated with this record.
	CommitVotes map[uint64]map[string][]string `json:"commit_votes,omitempty"`
	// `CommitVoted` stores the value associated with this record.
	CommitVoted map[uint64]map[string]string `json:"commit_voted,omitempty"`
	// `CommitVoteSignatures` stores the result produced by this operation.
	CommitVoteSignatures map[uint64]map[string]map[string]string `json:"commit_vote_signatures,omitempty"`
	// `CommittedHashes` stores the value associated with this record.
	CommittedHashes map[uint64]string `json:"committed_hashes,omitempty"`
	// `CommittedHeight` stores the value associated with this record.
	CommittedHeight uint64 `json:"committed_height,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `LastCommitHeight` stores the value associated with this record.
	LastCommitHeight uint64 `json:"last_commit_height,omitempty"`
	// `LastCommitAtUnix` stores the value associated with this record.
	LastCommitAtUnix int64 `json:"last_commit_at_unix,omitempty"`

	// `PostBlockSafeMode` stores the value associated with this record.
	PostBlockSafeMode map[uint64]consensusSafeModeWindowSnapshot `json:"post_block_safe_mode,omitempty"`
}

type consensusSafeModeWindowSnapshot struct {
	// `UntilUnixNano` stores the value associated with this record.
	UntilUnixNano int64 `json:"until_unix_nano"`
	// `WindowNanos` stores the value associated with this record.
	WindowNanos int64 `json:"window_nanos,omitempty"`
}

type consensusSafetyJournalRecord struct {
	// `Version` stores the value associated with this record.
	Version int `json:"version"`
	// `Seq` stores the value associated with this record.
	Seq uint64 `json:"seq"`
	// `SavedAtUnix` stores the value associated with this record.
	SavedAtUnix int64 `json:"saved_at_unix"`
	// `SHA256` stores the value associated with this record.
	SHA256 string `json:"sha256"`
	// `Payload` stores the value associated with this record.
	Payload []byte `json:"payload"`
}

type consensusEvidenceRecord struct {
	// `Version` stores the value associated with this record.
	Version int `json:"version"`
	// `Type` stores the value associated with this record.
	Type string `json:"type"`
	// `Key` stores the key used to access the related value.
	Key string `json:"key"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height,omitempty"`
	// `Round` stores the value associated with this record.
	Round uint32 `json:"round,omitempty"`
	// `Validator` stores whether the related condition is satisfied.
	Validator string `json:"validator,omitempty"`
	// `Expected` stores the value associated with this record.
	Expected string `json:"expected,omitempty"`
	// `Got` stores the value associated with this record.
	Got string `json:"got,omitempty"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash,omitempty"`
	// `PrevHash` stores the digest used to identify or verify the related data.
	PrevHash string `json:"prev_hash,omitempty"`
	// `SeenAtUnix` stores the value associated with this record.
	SeenAtUnix int64 `json:"seen_at_unix"`
}

type consensusTelemetryEvent struct {
	// `At` stores the value associated with this record.
	At string `json:"at"`
	// `UnixMillis` stores the value associated with this record.
	UnixMillis int64 `json:"unix_ms"`
	// `Node` stores the value associated with this record.
	Node string `json:"node,omitempty"`
	// `Type` stores the value associated with this record.
	Type string `json:"type"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason,omitempty"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height,omitempty"`
	// `Round` stores the value associated with this record.
	Round uint32 `json:"round,omitempty"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash,omitempty"`
	// `ConsensusMode` stores the value associated with this record.
	ConsensusMode string `json:"consensus_mode,omitempty"`
	// `QuorumPolicyVersion` stores the value associated with this record.
	QuorumPolicyVersion string `json:"quorum_policy_version,omitempty"`
	// `Required` stores the request data being processed.
	Required int `json:"required,omitempty"`
	// `ActiveReady` stores the value associated with this record.
	ActiveReady int `json:"active_ready,omitempty"`
	// `Fields` stores the value associated with this record.
	Fields map[string]interface{} `json:"fields,omitempty"`
}

// cloneStringMap clones string map.
func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]string, len(in))
	// `k` and `v` track the current values while iterating.
	for k, v := range in {
		out[k] = v
	}
	return out
}

// cloneBlockMap clones block map.
func cloneBlockMap(in map[uint64]Block) map[uint64]Block {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]Block, len(in))
	// `k` and `v` track the current values while iterating.
	for k, v := range in {
		out[k] = v
	}
	return out
}

// cloneStringBlockMap clones string block map.
func cloneStringBlockMap(in map[string]Block) map[string]Block {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]Block, len(in))
	// `k` and `v` track the current values while iterating.
	for k, v := range in {
		out[k] = v
	}
	return out
}

// cloneBlockVoteMap clones block vote map.
func cloneBlockVoteMap(in map[uint64]map[string]BlockVote) map[uint64]map[string]BlockVote {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]map[string]BlockVote, len(in))
	// `h` and `votes` track the current values while iterating.
	for h, votes := range in {
		if len(votes) == 0 {
			continue
		}
		// `dst` stores the value produced by this operation.
		dst := make(map[string]BlockVote, len(votes))
		// `signer` and `vote` track the current values while iterating.
		for signer, vote := range votes {
			dst[signer] = vote
		}
		out[h] = dst
	}
	return out
}

// cloneExecResultVoteMap clones exec result vote map.
func cloneExecResultVoteMap(in map[string]map[string]ExecutionResult) map[string]map[string]ExecutionResult {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]map[string]ExecutionResult, len(in))
	// `blockHash` and `votes` track the block data handled by this operation.
	for blockHash, votes := range in {
		if len(votes) == 0 {
			continue
		}
		// `dst` stores the value produced by this operation.
		dst := make(map[string]ExecutionResult, len(votes))
		// `signer` and `vote` track the current values while iterating.
		for signer, vote := range votes {
			dst[signer] = vote
		}
		out[blockHash] = dst
	}
	return out
}

// cloneLocalExecVoteByRound clones local exec vote by round.
func cloneLocalExecVoteByRound(in map[uint64]map[uint32]string) map[uint64]map[uint32]string {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]map[uint32]string, len(in))
	// `height` and `rounds` track the current values while iterating.
	for height, rounds := range in {
		if len(rounds) == 0 {
			continue
		}
		// `dst` stores the value produced by this operation.
		dst := make(map[uint32]string, len(rounds))
		// `round` and `key` track the key used to access the related value.
		for round, key := range rounds {
			dst[round] = key
		}
		out[height] = dst
	}
	return out
}

// proposalKeyEvidenceBlockHash implements the proposal key evidence block hash helper.
func proposalKeyEvidenceBlockHash(height uint64, proposalKey string, blocks map[string]Block, proposals map[uint64]Block) string {
	proposalKey = strings.TrimSpace(proposalKey)
	if height == 0 || proposalKey == "" {
		return ""
	}
	if blocks != nil {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := blocks[proposalKey]; ok && block.ID == height {
			return strings.TrimSpace(block.BlockHash)
		}
	}
	// `keyHeight`, `keyBlockHash`, and `ok` store whether the related condition is satisfied.
	keyHeight, _, keyBlockHash, _, _, ok := proposalVoteKeyParts(proposalKey)
	if ok && keyHeight == height && keyBlockHash != "" {
		return strings.TrimSpace(keyBlockHash)
	}
	if proposals != nil {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := proposals[height]; ok && block.ID == height {
			return strings.TrimSpace(block.BlockHash)
		}
	}
	return ""
}

// proposalKeyHasExecVoteEvidence implements the proposal key has exec vote evidence helper.
func proposalKeyHasExecVoteEvidence(height uint64, proposalKey string, blocks map[string]Block, proposals map[uint64]Block, execVotes map[string]map[string]ExecutionResult) bool {
	if height == 0 || strings.TrimSpace(proposalKey) == "" || len(execVotes) == 0 {
		return false
	}
	// `blockHash` stores the block data handled by this operation.
	blockHash := proposalKeyEvidenceBlockHash(height, proposalKey, blocks, proposals)
	if blockHash == "" {
		return false
	}
	// `votes` stores the value produced by this operation.
	votes := execVotes[blockHash]
	if len(votes) == 0 {
		return false
	}
	// `vote` tracks the current values while iterating.
	for _, vote := range votes {
		if vote.Height == 0 || vote.Height == height {
			return true
		}
	}
	return false
}

// safetyProposalKeyBacked implements the safety proposal key backed helper.
func safetyProposalKeyBacked(height uint64, proposalKey string, accepted map[string]string, quorum map[string]string, blocks map[string]Block, proposals map[uint64]Block, execVotes map[string]map[string]ExecutionResult) bool {
	proposalKey = strings.TrimSpace(proposalKey)
	if height == 0 || proposalKey == "" {
		return false
	}
	// `heightKey` stores the key used to access the related value.
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
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := blocks[proposalKey]; ok && block.ID == height {
			return proposalKeyHasExecVoteEvidence(height, proposalKey, blocks, proposals, execVotes)
		}
	}
	// `keyHeight`, `keyBlockHash`, and `ok` store whether the related condition is satisfied.
	keyHeight, _, keyBlockHash, _, _, ok := proposalVoteKeyParts(proposalKey)
	if !ok || keyHeight != height || keyBlockHash == "" {
		return false
	}
	if proposals != nil {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := proposals[height]; ok && block.ID == height &&
			strings.EqualFold(strings.TrimSpace(block.BlockHash), keyBlockHash) {
			return proposalKeyHasExecVoteEvidence(height, proposalKey, blocks, proposals, execVotes)
		}
	}
	return false
}

// pruneUnbackedLocalExecVoteByRound implements the prune unbacked local exec vote by round helper.
func pruneUnbackedLocalExecVoteByRound(in map[uint64]map[uint32]string, accepted map[string]string, quorum map[string]string, blocks map[string]Block, proposals map[uint64]Block, execVotes map[string]map[string]ExecutionResult, activeHeight uint64) map[uint64]map[uint32]string {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]map[uint32]string, len(in))
	// `height` and `rounds` track the current values while iterating.
	for height, rounds := range in {
		if len(rounds) == 0 {
			continue
		}
		if activeHeight > 0 && height != activeHeight {
			continue
		}
		// `dst` stores the value produced by this operation.
		dst := make(map[uint32]string, len(rounds))
		// `round` and `proposalKey` track the key used to access the related value.
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

// flattenCommitVotes implements the flatten commit votes helper.
func flattenCommitVotes(in map[uint64]map[string]map[string]struct{}) map[uint64]map[string][]string {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]map[string][]string, len(in))
	// `height` and `byHash` track the digest used to identify or verify the related data.
	for height, byHash := range in {
		if len(byHash) == 0 {
			continue
		}
		// `hashes` stores the digest used to identify or verify the related data.
		hashes := make(map[string][]string, len(byHash))
		// `hash` and `signers` track the digest used to identify or verify the related data.
		for hash, signers := range byHash {
			if len(signers) == 0 {
				continue
			}
			// `list` stores the value produced by this operation.
			list := make([]string, 0, len(signers))
			// `signer` tracks the current values while iterating.
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

// inflateCommitVotes implements the inflate commit votes helper.
func inflateCommitVotes(in map[uint64]map[string][]string) map[uint64]map[string]map[string]struct{} {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]map[string]map[string]struct{}, len(in))
	// `height` and `byHash` track the digest used to identify or verify the related data.
	for height, byHash := range in {
		if len(byHash) == 0 {
			continue
		}
		// `hashes` stores the digest used to identify or verify the related data.
		hashes := make(map[string]map[string]struct{}, len(byHash))
		// `hash` and `signers` track the digest used to identify or verify the related data.
		for hash, signers := range byHash {
			if len(signers) == 0 {
				continue
			}
			// `set` stores the value produced by this operation.
			set := make(map[string]struct{}, len(signers))
			// `signer` tracks the current values while iterating.
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

// cloneCommitVoted clones commit voted.
func cloneCommitVoted(in map[uint64]map[string]string) map[uint64]map[string]string {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]map[string]string, len(in))
	// `height` and `votes` track the current values while iterating.
	for height, votes := range in {
		if len(votes) == 0 {
			continue
		}
		// `dst` stores the value produced by this operation.
		dst := make(map[string]string, len(votes))
		// `signer` and `hash` track the digest used to identify or verify the related data.
		for signer, hash := range votes {
			dst[signer] = hash
		}
		out[height] = dst
	}
	return out
}

// cloneCommitVoteSignatures clones commit vote signatures.
func cloneCommitVoteSignatures(in map[uint64]map[string]map[string]string) map[uint64]map[string]map[string]string {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]map[string]map[string]string, len(in))
	// `height` and `byHash` track the digest used to identify or verify the related data.
	for height, byHash := range in {
		// `hashes` stores the digest used to identify or verify the related data.
		hashes := make(map[string]map[string]string, len(byHash))
		// `hash` and `bySigner` track the digest used to identify or verify the related data.
		for hash, bySigner := range byHash {
			// `signatures` stores the result produced by this operation.
			signatures := make(map[string]string, len(bySigner))
			// `signer` and `signature` track the current values while iterating.
			for signer, signature := range bySigner {
				signer = normalizeValidatorID(signer)
				signature = strings.TrimSpace(signature)
				if signer != "" && signature != "" {
					signatures[signer] = signature
				}
			}
			if len(signatures) > 0 {
				hashes[strings.TrimSpace(hash)] = signatures
			}
		}
		if len(hashes) > 0 {
			out[height] = hashes
		}
	}
	return out
}

// shouldPruneRestoredCommitVoteHeight implements the should prune restored commit vote height helper.
func shouldPruneRestoredCommitVoteHeight(height, chainHeight, activeHeight uint64) bool {
	if chainHeight == 0 {
		return height != activeHeight
	}
	return height != chainHeight && height != activeHeight
}

// cloneCommittedHashes clones committed hashes.
func cloneCommittedHashes(in map[uint64]string) map[uint64]string {
	if len(in) == 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]string, len(in))
	// `height` and `hash` track the digest used to identify or verify the related data.
	for height, hash := range in {
		out[height] = hash
	}
	return out
}

// clonePostBlockSafeModeWindows clones post block safe mode windows.
func clonePostBlockSafeModeWindows(untilByHeight map[uint64]time.Time, windowByHeight map[uint64]time.Duration, now time.Time) map[uint64]consensusSafeModeWindowSnapshot {
	if len(untilByHeight) == 0 {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	// `out` stores the result produced by this operation.
	out := make(map[uint64]consensusSafeModeWindowSnapshot, len(untilByHeight))
	// `height` and `until` track the current values while iterating.
	for height, until := range untilByHeight {
		if height == 0 || until.IsZero() || !until.After(now) {
			continue
		}
		// `window` stores the value produced by this operation.
		window := time.Duration(0)
		if windowByHeight != nil {
			window = windowByHeight[height]
		}
		out[height] = consensusSafeModeWindowSnapshot{
			UntilUnixNano: until.UnixNano(),
			WindowNanos:   int64(window),
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// restorePostBlockSafeModeWindows implements the restore post block safe mode windows helper.
func restorePostBlockSafeModeWindows(in map[uint64]consensusSafeModeWindowSnapshot, chainHeight uint64, now time.Time) (map[uint64]time.Time, map[uint64]time.Duration) {
	if len(in) == 0 || chainHeight == 0 {
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	// `untilByHeight` stores the value produced by this operation.
	untilByHeight := make(map[uint64]time.Time)
	// `windowByHeight` stores the value produced by this operation.
	windowByHeight := make(map[uint64]time.Duration)
	// `height` and `snap` track the current values while iterating.
	for height, snap := range in {
		if height == 0 || height <= chainHeight || height > chainHeight+1 || snap.UntilUnixNano <= 0 {
			continue
		}
		// `until` stores the value produced by this operation.
		until := time.Unix(0, snap.UntilUnixNano)
		if !until.After(now) {
			continue
		}
		untilByHeight[height] = until
		if snap.WindowNanos > 0 {
			windowByHeight[height] = time.Duration(snap.WindowNanos)
		}
	}
	if len(untilByHeight) == 0 {
		return nil, nil
	}
	return untilByHeight, windowByHeight
}

// snapshotConsensusSafetyState implements the snapshot consensus safety state helper.
func (n *Node) snapshotConsensusSafetyState(reason string) consensusSafetySnapshot {
	// `now` stores the value produced by this operation.
	now := time.Now()
	// `snap` stores the value produced by this operation.
	snap := consensusSafetySnapshot{
		Version:     consensusSafetyVersion,
		NodeID:      strings.TrimSpace(n.ID),
		SavedAtUnix: now.Unix(),
		Reason:      strings.TrimSpace(reason),
	}
	// Capture the consensus pointer once for the whole critical section. Recovery
	// and tests may replace n.Consensus while an asynchronous persistence request
	// is still draining; re-reading the field for Unlock could otherwise unlock a
	// different mutex from the one acquired above.
	if consensus := n.Consensus; consensus != nil {
		consensus.mu.Lock()
		snap.Height = consensus.Height
		snap.Round = consensus.Round
		snap.Phase = consensus.Phase
		if !consensus.RoundStart.IsZero() {
			snap.RoundStart = consensus.RoundStart.Unix()
		}
		snap.LastFinalized = consensus.LastFinalized
		snap.LockedBlock = consensus.LockedBlock
		snap.LockedBlockHash = consensus.LockedBlockHash
		snap.LockedRound = consensus.LockedRound
		snap.Committed = consensus.Committed
		snap.LastProposedHeight = consensus.LastProposedHeight
		snap.LastProposedRound = consensus.LastProposedRound
		snap.Votes = cloneBlockVoteMap(consensus.Votes)
		snap.Proposals = cloneBlockMap(consensus.Proposals)
		snap.ExecVotes = cloneExecResultVoteMap(consensus.ExecVotes)
		consensus.mu.Unlock()
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
	snap.CommitVoteSignatures = cloneCommitVoteSignatures(n.commitVoteSignatures)
	snap.CommittedHashes = cloneCommittedHashes(n.committed)
	snap.CommittedHeight = n.committedHeight
	snap.FinalizedHeight = n.finalizedHeight
	snap.LastCommitHeight = n.lastCommitHeight
	if !n.lastCommitAt.IsZero() {
		snap.LastCommitAtUnix = n.lastCommitAt.Unix()
	}
	n.commitMu.Unlock()
	n.validatorSetMu.RLock()
	snap.PostBlockSafeMode = clonePostBlockSafeModeWindows(n.safeModeUntilByHeight, n.safeModeWindowByHeight, now)
	n.validatorSetMu.RUnlock()
	return snap
}

// consensusSafetyJournalRecordKey implements the consensus safety journal record key helper.
func consensusSafetyJournalRecordKey(seq uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", consensusSafetyJournalRecordPrefix, seq))
}

// consensusSafetyJournalSeqFromKey implements the consensus safety journal seq from key helper.
func consensusSafetyJournalSeqFromKey(key []byte) (uint64, bool) {
	// `raw` stores the value produced by this operation.
	raw := strings.TrimPrefix(string(key), consensusSafetyJournalRecordPrefix)
	if raw == string(key) || raw == "" {
		return 0, false
	}
	// `seq` and `err` store the error produced by this operation.
	seq, err := strconv.ParseUint(raw, 10, 64)
	return seq, err == nil && seq > 0
}

// encodeConsensusSafetyJournalRecord implements the encode consensus safety journal record helper.
func encodeConsensusSafetyJournalRecord(seq uint64, payload []byte) ([]byte, error) {
	if seq == 0 {
		return nil, errors.New("consensus_safety_journal_seq_missing")
	}
	if len(payload) == 0 {
		return nil, errors.New("consensus_safety_journal_payload_missing")
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(payload)
	// `rec` stores the value produced by this operation.
	rec := consensusSafetyJournalRecord{
		Version:     consensusSafetyJournalVersion,
		Seq:         seq,
		SavedAtUnix: time.Now().Unix(),
		SHA256:      hex.EncodeToString(sum[:]),
		Payload:     append([]byte{}, payload...),
	}
	return json.Marshal(rec)
}

// decodeConsensusSafetyJournalRecord implements the decode consensus safety journal record helper.
func decodeConsensusSafetyJournalRecord(raw []byte) (consensusSafetySnapshot, uint64, error) {
	// `snap` stores the value used by this operation.
	var snap consensusSafetySnapshot
	// `plain` and `err` store the error produced by this operation.
	plain, err := decryptDBValue(raw)
	if err != nil {
		return snap, 0, err
	}
	// `rec` stores the value used by this operation.
	var rec consensusSafetyJournalRecord
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(plain, &rec); err != nil {
		return snap, 0, err
	}
	if rec.Version != consensusSafetyJournalVersion || rec.Seq == 0 || len(rec.Payload) == 0 {
		return snap, 0, errors.New("consensus_safety_journal_record_invalid")
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(rec.Payload)
	if !strings.EqualFold(strings.TrimSpace(rec.SHA256), hex.EncodeToString(sum[:])) {
		return snap, 0, errors.New("consensus_safety_journal_checksum_mismatch")
	}
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(rec.Payload, &snap); err != nil {
		return snap, 0, err
	}
	if snap.Version != consensusSafetyVersion {
		return snap, 0, errors.New("consensus_safety_snapshot_version_invalid")
	}
	return snap, rec.Seq, nil
}

// decodeConsensusSafetyJournalLatest implements the decode consensus safety journal latest helper.
func decodeConsensusSafetyJournalLatest(raw []byte) (uint64, error) {
	// `plain` and `err` store the error produced by this operation.
	plain, err := decryptDBValue(raw)
	if err != nil {
		return 0, err
	}
	// `seq` and `err` store the error produced by this operation.
	seq, err := strconv.ParseUint(strings.TrimSpace(string(plain)), 10, 64)
	if err != nil || seq == 0 {
		return 0, errors.New("consensus_safety_journal_latest_invalid")
	}
	return seq, nil
}

// latestConsensusSafetyJournalSeq implements the latest consensus safety journal seq helper.
func (n *Node) latestConsensusSafetyJournalSeq() uint64 {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return 0
	}
	// `seq` stores the value produced by this operation.
	seq := uint64(0)
	_ = n.DB.Meta.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get([]byte(consensusSafetyJournalLatestKey))
		if err == nil && item != nil {
			_ = item.Value(func(val []byte) error {
				// `decoded` and `derr` store the error produced by this operation.
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

// consensusSafetyJournalNextSeq implements the consensus safety journal next seq helper.
func consensusSafetyJournalNextSeq(txn *Txn) uint64 {
	// `seq` stores the value produced by this operation.
	seq := uint64(time.Now().UnixNano())
	if txn != nil {
		// `item` and `err` store the error produced by this operation.
		if item, err := txn.Get([]byte(consensusSafetyJournalLatestKey)); err == nil && item != nil {
			_ = item.Value(func(val []byte) error {
				// `prev` and `derr` store the error produced by this operation.
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

// consensusSafetyJournalPruneLocked implements the consensus safety journal prune locked helper.
func consensusSafetyJournalPruneLocked(txn *Txn) error {
	if txn == nil || consensusSafetyJournalKeepRecords <= 0 {
		return nil
	}
	// `prefix` stores the value produced by this operation.
	prefix := []byte(consensusSafetyJournalRecordPrefix)
	// `seqs` stores the value produced by this operation.
	seqs := make([]uint64, 0)
	// `it` stores the current position in the related collection.
	it := txn.NewIterator(IteratorOptions{Prefix: prefix})
	defer it.Close()
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		// `seq` and `ok` store whether the related condition is satisfied.
		if seq, ok := consensusSafetyJournalSeqFromKey(it.Item().Key()); ok {
			seqs = append(seqs, seq)
		}
	}
	if len(seqs) <= consensusSafetyJournalKeepRecords {
		return nil
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	// `seq` tracks the current values while iterating.
	for _, seq := range seqs[:len(seqs)-consensusSafetyJournalKeepRecords] {
		// `err` stores the error produced by this operation.
		if err := txn.Delete(consensusSafetyJournalRecordKey(seq)); err != nil {
			return err
		}
	}
	return nil
}

// loadConsensusSafetySnapshotFromJournal implements the load consensus safety snapshot from journal helper.
func (n *Node) loadConsensusSafetySnapshotFromJournal() (consensusSafetySnapshot, bool, error) {
	// `best` stores the value used by this operation.
	var best consensusSafetySnapshot
	// `bestSeq` stores the value produced by this operation.
	bestSeq := uint64(0)
	// `lastErr` stores the error produced by this operation.
	var lastErr error
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return best, false, nil
	}
	// `err` stores the error produced by this operation.
	err := n.DB.Meta.View(func(txn *Txn) error {
		// `prefix` stores the value produced by this operation.
		prefix := []byte(consensusSafetyJournalRecordPrefix)
		// `it` stores the current position in the related collection.
		it := txn.NewIterator(IteratorOptions{Prefix: prefix})
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			// `item` stores the current position in the related collection.
			item := it.Item()
			if item == nil {
				continue
			}
			// `keySeq` and `ok` store whether the related condition is satisfied.
			keySeq, ok := consensusSafetyJournalSeqFromKey(item.Key())
			if !ok {
				continue
			}
			_ = item.Value(func(val []byte) error {
				// `snap`, `recSeq`, and `derr` store the error produced by this operation.
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

// persistConsensusSafetyState implements the persist consensus safety state helper.
func (n *Node) persistConsensusSafetyState(reason string) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return nil
	}
	n.consensusSafetyPersistMu.Lock()
	defer n.consensusSafetyPersistMu.Unlock()

	// `snap` stores the value produced by this operation.
	snap := n.snapshotConsensusSafetyState(reason)
	// `data` and `err` store the error produced by this operation.
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	n.consensusSafetyWritesSincePrune++
	// `pruneJournal` stores the value produced by this operation.
	pruneJournal := consensusSafetyJournalPruneEvery <= 1 ||
		n.consensusSafetyWritesSincePrune >= consensusSafetyJournalPruneEvery
	// `err` stores the error produced by this operation.
	if err := n.DB.Meta.Update(func(txn *Txn) error {
		// `seq` stores the value produced by this operation.
		seq := consensusSafetyJournalNextSeq(txn)
		// `record` and `err` store the error produced by this operation.
		record, err := encodeConsensusSafetyJournalRecord(seq, data)
		if err != nil {
			return err
		}
		// `encRecord` and `err` store the error produced by this operation.
		encRecord, err := encryptDBValue(record)
		if err != nil {
			return err
		}
		// `err` stores the error produced by this operation.
		if err := txn.Set(consensusSafetyJournalRecordKey(seq), encRecord); err != nil {
			return err
		}
		// `latest` and `err` store the error produced by this operation.
		latest, err := encryptDBValue([]byte(fmt.Sprintf("%020d", seq)))
		if err != nil {
			return err
		}
		// `err` stores the error produced by this operation.
		if err := txn.Set([]byte(consensusSafetyJournalLatestKey), latest); err != nil {
			return err
		}
		if pruneJournal {
			// `err` stores the error produced by this operation.
			if err := consensusSafetyJournalPruneLocked(txn); err != nil {
				return err
			}
		}
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptDBValue(data)
		if err != nil {
			return err
		}
		return txn.Set([]byte(consensusSafetyDBKey), enc)
	}); err != nil {
		return err
	}
	if pruneJournal {
		n.consensusSafetyWritesSincePrune = 0
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

// persistConsensusSafetyStateAsync implements the persist consensus safety state async helper.
func (n *Node) persistConsensusSafetyStateAsync(reason string) {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return
	}
	n.consensusSafetyAsyncMu.Lock()
	n.consensusSafetyAsyncReason = strings.TrimSpace(reason)
	n.consensusSafetyAsyncPending = true
	if n.consensusSafetyAsyncRunning {
		n.consensusSafetyAsyncMu.Unlock()
		return
	}
	n.consensusSafetyAsyncRunning = true
	n.consensusSafetyAsyncMu.Unlock()

	go func() {
		for {
			n.consensusSafetyAsyncMu.Lock()
			if !n.consensusSafetyAsyncPending {
				n.consensusSafetyAsyncRunning = false
				n.consensusSafetyAsyncMu.Unlock()
				return
			}
			// `pendingReason` stores the value produced by this operation.
			pendingReason := n.consensusSafetyAsyncReason
			n.consensusSafetyAsyncPending = false
			n.consensusSafetyAsyncMu.Unlock()
			_ = n.persistConsensusSafetyState(pendingReason)
		}
	}()
}

// restoreConsensusSafetyState implements the restore consensus safety state helper.
func (n *Node) restoreConsensusSafetyState() error {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return nil
	}
	// `snap` stores the value used by this operation.
	var snap consensusSafetySnapshot
	// `snap`, `foundJournal`, and `journalErr` store the error produced by this operation.
	snap, foundJournal, journalErr := n.loadConsensusSafetySnapshotFromJournal()
	// `err` stores the error produced by this operation.
	err := journalErr
	if !foundJournal {
		// `legacyFound` stores whether the related condition is satisfied.
		legacyFound := false
		err = n.DB.Meta.View(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get([]byte(consensusSafetyDBKey))
			if err != nil {
				if errors.Is(err, ErrKeyNotFound) {
					return nil
				}
				return err
			}
			legacyFound = true
			return item.Value(func(val []byte) error {
				// `plain` and `err` store the error produced by this operation.
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
	// `chainHeight` stores the value produced by this operation.
	chainHeight := uint64(0)
	if n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}
	// `restoreActiveRound` stores the result produced by this operation.
	restoreActiveRound := snap.Height <= chainHeight
	if !restoreActiveRound &&
		chainHeight > 0 &&
		snap.Height == chainHeight+1 &&
		snap.CommittedHeight <= chainHeight &&
		!snap.Committed {
		restoreActiveRound = true
	}

	if consensus := n.Consensus; consensus != nil {
		consensus.mu.Lock()
		if restoreActiveRound && snap.Height > consensus.Height {
			consensus.Height = snap.Height
		}
		if chainHeight > consensus.Height {
			consensus.Height = chainHeight
		}
		if restoreActiveRound {
			consensus.Round = snap.Round
			consensus.Phase = snap.Phase
			if snap.RoundStart > 0 {
				consensus.RoundStart = time.Unix(snap.RoundStart, 0)
			}
			consensus.LastFinalized = snap.LastFinalized
			consensus.LockedBlock = strings.TrimSpace(snap.LockedBlock)
			consensus.LockedBlockHash = strings.TrimSpace(snap.LockedBlockHash)
			consensus.LockedRound = snap.LockedRound
			consensus.Committed = snap.Committed
			consensus.LastProposedHeight = snap.LastProposedHeight
			consensus.LastProposedRound = snap.LastProposedRound
			if snap.Votes != nil {
				consensus.Votes = cloneBlockVoteMap(snap.Votes)
			}
			if snap.Proposals != nil {
				consensus.Proposals = cloneBlockMap(snap.Proposals)
			}
			if snap.ExecVotes != nil {
				consensus.ExecVotes = cloneExecResultVoteMap(snap.ExecVotes)
			}
		} else {
			consensus.Round = 0
			consensus.Phase = PhasePropose
			consensus.RoundStart = time.Now()
			consensus.LockedBlock = ""
			consensus.LockedBlockHash = ""
			consensus.LockedRound = 0
			consensus.Committed = false
			if consensus.Votes == nil {
				consensus.Votes = make(map[uint64]map[string]BlockVote)
			}
			if consensus.Proposals == nil {
				consensus.Proposals = make(map[uint64]Block)
			}
			if consensus.ExecVotes == nil {
				consensus.ExecVotes = make(map[string]map[string]ExecutionResult)
			}
		}
		consensus.Syncing = false
		consensus.Paused = false
		consensus.mu.Unlock()
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

	// `activeCommitHeight` stores the value produced by this operation.
	activeCommitHeight := uint64(0)
	if restoreActiveRound {
		activeCommitHeight = snap.Height
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
		// `height` tracks the current values while iterating.
		for height := range n.commitVotes {
			if shouldPruneRestoredCommitVoteHeight(height, chainHeight, activeCommitHeight) {
				delete(n.commitVotes, height)
			}
		}
	}
	if snap.CommitVoted != nil {
		n.commitVoted = cloneCommitVoted(snap.CommitVoted)
		// `height` tracks the current values while iterating.
		for height := range n.commitVoted {
			if shouldPruneRestoredCommitVoteHeight(height, chainHeight, activeCommitHeight) {
				delete(n.commitVoted, height)
			}
		}
	}
	if snap.CommitVoteSignatures != nil {
		n.commitVoteSignatures = cloneCommitVoteSignatures(snap.CommitVoteSignatures)
		// `height` tracks the current values while iterating.
		for height := range n.commitVoteSignatures {
			if shouldPruneRestoredCommitVoteHeight(height, chainHeight, activeCommitHeight) {
				delete(n.commitVoteSignatures, height)
			}
		}
	}
	// Legacy journals could contain deterministic, unsigned commit counters.
	// Only persisted signatures are authoritative commit-vote evidence.
	n.commitVotes = make(map[uint64]map[string]map[string]struct{})
	n.commitVoted = make(map[uint64]map[string]string)
	// `height` and `byHash` track the digest used to identify or verify the related data.
	for height, byHash := range n.commitVoteSignatures {
		// `hash` and `bySigner` track the digest used to identify or verify the related data.
		for hash, bySigner := range byHash {
			if n.commitVotes[height] == nil {
				n.commitVotes[height] = make(map[string]map[string]struct{})
			}
			if n.commitVotes[height][hash] == nil {
				n.commitVotes[height][hash] = make(map[string]struct{})
			}
			if n.commitVoted[height] == nil {
				n.commitVoted[height] = make(map[string]string)
			}
			// `signer` and `signature` track the current values while iterating.
			for signer, signature := range bySigner {
				signer = normalizeValidatorID(signer)
				if signer == "" || strings.TrimSpace(signature) == "" {
					continue
				}
				n.commitVotes[height][hash][signer] = struct{}{}
				n.commitVoted[height][signer] = hash
			}
		}
	}
	if n.committed == nil {
		n.committed = make(map[uint64]string)
	}
	// `height` and `hash` track the digest used to identify or verify the related data.
	for height, hash := range snap.CommittedHashes {
		if strings.TrimSpace(hash) != "" && chainHeight > 0 && height <= chainHeight {
			n.committed[height] = hash
		}
	}
	// `height` tracks the current values while iterating.
	for height := range n.committed {
		if chainHeight == 0 || height > chainHeight {
			delete(n.committed, height)
		}
	}
	// `restoredCommittedHeight` stores the result produced by this operation.
	restoredCommittedHeight := snap.CommittedHeight
	if chainHeight == 0 {
		restoredCommittedHeight = 0
	} else if restoredCommittedHeight == 0 || restoredCommittedHeight > chainHeight {
		restoredCommittedHeight = chainHeight
	}
	if restoredCommittedHeight > n.committedHeight {
		n.committedHeight = restoredCommittedHeight
	}
	// `restoredFinalizedHeight` stores the result produced by this operation.
	restoredFinalizedHeight := snap.FinalizedHeight
	if chainHeight == 0 {
		restoredFinalizedHeight = 0
	} else if restoredFinalizedHeight == 0 || restoredFinalizedHeight > chainHeight {
		restoredFinalizedHeight = chainHeight
	}
	if restoredFinalizedHeight > n.finalizedHeight {
		n.finalizedHeight = restoredFinalizedHeight
	}
	// `restoredLastCommitHeight` stores the result produced by this operation.
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
	// `restoredCommitSignatures` stores the result produced by this operation.
	restoredCommitSignatures := cloneCommitVoteSignatures(n.commitVoteSignatures)
	n.commitMu.Unlock()
	ExecPool.mu.Lock()
	if ExecPool.commitChoice == nil {
		ExecPool.commitChoice = make(map[uint64]map[string]string)
	}
	// `height` and `byHash` track the digest used to identify or verify the related data.
	for height, byHash := range restoredCommitSignatures {
		if ExecPool.commitChoice[height] == nil {
			ExecPool.commitChoice[height] = make(map[string]string)
		}
		// `hash` and `bySigner` track the digest used to identify or verify the related data.
		for hash, bySigner := range byHash {
			// `scope` stores the value produced by this operation.
			scope := strings.TrimSpace(hash)
			if !strings.HasPrefix(scope, "block|") {
				scope = commitVoteScopeKey(height, hash)
			}
			// `signer` tracks the current values while iterating.
			for signer := range bySigner {
				ExecPool.commitChoice[height][normalizeValidatorID(signer)] = scope
			}
		}
	}
	ExecPool.mu.Unlock()

	// `restoredSafeModeUntil` and `restoredSafeModeWindow` store the result produced by this operation.
	restoredSafeModeUntil, restoredSafeModeWindow := restorePostBlockSafeModeWindows(snap.PostBlockSafeMode, chainHeight, time.Now())
	n.validatorSetMu.Lock()
	if n.safeModeUntilByHeight == nil {
		n.safeModeUntilByHeight = make(map[uint64]time.Time)
	}
	if n.safeModeWindowByHeight == nil {
		n.safeModeWindowByHeight = make(map[uint64]time.Duration)
	}
	// `height` tracks the current values while iterating.
	for height := range n.safeModeUntilByHeight {
		delete(n.safeModeUntilByHeight, height)
	}
	// `height` tracks the current values while iterating.
	for height := range n.safeModeWindowByHeight {
		delete(n.safeModeWindowByHeight, height)
	}
	// `height` and `until` track the current values while iterating.
	for height, until := range restoredSafeModeUntil {
		n.safeModeUntilByHeight[height] = until
	}
	// `height` and `window` track the current values while iterating.
	for height, window := range restoredSafeModeWindow {
		n.safeModeWindowByHeight[height] = window
	}
	n.validatorSetMu.Unlock()

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
			"safe_mode_windows":    len(restoredSafeModeUntil),
		},
	})
	return nil
}

// finalizedHashDBKey implements the finalized hash db key helper.
func finalizedHashDBKey(height uint64) []byte {
	return []byte(fmt.Sprintf("%s%020d", finalizedHashDBPrefix, height))
}

// loadFinalizedHashInvariant implements the load finalized hash invariant helper.
func (n *Node) loadFinalizedHashInvariant(height uint64) (string, bool, error) {
	if n == nil || n.DB == nil || n.DB.Meta == nil || height == 0 {
		return "", false, nil
	}
	// `out` stores the result produced by this operation.
	var out string
	// `found` stores whether the related condition is satisfied.
	found := false
	// `err` stores the error produced by this operation.
	err := n.DB.Meta.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get(finalizedHashDBKey(height))
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		found = true
		return item.Value(func(val []byte) error {
			// `plain` and `err` store the error produced by this operation.
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

// persistFinalizedHashInvariant implements the persist finalized hash invariant helper.
func (n *Node) persistFinalizedHashInvariant(block Block) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil || block.ID == 0 {
		return nil
	}
	// `hash` stores the digest used to identify or verify the related data.
	hash := strings.TrimSpace(block.BlockHash)
	if hash == "" {
		return nil
	}
	return n.DB.Meta.Update(func(txn *Txn) error {
		// `key` stores the key used to access the related value.
		key := finalizedHashDBKey(block.ID)
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get(key)
		if err != nil && !errors.Is(err, ErrKeyNotFound) {
			return err
		}
		if err == nil && item != nil {
			// `existing` stores the value used by this operation.
			var existing string
			// `vErr` stores the error produced by this operation.
			if vErr := item.Value(func(val []byte) error {
				// `plain` and `derr` store the error produced by this operation.
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
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptDBValue([]byte(hash))
		if err != nil {
			return err
		}
		return txn.Set(key, enc)
	})
}

type finalizedHashBackfillConflict struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `Round` stores the value associated with this record.
	Round uint32
	// `Expected` stores the value associated with this record.
	Expected string
	// `Got` stores the value associated with this record.
	Got string
}

// backfillFinalizedHashInvariants implements the backfill finalized hash invariants helper.
func (n *Node) backfillFinalizedHashInvariants(blocks []Block, reason string) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil || len(blocks) == 0 {
		return nil
	}
	// `writes` stores the value produced by this operation.
	writes := 0
	// `conflicts` stores the value produced by this operation.
	conflicts := make([]finalizedHashBackfillConflict, 0)
	// `err` stores the error produced by this operation.
	err := n.DB.Meta.Update(func(txn *Txn) error {
		// `block` tracks the synchronization state protecting shared data.
		for _, block := range blocks {
			if block.ID == 0 {
				continue
			}
			// `hash` stores the digest used to identify or verify the related data.
			hash := strings.TrimSpace(block.BlockHash)
			if hash == "" {
				continue
			}
			// `key` stores the key used to access the related value.
			key := finalizedHashDBKey(block.ID)
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get(key)
			if err != nil && !errors.Is(err, ErrKeyNotFound) {
				return err
			}
			if err == nil && item != nil {
				// `existing` stores the value produced by this operation.
				existing := ""
				// `vErr` stores the error produced by this operation.
				if vErr := item.Value(func(val []byte) error {
					// `plain` and `derr` store the error produced by this operation.
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
			// `enc` and `err` store the error produced by this operation.
			enc, err := encryptDBValue([]byte(hash))
			if err != nil {
				return err
			}
			// `err` stores the error produced by this operation.
			if err := txn.Set(key, enc); err != nil {
				return err
			}
			writes++
		}
		return nil
	})
	// `conflict` tracks the current values while iterating.
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

// persistSnapshotCommitSafety implements the persist snapshot commit safety helper.
func (n *Node) persistSnapshotCommitSafety(anchor Block, reason string) {
	if n == nil || anchor.ID == 0 || strings.TrimSpace(anchor.BlockHash) == "" {
		return
	}
	// `err` stores the error produced by this operation.
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
	// `err` stores the error produced by this operation.
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

// finalizedHashConflictEvidenceKey implements the finalized hash conflict evidence key helper.
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

// committedHashForEvidence implements the committed hash for evidence helper.
func (n *Node) committedHashForEvidence(height uint64) string {
	if n == nil || height == 0 {
		return ""
	}
	// `committedHash` and `ok` store whether the related condition is satisfied.
	if committedHash, ok := n.getCommittedHash(height); ok {
		return strings.TrimSpace(committedHash)
	}
	// `persistedHash`, `found`, and `err` store the error produced by this operation.
	persistedHash, found, err := n.loadFinalizedHashInvariant(height)
	if err == nil && found {
		return strings.TrimSpace(persistedHash)
	}
	return ""
}

// recordFinalizedHashConflictEvidence implements the record finalized hash conflict evidence helper.
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
	// `key` stores the key used to access the related value.
	key := finalizedHashConflictEvidenceKey(height, round, expectedHash, gotHash, reason)
	if key == "" {
		return
	}
	// `now` stores the value produced by this operation.
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

// pruneFinalizedHashInvariantsAboveHeight implements the prune finalized hash invariants above height helper.
func (n *Node) pruneFinalizedHashInvariantsAboveHeight(height uint64) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return nil
	}
	// `prefix` stores the value produced by this operation.
	prefix := []byte(finalizedHashDBPrefix)
	return n.DB.Meta.Update(func(txn *Txn) error {
		// `it` stores the current position in the related collection.
		it := txn.NewIterator(DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			// `key` stores the key used to access the related value.
			key := append([]byte(nil), it.Item().Key()...)
			// `rawHeight` stores the value produced by this operation.
			rawHeight := strings.TrimPrefix(string(key), finalizedHashDBPrefix)
			// `h` and `err` store the error produced by this operation.
			h, err := strconv.ParseUint(rawHeight, 10, 64)
			if err != nil || h <= height {
				continue
			}
			// `err` stores the error produced by this operation.
			if err := txn.Delete(key); err != nil {
				return err
			}
		}
		return nil
	})
}

// consensusEvidenceDBKey implements the consensus evidence db key helper.
func consensusEvidenceDBKey(ev consensusEvidenceRecord) []byte {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(strings.TrimSpace(ev.Type) + "|" + strings.TrimSpace(ev.Key)))
	return []byte(consensusEvidenceDBPrefix + hex.EncodeToString(sum[:]))
}

// persistConsensusEvidenceRecord implements the persist consensus evidence record helper.
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

// loadConsensusEvidenceSeenFromDB implements the load consensus evidence seen from db helper.
func (n *Node) loadConsensusEvidenceSeenFromDB() error {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil
	}
	// `records` stores the value produced by this operation.
	records := make([]consensusEvidenceRecord, 0)
	// `err` stores the error produced by this operation.
	err := n.DB.State.View(func(txn *Txn) error {
		// `opts` stores the value produced by this operation.
		opts := DefaultIteratorOptions
		opts.Prefix = []byte(consensusEvidenceDBPrefix)
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
			if err := item.Value(func(val []byte) error {
				// `plain` and `derr` store the error produced by this operation.
				plain, derr := decryptDBValue(val)
				if derr != nil {
					return derr
				}
				// `ev` stores the value used by this operation.
				var ev consensusEvidenceRecord
				// `uerr` stores the error produced by this operation.
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
	// `ev` tracks the current values while iterating.
	for _, ev := range records {
		// `seenAt` stores the value produced by this operation.
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

// emitConsensusTelemetry implements the emit consensus telemetry helper.
func (n *Node) emitConsensusTelemetry(ev consensusTelemetryEvent) {
	if n == nil || strings.TrimSpace(ev.Type) == "" {
		return
	}
	// `now` stores the value produced by this operation.
	now := time.Now().UTC()
	ev.At = now.Format(time.RFC3339Nano)
	ev.UnixMillis = now.UnixMilli()
	if ev.Node == "" {
		ev.Node = strings.TrimSpace(n.ID)
	}
	// `dir` stores the value produced by this operation.
	dir := nodeDataPath(n.DataDir, n.ID)
	if strings.TrimSpace(n.DataDir) == "" || strings.TrimSpace(n.ID) == "" {
		return
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	// `data` and `err` store the error produced by this operation.
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	// `path` stores the value produced by this operation.
	path := filepath.Join(dir, consensusTelemetryJSONL)
	consensusTelemetryMu.Lock()
	defer consensusTelemetryMu.Unlock()
	// `f` and `err` store the error produced by this operation.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, consensusTelemetryFileMode)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}
