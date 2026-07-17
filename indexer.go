package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	// `indexerDefaultListen` defines the current position in the related collection.
	indexerDefaultListen = "127.0.0.1:26780"
	// `indexerDefaultSource` defines the current position in the related collection.
	indexerDefaultSource = "http://127.0.0.1:26667"

	// `indexerMetaLastHeight` defines the current position in the related collection.
	indexerMetaLastHeight = "meta:last_height"
	// `indexerMetaLastHash` defines the digest used to identify or verify the related data.
	indexerMetaLastHash = "meta:last_hash"
)

type indexerConfig struct {
	// `SourceRPC` stores the value associated with this record.
	SourceRPC string
	// `ListenAddr` stores the address used by this operation.
	ListenAddr string
	// `DataDir` stores the value associated with this record.
	DataDir string
	// `ChainID` stores the value associated with this record.
	ChainID string
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string
	// `PollInterval` stores the value currently being processed.
	PollInterval time.Duration
	// `AllowFullSource` stores the value associated with this record.
	AllowFullSource bool
}

type explorerIndexer struct {
	// `cfg` stores the configuration used by this operation.
	cfg indexerConfig
	// `db` stores the value associated with this record.
	db *DB
	// `client` stores the value associated with this record.
	client *http.Client

	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `status` stores the value associated with this record.
	status indexerStatus
}

type indexerStatus struct {
	// `Healthy` stores the value associated with this record.
	Healthy bool `json:"healthy"`
	// `State` stores the value associated with this record.
	State string `json:"state"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason,omitempty"`
	// `SourceRPC` stores the value associated with this record.
	SourceRPC string `json:"source_rpc"`
	// `IndexedHeight` stores the current position in the related collection.
	IndexedHeight uint64 `json:"indexed_height"`
	// `ArchiveHeight` stores the value associated with this record.
	ArchiveHeight uint64 `json:"archive_height"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height"`
	// `IndexLag` stores the current position in the related collection.
	IndexLag uint64 `json:"index_lag"`
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash"`
	// `ArchiveMode` stores the value associated with this record.
	ArchiveMode bool `json:"archive_mode"`
	// `LastIndexedHash` stores the digest used to identify or verify the related data.
	LastIndexedHash string `json:"last_indexed_hash,omitempty"`
	// `LastError` stores the error produced by this operation.
	LastError string `json:"last_error,omitempty"`
	// `LastChecked` stores the value associated with this record.
	LastChecked int64 `json:"last_checked"`
	// `LastIndexedAt` stores the value associated with this record.
	LastIndexedAt int64 `json:"last_indexed_at,omitempty"`
}

type indexedBlockRecord struct {
	// `Summary` stores the value associated with this record.
	Summary map[string]any `json:"summary"`
	// `Block` stores the synchronization state protecting shared data.
	Block map[string]any `json:"block"`
	// `Transactions` stores the transaction data handled by this operation.
	Transactions []map[string]any `json:"transactions"`
	// `ExecutionResults` stores the value associated with this record.
	ExecutionResults []any `json:"execution_results,omitempty"`
	// `Receipts` stores the value associated with this record.
	Receipts []any `json:"receipts,omitempty"`
	// `IndexedAt` stores the current position in the related collection.
	IndexedAt int64 `json:"indexed_at"`
}

type indexedTxRecord struct {
	// `TxID` stores the transaction data handled by this operation.
	TxID string `json:"tx_id"`
	// `State` stores the value associated with this record.
	State string `json:"state"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `TxIndex` stores the transaction data handled by this operation.
	TxIndex int `json:"tx_index"`
	// `Confirmations` stores the value associated with this record.
	Confirmations uint64 `json:"confirmations,omitempty"`
	// `Block` stores the synchronization state protecting shared data.
	Block map[string]any `json:"block,omitempty"`
	// `Tx` stores the transaction data handled by this operation.
	Tx map[string]any `json:"tx"`
	// `Receipt` stores the value associated with this record.
	Receipt any `json:"receipt,omitempty"`
	// `IndexedAt` stores the current position in the related collection.
	IndexedAt int64 `json:"indexed_at"`
}

// operatorIndexerCommand implements the operator indexer command helper.
func operatorIndexerCommand(args []string) error {
	if len(args) == 0 || strings.EqualFold(args[0], "help") {
		fmt.Println("Indexer:")
		fmt.Println("  indexer run --source http://127.0.0.1:26667 --listen 127.0.0.1:26780 --datadir runtime-data/indexer/INDEXER1")
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "run":
		return runIndexerCommand(args[1:])
	default:
		return fmt.Errorf("unknown indexer command %q", args[0])
	}
}

// runIndexerCommand implements the run indexer command helper.
func runIndexerCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("indexer run", flag.ContinueOnError)
	// `source` stores the value produced by this operation.
	source := fs.String("source", firstNonEmpty(os.Getenv("MSC_INDEXER_SOURCE_RPC"), indexerDefaultSource), "archive RPC source URL")
	// `listen` stores the value produced by this operation.
	listen := fs.String("listen", firstNonEmpty(os.Getenv("MSC_INDEXER_LISTEN"), indexerDefaultListen), "indexer HTTP listen address")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", firstNonEmpty(os.Getenv("MSC_INDEXER_DATADIR"), filepath.Join("runtime-data", "indexer", "INDEXER1")), "indexer data directory")
	// `chainID` stores the value produced by this operation.
	chainID := fs.String("chain-id", firstNonEmpty(os.Getenv("MSC_CHAIN_ID"), protocolChainID()), "expected chain id")
	// `genesisHash` stores the digest used to identify or verify the related data.
	genesisHash := fs.String("genesis-hash", firstNonEmpty(os.Getenv("MSC_GENESIS_HASH"), expectedGenesisHash()), "expected genesis hash")
	// `poll` stores the value produced by this operation.
	poll := fs.Duration("poll", 2*time.Second, "archive polling interval")
	// `allowFullSource` stores the value produced by this operation.
	allowFullSource := fs.Bool("allow-full-source", envBool("MSC_INDEXER_ALLOW_FULL_SOURCE"), "allow non-archive full-node source for local tests")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `cfg` stores the configuration used by this operation.
	cfg := indexerConfig{
		SourceRPC:       strings.TrimRight(strings.TrimSpace(*source), "/"),
		ListenAddr:      strings.TrimSpace(*listen),
		DataDir:         strings.TrimSpace(*dataDir),
		ChainID:         strings.TrimSpace(*chainID),
		GenesisHash:     strings.TrimSpace(*genesisHash),
		PollInterval:    *poll,
		AllowFullSource: *allowFullSource,
	}
	if cfg.SourceRPC == "" {
		return errors.New("source archive RPC is required")
	}
	if cfg.ListenAddr == "" {
		return errors.New("listen address is required")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	// `idx` and `err` store the error produced by this operation.
	idx, err := openExplorerIndexer(cfg)
	if err != nil {
		return err
	}
	defer idx.Close()

	// `ctx` and `stop` store the context controlling this operation.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return idx.Run(ctx)
}

// openExplorerIndexer implements the open explorer indexer helper.
func openExplorerIndexer(cfg indexerConfig) (*explorerIndexer, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join("runtime-data", "indexer", "INDEXER1")
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	// `db` and `err` store the error produced by this operation.
	db, err := openPebbleDB(filepath.Join(cfg.DataDir, "indexer.db"))
	if err != nil {
		return nil, err
	}
	// `idx` stores the current position in the related collection.
	idx := &explorerIndexer{
		cfg:    cfg,
		db:     db,
		client: &http.Client{Timeout: 8 * time.Second},
	}
	// `last` stores the value produced by this operation.
	last, _ := idx.getMetaUint(indexerMetaLastHeight)
	// `lastHash` stores the digest used to identify or verify the related data.
	lastHash, _ := idx.getMetaString(indexerMetaLastHash)
	idx.status = indexerStatus{
		Healthy:         false,
		State:           "starting",
		Reason:          "warming",
		SourceRPC:       cfg.SourceRPC,
		IndexedHeight:   last,
		LastIndexedHash: lastHash,
		ChainID:         cfg.ChainID,
		GenesisHash:     cfg.GenesisHash,
		LastChecked:     time.Now().Unix(),
	}
	return idx, nil
}

// Close implements the close helper.
func (idx *explorerIndexer) Close() error {
	if idx == nil || idx.db == nil {
		return nil
	}
	return idx.db.Close()
}

// Run implements the run helper.
func (idx *explorerIndexer) Run(ctx context.Context) error {
	// `mux` stores the synchronization state protecting shared data.
	mux := http.NewServeMux()
	idx.registerRoutes(mux)
	// `server` stores the value produced by this operation.
	server := &http.Server{Addr: idx.cfg.ListenAddr, Handler: mux}
	// `errCh` stores the error produced by this operation.
	errCh := make(chan error, 1)
	go func() {
		// `err` stores the error produced by this operation.
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()
	go idx.indexLoop(ctx)

	select {
	case <-ctx.Done():
		// `shutdownCtx` and `cancel` store the context controlling this operation.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// indexLoop implements the index loop helper.
func (idx *explorerIndexer) indexLoop(ctx context.Context) {
	idx.runIndexOnce()
	// `ticker` stores the value produced by this operation.
	ticker := time.NewTicker(idx.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			idx.runIndexOnce()
		}
	}
}

// runIndexOnce implements the run index once helper.
func (idx *explorerIndexer) runIndexOnce() {
	// `source` and `err` store the error produced by this operation.
	source, err := idx.probeSource()
	if err != nil {
		idx.updateStatus(func(st *indexerStatus) {
			st.Healthy = false
			st.State = "unhealthy"
			st.Reason = "source_probe_failed"
			st.LastError = err.Error()
			st.LastChecked = time.Now().Unix()
		})
		return
	}
	// `last` stores the value produced by this operation.
	last, _ := idx.getMetaUint(indexerMetaLastHeight)
	// `target` stores the value produced by this operation.
	target := source.FinalizedHeight
	if target == 0 || target > source.Height {
		target = source.Height
	}
	// `h` stores the value produced by this operation.
	for h := last + 1; h <= target; h++ {
		// `err` stores the error produced by this operation.
		if err := idx.IndexHeight(h, source.Height, source.FinalizedHeight); err != nil {
			idx.updateStatus(func(st *indexerStatus) {
				st.Healthy = false
				st.State = "warning"
				st.Reason = "indexing_failed"
				st.LastError = err.Error()
				st.ArchiveHeight = source.Height
				st.FinalizedHeight = source.FinalizedHeight
				st.IndexedHeight = h - 1
				st.IndexLag = heightLag(source.Height, h-1)
				st.ArchiveMode = source.ArchiveMode
				st.LastChecked = time.Now().Unix()
			})
			return
		}
	}
	last, _ = idx.getMetaUint(indexerMetaLastHeight)
	// `lastHash` stores the digest used to identify or verify the related data.
	lastHash, _ := idx.getMetaString(indexerMetaLastHash)
	idx.updateStatus(func(st *indexerStatus) {
		st.Healthy = source.ArchiveMode && heightLag(source.Height, last) <= 2
		st.State = map[bool]string{true: "healthy", false: "catching_up"}[st.Healthy]
		st.Reason = map[bool]string{true: "indexed", false: "index_lag"}[st.Healthy]
		st.LastError = ""
		st.ArchiveHeight = source.Height
		st.FinalizedHeight = source.FinalizedHeight
		st.IndexedHeight = last
		st.IndexLag = heightLag(source.Height, last)
		st.ChainID = source.ChainID
		st.GenesisHash = source.GenesisHash
		st.ArchiveMode = source.ArchiveMode
		st.LastIndexedHash = lastHash
		st.LastChecked = time.Now().Unix()
		if last > 0 {
			st.LastIndexedAt = time.Now().Unix()
		}
	})
}

type indexerSourceStatus struct {
	// `Height` stores the value associated with this record.
	Height uint64
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64
	// `ChainID` stores the value associated with this record.
	ChainID string
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string
	// `Role` stores the value associated with this record.
	Role string
	// `IsValidator` stores the current position in the related collection.
	IsValidator bool
	// `ArchiveMode` stores the value associated with this record.
	ArchiveMode bool
}

// probeSource implements the probe source helper.
func (idx *explorerIndexer) probeSource() (indexerSourceStatus, error) {
	// `statusMap` and `err` store the error produced by this operation.
	statusMap, err := idx.fetchMap("/status")
	if err != nil {
		return indexerSourceStatus{}, err
	}
	// `source` stores the value produced by this operation.
	source := indexerSourceStatus{
		Height:          mapUint(statusMap, "height"),
		FinalizedHeight: mapUint(statusMap, "finalized_height"),
		ChainID:         mapString(statusMap, "chain_id"),
		GenesisHash:     firstNonEmpty(mapString(statusMap, "genesis_hash"), mapString(statusMap, "expected_genesis_hash")),
		Role:            mapString(statusMap, "role"),
		IsValidator:     mapBool(statusMap, "is_validator"),
	}
	if source.FinalizedHeight == 0 {
		source.FinalizedHeight = source.Height
	}
	if source.IsValidator || strings.EqualFold(source.Role, "validator") {
		return source, errors.New("indexer source must not be validator RPC")
	}
	if idx.cfg.ChainID != "" && source.ChainID != "" && source.ChainID != idx.cfg.ChainID {
		return source, fmt.Errorf("wrong chain id: got %s want %s", source.ChainID, idx.cfg.ChainID)
	}
	if idx.cfg.GenesisHash != "" && source.GenesisHash != "" && !strings.EqualFold(source.GenesisHash, idx.cfg.GenesisHash) {
		return source, fmt.Errorf("wrong genesis hash: got %s want %s", source.GenesisHash, idx.cfg.GenesisHash)
	}
	// `policy` and `err` store the error produced by this operation.
	if policy, err := idx.fetchMap("/storage/policy"); err == nil {
		source.ArchiveMode = mapBool(policy, "archive_mode") || strings.EqualFold(mapString(policy, "profile"), storageProfileArchive)
	}
	if !source.ArchiveMode && !idx.cfg.AllowFullSource {
		return source, errors.New("indexer source is not archive_mode")
	}
	if source.Height == 0 {
		return source, errors.New("archive source height is zero")
	}
	return source, nil
}

// IndexHeight implements the index height helper.
func (idx *explorerIndexer) IndexHeight(height uint64, latestHeight uint64, finalizedHeight uint64) error {
	// `block` and `err` store the error produced by this operation.
	block, err := idx.fetchMap("/explorer/block?height=" + url.QueryEscape(strconv.FormatUint(height, 10)))
	if err != nil {
		return err
	}
	return idx.indexBlock(block, latestHeight, finalizedHeight)
}

// indexBlock implements the index block helper.
func (idx *explorerIndexer) indexBlock(block map[string]any, latestHeight uint64, _ uint64) error {
	// `height` stores the value produced by this operation.
	height := mapUint(block, "height")
	if height == 0 {
		height = mapUint(mapMap(block, "summary"), "height")
	}
	if height == 0 {
		return errors.New("block height missing")
	}
	// `hash` stores the digest used to identify or verify the related data.
	hash := firstNonEmpty(mapString(block, "hash"), mapString(mapMap(block, "summary"), "hash"))
	if hash == "" {
		return fmt.Errorf("block %d hash missing", height)
	}
	// `summary` stores the value produced by this operation.
	summary := mapMap(block, "summary")
	if len(summary) == 0 {
		summary = explorerSummaryFromBlockMap(block)
	}
	// `transactions` stores the transaction data handled by this operation.
	transactions := mapSlice(block, "transactions")
	// `receipts` stores the value produced by this operation.
	receipts := anySlice(block["receipts"])
	// `execResults` stores the value produced by this operation.
	execResults := anySlice(block["execution_results"])
	// `record` stores the value produced by this operation.
	record := indexedBlockRecord{
		Summary:          summary,
		Block:            block,
		Transactions:     transactions,
		ExecutionResults: execResults,
		Receipts:         receipts,
		IndexedAt:        time.Now().Unix(),
	}
	// `recordBytes` and `err` store the error produced by this operation.
	recordBytes, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return idx.db.Update(func(txn *Txn) error {
		// `err` stores the error produced by this operation.
		if err := txn.Set([]byte(blockHeightKey(height)), recordBytes); err != nil {
			return err
		}
		// `err` stores the error produced by this operation.
		if err := txn.Set([]byte(blockHashKey(hash)), []byte(strconv.FormatUint(height, 10))); err != nil {
			return err
		}
		// `i` and `txMap` track the transaction data handled by this operation.
		for i, txMap := range transactions {
			// `txID` stores the transaction data handled by this operation.
			txID := firstNonEmpty(mapString(txMap, "id"), mapString(txMap, "tx_id"))
			if txID == "" {
				continue
			}
			// `txRecord` stores the transaction data handled by this operation.
			txRecord := indexedTxRecord{
				TxID:          txID,
				State:         "confirmed",
				Height:        height,
				TxIndex:       i,
				Confirmations: confirmationsFor(latestHeight, height),
				Block:         summary,
				Tx:            txMap,
				IndexedAt:     time.Now().Unix(),
			}
			if i >= 0 && i < len(receipts) {
				txRecord.Receipt = receipts[i]
			}
			// `txBytes` and `err` store the error produced by this operation.
			txBytes, err := json.Marshal(txRecord)
			if err != nil {
				return err
			}
			// `err` stores the error produced by this operation.
			if err := txn.Set([]byte(txKey(txID)), txBytes); err != nil {
				return err
			}
			// `addr` tracks the address used by this operation.
			for _, addr := range txAddresses(txMap) {
				// `err` stores the error produced by this operation.
				if err := txn.Set([]byte(addressTxKey(addr, height, txID)), txBytes); err != nil {
					return err
				}
			}
			// `token` tracks the current values while iterating.
			for _, token := range txTokens(txMap) {
				// `err` stores the error produced by this operation.
				if err := txn.Set([]byte(tokenTxKey(token, height, txID)), txBytes); err != nil {
					return err
				}
			}
		}
		// `proposer` stores the value produced by this operation.
		if proposer := strings.TrimSpace(mapString(summary, "proposer")); proposer != "" {
			// `err` stores the error produced by this operation.
			if err := txn.Set([]byte(validatorBlockKey(proposer, height)), recordBytes); err != nil {
				return err
			}
		}
		// `err` stores the error produced by this operation.
		if err := txn.Set([]byte(indexerMetaLastHeight), []byte(strconv.FormatUint(height, 10))); err != nil {
			return err
		}
		return txn.Set([]byte(indexerMetaLastHash), []byte(hash))
	})
}

// registerRoutes implements the register routes helper.
func (idx *explorerIndexer) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", idx.handleHealthz)
	mux.HandleFunc("/indexer/status", idx.handleStatus)
	mux.HandleFunc("/indexer/search", idx.handleSearch)
	mux.HandleFunc("/indexer/blocks", idx.handleBlocks)
	mux.HandleFunc("/indexer/block", idx.handleBlock)
	mux.HandleFunc("/indexer/tx", idx.handleTx)
	mux.HandleFunc("/indexer/address", idx.handleAddress)
	mux.HandleFunc("/indexer/validators", idx.handleValidators)
	mux.HandleFunc("/indexer/tokens", idx.handleTokens)
}

// handleHealthz handles healthz.
func (idx *explorerIndexer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	// `st` stores the value produced by this operation.
	st := idx.Status()
	if st.Healthy {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "indexed_height": st.IndexedHeight})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": st.State, "reason": st.Reason})
}

// handleStatus handles status.
func (idx *explorerIndexer) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, idx.Status())
}

// handleSearch handles search.
func (idx *explorerIndexer) handleSearch(w http.ResponseWriter, r *http.Request) {
	// `q` stores the value produced by this operation.
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "q required"})
		return
	}
	// `h` and `err` store the error produced by this operation.
	if h, err := strconv.ParseUint(q, 10, 64); err == nil && h > 0 {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := idx.blockByHeight(h); ok {
			writeJSON(w, http.StatusOK, map[string]any{"type": "block", "query": q, "result": block})
			return
		}
	}
	// `block` and `ok` store whether the related condition is satisfied.
	if block, ok := idx.blockByHash(q); ok {
		writeJSON(w, http.StatusOK, map[string]any{"type": "block", "query": q, "result": block})
		return
	}
	// `tx` and `ok` store whether the related condition is satisfied.
	if tx, ok := idx.txByID(q); ok {
		writeJSON(w, http.StatusOK, map[string]any{"type": "tx", "query": q, "result": tx})
		return
	}
	if strings.HasPrefix(strings.ToUpper(q), "MSC") || strings.HasPrefix(strings.ToLower(q), "0x") {
		// `items` stores the current position in the related collection.
		items := idx.addressHistory(q, 25, "")
		if len(items) > 0 {
			writeJSON(w, http.StatusOK, map[string]any{"type": "address", "query": q, "result": map[string]any{"address": q, "txs": items}})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found", "query": q})
}

// handleBlocks handles blocks.
func (idx *explorerIndexer) handleBlocks(w http.ResponseWriter, r *http.Request) {
	// `limit` stores the value produced by this operation.
	limit := clampInt(queryInt(r, "limit", 40), 1, 200)
	// `fromHeight` stores the value produced by this operation.
	fromHeight := queryUint(r, "from_height", 0)
	if fromHeight == 0 {
		fromHeight, _ = idx.getMetaUint(indexerMetaLastHeight)
	}
	// `blocks` stores the block data handled by this operation.
	blocks := idx.blocksFrom(fromHeight, limit)
	// `st` stores the value produced by this operation.
	st := idx.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"latest_height":    st.IndexedHeight,
		"archive_height":   st.ArchiveHeight,
		"finalized_height": st.FinalizedHeight,
		"index_lag":        st.IndexLag,
		"from_height":      fromHeight,
		"limit":            limit,
		"blocks":           blocks,
		"source":           "indexer",
	})
}

// handleBlock handles block.
func (idx *explorerIndexer) handleBlock(w http.ResponseWriter, r *http.Request) {
	// `h` stores the value produced by this operation.
	if h := queryUint(r, "height", 0); h > 0 {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := idx.blockByHeight(h); ok {
			writeJSON(w, http.StatusOK, block)
			return
		}
	}
	// `hash` stores the digest used to identify or verify the related data.
	if hash := strings.TrimSpace(r.URL.Query().Get("hash")); hash != "" {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := idx.blockByHash(hash); ok {
			writeJSON(w, http.StatusOK, block)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "block not found"})
}

// handleTx handles tx.
func (idx *explorerIndexer) handleTx(w http.ResponseWriter, r *http.Request) {
	// `txID` stores the transaction data handled by this operation.
	txID := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("tx_id"), r.URL.Query().Get("id")))
	if txID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tx_id required"})
		return
	}
	// `tx` and `ok` store whether the related condition is satisfied.
	if tx, ok := idx.txByID(txID); ok {
		writeJSON(w, http.StatusOK, tx)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "tx not found"})
}

// handleAddress handles address.
func (idx *explorerIndexer) handleAddress(w http.ResponseWriter, r *http.Request) {
	// `address` stores the address used by this operation.
	address := strings.TrimSpace(r.URL.Query().Get("address"))
	if address == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "address required"})
		return
	}
	// `limit` stores the value produced by this operation.
	limit := clampInt(queryInt(r, "limit", 50), 1, 200)
	// `cursor` stores the value produced by this operation.
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	// `items` stores the current position in the related collection.
	items := idx.addressHistory(address, limit, cursor)
	// `nextCursor` stores the value produced by this operation.
	nextCursor := ""
	if len(items) == limit {
		// `last` and `ok` store whether the related condition is satisfied.
		if last, ok := items[len(items)-1].(map[string]any); ok {
			nextCursor = fmt.Sprintf("%020d:%s", mapUint(last, "height"), firstNonEmpty(mapString(last, "tx_id"), mapString(mapMap(last, "tx"), "id")))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"address":     address,
		"limit":       limit,
		"cursor":      cursor,
		"next_cursor": nextCursor,
		"txs":         items,
	})
}

// handleValidators handles validators.
func (idx *explorerIndexer) handleValidators(w http.ResponseWriter, r *http.Request) {
	// `counts` stores the measured quantity used by this operation.
	counts := map[string]int{}
	_ = idx.db.View(func(txn *Txn) error {
		// `it` stores the current position in the related collection.
		it := txn.NewIterator(IteratorOptions{Prefix: []byte("validator:")})
		defer it.Close()
		for it.Rewind(); it.ValidForPrefix([]byte("validator:")); it.Next() {
			// `key` stores the key used to access the related value.
			key := string(it.Item().Key())
			// `parts` stores the value produced by this operation.
			parts := strings.Split(key, ":")
			if len(parts) >= 3 {
				counts[parts[1]]++
			}
		}
		return nil
	})
	// `ids` stores the current position in the related collection.
	ids := make([]string, 0, len(counts))
	// `id` tracks the current position in the related collection.
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// `out` stores the result produced by this operation.
	out := make([]map[string]any, 0, len(ids))
	// `id` tracks the current position in the related collection.
	for _, id := range ids {
		out = append(out, map[string]any{"validator": strings.ToUpper(id), "blocks_produced": counts[id]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"validators": out})
}

// handleTokens handles tokens.
func (idx *explorerIndexer) handleTokens(w http.ResponseWriter, r *http.Request) {
	// `counts` stores the measured quantity used by this operation.
	counts := map[string]int{}
	_ = idx.db.View(func(txn *Txn) error {
		// `it` stores the current position in the related collection.
		it := txn.NewIterator(IteratorOptions{Prefix: []byte("token:")})
		defer it.Close()
		for it.Rewind(); it.ValidForPrefix([]byte("token:")); it.Next() {
			// `key` stores the key used to access the related value.
			key := string(it.Item().Key())
			// `parts` stores the value produced by this operation.
			parts := strings.Split(key, ":")
			if len(parts) >= 3 {
				counts[parts[1]]++
			}
		}
		return nil
	})
	// `ids` stores the current position in the related collection.
	ids := make([]string, 0, len(counts))
	// `id` tracks the current position in the related collection.
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// `out` stores the result produced by this operation.
	out := make([]map[string]any, 0, len(ids))
	// `id` tracks the current position in the related collection.
	for _, id := range ids {
		out = append(out, map[string]any{"token": strings.ToUpper(id), "event_count": counts[id]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

// fetchMap implements the fetch map helper.
func (idx *explorerIndexer) fetchMap(path string) (map[string]any, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// `resp` and `err` store the error produced by this operation.
	resp, err := idx.client.Get(idx.cfg.SourceRPC + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// `body` and `err` store the error produced by this operation.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %s", path, resp.Status)
	}
	// `out` stores the result produced by this operation.
	var out map[string]any
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	// `success` and `ok` store whether the related condition is satisfied.
	if success, ok := out["success"].(bool); ok {
		if !success {
			return nil, fmt.Errorf("%s returned unsuccessful response", path)
		}
		// `data` and `ok` store whether the related condition is satisfied.
		if data, ok := out["data"].(map[string]any); ok {
			return data, nil
		}
	}
	return out, nil
}

// Status implements the status helper.
func (idx *explorerIndexer) Status() indexerStatus {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.status
}

// updateStatus implements the update status helper.
func (idx *explorerIndexer) updateStatus(fn func(*indexerStatus)) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	fn(&idx.status)
}

// getMetaUint implements the get meta uint helper.
func (idx *explorerIndexer) getMetaUint(key string) (uint64, bool) {
	// `raw` and `ok` store whether the related condition is satisfied.
	raw, ok := idx.getMetaString(key)
	if !ok {
		return 0, false
	}
	// `v` and `err` store the error produced by this operation.
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// getMetaString implements the get meta string helper.
func (idx *explorerIndexer) getMetaString(key string) (string, bool) {
	// `out` stores the result produced by this operation.
	var out string
	// `err` stores the error produced by this operation.
	err := idx.db.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			out = string(val)
			return nil
		})
	})
	if err != nil {
		return "", false
	}
	return out, true
}

// blockByHeight implements the block by height helper.
func (idx *explorerIndexer) blockByHeight(height uint64) (map[string]any, bool) {
	// `rec` stores the value used by this operation.
	var rec indexedBlockRecord
	if !idx.readJSON(blockHeightKey(height), &rec) {
		return nil, false
	}
	// `out` stores the result produced by this operation.
	out := cloneMap(rec.Block)
	// `ok` stores whether the related condition is satisfied.
	if _, ok := out["summary"]; !ok {
		out["summary"] = rec.Summary
	}
	out["transactions"] = rec.Transactions
	out["execution_results"] = rec.ExecutionResults
	out["receipts"] = rec.Receipts
	out["source"] = "indexer"
	return out, true
}

// blockByHash implements the block by hash helper.
func (idx *explorerIndexer) blockByHash(hash string) (map[string]any, bool) {
	// `heightRaw` stores the value used by this operation.
	var heightRaw string
	// `err` stores the error produced by this operation.
	err := idx.db.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get([]byte(blockHashKey(hash)))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			heightRaw = string(val)
			return nil
		})
	})
	if err != nil {
		return nil, false
	}
	// `height` and `err` store the error produced by this operation.
	height, err := strconv.ParseUint(heightRaw, 10, 64)
	if err != nil {
		return nil, false
	}
	return idx.blockByHeight(height)
}

// txByID implements the tx by id helper.
func (idx *explorerIndexer) txByID(txID string) (map[string]any, bool) {
	// `out` stores the result produced by this operation.
	var out map[string]any
	if !idx.readJSON(txKey(txID), &out) {
		return nil, false
	}
	return out, true
}

// blocksFrom implements the blocks from helper.
func (idx *explorerIndexer) blocksFrom(fromHeight uint64, limit int) []explorerBlockSummary {
	if limit <= 0 {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make([]explorerBlockSummary, 0, limit)
	// `h` stores the value produced by this operation.
	for h := fromHeight; h > 0 && len(out) < limit; h-- {
		// `rec` stores the value used by this operation.
		var rec indexedBlockRecord
		if !idx.readJSON(blockHeightKey(h), &rec) {
			continue
		}
		out = append(out, summaryToExplorerBlockSummary(rec.Summary))
	}
	return out
}

// addressHistory implements the address history helper.
func (idx *explorerIndexer) addressHistory(address string, limit int, cursor string) []any {
	// `clean` stores the value produced by this operation.
	clean := indexAddress(address)
	if clean == "" || limit <= 0 {
		return nil
	}
	// `prefix` stores the value produced by this operation.
	prefix := []byte("addr:" + clean + ":")
	// `values` stores the value currently being processed.
	values := make([]struct {
		// `key` stores the key used to access the related value.
		key string
		// `val` stores the value currently being processed.
		val []byte
	}, 0)
	_ = idx.db.View(func(txn *Txn) error {
		// `it` stores the current position in the related collection.
		it := txn.NewIterator(IteratorOptions{Prefix: prefix})
		defer it.Close()
		for it.Rewind(); it.ValidForPrefix(prefix); it.Next() {
			// `item` stores the current position in the related collection.
			item := it.Item()
			values = append(values, struct {
				// `key` stores the key used to access the related value.
				key string
				// `val` stores the value currently being processed.
				val []byte
			}{key: string(item.Key()), val: append([]byte{}, item.val...)})
		}
		return nil
	})
	sort.Slice(values, func(i, j int) bool { return values[i].key > values[j].key })
	// `started` stores the value produced by this operation.
	started := cursor == ""
	// `out` stores the result produced by this operation.
	out := make([]any, 0, limit)
	// `entry` tracks the current values while iterating.
	for _, entry := range values {
		// `cursorToken` stores the value produced by this operation.
		cursorToken := strings.TrimPrefix(entry.key, string(prefix))
		if !started {
			if cursorToken == cursor {
				started = true
			}
			continue
		}
		if len(out) >= limit {
			break
		}
		// `tx` stores the transaction data handled by this operation.
		var tx map[string]any
		if json.Unmarshal(entry.val, &tx) == nil {
			out = append(out, tx)
		}
	}
	return out
}

// readJSON implements the read json helper.
func (idx *explorerIndexer) readJSON(key string, target any) bool {
	// `err` stores the error produced by this operation.
	err := idx.db.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, target)
		})
	})
	return err == nil
}

// blockHeightKey implements the block height key helper.
func blockHeightKey(height uint64) string {
	return fmt.Sprintf("block:height:%020d", height)
}

// blockHashKey implements the block hash key helper.
func blockHashKey(hash string) string {
	return "block:hash:" + strings.ToLower(strings.TrimSpace(hash))
}

// txKey implements the tx key helper.
func txKey(txID string) string {
	return "tx:" + strings.ToLower(strings.TrimSpace(txID))
}

// addressTxKey implements the address tx key helper.
func addressTxKey(address string, height uint64, txID string) string {
	return fmt.Sprintf("addr:%s:%020d:%s", indexAddress(address), height, strings.ToLower(strings.TrimSpace(txID)))
}

// tokenTxKey implements the token tx key helper.
func tokenTxKey(token string, height uint64, txID string) string {
	return fmt.Sprintf("token:%s:%020d:%s", strings.ToLower(strings.TrimSpace(token)), height, strings.ToLower(strings.TrimSpace(txID)))
}

// validatorBlockKey implements the validator block key helper.
func validatorBlockKey(validator string, height uint64) string {
	return fmt.Sprintf("validator:%s:%020d", strings.ToLower(strings.TrimSpace(validator)), height)
}

// indexAddress implements the index address helper.
func indexAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	return strings.ToLower(address)
}

// txAddresses implements the tx addresses helper.
func txAddresses(txMap map[string]any) []string {
	// `seen` stores the value produced by this operation.
	seen := map[string]struct{}{}
	// `out` stores the result produced by this operation.
	var out []string
	// `key` tracks the key used to access the related value.
	for _, key := range []string{"from", "to", "address", "wallet", "validator_wallet", "reward_wallet"} {
		// `value` stores the value currently being processed.
		value := strings.TrimSpace(mapString(txMap, key))
		if value == "" || value == "-" {
			continue
		}
		// `clean` stores the value produced by this operation.
		clean := indexAddress(value)
		if clean == "" {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, value)
	}
	return out
}

// txTokens implements the tx tokens helper.
func txTokens(txMap map[string]any) []string {
	// `seen` stores the value produced by this operation.
	seen := map[string]struct{}{}
	// `out` stores the result produced by this operation.
	var out []string
	// `key` tracks the key used to access the related value.
	for _, key := range []string{"coin", "dtl_token_id", "route_token_in", "route_token_out"} {
		// `value` stores the value currently being processed.
		value := strings.TrimSpace(mapString(txMap, key))
		if value == "" {
			continue
		}
		// `clean` stores the value produced by this operation.
		clean := strings.ToLower(value)
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		out = append(out, CoinSymbol)
	}
	return out
}

// confirmationsFor implements the confirmations for helper.
func confirmationsFor(latestHeight, height uint64) uint64 {
	if latestHeight >= height {
		return latestHeight - height + 1
	}
	return 0
}

// heightLag implements the height lag helper.
func heightLag(best, height uint64) uint64 {
	if best > height {
		return best - height
	}
	return 0
}

// explorerSummaryFromBlockMap implements the explorer summary from block map helper.
func explorerSummaryFromBlockMap(block map[string]any) map[string]any {
	// `keys` stores the key used to access the related value.
	keys := []string{
		"height", "hash", "prev_hash", "proposer", "type", "timestamp", "block_time",
		"tx_count", "execution_result_count", "receipt_count", "signature_count",
		"mempool_root", "state_root", "validator_set_hash", "validator_set_root",
		"validator_registry_hash", "next_validator_set_hash", "next_validator_set_root",
		"next_validator_set_height", "activation_height", "consensus_mode",
		"quorum_policy_version", "active_ready_count", "required_quorum", "strict_quorum",
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]any, len(keys))
	// `key` tracks the key used to access the related value.
	for _, key := range keys {
		// `value` and `ok` store whether the related condition is satisfied.
		if value, ok := block[key]; ok {
			out[key] = value
		}
	}
	// `ok` stores whether the related condition is satisfied.
	if _, ok := out["tx_count"]; !ok {
		out["tx_count"] = len(mapSlice(block, "transactions"))
	}
	return out
}

// summaryToExplorerBlockSummary implements the summary to explorer block summary helper.
func summaryToExplorerBlockSummary(summary map[string]any) explorerBlockSummary {
	return explorerBlockSummary{
		Height:                 mapUint(summary, "height"),
		Hash:                   mapString(summary, "hash"),
		PrevHash:               mapString(summary, "prev_hash"),
		Proposer:               mapString(summary, "proposer"),
		Type:                   mapString(summary, "type"),
		Timestamp:              int64(mapUint(summary, "timestamp")),
		TxCount:                int(mapUint(summary, "tx_count")),
		ExecutionResultCount:   int(mapUint(summary, "execution_result_count")),
		ReceiptCount:           int(mapUint(summary, "receipt_count")),
		SignatureCount:         int(mapUint(summary, "signature_count")),
		MempoolRoot:            mapString(summary, "mempool_root"),
		StateRoot:              mapString(summary, "state_root"),
		ValidatorSetHash:       mapString(summary, "validator_set_hash"),
		ValidatorSetRoot:       mapString(summary, "validator_set_root"),
		ValidatorRegistryHash:  mapString(summary, "validator_registry_hash"),
		NextValidatorSetHash:   mapString(summary, "next_validator_set_hash"),
		NextValidatorSetRoot:   mapString(summary, "next_validator_set_root"),
		NextValidatorSetHeight: mapUint(summary, "next_validator_set_height"),
		ActivationHeight:       mapUint(summary, "activation_height"),
		ConsensusMode:          mapString(summary, "consensus_mode"),
		QuorumPolicyVersion:    mapString(summary, "quorum_policy_version"),
		ActiveReadyCount:       int(mapUint(summary, "active_ready_count")),
		RequiredQuorum:         int(mapUint(summary, "required_quorum")),
		StrictQuorum:           int(mapUint(summary, "strict_quorum")),
	}
}

// writeJSON implements the write json helper.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// queryInt implements the query int helper.
func queryInt(r *http.Request, key string, fallback int) int {
	if r == nil {
		return fallback
	}
	// `raw` stores the value produced by this operation.
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	// `parsed` and `err` store the error produced by this operation.
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

// queryUint implements the query uint helper.
func queryUint(r *http.Request, key string, fallback uint64) uint64 {
	if r == nil {
		return fallback
	}
	// `raw` stores the value produced by this operation.
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	// `parsed` and `err` store the error produced by this operation.
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

// clampInt implements the clamp int helper.
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// mapString implements the map string helper.
func mapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	// `v` stores the value produced by this operation.
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case float64:
		if v == float64(uint64(v)) {
			return strconv.FormatUint(uint64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case uint64:
		return strconv.FormatUint(v, 10)
	default:
		return ""
	}
}

// mapUint implements the map uint helper.
func mapUint(m map[string]any, key string) uint64 {
	if m == nil {
		return 0
	}
	// `v` stores the value produced by this operation.
	switch v := m[key].(type) {
	case uint64:
		return v
	case uint:
		return uint64(v)
	case int:
		if v > 0 {
			return uint64(v)
		}
	case int64:
		if v > 0 {
			return uint64(v)
		}
	case float64:
		if v > 0 {
			return uint64(v)
		}
	case json.Number:
		// `u` stores the value produced by this operation.
		u, _ := strconv.ParseUint(v.String(), 10, 64)
		return u
	case string:
		// `u` stores the value produced by this operation.
		u, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		return u
	}
	return 0
}

// mapBool implements the map bool helper.
func mapBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	// `v` stores the value produced by this operation.
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true") || strings.TrimSpace(v) == "1"
	case float64:
		return v != 0
	case int:
		return v != 0
	}
	return false
}

// mapMap implements the map map helper.
func mapMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	// `out` and `ok` store whether the related condition is satisfied.
	if out, ok := m[key].(map[string]any); ok {
		return out
	}
	return nil
}

// mapSlice implements the map slice helper.
func mapSlice(m map[string]any, key string) []map[string]any {
	// `raw` stores the value produced by this operation.
	raw := anySlice(m[key])
	// `out` stores the result produced by this operation.
	out := make([]map[string]any, 0, len(raw))
	// `item` tracks the current position in the related collection.
	for _, item := range raw {
		// `mm` and `ok` store whether the related condition is satisfied.
		if mm, ok := item.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}

// anySlice implements the any slice helper.
func anySlice(value any) []any {
	// `v` stores the value produced by this operation.
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		// `out` stores the result produced by this operation.
		out := make([]any, 0, len(v))
		// `item` tracks the current position in the related collection.
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

// cloneMap clones map.
func cloneMap(m map[string]any) map[string]any {
	// `out` stores the result produced by this operation.
	out := make(map[string]any, len(m))
	// `k` and `v` track the current values while iterating.
	for k, v := range m {
		out[k] = v
	}
	return out
}
