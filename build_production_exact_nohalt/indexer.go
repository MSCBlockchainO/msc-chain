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
	indexerDefaultListen = "127.0.0.1:26780"
	indexerDefaultSource = "http://127.0.0.1:26667"

	indexerMetaLastHeight = "meta:last_height"
	indexerMetaLastHash   = "meta:last_hash"
)

type indexerConfig struct {
	SourceRPC       string
	ListenAddr      string
	DataDir         string
	ChainID         string
	GenesisHash     string
	PollInterval    time.Duration
	AllowFullSource bool
}

type explorerIndexer struct {
	cfg    indexerConfig
	db     *DB
	client *http.Client

	mu     sync.RWMutex
	status indexerStatus
}

type indexerStatus struct {
	Healthy         bool   `json:"healthy"`
	State           string `json:"state"`
	Reason          string `json:"reason,omitempty"`
	SourceRPC       string `json:"source_rpc"`
	IndexedHeight   uint64 `json:"indexed_height"`
	ArchiveHeight   uint64 `json:"archive_height"`
	FinalizedHeight uint64 `json:"finalized_height"`
	IndexLag        uint64 `json:"index_lag"`
	ChainID         string `json:"chain_id"`
	GenesisHash     string `json:"genesis_hash"`
	ArchiveMode     bool   `json:"archive_mode"`
	LastIndexedHash string `json:"last_indexed_hash,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	LastChecked     int64  `json:"last_checked"`
	LastIndexedAt   int64  `json:"last_indexed_at,omitempty"`
}

type indexedBlockRecord struct {
	Summary          map[string]any   `json:"summary"`
	Block            map[string]any   `json:"block"`
	Transactions     []map[string]any `json:"transactions"`
	ExecutionResults []any            `json:"execution_results,omitempty"`
	Receipts         []any            `json:"receipts,omitempty"`
	IndexedAt        int64            `json:"indexed_at"`
}

type indexedTxRecord struct {
	TxID          string         `json:"tx_id"`
	State         string         `json:"state"`
	Height        uint64         `json:"height"`
	TxIndex       int            `json:"tx_index"`
	Confirmations uint64         `json:"confirmations,omitempty"`
	Block         map[string]any `json:"block,omitempty"`
	Tx            map[string]any `json:"tx"`
	Receipt       any            `json:"receipt,omitempty"`
	IndexedAt     int64          `json:"indexed_at"`
}

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

func runIndexerCommand(args []string) error {
	fs := flag.NewFlagSet("indexer run", flag.ContinueOnError)
	source := fs.String("source", firstNonEmpty(os.Getenv("MSC_INDEXER_SOURCE_RPC"), indexerDefaultSource), "archive RPC source URL")
	listen := fs.String("listen", firstNonEmpty(os.Getenv("MSC_INDEXER_LISTEN"), indexerDefaultListen), "indexer HTTP listen address")
	dataDir := fs.String("datadir", firstNonEmpty(os.Getenv("MSC_INDEXER_DATADIR"), filepath.Join("runtime-data", "indexer", "INDEXER1")), "indexer data directory")
	chainID := fs.String("chain-id", firstNonEmpty(os.Getenv("MSC_CHAIN_ID"), ChainID), "expected chain id")
	genesisHash := fs.String("genesis-hash", firstNonEmpty(os.Getenv("MSC_GENESIS_HASH"), expectedGenesisHash()), "expected genesis hash")
	poll := fs.Duration("poll", 2*time.Second, "archive polling interval")
	allowFullSource := fs.Bool("allow-full-source", envBool("MSC_INDEXER_ALLOW_FULL_SOURCE"), "allow non-archive full-node source for local tests")
	if err := fs.Parse(args); err != nil {
		return err
	}
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
	idx, err := openExplorerIndexer(cfg)
	if err != nil {
		return err
	}
	defer idx.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return idx.Run(ctx)
}

func openExplorerIndexer(cfg indexerConfig) (*explorerIndexer, error) {
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Join("runtime-data", "indexer", "INDEXER1")
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	db, err := openPebbleDB(filepath.Join(cfg.DataDir, "indexer.db"))
	if err != nil {
		return nil, err
	}
	idx := &explorerIndexer{
		cfg:    cfg,
		db:     db,
		client: &http.Client{Timeout: 8 * time.Second},
	}
	last, _ := idx.getMetaUint(indexerMetaLastHeight)
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

func (idx *explorerIndexer) Close() error {
	if idx == nil || idx.db == nil {
		return nil
	}
	return idx.db.Close()
}

func (idx *explorerIndexer) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	idx.registerRoutes(mux)
	server := &http.Server{Addr: idx.cfg.ListenAddr, Handler: mux}
	errCh := make(chan error, 1)
	go func() {
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
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (idx *explorerIndexer) indexLoop(ctx context.Context) {
	idx.runIndexOnce()
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

func (idx *explorerIndexer) runIndexOnce() {
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
	last, _ := idx.getMetaUint(indexerMetaLastHeight)
	target := source.FinalizedHeight
	if target == 0 || target > source.Height {
		target = source.Height
	}
	for h := last + 1; h <= target; h++ {
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
	Height          uint64
	FinalizedHeight uint64
	ChainID         string
	GenesisHash     string
	Role            string
	IsValidator     bool
	ArchiveMode     bool
}

func (idx *explorerIndexer) probeSource() (indexerSourceStatus, error) {
	statusMap, err := idx.fetchMap("/status")
	if err != nil {
		return indexerSourceStatus{}, err
	}
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

func (idx *explorerIndexer) IndexHeight(height uint64, latestHeight uint64, finalizedHeight uint64) error {
	block, err := idx.fetchMap("/explorer/block?height=" + url.QueryEscape(strconv.FormatUint(height, 10)))
	if err != nil {
		return err
	}
	return idx.indexBlock(block, latestHeight, finalizedHeight)
}

func (idx *explorerIndexer) indexBlock(block map[string]any, latestHeight uint64, finalizedHeight uint64) error {
	height := mapUint(block, "height")
	if height == 0 {
		height = mapUint(mapMap(block, "summary"), "height")
	}
	if height == 0 {
		return errors.New("block height missing")
	}
	hash := firstNonEmpty(mapString(block, "hash"), mapString(mapMap(block, "summary"), "hash"))
	if hash == "" {
		return fmt.Errorf("block %d hash missing", height)
	}
	summary := mapMap(block, "summary")
	if len(summary) == 0 {
		summary = explorerSummaryFromBlockMap(block)
	}
	transactions := mapSlice(block, "transactions")
	receipts := anySlice(block["receipts"])
	execResults := anySlice(block["execution_results"])
	record := indexedBlockRecord{
		Summary:          summary,
		Block:            block,
		Transactions:     transactions,
		ExecutionResults: execResults,
		Receipts:         receipts,
		IndexedAt:        time.Now().Unix(),
	}
	recordBytes, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return idx.db.Update(func(txn *Txn) error {
		if err := txn.Set([]byte(blockHeightKey(height)), recordBytes); err != nil {
			return err
		}
		if err := txn.Set([]byte(blockHashKey(hash)), []byte(strconv.FormatUint(height, 10))); err != nil {
			return err
		}
		for i, txMap := range transactions {
			txID := firstNonEmpty(mapString(txMap, "id"), mapString(txMap, "tx_id"))
			if txID == "" {
				continue
			}
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
			txBytes, err := json.Marshal(txRecord)
			if err != nil {
				return err
			}
			if err := txn.Set([]byte(txKey(txID)), txBytes); err != nil {
				return err
			}
			for _, addr := range txAddresses(txMap) {
				if err := txn.Set([]byte(addressTxKey(addr, height, txID)), txBytes); err != nil {
					return err
				}
			}
			for _, token := range txTokens(txMap) {
				if err := txn.Set([]byte(tokenTxKey(token, height, txID)), txBytes); err != nil {
					return err
				}
			}
		}
		if proposer := strings.TrimSpace(mapString(summary, "proposer")); proposer != "" {
			if err := txn.Set([]byte(validatorBlockKey(proposer, height)), recordBytes); err != nil {
				return err
			}
		}
		if err := txn.Set([]byte(indexerMetaLastHeight), []byte(strconv.FormatUint(height, 10))); err != nil {
			return err
		}
		return txn.Set([]byte(indexerMetaLastHash), []byte(hash))
	})
}

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

func (idx *explorerIndexer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	st := idx.Status()
	if st.Healthy {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "indexed_height": st.IndexedHeight})
		return
	}
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": st.State, "reason": st.Reason})
}

func (idx *explorerIndexer) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, idx.Status())
}

func (idx *explorerIndexer) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "q required"})
		return
	}
	if h, err := strconv.ParseUint(q, 10, 64); err == nil && h > 0 {
		if block, ok := idx.blockByHeight(h); ok {
			writeJSON(w, http.StatusOK, map[string]any{"type": "block", "query": q, "result": block})
			return
		}
	}
	if block, ok := idx.blockByHash(q); ok {
		writeJSON(w, http.StatusOK, map[string]any{"type": "block", "query": q, "result": block})
		return
	}
	if tx, ok := idx.txByID(q); ok {
		writeJSON(w, http.StatusOK, map[string]any{"type": "tx", "query": q, "result": tx})
		return
	}
	if strings.HasPrefix(strings.ToUpper(q), "MSC") || strings.HasPrefix(strings.ToLower(q), "0x") {
		items := idx.addressHistory(q, 25, "")
		if len(items) > 0 {
			writeJSON(w, http.StatusOK, map[string]any{"type": "address", "query": q, "result": map[string]any{"address": q, "txs": items}})
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found", "query": q})
}

func (idx *explorerIndexer) handleBlocks(w http.ResponseWriter, r *http.Request) {
	limit := clampInt(queryInt(r, "limit", 40), 1, 200)
	fromHeight := queryUint(r, "from_height", 0)
	if fromHeight == 0 {
		fromHeight, _ = idx.getMetaUint(indexerMetaLastHeight)
	}
	blocks := idx.blocksFrom(fromHeight, limit)
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

func (idx *explorerIndexer) handleBlock(w http.ResponseWriter, r *http.Request) {
	if h := queryUint(r, "height", 0); h > 0 {
		if block, ok := idx.blockByHeight(h); ok {
			writeJSON(w, http.StatusOK, block)
			return
		}
	}
	if hash := strings.TrimSpace(r.URL.Query().Get("hash")); hash != "" {
		if block, ok := idx.blockByHash(hash); ok {
			writeJSON(w, http.StatusOK, block)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "block not found"})
}

func (idx *explorerIndexer) handleTx(w http.ResponseWriter, r *http.Request) {
	txID := strings.TrimSpace(firstNonEmpty(r.URL.Query().Get("tx_id"), r.URL.Query().Get("id")))
	if txID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "tx_id required"})
		return
	}
	if tx, ok := idx.txByID(txID); ok {
		writeJSON(w, http.StatusOK, tx)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "tx not found"})
}

func (idx *explorerIndexer) handleAddress(w http.ResponseWriter, r *http.Request) {
	address := strings.TrimSpace(r.URL.Query().Get("address"))
	if address == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "address required"})
		return
	}
	limit := clampInt(queryInt(r, "limit", 50), 1, 200)
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	items := idx.addressHistory(address, limit, cursor)
	nextCursor := ""
	if len(items) == limit {
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

func (idx *explorerIndexer) handleValidators(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	_ = idx.db.View(func(txn *Txn) error {
		it := txn.NewIterator(IteratorOptions{Prefix: []byte("validator:")})
		defer it.Close()
		for it.Rewind(); it.ValidForPrefix([]byte("validator:")); it.Next() {
			key := string(it.Item().Key())
			parts := strings.Split(key, ":")
			if len(parts) >= 3 {
				counts[parts[1]]++
			}
		}
		return nil
	})
	ids := make([]string, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"validator": strings.ToUpper(id), "blocks_produced": counts[id]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"validators": out})
}

func (idx *explorerIndexer) handleTokens(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	_ = idx.db.View(func(txn *Txn) error {
		it := txn.NewIterator(IteratorOptions{Prefix: []byte("token:")})
		defer it.Close()
		for it.Rewind(); it.ValidForPrefix([]byte("token:")); it.Next() {
			key := string(it.Item().Key())
			parts := strings.Split(key, ":")
			if len(parts) >= 3 {
				counts[parts[1]]++
			}
		}
		return nil
	})
	ids := make([]string, 0, len(counts))
	for id := range counts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		out = append(out, map[string]any{"token": strings.ToUpper(id), "event_count": counts[id]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": out})
}

func (idx *explorerIndexer) fetchMap(path string) (map[string]any, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	resp, err := idx.client.Get(idx.cfg.SourceRPC + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s returned %s", path, resp.Status)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if success, ok := out["success"].(bool); ok {
		if !success {
			return nil, fmt.Errorf("%s returned unsuccessful response", path)
		}
		if data, ok := out["data"].(map[string]any); ok {
			return data, nil
		}
	}
	return out, nil
}

func (idx *explorerIndexer) Status() indexerStatus {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.status
}

func (idx *explorerIndexer) updateStatus(fn func(*indexerStatus)) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	fn(&idx.status)
}

func (idx *explorerIndexer) getMetaUint(key string) (uint64, bool) {
	raw, ok := idx.getMetaString(key)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func (idx *explorerIndexer) getMetaString(key string) (string, bool) {
	var out string
	err := idx.db.View(func(txn *Txn) error {
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

func (idx *explorerIndexer) blockByHeight(height uint64) (map[string]any, bool) {
	var rec indexedBlockRecord
	if !idx.readJSON(blockHeightKey(height), &rec) {
		return nil, false
	}
	out := cloneMap(rec.Block)
	if _, ok := out["summary"]; !ok {
		out["summary"] = rec.Summary
	}
	out["transactions"] = rec.Transactions
	out["execution_results"] = rec.ExecutionResults
	out["receipts"] = rec.Receipts
	out["source"] = "indexer"
	return out, true
}

func (idx *explorerIndexer) blockByHash(hash string) (map[string]any, bool) {
	var heightRaw string
	err := idx.db.View(func(txn *Txn) error {
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
	height, err := strconv.ParseUint(heightRaw, 10, 64)
	if err != nil {
		return nil, false
	}
	return idx.blockByHeight(height)
}

func (idx *explorerIndexer) txByID(txID string) (map[string]any, bool) {
	var out map[string]any
	if !idx.readJSON(txKey(txID), &out) {
		return nil, false
	}
	return out, true
}

func (idx *explorerIndexer) blocksFrom(fromHeight uint64, limit int) []explorerBlockSummary {
	if limit <= 0 {
		return nil
	}
	out := make([]explorerBlockSummary, 0, limit)
	for h := fromHeight; h > 0 && len(out) < limit; h-- {
		var rec indexedBlockRecord
		if !idx.readJSON(blockHeightKey(h), &rec) {
			continue
		}
		out = append(out, summaryToExplorerBlockSummary(rec.Summary))
	}
	return out
}

func (idx *explorerIndexer) addressHistory(address string, limit int, cursor string) []any {
	clean := indexAddress(address)
	if clean == "" || limit <= 0 {
		return nil
	}
	prefix := []byte("addr:" + clean + ":")
	values := make([]struct {
		key string
		val []byte
	}, 0)
	_ = idx.db.View(func(txn *Txn) error {
		it := txn.NewIterator(IteratorOptions{Prefix: prefix})
		defer it.Close()
		for it.Rewind(); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			values = append(values, struct {
				key string
				val []byte
			}{key: string(item.Key()), val: append([]byte{}, item.val...)})
		}
		return nil
	})
	sort.Slice(values, func(i, j int) bool { return values[i].key > values[j].key })
	started := cursor == ""
	out := make([]any, 0, limit)
	for _, entry := range values {
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
		var tx map[string]any
		if json.Unmarshal(entry.val, &tx) == nil {
			out = append(out, tx)
		}
	}
	return out
}

func (idx *explorerIndexer) readJSON(key string, target any) bool {
	err := idx.db.View(func(txn *Txn) error {
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

func blockHeightKey(height uint64) string {
	return fmt.Sprintf("block:height:%020d", height)
}

func blockHashKey(hash string) string {
	return "block:hash:" + strings.ToLower(strings.TrimSpace(hash))
}

func txKey(txID string) string {
	return "tx:" + strings.ToLower(strings.TrimSpace(txID))
}

func addressTxKey(address string, height uint64, txID string) string {
	return fmt.Sprintf("addr:%s:%020d:%s", indexAddress(address), height, strings.ToLower(strings.TrimSpace(txID)))
}

func tokenTxKey(token string, height uint64, txID string) string {
	return fmt.Sprintf("token:%s:%020d:%s", strings.ToLower(strings.TrimSpace(token)), height, strings.ToLower(strings.TrimSpace(txID)))
}

func validatorBlockKey(validator string, height uint64) string {
	return fmt.Sprintf("validator:%s:%020d", strings.ToLower(strings.TrimSpace(validator)), height)
}

func indexAddress(address string) string {
	address = strings.TrimSpace(address)
	if address == "" {
		return ""
	}
	return strings.ToLower(address)
}

func txAddresses(txMap map[string]any) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, key := range []string{"from", "to", "address", "wallet", "validator_wallet", "reward_wallet"} {
		value := strings.TrimSpace(mapString(txMap, key))
		if value == "" || value == "-" {
			continue
		}
		clean := indexAddress(value)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, value)
	}
	return out
}

func txTokens(txMap map[string]any) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, key := range []string{"coin", "dtl_token_id", "route_token_in", "route_token_out"} {
		value := strings.TrimSpace(mapString(txMap, key))
		if value == "" {
			continue
		}
		clean := strings.ToLower(value)
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

func confirmationsFor(latestHeight, height uint64) uint64 {
	if latestHeight >= height {
		return latestHeight - height + 1
	}
	return 0
}

func heightLag(best, height uint64) uint64 {
	if best > height {
		return best - height
	}
	return 0
}

func explorerSummaryFromBlockMap(block map[string]any) map[string]any {
	keys := []string{
		"height", "hash", "prev_hash", "proposer", "type", "timestamp", "block_time",
		"tx_count", "execution_result_count", "receipt_count", "signature_count",
		"mempool_root", "state_root", "validator_set_hash", "validator_set_root",
		"validator_registry_hash", "next_validator_set_hash", "next_validator_set_root",
		"next_validator_set_height", "activation_height", "consensus_mode",
		"quorum_policy_version", "active_ready_count", "required_quorum", "strict_quorum",
	}
	out := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := block[key]; ok {
			out[key] = value
		}
	}
	if _, ok := out["tx_count"]; !ok {
		out["tx_count"] = len(mapSlice(block, "transactions"))
	}
	return out
}

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

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func queryInt(r *http.Request, key string, fallback int) int {
	if r == nil {
		return fallback
	}
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func queryUint(r *http.Request, key string, fallback uint64) uint64 {
	if r == nil {
		return fallback
	}
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func mapString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	case json.Number:
		return v.String()
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

func mapUint(m map[string]any, key string) uint64 {
	if m == nil {
		return 0
	}
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
		u, _ := strconv.ParseUint(v.String(), 10, 64)
		return u
	case string:
		u, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		return u
	}
	return 0
}

func mapBool(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
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

func mapMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if out, ok := m[key].(map[string]any); ok {
		return out
	}
	return nil
}

func mapSlice(m map[string]any, key string) []map[string]any {
	raw := anySlice(m[key])
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if mm, ok := item.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}

func anySlice(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
