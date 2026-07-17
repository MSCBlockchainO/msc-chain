package main

import (
	"fmt"
	"strings"

	consensuscore "msc-chain/consensus"
	executioncore "msc-chain/execution"
)

func executionCommitmentConsensusHeader(block Block) consensuscore.Header {
	return consensuscore.Header{
		Height:                block.ID,
		ProtocolVersion:       executioncore.Version(block.ProtocolVersion),
		FeatureBitmap:         executioncore.FeatureBitmap(block.FeatureBitmap),
		DTLV2ActivationHeight: block.DTLV2ActivationHeight,
		ValidatorSetVersion:   block.ValidatorSetVersion,
		CommitteeHash:         strings.TrimSpace(block.CommitteeHash),
		Execution:             executionCommitmentsFromBlock(block),
	}
}

// executionCommitmentHashForBlock is empty for historical pre-version blocks.
// Versioned blocks use it as a separately signed consensus vote payload.
func executionCommitmentHashForBlock(block Block) string {
	if block.ProtocolVersion == 0 {
		return ""
	}
	return executionCommitmentsFromBlock(block).Hash()
}

func verifyCommitVoteExecutionCommitment(block Block, got string) error {
	want := executionCommitmentHashForBlock(block)
	if !strings.EqualFold(strings.TrimSpace(got), want) {
		return fmt.Errorf("execution_commitment_vote_mismatch")
	}
	return nil
}

// verifyExecutionCommitmentSignerQuorum is called only after finality witness
// signatures are cryptographically verified. Those signatures bind the block
// hash, and the block hash binds the complete execution tuple. This adapter
// makes that exact tuple the opaque value consumed by the physical consensus
// package; consensus never reads execution or DTL state.
func verifyExecutionCommitmentSignerQuorum(block Block, verifiedSigners []string, required int) error {
	if block.ProtocolVersion == 0 {
		return nil
	}
	header := executionCommitmentConsensusHeader(block)
	commitmentHash := header.Execution.Hash()
	votes := make([]consensuscore.CommitmentVote, 0, len(verifiedSigners))
	for _, rawSigner := range verifiedSigners {
		signer := normalizeValidatorID(rawSigner)
		if signer == "" {
			continue
		}
		votes = append(votes, consensuscore.CommitmentVote{
			Height:         block.ID,
			ValidatorID:    signer,
			CommitmentHash: commitmentHash,
		})
	}
	if err := consensuscore.VerifyCommitmentQuorum(header, votes, required); err != nil {
		return fmt.Errorf("execution_commitment_quorum: %w", err)
	}
	return nil
}
