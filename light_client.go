package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const (
	LightProtocolVersion = "msc-light-v1"
	lightMaxHeaderLimit  = 512
)

type LightHeader struct {
	Version                   string                     `json:"version"`
	ChainID                   string                     `json:"chain_id"`
	Height                    uint64                     `json:"height"`
	BlockHash                 string                     `json:"block_hash"`
	PrevHash                  string                     `json:"prev_hash"`
	StateRoot                 string                     `json:"state_root"`
	StateMerkleRoot           string                     `json:"state_merkle_root,omitempty"`
	TxRoot                    string                     `json:"tx_root"`
	ReceiptRoot               string                     `json:"receipt_root,omitempty"`
	ValidatorSetHash          string                     `json:"validator_set_hash"`
	ValidatorSetRoot          string                     `json:"validator_set_root,omitempty"`
	ValidatorRegistryHash     string                     `json:"validator_registry_hash,omitempty"`
	NextValidatorSetHash      string                     `json:"next_validator_set_hash,omitempty"`
	NextValidatorSetRoot      string                     `json:"next_validator_set_root,omitempty"`
	FinalityRoot              string                     `json:"finality_root,omitempty"`
	EpochAnchorHash           string                     `json:"epoch_anchor_hash,omitempty"`
	PreviousEpochAnchorHash   string                     `json:"previous_epoch_anchor_hash,omitempty"`
	FinalizedHeight           uint64                     `json:"finalized_height,omitempty"`
	FinalizedStateRoot        string                     `json:"finalized_state_root,omitempty"`
	FinalizedValidatorSetHash string                     `json:"finalized_validator_set_hash,omitempty"`
	FinalizedValidatorSetRoot string                     `json:"finalized_validator_set_root,omitempty"`
	ConsensusMode             string                     `json:"consensus_mode,omitempty"`
	QuorumPolicyVersion       string                     `json:"quorum_policy_version,omitempty"`
	ActiveReadyCount          int                        `json:"active_ready_count,omitempty"`
	RequiredQuorum            int                        `json:"required_quorum,omitempty"`
	StrictQuorum              int                        `json:"strict_quorum,omitempty"`
	Proposer                  string                     `json:"proposer,omitempty"`
	Timestamp                 int64                      `json:"timestamp,omitempty"`
	FinalityCertificate       *FinalizedEpochCertificate `json:"finality_certificate,omitempty"`
}

type LightMerkleSibling struct {
	Position string `json:"position"`
	Hash     string `json:"hash"`
}

type LightMerkleProof struct {
	Version     string               `json:"version"`
	Domain      string               `json:"domain"`
	Root        string               `json:"root"`
	LeafKey     string               `json:"leaf_key"`
	LeafValue   string               `json:"leaf_value"`
	LeafHash    string               `json:"leaf_hash"`
	LeafIndex   int                  `json:"leaf_index"`
	TotalLeaves int                  `json:"total_leaves"`
	Siblings    []LightMerkleSibling `json:"siblings"`
}

type LightProofResponse struct {
	Version     string           `json:"version"`
	ProofType   string           `json:"proof_type"`
	Trusted     bool             `json:"trusted"`
	TrustSource string           `json:"trust_source"`
	Header      LightHeader      `json:"header"`
	Proof       LightMerkleProof `json:"proof"`
	Value       any              `json:"value,omitempty"`
	VerifyHint  string           `json:"verify_hint"`
}

type lightMerkleLeaf struct {
	Key   string
	Value string
	Hash  string
}

func lightHashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func normalizeLightHexHash(hash string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if len(hash) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return ""
	}
	return hash
}

func lightLeafHash(leaf lightMerkleLeaf) string {
	if hash := normalizeLightHexHash(leaf.Hash); hash != "" {
		return hash
	}
	return lightHashString(leaf.Value)
}

func buildLightMerkleProof(domain string, leaves []lightMerkleLeaf, targetKey string) (LightMerkleProof, bool) {
	targetKey = strings.TrimSpace(targetKey)
	if targetKey == "" || len(leaves) == 0 {
		return LightMerkleProof{}, false
	}

	hashes := make([]string, 0, len(leaves))
	targetIndex := -1
	targetValue := ""
	for i, leaf := range leaves {
		hash := lightLeafHash(leaf)
		if normalizeLightHexHash(hash) == "" {
			return LightMerkleProof{}, false
		}
		hashes = append(hashes, hash)
		if leaf.Key == targetKey {
			targetIndex = i
			targetValue = leaf.Value
		}
	}
	if targetIndex < 0 {
		return LightMerkleProof{}, false
	}

	root := merkleRootFromHexLeaves(hashes)
	level := append([]string(nil), hashes...)
	index := targetIndex
	siblings := make([]LightMerkleSibling, 0)
	for len(level) > 1 {
		siblingIndex := index ^ 1
		position := "right"
		if index%2 == 1 {
			position = "left"
		}
		if siblingIndex >= len(level) {
			siblingIndex = index
		}
		siblings = append(siblings, LightMerkleSibling{
			Position: position,
			Hash:     level[siblingIndex],
		})

		next := make([]string, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			leftBytes, lerr := hex.DecodeString(left)
			rightBytes, rerr := hex.DecodeString(right)
			if lerr != nil || rerr != nil {
				return LightMerkleProof{}, false
			}
			pair := append(append([]byte{}, leftBytes...), rightBytes...)
			sum := sha256.Sum256(pair)
			next = append(next, hex.EncodeToString(sum[:]))
		}
		level = next
		index /= 2
	}

	return LightMerkleProof{
		Version:     LightProtocolVersion,
		Domain:      domain,
		Root:        root,
		LeafKey:     targetKey,
		LeafValue:   targetValue,
		LeafHash:    hashes[targetIndex],
		LeafIndex:   targetIndex,
		TotalLeaves: len(hashes),
		Siblings:    siblings,
	}, true
}

func VerifyLightMerkleProof(proof LightMerkleProof) bool {
	root := normalizeLightHexHash(proof.Root)
	leafHash := normalizeLightHexHash(proof.LeafHash)
	if root == "" || leafHash == "" || proof.TotalLeaves <= 0 || proof.LeafIndex < 0 || proof.LeafIndex >= proof.TotalLeaves {
		return false
	}
	if proof.LeafValue != "" && lightHashString(proof.LeafValue) != leafHash {
		return false
	}
	current := leafHash
	for _, sibling := range proof.Siblings {
		siblingHash := normalizeLightHexHash(sibling.Hash)
		if siblingHash == "" {
			return false
		}
		currentBytes, cerr := hex.DecodeString(current)
		siblingBytes, serr := hex.DecodeString(siblingHash)
		if cerr != nil || serr != nil {
			return false
		}
		var pair []byte
		switch strings.ToLower(strings.TrimSpace(sibling.Position)) {
		case "left":
			pair = append(append([]byte{}, siblingBytes...), currentBytes...)
		case "right":
			pair = append(append([]byte{}, currentBytes...), siblingBytes...)
		default:
			return false
		}
		sum := sha256.Sum256(pair)
		current = hex.EncodeToString(sum[:])
	}
	return strings.EqualFold(current, root)
}

func VerifyLightHeaderChain(headers []LightHeader) error {
	if len(headers) == 0 {
		return errors.New("light header chain empty")
	}
	for i := 1; i < len(headers); i++ {
		prev := headers[i-1]
		curr := headers[i]
		if curr.Height != prev.Height+1 {
			return fmt.Errorf("light header height gap at index %d: got=%d want=%d", i, curr.Height, prev.Height+1)
		}
		if strings.TrimSpace(curr.PrevHash) == "" || !strings.EqualFold(strings.TrimSpace(curr.PrevHash), strings.TrimSpace(prev.BlockHash)) {
			return fmt.Errorf("light header prev_hash mismatch at height %d", curr.Height)
		}
	}
	return nil
}

func VerifyLightStateProof(header LightHeader, proof LightMerkleProof) bool {
	if strings.TrimSpace(header.StateMerkleRoot) == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(header.StateMerkleRoot), strings.TrimSpace(proof.Root)) {
		return false
	}
	return VerifyLightMerkleProof(proof)
}

func VerifyLightReceiptProof(header LightHeader, proof LightMerkleProof) bool {
	if strings.TrimSpace(header.ReceiptRoot) == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(header.ReceiptRoot), strings.TrimSpace(proof.Root)) {
		return false
	}
	return VerifyLightMerkleProof(proof)
}

func canonicalLedgerStateLeaves(ledger Ledger) []lightMerkleLeaf {
	material := canonicalLedgerHashMaterial(ledger)
	if strings.TrimSpace(material) == "" {
		return nil
	}
	parts := strings.Split(material, ";")
	leaves := make([]lightMerkleLeaf, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key := part
		if idx := strings.Index(part, "="); idx >= 0 {
			key = part[:idx]
		}
		leaves = append(leaves, lightMerkleLeaf{
			Key:   key,
			Value: part,
		})
	}
	return leaves
}

func blockReceiptLeaves(block Block) []lightMerkleLeaf {
	if len(block.Receipts) == 0 {
		return nil
	}
	leaves := make([]lightMerkleLeaf, 0, len(block.Receipts))
	for i, receipt := range block.Receipts {
		value := fmt.Sprintf("%d|%s|%s|%s",
			i,
			strings.TrimSpace(receipt.TxHash),
			strings.TrimSpace(receipt.PreStateHash),
			strings.TrimSpace(receipt.PostStateHash),
		)
		key := strings.TrimSpace(receipt.TxHash)
		if key == "" {
			key = strconv.Itoa(i)
		}
		leaves = append(leaves, lightMerkleLeaf{
			Key:   key,
			Value: value,
		})
	}
	return leaves
}

func (s *Server) lightBlockByHeight(height uint64) (Block, bool) {
	if s == nil || s.Node == nil {
		return Block{}, false
	}
	if s.Node.Blockchain != nil {
		if block, ok := s.Node.Blockchain.GetBlock(height); ok {
			return block, true
		}
	}
	return s.Node.LoadBlock(int(height))
}

func lightHeaderFromBlock(block Block, chainID string, stateMerkleRoot string) LightHeader {
	return LightHeader{
		Version:                   LightProtocolVersion,
		ChainID:                   chainID,
		Height:                    block.ID,
		BlockHash:                 strings.TrimSpace(block.BlockHash),
		PrevHash:                  strings.TrimSpace(block.PrevHash),
		StateRoot:                 strings.TrimSpace(block.StateRoot),
		StateMerkleRoot:           strings.TrimSpace(stateMerkleRoot),
		TxRoot:                    strings.TrimSpace(block.MempoolRoot),
		ReceiptRoot:               strings.TrimSpace(block.ReceiptRoot),
		ValidatorSetHash:          strings.TrimSpace(block.ValidatorSetHash),
		ValidatorSetRoot:          strings.TrimSpace(block.ValidatorSetRoot),
		ValidatorRegistryHash:     strings.TrimSpace(block.ValidatorRegistryHash),
		NextValidatorSetHash:      strings.TrimSpace(block.NextValidatorSetHash),
		NextValidatorSetRoot:      strings.TrimSpace(block.NextValidatorSetRoot),
		FinalityRoot:              strings.TrimSpace(block.FinalityRoot),
		EpochAnchorHash:           strings.TrimSpace(block.EpochAnchorHash),
		PreviousEpochAnchorHash:   strings.TrimSpace(block.PreviousEpochAnchorHash),
		FinalizedHeight:           block.FinalizedHeight,
		FinalizedStateRoot:        strings.TrimSpace(block.FinalizedStateRoot),
		FinalizedValidatorSetHash: strings.TrimSpace(block.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot: strings.TrimSpace(block.FinalizedValidatorSetRoot),
		ConsensusMode:             strings.TrimSpace(block.ConsensusMode),
		QuorumPolicyVersion:       strings.TrimSpace(block.QuorumPolicyVersion),
		ActiveReadyCount:          block.ActiveReadyCount,
		RequiredQuorum:            block.RequiredQuorum,
		StrictQuorum:              block.StrictQuorum,
		Proposer:                  strings.TrimSpace(block.Proposer),
		Timestamp:                 block.Timestamp,
		FinalityCertificate:       block.FinalityCertificate,
	}
}

func (s *Server) lightHeaderForBlock(block Block) LightHeader {
	stateMerkleRoot := ""
	if s != nil && s.Node != nil && s.Node.DB != nil {
		if snap, _, _, ok := s.Node.ResolveCommittedStateSnapshot(block.ID); ok && snap != nil {
			stateMerkleRoot = strings.TrimSpace(snap.StateMerkleRoot)
		}
	}
	return lightHeaderFromBlock(block, ChainID, stateMerkleRoot)
}

func (s *Server) lightHeaderForStateProof(block Block, ledger Ledger) (LightHeader, bool, string) {
	proofRoot := LedgerStateMerkleRoot(ledger)
	trusted := false
	source := "runtime_state"
	if s != nil && s.Node != nil && s.Node.DB != nil {
		if snap, _, snapSource, ok := s.Node.ResolveCommittedStateSnapshot(block.ID); ok && snap != nil {
			if strings.EqualFold(strings.TrimSpace(snap.StateMerkleRoot), strings.TrimSpace(proofRoot)) {
				trusted = true
				source = snapSource
			}
		}
	}
	header := lightHeaderFromBlock(block, ChainID, proofRoot)
	return header, trusted, source
}

func parseLightUintParam(r *http.Request, key string, fallback uint64) (uint64, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return value, nil
}

func (s *Server) handleLightHeaders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "node unavailable")
		return
	}

	latest := s.Node.Blockchain.Height()
	fromProvided := strings.TrimSpace(r.URL.Query().Get("from")) != ""
	from, err := parseLightUintParam(r, "from", latest)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, "", err.Error())
		return
	}
	limit, err := parseLightUintParam(r, "limit", 64)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, "", err.Error())
		return
	}
	if limit == 0 {
		limit = 1
	}
	if limit > lightMaxHeaderLimit {
		limit = lightMaxHeaderLimit
	}
	if !fromProvided {
		if latest+1 > limit {
			from = latest - limit + 1
		} else {
			from = 1
		}
	}
	if from == 0 || from > latest {
		writeV1Error(w, http.StatusBadRequest, "", "from height unavailable")
		return
	}

	headers := make([]LightHeader, 0, limit)
	for h := from; h <= latest && uint64(len(headers)) < limit; h++ {
		block, ok := s.lightBlockByHeight(h)
		if !ok {
			break
		}
		headers = append(headers, s.lightHeaderForBlock(block))
	}
	if len(headers) == 0 {
		writeV1Error(w, http.StatusNotFound, "", "headers not found")
		return
	}
	writeV1Data(w, http.StatusOK, map[string]any{
		"headers":       headers,
		"latest_height": latest,
	})
}

func (s *Server) handleLightCheckpointLatest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "node unavailable")
		return
	}

	if s.Node.DB != nil {
		if snapshot, meta, source, err := s.Node.latestCommittedSnapshotMeta(); err == nil && snapshot != nil {
			block, ok := s.lightBlockByHeight(snapshot.Height)
			header := LightHeader{
				Version:         LightProtocolVersion,
				ChainID:         ChainID,
				Height:          snapshot.Height,
				BlockHash:       strings.TrimSpace(snapshot.BlockHash),
				StateRoot:       strings.TrimSpace(snapshot.StateRoot),
				StateMerkleRoot: strings.TrimSpace(snapshot.StateMerkleRoot),
				FinalityRoot:    strings.TrimSpace(snapshot.FinalityRoot),
			}
			if ok {
				header = lightHeaderFromBlock(block, ChainID, snapshot.StateMerkleRoot)
			}
			writeV1Data(w, http.StatusOK, map[string]any{
				"trusted":  true,
				"source":   source,
				"header":   header,
				"snapshot": snapshotMetaResponse(snapshot, meta, source),
			})
			return
		}
	}

	latest := s.Node.Blockchain.Height()
	block, ok := s.lightBlockByHeight(latest)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "checkpoint unavailable")
		return
	}
	ledger, ok := s.ledgerAtHeight(latest)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "checkpoint ledger unavailable")
		return
	}
	writeV1Data(w, http.StatusOK, map[string]any{
		"trusted": false,
		"source":  "runtime_state",
		"header":  lightHeaderFromBlock(block, ChainID, LedgerStateMerkleRoot(ledger)),
	})
}

func (s *Server) handleLightBalanceProof(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "node unavailable")
		return
	}

	addr := canonicalAddressKey(r.URL.Query().Get("address"))
	if addr == "" {
		writeV1Error(w, http.StatusBadRequest, "", "address required")
		return
	}
	coin := normalizeCoin(r.URL.Query().Get("coin"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	height, err := parseLightUintParam(r, "height", 0)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, "", err.Error())
		return
	}

	var ledger Ledger
	var resolvedState string
	if height > 0 {
		var ok bool
		ledger, ok = s.ledgerAtHeight(height)
		if !ok {
			writeV1Error(w, http.StatusNotFound, "", "state height unavailable")
			return
		}
		resolvedState = "height"
	} else {
		var ok bool
		ledger, height, resolvedState, ok = s.resolveBalanceLedger(state)
		if !ok {
			writeV1Error(w, http.StatusBadRequest, "", "invalid state (use latest|finalized|pending)")
			return
		}
	}
	block, ok := s.lightBlockByHeight(height)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "anchor block unavailable")
		return
	}

	leafKey := balanceKey(coin, addr)
	proof, ok := buildLightMerkleProof("ledger_state", canonicalLedgerStateLeaves(ledger), leafKey)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "balance leaf absent")
		return
	}
	header, trusted, source := s.lightHeaderForStateProof(block, ledger)
	resp := LightProofResponse{
		Version:     LightProtocolVersion,
		ProofType:   "balance",
		Trusted:     trusted,
		TrustSource: source,
		Header:      header,
		Proof:       proof,
		Value: map[string]any{
			"address": displayAddress(addr),
			"coin":    coin,
			"balance": getBalance(ledger, coin, addr),
			"state":   resolvedState,
			"height":  height,
		},
		VerifyHint: "Verify proof.root == header.state_merkle_root, then VerifyLightMerkleProof(proof).",
	}
	writeV1Data(w, http.StatusOK, resp)
}

func (s *Server) findLightTx(txID string) (Block, Transaction, int, bool) {
	txID = strings.TrimSpace(txID)
	if s == nil || s.Node == nil || s.Node.Blockchain == nil || txID == "" {
		return Block{}, Transaction{}, -1, false
	}
	s.Node.Blockchain.mu.RLock()
	blocks := append([]Block(nil), s.Node.Blockchain.Blocks...)
	s.Node.Blockchain.mu.RUnlock()
	for i := len(blocks) - 1; i >= 0; i-- {
		block := blocks[i]
		for txIndex, tx := range block.Transactions {
			if strings.EqualFold(strings.TrimSpace(txID), strings.TrimSpace(txIDFromAny(tx))) {
				return block, tx, txIndex, true
			}
		}
	}
	return Block{}, Transaction{}, -1, false
}

func txIDFromAny(tx Transaction) string {
	if id := strings.TrimSpace(tx.ID); id != "" {
		return id
	}
	if id := strings.TrimSpace(tx.EVMTxHash); id != "" {
		return id
	}
	return txID(tx)
}

func (s *Server) handleLightReceiptProof(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "node unavailable")
		return
	}
	txHash := strings.TrimSpace(r.URL.Query().Get("tx_id"))
	if txHash == "" {
		txHash = strings.TrimSpace(r.URL.Query().Get("hash"))
	}
	if txHash == "" {
		writeV1Error(w, http.StatusBadRequest, "", "tx_id required")
		return
	}
	block, tx, txIndex, ok := s.findLightTx(txHash)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "tx not found")
		return
	}
	if strings.TrimSpace(block.ReceiptRoot) == "" || txIndex < 0 || txIndex >= len(block.Receipts) {
		writeV1Error(w, http.StatusNotFound, "", "receipt proof unavailable")
		return
	}
	receipt := block.Receipts[txIndex]
	key := strings.TrimSpace(receipt.TxHash)
	if key == "" {
		key = strconv.Itoa(txIndex)
	}
	proof, ok := buildLightMerkleProof("receipt", blockReceiptLeaves(block), key)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "receipt proof unavailable")
		return
	}
	header := s.lightHeaderForBlock(block)
	resp := LightProofResponse{
		Version:     LightProtocolVersion,
		ProofType:   "receipt",
		Trusted:     strings.TrimSpace(block.ReceiptRoot) != "",
		TrustSource: "block_receipt_root",
		Header:      header,
		Proof:       proof,
		Value: map[string]any{
			"tx_id":    txIDFromAny(tx),
			"tx_index": txIndex,
			"height":   block.ID,
			"receipt":  receipt,
		},
		VerifyHint: "Verify proof.root == header.receipt_root, then VerifyLightMerkleProof(proof).",
	}
	writeV1Data(w, http.StatusOK, resp)
}

func (s *Server) handleLightTxProof(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeV1Error(w, http.StatusMethodNotAllowed, "", "method not allowed")
		return
	}
	if !authorized(r) {
		writeV1Error(w, http.StatusUnauthorized, "", "unauthorized")
		return
	}
	if s == nil || s.Node == nil || s.Node.Blockchain == nil {
		writeV1Error(w, http.StatusServiceUnavailable, "", "node unavailable")
		return
	}
	txHash := strings.TrimSpace(r.URL.Query().Get("tx_id"))
	if txHash == "" {
		txHash = strings.TrimSpace(r.URL.Query().Get("hash"))
	}
	if txHash == "" {
		writeV1Error(w, http.StatusBadRequest, "", "tx_id required")
		return
	}
	block, tx, txIndex, ok := s.findLightTx(txHash)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "tx not found")
		return
	}
	if strings.TrimSpace(block.ReceiptRoot) == "" || txIndex < 0 || txIndex >= len(block.Receipts) {
		writeV1Error(w, http.StatusNotFound, "", "receipt-backed tx proof unavailable")
		return
	}
	receipt := block.Receipts[txIndex]
	key := strings.TrimSpace(receipt.TxHash)
	if key == "" {
		key = strconv.Itoa(txIndex)
	}
	proof, ok := buildLightMerkleProof("receipt", blockReceiptLeaves(block), key)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "receipt-backed tx proof unavailable")
		return
	}
	header := s.lightHeaderForBlock(block)
	resp := LightProofResponse{
		Version:     LightProtocolVersion,
		ProofType:   "tx_receipt",
		Trusted:     strings.TrimSpace(block.ReceiptRoot) != "",
		TrustSource: "block_receipt_root",
		Header:      header,
		Proof:       proof,
		Value: map[string]any{
			"tx_id":    txIDFromAny(tx),
			"tx_index": txIndex,
			"height":   block.ID,
			"tx":       tx,
			"receipt":  receipt,
		},
		VerifyHint: "MSC v1 tx SPV is receipt-backed: verify receipt proof against header.receipt_root, then match receipt.tx_hash to tx.id.",
	}
	writeV1Data(w, http.StatusOK, resp)
}
