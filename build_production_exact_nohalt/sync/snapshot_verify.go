package syncpipeline

import (
	"context"
	"encoding/hex"
	"errors"
)

type SnapshotProof struct {
	Height                uint64
	CheckpointHeight      uint64
	BlockHash             string
	SnapshotHash          string
	StateRoot             string
	StateMerkleRoot       string
	LedgerHash            string
	ValidatorSetHash      string
	ValidatorSetRoot      string
	ValidatorRegistryHash string
	CheckpointDomain      string
	Validator             string
	Signature             []byte
}

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

func VerifySnapshotProofQuorum(ctx context.Context, verifier SnapshotVerifier, meta SnapshotMeta) (int, int, error) {
	if verifier == nil {
		return 0, 0, errors.New("snapshot verifier unavailable")
	}
	proofs, err := verifier.CollectSnapshotProofs(ctx, meta)
	if err != nil {
		return 0, 0, err
	}
	required := verifier.RequiredProofs(ctx, meta)
	if required <= 0 {
		required = 1
	}
	unique := make(map[string]struct{}, len(proofs))
	valid := 0
	for _, proof := range proofs {
		if _, seen := unique[proof.Validator]; seen {
			continue
		}
		if verifier.VerifyProof(ctx, proof) {
			unique[proof.Validator] = struct{}{}
			valid++
		}
	}
	if valid < required {
		return valid, required, errors.New("snapshot proof quorum not reached")
	}
	return valid, required, nil
}
