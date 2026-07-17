// Package consensus owns ordering, voting, quorum, and finality contracts. It
// consumes opaque execution commitments and never imports DTL functionality.
package consensus

import (
	"errors"
	"strings"

	"msc-chain/execution"
)

type Header struct {
	Height                uint64
	ProtocolVersion       execution.Version
	FeatureBitmap         execution.FeatureBitmap
	DTLV2ActivationHeight uint64
	ValidatorSetVersion   uint64
	CommitteeHash         string
	Execution             execution.Commitment
}

type CommitmentVote struct {
	Height         uint64
	ValidatorID    string
	CommitmentHash string
}

var ErrNoQuorum = errors.New("consensus: execution commitment quorum unavailable")

// VerifyCommitmentQuorum counts each validator once and only for the exact
// execution tuple committed by the header.
func VerifyCommitmentQuorum(header Header, votes []CommitmentVote, required int) error {
	if required <= 0 {
		return ErrNoQuorum
	}
	want := header.Execution.Hash()
	seen := make(map[string]struct{}, len(votes))
	for _, vote := range votes {
		validator := strings.TrimSpace(vote.ValidatorID)
		if vote.Height != header.Height || validator == "" || !strings.EqualFold(strings.TrimSpace(vote.CommitmentHash), want) {
			continue
		}
		seen[validator] = struct{}{}
	}
	if len(seen) < required {
		return ErrNoQuorum
	}
	return nil
}
