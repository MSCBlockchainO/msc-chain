package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"
)

func TestRequiredSnapshotProofsUsesTwoThirdsQuorum(t *testing.T) {
	if got := RequiredSnapshotProofs(4); got != 3 {
		t.Fatalf("unexpected required proofs for 4 validators: got=%d want=3", got)
	}
	if got := RequiredSnapshotProofs(1); got != 1 {
		t.Fatalf("unexpected required proofs for 1 validator: got=%d want=1", got)
	}
}

func TestSnapshotManagerDiscoverCheckpointUsesCheckpointInterval(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	manager := NewSnapshotManager(n, 87, 51, false)
	if err := manager.DiscoverCheckpoint(); err != nil {
		t.Fatalf("discover checkpoint: %v", err)
	}
	if manager.CheckpointHeight != 64 {
		t.Fatalf("unexpected checkpoint height: got=%d want=64", manager.CheckpointHeight)
	}
}

func TestSnapshotRetryControllerTracksSessionFailures(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()

	n := makeStrictActivationNode(50)
	n.startSnapshotSession(87, "late_join")

	controller := SnapshotRetryController{Node: n}
	if !controller.RecordFailure("snapshot_anchor_unreachable") {
		t.Fatalf("expected retry controller to keep session active after first failure")
	}
	if controller.RetryCount() != 1 {
		t.Fatalf("unexpected retry count: got=%d want=1", controller.RetryCount())
	}
	if controller.Exhausted() {
		t.Fatalf("retry controller should not be exhausted after first failure")
	}
}

func TestSnapshotRetryControllerBackoffDelayUsesExponentialBackoff(t *testing.T) {
	node := &Node{}
	node.snapshotSessionMu.Lock()
	node.snapshotSession.Active = true
	node.snapshotSession.RetryCount = 3
	node.snapshotSessionMu.Unlock()

	controller := SnapshotRetryController{Node: node}
	if got := controller.BackoffDelay(5 * time.Second); got != 20*time.Second {
		t.Fatalf("unexpected exponential backoff: got=%s want=%s", got, 20*time.Second)
	}

	node.snapshotSessionMu.Lock()
	node.snapshotSession.RetryCount = 8
	node.snapshotSessionMu.Unlock()
	if got := controller.BackoffDelay(5 * time.Second); got != snapshotFailoverBackoffMax {
		t.Fatalf("expected capped backoff: got=%s want=%s", got, snapshotFailoverBackoffMax)
	}
}

func TestSnapshotVerifierRejectsStateMerkleMismatch(t *testing.T) {
	block, snapshot := makeSnapshotLayerFixture(32, "", NewLedger(), testValidatorSetMaterializationRegistry())
	_ = block
	snapshot.StateMerkleRoot = "bad-root"

	if err := (SnapshotVerifier{}).Verify(&snapshot); err == nil {
		t.Fatalf("expected snapshot verifier to reject bad state merkle root")
	}
}

func TestSignSnapshotProducesValidSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	root := []byte("state-root")
	sig := SignSnapshot(root, priv)
	if len(sig) != ed25519.SignatureSize {
		t.Fatalf("unexpected signature size: got=%d want=%d", len(sig), ed25519.SignatureSize)
	}
	if !ed25519.Verify(pub, root, sig) {
		t.Fatalf("expected signature to verify")
	}
}

func TestSnapshotManagerPersistSnapshotStoresVerifiedSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block, snapshot := makeSnapshotLayerFixture(8, "", NewLedger(), testValidatorSetMaterializationRegistry())
	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block}},
	}
	manager := NewSnapshotManager(n, snapshot.Height, snapshot.Height, false)
	manager.Snapshot = &snapshot

	if err := manager.PersistSnapshot("trusted_snapshot_download"); err != nil {
		t.Fatalf("persist snapshot: %v", err)
	}
	if !manager.Stored {
		t.Fatalf("expected snapshot manager to mark snapshot stored")
	}
	if manager.Meta == nil || manager.Meta.Height != snapshot.Height {
		t.Fatalf("expected persisted snapshot meta, got=%+v", manager.Meta)
	}
	stored, err := n.GetSnapshot(snapshot.Height)
	if err != nil {
		t.Fatalf("load stored snapshot: %v", err)
	}
	if stored == nil || stored.SnapshotHash != snapshot.SnapshotHash {
		t.Fatalf("unexpected stored snapshot: %+v", stored)
	}
}
