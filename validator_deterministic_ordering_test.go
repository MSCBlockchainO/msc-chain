package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func TestDeterministicStakeHashOrderedValidatorIDsStableWithStakeTieBreak(t *testing.T) {
	oldChainID := ChainID
	validatorPubKeysMu.RLock()
	oldGenesisKeys := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pk := range GenesisValidatorPubKeys {
		oldGenesisKeys[id] = append(ed25519.PublicKey(nil), pk...)
	}
	validatorPubKeysMu.RUnlock()

	ChainID = "test-chain-order"
	validatorPubKeysMu.Lock()
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": bytesRepeat(0x11, ed25519.PublicKeySize),
		"B": bytesRepeat(0x22, ed25519.PublicKeySize),
	}
	validatorPubKeysMu.Unlock()
	defer func() {
		ChainID = oldChainID
		validatorPubKeysMu.Lock()
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldGenesisKeys))
		for id, pk := range oldGenesisKeys {
			GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pk...)
		}
		validatorPubKeysMu.Unlock()
	}()

	stakes := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 300},
		"D": {ID: "D", Stake: 10},
	}

	a := deterministicStakeHashOrderedValidatorIDs([]string{"b", "A", "D", "C"}, stakes)
	b := deterministicStakeHashOrderedValidatorIDs([]string{"C", "D", "A", "B"}, stakes)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("deterministic ordering must be input-order independent: %v vs %v", a, b)
	}
	if len(a) != 4 {
		t.Fatalf("unexpected ordered size: got=%d want=4", len(a))
	}
	if a[0] != "C" {
		t.Fatalf("highest stake validator must lead ordering: got=%v", a)
	}
	if a[len(a)-1] != "D" {
		t.Fatalf("lowest stake validator must be last: got=%v", a)
	}

	addrA := canonicalValidatorAddressForID("A")
	addrB := canonicalValidatorAddressForID("B")
	wantSecond := "A"
	wantThird := "B"
	if addrB < addrA {
		wantSecond = "B"
		wantThird = "A"
	}
	if a[1] != wantSecond || a[2] != wantThird {
		t.Fatalf("stake tie-break must use canonical address ordering: got=%v want second=%s third=%s", a, wantSecond, wantThird)
	}
}

func TestCanonicalValidatorAddressForIDUsesLowerHexPubkey(t *testing.T) {
	validatorPubKeysMu.RLock()
	oldGenesisKeys := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pk := range GenesisValidatorPubKeys {
		oldGenesisKeys[id] = append(ed25519.PublicKey(nil), pk...)
	}
	validatorPubKeysMu.RUnlock()

	validatorPubKeysMu.Lock()
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": bytesRepeat(0xAB, ed25519.PublicKeySize),
	}
	validatorPubKeysMu.Unlock()
	defer func() {
		validatorPubKeysMu.Lock()
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldGenesisKeys))
		for id, pk := range oldGenesisKeys {
			GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pk...)
		}
		validatorPubKeysMu.Unlock()
	}()

	got := canonicalValidatorAddressForID("a")
	want := strings.ToLower(hex.EncodeToString(bytesRepeat(0xAB, ed25519.PublicKeySize)))
	if got != want {
		t.Fatalf("unexpected canonical address: got=%q want=%q", got, want)
	}
}

func TestCanonicalValidatorTieBreakHashUsesPubKeyPlusBigEndianStake(t *testing.T) {
	validatorPubKeysMu.RLock()
	oldGenesisKeys := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pk := range GenesisValidatorPubKeys {
		oldGenesisKeys[id] = append(ed25519.PublicKey(nil), pk...)
	}
	validatorPubKeysMu.RUnlock()

	pub := bytesRepeat(0x11, ed25519.PublicKeySize)
	validatorPubKeysMu.Lock()
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": append(ed25519.PublicKey(nil), pub...),
	}
	validatorPubKeysMu.Unlock()
	defer func() {
		validatorPubKeysMu.Lock()
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldGenesisKeys))
		for id, pk := range oldGenesisKeys {
			GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pk...)
		}
		validatorPubKeysMu.Unlock()
	}()

	stake := uint64(100)
	got := canonicalValidatorTieBreakHash("A", stake)

	payload := make([]byte, 0, ed25519.PublicKeySize+8)
	payload = append(payload, pub...)
	stakeBE := canonicalUint64BigEndianBytes(stake)
	payload = append(payload, stakeBE[:]...)
	wantSum := sha256.Sum256(payload)
	want := strings.ToLower(hex.EncodeToString(wantSum[:]))
	if got != want {
		t.Fatalf("unexpected canonical validator tie-break hash: got=%q want=%q", got, want)
	}

	stakeFirst := make([]byte, 0, ed25519.PublicKeySize+8)
	stakeFirst = append(stakeFirst, stakeBE[:]...)
	stakeFirst = append(stakeFirst, pub...)
	wrongOrder := sha256.Sum256(stakeFirst)
	if got == strings.ToLower(hex.EncodeToString(wrongOrder[:])) {
		t.Fatalf("hash must not encode stake before pubkey")
	}

	var stakeLE [8]byte
	binary.LittleEndian.PutUint64(stakeLE[:], stake)
	littleEndianPayload := make([]byte, 0, ed25519.PublicKeySize+8)
	littleEndianPayload = append(littleEndianPayload, pub...)
	littleEndianPayload = append(littleEndianPayload, stakeLE[:]...)
	wrongEndian := sha256.Sum256(littleEndianPayload)
	if got == strings.ToLower(hex.EncodeToString(wrongEndian[:])) {
		t.Fatalf("hash must use big-endian stake encoding")
	}
}

func TestCanonicalUint64BigEndianBytes(t *testing.T) {
	got := canonicalUint64BigEndianBytes(100)
	want := [8]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x64}
	if got != want {
		t.Fatalf("unexpected big-endian encoding for 100: got=%x want=%x", got, want)
	}

	var little [8]byte
	binary.LittleEndian.PutUint64(little[:], 100)
	if got == little {
		t.Fatalf("encoding must not match little-endian bytes: got=%x little=%x", got, little)
	}
}

func TestValidatorSetMerkleRootDeterministicPipeline(t *testing.T) {
	validatorPubKeysMu.RLock()
	oldGenesisKeys := make(map[string]ed25519.PublicKey, len(GenesisValidatorPubKeys))
	for id, pk := range GenesisValidatorPubKeys {
		oldGenesisKeys[id] = append(ed25519.PublicKey(nil), pk...)
	}
	validatorPubKeysMu.RUnlock()

	validatorPubKeysMu.Lock()
	GenesisValidatorPubKeys = map[string]ed25519.PublicKey{
		"A": bytesRepeat(0x11, ed25519.PublicKeySize),
		"B": bytesRepeat(0x22, ed25519.PublicKeySize),
		"C": bytesRepeat(0x33, ed25519.PublicKeySize),
		"D": bytesRepeat(0x44, ed25519.PublicKeySize),
	}
	validatorPubKeysMu.Unlock()
	defer func() {
		validatorPubKeysMu.Lock()
		GenesisValidatorPubKeys = make(map[string]ed25519.PublicKey, len(oldGenesisKeys))
		for id, pk := range oldGenesisKeys {
			GenesisValidatorPubKeys[id] = append(ed25519.PublicKey(nil), pk...)
		}
		validatorPubKeysMu.Unlock()
	}()

	stakes := map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100},
		"B": {ID: "B", Stake: 100},
		"C": {ID: "C", Stake: 250},
		"D": {ID: "D", Stake: 50},
	}

	rootA := ValidatorSetMerkleRoot(260, []string{"B", "D", "A", "C"}, stakes)
	rootB := ValidatorSetMerkleRoot(260, []string{"C", "A", "D", "B"}, stakes)
	if rootA == "" || rootB == "" {
		t.Fatalf("validator_set_root must not be empty for non-empty validator set")
	}
	if rootA != rootB {
		t.Fatalf("validator_set_root must be input-order independent: rootA=%s rootB=%s", rootA, rootB)
	}

	hashes := deterministicValidatorCommitmentHashes([]string{"B", "D", "A", "C"}, stakes)
	if len(hashes) != 4 {
		t.Fatalf("unexpected commitment hash count: got=%d want=4", len(hashes))
	}
	highestStakeHash := canonicalValidatorTieBreakHash("C", 250)
	if hashes[0] != highestStakeHash {
		t.Fatalf("highest stake validator hash must lead sorted commitment hashes: got_first=%s want=%s", hashes[0], highestStakeHash)
	}
}

func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestSelectAllStakedValidatorsUsesDeterministicStakeHashOrdering(t *testing.T) {
	oldChainID := ChainID
	oldMinStake := ValidatorMinStake
	oldRegistry := GlobalValidatorRegistry.Snapshot()
	ChainID = "test-chain-order"
	ValidatorMinStake = 1
	defer func() {
		ChainID = oldChainID
		ValidatorMinStake = oldMinStake
		GlobalValidatorRegistry.Load(oldRegistry)
	}()

	GlobalValidatorRegistry.Load(map[string]ValidatorRecord{
		"A": {ID: "A", Stake: 100, Status: ValidatorActive, Reputation: 1},
		"B": {ID: "B", Stake: 100, Status: ValidatorActive, Reputation: 1},
		"C": {ID: "C", Stake: 350, Status: ValidatorActive, Reputation: 1},
		"D": {ID: "D", Stake: 10, Status: ValidatorActive, Reputation: 1},
	})

	got1 := selectAllStakedValidators(64)
	got2 := selectAllStakedValidators(64)
	if !reflect.DeepEqual(got1, got2) {
		t.Fatalf("staked validator ordering must be stable: %v vs %v", got1, got2)
	}
	if len(got1) != 4 {
		t.Fatalf("unexpected staked validator count: got=%d want=4", len(got1))
	}
	if got1[0] != "C" {
		t.Fatalf("highest stake validator must come first: got=%v", got1)
	}
}

func TestCanonicalValidatorIDsFromMapKeysDeterministic(t *testing.T) {
	first := map[string]int{
		"c": 1,
		"A": 1,
		"b": 1,
	}
	second := map[string]int{
		"B": 1,
		"C": 1,
		"a": 1,
	}

	gotA := canonicalValidatorIDsFromMapKeys(first)
	gotB := canonicalValidatorIDsFromMapKeys(second)
	want := []string{"A", "B", "C"}

	if !reflect.DeepEqual(gotA, want) {
		t.Fatalf("unexpected canonical map ordering from first map: got=%v want=%v", gotA, want)
	}
	if !reflect.DeepEqual(gotB, want) {
		t.Fatalf("unexpected canonical map ordering from second map: got=%v want=%v", gotB, want)
	}
	if !reflect.DeepEqual(gotA, gotB) {
		t.Fatalf("map insertion order must not change canonical validator IDs: a=%v b=%v", gotA, gotB)
	}
}
