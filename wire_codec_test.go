package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestP2PMessageProtobufEnvelopeRoundTripAndJSONFallback(t *testing.T) {
	original := Message{Type: MsgTx, Data: []byte("payload")}
	wire, err := MarshalP2PMessage(original)
	if err != nil {
		t.Fatalf("marshal p2p message: %v", err)
	}
	if bytes.HasPrefix(bytes.TrimSpace(wire), []byte("{")) {
		t.Fatalf("p2p message should use protobuf-wire envelope, got JSON")
	}
	var decoded Message
	if err := UnmarshalP2PMessage(wire, &decoded); err != nil {
		t.Fatalf("unmarshal p2p protobuf envelope: %v", err)
	}
	if decoded.Type != original.Type || string(decoded.Data) != string(original.Data) {
		t.Fatalf("p2p message mismatch: got %#v want %#v", decoded, original)
	}

	legacy, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal legacy message: %v", err)
	}
	decoded = Message{}
	if err := UnmarshalP2PMessage(legacy, &decoded); err != nil {
		t.Fatalf("unmarshal legacy JSON message: %v", err)
	}
	if decoded.Type != original.Type || string(decoded.Data) != string(original.Data) {
		t.Fatalf("legacy p2p message mismatch: got %#v want %#v", decoded, original)
	}
}

func TestTransactionProtobufRoundTrip(t *testing.T) {
	tx := Transaction{
		ID:              "tx-1",
		From:            "alice",
		To:              "bob",
		Amount:          42,
		Nonce:           7,
		PublicKey:       "pub",
		Signature:       "sig",
		Fee:             3,
		Expiry:          99,
		GasLimit:        21000,
		StakeEpochs:     123,
		ValidatorPubKey: "validator-pub",
		DTLTxType:       "TOKEN_TRANSFER",
		DTLTokenID:      "MSC",
		DTLPayload:      `{"ok":true}`,
		Type:            TxDTL,
		ChainID:         "91938",
		Coin:            "MSC",
	}
	wire, err := MarshalTransactionProtobuf(tx)
	if err != nil {
		t.Fatalf("marshal tx protobuf: %v", err)
	}
	if bytes.HasPrefix(bytes.TrimSpace(wire), []byte("{")) {
		t.Fatalf("tx should use protobuf-wire encoding, got JSON")
	}
	var got Transaction
	if err := UnmarshalTransactionWire(wire, &got); err != nil {
		t.Fatalf("unmarshal tx protobuf: %v", err)
	}
	if got.ID != tx.ID || got.Amount != tx.Amount || got.Type != tx.Type || got.DTLPayload != tx.DTLPayload || got.ChainID != tx.ChainID {
		t.Fatalf("tx mismatch: got %#v want %#v", got, tx)
	}
}

func TestBlockProtobufRoundTrip(t *testing.T) {
	block := Block{
		ID:               10,
		Height:           10,
		Round:            2,
		BlockHash:        "block-hash",
		PrevHash:         "prev-hash",
		Proposer:         "A",
		Timestamp:        12345,
		StateRoot:        "state-root",
		MempoolRoot:      "tx-root",
		ValidatorSetHash: "validator-set",
		ConsensusMode:    "strict",
		FinalizedHeight:  9,
		FinalityRoot:     "finality-root",
		Signatures:       []string{"A", "B", "C"},
		Transactions: []Transaction{{
			ID:      "tx-1",
			From:    "alice",
			To:      "bob",
			Amount:  5,
			ChainID: "91938",
			Coin:    "MSC",
		}},
	}
	wire, err := MarshalBlockProtobuf(block)
	if err != nil {
		t.Fatalf("marshal block protobuf: %v", err)
	}
	if bytes.HasPrefix(bytes.TrimSpace(wire), []byte("{")) {
		t.Fatalf("block should use protobuf-wire encoding, got JSON")
	}
	var got Block
	if err := UnmarshalBlockWire(wire, &got); err != nil {
		t.Fatalf("unmarshal block protobuf: %v", err)
	}
	if got.ID != block.ID || got.BlockHash != block.BlockHash || got.Transactions[0].ID != "tx-1" || len(got.Signatures) != 3 {
		t.Fatalf("block mismatch: got %#v want %#v", got, block)
	}
}

func TestSnapshotBinaryManifestRoundTrip(t *testing.T) {
	snap := StateSnapshot{
		Height:                12,
		BlockHash:             "block-12",
		StateRoot:             "state-root",
		ValidatorSetHash:      "validator-set",
		ValidatorRegistryHash: "validator-registry",
		Ledger:                NewLedger(),
	}
	populateSnapshotDerivedFields(&snap)
	manifest, payload, err := snapshotManifestFromSnapshot(&snap)
	if err != nil {
		t.Fatalf("snapshot manifest: %v", err)
	}
	if !bytes.HasPrefix(payload, []byte(snapshotBinaryMagic)) {
		t.Fatalf("snapshot payload should be binary envelope")
	}
	verified, err := verifySnapshotPayloadAgainstManifest(payload, manifest, 0)
	if err != nil {
		t.Fatalf("verify binary snapshot payload: %v", err)
	}
	if verified.Height != snap.Height || verified.SnapshotHash != snap.SnapshotHash {
		t.Fatalf("snapshot mismatch: got h=%d hash=%s want h=%d hash=%s", verified.Height, verified.SnapshotHash, snap.Height, snap.SnapshotHash)
	}
}
