package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"

	syncpipeline "msc-chain/sync"
)

func connectSnapshotProofPeerForTest(t *testing.T, ctx context.Context, local peer.ID, localConnect func(context.Context, peer.AddrInfo) error, remoteHost peer.AddrInfo) {
	t.Helper()
	_ = local
	if err := localConnect(ctx, remoteHost); err != nil {
		t.Fatalf("failed to connect snapshot proof peer: %v", err)
	}
}

func TestRequestSnapshotMetaUsesProofQuorumWhenMetadataStreamUnavailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()

	remoteHosts := make([]peer.AddrInfo, 0, 3)
	for i := 0; i < 3; i++ {
		h, err := libp2p.New()
		if err != nil {
			t.Fatalf("failed to create remote host: %v", err)
		}
		defer h.Close()
		info := peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()}
		connectSnapshotProofPeerForTest(t, ctx, localHost.ID(), localHost.Connect, info)
		remoteHosts = append(remoteHosts, info)
	}

	n := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C"})
	n.Host = localHost
	n.peerStateMu.Lock()
	n.peerToValidator[remoteHosts[0].ID.String()] = "A"
	n.peerToValidator[remoteHosts[1].ID.String()] = "B"
	n.peerToValidator[remoteHosts[2].ID.String()] = "C"
	n.peerStateMu.Unlock()

	height := uint64(448)
	checkpoint := snapshotCheckpointHeightFor(height)
	proofs := []SnapshotProof{
		{
			Validator:             "A",
			Height:                height,
			CheckpointHeight:      checkpoint,
			SnapshotHash:          "snap-448",
			StateRoot:             "state-448",
			StateMerkleRoot:       "merkle-448",
			ValidatorSetHash:      "set-448",
			ValidatorRegistryHash: "registry-448",
			SignatureHex:          "aa",
		},
		{
			Validator:             "B",
			Height:                height,
			CheckpointHeight:      checkpoint,
			SnapshotHash:          "snap-448",
			StateRoot:             "state-448",
			StateMerkleRoot:       "merkle-448",
			ValidatorSetHash:      "set-448",
			ValidatorRegistryHash: "registry-448",
			SignatureHex:          "bb",
		},
		{
			Validator:             "C",
			Height:                height,
			CheckpointHeight:      checkpoint,
			SnapshotHash:          "snap-448",
			StateRoot:             "state-448",
			StateMerkleRoot:       "merkle-448",
			ValidatorSetHash:      "set-448",
			ValidatorRegistryHash: "registry-448",
			SignatureHex:          "cc",
		},
	}
	candidate := snapshotProofCandidateFromProof(&proofs[0])
	key := strictSnapshotMetaCandidateKey(candidate)
	n.snapshotProofMu.Lock()
	n.snapshotProofs = map[string]map[string]SnapshotProof{
		key: {
			"A": proofs[0],
			"B": proofs[1],
			"C": proofs[2],
		},
	}
	n.snapshotProofMu.Unlock()

	adapter := &nodeSnapshotSyncAdapter{
		node:      n,
		minHeight: 200,
	}
	meta, err := adapter.RequestSnapshotMeta(context.Background(), 200, height)
	if err != nil {
		t.Fatalf("RequestSnapshotMeta failed: %v", err)
	}
	if meta.Height != height {
		t.Fatalf("unexpected proof-backed meta height: got=%d want=%d", meta.Height, height)
	}
	if meta.SnapshotHash != "snap-448" || meta.StateRoot != "state-448" {
		t.Fatalf("unexpected proof-backed meta identity: %+v", meta)
	}
	if meta.CheckpointHeight != checkpoint {
		t.Fatalf("unexpected checkpoint: got=%d want=%d", meta.CheckpointHeight, checkpoint)
	}
	if len(meta.Providers) != 3 {
		t.Fatalf("expected three proof-backed providers, got=%d %+v", len(meta.Providers), meta.Providers)
	}
	wantRequired := RequiredSnapshotProofs(3)
	if adapter.requiredProofs != wantRequired {
		t.Fatalf("unexpected required proofs: got=%d want=%d", adapter.requiredProofs, wantRequired)
	}
}

func TestRequestSnapshotMetaUsesProofProviderCacheWithoutPeerMap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	localHost, err := libp2p.New()
	if err != nil {
		t.Fatalf("failed to create local host: %v", err)
	}
	defer localHost.Close()

	remoteHosts := make([]peer.AddrInfo, 0, 3)
	for i := 0; i < 3; i++ {
		h, err := libp2p.New()
		if err != nil {
			t.Fatalf("failed to create remote host: %v", err)
		}
		defer h.Close()
		info := peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()}
		connectSnapshotProofPeerForTest(t, ctx, localHost.ID(), localHost.Connect, info)
		remoteHosts = append(remoteHosts, info)
	}

	n := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C"})
	n.Host = localHost

	height := uint64(448)
	checkpoint := snapshotCheckpointHeightFor(height)
	proofs := []SnapshotProof{
		{
			Validator:             "A",
			Height:                height,
			CheckpointHeight:      checkpoint,
			SnapshotHash:          "snap-448",
			StateRoot:             "state-448",
			StateMerkleRoot:       "merkle-448",
			ValidatorSetHash:      "set-448",
			ValidatorRegistryHash: "registry-448",
			SignatureHex:          "aa",
		},
		{
			Validator:             "B",
			Height:                height,
			CheckpointHeight:      checkpoint,
			SnapshotHash:          "snap-448",
			StateRoot:             "state-448",
			StateMerkleRoot:       "merkle-448",
			ValidatorSetHash:      "set-448",
			ValidatorRegistryHash: "registry-448",
			SignatureHex:          "bb",
		},
		{
			Validator:             "C",
			Height:                height,
			CheckpointHeight:      checkpoint,
			SnapshotHash:          "snap-448",
			StateRoot:             "state-448",
			StateMerkleRoot:       "merkle-448",
			ValidatorSetHash:      "set-448",
			ValidatorRegistryHash: "registry-448",
			SignatureHex:          "cc",
		},
	}
	candidate := snapshotProofCandidateFromProof(&proofs[0])
	key := strictSnapshotMetaCandidateKey(candidate)
	n.snapshotProofMu.Lock()
	n.snapshotProofs = map[string]map[string]SnapshotProof{
		key: {
			"A": proofs[0],
			"B": proofs[1],
			"C": proofs[2],
		},
	}
	n.snapshotProofProviders = map[string]map[string]string{
		key: {
			"A": remoteHosts[0].ID.String(),
			"B": remoteHosts[1].ID.String(),
			"C": remoteHosts[2].ID.String(),
		},
	}
	n.snapshotProofMu.Unlock()

	adapter := &nodeSnapshotSyncAdapter{
		node:      n,
		minHeight: 200,
	}
	meta, err := adapter.RequestSnapshotMeta(context.Background(), 200, height)
	if err != nil {
		t.Fatalf("RequestSnapshotMeta failed: %v", err)
	}
	if meta.Height != height {
		t.Fatalf("unexpected proof-backed meta height: got=%d want=%d", meta.Height, height)
	}
	if len(meta.Providers) != 3 {
		t.Fatalf("expected proof providers without peer map, got=%d %+v", len(meta.Providers), meta.Providers)
	}
	for _, info := range remoteHosts {
		if adapter.selectedProviderValidators[info.ID.String()] == "" {
			t.Fatalf("missing selected provider validator for %s", info.ID.String())
		}
	}
}

func TestSnapshotPipelineVerifyProofPreservesSignedFields(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate validator key: %v", err)
	}
	validatorPubKeysMu.Lock()
	oldPub, hadOldPub := ValidatorPubKeys["A"]
	ValidatorPubKeys["A"] = append(ed25519.PublicKey(nil), pub...)
	validatorPubKeysMu.Unlock()
	t.Cleanup(func() {
		validatorPubKeysMu.Lock()
		if hadOldPub {
			ValidatorPubKeys["A"] = oldPub
		} else {
			delete(ValidatorPubKeys, "A")
		}
		validatorPubKeysMu.Unlock()
	})

	height := uint64(448)
	snapshot := &StateSnapshot{
		Version:               SnapshotVersion,
		Height:                height,
		StateRoot:             "state-448",
		StateMerkleRoot:       "merkle-448",
		ValidatorSetHash:      "set-448",
		ValidatorSetRoot:      "set-root-448",
		ValidatorRegistryHash: "registry-448",
		CheckpointHeight:      snapshotCheckpointHeightFor(height),
		CheckpointDomain:      syncSnapshotCheckpointDomain(),
	}
	n := &Node{
		ID: "A",
		ValidatorKey: ValidatorKey{
			ID:         "A",
			PublicKey:  append(ed25519.PublicKey(nil), pub...),
			PrivateKey: append(ed25519.PrivateKey(nil), priv...),
		},
	}
	n.attachSnapshotCheckpointProof(snapshot)
	proof := snapshotProofFromSnapshot("A", snapshot)
	sig, err := hex.DecodeString(proof.SignatureHex)
	if err != nil {
		t.Fatalf("decode proof signature: %v", err)
	}

	adapter := &nodeSnapshotSyncAdapter{node: n}
	ok := adapter.VerifyProof(context.Background(), syncpipeline.SnapshotProof{
		Height:                proof.Height,
		CheckpointHeight:      proof.CheckpointHeight,
		BlockHash:             proof.BlockHash,
		SnapshotHash:          proof.SnapshotHash,
		StateRoot:             proof.StateRoot,
		StateMerkleRoot:       proof.StateMerkleRoot,
		LedgerHash:            proof.LedgerHash,
		ValidatorSetHash:      proof.ValidatorSetHash,
		ValidatorSetRoot:      proof.ValidatorSetRoot,
		ValidatorRegistryHash: proof.ValidatorRegistryHash,
		CheckpointDomain:      proof.CheckpointDomain,
		Validator:             proof.Validator,
		Signature:             sig,
	})
	if !ok {
		t.Fatalf("expected sync-pipeline proof to verify with preserved signed fields")
	}
}
