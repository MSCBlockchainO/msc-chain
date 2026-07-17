package syncpipeline

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
)

type SnapshotProof struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `CheckpointHeight` stores the value associated with this record.
	CheckpointHeight uint64
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string
	// `SnapshotHash` stores the digest used to identify or verify the related data.
	SnapshotHash string
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string
	// `StateMerkleRoot` stores the digest used to identify or verify the related data.
	StateMerkleRoot string
	// `LedgerHash` stores the digest used to identify or verify the related data.
	LedgerHash string
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string
	// `CheckpointDomain` stores the value associated with this record.
	CheckpointDomain string
	// `Validator` stores whether the related condition is satisfied.
	Validator string
	// `Signature` stores the value associated with this record.
	Signature []byte
}

// SignatureHex implements the signature hex helper.
func (p SnapshotProof) SignatureHex() string {
	if len(p.Signature) == 0 {
		return ""
	}
	return hex.EncodeToString(p.Signature)
}

type SnapshotVerifier interface {
	CollectSnapshotProofs(ctx context.Context, meta SnapshotMeta) ([]SnapshotProof, error)
	RequiredProofs(ctx context.Context, meta SnapshotMeta) int
	VerifyProof(ctx context.Context, proof SnapshotProof) bool
	VerifyStateRoot(ctx context.Context, meta SnapshotMeta) error
}

// VerifySnapshotProofQuorum verifies snapshot proof quorum.
func VerifySnapshotProofQuorum(ctx context.Context, verifier SnapshotVerifier, meta SnapshotMeta) (int, int, error) {
	if verifier == nil {
		return 0, 0, errors.New("snapshot verifier unavailable")
	}
	// `proofs` and `err` store the error produced by this operation.
	proofs, err := verifier.CollectSnapshotProofs(ctx, meta)
	if err != nil {
		return 0, 0, err
	}
	// `required` stores the request data being processed.
	required := verifier.RequiredProofs(ctx, meta)
	if required <= 0 {
		required = 1
	}
	// `unique` stores the value produced by this operation.
	unique := make(map[string]struct{}, len(proofs))
	// `valid` stores whether the related condition is satisfied.
	valid := 0
	// `proof` tracks the current values while iterating.
	for _, proof := range proofs {
		validatorID := strings.ToUpper(strings.TrimSpace(proof.Validator))
		if validatorID == "" {
			continue
		}
		// `seen` stores the value produced by this operation.
		if _, seen := unique[validatorID]; seen {
			continue
		}
		if verifier.VerifyProof(ctx, proof) {
			unique[validatorID] = struct{}{}
			valid++
		}
	}
	if valid < required {
		return valid, required, errors.New("snapshot proof quorum not reached")
	}
	return valid, required, nil
}
