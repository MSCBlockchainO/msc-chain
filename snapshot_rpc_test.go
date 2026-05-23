package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishSnapshotMetaAndChunkGossipCachesLocally(t *testing.T) {
	block, snapshot := makeSnapshotLayerFixture(32, "", NewLedger(), testValidatorSetMaterializationRegistry())
	_ = block
	n := &Node{ID: "A"}

	if ok := n.publishSnapshotMetaGossip(&snapshot); ok {
		t.Fatalf("expected publish without pubsub to skip network send")
	}
	if ok := n.publishSnapshotChunkGossip(&snapshot); ok {
		t.Fatalf("expected publish without pubsub to skip network send")
	}

	n.snapshotGossipMu.RLock()
	defer n.snapshotGossipMu.RUnlock()
	if len(n.snapshotMetaGossipCache) != 1 {
		t.Fatalf("expected one cached snapshot meta gossip entry, got=%d", len(n.snapshotMetaGossipCache))
	}
	if len(n.snapshotChunkGossipCache) != 1 {
		t.Fatalf("expected one cached snapshot chunk gossip entry, got=%d", len(n.snapshotChunkGossipCache))
	}
}

func TestHandleV1SnapshotLatest(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block1, snap1 := makeSnapshotLayerFixture(1, "", NewLedger(), testValidatorSetMaterializationRegistry())
	n := &Node{
		DB:         db,
		Blockchain: &Blockchain{Blocks: []Block{block1}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test_commit"); err != nil {
		t.Fatalf("store committed snapshot: %v", err)
	}

	server := &Server{Node: n}
	req := httptest.NewRequest(http.MethodGet, "/v1/snapshot/latest", nil)
	rec := httptest.NewRecorder()
	server.handleV1SnapshotLatest(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected success response")
	}
	if got := uint64(resp.Data["height"].(float64)); got != 1 {
		t.Fatalf("unexpected height: got=%d want=1", got)
	}
}

func TestHandleV1SnapshotCreateAndSnapshotExport(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	registry := testValidatorSetMaterializationRegistry()
	withValidatorRegistryTestState(t, registry)

	ledger := NewLedger()
	ledger.Balances["alice"] = 10
	block1 := Block{ID: 1, BlockHash: "block-1"}
	block2, _ := makeSnapshotLayerFixture(2, block1.BlockHash, ledger, registry)
	block2.Signatures = canonicalValidatorIDs([]string{"A", "B", "C", "D"})
	block2.StateRoot = ComputeExecHash(block2, HashLedger(ledger))

	dataDir := t.TempDir()
	n := &Node{
		DB:                db,
		DataDir:           dataDir,
		Ledger:            ledger.Clone(),
		Blockchain:        &Blockchain{Blocks: []Block{block1, block2}},
		GenesisValidators: canonicalValidatorIDs([]string{"A", "B", "C", "D"}),
	}
	server := &Server{Node: n}

	createReq := httptest.NewRequest(http.MethodPost, "/v1/snapshot/create?force=1", nil)
	createRec := httptest.NewRecorder()
	server.handleV1SnapshotCreate(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("unexpected create status: got=%d want=%d body=%s", createRec.Code, http.StatusOK, createRec.Body.String())
	}

	exportReq := httptest.NewRequest(http.MethodPost, "/snapshot/export?force=1", nil)
	exportRec := httptest.NewRecorder()
	server.handleSnapshotExport(exportRec, exportReq)
	if exportRec.Code != http.StatusOK {
		t.Fatalf("unexpected export status: got=%d want=%d body=%s", exportRec.Code, http.StatusOK, exportRec.Body.String())
	}

	var exportResp map[string]interface{}
	if err := json.Unmarshal(exportRec.Body.Bytes(), &exportResp); err != nil {
		t.Fatalf("decode export response: %v", err)
	}
	exportDir, _ := exportResp["export_dir"].(string)
	if exportDir == "" {
		t.Fatalf("expected export_dir in response")
	}
	if _, err := os.Stat(filepath.Join(exportDir, "meta.json")); err != nil {
		t.Fatalf("expected exported meta.json: %v", err)
	}
}

func TestHandleV1SnapshotManifestAndChunk(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block1, snap1 := makeSnapshotLayerFixture(4, "", NewLedger(), testValidatorSetMaterializationRegistry())
	n := &Node{
		DB:         db,
		DataDir:    t.TempDir(),
		Blockchain: &Blockchain{Blocks: []Block{block1}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test_manifest"); err != nil {
		t.Fatalf("store committed snapshot: %v", err)
	}

	server := &Server{Node: n}

	manifestReq := httptest.NewRequest(http.MethodGet, "/v1/snapshot/manifest?height=4", nil)
	manifestRec := httptest.NewRecorder()
	server.handleV1SnapshotManifest(manifestRec, manifestReq)
	if manifestRec.Code != http.StatusOK {
		t.Fatalf("unexpected manifest status: got=%d want=%d body=%s", manifestRec.Code, http.StatusOK, manifestRec.Body.String())
	}

	var manifestResp struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(manifestRec.Body.Bytes(), &manifestResp); err != nil {
		t.Fatalf("decode manifest response: %v", err)
	}
	if !manifestResp.Success {
		t.Fatalf("expected manifest success response")
	}
	if got := uint64(manifestResp.Data["height"].(float64)); got != 4 {
		t.Fatalf("unexpected manifest height: got=%d want=4", got)
	}

	chunkReq := httptest.NewRequest(http.MethodGet, "/v1/snapshot/chunk?height=4&index=0", nil)
	chunkRec := httptest.NewRecorder()
	server.handleV1SnapshotChunk(chunkRec, chunkReq)
	if chunkRec.Code != http.StatusOK {
		t.Fatalf("unexpected chunk status: got=%d want=%d body=%s", chunkRec.Code, http.StatusOK, chunkRec.Body.String())
	}

	var chunkResp struct {
		Success bool `json:"success"`
		Data    struct {
			Chunk SnapshotChunkResponse `json:"chunk"`
		} `json:"data"`
	}
	if err := json.Unmarshal(chunkRec.Body.Bytes(), &chunkResp); err != nil {
		t.Fatalf("decode chunk response: %v", err)
	}
	if !chunkResp.Success {
		t.Fatalf("expected chunk success response")
	}
	if chunkResp.Data.Chunk.Height != 4 || chunkResp.Data.Chunk.Index != 0 {
		t.Fatalf("unexpected chunk response: %+v", chunkResp.Data.Chunk)
	}
	if len(chunkResp.Data.Chunk.Data) == 0 {
		t.Fatalf("expected chunk data payload")
	}
}

func TestHandleV1SnapshotDownloadUsesExistingStoredSnapshot(t *testing.T) {
	db, cleanup := openNodeDBForTest(t)
	defer cleanup()

	block1, snap1 := makeSnapshotLayerFixture(6, "", NewLedger(), testValidatorSetMaterializationRegistry())
	n := &Node{
		DB:         db,
		DataDir:    t.TempDir(),
		Blockchain: &Blockchain{Blocks: []Block{block1}},
	}
	if err := n.storeCommittedStateSnapshotRecord(&snap1, "test_download_existing"); err != nil {
		t.Fatalf("store committed snapshot: %v", err)
	}

	server := &Server{Node: n}
	req := httptest.NewRequest(http.MethodPost, "/v1/snapshot/download?height=6&apply=0", nil)
	rec := httptest.NewRecorder()
	server.handleV1SnapshotDownload(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected download status: got=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode download response: %v", err)
	}
	if !resp.Success {
		t.Fatalf("expected download success response")
	}
	if got, _ := resp.Data["stored"].(bool); !got {
		t.Fatalf("expected stored=true in download response")
	}
	if got, _ := resp.Data["applied"].(bool); got {
		t.Fatalf("expected applied=false when apply=0")
	}
	if got, _ := resp.Data["source"].(string); got != "existing_verified_store" {
		t.Fatalf("unexpected download source: got=%q want=existing_verified_store", got)
	}
}
