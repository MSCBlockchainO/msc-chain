package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testLightServer(t *testing.T) (*Server, string, string) {
	t.Helper()

	addr := "MSC01abc0000000000000000000000000000000001"
	receiver := "MSC01def0000000000000000000000000000000002"
	ledger := NewLedger()
	setBalance(&ledger, CoinSymbol, addr, 321)
	setBalance(&ledger, CoinSymbol, receiver, 10)

	tx := Transaction{
		ID:     "tx-light-1",
		From:   addr,
		To:     receiver,
		Amount: 7,
		Coin:   CoinSymbol,
	}
	receipt := StateReceipt{
		TxHash:        tx.ID,
		PreStateHash:  "pre-state",
		PostStateHash: "post-state",
	}
	block := Block{
		ID:               1,
		BlockHash:        "block-light-1",
		PrevHash:         GenesisHash,
		Transactions:     []Transaction{tx},
		Receipts:         []StateReceipt{receipt},
		MempoolRoot:      ComputeMempoolRoot([]Transaction{tx}),
		ReceiptRoot:      ComputeReceiptRoot([]StateReceipt{receipt}),
		StateRoot:        "state-root",
		ValidatorSetHash: ValidatorSetHash([]string{"A", "B", "C", "D"}),
		FinalizedHeight:  1,
	}

	node := &Node{
		Blockchain: &Blockchain{Blocks: []Block{block}},
		Ledger:     ledger,
	}
	return &Server{Node: node}, addr, tx.ID
}

func TestLightMerkleProofVerifiesAndRejectsTamper(t *testing.T) {
	leaves := []lightMerkleLeaf{
		{Key: "a", Value: "a=1"},
		{Key: "b", Value: "b=2"},
		{Key: "c", Value: "c=3"},
	}
	proof, ok := buildLightMerkleProof("test", leaves, "b")
	if !ok {
		t.Fatal("expected proof")
	}
	if !VerifyLightMerkleProof(proof) {
		t.Fatal("expected proof to verify")
	}
	proof.LeafValue = "b=999"
	if VerifyLightMerkleProof(proof) {
		t.Fatal("tampered proof verified")
	}
}

func TestLightBalanceProofEndpointVerifies(t *testing.T) {
	server, addr, _ := testLightServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/light/proof/balance?address="+addr+"&coin="+CoinSymbol+"&state=latest", nil)
	rr := httptest.NewRecorder()

	server.handleLightBalanceProof(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Success bool               `json:"success"`
		Data    LightProofResponse `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.Success {
		t.Fatalf("expected success: %+v", out)
	}
	if !VerifyLightStateProof(out.Data.Header, out.Data.Proof) {
		t.Fatalf("balance proof did not verify: %s", out.Data.Proof.Root)
	}
	if !strings.Contains(out.Data.Proof.LeafValue, "=321") {
		t.Fatalf("unexpected leaf value %q", out.Data.Proof.LeafValue)
	}
}

func TestLightReceiptProofEndpointVerifies(t *testing.T) {
	server, _, txID := testLightServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/light/proof/receipt?tx_id="+txID, nil)
	rr := httptest.NewRecorder()

	server.handleLightReceiptProof(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Success bool               `json:"success"`
		Data    LightProofResponse `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !VerifyLightReceiptProof(out.Data.Header, out.Data.Proof) {
		t.Fatalf("receipt proof did not verify: header=%+v proof=%+v", out.Data.Header, out.Data.Proof)
	}
	out.Data.Proof.Siblings = append(out.Data.Proof.Siblings, LightMerkleSibling{Position: "right", Hash: strings.Repeat("0", 64)})
	if VerifyLightReceiptProof(out.Data.Header, out.Data.Proof) {
		t.Fatal("tampered receipt proof verified")
	}
}

func TestLightHeaderChainRejectsBrokenLink(t *testing.T) {
	headers := []LightHeader{
		{Height: 1, BlockHash: "block-1"},
		{Height: 2, BlockHash: "block-2", PrevHash: "block-1"},
	}
	if err := VerifyLightHeaderChain(headers); err != nil {
		t.Fatalf("valid header chain rejected: %v", err)
	}
	headers[1].PrevHash = "wrong"
	if err := VerifyLightHeaderChain(headers); err == nil {
		t.Fatal("broken header chain accepted")
	}
}
