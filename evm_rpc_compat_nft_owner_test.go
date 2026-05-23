package main

import (
	"encoding/json"
	"testing"
)

func newDTLNFTOwnerTestServer() *Server {
	bc := NewBlockchain()
	node := &Node{
		Blockchain: &bc,
		Ledger:     GenesisLedger(),
	}
	ensureDTLState(&node.Ledger)
	return NewServer(node)
}

func asMapSlice(t *testing.T, v any) []map[string]any {
	t.Helper()
	itemsRaw, ok := v.([]map[string]any)
	if ok {
		return itemsRaw
	}
	anySlice, ok := v.([]any)
	if !ok {
		t.Fatalf("expected []any items, got %T", v)
	}
	out := make([]map[string]any, 0, len(anySlice))
	for _, item := range anySlice {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected map item, got %T", item)
		}
		out = append(out, m)
	}
	return out
}

func TestDTLListNFT721ByOwnerFiltersSortsAndPaginates(t *testing.T) {
	s := newDTLNFTOwnerTestServer()
	dtl := s.Node.Ledger.DTL

	dtl.NFT721Collections["alpha"] = &DTLNFT721CollectionState{
		CollectionID: "alpha",
		Symbol:       "ALP",
		Name:         "Alpha",
		BaseURI:      "https://example.com/alpha/",
	}
	dtl.NFT721Collections["beta"] = &DTLNFT721CollectionState{
		CollectionID: "beta",
		Symbol:       "BET",
		Name:         "Beta",
		BaseURI:      "https://example.com/beta/",
	}

	ownerKey1 := dtlNFT721OwnerKey("beta", 8)
	ownerKey2 := dtlNFT721OwnerKey("alpha", 12)
	ownerKey3 := dtlNFT721OwnerKey("alpha", 2)
	ownerKeyOther := dtlNFT721OwnerKey("alpha", 1)

	dtl.NFT721Owners[ownerKey1] = "alice"
	dtl.NFT721Owners[ownerKey2] = "alice"
	dtl.NFT721Owners[ownerKey3] = "alice"
	dtl.NFT721Owners[ownerKeyOther] = "bob"
	dtl.NFT721TokenURIs[ownerKey2] = "ipfs://bafy/token12.json"

	got, err := s.dtlListNFT721ByOwner("ALICE", 1, 1, nil)
	if err != nil {
		t.Fatalf("dtlListNFT721ByOwner returned error: %v", err)
	}

	if got["total"] != 3 {
		t.Fatalf("expected total=3, got %#v", got["total"])
	}
	if got["next_offset"] != 2 {
		t.Fatalf("expected next_offset=2, got %#v", got["next_offset"])
	}
	if got["block_number"] != "0x0" {
		t.Fatalf("expected block_number=0x0, got %#v", got["block_number"])
	}

	items := asMapSlice(t, got["items"])
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0]["collection_id"] != "alpha" {
		t.Fatalf("expected collection_id=alpha, got %#v", items[0]["collection_id"])
	}
	if items[0]["token_id"] != "12" {
		t.Fatalf("expected token_id=12 after sorted pagination, got %#v", items[0]["token_id"])
	}
	if items[0]["token_uri"] != "ipfs://bafy/token12.json" {
		t.Fatalf("unexpected token_uri: %#v", items[0]["token_uri"])
	}
}

func TestDTLListNFT1155ByOwnerFiltersPositiveSortsAndPaginates(t *testing.T) {
	s := newDTLNFTOwnerTestServer()
	dtl := s.Node.Ledger.DTL

	dtl.NFT1155Collections["alpha"] = &DTLNFT1155CollectionState{
		CollectionID: "alpha",
		Symbol:       "AL1155",
		Name:         "Alpha1155",
		BaseURI:      "https://example.com/1155/{id}.json",
	}
	dtl.NFT1155Collections["beta"] = &DTLNFT1155CollectionState{
		CollectionID: "beta",
		Symbol:       "BE1155",
		Name:         "Beta1155",
		BaseURI:      "https://example.com/1155b/{id}.json",
	}

	dtl.NFT1155Balances[dtlNFT1155BalanceKey("beta", 9, "alice")] = 4
	dtl.NFT1155Balances[dtlNFT1155BalanceKey("alpha", 11, "alice")] = 7
	dtl.NFT1155Balances[dtlNFT1155BalanceKey("alpha", 3, "alice")] = 1
	dtl.NFT1155Balances[dtlNFT1155BalanceKey("alpha", 1, "alice")] = 0 // filtered
	dtl.NFT1155Balances[dtlNFT1155BalanceKey("alpha", 2, "bob")] = 9   // other owner

	got, err := s.dtlListNFT1155ByOwner("alice", 0, 2, nil)
	if err != nil {
		t.Fatalf("dtlListNFT1155ByOwner returned error: %v", err)
	}

	if got["total"] != 3 {
		t.Fatalf("expected total=3, got %#v", got["total"])
	}
	if got["next_offset"] != 2 {
		t.Fatalf("expected next_offset=2, got %#v", got["next_offset"])
	}

	items := asMapSlice(t, got["items"])
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0]["collection_id"] != "alpha" || items[0]["token_id"] != "3" {
		t.Fatalf("unexpected first item sort order: %#v", items[0])
	}
	if items[1]["collection_id"] != "alpha" || items[1]["token_id"] != "11" {
		t.Fatalf("unexpected second item sort order: %#v", items[1])
	}
}

func TestDTLListNFTOwnerEmptyAndInvalidAccount(t *testing.T) {
	s := newDTLNFTOwnerTestServer()

	got721, err := s.dtlListNFT721ByOwner("alice", 0, 50, nil)
	if err != nil {
		t.Fatalf("unexpected error for empty 721 inventory: %v", err)
	}
	items721 := asMapSlice(t, got721["items"])
	if len(items721) != 0 {
		t.Fatalf("expected empty 721 items, got %d", len(items721))
	}

	got1155, err := s.dtlListNFT1155ByOwner("alice", 0, 50, nil)
	if err != nil {
		t.Fatalf("unexpected error for empty 1155 inventory: %v", err)
	}
	items1155 := asMapSlice(t, got1155["items"])
	if len(items1155) != 0 {
		t.Fatalf("expected empty 1155 items, got %d", len(items1155))
	}

	if _, err := s.dtlListNFT721ByOwner("   ", 0, 50, nil); err == nil {
		t.Fatalf("expected invalid account error for 721")
	}
	if _, err := s.dtlListNFT1155ByOwner("", 0, 50, nil); err == nil {
		t.Fatalf("expected invalid account error for 1155")
	}
}

func TestJSONRPCDTLListNFTOwnerInvalidAccountReturnsInvalidParams(t *testing.T) {
	s := newDTLNFTOwnerTestServer()

	req721 := jsonRPCRequest{
		ID:      json.RawMessage(`1`),
		Method:  "dtl_listNFT721ByOwner",
		JSONRPC: "2.0",
		Params:  json.RawMessage(`[""]`),
	}
	resp721 := s.handleJSONRPCMethod(req721)
	if resp721.Error == nil || resp721.Error.Code != -32602 {
		t.Fatalf("expected -32602 for dtl_listNFT721ByOwner invalid account, got %#v", resp721.Error)
	}

	req1155 := jsonRPCRequest{
		ID:      json.RawMessage(`1`),
		Method:  "dtl_listNFT1155ByOwner",
		JSONRPC: "2.0",
		Params:  json.RawMessage(`["   "]`),
	}
	resp1155 := s.handleJSONRPCMethod(req1155)
	if resp1155.Error == nil || resp1155.Error.Code != -32602 {
		t.Fatalf("expected -32602 for dtl_listNFT1155ByOwner invalid account, got %#v", resp1155.Error)
	}
}
