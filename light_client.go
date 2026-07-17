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
	// `LightProtocolVersion` defines the constant value used by this package.
	LightProtocolVersion = "msc-light-v1"
	// `lightMaxHeaderLimit` defines the constant value used by this package.
	lightMaxHeaderLimit = 512
)

type LightHeader struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `ChainID` stores the value associated with this record.
	ChainID string `json:"chain_id"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string `json:"block_hash"`
	// `PrevHash` stores the digest used to identify or verify the related data.
	PrevHash string `json:"prev_hash"`
	// `StateRoot` stores the digest used to identify or verify the related data.
	StateRoot string `json:"state_root"`
	// `StateMerkleRoot` stores the digest used to identify or verify the related data.
	StateMerkleRoot string `json:"state_merkle_root,omitempty"`
	// `TxRoot` stores the transaction data handled by this operation.
	TxRoot string `json:"tx_root"`
	// `ReceiptRoot` stores the digest used to identify or verify the related data.
	ReceiptRoot string `json:"receipt_root,omitempty"`
	// EventRoot anchors bridge-specific canonical event batches for external checkpoints.
	EventRoot string `json:"event_root,omitempty"`
	// `ValidatorSetHash` stores whether the related condition is satisfied.
	ValidatorSetHash string `json:"validator_set_hash"`
	// `ValidatorSetRoot` stores whether the related condition is satisfied.
	ValidatorSetRoot string `json:"validator_set_root,omitempty"`
	// `ValidatorRegistryHash` stores whether the related condition is satisfied.
	ValidatorRegistryHash string `json:"validator_registry_hash,omitempty"`
	// `NextValidatorSetHash` stores the digest used to identify or verify the related data.
	NextValidatorSetHash string `json:"next_validator_set_hash,omitempty"`
	// `NextValidatorSetRoot` stores the digest used to identify or verify the related data.
	NextValidatorSetRoot string `json:"next_validator_set_root,omitempty"`
	// `FinalityRoot` stores the digest used to identify or verify the related data.
	FinalityRoot string `json:"finality_root,omitempty"`
	// `EpochAnchorHash` stores the digest used to identify or verify the related data.
	EpochAnchorHash string `json:"epoch_anchor_hash,omitempty"`
	// `PreviousEpochAnchorHash` stores the digest used to identify or verify the related data.
	PreviousEpochAnchorHash string `json:"previous_epoch_anchor_hash,omitempty"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `FinalizedStateRoot` stores the digest used to identify or verify the related data.
	FinalizedStateRoot string `json:"finalized_state_root,omitempty"`
	// `FinalizedValidatorSetHash` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetHash string `json:"finalized_validator_set_hash,omitempty"`
	// `FinalizedValidatorSetRoot` stores the digest used to identify or verify the related data.
	FinalizedValidatorSetRoot string `json:"finalized_validator_set_root,omitempty"`
	// `ConsensusMode` stores the value associated with this record.
	ConsensusMode string `json:"consensus_mode,omitempty"`
	// `QuorumPolicyVersion` stores the value associated with this record.
	QuorumPolicyVersion string `json:"quorum_policy_version,omitempty"`
	// `ActiveReadyCount` stores the measured quantity used by this operation.
	ActiveReadyCount int `json:"active_ready_count,omitempty"`
	// `RequiredQuorum` stores the request data being processed.
	RequiredQuorum int `json:"required_quorum,omitempty"`
	// `StrictQuorum` stores the value associated with this record.
	StrictQuorum int `json:"strict_quorum,omitempty"`
	// `Proposer` stores the value associated with this record.
	Proposer string `json:"proposer,omitempty"`
	// `Timestamp` stores the value associated with this record.
	Timestamp int64 `json:"timestamp,omitempty"`
	// `FinalityCertificate` stores the value associated with this record.
	FinalityCertificate *FinalizedEpochCertificate `json:"finality_certificate,omitempty"`
}

type LightMerkleSibling struct {
	// `Position` stores the value associated with this record.
	Position string `json:"position"`
	// `Hash` stores the digest used to identify or verify the related data.
	Hash string `json:"hash"`
}

type LightMerkleProof struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `Domain` stores the value associated with this record.
	Domain string `json:"domain"`
	// `Root` stores the digest used to identify or verify the related data.
	Root string `json:"root"`
	// `LeafKey` stores the key used to access the related value.
	LeafKey string `json:"leaf_key"`
	// `LeafValue` stores the value currently being processed.
	LeafValue string `json:"leaf_value"`
	// `LeafHash` stores the digest used to identify or verify the related data.
	LeafHash string `json:"leaf_hash"`
	// `LeafIndex` stores the current position in the related collection.
	LeafIndex int `json:"leaf_index"`
	// `TotalLeaves` stores the measured quantity used by this operation.
	TotalLeaves int `json:"total_leaves"`
	// `Siblings` stores the value associated with this record.
	Siblings []LightMerkleSibling `json:"siblings"`
}

type LightProofResponse struct {
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `ProofType` stores the value associated with this record.
	ProofType string `json:"proof_type"`
	// `Trusted` stores the value associated with this record.
	Trusted bool `json:"trusted"`
	// `TrustSource` stores the value associated with this record.
	TrustSource string `json:"trust_source"`
	// `Header` stores the block data handled by this operation.
	Header LightHeader `json:"header"`
	// `Proof` stores the value associated with this record.
	Proof LightMerkleProof `json:"proof"`
	// `Value` stores the value currently being processed.
	Value any `json:"value,omitempty"`
	// `VerifyHint` stores the value associated with this record.
	VerifyHint string `json:"verify_hint"`
}

type lightMerkleLeaf struct {
	// `Key` stores the key used to access the related value.
	Key string
	// `Value` stores the value currently being processed.
	Value string
	// `Hash` stores the digest used to identify or verify the related data.
	Hash string
}

// committedLightReceipt returns only the StateReceipt fields committed by the
// receipt Merkle leaf/root. DTL presentation metadata and logs are intentionally
// verified by full replay, not by light receipt proofs.
func committedLightReceipt(receipt StateReceipt) StateReceipt {
	return StateReceipt{
		TxHash:        strings.TrimSpace(receipt.TxHash),
		PreStateHash:  strings.TrimSpace(receipt.PreStateHash),
		PostStateHash: strings.TrimSpace(receipt.PostStateHash),
	}
}

func committedLightReceiptForTx(block Block, tx Transaction, txIndex int) (StateReceipt, bool) {
	if txIndex < 0 || txIndex >= len(block.Receipts) {
		return StateReceipt{}, false
	}
	// `committed` stores the value produced by this operation.
	committed := committedLightReceipt(block.Receipts[txIndex])
	// `txID` stores the transaction data handled by this operation.
	txID := strings.TrimSpace(txIDFromAny(tx))
	if committed.TxHash == "" || txID == "" || !strings.EqualFold(committed.TxHash, txID) {
		return StateReceipt{}, false
	}
	return committed, true
}

// lightHashString implements the light hash string helper.
func lightHashString(value string) string {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// normalizeLightHexHash normalizes light hex hash.
func normalizeLightHexHash(hash string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if len(hash) != 64 {
		return ""
	}
	// `err` stores the error produced by this operation.
	if _, err := hex.DecodeString(hash); err != nil {
		return ""
	}
	return hash
}

// lightLeafHash implements the light leaf hash helper.
func lightLeafHash(leaf lightMerkleLeaf) string {
	// `hash` stores the digest used to identify or verify the related data.
	if hash := normalizeLightHexHash(leaf.Hash); hash != "" {
		return hash
	}
	return lightHashString(leaf.Value)
}

// buildLightMerkleProof builds light merkle proof.
func buildLightMerkleProof(domain string, leaves []lightMerkleLeaf, targetKey string) (LightMerkleProof, bool) {
	targetKey = strings.TrimSpace(targetKey)
	if targetKey == "" || len(leaves) == 0 {
		return LightMerkleProof{}, false
	}

	// `hashes` stores the digest used to identify or verify the related data.
	hashes := make([]string, 0, len(leaves))
	// `targetIndex` stores the current position in the related collection.
	targetIndex := -1
	// `targetValue` stores the value currently being processed.
	targetValue := ""
	// `i` and `leaf` track the current position in the related collection.
	for i, leaf := range leaves {
		// `hash` stores the digest used to identify or verify the related data.
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

	// `root` stores the digest used to identify or verify the related data.
	root := merkleRootFromHexLeaves(hashes)
	// `level` stores the value produced by this operation.
	level := append([]string(nil), hashes...)
	// `index` stores the current position in the related collection.
	index := targetIndex
	// `siblings` stores the value produced by this operation.
	siblings := make([]LightMerkleSibling, 0)
	for len(level) > 1 {
		// `siblingIndex` stores the current position in the related collection.
		siblingIndex := index ^ 1
		// `position` stores the value produced by this operation.
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

		// `next` stores the value produced by this operation.
		next := make([]string, 0, (len(level)+1)/2)
		// `i` stores the current position in the related collection.
		for i := 0; i < len(level); i += 2 {
			// `left` stores the value produced by this operation.
			left := level[i]
			// `right` stores the value produced by this operation.
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			// `leftBytes` and `lerr` store the error produced by this operation.
			leftBytes, lerr := hex.DecodeString(left)
			// `rightBytes` and `rerr` store the error produced by this operation.
			rightBytes, rerr := hex.DecodeString(right)
			if lerr != nil || rerr != nil {
				return LightMerkleProof{}, false
			}
			// `pair` stores the value produced by this operation.
			pair := append(append([]byte{}, leftBytes...), rightBytes...)
			// `sum` stores the value produced by this operation.
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

// VerifyLightMerkleProof verifies light merkle proof.
func VerifyLightMerkleProof(proof LightMerkleProof) bool {
	// `root` stores the digest used to identify or verify the related data.
	root := normalizeLightHexHash(proof.Root)
	// `leafHash` stores the digest used to identify or verify the related data.
	leafHash := normalizeLightHexHash(proof.LeafHash)
	if root == "" || leafHash == "" || proof.TotalLeaves <= 0 || proof.LeafIndex < 0 || proof.LeafIndex >= proof.TotalLeaves {
		return false
	}
	if proof.LeafValue != "" && lightHashString(proof.LeafValue) != leafHash {
		return false
	}
	// `current` stores the value produced by this operation.
	current := leafHash
	// `sibling` tracks the current values while iterating.
	for _, sibling := range proof.Siblings {
		// `siblingHash` stores the digest used to identify or verify the related data.
		siblingHash := normalizeLightHexHash(sibling.Hash)
		if siblingHash == "" {
			return false
		}
		// `currentBytes` and `cerr` store the error produced by this operation.
		currentBytes, cerr := hex.DecodeString(current)
		// `siblingBytes` and `serr` store the error produced by this operation.
		siblingBytes, serr := hex.DecodeString(siblingHash)
		if cerr != nil || serr != nil {
			return false
		}
		// `pair` stores the value used by this operation.
		var pair []byte
		switch strings.ToLower(strings.TrimSpace(sibling.Position)) {
		case "left":
			pair = append(append([]byte{}, siblingBytes...), currentBytes...)
		case "right":
			pair = append(append([]byte{}, currentBytes...), siblingBytes...)
		default:
			return false
		}
		// `sum` stores the value produced by this operation.
		sum := sha256.Sum256(pair)
		current = hex.EncodeToString(sum[:])
	}
	return strings.EqualFold(current, root)
}

// VerifyLightHeaderChain verifies light header chain.
func VerifyLightHeaderChain(headers []LightHeader) error {
	if len(headers) == 0 {
		return errors.New("light header chain empty")
	}
	// `i` stores the current position in the related collection.
	for i := 1; i < len(headers); i++ {
		// `prev` stores the value produced by this operation.
		prev := headers[i-1]
		// `curr` stores the value produced by this operation.
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

// VerifyLightStateProof verifies light state proof.
func VerifyLightStateProof(header LightHeader, proof LightMerkleProof) bool {
	if strings.TrimSpace(header.StateMerkleRoot) == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(header.StateMerkleRoot), strings.TrimSpace(proof.Root)) {
		return false
	}
	return VerifyLightMerkleProof(proof)
}

// VerifyLightReceiptProof verifies light receipt proof.
func VerifyLightReceiptProof(header LightHeader, proof LightMerkleProof) bool {
	if strings.TrimSpace(header.ReceiptRoot) == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(header.ReceiptRoot), strings.TrimSpace(proof.Root)) {
		return false
	}
	return VerifyLightMerkleProof(proof)
}

// canonicalLedgerStateLeaves returns canonical ledger state leaves.
func canonicalLedgerStateLeaves(ledger Ledger) []lightMerkleLeaf {
	// `material` stores the value produced by this operation.
	material := canonicalLedgerHashMaterial(ledger)
	if strings.TrimSpace(material) == "" {
		return nil
	}
	// `parts` stores the value produced by this operation.
	parts := strings.Split(material, ";")
	// `leaves` stores the value produced by this operation.
	leaves := make([]lightMerkleLeaf, 0, len(parts))
	// `part` tracks the current values while iterating.
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// `key` stores the key used to access the related value.
		key := part
		// `idx` stores the current position in the related collection.
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

// blockReceiptLeaves implements the block receipt leaves helper.
func blockReceiptLeaves(block Block) []lightMerkleLeaf {
	if len(block.Receipts) == 0 {
		return nil
	}
	// `leaves` stores the value produced by this operation.
	leaves := make([]lightMerkleLeaf, 0, len(block.Receipts))
	// `i` and `receipt` track the current position in the related collection.
	for i, receipt := range block.Receipts {
		// `committed` stores the value produced by this operation.
		committed := committedLightReceipt(receipt)
		// `value` stores the value currently being processed.
		value := fmt.Sprintf("%d|%s|%s|%s",
			i,
			committed.TxHash,
			committed.PreStateHash,
			committed.PostStateHash,
		)
		// `key` stores the key used to access the related value.
		key := committed.TxHash
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

// lightBlockByHeight implements the light block by height helper.
func (s *Server) lightBlockByHeight(height uint64) (Block, bool) {
	if s == nil || s.Node == nil {
		return Block{}, false
	}
	if s.Node.Blockchain != nil {
		// `block` and `ok` store whether the related condition is satisfied.
		if block, ok := s.Node.Blockchain.GetBlock(height); ok {
			return block, true
		}
	}
	return s.Node.LoadBlock(int(height))
}

// lightHeaderFromBlock implements the light header from block helper.
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

// lightHeaderForBlock implements the light header for block helper.
func (s *Server) lightHeaderForBlock(block Block) LightHeader {
	// `stateMerkleRoot` stores the digest used to identify or verify the related data.
	stateMerkleRoot := ""
	if s != nil && s.Node != nil && s.Node.DB != nil {
		// `snap` and `ok` store whether the related condition is satisfied.
		if snap, _, _, ok := s.Node.ResolveCommittedStateSnapshot(block.ID); ok && snap != nil {
			stateMerkleRoot = strings.TrimSpace(snap.StateMerkleRoot)
		}
	}
	return lightHeaderFromBlock(block, protocolChainID(), stateMerkleRoot)
}

// lightHeaderForStateProof implements the light header for state proof helper.
func (s *Server) lightHeaderForStateProof(block Block, ledger Ledger) (LightHeader, bool, string) {
	// `proofRoot` stores the digest used to identify or verify the related data.
	proofRoot := LedgerStateMerkleRoot(ledger)
	// `trusted` stores the value produced by this operation.
	trusted := false
	// `source` stores the value produced by this operation.
	source := "runtime_state"
	if s != nil && s.Node != nil && s.Node.DB != nil {
		// `snap`, `snapSource`, and `ok` store whether the related condition is satisfied.
		if snap, _, snapSource, ok := s.Node.ResolveCommittedStateSnapshot(block.ID); ok && snap != nil {
			if strings.EqualFold(strings.TrimSpace(snap.StateMerkleRoot), strings.TrimSpace(proofRoot)) {
				trusted = true
				source = snapSource
			}
		}
	}
	// `header` stores the block data handled by this operation.
	header := lightHeaderFromBlock(block, protocolChainID(), proofRoot)
	return header, trusted, source
}

// parseLightUintParam parses light uint param.
func parseLightUintParam(r *http.Request, key string, fallback uint64) (uint64, error) {
	// `raw` stores the value produced by this operation.
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback, nil
	}
	// `value` and `err` store the error produced by this operation.
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return value, nil
}

// handleLightHeaders handles light headers.
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

	// `latest` stores the value produced by this operation.
	latest := s.Node.Blockchain.Height()
	// `fromProvided` stores the value produced by this operation.
	fromProvided := strings.TrimSpace(r.URL.Query().Get("from")) != ""
	// `from` and `err` store the error produced by this operation.
	from, err := parseLightUintParam(r, "from", latest)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, "", err.Error())
		return
	}
	// `limit` and `err` store the error produced by this operation.
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

	// `headers` stores the block data handled by this operation.
	headers := make([]LightHeader, 0, limit)
	// `h` stores the value produced by this operation.
	for h := from; h <= latest && uint64(len(headers)) < limit; h++ {
		// `block` and `ok` store whether the related condition is satisfied.
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

// handleLightCheckpointLatest handles light checkpoint latest.
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
		// `snapshot`, `meta`, `source`, and `err` store the error produced by this operation.
		if snapshot, meta, source, err := s.Node.latestCommittedSnapshotMeta(); err == nil && snapshot != nil {
			// `block` and `ok` store whether the related condition is satisfied.
			block, ok := s.lightBlockByHeight(snapshot.Height)
			// `header` stores the block data handled by this operation.
			header := LightHeader{
				Version:         LightProtocolVersion,
				ChainID:         protocolChainID(),
				Height:          snapshot.Height,
				BlockHash:       strings.TrimSpace(snapshot.BlockHash),
				StateRoot:       strings.TrimSpace(snapshot.StateRoot),
				StateMerkleRoot: strings.TrimSpace(snapshot.StateMerkleRoot),
				FinalityRoot:    strings.TrimSpace(snapshot.FinalityRoot),
			}
			if ok {
				header = lightHeaderFromBlock(block, protocolChainID(), snapshot.StateMerkleRoot)
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

	// `latest` stores the value produced by this operation.
	latest := s.Node.Blockchain.Height()
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := s.lightBlockByHeight(latest)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "checkpoint unavailable")
		return
	}
	// `ledger` and `ok` store whether the related condition is satisfied.
	ledger, ok := s.ledgerAtHeight(latest)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "checkpoint ledger unavailable")
		return
	}
	writeV1Data(w, http.StatusOK, map[string]any{
		"trusted": false,
		"source":  "runtime_state",
		"header":  lightHeaderFromBlock(block, protocolChainID(), LedgerStateMerkleRoot(ledger)),
	})
}

// handleLightBalanceProof handles light balance proof.
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

	// `addr` stores the address used by this operation.
	addr := canonicalAddressKey(r.URL.Query().Get("address"))
	if addr == "" {
		writeV1Error(w, http.StatusBadRequest, "", "address required")
		return
	}
	// `coin` stores the value produced by this operation.
	coin := normalizeCoin(r.URL.Query().Get("coin"))
	// `state` stores the value produced by this operation.
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	// `height` and `err` store the error produced by this operation.
	height, err := parseLightUintParam(r, "height", 0)
	if err != nil {
		writeV1Error(w, http.StatusBadRequest, "", err.Error())
		return
	}

	// `ledger` stores the value used by this operation.
	var ledger Ledger
	// `resolvedState` stores the result produced by this operation.
	var resolvedState string
	if height > 0 {
		// `ok` stores whether the related condition is satisfied.
		var ok bool
		ledger, ok = s.ledgerAtHeight(height)
		if !ok {
			writeV1Error(w, http.StatusNotFound, "", "state height unavailable")
			return
		}
		resolvedState = "height"
	} else {
		// `ok` stores whether the related condition is satisfied.
		var ok bool
		ledger, height, resolvedState, ok = s.resolveBalanceLedger(state)
		if !ok {
			writeV1Error(w, http.StatusBadRequest, "", "invalid state (use latest|finalized|pending)")
			return
		}
	}
	// `block` and `ok` store whether the related condition is satisfied.
	block, ok := s.lightBlockByHeight(height)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "anchor block unavailable")
		return
	}

	// `leafKey` stores the key used to access the related value.
	leafKey := balanceKey(coin, addr)
	// `proof` and `ok` store whether the related condition is satisfied.
	proof, ok := buildLightMerkleProof("ledger_state", canonicalLedgerStateLeaves(ledger), leafKey)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "balance leaf absent")
		return
	}
	// `header`, `trusted`, and `source` store the block data handled by this operation.
	header, trusted, source := s.lightHeaderForStateProof(block, ledger)
	// `resp` stores the response produced by this operation.
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

// findLightTx implements the find light tx helper.
func (s *Server) findLightTx(txID string) (Block, Transaction, int, bool) {
	txID = strings.TrimSpace(txID)
	if s == nil || s.Node == nil || s.Node.Blockchain == nil || txID == "" {
		return Block{}, Transaction{}, -1, false
	}
	s.Node.Blockchain.mu.RLock()
	// `blocks` stores the block data handled by this operation.
	blocks := append([]Block(nil), s.Node.Blockchain.Blocks...)
	s.Node.Blockchain.mu.RUnlock()
	// `i` stores the current position in the related collection.
	for i := len(blocks) - 1; i >= 0; i-- {
		// `block` stores the synchronization state protecting shared data.
		block := blocks[i]
		// `txIndex` and `tx` track the transaction data handled by this operation.
		for txIndex, tx := range block.Transactions {
			if strings.EqualFold(strings.TrimSpace(txID), strings.TrimSpace(txIDFromAny(tx))) {
				return block, tx, txIndex, true
			}
		}
	}
	return Block{}, Transaction{}, -1, false
}

// txIDFromAny implements the tx id from any helper.
func txIDFromAny(tx Transaction) string {
	// `id` stores the current position in the related collection.
	if id := strings.TrimSpace(tx.ID); id != "" {
		return id
	}
	return txID(tx)
}

// handleLightReceiptProof handles light receipt proof.
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
	// `txHash` stores the transaction data handled by this operation.
	txHash := strings.TrimSpace(r.URL.Query().Get("tx_id"))
	if txHash == "" {
		txHash = strings.TrimSpace(r.URL.Query().Get("hash"))
	}
	if txHash == "" {
		writeV1Error(w, http.StatusBadRequest, "", "tx_id required")
		return
	}
	// `block`, `tx`, `txIndex`, and `ok` store whether the related condition is satisfied.
	block, tx, txIndex, ok := s.findLightTx(txHash)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "tx not found")
		return
	}
	if strings.TrimSpace(block.ReceiptRoot) == "" {
		writeV1Error(w, http.StatusNotFound, "", "receipt proof unavailable")
		return
	}
	// `committedReceipt` stores the value produced by this operation.
	committedReceipt, ok := committedLightReceiptForTx(block, tx, txIndex)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "receipt proof unavailable")
		return
	}
	// `key` stores the key used to access the related value.
	key := committedReceipt.TxHash
	if key == "" {
		key = strconv.Itoa(txIndex)
	}
	// `proof` and `ok` store whether the related condition is satisfied.
	proof, ok := buildLightMerkleProof("receipt", blockReceiptLeaves(block), key)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "receipt proof unavailable")
		return
	}
	// `header` stores the block data handled by this operation.
	header := s.lightHeaderForBlock(block)
	// `resp` stores the response produced by this operation.
	resp := LightProofResponse{
		Version:     LightProtocolVersion,
		ProofType:   "receipt",
		Trusted:     strings.TrimSpace(block.ReceiptRoot) != "",
		TrustSource: "block_receipt_root",
		Header:      header,
		Proof:       proof,
		Value: map[string]any{
			"tx_id":    committedReceipt.TxHash,
			"tx_index": txIndex,
			"height":   block.ID,
			"receipt":  committedReceipt,
		},
		VerifyHint: "Verify proof.root == header.receipt_root, then VerifyLightMerkleProof(proof). The receipt proof commits tx_hash/pre_state_hash/post_state_hash only; DTL metadata/logs require full replay verification.",
	}
	writeV1Data(w, http.StatusOK, resp)
}

// handleLightTxProof handles light tx proof.
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
	// `txHash` stores the transaction data handled by this operation.
	txHash := strings.TrimSpace(r.URL.Query().Get("tx_id"))
	if txHash == "" {
		txHash = strings.TrimSpace(r.URL.Query().Get("hash"))
	}
	if txHash == "" {
		writeV1Error(w, http.StatusBadRequest, "", "tx_id required")
		return
	}
	// `block`, `tx`, `txIndex`, and `ok` store whether the related condition is satisfied.
	block, tx, txIndex, ok := s.findLightTx(txHash)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "tx not found")
		return
	}
	if strings.TrimSpace(block.ReceiptRoot) == "" {
		writeV1Error(w, http.StatusNotFound, "", "receipt-backed tx proof unavailable")
		return
	}
	// `committedReceipt` stores the value produced by this operation.
	committedReceipt, ok := committedLightReceiptForTx(block, tx, txIndex)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "receipt-backed tx proof unavailable")
		return
	}
	// `key` stores the key used to access the related value.
	key := committedReceipt.TxHash
	if key == "" {
		key = strconv.Itoa(txIndex)
	}
	// `proof` and `ok` store whether the related condition is satisfied.
	proof, ok := buildLightMerkleProof("receipt", blockReceiptLeaves(block), key)
	if !ok {
		writeV1Error(w, http.StatusNotFound, "", "receipt-backed tx proof unavailable")
		return
	}
	// `header` stores the block data handled by this operation.
	header := s.lightHeaderForBlock(block)
	// `resp` stores the response produced by this operation.
	resp := LightProofResponse{
		Version:     LightProtocolVersion,
		ProofType:   "tx_receipt",
		Trusted:     strings.TrimSpace(block.ReceiptRoot) != "",
		TrustSource: "block_receipt_root",
		Header:      header,
		Proof:       proof,
		Value: map[string]any{
			"tx_id":                       committedReceipt.TxHash,
			"tx_index":                    txIndex,
			"height":                      block.ID,
			"tx":                          tx,
			"tx_trusted":                  false,
			"tx_trust_source":             "receipt_tx_hash_only",
			"tx_verification_requirement": "recompute tx_id from transaction payload and match receipt.tx_hash",
			"receipt":                     committedReceipt,
		},
		VerifyHint: "MSC v1 tx SPV is receipt-backed: verify receipt proof against header.receipt_root, recompute tx_id from the returned transaction payload, then match it to receipt.tx_hash. The receipt proof commits tx_hash/pre_state_hash/post_state_hash only; DTL metadata/logs require full replay verification.",
	}
	writeV1Data(w, http.StatusOK, resp)
}
