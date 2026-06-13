package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func newValidatorRoundTestNode(t *testing.T, dataDir string, id string, validators []string, pub ed25519.PublicKey, priv ed25519.PrivateKey) *Node {
	t.Helper()

	node := newTestNodeForResultGossip(t, dataDir, validators)
	node.ID = id
	node.Role = "validator"
	node.ValidatorKey = ValidatorKey{
		ID:         id,
		PublicKey:  pub,
		PrivateKey: priv,
	}
	return node
}

func resetExecPoolForTest(t *testing.T) {
	t.Helper()

	ExecPool.mu.Lock()
	oldPool := ExecPool.pool
	oldMerkle := ExecPool.txMerkle
	oldFrozen := ExecPool.frozen
	oldSigners := ExecPool.signers
	oldChoice := ExecPool.choice
	oldEpochChoice := ExecPool.epochChoice
	oldCommitChoice := ExecPool.commitChoice
	ExecPool.pool = make(map[uint64]map[string]map[string]ExecutionResult)
	ExecPool.txMerkle = make(map[uint64]map[string]string)
	ExecPool.frozen = make(map[uint64]map[string]string)
	ExecPool.signers = make(map[uint64]map[string]map[string]bool)
	ExecPool.choice = make(map[uint64]map[string]map[string]string)
	ExecPool.epochChoice = make(map[uint64]map[string]string)
	ExecPool.commitChoice = make(map[uint64]map[string]string)
	ExecPool.mu.Unlock()

	t.Cleanup(func() {
		ExecPool.mu.Lock()
		ExecPool.pool = oldPool
		ExecPool.txMerkle = oldMerkle
		ExecPool.frozen = oldFrozen
		ExecPool.signers = oldSigners
		ExecPool.choice = oldChoice
		ExecPool.epochChoice = oldEpochChoice
		ExecPool.commitChoice = oldCommitChoice
		ExecPool.mu.Unlock()
	})
}

func TestLocalExecutionVoteGuardAllowsMonotonicHigherRoundProposalSwitch(t *testing.T) {
	node := &Node{
		ID:                   "A",
		localExecVoteByRound: make(map[uint64]map[uint32]string),
	}
	epoch := uint64(77)
	first := proposalVoteKey(epoch, 1, "block-a", "", "root-a")
	sameRoundConflict := proposalVoteKey(epoch, 1, "block-b", "", "root-b")
	higherRoundConflict := proposalVoteKey(epoch, 3, "block-c", "", "root-c")
	higherRoundSameBlock := proposalVoteKey(epoch, 4, "block-a", "", "root-a")

	if !node.allowLocalExecutionVoteRound(epoch, 1, first) {
		t.Fatalf("expected first local vote to be allowed")
	}
	if node.allowLocalExecutionVoteRound(epoch, 1, sameRoundConflict) {
		t.Fatalf("expected conflicting same-round local vote to be blocked")
	}
	if !node.allowLocalExecutionVoteRound(epoch, 3, higherRoundConflict) {
		t.Fatalf("expected conflicting higher-round proposal switch before quorum lock")
	}
	if !node.allowLocalExecutionVoteRound(epoch, 4, higherRoundSameBlock) {
		t.Fatalf("expected higher-round same-block rebroadcast to be allowed")
	}
	if node.allowLocalExecutionVoteRound(epoch, 2, first) {
		t.Fatalf("expected stale lower round vote to be blocked after advancing")
	}
}

func TestLocalExecutionVoteGuardAdvancesPastEvidenceFreeRoundMarker(t *testing.T) {
	node := &Node{
		ID:                   "A",
		localExecVoteByRound: make(map[uint64]map[uint32]string),
	}
	epoch := uint64(78)
	stale := proposalVoteKey(epoch, 0, "stale-block", "", "stale-root")
	fresh := proposalVoteKey(epoch, localExecVoteStaleRoundReleaseGap+1, "fresh-block", "", "fresh-root")

	if !node.allowLocalExecutionVoteRound(epoch, 0, stale) {
		t.Fatalf("expected stale seed vote to be allowed")
	}
	if !node.allowLocalExecutionVoteRound(epoch, localExecVoteStaleRoundReleaseGap+1, fresh) {
		t.Fatalf("non-quorum marker must not block a higher-round vote")
	}
	if got := node.localExecVoteByRound[epoch][localExecVoteStaleRoundReleaseGap+1]; got != fresh {
		t.Fatalf("higher-round marker not stored: got=%q", got)
	}
	if got := node.localExecVoteByRound[epoch][0]; got != stale {
		t.Fatalf("earlier round marker should remain for same-round equivocation protection: got=%q", got)
	}
}

func TestAcceptedProposalVoteCountIgnoresUncreditedSignerMarkers(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	block := Block{
		ID:          82,
		Round:       0,
		BlockHash:   "block-82",
		StateRoot:   "root-82",
		MempoolRoot: "tx-82",
	}
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	node.acceptedProposalBlocks = map[string]Block{proposalKey: block}
	node.execSignerSeen = map[uint64]map[string]map[string]bool{
		block.ID: {
			execPoolScopeKey(block.ID, proposalKey): {
				"A": true,
				"B": true,
				"C": true,
			},
		},
	}
	node.Consensus.ExecVotes = map[string]map[string]ExecutionResult{
		block.BlockHash: {
			"A": {Signer: "A", ResultHash: block.StateRoot},
			"B": {Signer: "B", ResultHash: block.StateRoot},
			"C": {Signer: "C", ResultHash: block.StateRoot},
		},
	}

	if got := node.acceptedProposalVoteCountLocked(block.ID, proposalKey); got != 0 {
		t.Fatalf("uncredited signer and consensus mirrors must not count as quorum evidence, got=%d", got)
	}
	count, ok, equivocation := recordExecResultGlobal(block.ID, proposalKey, block.StateRoot, block.MempoolRoot, ExecutionResult{
		Height:     block.ID,
		Round:      block.Round,
		BlockHash:  block.BlockHash,
		Signer:     "A",
		ResultHash: block.StateRoot,
		TxMerkle:   block.MempoolRoot,
	})
	if !ok || equivocation || count != 1 {
		t.Fatalf("expected credited vote count=1, count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if got := node.acceptedProposalVoteCountLocked(block.ID, proposalKey); got != 1 {
		t.Fatalf("credited payload vote should count, got=%d", got)
	}
}

func TestStaleExecutionVoteMirrorsCannotKeepProposalLocked(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	block := Block{
		ID:          83,
		Round:       2,
		BlockHash:   "block-83",
		StateRoot:   "root-83",
		MempoolRoot: "tx-83",
	}
	proposalKey := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	heightKey := acceptedProposalHeightKey(block.ID)
	node.acceptedProposal = map[string]string{heightKey: proposalKey}
	node.quorumLockedProposal = map[string]string{heightKey: proposalKey}
	node.acceptedProposalBlocks = map[string]Block{proposalKey: block}
	node.execSignerSeen = map[uint64]map[string]map[string]bool{
		block.ID: {
			execPoolScopeKey(block.ID, proposalKey): {
				"A": true,
				"B": true,
				"C": true,
			},
		},
	}
	node.Consensus.ExecVotes = map[string]map[string]ExecutionResult{
		block.BlockHash: {
			"A": {Signer: "A", ResultHash: block.StateRoot},
			"B": {Signer: "B", ResultHash: block.StateRoot},
			"C": {Signer: "C", ResultHash: block.StateRoot},
		},
	}

	if _, votes, keep, _ := node.acceptedProposalLockState(block.ID); keep || votes != 0 {
		t.Fatalf("stale accepted-proposal mirrors must not hold a lock: keep=%t votes=%d", keep, votes)
	}
	if _, votes, keep, _ := node.quorumLockedProposalLockState(block.ID); keep || votes != 0 {
		t.Fatalf("stale precommit mirrors must not hold a lock: keep=%t votes=%d", keep, votes)
	}
}

func TestLocalExecutionVoteGuardAdvancesPastMinorityRoundMarker(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.localExecVoteByRound = make(map[uint64]map[uint32]string)

	epoch := uint64(79)
	stale := proposalVoteKey(epoch, 0, "stale-block", "", "stale-root")
	fresh := proposalVoteKey(epoch, localExecVoteStaleRoundReleaseGap+1, "fresh-block", "", "fresh-root")
	node.localExecVoteByRound[epoch] = map[uint32]string{0: stale}
	node.execSignerSeen = map[uint64]map[string]map[string]bool{
		epoch: {
			execPoolScopeKey(epoch, stale): {
				"A": true,
				"B": true,
			},
		},
	}

	if !node.allowLocalExecutionVoteRound(epoch, localExecVoteStaleRoundReleaseGap+1, fresh) {
		t.Fatalf("minority-backed marker must not block a higher-round vote")
	}
	if got := node.localExecVoteByRound[epoch][0]; got != stale {
		t.Fatalf("minority marker should remain for audit history: got=%q", got)
	}
}

func TestLocalExecutionVoteGuardAdvancesWithoutUsingCommitStall(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.localExecVoteByRound = make(map[uint64]map[uint32]string)

	epoch := uint64(179)
	stale := proposalVoteKey(epoch, 0, "stale-block", "", "stale-root")
	fresh := proposalVoteKey(epoch, 2, "fresh-block", "", "fresh-root")
	node.localExecVoteByRound[epoch] = map[uint32]string{0: stale}
	for _, signer := range []string{"A", "B"} {
		count, ok, equivocation := recordExecResultGlobal(epoch, stale, "stale-root", "", ExecutionResult{
			Height:     epoch,
			Round:      0,
			BlockHash:  "stale-block",
			Signer:     signer,
			ResultHash: "stale-root",
			TxMerkle:   "",
		})
		if !ok || equivocation || count <= 0 {
			t.Fatalf("seed stale minority vote signer=%s count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}
	node.commitMu.Lock()
	node.lastCommitAt = time.Now().Add(-2 * execQuorumEmergencyStallTimeout)
	node.commitMu.Unlock()

	if !node.allowLocalExecutionVoteRound(epoch, 2, fresh) {
		t.Fatalf("higher-round vote should advance without relying on local commit age")
	}
	if got := node.localExecVoteByRound[epoch][2]; got != fresh {
		t.Fatalf("fresh higher-round marker missing: got=%q", got)
	}
	if got := node.localExecVoteByRound[epoch][0]; got != stale {
		t.Fatalf("existing marker should remain for audit history: got=%q", got)
	}
}

func TestLocalExecutionVoteGuardReleasesNonQuorumMarkerForHigherRoundQuorum(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "B"
	node.localExecVoteByRound = make(map[uint64]map[uint32]string)

	epoch := uint64(81)
	stale := proposalVoteKey(epoch, 1, "stale-block", "", "stale-root")
	fresh := proposalVoteKey(epoch, 2, "fresh-block", "", "fresh-root")
	node.localExecVoteByRound[epoch] = map[uint32]string{1: stale}
	for _, signer := range []string{"A", "C", "D"} {
		count, ok, equivocation := recordExecResultGlobal(epoch, fresh, "fresh-root", "", ExecutionResult{
			Height:     epoch,
			Round:      2,
			BlockHash:  "fresh-block",
			Signer:     signer,
			ResultHash: "fresh-root",
			TxMerkle:   "",
		})
		if !ok || equivocation || count <= 0 {
			t.Fatalf("seed fresh near-quorum vote signer=%s count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}

	if !node.allowLocalExecutionVoteRound(epoch, 2, fresh) {
		t.Fatalf("expected higher-round quorum to release non-quorum stale marker")
	}
	if got := node.localExecVoteByRound[epoch][2]; got != fresh {
		t.Fatalf("fresh marker not stored after near-quorum release: got=%q", got)
	}
	if got := node.localExecVoteByRound[epoch][1]; got != stale {
		t.Fatalf("earlier marker should remain for same-round equivocation protection: got=%q", got)
	}
}

func TestLocalExecutionVoteGuardRetainsSameScopeRoundMarkers(t *testing.T) {
	node := &Node{
		ID:                   "B",
		localExecVoteByRound: make(map[uint64]map[uint32]string),
	}
	epoch := uint64(183)
	round0 := proposalVoteKey(epoch, 0, "same-block", "", "same-root")
	round3 := proposalVoteKey(epoch, 3, "same-block", "", "same-root")

	if !node.allowLocalExecutionVoteRound(epoch, 0, round0) {
		t.Fatalf("expected first same-scope vote marker to be allowed")
	}
	if !node.allowLocalExecutionVoteRound(epoch, 3, round3) {
		t.Fatalf("expected higher-round same-scope marker to be allowed")
	}
	if got := node.localExecVoteByRound[epoch][0]; got != round0 {
		t.Fatalf("older same-scope marker should remain for same-round equivocation protection: got=%q", got)
	}
	if got := node.localExecVoteByRound[epoch][3]; got != round3 {
		t.Fatalf("higher-round same-scope marker not stored: got=%q", got)
	}
}

func TestLocalExecutionVoteGuardNeverReleasesToLowerRound(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"B", "C", "D"})
	node.ID = "B"
	node.localExecVoteByRound = make(map[uint64]map[uint32]string)

	epoch := uint64(181)
	existing := proposalVoteKey(epoch, 6, "existing-block", "", "existing-root")
	incoming := proposalVoteKey(epoch, 5, "incoming-block", "", "incoming-root")
	node.localExecVoteByRound[epoch] = map[uint32]string{6: existing}
	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = []Block{{ID: 0, BlockHash: "genesis"}, {ID: epoch - 1, BlockHash: "tip"}}
	node.Blockchain.mu.Unlock()
	node.commitMu.Lock()
	node.lastCommitAt = time.Now().Add(-2 * execQuorumEmergencyStallTimeout)
	node.commitMu.Unlock()

	if count, ok, equivocation := recordExecResultGlobal(epoch, existing, "existing-root", "", ExecutionResult{
		Height:     epoch,
		Round:      6,
		BlockHash:  "existing-block",
		Signer:     "B",
		ResultHash: "existing-root",
		TxMerkle:   "",
	}); !ok || equivocation || count != 1 {
		t.Fatalf("seed existing vote count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	for _, signer := range []string{"C", "D"} {
		count, ok, equivocation := recordExecResultGlobal(epoch, incoming, "incoming-root", "", ExecutionResult{
			Height:     epoch,
			Round:      5,
			BlockHash:  "incoming-block",
			Signer:     signer,
			ResultHash: "incoming-root",
			TxMerkle:   "",
		})
		if !ok || equivocation || count <= 0 {
			t.Fatalf("seed incoming near-quorum vote signer=%s count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}
	if node.allowLocalExecutionVoteRound(epoch, 5, incoming) {
		t.Fatalf("lower-round proposal must remain blocked regardless of local stall or evidence")
	}
	if got := node.localExecVoteByRound[epoch][5]; got != "" {
		t.Fatalf("lower-round marker must not be stored: got=%q", got)
	}
	if got := node.localExecVoteByRound[epoch][6]; got != existing {
		t.Fatalf("existing higher-round marker should remain: got=%q", got)
	}
}

func TestLocalExecutionVoteGuardKeepsHigherRoundOnLowerRoundEvidenceTieAfterStall(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "D"
	node.localExecVoteByRound = make(map[uint64]map[uint32]string)

	epoch := uint64(182)
	existing := proposalVoteKey(epoch, 5, "existing-block", "", "existing-root")
	incoming := proposalVoteKey(epoch, 0, "incoming-block", "", "incoming-root")
	node.localExecVoteByRound[epoch] = map[uint32]string{5: existing}
	node.Blockchain.mu.Lock()
	node.Blockchain.Blocks = []Block{{ID: 0, BlockHash: "genesis"}, {ID: epoch - 1, BlockHash: "tip"}}
	node.Blockchain.mu.Unlock()
	node.commitMu.Lock()
	node.lastCommitAt = time.Now().Add(-2 * execQuorumEmergencyStallTimeout)
	node.commitMu.Unlock()

	for _, signer := range []string{"C", "D"} {
		count, ok, equivocation := recordExecResultGlobal(epoch, existing, "existing-root", "", ExecutionResult{
			Height:     epoch,
			Round:      5,
			BlockHash:  "existing-block",
			Signer:     signer,
			ResultHash: "existing-root",
			TxMerkle:   "",
		})
		if !ok || equivocation || count <= 0 {
			t.Fatalf("seed existing near-quorum vote signer=%s count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}
	for _, signer := range []string{"A", "B"} {
		count, ok, equivocation := recordExecResultGlobal(epoch, incoming, "incoming-root", "", ExecutionResult{
			Height:     epoch,
			Round:      0,
			BlockHash:  "incoming-block",
			Signer:     signer,
			ResultHash: "incoming-root",
			TxMerkle:   "",
		})
		if !ok || equivocation || count <= 0 {
			t.Fatalf("seed incoming near-quorum vote signer=%s count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}

	if node.allowLocalExecutionVoteRound(epoch, 0, incoming) {
		t.Fatalf("expected lower-round evidence tie to remain blocked after stall")
	}
	if got := node.localExecVoteByRound[epoch][5]; got != existing {
		t.Fatalf("higher-round marker should remain on evidence tie, got=%q", got)
	}
	if got := node.localExecVoteByRound[epoch][0]; got != "" {
		t.Fatalf("lower-round marker should not be stored on evidence tie, got=%q", got)
	}
}

func TestLocalExecutionVoteGuardMovesPastStaleExecutionQuorum(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.localExecVoteByRound = make(map[uint64]map[uint32]string)

	epoch := uint64(80)
	stale := proposalVoteKey(epoch, 0, "stale-block", "", "stale-root")
	fresh := proposalVoteKey(epoch, localExecVoteStaleRoundReleaseGap+1, "fresh-block", "", "fresh-root")
	node.localExecVoteByRound[epoch] = map[uint32]string{0: stale}
	for _, signer := range []string{"A", "B", "C"} {
		count, ok, equivocation := recordExecResultGlobal(epoch, stale, "stale-root", "", ExecutionResult{
			Height:     epoch,
			Round:      0,
			BlockHash:  "stale-block",
			Signer:     signer,
			ResultHash: "stale-root",
			TxMerkle:   "",
		})
		if !ok || equivocation || count <= 0 {
			t.Fatalf("seed stale quorum vote signer=%s count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}

	if !node.allowLocalExecutionVoteRound(epoch, localExecVoteStaleRoundReleaseGap+1, fresh) {
		t.Fatalf("execution prevote quorum must not block a conflicting higher-round vote")
	}
	if got := node.localExecVoteByRound[epoch][0]; got != stale {
		t.Fatalf("quorum-backed earlier-round marker should remain for audit history, got=%q", got)
	}
	if got := node.localExecVoteByRound[epoch][localExecVoteStaleRoundReleaseGap+1]; got != fresh {
		t.Fatalf("fresh higher-round marker should be stored, got=%q", got)
	}
}

func TestLocalExecutionVoteGuardMovesPastExecutionQuorumAfterCommitStall(t *testing.T) {
	resetExecPoolForTest(t)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.ID = "A"
	node.localExecVoteByRound = make(map[uint64]map[uint32]string)

	epoch := uint64(1)
	stale := proposalVoteKey(epoch, 0, "stale-block", "", "stale-root")
	fresh := proposalVoteKey(epoch, localExecVoteStaleRoundReleaseGap+1, "fresh-block", "", "fresh-root")
	node.localExecVoteByRound[epoch] = map[uint32]string{0: stale}
	for _, signer := range []string{"A", "B", "C"} {
		count, ok, equivocation := recordExecResultGlobal(epoch, stale, "stale-root", "", ExecutionResult{
			Height:     epoch,
			Round:      0,
			BlockHash:  "stale-block",
			Signer:     signer,
			ResultHash: "stale-root",
			TxMerkle:   "",
		})
		if !ok || equivocation || count <= 0 {
			t.Fatalf("seed stale quorum vote signer=%s count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}

	node.commitMu.Lock()
	node.lastCommitAt = time.Now().Add(-2 * execQuorumEmergencyStallTimeout)
	node.commitMu.Unlock()

	if !node.allowLocalExecutionVoteRound(epoch, localExecVoteStaleRoundReleaseGap+1, fresh) {
		t.Fatalf("local commit stall must not turn execution prevotes into a permanent lock")
	}
	if got := node.localExecVoteByRound[epoch][localExecVoteStaleRoundReleaseGap+1]; got != fresh {
		t.Fatalf("expected fresh marker after stalled execution quorum: got=%q", got)
	}
	if got := node.localExecVoteByRound[epoch][0]; got != stale {
		t.Fatalf("quorum marker should remain after local commit stall: got=%q", got)
	}
}

func TestLocalExecutionVoteCanMovePastSingleSignedCommitChoice(t *testing.T) {
	resetExecPoolForTest(t)
	validators := []string{"A", "B", "C", "D"}
	privKeys := installCommitVoteKeysForTest(t, validators)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.localExecVoteByRound = make(map[uint64]map[uint32]string)

	block := Block{ID: 1, Round: 0, BlockHash: "committed-block", StateRoot: "committed-root"}
	committed := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	fresh := proposalVoteKey(block.ID, 3, "fresh-block", "", "fresh-root")
	node.localExecVoteByRound[block.ID] = map[uint32]string{block.Round: committed}
	recordSignedCommitVotesForTest(t, node, block, []string{"A"}, privKeys)

	if !node.allowLocalExecutionVoteRound(block.ID, 3, fresh) {
		t.Fatalf("single signed commit choice must not block higher-round execution convergence")
	}
	if got := node.localExecVoteByRound[block.ID][3]; got != fresh {
		t.Fatalf("expected higher-round marker after non-quorum commit choice: got=%q", got)
	}
}

func TestLocalExecutionVoteGuardKeepsSignedCommitQuorumChoice(t *testing.T) {
	resetExecPoolForTest(t)
	validators := []string{"A", "B", "C", "D"}
	privKeys := installCommitVoteKeysForTest(t, validators)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"
	node.localExecVoteByRound = make(map[uint64]map[uint32]string)

	block := Block{ID: 1, Round: 0, BlockHash: "committed-block", StateRoot: "committed-root"}
	committed := proposalVoteKey(block.ID, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	fresh := proposalVoteKey(block.ID, 3, "fresh-block", "", "fresh-root")
	node.localExecVoteByRound[block.ID] = map[uint32]string{block.Round: committed}
	recordSignedCommitVotesForTest(t, node, block, []string{"A", "B", "C"}, privKeys)

	if node.allowLocalExecutionVoteRound(block.ID, 3, fresh) {
		t.Fatalf("signed commit quorum must block a conflicting higher-round execution vote")
	}
	if got := node.localExecVoteByRound[block.ID][3]; got != "" {
		t.Fatalf("unexpected conflicting marker after signed commit quorum: got=%q", got)
	}
}

func TestSignedCommitVoteCanMoveToHigherRoundBeforeQuorum(t *testing.T) {
	resetExecPoolForTest(t)
	validators := []string{"A", "B", "C", "D"}
	privKeys := installCommitVoteKeysForTest(t, validators)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"

	lowBlock := Block{ID: 1, Round: 2, BlockHash: "low-round-block", StateRoot: "low-root"}
	highBlock := Block{ID: 1, Round: 7, BlockHash: "high-round-block", StateRoot: "high-root"}
	node.noteObservedProposal(lowBlock)
	node.noteObservedProposal(highBlock)

	if _, _, ok := node.recordVerifiedCommitVote(signedCommitMsgForTest(t, lowBlock, "A", privKeys["A"])); !ok {
		t.Fatalf("expected first signed commit vote to record")
	}
	if _, _, ok := node.recordVerifiedCommitVote(signedCommitMsgForTest(t, highBlock, "A", privKeys["A"])); !ok {
		t.Fatalf("expected higher-round signed commit vote to replace non-quorum prior vote")
	}
	if got := node.localSignedCommitChoice(lowBlock.ID); got != highBlock.BlockHash {
		t.Fatalf("expected local commit choice to move to higher round, got=%q want=%q", got, highBlock.BlockHash)
	}
	_, _, lowCount, _ := node.commitVoteEvidence(lowBlock.ID, lowBlock.BlockHash)
	if lowCount != 0 {
		t.Fatalf("expected low-round commit evidence to be removed after replacement, got=%d", lowCount)
	}
	_, _, highCount, required := node.commitVoteEvidence(highBlock.ID, highBlock.BlockHash)
	if highCount != 1 || required != 3 {
		t.Fatalf("unexpected high-round commit evidence: count=%d required=%d", highCount, required)
	}

	ExecPool.mu.Lock()
	choice := strings.TrimSpace(ExecPool.commitChoice[highBlock.ID]["A"])
	ExecPool.mu.Unlock()
	if choice != commitVoteScopeKey(highBlock.ID, highBlock.BlockHash) {
		t.Fatalf("expected global commit choice to follow replacement, got=%q", choice)
	}
}

func TestSignedCommitVoteCannotReplaceQuorumChoice(t *testing.T) {
	resetExecPoolForTest(t)
	validators := []string{"A", "B", "C", "D"}
	privKeys := installCommitVoteKeysForTest(t, validators)
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	node.ID = "A"

	lowBlock := Block{ID: 1, Round: 2, BlockHash: "low-round-block", StateRoot: "low-root"}
	highBlock := Block{ID: 1, Round: 7, BlockHash: "high-round-block", StateRoot: "high-root"}
	node.noteObservedProposal(lowBlock)
	node.noteObservedProposal(highBlock)
	recordSignedCommitVotesForTest(t, node, lowBlock, []string{"A", "B", "C"}, privKeys)

	if _, _, ok := node.recordVerifiedCommitVote(signedCommitMsgForTest(t, highBlock, "A", privKeys["A"])); ok {
		t.Fatalf("signed commit quorum must reject conflicting higher-round replacement")
	}
	if got := node.localSignedCommitChoice(lowBlock.ID); got != lowBlock.BlockHash {
		t.Fatalf("expected local commit choice to remain on quorum block, got=%q want=%q", got, lowBlock.BlockHash)
	}
}

func TestExecutionVoteWithHashMustMatchRound(t *testing.T) {
	snap := execProposalSnapshot{
		Epoch:     10,
		Round:     4,
		BlockHash: "same-block-hash",
		TxMerkle:  "",
		StateRoot: "root",
	}
	if voteBelongsToCurrentProposal(ExecutionResultMsg{
		HeightHint:    10,
		RoundHint:     0,
		BlockHashHint: "same-block-hash",
		ExecHash:      "root",
	}, snap) {
		t.Fatalf("round-0 vote must not attach to a higher-round proposal with the same block hash")
	}
	if !voteBelongsToCurrentProposal(ExecutionResultMsg{
		HeightHint:    10,
		RoundHint:     4,
		BlockHashHint: "same-block-hash",
		ExecHash:      "root",
	}, snap) {
		t.Fatalf("matching round and block hash should attach to the proposal")
	}
}

func TestRecordExecResultGlobalRejectsSignerSameRoundEquivocationAcrossProposals(t *testing.T) {
	resetExecPoolForTest(t)

	epoch := uint64(88)
	first := proposalVoteKey(epoch, 1, "block-a", "", "root-a")
	conflict := proposalVoteKey(epoch, 1, "block-b", "", "root-b")
	if count, ok, equivocation := recordExecResultGlobal(epoch, first, "root-a", "", ExecutionResult{
		Height:     epoch,
		Round:      1,
		BlockHash:  "block-a",
		Signer:     "A",
		ResultHash: "root-a",
	}); !ok || equivocation || count != 1 {
		t.Fatalf("expected first vote count=1 ok=true equivocation=false, got count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if count, ok, equivocation := recordExecResultGlobal(epoch, conflict, "root-b", "", ExecutionResult{
		Height:     epoch,
		BlockHash:  "block-b",
		Signer:     "A",
		ResultHash: "root-b",
	}); ok || !equivocation || count != 0 {
		t.Fatalf("expected conflicting vote to be rejected as equivocation, got count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if got := getExecCountGlobal(epoch, conflict, "root-b", ""); got != 0 {
		t.Fatalf("conflicting proposal should not gain a vote, got %d", got)
	}
}

func TestRecordExecResultGlobalMovesSignerToHigherRoundBeforeQuorum(t *testing.T) {
	resetExecPoolForTest(t)

	epoch := uint64(89)
	first := proposalVoteKey(epoch, 1, "block-a", "", "root-a")
	higher := proposalVoteKey(epoch, 4, "block-b", "", "root-b")
	if count, ok, equivocation := recordExecResultGlobal(epoch, first, "root-a", "", ExecutionResult{
		Height:     epoch,
		Round:      1,
		BlockHash:  "block-a",
		Signer:     "A",
		ResultHash: "root-a",
	}); !ok || equivocation || count != 1 {
		t.Fatalf("expected first vote count=1 ok=true equivocation=false, got count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if count, ok, equivocation := recordExecResultGlobalWithRequired(epoch, higher, "root-b", "", ExecutionResult{
		Height:     epoch,
		Round:      4,
		BlockHash:  "block-b",
		Signer:     "A",
		ResultHash: "root-b",
	}, 3); !ok || equivocation || count != 1 {
		t.Fatalf("expected signer choice to move to higher round, got count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if got := getExecCountGlobal(epoch, first, "root-a", ""); got != 0 {
		t.Fatalf("lower-round proposal should lose moved signer vote, got %d", got)
	}
	if got := getExecCountGlobal(epoch, higher, "root-b", ""); got != 1 {
		t.Fatalf("higher-round proposal should gain moved signer vote, got %d", got)
	}
}

func TestRecordExecResultGlobalConvergesAfterSplitLowerRoundVotes(t *testing.T) {
	resetExecPoolForTest(t)

	const epoch uint64 = 90
	const higherRound uint32 = 10
	higher := proposalVoteKey(epoch, higherRound, "block-final", "", "root-final")
	for i, signer := range []string{"A", "B", "C"} {
		round := uint32(i + 1)
		lowerBlock := "block-" + signer
		lowerRoot := "root-" + signer
		lower := proposalVoteKey(epoch, round, lowerBlock, "", lowerRoot)
		if count, ok, equivocation := recordExecResultGlobalWithRequired(epoch, lower, lowerRoot, "", ExecutionResult{
			Height:     epoch,
			Round:      round,
			BlockHash:  lowerBlock,
			Signer:     signer,
			ResultHash: lowerRoot,
		}, 3); !ok || equivocation || count != 1 {
			t.Fatalf("seed split lower-round vote signer=%s count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}

	for i, signer := range []string{"A", "B", "C"} {
		count, ok, equivocation := recordExecResultGlobalWithRequired(epoch, higher, "root-final", "", ExecutionResult{
			Height:     epoch,
			Round:      higherRound,
			BlockHash:  "block-final",
			Signer:     signer,
			ResultHash: "root-final",
		}, 3)
		if !ok || equivocation || count != i+1 {
			t.Fatalf("move split vote signer=%s count=%d want=%d ok=%t equivocation=%t", signer, count, i+1, ok, equivocation)
		}
	}
	if got := getExecCountGlobal(epoch, higher, "root-final", ""); got != 3 {
		t.Fatalf("higher-round proposal did not converge to quorum: got=%d want=3", got)
	}
}

func TestRecordExecResultGlobalKeepsQuorumChoiceLockedAcrossRounds(t *testing.T) {
	resetExecPoolForTest(t)

	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": make(ed25519.PublicKey, ed25519.PublicKeySize),
		"B": make(ed25519.PublicKey, ed25519.PublicKeySize),
		"C": make(ed25519.PublicKey, ed25519.PublicKeySize),
		"D": make(ed25519.PublicKey, ed25519.PublicKeySize),
	}
	t.Cleanup(func() { GenesisValidatorPubKeys = oldGenesisValidatorPubKeys })

	epoch := uint64(89)
	first := proposalVoteKey(epoch, 1, "block-a", "", "root-a")
	higher := proposalVoteKey(epoch, 4, "block-b", "", "root-b")
	for _, signer := range []string{"A", "B", "C"} {
		if count, ok, equivocation := recordExecResultGlobal(epoch, first, "root-a", "", ExecutionResult{
			Height:     epoch,
			Round:      1,
			BlockHash:  "block-a",
			Signer:     signer,
			ResultHash: "root-a",
		}); !ok || equivocation || count <= 0 {
			t.Fatalf("expected quorum seed vote for %s, got count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}
	if count, ok, equivocation := recordExecResultGlobal(epoch, higher, "root-b", "", ExecutionResult{
		Height:     epoch,
		Round:      4,
		BlockHash:  "block-b",
		Signer:     "A",
		ResultHash: "root-b",
	}); ok || !equivocation || count != 0 {
		t.Fatalf("expected quorum lower-round choice to reject conflict as equivocation, got count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if got := getExecCountGlobal(epoch, first, "root-a", ""); got != 3 {
		t.Fatalf("quorum proposal should retain votes, got %d", got)
	}
	if got := getExecCountGlobal(epoch, higher, "root-b", ""); got != 0 {
		t.Fatalf("higher-round conflict should not gain vote, got %d", got)
	}
}

func TestRecordExecResultGlobalConvergesMovedVotesOnHigherRoundQuorum(t *testing.T) {
	resetExecPoolForTest(t)

	epoch := uint64(89)
	first := proposalVoteKey(epoch, 1, "block-a", "", "root-a")
	higher := proposalVoteKey(epoch, 4, "block-b", "", "root-b")
	if count, ok, equivocation := recordExecResultGlobalWithRequired(epoch, first, "root-a", "", ExecutionResult{
		Height:     epoch,
		Round:      1,
		BlockHash:  "block-a",
		Signer:     "A",
		ResultHash: "root-a",
	}, 4); !ok || equivocation || count != 1 {
		t.Fatalf("expected lower-round seed vote, got count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	for _, signer := range []string{"B", "C", "D"} {
		if count, ok, equivocation := recordExecResultGlobalWithRequired(epoch, higher, "root-b", "", ExecutionResult{
			Height:     epoch,
			Round:      4,
			BlockHash:  "block-b",
			Signer:     signer,
			ResultHash: "root-b",
		}, 4); !ok || equivocation || count <= 0 {
			t.Fatalf("expected higher-round quorum seed vote for %s, got count=%d ok=%t equivocation=%t", signer, count, ok, equivocation)
		}
	}
	if count, ok, equivocation := recordExecResultGlobalWithRequired(epoch, higher, "root-b", "", ExecutionResult{
		Height:     epoch,
		Round:      4,
		BlockHash:  "block-b",
		Signer:     "A",
		ResultHash: "root-b",
	}, 4); !ok || equivocation || count != 4 {
		t.Fatalf("expected projected higher-round quorum to release signer, got count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if got := getExecCountGlobal(epoch, first, "root-a", ""); got != 0 {
		t.Fatalf("released lower-round proposal should lose switched vote, got %d", got)
	}
	if got := getExecCountGlobal(epoch, higher, "root-b", ""); got != 4 {
		t.Fatalf("higher-round proposal should reach quorum, got %d", got)
	}
}

func TestQueuedExecutionVoteDropsAreThrottled(t *testing.T) {
	for _, reason := range []string{
		"queued_syncing",
		"queued_sync_incomplete",
		"queued_recompute_pause",
		"queued_missing_validator_set",
		"queued_proposal_unresolved",
	} {
		if !shouldThrottleExecutionVoteDrop(reason) {
			t.Fatalf("expected queued execution vote drop %q to be throttled", reason)
		}
	}
	if shouldThrottleExecutionVoteDrop("invalid_signature") {
		t.Fatal("security-significant invalid signature drop must remain immediately visible")
	}
}

func TestQueuedExecutionVoteDropThrottleStaysCoarseInDebugMode(t *testing.T) {
	oldDebugSync := DebugSync
	DebugSync = true
	t.Cleanup(func() {
		DebugSync = oldDebugSync
	})

	node := &Node{}
	first := ExecutionResultMsg{HeightHint: 100, RoundHint: 1, Signer: "A", ExecHash: "root-a"}
	second := ExecutionResultMsg{HeightHint: 101, RoundHint: 4, Signer: "A", ExecHash: "root-b"}
	if !node.shouldLogExecutionVoteDrop("queued_syncing", first, execProposalSnapshot{}) {
		t.Fatal("expected first queued drop to be logged")
	}
	if node.shouldLogExecutionVoteDrop("queued_syncing", second, execProposalSnapshot{}) {
		t.Fatal("queued drop throttle must not be bypassed by per-vote debug keys")
	}
}

func TestRecordExecResultGlobalMovesChoiceForwardAcrossRoundGap(t *testing.T) {
	resetExecPoolForTest(t)

	epoch := uint64(89)
	first := proposalVoteKey(epoch, 1, "block-a", "", "root-a")
	higher := proposalVoteKey(epoch, 1+localExecVoteStaleRoundReleaseGap, "block-b", "", "root-b")
	if count, ok, equivocation := recordExecResultGlobal(epoch, first, "root-a", "", ExecutionResult{
		Height:     epoch,
		Round:      1,
		BlockHash:  "block-a",
		Signer:     "A",
		ResultHash: "root-a",
	}); !ok || equivocation || count != 1 {
		t.Fatalf("expected first vote count=1 ok=true equivocation=false, got count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if count, ok, equivocation := recordExecResultGlobalWithRequired(epoch, higher, "root-b", "", ExecutionResult{
		Height:     epoch,
		Round:      1 + localExecVoteStaleRoundReleaseGap,
		BlockHash:  "block-b",
		Signer:     "A",
		ResultHash: "root-b",
	}, 3); !ok || equivocation || count != 1 {
		t.Fatalf("expected monotonic higher-round switch, got count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if got := getExecCountGlobal(epoch, first, "root-a", ""); got != 0 {
		t.Fatalf("lower-round proposal should lose moved signer vote, got %d", got)
	}
	if got := getExecCountGlobal(epoch, higher, "root-b", ""); got != 1 {
		t.Fatalf("higher-round proposal should gain moved signer vote, got %d", got)
	}
}

func TestRecordExecResultGlobalScopesCrossRoundEquivocationByHeight(t *testing.T) {
	resetExecPoolForTest(t)

	firstHeight := uint64(90)
	nextHeight := uint64(91)
	first := proposalVoteKey(firstHeight, 1, "block-a", "", "root-a")
	next := proposalVoteKey(nextHeight, 1, "block-b", "", "root-b")

	if count, ok, equivocation := recordExecResultGlobal(firstHeight, first, "root-a", "", ExecutionResult{
		Height:     firstHeight,
		BlockHash:  "block-a",
		Signer:     "A",
		ResultHash: "root-a",
	}); !ok || equivocation || count != 1 {
		t.Fatalf("expected first-height vote accepted, count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
	if count, ok, equivocation := recordExecResultGlobal(nextHeight, next, "root-b", "", ExecutionResult{
		Height:     nextHeight,
		BlockHash:  "block-b",
		Signer:     "A",
		ResultHash: "root-b",
	}); !ok || equivocation || count != 1 {
		t.Fatalf("expected next-height vote accepted, count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
}

func TestDelayedCrossRoundExecutionVoteRejectsAndDoesNotPoisonProposal(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)

	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		privKeys[id] = priv
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	const lowRound uint32 = 10
	highRound := lowRound + execProposalSwitchRoundGap + 1
	lowBlock := buildProposalForRound(t, epoch, lowRound, validators, sources)
	highBlock := buildProposalForRound(t, epoch, highRound, validators, sources)
	if lowBlock.BlockHash == highBlock.BlockHash {
		t.Fatalf("test requires conflicting proposals")
	}

	target.noteObservedProposal(lowBlock)
	lowMsg := signedExecutionResultMsgForBlock(t, "B", privKeys["B"], lowBlock)
	target.processExecutionResultMsg(lowMsg, false)

	lowKey := proposalVoteKey(epoch, lowBlock.Round, lowBlock.BlockHash, lowBlock.MempoolRoot, lowBlock.StateRoot)
	if got := getExecCountGlobal(epoch, lowKey, lowBlock.StateRoot, lowBlock.MempoolRoot); got != 1 {
		t.Fatalf("expected initial low-round vote recorded, got=%d", got)
	}

	target.commitMu.Lock()
	target.committedHeight = epoch
	target.committed[epoch] = lowBlock.BlockHash
	target.commitMu.Unlock()
	target.noteObservedProposal(highBlock)

	highMsg := signedExecutionResultMsgForBlock(t, "B", privKeys["B"], highBlock)
	target.processExecutionResultMsg(highMsg, false)

	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := getExecCountGlobal(epoch, highKey, highBlock.StateRoot, highBlock.MempoolRoot); got != 0 {
		t.Fatalf("delayed conflicting higher-round vote should not be recorded, got=%d", got)
	}
	if target.hasExecSignerSeenForProposal(epoch, highKey, "B") {
		t.Fatalf("rejected equivocation must not mark signer as seen for conflicting proposal")
	}
	entries := target.MisbehaviorLog["B"]
	if len(entries) != 0 {
		t.Fatalf("committed-height delayed replay should be fenced before slashing evidence, got=%+v", entries)
	}
}

func buildProposalForRound(t *testing.T, epoch uint64, round uint32, validators []string, sources map[string]*Node) Block {
	t.Helper()

	leader := sources[validators[0]].consensusLeaderForHeightRound(epoch, round, validators)
	source, ok := sources[leader]
	if !ok {
		t.Fatalf("missing source for leader %s", leader)
	}
	source.setProposedRound(epoch, round)
	block := source.BuildLeaderBlock(epoch)
	if block.Round != round {
		t.Fatalf("unexpected proposal round: got=%d want=%d", block.Round, round)
	}
	return block
}

func signedExecutionResultMsgForBlock(t *testing.T, signer string, priv ed25519.PrivateKey, block Block) ExecutionResultMsg {
	t.Helper()

	msg := ExecutionResultMsg{
		HeightHint:    block.ID,
		RoundHint:     block.Round,
		BlockHashHint: block.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      block.StateRoot,
		TxMerkle:      block.MempoolRoot,
		Signer:        signer,
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(priv, execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))
	return msg
}

func setProposerRoundMaxForTest(t *testing.T, max uint32) {
	t.Helper()

	old := ProposerRoundMax
	ProposerRoundMax = max
	t.Cleanup(func() {
		ProposerRoundMax = old
	})
}

func TestEnterProposePhaseStoresLeaderBlockAndAdvancesTick(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	validators := []string{"A", "B", "C", "D"}
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	ValidatorPubKeys = map[string]ed25519.PublicKey{"A": pub}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{"A": pub}

	node := newValidatorRoundTestNode(t, t.TempDir(), "A", validators, pub, priv)

	epoch := node.currentEpoch()
	proposeRound := uint32(0)
	foundRound := false
	for round := uint32(0); round < 32; round++ {
		if node.consensusLeaderForHeightRound(epoch, round, validators) == node.ID {
			proposeRound = round
			foundRound = true
			break
		}
	}
	if !foundRound {
		t.Fatal("did not find a proposer round for node A")
	}
	node.setProposedRound(epoch, proposeRound)
	node.setLogicalTick(epoch, TickVote)
	block := node.BuildLeaderBlock(epoch)
	node.Role = "full"

	if !node.enterProposePhase(block, "unit_test") {
		t.Fatalf("enterProposePhase rejected valid leader block")
	}

	stored, ok := node.getLeaderBlock(block.ID)
	if !ok {
		t.Fatalf("leader block was not stored")
	}
	if stored.BlockHash != block.BlockHash {
		t.Fatalf("stored block mismatch: got=%s want=%s", stored.BlockHash, block.BlockHash)
	}

	node.logicalMu.Lock()
	tick := node.logicalClock.Tick
	node.logicalMu.Unlock()
	if tick != TickExec {
		t.Fatalf("logical tick not advanced to exec: got=%d want=%d", tick, TickExec)
	}
}

func TestEnterProposePhaseCommitsImmediatelyWhenProposalAlreadyHasQuorumVotes(t *testing.T) {
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})

	keys := make(map[string]ValidatorKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate key for %s: %v", id, err)
		}
		keys[id] = ValidatorKey{
			ID:         id,
			PublicKey:  pub,
			PrivateKey: priv,
		}
		ValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
		GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pub...)
	}

	node := newValidatorRoundTestNode(t, t.TempDir(), "A", validators, keys["A"].PublicKey, keys["A"].PrivateKey)

	epoch := node.currentEpoch()
	proposeRound := uint32(0)
	foundRound := false
	for round := uint32(0); round < 32; round++ {
		if node.consensusLeaderForHeightRound(epoch, round, validators) == node.ID {
			proposeRound = round
			foundRound = true
			break
		}
	}
	if !foundRound {
		t.Fatal("did not find a proposer round for node A")
	}
	node.setProposedRound(epoch, proposeRound)
	block := node.BuildLeaderBlock(epoch)

	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	for _, signer := range []string{"A", "B", "C"} {
		if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, ExecutionResult{
			Height:     epoch,
			BlockHash:  block.BlockHash,
			Signer:     signer,
			ResultHash: block.StateRoot,
			TxMerkle:   block.MempoolRoot,
		}); !ok {
			t.Fatalf("failed to preload exec quorum result for signer %s", signer)
		}
	}

	if !node.enterProposePhase(block, "unit_existing_quorum") {
		t.Fatalf("enterProposePhase rejected quorum-backed leader block")
	}
	if got := node.Blockchain.Height(); got != epoch {
		t.Fatalf("expected immediate commit at height %d, got=%d", epoch, got)
	}
	if _, ok := node.Blockchain.GetBlock(epoch); !ok {
		t.Fatalf("missing committed block at height %d", epoch)
	}
}

func TestEnterProposePhaseRejectsInvalidLeaderBlock(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	validators := []string{"A", "B", "C", "D"}
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	ValidatorPubKeys = map[string]ed25519.PublicKey{"A": pub}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{"A": pub}

	node := newValidatorRoundTestNode(t, t.TempDir(), "A", validators, pub, priv)

	epoch := node.currentEpoch()
	proposeRound := uint32(0)
	foundRound := false
	for round := uint32(0); round < 32; round++ {
		if node.consensusLeaderForHeightRound(epoch, round, validators) == node.ID {
			proposeRound = round
			foundRound = true
			break
		}
	}
	if !foundRound {
		t.Fatal("did not find a proposer round for node A")
	}
	node.setProposedRound(epoch, proposeRound)
	node.setLogicalTick(epoch, TickVote)
	block := node.BuildLeaderBlock(epoch)
	block.PrevHash = "bad-prev-hash"
	block.BlockHash = HashBlock(block)
	node.SignBlock(&block)

	if node.enterProposePhase(block, "unit_test_invalid") {
		t.Fatalf("enterProposePhase accepted invalid leader block")
	}
	if _, ok := node.getLeaderBlock(block.ID); ok {
		t.Fatalf("invalid proposal was stored")
	}

	node.logicalMu.Lock()
	tick := node.logicalClock.Tick
	node.logicalMu.Unlock()
	if tick != TickVote {
		t.Fatalf("logical tick changed on rejected proposal: got=%d want=%d", tick, TickVote)
	}
}

func TestSetProposedRoundIsMonotonic(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})

	const height uint64 = 709
	node.setProposedRound(height, 250)
	node.setProposedRound(height, 11)

	if got := node.proposedRoundForHeight(height); got != 250 {
		t.Fatalf("proposed round moved backwards: got=%d want=250", got)
	}
}

func TestSetProposedRoundClampsToConfiguredMax(t *testing.T) {
	setProposerRoundMaxForTest(t, 10)
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})

	const height uint64 = 709
	node.setProposedRound(height, 250)

	if got := node.proposedRoundForHeight(height); got != 10 {
		t.Fatalf("proposed round was not clamped: got=%d want=10", got)
	}
}

func TestProposerRoundRecoveryCapUsesExplicitConfig(t *testing.T) {
	t.Run("default is uncapped", func(t *testing.T) {
		setProposerRoundMaxForTest(t, 0)
		if got := proposerRoundRecoveryCap(); got != 0 {
			t.Fatalf("unexpected default recovery cap: got=%d want=0", got)
		}
	})

	t.Run("configured cap is honored", func(t *testing.T) {
		setProposerRoundMaxForTest(t, 7)
		if got := proposerRoundRecoveryCap(); got != 7 {
			t.Fatalf("unexpected lower recovery cap: got=%d want=7", got)
		}
	})

	t.Run("configured higher cap is not silently bounded", func(t *testing.T) {
		setProposerRoundMaxForTest(t, 84)
		if got := proposerRoundRecoveryCap(); got != 84 {
			t.Fatalf("unexpected higher recovery cap: got=%d want=84", got)
		}
	})
}

func TestEnterProposerRoundRecoveryModePausesConsensus(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})
	node.enterProposerRoundRecoveryMode(2, 84, 10)

	node.recomputePauseMu.Lock()
	reason := node.recomputePauseReason
	height := node.recomputePauseHeight
	until := node.recomputePauseUntil
	node.recomputePauseMu.Unlock()
	if reason != "round_cap_exceeded" {
		t.Fatalf("unexpected recovery pause reason: got=%q want=%q", reason, "round_cap_exceeded")
	}
	if height != 2 {
		t.Fatalf("unexpected recovery pause height: got=%d want=2", height)
	}
	if until.IsZero() {
		t.Fatalf("expected recovery pause deadline to be set")
	}
	if node.Consensus == nil || !node.Consensus.Paused {
		t.Fatalf("expected consensus to be paused during round recovery")
	}
}

func TestPauseConsensusForLivenessShortfallRecordsNonBlockingPause(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	height := uint64(747)
	snap := CommitteeLivenessSnapshot{
		Height:        height,
		CommitteeSize: 4,
		Live:          2,
		Offline:       2,
	}

	node.pauseConsensusForLivenessShortfall(height, 3, snap)

	node.recomputePauseMu.Lock()
	reason := node.recomputePauseReason
	pausedHeight := node.recomputePauseHeight
	until := node.recomputePauseUntil
	node.recomputePauseMu.Unlock()
	if reason != "live_quorum_unavailable" {
		t.Fatalf("unexpected liveness pause reason: got=%q want=%q", reason, "live_quorum_unavailable")
	}
	if pausedHeight != height {
		t.Fatalf("unexpected liveness pause height: got=%d want=%d", pausedHeight, height)
	}
	if until.IsZero() {
		t.Fatalf("expected liveness pause deadline to be set")
	}
	if node.Consensus != nil && node.Consensus.Paused {
		t.Fatalf("expected liveness shortfall pause to stay non-blocking")
	}
	if node.consensusRecomputePauseActive() {
		t.Fatalf("expected liveness shortfall pause not to block proposal vote/finality paths")
	}
}

func TestPauseConsensusForLivenessShortfallDoesNotPauseAtQuorum(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	height := uint64(747)
	snap := CommitteeLivenessSnapshot{
		Height:        height,
		CommitteeSize: 4,
		Live:          3,
		Offline:       1,
	}

	node.pauseConsensusForLivenessShortfall(height, 3, snap)

	node.recomputePauseMu.Lock()
	reason := node.recomputePauseReason
	until := node.recomputePauseUntil
	node.recomputePauseMu.Unlock()
	if reason != "" {
		t.Fatalf("expected no liveness pause reason at quorum: got=%q", reason)
	}
	if !until.IsZero() {
		t.Fatalf("expected no liveness pause deadline at quorum")
	}
	if node.Consensus != nil && node.Consensus.Paused {
		t.Fatalf("expected consensus to remain unpaused at quorum")
	}
}

func TestLeaderProposalRetryStateAllowsHigherRoundSameEpoch(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.lastLeaderEpoch = 17
	node.lastLeaderRound = 0
	node.lastLeaderSlot = time.Now().UnixNano()

	sameEpoch, sameRound, throttle := node.leaderProposalRetryState(17, 1, 5*time.Second)
	if !sameEpoch {
		t.Fatalf("expected same epoch retry state")
	}
	if sameRound {
		t.Fatalf("expected higher round to avoid same-round throttle classification")
	}
	if throttle {
		t.Fatalf("expected higher round in same epoch to bypass resend throttle")
	}
}

func TestLeaderProposalRetryStateThrottlesSameRoundSameEpoch(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	node.lastLeaderEpoch = 17
	node.lastLeaderRound = 3
	node.lastLeaderSlot = time.Now().UnixNano()

	sameEpoch, sameRound, throttle := node.leaderProposalRetryState(17, 3, 5*time.Second)
	if !sameEpoch || !sameRound {
		t.Fatalf("expected same-round retry classification, got sameEpoch=%t sameRound=%t", sameEpoch, sameRound)
	}
	if !throttle {
		t.Fatalf("expected same-round resend to stay throttled inside retry window")
	}
}

func TestSelectLiveLeaderForHeightRoundIgnoresLocalLiveness(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	now := time.Now()
	height := uint64(1)

	node.commitMu.Lock()
	node.finalizedHeight = height
	node.committedHeight = height
	node.commitMu.Unlock()

	deadLeaderRound := uint32(0)
	found := false
	for round := uint32(0); round < 32; round++ {
		if node.consensusLeaderForHeightRound(height, round, validators) == "A" {
			deadLeaderRound = round
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected to find a canonical round for leader A")
	}

	node.validatorMu.Lock()
	node.validatorStatus = make(map[string]*ValidatorStatus, len(validators))
	for _, id := range validators {
		lastSeen := now
		if id == "A" {
			lastSeen = now.Add(-(validatorLivenessHeartbeatTTL() + validatorLivenessGrace() + time.Second))
		}
		node.validatorStatus[id] = &ValidatorStatus{
			ID:                 id,
			ReportedHeight:     height,
			FinalizedHeight:    height,
			ExecEpoch:          height,
			ValidatorSetHeight: height,
			LastSeen:           lastSeen,
			Active:             true,
		}
	}
	node.validatorMu.Unlock()

	gotLeader, gotRound, skipped := node.selectLiveLeaderForHeightRound(height, deadLeaderRound, validators)
	if gotLeader != "A" {
		t.Fatalf("local liveness must not change canonical leader: got=%s want=A", gotLeader)
	}
	if gotRound != deadLeaderRound {
		t.Fatalf("local liveness must not advance the round: got=%d want=%d", gotRound, deadLeaderRound)
	}
	if skipped != 0 {
		t.Fatalf("local liveness must not report skipped leaders: got=%d", skipped)
	}
	if canonical := node.consensusLeaderForHeightRound(height, gotRound, validators); canonical != "A" {
		t.Fatalf("expected canonical leader to remain A, got=%s", canonical)
	}
}

func TestSelectLiveLeaderForHeightRoundKeepsCanonicalLeaderWhenLive(t *testing.T) {
	validators := []string{"A", "B", "C", "D"}
	node := newTestNodeForResultGossip(t, t.TempDir(), validators)
	now := time.Now()
	height := uint64(1)

	node.commitMu.Lock()
	node.finalizedHeight = height
	node.committedHeight = height
	node.commitMu.Unlock()

	startRound := uint32(0)
	wantLeader := node.consensusLeaderForHeightRound(height, startRound, validators)
	if wantLeader == "" {
		t.Fatalf("expected canonical leader for round 0")
	}

	node.validatorMu.Lock()
	node.validatorStatus = make(map[string]*ValidatorStatus, len(validators))
	for _, id := range validators {
		node.validatorStatus[id] = &ValidatorStatus{
			ID:                 id,
			ReportedHeight:     height,
			FinalizedHeight:    height,
			ExecEpoch:          height,
			ValidatorSetHeight: height,
			LastSeen:           now,
			Active:             true,
		}
	}
	node.validatorMu.Unlock()

	gotLeader, gotRound, skipped := node.selectLiveLeaderForHeightRound(height, startRound, validators)
	if gotLeader != wantLeader {
		t.Fatalf("unexpected live leader: got=%s want=%s", gotLeader, wantLeader)
	}
	if gotRound != startRound {
		t.Fatalf("unexpected round advance for live canonical leader: got=%d want=%d", gotRound, startRound)
	}
	if skipped != 0 {
		t.Fatalf("unexpected skip count for live canonical leader: got=%d want=0", skipped)
	}
}

func TestEffectiveProposerRoundTimeoutUsesProposalWindowFloor(t *testing.T) {
	oldFast := ConsensusFastProposerFailoverEnabled
	t.Cleanup(func() {
		ConsensusFastProposerFailoverEnabled = oldFast
	})
	ConsensusFastProposerFailoverEnabled = false

	got := effectiveProposerRoundTimeout(1*time.Second, 4*time.Second, 500*time.Millisecond)
	if got != 4*time.Second {
		t.Fatalf("expected min-block-interval floor to win: got=%s want=%s", got, 4*time.Second)
	}
}

func TestEffectiveProposerRoundTimeoutFastFailoverHonorsConfiguredTimeout(t *testing.T) {
	oldFast := ConsensusFastProposerFailoverEnabled
	oldMin := ConsensusFastProposerFailoverMin
	t.Cleanup(func() {
		ConsensusFastProposerFailoverEnabled = oldFast
		ConsensusFastProposerFailoverMin = oldMin
	})
	ConsensusFastProposerFailoverEnabled = true
	ConsensusFastProposerFailoverMin = time.Second

	got := effectiveProposerRoundTimeout(2*time.Second, 4*time.Second, 2*time.Second)
	if got != 2*time.Second {
		t.Fatalf("expected fast failover to honor configured timeout: got=%s want=%s", got, 2*time.Second)
	}
}

func TestComputeConsensusRoundStartsAtZeroWhenProposalWindowOpens(t *testing.T) {
	epochStartedAt := time.Unix(100, 0)
	now := epochStartedAt.Add(4 * time.Second)
	got := computeConsensusRound(now, epochStartedAt, 0, time.Time{}, 4*time.Second, 1*time.Second, 500*time.Millisecond)
	if got != 0 {
		t.Fatalf("expected first proposal window to remain on round 0: got=%d want=0", got)
	}
}

func TestComputeConsensusRoundFastFailoverDoesNotWaitForMinBlockWindow(t *testing.T) {
	oldFast := ConsensusFastProposerFailoverEnabled
	oldMin := ConsensusFastProposerFailoverMin
	t.Cleanup(func() {
		ConsensusFastProposerFailoverEnabled = oldFast
		ConsensusFastProposerFailoverMin = oldMin
	})
	ConsensusFastProposerFailoverEnabled = true
	ConsensusFastProposerFailoverMin = time.Second

	epochStartedAt := time.Unix(100, 0)
	got := computeConsensusRound(epochStartedAt.Add(2*time.Second), epochStartedAt, 0, time.Time{}, 4*time.Second, 2*time.Second, 2*time.Second)
	if got != 1 {
		t.Fatalf("expected fast failover to advance after configured timeout: got=%d want=1", got)
	}
}

func TestComputeConsensusRoundAdvancesFromObservedRoundAnchor(t *testing.T) {
	oldFast := ConsensusFastProposerFailoverEnabled
	t.Cleanup(func() {
		ConsensusFastProposerFailoverEnabled = oldFast
	})
	ConsensusFastProposerFailoverEnabled = false

	epochStartedAt := time.Unix(100, 0)
	observedAt := epochStartedAt.Add(9 * time.Second)

	got := computeConsensusRound(observedAt.Add(3*time.Second), epochStartedAt, 5, observedAt, 4*time.Second, 1*time.Second, 500*time.Millisecond)
	if got != 5 {
		t.Fatalf("expected observed round to stay sticky inside timeout window: got=%d want=5", got)
	}

	got = computeConsensusRound(observedAt.Add(4*time.Second), epochStartedAt, 5, observedAt, 4*time.Second, 1*time.Second, 500*time.Millisecond)
	if got != 6 {
		t.Fatalf("expected one failover step after timeout: got=%d want=6", got)
	}
}

func TestConsensusRoundAnchorsWithGateHoldFreezesRoundClock(t *testing.T) {
	oldFast := ConsensusFastProposerFailoverEnabled
	t.Cleanup(func() {
		ConsensusFastProposerFailoverEnabled = oldFast
	})
	ConsensusFastProposerFailoverEnabled = false

	epochStartedAt := time.Unix(100, 0)
	observedAt := epochStartedAt.Add(4 * time.Second)
	gateHeldAt := observedAt.Add(20 * time.Second)

	heldEpochAt, heldObservedAt := consensusRoundAnchorsWithGateHold(epochStartedAt, observedAt, gateHeldAt)
	got := computeConsensusRound(gateHeldAt.Add(3*time.Second), heldEpochAt, 2, heldObservedAt, 4*time.Second, 1*time.Second, 500*time.Millisecond)
	if got != 2 {
		t.Fatalf("expected gate hold to keep observed round sticky: got=%d want=2", got)
	}

	got = computeConsensusRound(gateHeldAt.Add(8*time.Second), heldEpochAt, 2, heldObservedAt, 4*time.Second, 1*time.Second, 500*time.Millisecond)
	if got != 3 {
		t.Fatalf("expected round to advance only after a fresh timeout: got=%d want=3", got)
	}
}

func TestVerifyLeaderBlockCatchesUpToObservedRound(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldDebugConsensus := DebugConsensus
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		DebugConsensus = oldDebugConsensus
	})

	DebugConsensus = false

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	ValidatorPubKeys = map[string]ed25519.PublicKey{"A": pub}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{"A": pub}

	validators := []string{"A"}
	source := newValidatorRoundTestNode(t, t.TempDir(), "A", validators, pub, priv)
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	target.ID = "TARGET"

	const epoch uint64 = 1
	source.setProposedRound(epoch, 250)
	block := source.BuildLeaderBlock(epoch)
	if block.Round != 250 {
		t.Fatalf("unexpected source block round: got=%d want=250", block.Round)
	}

	target.setProposedRound(epoch, 10)
	if got := target.proposedRoundForHeight(epoch); got != 10 {
		t.Fatalf("unexpected initial local round: got=%d want=10", got)
	}

	if ok := target.verifyLeaderBlock(block, "peer-A"); !ok {
		t.Fatalf("expected valid high-round proposal to be accepted after round catch-up")
	}
	if got := target.proposedRoundForHeight(epoch); got != 250 {
		t.Fatalf("target did not catch up to observed round: got=%d want=250", got)
	}
}

func TestVerifyLeaderBlockRejectsProposalAboveConfiguredMax(t *testing.T) {
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldDebugConsensus := DebugConsensus
	oldMaxRound := ProposerRoundMax
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		DebugConsensus = oldDebugConsensus
		ProposerRoundMax = oldMaxRound
	})

	DebugConsensus = false

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	ValidatorPubKeys = map[string]ed25519.PublicKey{"A": pub}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{"A": pub}

	validators := []string{"A"}
	source := newValidatorRoundTestNode(t, t.TempDir(), "A", validators, pub, priv)
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	target.ID = "TARGET"

	const epoch uint64 = 1
	ProposerRoundMax = 0
	source.setProposedRound(epoch, 11)
	block := source.BuildLeaderBlock(epoch)
	if block.Round != 11 {
		t.Fatalf("unexpected source block round: got=%d want=11", block.Round)
	}

	ProposerRoundMax = 10
	if ok := target.verifyLeaderBlock(block, "peer-A"); ok {
		t.Fatalf("expected over-cap proposal to be rejected")
	}
}

func TestHandleLeaderBlockReplacesLowerRoundAfterCatchUp(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldDebugConsensus := DebugConsensus
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		DebugConsensus = oldDebugConsensus
	})

	DebugConsensus = false

	pubA, privA, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	pubB, privB, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen failed: %v", err)
	}
	ValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": pubA,
		"B": pubB,
	}
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": pubA,
		"B": pubB,
	}

	validators := []string{"A", "B"}
	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	target.ID = "TARGET"

	const epoch uint64 = 1
	lowRound := uint32(10)
	highRound := uint32(250)
	lowLeader := target.consensusLeaderForHeightRound(epoch, lowRound, validators)
	highLeader := target.consensusLeaderForHeightRound(epoch, highRound, validators)
	for highLeader == lowLeader {
		highRound++
		highLeader = target.consensusLeaderForHeightRound(epoch, highRound, validators)
	}
	if lowLeader == "" || highLeader == "" {
		t.Fatalf("expected deterministic leaders for both rounds")
	}

	var lowSource *Node
	switch lowLeader {
	case "A":
		lowSource = newValidatorRoundTestNode(t, t.TempDir(), "A", validators, pubA, privA)
	case "B":
		lowSource = newValidatorRoundTestNode(t, t.TempDir(), "B", validators, pubB, privB)
	default:
		t.Fatalf("unexpected low-round leader: %s", lowLeader)
	}
	var highSource *Node
	switch highLeader {
	case "A":
		highSource = newValidatorRoundTestNode(t, t.TempDir(), "A", validators, pubA, privA)
	case "B":
		highSource = newValidatorRoundTestNode(t, t.TempDir(), "B", validators, pubB, privB)
	default:
		t.Fatalf("unexpected high-round leader: %s", highLeader)
	}

	lowSource.setProposedRound(epoch, lowRound)
	lowBlock := lowSource.BuildLeaderBlock(epoch)
	if lowBlock.Round != lowRound {
		t.Fatalf("unexpected low block round: got=%d want=%d", lowBlock.Round, lowRound)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round leader block")
	}

	target.setProposedRound(epoch, lowRound)

	highSource.setProposedRound(epoch, highRound)
	highBlock := highSource.BuildLeaderBlock(epoch)
	if highBlock.Round != highRound {
		t.Fatalf("unexpected high block round: got=%d want=%d", highBlock.Round, highRound)
	}
	if highBlock.BlockHash == lowBlock.BlockHash {
		t.Fatalf("expected distinct proposal hash for replacement path")
	}

	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("expected higher-round proposal to be stored")
	}

	got, ok := target.getLeaderBlock(epoch)
	if !ok {
		t.Fatalf("expected leader block at epoch %d", epoch)
	}
	if got.BlockHash != highBlock.BlockHash {
		t.Fatalf("expected higher-round proposal to replace stored block: got=%s want=%s", got.BlockHash, highBlock.BlockHash)
	}
	if got.Round != highRound {
		t.Fatalf("expected stored block round to advance: got=%d want=%d", got.Round, highRound)
	}
	if gotRound := target.proposedRoundForHeight(epoch); gotRound != highRound {
		t.Fatalf("expected round floor to advance with replacement: got=%d want=%d", gotRound, highRound)
	}
}

func TestStoreLeaderBlockAllowsConflictingProposalWithOnlyPrevotes(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	const lowRound uint32 = 10
	highRound := lowRound + 1
	lowBlock := buildProposalForRound(t, epoch, lowRound, validators, sources)
	highBlock := buildProposalForRound(t, epoch, highRound, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash {
		highRound++
		highBlock = buildProposalForRound(t, epoch, highRound, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	lockedKey := target.currentProposalVoteKey(epoch)
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, "A") {
		t.Fatalf("expected first prevote on proposal")
	}
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, "B") {
		t.Fatalf("expected second prevote on proposal")
	}
	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("expected conflicting higher-round proposal to remain admissible before quorum lock")
	}
	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := target.currentProposalVoteKey(epoch); got != highKey {
		t.Fatalf("expected higher-round proposal to replace prevote-only candidate: got=%s want=%s old=%s", got, highKey, lockedKey)
	}
	got, ok := target.getLeaderBlock(epoch)
	if !ok || got.BlockHash != highBlock.BlockHash {
		t.Fatalf("expected higher-round leader block to replace prevote-only proposal")
	}
}

func TestStoreLeaderBlockAllowsHigherRoundAfterSingleVote(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	const lowRound uint32 = 10
	highRound := lowRound + 1
	lowBlock := buildProposalForRound(t, epoch, lowRound, validators, sources)
	highBlock := buildProposalForRound(t, epoch, highRound, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash {
		highRound++
		highBlock = buildProposalForRound(t, epoch, highRound, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	lockedKey := target.currentProposalVoteKey(epoch)
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, "A") {
		t.Fatalf("expected first signer mark on locked proposal")
	}
	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("failed to store higher-round proposal")
	}

	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := target.currentProposalVoteKey(epoch); got != highKey {
		t.Fatalf("expected higher-round proposal to replace single-vote candidate: got=%s want=%s old=%s", got, highKey, lockedKey)
	}
}

func TestAcceptedProposalSingleVoteDoesNotLockRoundZero(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	lowBlock := buildProposalForRound(t, epoch, 0, validators, sources)
	highBlock := buildProposalForRound(t, epoch, 2, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash {
		highBlock = buildProposalForRound(t, epoch, highBlock.Round+1, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store round-zero proposal")
	}
	lockedKey := target.currentProposalVoteKey(epoch)
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, "A") {
		t.Fatalf("expected first signer mark on round-zero proposal")
	}
	lockedBlock, lockedVotes, locked, reason := target.acceptedProposalVoteLockForRound(epoch, highBlock.Round)
	if locked || lockedVotes != 0 || reason != "" || lockedBlock.ID != 0 {
		t.Fatalf("single vote must not create a round lock, locked=%t votes=%d reason=%q block=%s",
			locked, lockedVotes, reason, ShortHash(lockedBlock.BlockHash))
	}

	target.execResultsMu.Lock()
	changed := target.setAcceptedProposalLocked(highBlock, "observed", false)
	target.execResultsMu.Unlock()
	if !changed {
		t.Fatalf("expected higher-round proposal to replace non-quorum round-zero proposal")
	}
	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := target.currentProposalVoteKey(epoch); got != highKey {
		t.Fatalf("expected higher-round proposal to become current: got=%s want=%s old=%s", got, highKey, lockedKey)
	}
}

func TestAcceptedProposalLockIgnoresLocalCommitAge(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	lowBlock := buildProposalForRound(t, epoch, 1, validators, sources)
	highBlock := buildProposalForRound(t, epoch, 1+execProposalSwitchRoundGap+1, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash {
		highBlock = buildProposalForRound(t, epoch, highBlock.Round+1, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	lockedKey := target.currentProposalVoteKey(epoch)
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, "A") {
		t.Fatalf("expected local vote marker on locked proposal")
	}
	target.lastCommitAt = time.Now().Add(-blockProductionStaleThreshold() - time.Second)

	lockedBlock, lockedVotes, locked, reason := target.acceptedProposalVoteLockForRound(epoch, highBlock.Round)
	if locked || lockedVotes != 0 || reason != "" || lockedBlock.ID != 0 {
		t.Fatalf("local commit age and a single vote must not create a lock, locked=%t votes=%d reason=%q block=%s",
			locked, lockedVotes, reason, ShortHash(lockedBlock.BlockHash))
	}

	target.execResultsMu.Lock()
	changed := target.setAcceptedProposalLocked(highBlock, "observed_after_soft_expiry", false)
	target.execResultsMu.Unlock()
	if !changed {
		t.Fatalf("expected higher-round proposal to replace non-quorum proposal regardless of local commit age")
	}
	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := target.currentProposalVoteKey(epoch); got != highKey {
		t.Fatalf("expected higher-round proposal to become current: got=%s want=%s old=%s", got, highKey, lockedKey)
	}
}

func TestStoreLeaderBlockAllowsNearbyHigherRoundAfterLocalPrevote(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	target.ID = "A"

	const epoch uint64 = 1
	const lowRound uint32 = 10
	highRound := lowRound + 1
	lowBlock := buildProposalForRound(t, epoch, lowRound, validators, sources)
	highBlock := buildProposalForRound(t, epoch, highRound, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash {
		highRound++
		highBlock = buildProposalForRound(t, epoch, highRound, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	lockedKey := target.currentProposalVoteKey(epoch)
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, target.ID) {
		t.Fatalf("expected local prevote to be tracked on proposal")
	}
	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("expected nearby conflicting higher-round proposal to stay admissible after local prevote")
	}
	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := target.currentProposalVoteKey(epoch); got != highKey {
		t.Fatalf("expected nearby higher-round proposal to replace local prevote candidate: got=%s want=%s old=%s", got, highKey, lockedKey)
	}
}

func TestStoreLeaderBlockAllowsFarHigherRoundAfterLocalPrevote(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	target.ID = "A"

	const epoch uint64 = 1
	const lowRound uint32 = 10
	highRound := lowRound + execProposalSwitchRoundGap + 1
	lowBlock := buildProposalForRound(t, epoch, lowRound, validators, sources)
	highBlock := buildProposalForRound(t, epoch, highRound, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash {
		highRound++
		highBlock = buildProposalForRound(t, epoch, highRound, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	lockedKey := target.currentProposalVoteKey(epoch)
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, target.ID) {
		t.Fatalf("expected local prevote to be tracked on proposal")
	}
	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("expected far higher-round proposal to remain admissible after local prevote")
	}

	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := target.currentProposalVoteKey(epoch); got != highKey {
		t.Fatalf("expected far higher-round proposal to replace local prevote candidate: got=%s want=%s old=%s", got, highKey, lockedKey)
	}
}

func TestStoreLeaderBlockDoesNotUseLocalReplacementTimer(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	const lowRound uint32 = 10
	midRound := lowRound + 1
	highRound := midRound + 1
	lowBlock := buildProposalForRound(t, epoch, lowRound, validators, sources)
	midBlock := buildProposalForRound(t, epoch, midRound, validators, sources)
	for midBlock.BlockHash == lowBlock.BlockHash {
		midRound++
		highRound = midRound + 1
		midBlock = buildProposalForRound(t, epoch, midRound, validators, sources)
	}
	highBlock := buildProposalForRound(t, epoch, highRound, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash || highBlock.BlockHash == midBlock.BlockHash {
		highRound++
		highBlock = buildProposalForRound(t, epoch, highRound, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	if !target.storeLeaderBlock(midBlock) {
		t.Fatalf("expected first conflicting replacement to be accepted")
	}
	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("expected rapid higher-round replacement to be accepted without a local timer gate")
	}

	got, ok := target.getLeaderBlock(epoch)
	if !ok || got.BlockHash != highBlock.BlockHash {
		t.Fatalf("expected leader block to advance to highest observed round")
	}
}

func TestStoreLeaderBlockAllowsSequentialHigherRoundReplacement(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	const lowRound uint32 = 10
	midRound := lowRound + 1
	highRound := midRound + 1
	lowBlock := buildProposalForRound(t, epoch, lowRound, validators, sources)
	midBlock := buildProposalForRound(t, epoch, midRound, validators, sources)
	for midBlock.BlockHash == lowBlock.BlockHash {
		midRound++
		highRound = midRound + 1
		midBlock = buildProposalForRound(t, epoch, midRound, validators, sources)
	}
	highBlock := buildProposalForRound(t, epoch, highRound, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash || highBlock.BlockHash == midBlock.BlockHash {
		highRound++
		highBlock = buildProposalForRound(t, epoch, highRound, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	if !target.storeLeaderBlock(midBlock) {
		t.Fatalf("expected first conflicting replacement to be accepted")
	}

	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("expected sequential higher-round replacement to be accepted")
	}

	got, ok := target.getLeaderBlock(epoch)
	if !ok || got.BlockHash != highBlock.BlockHash {
		t.Fatalf("expected leader block to advance after stabilization window")
	}
}

func TestStoreLeaderBlockAdvancesSameHashHigherRound(t *testing.T) {
	target := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	epoch := target.currentEpoch()
	block := target.BuildLeaderBlock(epoch)
	if !target.storeLeaderBlock(block) {
		t.Fatalf("failed to store initial leader block")
	}

	advanced := block
	advanced.Round = block.Round + execProposalSwitchRoundGap + 1
	if !target.storeLeaderBlock(advanced) {
		t.Fatalf("expected same-hash higher-round block to be accepted")
	}

	got, ok := target.getLeaderBlock(epoch)
	if !ok {
		t.Fatalf("missing stored leader block at epoch %d", epoch)
	}
	if got.Round != advanced.Round {
		t.Fatalf("expected leader round to advance for same hash: got=%d want=%d", got.Round, advanced.Round)
	}
	wantKey := proposalVoteKey(epoch, advanced.Round, advanced.BlockHash, advanced.MempoolRoot, advanced.StateRoot)
	if gotKey := target.currentProposalVoteKey(epoch); gotKey != wantKey {
		t.Fatalf("expected accepted proposal key to advance with same-hash round: got=%s want=%s", gotKey, wantKey)
	}
	if blockSeenKey(block) == blockSeenKey(advanced) {
		t.Fatalf("expected seen-block keys to differ across rounds for same hash")
	}
}

func TestStoreLeaderBlockKeepsSameHashRoundWhenQuorumLocked(t *testing.T) {
	resetExecPoolForTest(t)
	target := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	epoch := target.currentEpoch()
	block := target.BuildLeaderBlock(epoch)
	if !target.storeLeaderBlock(block) {
		t.Fatalf("failed to store initial leader block")
	}

	proposalKey := proposalVoteKey(epoch, block.Round, block.BlockHash, block.MempoolRoot, block.StateRoot)
	for _, signer := range []string{"A", "B", "C"} {
		if _, ok, _ := recordExecResultGlobal(epoch, proposalKey, block.StateRoot, block.MempoolRoot, ExecutionResult{
			Height:     epoch,
			BlockHash:  block.BlockHash,
			Signer:     signer,
			ResultHash: block.StateRoot,
			TxMerkle:   block.MempoolRoot,
		}); !ok {
			t.Fatalf("failed to store execution vote for signer %s", signer)
		}
	}

	advanced := block
	advanced.Round = block.Round + execProposalSwitchRoundGap + 1
	if !target.storeLeaderBlock(advanced) {
		t.Fatalf("expected quorum-locked same-hash proposal to remain observable")
	}

	got, ok := target.getLeaderBlock(epoch)
	if !ok {
		t.Fatalf("missing leader block after quorum lock test")
	}
	if got.Round != block.Round {
		t.Fatalf("expected quorum-locked leader round to stay put: got=%d want=%d", got.Round, block.Round)
	}
}

func TestQueueFutureBlockBoundsPerHeight(t *testing.T) {
	target := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	height := target.Blockchain.Height() + 2
	for round := uint32(1); round <= maxQueuedForkBlocksPerHeight+3; round++ {
		target.QueueFutureBlock(Block{
			ID:        height,
			Round:     round,
			BlockHash: "block-" + string(rune('a'+round)),
			PrevHash:  "prev",
			Proposer:  "A",
		})
	}

	got := len(target.ForkBlocks[height])
	if got != maxQueuedForkBlocksPerHeight {
		t.Fatalf("expected bounded future block queue: got=%d want=%d", got, maxQueuedForkBlocksPerHeight)
	}
	highest := uint32(0)
	for _, block := range target.ForkBlocks[height] {
		if block.Round > highest {
			highest = block.Round
		}
	}
	if want := uint32(maxQueuedForkBlocksPerHeight + 3); highest != want {
		t.Fatalf("expected highest rounds to be retained: got=%d want=%d", highest, want)
	}
}

func TestProcessExecutionResultMsgRejectsConflictingLockedVote(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	const lowRound uint32 = 10
	highRound := lowRound + execProposalSwitchRoundGap + 1
	lowBlock := buildProposalForRound(t, epoch, lowRound, validators, sources)
	highBlock := buildProposalForRound(t, epoch, highRound, validators, sources)
	for highBlock.BlockHash == lowBlock.BlockHash {
		highRound++
		highBlock = buildProposalForRound(t, epoch, highRound, validators, sources)
	}
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	lockedKey := target.currentProposalVoteKey(epoch)
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, "A") {
		t.Fatalf("expected first signer mark on locked proposal")
	}
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, "C") {
		t.Fatalf("expected second signer mark on locked proposal")
	}
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, "D") {
		t.Fatalf("expected quorum signer mark on locked proposal")
	}
	for _, signer := range []string{"A", "C", "D"} {
		if _, ok, equivocation := recordExecResultGlobal(epoch, lockedKey, lowBlock.StateRoot, lowBlock.MempoolRoot, ExecutionResult{
			Height:     epoch,
			BlockHash:  lowBlock.BlockHash,
			Signer:     signer,
			ResultHash: lowBlock.StateRoot,
			TxMerkle:   lowBlock.MempoolRoot,
		}); !ok || equivocation {
			t.Fatalf("expected quorum vote record for signer %s: ok=%t equivocation=%t", signer, ok, equivocation)
		}
	}
	if !target.proposalHasExecutionQuorum(lowBlock) {
		t.Fatalf("expected low-round proposal to be quorum locked")
	}
	recordSignedCommitVotesForTest(t, target, lowBlock, []string{"A", "C", "D"}, privKeys)
	target.execResultsMu.Lock()
	if !target.setQuorumLockedProposalLocked(lowBlock, "test_quorum_lock", 3, 3) {
		target.execResultsMu.Unlock()
		t.Fatalf("expected explicit quorum lock to be recorded")
	}
	target.execResultsMu.Unlock()
	target.noteObservedProposal(highBlock)

	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     highBlock.Round,
		BlockHashHint: highBlock.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      highBlock.StateRoot,
		TxMerkle:      highBlock.MempoolRoot,
		Signer:        "B",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	target.processExecutionResultMsg(msg, false)

	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := getExecCountGlobal(epoch, highKey, highBlock.StateRoot, highBlock.MempoolRoot); got != 0 {
		t.Fatalf("conflicting locked vote should not be recorded: got=%d want=0", got)
	}
	if got := target.currentProposalVoteKey(epoch); got != lockedKey {
		t.Fatalf("accepted proposal should stay locked while prior proposal has votes: got=%s want=%s", got, lockedKey)
	}
}

func TestCurrentProposalVoteKeyPrefersQuorumLockedProposal(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		privKeys[id] = priv
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	const lowRound uint32 = 10
	highRound := lowRound + execProposalSwitchRoundGap + 1
	lowBlock := buildProposalForRound(t, epoch, lowRound, validators, sources)
	highBlock := buildProposalForRound(t, epoch, highRound, validators, sources)
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	lowKey := proposalVoteKey(epoch, lowBlock.Round, lowBlock.BlockHash, lowBlock.MempoolRoot, lowBlock.StateRoot)
	for _, signer := range []string{"A", "B", "C"} {
		if _, ok, equivocation := recordExecResultGlobal(epoch, lowKey, lowBlock.StateRoot, lowBlock.MempoolRoot, ExecutionResult{
			Height:     epoch,
			BlockHash:  lowBlock.BlockHash,
			Signer:     signer,
			ResultHash: lowBlock.StateRoot,
			TxMerkle:   lowBlock.MempoolRoot,
		}); !ok || equivocation {
			t.Fatalf("expected quorum vote record for signer %s: ok=%t equivocation=%t", signer, ok, equivocation)
		}
	}

	target.execResultsMu.Lock()
	if !target.setAcceptedProposalLocked(highBlock, "test_current_candidate", true) {
		target.execResultsMu.Unlock()
		t.Fatalf("expected current candidate to move for test setup")
	}
	recordSignedCommitVotesForTest(t, target, lowBlock, []string{"A", "B", "C"}, privKeys)
	if !target.setQuorumLockedProposalLocked(lowBlock, "test_quorum_lock", 3, 3) {
		target.execResultsMu.Unlock()
		t.Fatalf("expected quorum lock to be recorded")
	}
	target.execResultsMu.Unlock()

	if got := target.currentProposalVoteKey(epoch); got != lowKey {
		t.Fatalf("expected vote target to prefer quorum-locked proposal: got=%s want=%s", got, lowKey)
	}
}

func TestProcessExecutionResultMsgAcceptsDelayedVoteAfterRoundChurn(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1

	baseBlock := buildProposalForRound(t, epoch, 10, validators, sources)
	if !target.storeLeaderBlock(baseBlock) {
		t.Fatalf("failed to store base proposal")
	}

	for round := uint32(11); round <= 22; round++ {
		block := buildProposalForRound(t, epoch, round, validators, sources)
		target.handleLeaderBlock(block, "peer-round-churn")
	}

	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     baseBlock.Round,
		BlockHashHint: baseBlock.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      baseBlock.StateRoot,
		TxMerkle:      baseBlock.MempoolRoot,
		Signer:        "B",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	target.processExecutionResultMsg(msg, false)

	baseKey := proposalVoteKey(epoch, baseBlock.Round, baseBlock.BlockHash, baseBlock.MempoolRoot, baseBlock.StateRoot)
	if got := getExecCountGlobal(epoch, baseKey, baseBlock.StateRoot, baseBlock.MempoolRoot); got != 1 {
		t.Fatalf("delayed execution vote was not recorded after round churn: got=%d want=1", got)
	}
}

func TestProcessExecutionResultMsgAdoptsRecentProposalAfterSinglePriorVote(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
	})
	resetExecPoolForTest(t)

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	lowBlock := buildProposalForRound(t, epoch, 10, validators, sources)
	highBlock := buildProposalForRound(t, epoch, 11, validators, sources)
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	lockedKey := target.currentProposalVoteKey(epoch)
	if !target.markExecSignerSeenForProposal(epoch, lockedKey, "A") {
		t.Fatalf("expected first signer mark on locked proposal")
	}
	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("failed to store higher-round proposal")
	}
	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := target.currentProposalVoteKey(epoch); got != highKey {
		t.Fatalf("expected higher-round proposal to replace single-vote proposal: got=%s want=%s old=%s", got, highKey, lockedKey)
	}

	msg := ExecutionResultMsg{
		HeightHint:    epoch,
		RoundHint:     highBlock.Round,
		BlockHashHint: highBlock.BlockHash,
		SigVersion:    execResultSigVersionV2,
		ExecHash:      highBlock.StateRoot,
		TxMerkle:      highBlock.MempoolRoot,
		Signer:        "B",
	}
	msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys["B"], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))

	target.processExecutionResultMsg(msg, false)

	if got := target.currentProposalVoteKey(epoch); got != highKey {
		t.Fatalf("expected recent proposal adoption to stay on higher-round proposal: got=%s want=%s", got, highKey)
	}
}

func TestProcessExecutionResultMsgFinalizesRecentProposalAfterObservedVotes(t *testing.T) {
	setProposerRoundMaxForTest(t, 0)
	oldValidatorPubKeys := ValidatorPubKeys
	oldGenesisValidatorPubKeys := GenesisValidatorPubKeys
	oldDebugConsensus := DebugConsensus
	t.Cleanup(func() {
		ValidatorPubKeys = oldValidatorPubKeys
		GenesisValidatorPubKeys = oldGenesisValidatorPubKeys
		DebugConsensus = oldDebugConsensus
	})
	resetExecPoolForTest(t)
	DebugConsensus = false

	validators := []string{"A", "B", "C", "D"}
	privKeys := make(map[string]ed25519.PrivateKey, len(validators))
	ValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(validators))
	sources := make(map[string]*Node, len(validators))
	for _, id := range validators {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("keygen failed: %v", err)
		}
		privKeys[id] = priv
		ValidatorPubKeys[id] = pub
		GenesisValidatorPubKeys[id] = pub
		sources[id] = newValidatorRoundTestNode(t, t.TempDir(), id, validators, pub, priv)
	}

	target := newTestNodeForResultGossip(t, t.TempDir(), validators)
	const epoch uint64 = 1
	lowBlock := buildProposalForRound(t, epoch, 10, validators, sources)
	highBlock := buildProposalForRound(t, epoch, 11, validators, sources)
	if !target.storeLeaderBlock(lowBlock) {
		t.Fatalf("failed to store lower-round proposal")
	}
	if !target.storeLeaderBlock(highBlock) {
		t.Fatalf("failed to store higher-round proposal")
	}
	highKey := proposalVoteKey(epoch, highBlock.Round, highBlock.BlockHash, highBlock.MempoolRoot, highBlock.StateRoot)
	if got := target.currentProposalVoteKey(epoch); got != highKey {
		t.Fatalf("expected deterministic higher-round proposal to replace non-quorum lower round: got=%s want=%s", got, highKey)
	}

	for _, signer := range []string{"A", "B", "C"} {
		msg := ExecutionResultMsg{
			HeightHint:    epoch,
			RoundHint:     highBlock.Round,
			BlockHashHint: highBlock.BlockHash,
			SigVersion:    execResultSigVersionV2,
			ExecHash:      highBlock.StateRoot,
			TxMerkle:      highBlock.MempoolRoot,
			Signer:        signer,
		}
		msg.Signature = hex.EncodeToString(ed25519.Sign(privKeys[signer], execResultSignBytesV2(msg.HeightHint, msg.RoundHint, msg.BlockHashHint, msg.ExecHash, msg.TxMerkle)))
		target.processExecutionResultMsg(msg, false)
	}
	for _, signer := range []string{"A", "B", "C"} {
		target.handleCommitMsg(signedCommitMsgForTest(t, highBlock, signer, privKeys[signer]))
	}

	if got := target.Blockchain.Height(); got != epoch {
		t.Fatalf("expected recent proposal quorum to finalize height %d, got chain height %d", epoch, got)
	}
	finalBlock, ok := target.Blockchain.GetBlock(epoch)
	if !ok {
		t.Fatalf("missing finalized block at height %d", epoch)
	}
	if finalBlock.Round != highBlock.Round {
		t.Fatalf("expected finalized round to match recent proposal: got=%d want=%d", finalBlock.Round, highBlock.Round)
	}
	if finalBlock.Proposer != highBlock.Proposer {
		t.Fatalf("expected finalized proposer to match recent proposal: got=%s want=%s", finalBlock.Proposer, highBlock.Proposer)
	}
}
