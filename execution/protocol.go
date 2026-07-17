// Package execution owns deterministic protocol selection and execution
// commitments. It has no dependency on consensus, networking, node runtime,
// clocks, randomness, or mempool state.
package execution

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"strings"

	"msc-chain/state"
)

type Version uint32

const (
	VersionV1 Version = 1
	VersionV2 Version = 2
)

type FeatureBitmap uint64

const (
	FeatureDTLV2 FeatureBitmap = 1 << iota
	FeatureBridge
	FeatureNFT
	FeatureLending
)

func (b FeatureBitmap) Has(feature FeatureBitmap) bool { return b&feature == feature }

type Protocol struct {
	Version  Version
	Features FeatureBitmap
}

// Context is the only state authority granted to block execution. Persistence
// and final commit remain owned by the caller/state manager.
type Context struct {
	Reader state.StateReader
	Writer state.StateWriter
}

type Activation struct {
	Height   uint64
	Protocol Protocol
}

// Schedule is immutable once built. Activations must be strictly increasing;
// an exact activation height therefore yields the same version on every node.
type Schedule struct{ activations []Activation }

func NewSchedule(activations ...Activation) (Schedule, error) {
	if len(activations) == 0 || activations[0].Height != 0 || activations[0].Protocol.Version == 0 {
		return Schedule{}, errors.New("execution: schedule must begin at height zero with a version")
	}
	copyActivations := append([]Activation(nil), activations...)
	for i := 1; i < len(copyActivations); i++ {
		if copyActivations[i].Height <= copyActivations[i-1].Height || copyActivations[i].Protocol.Version == 0 {
			return Schedule{}, errors.New("execution: activations must be strictly increasing")
		}
	}
	return Schedule{activations: copyActivations}, nil
}

func (s Schedule) At(height uint64) Protocol {
	if len(s.activations) == 0 {
		return Protocol{}
	}
	selected := s.activations[0].Protocol
	for i := 1; i < len(s.activations); i++ {
		if height < s.activations[i].Height {
			break
		}
		selected = s.activations[i].Protocol
	}
	return selected
}

// Commitment is the complete execution output consensus is allowed to vote on.
type Commitment struct {
	StateRoot       string
	ReceiptsRoot    string
	EventRoot       string
	ExecutionHash   string
	FeeRoot         string
	DTLStateRoot    string
	DTLReceiptsRoot string
}

func writeField(dst *[]byte, value string) {
	value = strings.ToLower(strings.TrimSpace(value))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	*dst = append(*dst, length[:]...)
	*dst = append(*dst, value...)
}

// Hash uses length-prefixed fields so different tuples cannot collide through
// delimiter ambiguity.
func (c Commitment) Hash() string {
	material := make([]byte, 0, 7*68)
	for _, value := range []string{c.StateRoot, c.ReceiptsRoot, c.EventRoot, c.ExecutionHash, c.FeeRoot, c.DTLStateRoot, c.DTLReceiptsRoot} {
		writeField(&material, value)
	}
	sum := sha256.Sum256(material)
	return hex.EncodeToString(sum[:])
}

type Executor[I, O any] interface {
	Execute(I) (O, error)
}
