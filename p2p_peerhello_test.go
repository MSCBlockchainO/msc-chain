package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

func TestDecodePeerHelloPayloadRaw(t *testing.T) {
	in := PeerHello{
		ChainID:       "91938",
		GenesisHash:   "abc",
		Version:       "v1",
		ConsensusHash: "hash",
		ValidatorID:   "A",
		P2PAddr:       "/ip4/127.0.0.1/tcp/7001/p2p/peerA",
		Height:        10,
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal raw hello: %v", err)
	}

	got, err := decodePeerHelloPayload(raw)
	if err != nil {
		t.Fatalf("decode raw hello failed: %v", err)
	}
	if got.ChainID != in.ChainID || got.ValidatorID != in.ValidatorID {
		t.Fatalf("decoded hello mismatch: got=%+v want=%+v", got, in)
	}
}

func TestOnPeerDisconnectedDebouncesValidatorOffline(t *testing.T) {
	pid := peer.ID("peerA")
	n := &Node{
		peerToValidator:       map[string]string{pid.String(): "A"},
		peerSuspectAt:         make(map[string]time.Time),
		peerHelloOK:           make(map[string]bool),
		peerAckHeight:         make(map[string]uint64),
		peerTipHash:           make(map[string]string),
		peerFlapTimes:         make(map[string][]time.Time),
		validatorSuspect:      make(map[string]time.Time),
		validatorOfflineSince: make(map[string]time.Time),
		validatorStatus: map[string]*ValidatorStatus{
			"A": {
				Active:              true,
				Enabled:             true,
				ConsensusReadyKnown: true,
				LastSeen:            time.Now(),
			},
		},
	}

	n.onPeerDisconnected(pid)

	if st := n.validatorStatus["A"]; st == nil || !st.Active {
		t.Fatalf("transient peer disconnect should not immediately mark validator offline: %+v", st)
	}
	n.validatorMu.RLock()
	if _, ok := n.validatorSuspect["A"]; !ok {
		n.validatorMu.RUnlock()
		t.Fatalf("expected validator to be marked suspect")
	}
	n.validatorMu.RUnlock()
	if _, ok := n.peerSuspectAt[pid.String()]; !ok {
		t.Fatalf("expected peer to enter suspect debounce window")
	}
	if _, offline := n.validatorOfflineSince["A"]; offline {
		t.Fatalf("validator should not enter offline state until suspect timeout expires")
	}

	n.applyPeerInfo(pid.String(), PeerHello{Role: "validator", ValidatorID: "A"})
	n.validatorMu.RLock()
	_, stillSuspect := n.validatorSuspect["A"]
	n.validatorMu.RUnlock()
	if stillSuspect {
		t.Fatalf("validated peer info should clear validator suspect state")
	}
}

func TestDecodePeerHelloPayloadWrappedMessage(t *testing.T) {
	in := PeerHello{
		ChainID:       "91938",
		GenesisHash:   "abc",
		Version:       "v1",
		ConsensusHash: "hash",
		ValidatorID:   "B",
		P2PAddr:       "/ip4/127.0.0.1/tcp/7002/p2p/peerB",
		Height:        11,
	}
	msg := Message{
		Type: MsgPeerHello,
		Data: MustJSON(in),
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal wrapped hello: %v", err)
	}

	got, err := decodePeerHelloPayload(raw)
	if err != nil {
		t.Fatalf("decode wrapped hello failed: %v", err)
	}
	if got.ChainID != in.ChainID || got.ValidatorID != in.ValidatorID {
		t.Fatalf("decoded wrapped hello mismatch: got=%+v want=%+v", got, in)
	}
}

func peerHelloIdentityTestPayload(p2pAddr string) PeerHello {
	return PeerHello{
		ChainID:       ChainID,
		GenesisHash:   GenesisHash,
		Version:       Version,
		ConsensusHash: consensusParamsHash(),
		Role:          "full",
		P2PAddr:       p2pAddr,
		Height:        1,
	}
}

func TestValidatePeerHelloRejectsAdvertisedPeerIDMismatch(t *testing.T) {
	remotePeerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	advertisedPeerID := "12D3KooWRgzavLAH2MjsQc6H7ku4oyKexEBWNPpQggV1wyj8uYLW"
	node := &Node{}
	node.ensurePeerIsolationMaps()

	hello := peerHelloIdentityTestPayload("/ip4/127.0.0.1/tcp/7002/p2p/" + advertisedPeerID)
	if node.validatePeerHello(remotePeerID, hello) {
		t.Fatal("peer hello with mismatched advertised /p2p/ identity must be rejected")
	}
	if !node.isPeerQuarantined(remotePeerID) {
		t.Fatal("identity-spoofing peer should be quarantined")
	}
	if node.isPeerHelloOK(remotePeerID) {
		t.Fatal("identity-spoofing peer must not be marked hello-ok")
	}
}

func TestValidatePeerHelloAcceptsMatchingAdvertisedPeerID(t *testing.T) {
	remotePeerID, hello := signedPeerHelloForTest(t)
	node := &Node{}
	node.ensurePeerIsolationMaps()

	if !node.validatePeerHello(remotePeerID, hello) {
		t.Fatal("matching peer hello should be accepted")
	}
	if !node.isPeerHelloOK(remotePeerID) {
		t.Fatal("matching peer hello should mark peer hello-ok")
	}
	if node.isPeerQuarantined(remotePeerID) {
		t.Fatal("matching peer hello must not quarantine peer")
	}
}

func TestOutboundPeerHelloPreValidationRedactsSensitiveFields(t *testing.T) {
	node := &Node{}

	hello := node.outboundPeerHelloPreValidation()

	if hello.ChainID != ChainID || hello.GenesisHash != GenesisHash || hello.Version != Version || hello.ConsensusHash != consensusParamsHash() {
		t.Fatalf("pre-validation hello missing compatibility fields: %+v", hello)
	}
	if peerHelloHasPostValidationFields(hello) {
		t.Fatalf("pre-validation hello leaked post-validation fields: %+v", hello)
	}
	if !node.validatePeerHelloEnvelope("peerA", hello) {
		t.Fatal("pre-validation hello envelope should validate compatibility fields")
	}
	if node.isPeerHelloOK("peerA") {
		t.Fatal("envelope validation must not mark peer hello-ok")
	}
}

func TestValidatePeerHelloEnvelopeDoesNotReserveIdentity(t *testing.T) {
	remotePeerID, hello := signedPeerHelloForTest(t)
	node := &Node{}
	node.ensurePeerIsolationMaps()

	if !node.validatePeerHelloEnvelope(remotePeerID, hello) {
		t.Fatal("expected signed hello envelope to validate")
	}
	if node.isPeerHelloOK(remotePeerID) {
		t.Fatal("envelope validation must not mark peer hello-ok")
	}
	if got := node.validatorToPeer[hello.ValidatorID]; got != "" {
		t.Fatalf("envelope validation must not reserve validator identity, got peer %q", got)
	}

	if !node.validatePeerHello(remotePeerID, hello) {
		t.Fatal("full peer hello should validate after envelope-only check")
	}
	if !node.isPeerHelloOK(remotePeerID) {
		t.Fatal("full peer hello should mark peer hello-ok")
	}
}

func TestReserveValidatorPeerIdentityRejectsLocalNodeIDClone(t *testing.T) {
	remotePeerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	node := &Node{ID: "A"}
	node.ensurePeerIsolationMaps()

	if node.reserveValidatorPeerIdentity(remotePeerID, "A", "/ip4/127.0.0.1/tcp/7002/p2p/"+remotePeerID) {
		t.Fatal("remote peer must not be allowed to claim the local validator/node id")
	}
	if !node.isPeerQuarantined(remotePeerID) {
		t.Fatal("duplicate node id peer should be quarantined")
	}
	if got := node.validatorToPeer["A"]; got != "" {
		t.Fatalf("duplicate node id must not reserve validator mapping, got=%s", got)
	}
}

func TestReserveNodePeerIdentityRejectsLocalNodeIDClone(t *testing.T) {
	remotePeerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	node := &Node{ID: "A"}
	node.ensurePeerIsolationMaps()

	if node.reserveNodePeerIdentity(remotePeerID, "A") {
		t.Fatal("remote peer must not be allowed to claim the local node id")
	}
	if !node.isPeerQuarantined(remotePeerID) {
		t.Fatal("duplicate node id peer should be quarantined")
	}
	if got := node.nodeIDToPeer["A"]; got != "" {
		t.Fatalf("duplicate node id must not reserve node mapping, got=%s", got)
	}
}

func TestReserveNodePeerIdentityRejectsLiveDuplicate(t *testing.T) {
	oldPeerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	newPeerID := "12D3KooWRgzavLAH2MjsQc6H7ku4oyKexEBWNPpQggV1wyj8uYLW"
	node := &Node{ID: "A"}
	node.ensurePeerIsolationMaps()

	if !node.reserveNodePeerIdentity(oldPeerID, "F") {
		t.Fatal("first node id mapping should be accepted")
	}
	if node.reserveNodePeerIdentity(newPeerID, "F") {
		t.Fatal("same node id from a different peer id must be rejected")
	}
	if got := node.nodeIDToPeer["F"]; got != oldPeerID {
		t.Fatalf("node id mapping changed after duplicate: got=%s want=%s", got, oldPeerID)
	}
	if !node.isPeerQuarantined(newPeerID) {
		t.Fatal("conflicting node id peer should be quarantined")
	}
}

func TestValidatePeerHelloRejectsDuplicateNodeID(t *testing.T) {
	firstPeerID, firstHello := signedPeerHelloForTest(t)
	firstHello.NodeID = "F"
	secondPeerID, secondHello := signedPeerHelloForTest(t)
	secondHello.NodeID = "F"
	node := &Node{ID: "A"}
	node.ensurePeerIsolationMaps()

	if !node.validatePeerHello(firstPeerID, firstHello) {
		t.Fatal("first node id claim should be accepted")
	}
	if node.validatePeerHello(secondPeerID, secondHello) {
		t.Fatal("duplicate node id claim from another peer should be rejected")
	}
	if got := node.nodeIDToPeer["F"]; got != firstPeerID {
		t.Fatalf("node id mapping changed after duplicate hello: got=%s want=%s", got, firstPeerID)
	}
	if !node.isPeerQuarantined(secondPeerID) {
		t.Fatal("duplicate peer hello should quarantine the second peer")
	}
}

func TestClearPeerStateReleasesNodeIDMapping(t *testing.T) {
	peerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	node := &Node{ID: "A"}
	node.ensurePeerIsolationMaps()

	if !node.reserveNodePeerIdentity(peerID, "F") {
		t.Fatal("node id mapping should be accepted")
	}
	node.clearPeerState(peerID)
	if got := node.nodeIDToPeer["F"]; got != "" {
		t.Fatalf("clearPeerState should release node id mapping, got=%s", got)
	}
}

func TestClearPeerStatePreservesPeerHelloNonceUntilTTL(t *testing.T) {
	peerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	node := &Node{ID: "A"}
	node.ensurePeerIsolationMaps()
	node.peerStateMu.Lock()
	node.peerHelloNonces[peerID+"|nonce-1"] = time.Now()
	node.peerHelloNonces["other-peer|nonce-2"] = time.Now()
	node.peerStateMu.Unlock()

	node.clearPeerState(peerID)

	node.peerStateMu.Lock()
	_, stale := node.peerHelloNonces[peerID+"|nonce-1"]
	_, other := node.peerHelloNonces["other-peer|nonce-2"]
	node.peerStateMu.Unlock()
	if !stale {
		t.Fatal("clearPeerState must not clear nonce tombstones before TTL expiry")
	}
	if !other {
		t.Fatal("clearPeerState should not remove nonce entries for other peers")
	}
}

func TestPeerHelloNonceReplayRejectedAfterRestart(t *testing.T) {
	dataDir := t.TempDir()
	peerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	hello := PeerHello{Nonce: "nonce-replay"}

	first := &Node{ID: "A", DataDir: dataDir}
	first.ensurePeerIsolationMaps()
	if !first.acceptPeerHelloNonce(peerID, hello) {
		t.Fatal("first peer hello nonce should be accepted")
	}

	restarted := &Node{ID: "A", DataDir: dataDir}
	restarted.ensurePeerIsolationMaps()
	restarted.loadPeerHelloNonces()
	if restarted.acceptPeerHelloNonce(peerID, hello) {
		t.Fatal("persisted nonce tombstone should reject replay after restart")
	}
}

func TestSweepPeerHelloNoncesPrunesExpiredEntries(t *testing.T) {
	node := &Node{ID: "A", DataDir: t.TempDir()}
	node.ensurePeerIsolationMaps()
	now := time.Now()
	node.peerStateMu.Lock()
	node.peerHelloNonces["peer-a|old"] = now.Add(-peerHelloNonceTTL - time.Second)
	node.peerHelloNonces["peer-a|fresh"] = now
	node.peerStateMu.Unlock()

	if !node.sweepPeerHelloNonces(now) {
		t.Fatal("expected sweep to prune expired nonce")
	}

	node.peerStateMu.Lock()
	_, oldExists := node.peerHelloNonces["peer-a|old"]
	_, freshExists := node.peerHelloNonces["peer-a|fresh"]
	node.peerStateMu.Unlock()
	if oldExists {
		t.Fatal("expired nonce should be removed")
	}
	if !freshExists {
		t.Fatal("fresh nonce should remain")
	}
	raw, err := os.ReadFile(node.peerHelloNonceStorePath())
	if err != nil {
		t.Fatalf("read nonce store: %v", err)
	}
	if strings.Contains(string(raw), "peer-a|old") {
		t.Fatal("expired nonce should be removed from persisted store")
	}
}

func TestValidatorStatusSnapshotCopiesUnderLock(t *testing.T) {
	node := &Node{ID: "A"}
	node.validatorStatus = map[string]*ValidatorStatus{
		"B": {
			ReportedHeight:   10,
			FinalizedHeight:  9,
			ValidatorSetHash: "hash-1",
			LastSeen:         time.Now(),
		},
	}

	snapshot, ok := node.validatorStatusSnapshot("B")
	if !ok {
		t.Fatal("expected validator status snapshot")
	}
	node.validatorMu.Lock()
	node.validatorStatus["B"].ValidatorSetHash = "hash-2"
	node.validatorStatus["B"].ReportedHeight = 11
	node.validatorMu.Unlock()

	if snapshot.ValidatorSetHash != "hash-1" || snapshot.ReportedHeight != 10 {
		t.Fatalf("snapshot should be a stable copy, got hash=%s height=%d", snapshot.ValidatorSetHash, snapshot.ReportedHeight)
	}
}

func TestReserveValidatorPeerIdentityRejectsAddressBookConflict(t *testing.T) {
	oldPeerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	newPeerID := "12D3KooWRgzavLAH2MjsQc6H7ku4oyKexEBWNPpQggV1wyj8uYLW"
	oldAddrBook := map[string]string{}
	ValidatorAddrBook.mu.Lock()
	for id, addr := range ValidatorAddrBook.m {
		oldAddrBook[id] = addr
	}
	ValidatorAddrBook.m = map[string]string{
		"B": "/ip4/127.0.0.1/tcp/7002/p2p/" + oldPeerID,
	}
	ValidatorAddrBook.mu.Unlock()
	defer func() {
		ValidatorAddrBook.mu.Lock()
		ValidatorAddrBook.m = oldAddrBook
		ValidatorAddrBook.mu.Unlock()
	}()

	node := &Node{ID: "A"}
	node.ensurePeerIsolationMaps()
	if node.reserveValidatorPeerIdentity(newPeerID, "B", "/ip4/127.0.0.1/tcp/7003/p2p/"+newPeerID) {
		t.Fatal("same validator id with a different peer id must be rejected")
	}
	if !node.isPeerQuarantined(newPeerID) {
		t.Fatal("conflicting validator id peer should be quarantined")
	}
}

func TestReserveValidatorPeerIdentityAllowsSameValidatorSamePeer(t *testing.T) {
	peerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	oldAddrBook := map[string]string{}
	ValidatorAddrBook.mu.Lock()
	for id, addr := range ValidatorAddrBook.m {
		oldAddrBook[id] = addr
	}
	ValidatorAddrBook.m = map[string]string{}
	ValidatorAddrBook.mu.Unlock()
	defer func() {
		ValidatorAddrBook.mu.Lock()
		ValidatorAddrBook.m = oldAddrBook
		ValidatorAddrBook.mu.Unlock()
	}()

	node := &Node{ID: "A"}
	node.ensurePeerIsolationMaps()

	if !node.reserveValidatorPeerIdentity(peerID, "B", "/ip4/127.0.0.1/tcp/7002/p2p/"+peerID) {
		t.Fatal("first validator peer mapping should be accepted")
	}
	if !node.reserveValidatorPeerIdentity(peerID, "B", "/ip4/127.0.0.1/tcp/7002/p2p/"+peerID) {
		t.Fatal("same validator id from same peer should remain accepted")
	}
	if got := node.validatorToPeer["B"]; got != peerID {
		t.Fatalf("validator mapping mismatch: got=%s want=%s", got, peerID)
	}
}

func TestValidatorSetHashCanonical(t *testing.T) {
	a := []string{"C", "A", "B", "A", " "}
	b := []string{"B", "C", "A"}

	ha := ValidatorSetHash(a)
	hb := ValidatorSetHash(b)
	if ha == "" || hb == "" {
		t.Fatalf("validator set hash should not be empty: ha=%q hb=%q", ha, hb)
	}
	if ha != hb {
		t.Fatalf("canonical hash mismatch: ha=%s hb=%s", ha, hb)
	}
}

func TestValidatorMeshTargetsActiveOnly(t *testing.T) {
	prevCore := append([]string{}, ConfigAuthCoreValidators...)
	defer func() { ConfigAuthCoreValidators = prevCore }()
	ConfigAuthCoreValidators = []string{"A", "B", "C", "D", "E"}

	oldAddrBook := map[string]string{}
	ValidatorAddrBook.mu.Lock()
	for id, addr := range ValidatorAddrBook.m {
		oldAddrBook[id] = addr
	}
	ValidatorAddrBook.m = map[string]string{
		"A": "/ip4/127.0.0.1/tcp/7001/p2p/12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo",
		"B": "/ip4/127.0.0.1/tcp/7002/p2p/12D3KooWRgzavLAH2MjsQc6H7ku4oyKexEBWNPpQggV1wyj8uYLW",
		"C": "/ip4/127.0.0.1/tcp/7003/p2p/12D3KooWHn73E83TtSc2D46aRFUWgYBg2QBqXMBGsgCkdxtBgAof",
		"D": "/ip4/127.0.0.1/tcp/7004/p2p/12D3KooWKnasaTuzD7kuwcmReBAwRK34Nj8XjiURC2vbQ9KEyogq",
		"E": "/ip4/127.0.0.1/tcp/7009/p2p/12D3KooWHCbU8v5NbPDg97P2LtqvAPoXZ8geYiazDCeh7zfoPg99",
		"X": "/ip4/127.0.0.1/tcp/7010/p2p/12D3KooWMyWdH42Hzy8mBDvzeuYhoJaMItSub8Jwjx9v53YJQfE5",
	}
	ValidatorAddrBook.mu.Unlock()
	defer func() {
		ValidatorAddrBook.mu.Lock()
		ValidatorAddrBook.m = oldAddrBook
		ValidatorAddrBook.mu.Unlock()
	}()

	n := &Node{
		ID: "A",
		Config: &NodeConfig{
			PersistentPeers: []string{ValidatorAddrBook.m["E"]},
			Seeds:           []string{ValidatorAddrBook.m["X"]},
		},
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 100}},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			101: {"A", "B", "C", "D"},
		},
	}

	got := n.validatorMeshTargets()
	want := []string{
		ValidatorAddrBook.m["B"],
		ValidatorAddrBook.m["C"],
		ValidatorAddrBook.m["D"],
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("active-only mesh targets mismatch: got=%v want=%v", got, want)
	}
}

func TestValidatorMeshReconcileIntervalMainnet(t *testing.T) {
	if got := validatorMeshReconcileInterval(); got != 8*time.Second {
		t.Fatalf("mesh reconcile interval mismatch: got=%s want=8s", got)
	}
	if mode := validatorMeshMode(); mode != "active_only" {
		t.Fatalf("mesh mode mismatch: got=%q want=active_only", mode)
	}
}

func TestShouldSyncForValidatorSetMismatch(t *testing.T) {
	if shouldSyncForValidatorSetMismatch(224, 219) {
		t.Fatalf("stale peer mismatch should not trigger sync")
	}
	if !shouldSyncForValidatorSetMismatch(224, 224) {
		t.Fatalf("same-height mismatch should trigger sync")
	}
	if !shouldSyncForValidatorSetMismatch(224, 225) {
		t.Fatalf("higher peer mismatch should trigger sync")
	}
	if shouldSyncForValidatorSetMismatch(224, 0) {
		t.Fatalf("zero-height mismatch should not trigger sync")
	}
}

func TestShouldForceSnapshotResyncForValidatorSetMismatch(t *testing.T) {
	prevNearTip := ValidatorSetAutohealNearTipForceAfter
	defer func() { ValidatorSetAutohealNearTipForceAfter = prevNearTip }()

	if shouldForceSnapshotResyncForValidatorSetMismatch(142, 0) {
		t.Fatalf("zero target should not force snapshot resync")
	}
	if shouldForceSnapshotResyncForValidatorSetMismatch(142, 142) {
		t.Fatalf("same-height mismatch should not force snapshot resync")
	}

	ValidatorSetAutohealNearTipForceAfter = 2
	if !shouldForceSnapshotResyncForValidatorSetMismatch(142, 143) {
		t.Fatalf("near-tip mismatch should force snapshot resync when near-tip autoheal is enabled")
	}
	ValidatorSetAutohealNearTipForceAfter = 0
	if !shouldForceSnapshotResyncForValidatorSetMismatch(142, 143) {
		t.Fatalf("snapshot-first mismatch recovery should force snapshot resync when local is behind")
	}
	if !shouldForceSnapshotResyncForValidatorSetMismatch(142, 145) {
		t.Fatalf("behind mismatch should force snapshot resync for medium gaps")
	}
	if !shouldForceSnapshotResyncForValidatorSetMismatch(142, 207) {
		t.Fatalf("larger gap mismatch should force snapshot resync")
	}
}

func TestRefreshPeerIDMismatchClearsReplacementRetryState(t *testing.T) {
	oldPeerID := "12D3KooWSjgBtznLkWFcuKkib3o4GAxxREFUPrRtcYqwTAUruXJo"
	newPeerID := "12D3KooWRgzavLAH2MjsQc6H7ku4oyKexEBWNPpQggV1wyj8uYLW"
	oldAddr := "/ip4/127.0.0.1/tcp/7004/p2p/" + oldPeerID

	n := &Node{
		ID: "A",
		Config: &NodeConfig{
			PersistentPeers: []string{oldAddr},
			Seeds:           []string{oldAddr},
		},
		peerDialFailures: map[string]int{
			newPeerID: 2,
		},
		peerDialNext: map[string]time.Time{
			oldPeerID: time.Now().Add(30 * time.Second),
			newPeerID: time.Now().Add(30 * time.Second),
		},
		quarantineUntil: map[string]time.Time{
			newPeerID: time.Now().Add(30 * time.Second),
		},
		connectingPeers: map[string]bool{
			newPeerID: true,
		},
		allowedPeerIDs: map[string]bool{},
	}

	err := fmt.Errorf("peer id mismatch: expected %s but remote key matches %s", oldPeerID, newPeerID)
	if !n.refreshPeerIDMismatch(oldAddr, oldPeerID, err) {
		t.Fatalf("expected peer-id mismatch refresh to succeed")
	}

	if !n.allowedPeerIDs[newPeerID] {
		t.Fatalf("expected refreshed peer id to be allowed")
	}
	if !n.canDialPeerID(newPeerID) {
		t.Fatalf("expected refreshed peer id dial backoff/quarantine to be cleared")
	}
	if _, ok := n.peerDialFailures[newPeerID]; ok {
		t.Fatalf("expected refreshed peer id dial failures cleared")
	}
	if _, ok := n.peerDialNext[newPeerID]; ok {
		t.Fatalf("expected refreshed peer id dial backoff cleared")
	}
	if _, ok := n.quarantineUntil[newPeerID]; ok {
		t.Fatalf("expected refreshed peer id quarantine cleared")
	}
	if n.connectingPeers[newPeerID] {
		t.Fatalf("expected refreshed peer id connecting state cleared")
	}

	for _, addr := range n.Config.PersistentPeers {
		if strings.Contains(addr, oldPeerID) {
			t.Fatalf("expected stale persistent peer id removed, peers=%v", n.Config.PersistentPeers)
		}
	}
	for _, addr := range n.Config.Seeds {
		if strings.Contains(addr, oldPeerID) {
			t.Fatalf("expected stale seed peer id removed, seeds=%v", n.Config.Seeds)
		}
	}
	foundNew := false
	for _, addr := range n.Config.PersistentPeers {
		if strings.Contains(addr, newPeerID) {
			foundNew = true
			break
		}
	}
	if !foundNew {
		t.Fatalf("expected refreshed peer id persisted in peer lists, peers=%v", n.Config.PersistentPeers)
	}
}

func TestLoadBlockFallsBackToMemory(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{ID: 219},
				{ID: 220, BlockHash: "h220"},
			},
		},
	}

	b, ok := n.LoadBlock(220)
	if !ok {
		t.Fatalf("expected in-memory fallback block")
	}
	if b.ID != 220 || b.BlockHash != "h220" {
		t.Fatalf("unexpected block from fallback: %+v", b)
	}
}

func TestClassifyPeerDriftMatrix(t *testing.T) {
	if got := classifyPeerDrift(9, 9, 10, 10); got != PeerDriftClassStale {
		t.Fatalf("peer<local should be stale, got=%s", got)
	}
	if got := classifyPeerDrift(10, 10, 10, 10); got != PeerDriftClassDangerous {
		t.Fatalf("same-height+same-finalized should be dangerous, got=%s", got)
	}
	if got := classifyPeerDrift(11, 11, 10, 10); got != PeerDriftClassAhead {
		t.Fatalf("peer>local should be ahead, got=%s", got)
	}
	if got := classifyPeerDrift(10, 9, 10, 10); got != PeerDriftClassStale {
		t.Fatalf("same-height+lower-finalized should be stale, got=%s", got)
	}
	if got := classifyPeerDrift(10, 11, 10, 10); got != PeerDriftClassAhead {
		t.Fatalf("same-height+higher-finalized should be ahead, got=%s", got)
	}
}

func TestSyncOnlyAllowlist(t *testing.T) {
	allowed := []string{MsgPeerHello, MsgGetBlocks, MsgBlockAck, MsgPing, MsgPong}
	for _, typ := range allowed {
		if !isSyncOnlyAllowedMsgType(typ) {
			t.Fatalf("expected allowed message type: %s", typ)
		}
	}
	dropped := []string{MsgLeaderBlock, MsgExecutionResult, MsgCommit, MsgFinalBlock, MsgPeers, MsgTx}
	for _, typ := range dropped {
		if isSyncOnlyAllowedMsgType(typ) {
			t.Fatalf("expected dropped message type: %s", typ)
		}
	}
}

func TestRecordFinalizedDriftCounterSemantics(t *testing.T) {
	n := &Node{
		peerDriftState: make(map[string]PeerDriftState),
	}
	s1 := n.recordFinalizedDrift("peer1", 100, 101, "expA", "gotA", 1)
	if s1.Count != 1 {
		t.Fatalf("expected first count=1, got=%d", s1.Count)
	}
	s2 := n.recordFinalizedDrift("peer1", 100, 101, "expA", "gotA", 1)
	if s2.Count != 2 {
		t.Fatalf("expected same tuple count increment to 2, got=%d", s2.Count)
	}

	s3 := n.recordFinalizedDrift("peer1", 100, 101, "expB", "gotB", 1)
	if s3.Count != 1 {
		t.Fatalf("expected tuple change reset to 1, got=%d", s3.Count)
	}

	key := driftTupleKey("peer1", "expA", "gotA")
	n.peerStateMu.Lock()
	st := n.peerDriftState[key]
	st.LastSeen = time.Now().Add(-finalizedDriftWindow - time.Second)
	n.peerDriftState[key] = st
	n.peerStateMu.Unlock()
	s4 := n.recordFinalizedDrift("peer1", 100, 101, "expA", "gotA", 1)
	if s4.Count != 1 {
		t.Fatalf("expected window reset to count=1, got=%d", s4.Count)
	}

	oldKey := driftTupleKey("peer2", "expX", "gotX")
	n.peerStateMu.Lock()
	n.peerDriftState[oldKey] = PeerDriftState{Count: 9, LastSeen: time.Now().Add(-2*finalizedDriftWindow - time.Minute)}
	n.peerStateMu.Unlock()
	_ = n.recordFinalizedDrift("peer1", 102, 103, "expA", "gotA", 1)
	n.peerStateMu.Lock()
	_, exists := n.peerDriftState[oldKey]
	n.peerStateMu.Unlock()
	if exists {
		t.Fatalf("expected stale drift tuple entry to be garbage-collected")
	}
}

func TestFinalizedDriftPolicyActions(t *testing.T) {
	n := &Node{
		ID:                      "A",
		Blockchain:              &Blockchain{Blocks: []Block{{ID: 100}}},
		peerAckHeight:           map[string]uint64{"peer1": 100},
		peerToValidator:         map[string]string{"peer1": "V1", "peer2": "V2"},
		peerDriftState:          make(map[string]PeerDriftState),
		peerSyncOnlyUntil:       make(map[string]time.Time),
		peerSyncOnlyClass:       make(map[string]string),
		peerSyncOnlyLastDropLog: make(map[string]time.Time),
		quarantineUntil:         make(map[string]time.Time),
		peerDialFailures:        make(map[string]int),
		peerDialNext:            make(map[string]time.Time),
		validatorStatus: map[string]*ValidatorStatus{
			"V1": {ReportedHeight: 100, FinalizedHeight: 100, LastSeen: time.Now()},
			"V2": {ReportedHeight: 90, FinalizedHeight: 90, LastSeen: time.Now()},
		},
	}

	// <= threshold: no sync-only action.
	n.applyFinalizedDriftPolicy("peer1", PeerDriftState{Count: finalizedDriftThreshold, From: 95, To: 100})
	if n.isPeerSyncOnly("peer1") {
		t.Fatalf("expected no sync-only at threshold")
	}

	// Stale > threshold: enters sync-only.
	n.peerStateMu.Lock()
	n.peerAckHeight["peer2"] = 90
	n.peerStateMu.Unlock()
	n.applyFinalizedDriftPolicy("peer2", PeerDriftState{Count: finalizedDriftThreshold + 1, From: 90, To: 100, Expected: "e", Got: ""})
	if !n.isPeerSyncOnly("peer2") {
		t.Fatalf("expected stale peer to enter sync-only")
	}

	// Dangerous > threshold:
	// first call triggers recompute window marker (no disconnect),
	// second call in cooldown disconnects/quarantines.
	state := PeerDriftState{Count: finalizedDriftThreshold + 1, From: 95, To: 100, Expected: "e", Got: ""}
	n.applyFinalizedDriftPolicy("peer1", state)
	if _, ok := n.quarantineUntil["peer1"]; ok {
		t.Fatalf("unexpected quarantine on first dangerous drift policy pass")
	}
	n.applyFinalizedDriftPolicy("peer1", state)
	if _, ok := n.quarantineUntil["peer1"]; !ok {
		t.Fatalf("expected quarantine on repeated dangerous drift in cooldown window")
	}
}

func makePeerInfoMismatchNode(height uint64, expectedSet []string) *Node {
	hash := ValidatorSetHash(expectedSet)
	return &Node{
		ID:                          "A",
		Blockchain:                  &Blockchain{Blocks: []Block{{ID: height}}},
		peerToValidator:             make(map[string]string),
		peerRole:                    make(map[string]string),
		peerSetHash:                 make(map[string]string),
		peerHashMatch:               make(map[string]bool),
		peerAckHeight:               make(map[string]uint64),
		validatorSuspect:            make(map[string]time.Time),
		frozenValidatorsByHeight:    map[uint64][]string{height + 1: append([]string{}, expectedSet...)},
		frozenValidatorHashByHeight: map[uint64]string{height + 1: hash},
	}
}

func TestApplyPeerInfoSkipsMismatchEscalationForFullPeer(t *testing.T) {
	n := makePeerInfoMismatchNode(21, []string{"A", "B", "C", "D"})
	hello := PeerHello{
		Role:             "full",
		ValidatorID:      "",
		Height:           21,
		ValidatorSetHash: "deadbeef",
	}
	n.applyPeerInfo("peer-full", hello)

	if n.validatorSetMismatchCnt != 0 {
		t.Fatalf("full/candidate peer mismatch should not escalate, got mismatchCnt=%d", n.validatorSetMismatchCnt)
	}
	n.peerStateMu.Lock()
	gotRole := n.peerRole["peer-full"]
	n.peerStateMu.Unlock()
	if gotRole != "full" {
		t.Fatalf("expected stored peer role=full, got=%s", gotRole)
	}
}

func TestApplyPeerInfoValidatorMismatchFrozenSourceEscalates(t *testing.T) {
	n := makePeerInfoMismatchNode(21, []string{"A", "B", "C", "D"})
	hello := PeerHello{
		Role:             "validator",
		ValidatorID:      "Z",
		Height:           21,
		ValidatorSetHash: "deadbeef",
	}
	n.applyPeerInfo("peer-validator", hello)

	if n.validatorSetMismatchCnt != 1 {
		t.Fatalf("frozen-source validator mismatch should escalate, got mismatchCnt=%d", n.validatorSetMismatchCnt)
	}
}

func TestApplyPeerInfoValidatorMismatchChainSourceEscalates(t *testing.T) {
	prevFork := ValidatorSetCommitmentV2Height
	ValidatorSetCommitmentV2Height = 21
	defer func() { ValidatorSetCommitmentV2Height = prevFork }()

	expectedSet := []string{"A", "B", "C", "D"}
	expectedHash := ValidatorSetHash(expectedSet)
	parent := Block{
		ID:                     21,
		BlockHash:              "h21",
		ValidatorSetHash:       expectedHash,
		NextValidatorSetHash:   expectedHash,
		NextValidatorSetHeight: 22,
		ActivationHeight:       22,
	}
	n := &Node{
		ID:                          "A",
		Blockchain:                  &Blockchain{Blocks: []Block{parent}},
		peerToValidator:             make(map[string]string),
		peerRole:                    make(map[string]string),
		peerSetHash:                 make(map[string]string),
		peerHashMatch:               make(map[string]bool),
		peerAckHeight:               make(map[string]uint64),
		validatorSuspect:            make(map[string]time.Time),
		frozenValidatorsByHeight:    map[uint64][]string{22: append([]string{}, expectedSet...)},
		frozenValidatorHashByHeight: map[uint64]string{22: expectedHash},
	}
	hello := PeerHello{
		Role:             "validator",
		ValidatorID:      "Z",
		Height:           21,
		ValidatorSetHash: "deadbeef",
		ActivationHeight: 22,
	}
	n.applyPeerInfo("peer-validator", hello)

	if n.validatorSetMismatchCnt == 0 {
		t.Fatalf("chain-authoritative validator mismatch should still track/escalate mismatches")
	}
}

func TestPeerHelloAdvertiseIdentityKeepsActiveValidatorIdentityWhileParticipationBlocked(t *testing.T) {
	defer withOnboardingStrictActivationGlobals(t)()
	configureStrictActivationDefaults()
	ValidatorRequireStake = false

	n := &Node{
		ID:           "A",
		Role:         "validator",
		ValidatorKey: strictActivationTestValidatorKey(11, "A"),
		Blockchain: &Blockchain{
			Blocks: []Block{{ID: 954, BlockHash: "h954"}},
		},
		frozenValidatorsByHeight: map[uint64][]string{
			955: {"A", "B", "C", "D"},
		},
	}

	role, validatorID, pubHex := n.peerHelloAdvertiseIdentity(955)
	if role != "validator" {
		t.Fatalf("expected validator role, got=%s", role)
	}
	if validatorID != "A" {
		t.Fatalf("expected validator id A, got=%s", validatorID)
	}
	if pubHex == "" {
		t.Fatalf("expected validator pubkey to be advertised")
	}
}

func TestBestObservedValidatorSetHashFallsBackToValidatedPeerHello(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	n := &Node{
		peerRole: map[string]string{
			"peerB": "validator",
			"peerC": "validator",
			"peerD": "full",
		},
		peerHelloOK: map[string]bool{
			"peerB": true,
			"peerC": true,
			"peerD": true,
		},
		peerToValidator: map[string]string{
			"peerB": "B",
			"peerC": "C",
			"peerD": "",
		},
		peerSetHash: map[string]string{
			"peerB": hash,
			"peerC": hash,
			"peerD": hash,
		},
		peerAckHeight: map[string]uint64{
			"peerB": 993,
			"peerC": 993,
			"peerD": 993,
		},
	}

	gotHash, gotVotes, gotHeight, ok := n.bestObservedValidatorSetHash()
	if !ok {
		t.Fatalf("expected peer-hello fallback sample to resolve")
	}
	if gotHash != hash {
		t.Fatalf("unexpected hash: got=%s want=%s", gotHash, hash)
	}
	if gotVotes != 2 {
		t.Fatalf("unexpected votes: got=%d want=2", gotVotes)
	}
	if gotHeight != 993 {
		t.Fatalf("unexpected height: got=%d want=993", gotHeight)
	}
}
