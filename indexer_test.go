package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
)

func newIndexerSourceTestServer(t *testing.T, opts map[string]any) *httptest.Server {
	t.Helper()
	blocks := map[uint64]map[string]any{
		1: {
			"summary": map[string]any{
				"height":                 1,
				"hash":                   "blockhash1",
				"prev_hash":              "genesis",
				"proposer":               "A",
				"type":                   "FINAL",
				"timestamp":              100,
				"tx_count":               1,
				"execution_result_count": 1,
				"receipt_count":          1,
				"signature_count":        3,
			},
			"height":           1,
			"hash":             "blockhash1",
			"prev_hash":        "genesis",
			"type":             "FINAL",
			"proposer":         "A",
			"timestamp":        100,
			"latest_height":    2,
			"finalized_height": 2,
			"transactions": []map[string]any{{
				"id":     "tx1",
				"from":   "MSC01from",
				"to":     "MSC01to",
				"amount": 7,
				"coin":   "MSC",
			}},
			"receipts": []any{map[string]any{"hash": "receipt1"}},
		},
		2: {
			"summary": map[string]any{
				"height":                 2,
				"hash":                   "blockhash2",
				"prev_hash":              "blockhash1",
				"proposer":               "B",
				"type":                   "FINAL",
				"timestamp":              200,
				"tx_count":               1,
				"execution_result_count": 1,
				"receipt_count":          1,
				"signature_count":        3,
			},
			"height":           2,
			"hash":             "blockhash2",
			"prev_hash":        "blockhash1",
			"type":             "FINAL",
			"proposer":         "B",
			"timestamp":        200,
			"latest_height":    2,
			"finalized_height": 2,
			"transactions": []map[string]any{{
				"id":           "tx2",
				"from":         "MSC01to",
				"to":           "MSC01third",
				"amount":       3,
				"dtl_token_id": "MSCX",
			}},
			"receipts": []any{map[string]any{"hash": "receipt2"}},
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/status":
			role, _ := opts["role"].(string)
			if role == "" {
				role = "full"
			}
			genesis, _ := opts["genesis_hash"].(string)
			if genesis == "" {
				genesis = "genesis-ok"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"node_id":          "ARCHIVE1",
				"role":             role,
				"is_validator":     opts["is_validator"] == true,
				"chain_id":         "91938",
				"genesis_hash":     genesis,
				"height":           2,
				"finalized_height": 2,
			})
		case "/storage/policy":
			archiveMode := true
			if opts["archive_mode"] == false {
				archiveMode = false
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"profile":      map[bool]string{true: "archive", false: "full"}[archiveMode],
				"archive_mode": archiveMode,
			})
		case "/explorer/block":
			height, _ := strconv.ParseUint(r.URL.Query().Get("height"), 10, 64)
			block, ok := blocks[height]
			if !ok {
				http.Error(w, "block not found", http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(block)
		default:
			http.NotFound(w, r)
		}
	}))
}

func openIndexerForTest(t *testing.T, source string) *explorerIndexer {
	t.Helper()
	idx, err := openExplorerIndexer(indexerConfig{
		SourceRPC:    source,
		DataDir:      filepath.Join(t.TempDir(), "indexer"),
		ChainID:      "91938",
		GenesisHash:  "genesis-ok",
		PollInterval: 10_000_000_000,
	})
	if err != nil {
		t.Fatalf("open indexer: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

func TestExplorerIndexerIndexesAndResumes(t *testing.T) {
	source := newIndexerSourceTestServer(t, nil)
	defer source.Close()
	idx := openIndexerForTest(t, source.URL)

	idx.runIndexOnce()
	idx.runIndexOnce()

	st := idx.Status()
	if !st.Healthy || st.IndexedHeight != 2 || st.ArchiveHeight != 2 || st.IndexLag != 0 {
		t.Fatalf("unexpected status: %+v", st)
	}
	if tx, ok := idx.txByID("tx1"); !ok || mapUint(tx, "height") != 1 {
		t.Fatalf("tx1 not indexed: ok=%v tx=%+v", ok, tx)
	}
	history := idx.addressHistory("MSC01to", 10, "")
	if len(history) != 2 {
		t.Fatalf("expected two address txs without duplicates, got %d: %+v", len(history), history)
	}
	if block, ok := idx.blockByHash("blockhash2"); !ok || mapUint(block, "height") != 2 {
		t.Fatalf("block hash index missing: ok=%v block=%+v", ok, block)
	}
}

func TestExplorerIndexerHTTPAPI(t *testing.T) {
	source := newIndexerSourceTestServer(t, nil)
	defer source.Close()
	idx := openIndexerForTest(t, source.URL)
	idx.runIndexOnce()

	mux := http.NewServeMux()
	idx.registerRoutes(mux)

	for _, path := range []string{
		"/indexer/status",
		"/indexer/search?q=tx2",
		"/indexer/search?q=blockhash1",
		"/indexer/blocks?limit=2",
		"/indexer/block?height=1",
		"/indexer/tx?tx_id=tx1",
		"/indexer/address?address=MSC01to",
		"/indexer/validators",
		"/indexer/tokens",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestExplorerIndexerRejectsUnsafeSources(t *testing.T) {
	validator := newIndexerSourceTestServer(t, map[string]any{"role": "validator", "is_validator": true})
	defer validator.Close()
	idx := openIndexerForTest(t, validator.URL)
	if _, err := idx.probeSource(); err == nil {
		t.Fatal("expected validator RPC source rejection")
	}

	wrongGenesis := newIndexerSourceTestServer(t, map[string]any{"genesis_hash": "wrong"})
	defer wrongGenesis.Close()
	idx2 := openIndexerForTest(t, wrongGenesis.URL)
	if _, err := idx2.probeSource(); err == nil {
		t.Fatal("expected wrong genesis source rejection")
	}
}
