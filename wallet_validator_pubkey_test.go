package main

import (
	"bytes"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withWalletStatusAuthDisabled(t *testing.T) func() {
	t.Helper()

	oldRequireReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	return func() {
		ConfigRPCRequireAuthForReadEndpoints = oldRequireReadAuth
	}
}

func newWalletStatusRequestAddress(t *testing.T) string {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate request address pubkey: %v", err)
	}
	return AddressFromPublicKey(pub)
}

func TestHandleWalletStatusIncludesLocalValidatorConsensusPubKey(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()
	defer withWalletStatusAuthDisabled(t)()

	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate validator key: %v", err)
	}
	t.Cleanup(func() { ZeroMemory(priv) })

	validatorPubKeysMu.Lock()
	GenesisValidatorPubKeys["F"] = append(ed25519.PublicKey(nil), pub...)
	validatorPubKeysMu.Unlock()
	GlobalValidatorRegistry.Load(nil)

	server := &Server{
		Node: &Node{
			ID:                "F",
			DataDir:           t.TempDir(),
			GenesisValidators: []string{"F"},
			ValidatorKey: ValidatorKey{
				ID:         "F",
				PublicKey:  append(ed25519.PublicKey(nil), pub...),
				PrivateKey: append(ed25519.PrivateKey(nil), priv...),
			},
			Blockchain: &Blockchain{},
			Ledger: Ledger{
				Stakes:                 map[string]StakeLock{},
				ValidatorRewardWallets: map[string]string{},
			},
		},
	}

	req := httptest.NewRequest(
		http.MethodGet,
		"/wallet/status?address="+newWalletStatusRequestAddress(t),
		nil,
	)
	rec := httptest.NewRecorder()
	server.handleWalletStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	wantPubKey := strings.ToLower(hex.EncodeToString(pub))
	if got, _ := payload["local_validator_id"].(string); got != "F" {
		t.Fatalf("unexpected local_validator_id: got=%q want=%q", got, "F")
	}
	if got, _ := payload["local_validator_key_loaded"].(bool); !got {
		t.Fatalf("expected local validator key to be loaded")
	}
	if got, _ := payload["local_validator_consensus_pubkey"].(string); got != wantPubKey {
		t.Fatalf("unexpected local validator consensus pubkey: got=%q want=%q", got, wantPubKey)
	}
	if got, _ := payload["local_validator_consensus_pubkey_anchored"].(bool); !got {
		t.Fatalf("expected local validator consensus pubkey to be anchored")
	}
	if got, _ := payload["local_validator_consensus_pubkey_source"].(string); got != "registry_snapshot" {
		t.Fatalf("unexpected local validator consensus pubkey source: got=%q want=%q", got, "registry_snapshot")
	}
}

func TestHandleWalletStatusReturnsEmptyLocalValidatorConsensusPubKeyWhenKeyUnavailable(t *testing.T) {
	defer withStakeConsensusPubKeyGlobals(t)()
	defer withWalletStatusAuthDisabled(t)()

	pub, _, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		t.Fatalf("generate validator key: %v", err)
	}

	validatorPubKeysMu.Lock()
	GenesisValidatorPubKeys["F"] = append(ed25519.PublicKey(nil), pub...)
	validatorPubKeysMu.Unlock()
	GlobalValidatorRegistry.Load(nil)

	server := &Server{
		Node: &Node{
			ID:                "F",
			DataDir:           t.TempDir(),
			GenesisValidators: []string{"F"},
			Blockchain:        &Blockchain{},
			Ledger: Ledger{
				Stakes:                 map[string]StakeLock{},
				ValidatorRewardWallets: map[string]string{},
			},
		},
	}

	req := httptest.NewRequest(
		http.MethodGet,
		"/wallet/status?address="+newWalletStatusRequestAddress(t),
		nil,
	)
	rec := httptest.NewRecorder()
	server.handleWalletStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if got, _ := payload["local_validator_key_loaded"].(bool); got {
		t.Fatalf("expected local validator key to be unavailable")
	}
	if got, _ := payload["local_validator_consensus_pubkey"].(string); got != "" {
		t.Fatalf("expected empty local validator consensus pubkey, got=%q", got)
	}
	if got, _ := payload["local_validator_consensus_pubkey_anchored"].(bool); !got {
		t.Fatalf("expected anchored status to remain true from authoritative registry state")
	}
}

func browserStakePayloadForTest(tx Transaction, chainID string) []byte {
	var buf bytes.Buffer

	pushString := func(value string) {
		buf.WriteString(value)
		buf.WriteByte(0)
	}
	pushInt64 := func(value int64) {
		_ = binary.Write(&buf, binary.BigEndian, value)
	}

	txType := int(tx.Type)
	pushString(tx.From)
	pushString(tx.To)
	pushString(normalizeCoin(tx.Coin))
	pushInt64(int64(tx.Amount))
	pushInt64(int64(tx.Fee))
	pushInt64(int64(tx.Nonce))
	pushInt64(int64(tx.Expiry))
	pushInt64(int64(tx.StakeEpochs))
	if txType == int(TxStake) {
		if normalized := normalizeConsensusPubKeyHex(tx.ValidatorPubKey); normalized != "" {
			pushString(normalized)
		}
	}
	pushInt64(int64(tx.EVMGasLimit))
	pushString(stripHexPrefix(tx.EVMCode))
	pushString(stripHexPrefix(tx.EVMInput))
	pushString(stripHexPrefix(tx.EVMRawTx))
	pushString(stripHexPrefix(tx.EVMTxHash))
	if txType == int(TxDTL) {
		pushString(strings.TrimSpace(tx.DTLTxType))
		pushString(strings.TrimSpace(tx.DTLTokenID))
		pushString(strings.TrimSpace(tx.DTLPayload))
		pushString(strings.TrimSpace(tx.DTLGovernanceCert))
	}
	if txType == int(TxValidatorUpdate) {
		pushString(validatorUpdateCertPayloadForTx(tx.ValidatorUpdateCert))
	}
	pushString(chainID)
	buf.WriteByte(byte(txType))

	return buf.Bytes()
}

func TestBrowserStakePayloadMatchesGoTxPayloadWhenValidatorPubKeyPresent(t *testing.T) {
	tx := Transaction{
		From:            "MSCsender",
		To:              "F",
		Amount:          100,
		Fee:             1,
		Nonce:           7,
		Expiry:          123456789,
		Coin:            CoinSymbol,
		Type:            TxStake,
		StakeEpochs:     DefaultStakeLockEpochs,
		ValidatorPubKey: "0x" + strings.Repeat("ab", ed25519.PublicKeySize),
	}

	got := browserStakePayloadForTest(tx, ChainID)
	want := TxPayload(tx)
	if !bytes.Equal(got, want) {
		t.Fatalf("browser stake payload mismatch with Go TxPayload")
	}
}
