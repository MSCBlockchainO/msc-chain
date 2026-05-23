package main

import (
	crand "crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

func signedPeerHelloForTest(t *testing.T) (string, PeerHello) {
	t.Helper()
	priv, pub, err := libp2pcrypto.GenerateEd25519Key(crand.Reader)
	if err != nil {
		t.Fatalf("generate libp2p key: %v", err)
	}
	pid, err := peer.IDFromPublicKey(pub)
	if err != nil {
		t.Fatalf("peer id: %v", err)
	}
	hello := peerHelloIdentityTestPayload("/ip4/127.0.0.1/tcp/7001/p2p/" + pid.String())
	hello.Timestamp = time.Now().Unix()
	hello.Nonce = peerHelloNonce()
	sig, err := priv.Sign(peerHelloSignBytes(hello))
	if err != nil {
		t.Fatalf("sign hello: %v", err)
	}
	hello.SignatureHex = hex.EncodeToString(sig)
	return pid.String(), hello
}

func TestValidatePeerHelloAcceptsSignedHandshake(t *testing.T) {
	peerID, hello := signedPeerHelloForTest(t)
	n := &Node{}
	n.ensurePeerIsolationMaps()

	if !n.validatePeerHello(peerID, hello) {
		t.Fatalf("expected signed peer hello to validate")
	}
	if !n.isPeerHelloOK(peerID) {
		t.Fatalf("expected signed peer hello to mark peer verified")
	}
}

func TestValidatePeerHelloRejectsBadSignature(t *testing.T) {
	peerID, hello := signedPeerHelloForTest(t)
	hello.Nonce = "tampered-after-sign"
	n := &Node{}
	n.ensurePeerIsolationMaps()

	if n.validatePeerHello(peerID, hello) {
		t.Fatalf("expected tampered signed peer hello to be rejected")
	}
	if !n.isPeerQuarantined(peerID) {
		t.Fatalf("expected bad signature peer to be quarantined")
	}
}

func TestValidatePeerHelloRejectsReplayNonce(t *testing.T) {
	peerID, hello := signedPeerHelloForTest(t)
	n := &Node{}
	n.ensurePeerIsolationMaps()

	if !n.validatePeerHello(peerID, hello) {
		t.Fatalf("expected first signed peer hello to validate")
	}
	if n.validatePeerHello(peerID, hello) {
		t.Fatalf("expected replayed peer hello nonce to be rejected")
	}
}

func TestPeerReputationPersistenceAdmissionBlocksLowScore(t *testing.T) {
	peerID := "peer-low-score"
	n := &Node{
		DataDir:         t.TempDir(),
		syncPeerScores:  make(map[string]*SyncPeerScore),
		quarantineUntil: make(map[string]time.Time),
	}
	n.syncPeerScores[peerID] = &SyncPeerScore{DialFailure: PeerAdmissionMinSamples, UpdatedAt: time.Now()}
	n.savePeerReputation()

	loaded := &Node{DataDir: n.DataDir}
	loaded.ensurePeerIsolationMaps()
	loaded.loadPeerReputation()

	if loaded.peerAdmissionAllowed(peerID) {
		t.Fatalf("expected persisted low-reputation peer to be blocked")
	}
	if !loaded.isPeerQuarantined(peerID) {
		t.Fatalf("expected low-reputation peer to be quarantined")
	}
}
