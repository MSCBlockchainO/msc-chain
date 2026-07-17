package bridgeobserver

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"msc-chain/bridgeevmproof"
	"msc-chain/bridgetronproof"
)

const (
	BridgeProtocolVersion   = "msc-bridge-v5"
	BridgeCheckpointVersion = "msc-bridge-checkpoint-v2"
	LightProtocolVersion    = "msc-light-v1"
	ObservationVersion      = "msc-bridge-observation-v3"
)

type Signature struct {
	Signer    string `json:"signer"`
	PublicKey string `json:"public_key,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type Checkpoint struct {
	Version              string      `json:"version"`
	CheckpointID         string      `json:"checkpoint_id"`
	PreviousCheckpointID string      `json:"previous_checkpoint_id,omitempty"`
	SourceChainID        string      `json:"source_chain_id"`
	Height               uint64      `json:"height"`
	ObservedHeight       uint64      `json:"observed_height"`
	BlockHash            string      `json:"block_hash"`
	EventRoot            string      `json:"event_root"`
	TransactionRoot      string      `json:"transaction_root,omitempty"`
	ReceiptRoot          string      `json:"receipt_root,omitempty"`
	StateRoot            string      `json:"state_root,omitempty"`
	IssuedAtUnix         int64       `json:"issued_at_unix"`
	ValidatorSignatures  []Signature `json:"validator_signatures"`
	CreatedAtUnix        int64       `json:"created_at_unix,omitempty"`
}

type MerkleSibling struct {
	Position string `json:"position"`
	Hash     string `json:"hash"`
}

type MerkleProof struct {
	Version     string          `json:"version"`
	Domain      string          `json:"domain"`
	Root        string          `json:"root"`
	LeafKey     string          `json:"leaf_key"`
	LeafValue   string          `json:"leaf_value"`
	LeafHash    string          `json:"leaf_hash"`
	LeafIndex   int             `json:"leaf_index"`
	TotalLeaves int             `json:"total_leaves"`
	Siblings    []MerkleSibling `json:"siblings"`
}

type LightHeader struct {
	Height      uint64 `json:"height"`
	ChainID     string `json:"chain_id"`
	BlockHash   string `json:"block_hash"`
	StateRoot   string `json:"state_root,omitempty"`
	TxRoot      string `json:"tx_root,omitempty"`
	ReceiptRoot string `json:"receipt_root,omitempty"`
	EventRoot   string `json:"event_root,omitempty"`
}

type Event struct {
	SourceChainID   string `json:"source_chain_id"`
	SourceTxHash    string `json:"source_tx_hash"`
	LogIndex        uint64 `json:"log_index"`
	EventType       string `json:"event_type"`
	WithdrawalID    string `json:"withdrawal_id,omitempty"`
	EventContract   string `json:"event_contract"`
	AssetDenom      string `json:"asset_denom"`
	OriginAsset     string `json:"origin_asset"`
	Recipient       string `json:"recipient"`
	Amount          string `json:"amount"`
	SourceHeight    uint64 `json:"source_height"`
	SourceBlockHash string `json:"source_block_hash"`
}

type Proof struct {
	Version              string                 `json:"version,omitempty"`
	SourceChainID        string                 `json:"source_chain_id"`
	EventID              string                 `json:"event_id"`
	SourceTxHash         string                 `json:"source_tx_hash,omitempty"`
	LogIndex             uint64                 `json:"log_index,omitempty"`
	EventType            string                 `json:"event_type,omitempty"`
	WithdrawalID         string                 `json:"withdrawal_id,omitempty"`
	EventContract        string                 `json:"event_contract,omitempty"`
	CheckpointID         string                 `json:"checkpoint_id,omitempty"`
	FinalityCheckpoint   *Checkpoint            `json:"finality_checkpoint,omitempty"`
	AssetDenom           string                 `json:"asset_denom"`
	OriginAsset          string                 `json:"origin_asset,omitempty"`
	Recipient            string                 `json:"recipient,omitempty"`
	Amount               string                 `json:"amount,omitempty"`
	PayloadHash          string                 `json:"payload_hash,omitempty"`
	SourceHeight         uint64                 `json:"source_height"`
	ConfirmedHeight      uint64                 `json:"confirmed_height,omitempty"`
	SourceBlockHash      string                 `json:"source_block_hash,omitempty"`
	LightClientHeader    *LightHeader           `json:"light_client_header,omitempty"`
	MerkleProof          *MerkleProof           `json:"merkle_proof,omitempty"`
	EVMReceiptProof      *bridgeevmproof.Proof  `json:"evm_receipt_proof,omitempty"`
	TronTransactionProof *bridgetronproof.Proof `json:"tron_transaction_proof,omitempty"`
	OracleSignatures     []Signature            `json:"oracle_signatures,omitempty"`
}

type QuorumEvidence struct {
	Queried        int      `json:"queried"`
	Agreed         int      `json:"agreed"`
	Required       int      `json:"required"`
	Fingerprint    string   `json:"fingerprint"`
	EndpointHashes []string `json:"endpoint_hashes"`
}

type Artifact struct {
	Version        string         `json:"version"`
	ChainType      string         `json:"chain_type"`
	ObservedAtUnix int64          `json:"observed_at_unix"`
	Evidence       QuorumEvidence `json:"rpc_quorum_evidence"`
	Checkpoint     Checkpoint     `json:"checkpoint"`
	Proofs         []Proof        `json:"proofs"`
}

type merkleLeaf struct {
	key   string
	value string
	hash  string
}

func HashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func NormalizeRegistryID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > 96 {
		return ""
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ':' {
			continue
		}
		return ""
	}
	return value
}

func NormalizeHexHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "0x")
	if len(value) != 64 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func canonicalWithdrawalID(value string) string {
	if normalized := NormalizeHexHash(value); normalized != "" && strings.Trim(normalized, "0") != "" {
		return "0x" + normalized
	}
	return ""
}

func normalizeEventType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "lock"
	}
	return value
}

func BridgeID(event Event) string {
	source := NormalizeRegistryID(event.SourceChainID)
	txHash := canonicalExternalIdentifier(event.SourceTxHash)
	if source == "" || txHash == "" {
		return ""
	}
	material := strings.Join([]string{
		"MSC", "BRIDGE_ID", BridgeProtocolVersion, source, txHash,
		strconv.FormatUint(event.LogIndex, 10),
	}, "|")
	return "bridge_" + HashString(material)
}

func CanonicalEventPayload(event Event) string {
	return strings.Join([]string{
		"MSC",
		"BRIDGE",
		BridgeProtocolVersion,
		NormalizeRegistryID(event.SourceChainID),
		canonicalExternalIdentifier(event.SourceTxHash),
		strconv.FormatUint(event.LogIndex, 10),
		BridgeID(event),
		normalizeEventType(event.EventType),
		canonicalWithdrawalID(event.WithdrawalID),
		canonicalEventContract(event.EventContract),
		strings.ToLower(strings.TrimSpace(event.AssetDenom)),
		strings.TrimSpace(event.OriginAsset),
		strings.TrimSpace(event.Recipient),
		strings.TrimSpace(event.Amount),
		strconv.FormatUint(event.SourceHeight, 10),
		canonicalExternalIdentifier(event.SourceBlockHash),
	}, "|")
}

func canonicalExternalIdentifier(value string) string {
	value = strings.TrimSpace(value)
	raw := strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")
	if len(raw) == 64 {
		if _, err := hex.DecodeString(raw); err == nil {
			return strings.ToLower(value)
		}
	}
	return value
}

func canonicalEventContract(value string) string {
	value = strings.TrimSpace(value)
	if normalizeEVMAddress(value) != "" {
		return strings.ToLower(value)
	}
	return value
}

func EventPayloadHash(event Event) string {
	return HashString(CanonicalEventPayload(event))
}

func normalizeCheckpoint(checkpoint Checkpoint, chainType string) Checkpoint {
	checkpoint.Version = strings.TrimSpace(checkpoint.Version)
	checkpoint.CheckpointID = strings.ToLower(strings.TrimSpace(checkpoint.CheckpointID))
	checkpoint.PreviousCheckpointID = strings.ToLower(strings.TrimSpace(checkpoint.PreviousCheckpointID))
	checkpoint.SourceChainID = NormalizeRegistryID(checkpoint.SourceChainID)
	checkpoint.BlockHash = strings.TrimSpace(checkpoint.BlockHash)
	switch strings.ToLower(strings.TrimSpace(chainType)) {
	case "evm", "tron", "utxo":
		checkpoint.BlockHash = strings.ToLower(checkpoint.BlockHash)
	}
	checkpoint.EventRoot = NormalizeHexHash(checkpoint.EventRoot)
	checkpoint.TransactionRoot = NormalizeHexHash(checkpoint.TransactionRoot)
	checkpoint.ReceiptRoot = NormalizeHexHash(checkpoint.ReceiptRoot)
	checkpoint.StateRoot = NormalizeHexHash(checkpoint.StateRoot)
	checkpoint.CreatedAtUnix = 0
	return checkpoint
}

func CanonicalCheckpointPayload(checkpoint Checkpoint, chainType string) string {
	checkpoint = normalizeCheckpoint(checkpoint, chainType)
	return strings.Join([]string{
		"MSC", "BRIDGE_CHECKPOINT", BridgeCheckpointVersion,
		checkpoint.SourceChainID,
		strconv.FormatUint(checkpoint.Height, 10),
		strconv.FormatUint(checkpoint.ObservedHeight, 10),
		checkpoint.BlockHash,
		checkpoint.EventRoot,
		checkpoint.TransactionRoot,
		checkpoint.ReceiptRoot,
		checkpoint.StateRoot,
		checkpoint.PreviousCheckpointID,
		strconv.FormatInt(checkpoint.IssuedAtUnix, 10),
	}, "|")
}

func CheckpointID(checkpoint Checkpoint, chainType string) string {
	return "bcp_" + HashString(CanonicalCheckpointPayload(checkpoint, chainType))
}

func validateEvent(event Event) error {
	if NormalizeRegistryID(event.SourceChainID) == "" || strings.TrimSpace(event.SourceTxHash) == "" {
		return errors.New("event source chain and transaction hash are required")
	}
	if event.SourceHeight == 0 || strings.TrimSpace(event.SourceBlockHash) == "" {
		return errors.New("event source height or block hash invalid")
	}
	if strings.TrimSpace(event.EventContract) == "" || strings.TrimSpace(event.AssetDenom) == "" || strings.TrimSpace(event.OriginAsset) == "" {
		return errors.New("event contract and asset identity are required")
	}
	if strings.TrimSpace(event.Recipient) == "" || strings.TrimSpace(event.Amount) == "" {
		return errors.New("event recipient and amount are required")
	}
	eventType := normalizeEventType(event.EventType)
	if eventType != "lock" && eventType != "unlock" {
		return fmt.Errorf("unsupported bridge event type %q", eventType)
	}
	if eventType == "lock" && strings.TrimSpace(event.WithdrawalID) != "" {
		return errors.New("lock event must not contain a withdrawal ID")
	}
	if eventType == "unlock" && canonicalWithdrawalID(event.WithdrawalID) == "" {
		return errors.New("unlock event requires a canonical 32-byte withdrawal ID")
	}
	return nil
}

func merkleRoot(hashes []string) string {
	if len(hashes) == 0 {
		return ""
	}
	level := append([]string(nil), hashes...)
	for len(level) > 1 {
		next := make([]string, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			leftBytes, leftErr := hex.DecodeString(left)
			rightBytes, rightErr := hex.DecodeString(right)
			if leftErr != nil || rightErr != nil {
				return ""
			}
			pair := append(append([]byte(nil), leftBytes...), rightBytes...)
			sum := sha256.Sum256(pair)
			next = append(next, hex.EncodeToString(sum[:]))
		}
		level = next
	}
	return level[0]
}

func buildMerkleProof(leaves []merkleLeaf, target int) (MerkleProof, error) {
	if len(leaves) == 0 || target < 0 || target >= len(leaves) {
		return MerkleProof{}, errors.New("invalid Merkle proof target")
	}
	hashes := make([]string, len(leaves))
	for index, leaf := range leaves {
		hashes[index] = NormalizeHexHash(leaf.hash)
		if hashes[index] == "" {
			hashes[index] = HashString(leaf.value)
		}
	}
	root := merkleRoot(hashes)
	if root == "" {
		return MerkleProof{}, errors.New("could not build Merkle root")
	}
	level := append([]string(nil), hashes...)
	index := target
	siblings := make([]MerkleSibling, 0)
	for len(level) > 1 {
		siblingIndex := index ^ 1
		position := "right"
		if index%2 == 1 {
			position = "left"
		}
		if siblingIndex >= len(level) {
			siblingIndex = index
		}
		siblings = append(siblings, MerkleSibling{Position: position, Hash: level[siblingIndex]})
		next := make([]string, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			leftBytes, _ := hex.DecodeString(left)
			rightBytes, _ := hex.DecodeString(right)
			pair := append(append([]byte(nil), leftBytes...), rightBytes...)
			sum := sha256.Sum256(pair)
			next = append(next, hex.EncodeToString(sum[:]))
		}
		level = next
		index /= 2
	}
	return MerkleProof{
		Version: LightProtocolVersion, Domain: "bridge_event", Root: root,
		LeafKey: leaves[target].key, LeafValue: leaves[target].value,
		LeafHash: hashes[target], LeafIndex: target, TotalLeaves: len(leaves), Siblings: siblings,
	}, nil
}

func VerifyMerkleProof(proof MerkleProof) bool {
	root := NormalizeHexHash(proof.Root)
	leafHash := NormalizeHexHash(proof.LeafHash)
	if proof.Version != LightProtocolVersion || proof.Domain != "bridge_event" || root == "" || leafHash == "" ||
		proof.TotalLeaves <= 0 || proof.LeafIndex < 0 || proof.LeafIndex >= proof.TotalLeaves || HashString(proof.LeafValue) != leafHash {
		return false
	}
	expectedLevels := 0
	for leaves := proof.TotalLeaves; leaves > 1; leaves = (leaves + 1) / 2 {
		expectedLevels++
	}
	if len(proof.Siblings) != expectedLevels {
		return false
	}
	current := leafHash
	for _, sibling := range proof.Siblings {
		siblingHash := NormalizeHexHash(sibling.Hash)
		currentBytes, currentErr := hex.DecodeString(current)
		siblingBytes, siblingErr := hex.DecodeString(siblingHash)
		if siblingHash == "" || currentErr != nil || siblingErr != nil {
			return false
		}
		var pair []byte
		switch strings.ToLower(strings.TrimSpace(sibling.Position)) {
		case "left":
			pair = append(append([]byte(nil), siblingBytes...), currentBytes...)
		case "right":
			pair = append(append([]byte(nil), currentBytes...), siblingBytes...)
		default:
			return false
		}
		sum := sha256.Sum256(pair)
		current = hex.EncodeToString(sum[:])
	}
	return current == root
}

func BuildProofs(events []Event, confirmedHeight uint64) ([]Proof, string, error) {
	if len(events) == 0 {
		return []Proof{}, EmptyEventRoot(), nil
	}
	sortedEvents := append([]Event(nil), events...)
	sort.Slice(sortedEvents, func(i, j int) bool { return BridgeID(sortedEvents[i]) < BridgeID(sortedEvents[j]) })
	leaves := make([]merkleLeaf, len(sortedEvents))
	seen := make(map[string]struct{}, len(sortedEvents))
	for index, event := range sortedEvents {
		if err := validateEvent(event); err != nil {
			return nil, "", fmt.Errorf("event %d: %w", index, err)
		}
		id := BridgeID(event)
		if _, exists := seen[id]; exists {
			return nil, "", fmt.Errorf("duplicate bridge event %s", id)
		}
		seen[id] = struct{}{}
		leaves[index] = merkleLeaf{key: id, value: CanonicalEventPayload(event), hash: EventPayloadHash(event)}
	}
	proofs := make([]Proof, 0, len(sortedEvents))
	for index, event := range sortedEvents {
		merkleProof, err := buildMerkleProof(leaves, index)
		if err != nil {
			return nil, "", err
		}
		id := BridgeID(event)
		proofs = append(proofs, Proof{
			Version: BridgeProtocolVersion, SourceChainID: event.SourceChainID, EventID: id,
			SourceTxHash: event.SourceTxHash, LogIndex: event.LogIndex, EventType: normalizeEventType(event.EventType),
			WithdrawalID: canonicalWithdrawalID(event.WithdrawalID), EventContract: event.EventContract,
			AssetDenom: event.AssetDenom, OriginAsset: event.OriginAsset,
			Recipient: event.Recipient, Amount: event.Amount, PayloadHash: EventPayloadHash(event),
			SourceHeight: event.SourceHeight, ConfirmedHeight: confirmedHeight, SourceBlockHash: event.SourceBlockHash,
			MerkleProof: &merkleProof,
		})
	}
	return proofs, proofs[0].MerkleProof.Root, nil
}

func EmptyEventRoot() string {
	sum := sha256.Sum256([]byte("MSC|BRIDGE_EVENT_ROOT|EMPTY|" + BridgeProtocolVersion))
	return hex.EncodeToString(sum[:])
}

func ValidateArtifact(artifact Artifact) error {
	chainType := strings.ToLower(strings.TrimSpace(artifact.ChainType))
	if artifact.Version != ObservationVersion || (chainType != "evm" && chainType != "tron" && chainType != "solana") {
		return errors.New("unsupported observation artifact version or chain type")
	}
	if err := validateQuorumEvidence(artifact.Evidence); err != nil {
		return fmt.Errorf("RPC quorum evidence: %w", err)
	}
	checkpoint := normalizeCheckpoint(artifact.Checkpoint, artifact.ChainType)
	if checkpoint.Version != BridgeCheckpointVersion || checkpoint.CheckpointID == "" || checkpoint.CheckpointID != CheckpointID(checkpoint, artifact.ChainType) {
		return errors.New("checkpoint ID or version is not canonical")
	}
	if checkpoint.SourceChainID == "" || checkpoint.Height == 0 || checkpoint.ObservedHeight < checkpoint.Height || checkpoint.IssuedAtUnix <= 0 ||
		NormalizeHexHash(checkpoint.EventRoot) == "" || (NormalizeHexHash(checkpoint.ReceiptRoot) == "" && NormalizeHexHash(checkpoint.StateRoot) == "") ||
		!validObservationBlockID(chainType, checkpoint.BlockHash) {
		return errors.New("checkpoint finality context is incomplete")
	}
	if checkpoint.PreviousCheckpointID != "" && (!strings.HasPrefix(checkpoint.PreviousCheckpointID, "bcp_") || len(checkpoint.PreviousCheckpointID) != 68 || NormalizeHexHash(strings.TrimPrefix(checkpoint.PreviousCheckpointID, "bcp_")) == "") {
		return errors.New("checkpoint previous ID invalid")
	}
	if err := validateSignatureSet(checkpoint.ValidatorSignatures, CanonicalCheckpointPayload(checkpoint, artifact.ChainType)); err != nil {
		return fmt.Errorf("checkpoint signatures: %w", err)
	}
	if len(artifact.Proofs) == 0 {
		if checkpoint.EventRoot != EmptyEventRoot() {
			return errors.New("checkpoint without event proofs must use the canonical empty event root")
		}
		return nil
	}
	seen := make(map[string]struct{}, len(artifact.Proofs))
	for index, proof := range artifact.Proofs {
		event := eventFromProof(proof)
		if err := validateEvent(event); err != nil {
			return fmt.Errorf("proof %d: %w", index, err)
		}
		if err := validateEventForChain(chainType, event); err != nil {
			return fmt.Errorf("proof %d: %w", index, err)
		}
		if !validObservationTransactionID(chainType, event.SourceTxHash) || !validObservationBlockID(chainType, event.SourceBlockHash) {
			return fmt.Errorf("proof %d source transaction or block hash invalid", index)
		}
		id := BridgeID(event)
		if proof.Version != BridgeProtocolVersion || proof.EventID != id || proof.PayloadHash != EventPayloadHash(event) || proof.MerkleProof == nil ||
			proof.MerkleProof.LeafKey != id || proof.MerkleProof.LeafValue != CanonicalEventPayload(event) || !VerifyMerkleProof(*proof.MerkleProof) {
			return fmt.Errorf("proof %d canonical event or Merkle binding invalid", index)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("proof %d duplicates bridge event %s", index, id)
		}
		seen[id] = struct{}{}
		if !strings.EqualFold(proof.SourceChainID, checkpoint.SourceChainID) || proof.CheckpointID != checkpoint.CheckpointID ||
			proof.MerkleProof.Root != checkpoint.EventRoot || proof.SourceHeight != checkpoint.Height ||
			proof.ConfirmedHeight < proof.SourceHeight || proof.ConfirmedHeight > checkpoint.ObservedHeight || !sameObservationIdentifier(chainType, proof.SourceBlockHash, checkpoint.BlockHash) {
			return fmt.Errorf("proof %d checkpoint context mismatch", index)
		}
		if proof.FinalityCheckpoint == nil || CanonicalCheckpointPayload(*proof.FinalityCheckpoint, artifact.ChainType) != CanonicalCheckpointPayload(checkpoint, artifact.ChainType) ||
			proof.FinalityCheckpoint.CheckpointID != checkpoint.CheckpointID || !sameSignatureSet(proof.FinalityCheckpoint.ValidatorSignatures, checkpoint.ValidatorSignatures) {
			return fmt.Errorf("proof %d attached checkpoint mismatch", index)
		}
		if proof.LightClientHeader == nil || proof.LightClientHeader.Height != checkpoint.Height ||
			!strings.EqualFold(proof.LightClientHeader.ChainID, checkpoint.SourceChainID) || !sameObservationIdentifier(chainType, proof.LightClientHeader.BlockHash, checkpoint.BlockHash) ||
			proof.LightClientHeader.EventRoot != checkpoint.EventRoot || proof.LightClientHeader.TxRoot != checkpoint.TransactionRoot ||
			proof.LightClientHeader.ReceiptRoot != checkpoint.ReceiptRoot || proof.LightClientHeader.StateRoot != checkpoint.StateRoot {
			return fmt.Errorf("proof %d light header mismatch", index)
		}
		if chainType == "evm" {
			if checkpoint.TransactionRoot == "" || proof.EVMReceiptProof == nil || proof.TronTransactionProof != nil {
				return fmt.Errorf("proof %d canonical EVM transaction/receipt proof missing", index)
			}
			if err := validateEVMProofBinding(proof, checkpoint); err != nil {
				return fmt.Errorf("proof %d canonical EVM inclusion: %w", index, err)
			}
		} else if chainType == "tron" {
			if checkpoint.TransactionRoot == "" || proof.TronTransactionProof == nil || proof.EVMReceiptProof != nil {
				return fmt.Errorf("proof %d canonical Tron transaction proof missing", index)
			}
			if err := bridgetronproof.Verify(*proof.TronTransactionProof, checkpoint.TransactionRoot, proof.SourceTxHash); err != nil {
				return fmt.Errorf("proof %d canonical Tron transaction inclusion: %w", index, err)
			}
		} else if proof.EVMReceiptProof != nil || proof.TronTransactionProof != nil {
			return fmt.Errorf("proof %d carries a chain-specific proof for the wrong chain", index)
		}
		if err := validateSignatureSet(proof.OracleSignatures, CanonicalEventPayload(event)); err != nil {
			return fmt.Errorf("proof %d event signatures: %w", index, err)
		}
	}
	return nil
}

func validateQuorumEvidence(evidence QuorumEvidence) error {
	if evidence.Queried < 2 || evidence.Required < 2 || evidence.Agreed < evidence.Required || evidence.Agreed > evidence.Queried ||
		NormalizeHexHash(evidence.Fingerprint) == "" || len(evidence.EndpointHashes) != evidence.Agreed {
		return errors.New("counts or fingerprint invalid")
	}
	seen := make(map[string]struct{}, len(evidence.EndpointHashes))
	for _, endpointHash := range evidence.EndpointHashes {
		endpointHash = strings.ToLower(strings.TrimSpace(endpointHash))
		if len(endpointHash) != 16 {
			return errors.New("endpoint hash must be 8-byte hex")
		}
		if _, err := hex.DecodeString(endpointHash); err != nil {
			return errors.New("endpoint hash must be 8-byte hex")
		}
		if _, exists := seen[endpointHash]; exists {
			return errors.New("duplicate endpoint hash")
		}
		seen[endpointHash] = struct{}{}
	}
	return nil
}

func validateEventForChain(chainType string, event Event) error {
	if NormalizeRegistryID(event.AssetDenom) == "" || !validPositiveTokenAmount(event.Amount) {
		return errors.New("asset denomination or amount is not canonical")
	}
	eventType := normalizeEventType(event.EventType)
	switch chainType {
	case "evm":
		if normalizeEVMAddress(event.EventContract) == "" || normalizeEVMAddress(event.OriginAsset) == "" {
			return errors.New("EVM event contract or token address invalid")
		}
		if eventType == "lock" && !validMSCRecipient(event.Recipient) {
			return errors.New("EVM lock recipient invalid")
		}
		if eventType == "unlock" && normalizeEVMAddress(event.Recipient) == "" {
			return errors.New("EVM unlock recipient invalid")
		}
	case "tron":
		if normalizeTronAddress(event.EventContract) == "" || normalizeTronAddress(event.OriginAsset) == "" {
			return errors.New("Tron event contract or token address invalid")
		}
		if eventType == "lock" && !validMSCRecipient(event.Recipient) {
			return errors.New("Tron lock recipient invalid")
		}
		if eventType == "unlock" && normalizeTronAddress(event.Recipient) == "" {
			return errors.New("Tron unlock recipient invalid")
		}
	case "solana":
		if normalizeSolanaPubkey(event.EventContract) == "" || normalizeSolanaPubkey(event.OriginAsset) == "" {
			return errors.New("Solana bridge program or token mint invalid")
		}
		if eventType == "lock" && !validMSCRecipient(event.Recipient) {
			return errors.New("Solana lock recipient invalid")
		}
		if eventType == "unlock" && normalizeSolanaPubkey(event.Recipient) == "" {
			return errors.New("Solana unlock recipient invalid")
		}
	default:
		return errors.New("unsupported event chain type")
	}
	return nil
}

func validPositiveTokenAmount(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") || strings.Count(value, ".") > 1 {
		return false
	}
	parts := strings.SplitN(value, ".", 2)
	if parts[0] == "" || (len(parts) == 2 && parts[1] == "") {
		return false
	}
	if len(parts[0]) > 1 && parts[0][0] == '0' {
		return false
	}
	if len(parts) == 2 && strings.HasSuffix(parts[1], "0") {
		return false
	}
	digits := strings.Join(parts, "")
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	digits = strings.TrimLeft(digits, "0")
	return digits != ""
}

func validObservationTransactionID(chainType, value string) bool {
	switch chainType {
	case "evm":
		return normalizeEVMHash(value) != ""
	case "tron":
		return normalizeTronHash(value) != ""
	case "solana":
		return normalizeSolanaSignature(value) != ""
	default:
		return false
	}
}

func validObservationBlockID(chainType, value string) bool {
	switch chainType {
	case "evm":
		return normalizeEVMHash(value) != ""
	case "tron":
		return normalizeTronHash(value) != ""
	case "solana":
		return normalizeSolanaPubkey(value) != ""
	default:
		return false
	}
}

func sameObservationIdentifier(chainType, left, right string) bool {
	if chainType == "solana" {
		return strings.TrimSpace(left) == strings.TrimSpace(right)
	}
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func validateSignatureSet(signatures []Signature, message string) error {
	seen := make(map[string]struct{}, len(signatures))
	for _, signature := range signatures {
		publicKey := strings.ToLower(strings.TrimSpace(signature.PublicKey))
		if strings.TrimSpace(signature.Signer) == "" || publicKey == "" || !verifySignature(signature, message) {
			return errors.New("invalid Ed25519 signature")
		}
		if _, exists := seen[publicKey]; exists {
			return errors.New("duplicate signer public key")
		}
		seen[publicKey] = struct{}{}
	}
	return nil
}

func sameSignatureSet(left, right []Signature) bool {
	if len(left) != len(right) {
		return false
	}
	canonical := func(values []Signature) []string {
		out := make([]string, 0, len(values))
		for _, value := range values {
			out = append(out, strings.Join([]string{
				strings.TrimSpace(value.Signer), strings.ToLower(strings.TrimSpace(value.PublicKey)), strings.ToLower(strings.TrimSpace(value.Signature)),
			}, "|"))
		}
		sort.Strings(out)
		return out
	}
	leftCanonical := canonical(left)
	rightCanonical := canonical(right)
	for index := range leftCanonical {
		if leftCanonical[index] != rightCanonical[index] {
			return false
		}
	}
	return true
}

func SignArtifact(artifact *Artifact, signer string, privateKey ed25519.PrivateKey) error {
	if artifact == nil {
		return errors.New("artifact required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return errors.New("Ed25519 private key must be 64 bytes")
	}
	if strings.TrimSpace(signer) == "" {
		return errors.New("signer ID required")
	}
	if err := ValidateArtifact(*artifact); err != nil {
		return fmt.Errorf("refusing to sign invalid artifact: %w", err)
	}
	checkpoint := normalizeCheckpoint(artifact.Checkpoint, artifact.ChainType)
	if checkpoint.Version != BridgeCheckpointVersion || checkpoint.CheckpointID != CheckpointID(checkpoint, artifact.ChainType) {
		return errors.New("artifact checkpoint is not canonical")
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	signature := Signature{
		Signer: signer, PublicKey: hex.EncodeToString(publicKey),
		Signature: hex.EncodeToString(ed25519.Sign(privateKey, []byte(CanonicalCheckpointPayload(checkpoint, artifact.ChainType)))),
	}
	artifact.Checkpoint.ValidatorSignatures = mergeSignature(artifact.Checkpoint.ValidatorSignatures, signature)
	for index := range artifact.Proofs {
		event := eventFromProof(artifact.Proofs[index])
		eventSignature := Signature{
			Signer: signer, PublicKey: hex.EncodeToString(publicKey),
			Signature: hex.EncodeToString(ed25519.Sign(privateKey, []byte(CanonicalEventPayload(event)))),
		}
		artifact.Proofs[index].OracleSignatures = mergeSignature(artifact.Proofs[index].OracleSignatures, eventSignature)
		checkpointCopy := artifact.Checkpoint
		checkpointCopy.ValidatorSignatures = append([]Signature(nil), artifact.Checkpoint.ValidatorSignatures...)
		artifact.Proofs[index].CheckpointID = artifact.Checkpoint.CheckpointID
		artifact.Proofs[index].FinalityCheckpoint = &checkpointCopy
	}
	return nil
}

func eventFromProof(proof Proof) Event {
	return Event{
		SourceChainID: proof.SourceChainID, SourceTxHash: proof.SourceTxHash, LogIndex: proof.LogIndex,
		EventType: proof.EventType, WithdrawalID: proof.WithdrawalID, EventContract: proof.EventContract, AssetDenom: proof.AssetDenom,
		OriginAsset: proof.OriginAsset, Recipient: proof.Recipient, Amount: proof.Amount,
		SourceHeight: proof.SourceHeight, SourceBlockHash: proof.SourceBlockHash,
	}
}

func sameEVMReceiptProof(left, right *bridgeevmproof.Proof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func sameTronTransactionProof(left, right *bridgetronproof.Proof) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func mergeSignature(signatures []Signature, candidate Signature) []Signature {
	key := strings.ToLower(strings.TrimSpace(candidate.PublicKey))
	for index := range signatures {
		if strings.EqualFold(strings.TrimSpace(signatures[index].PublicKey), key) {
			signatures[index] = candidate
			return signatures
		}
	}
	return append(signatures, candidate)
}

func verifySignature(signature Signature, message string) bool {
	publicKey, publicErr := hex.DecodeString(strings.TrimSpace(signature.PublicKey))
	rawSignature, signatureErr := hex.DecodeString(strings.TrimSpace(signature.Signature))
	return publicErr == nil && signatureErr == nil && len(publicKey) == ed25519.PublicKeySize && len(rawSignature) == ed25519.SignatureSize &&
		ed25519.Verify(ed25519.PublicKey(publicKey), []byte(message), rawSignature)
}

func MergeArtifacts(artifacts []Artifact, requiredSigners int) (Artifact, error) {
	if len(artifacts) == 0 {
		return Artifact{}, errors.New("no observation artifacts supplied")
	}
	for index, artifact := range artifacts {
		if err := ValidateArtifact(artifact); err != nil {
			return Artifact{}, fmt.Errorf("artifact %d invalid: %w", index, err)
		}
	}
	rawBase, err := json.Marshal(artifacts[0])
	if err != nil {
		return Artifact{}, fmt.Errorf("clone base artifact: %w", err)
	}
	var merged Artifact
	if err := json.Unmarshal(rawBase, &merged); err != nil {
		return Artifact{}, fmt.Errorf("clone base artifact: %w", err)
	}
	checkpointPayload := CanonicalCheckpointPayload(merged.Checkpoint, merged.ChainType)
	if merged.Checkpoint.CheckpointID != CheckpointID(merged.Checkpoint, merged.ChainType) {
		return Artifact{}, errors.New("base artifact checkpoint ID mismatch")
	}
	proofs := make(map[string]*Proof, len(merged.Proofs))
	for index := range merged.Proofs {
		proof := &merged.Proofs[index]
		proofs[BridgeID(eventFromProof(*proof))] = proof
		proof.OracleSignatures = nil
	}
	merged.Checkpoint.ValidatorSignatures = nil
	for artifactIndex, artifact := range artifacts {
		if artifact.Version != ObservationVersion || !strings.EqualFold(artifact.ChainType, merged.ChainType) ||
			CanonicalCheckpointPayload(artifact.Checkpoint, artifact.ChainType) != checkpointPayload || artifact.Checkpoint.CheckpointID != merged.Checkpoint.CheckpointID {
			return Artifact{}, fmt.Errorf("artifact %d does not describe the same canonical checkpoint", artifactIndex)
		}
		if len(artifact.Proofs) != len(proofs) {
			return Artifact{}, fmt.Errorf("artifact %d proof set size mismatch", artifactIndex)
		}
		for _, signature := range artifact.Checkpoint.ValidatorSignatures {
			if !verifySignature(signature, checkpointPayload) {
				return Artifact{}, fmt.Errorf("artifact %d has invalid checkpoint signature", artifactIndex)
			}
			merged.Checkpoint.ValidatorSignatures = mergeSignature(merged.Checkpoint.ValidatorSignatures, signature)
		}
		for _, proof := range artifact.Proofs {
			id := BridgeID(eventFromProof(proof))
			target, exists := proofs[id]
			if !exists || proof.MerkleProof == nil || target.MerkleProof == nil || proof.PayloadHash != target.PayloadHash ||
				proof.MerkleProof.Root != target.MerkleProof.Root || !sameEVMReceiptProof(proof.EVMReceiptProof, target.EVMReceiptProof) ||
				!sameTronTransactionProof(proof.TronTransactionProof, target.TronTransactionProof) {
				return Artifact{}, fmt.Errorf("artifact %d proof %s mismatch", artifactIndex, id)
			}
			message := CanonicalEventPayload(eventFromProof(proof))
			for _, signature := range proof.OracleSignatures {
				if !verifySignature(signature, message) {
					return Artifact{}, fmt.Errorf("artifact %d proof %s has invalid event signature", artifactIndex, id)
				}
				target.OracleSignatures = mergeSignature(target.OracleSignatures, signature)
			}
		}
	}
	if requiredSigners > 0 && len(merged.Checkpoint.ValidatorSignatures) < requiredSigners {
		return Artifact{}, fmt.Errorf("checkpoint signatures %d below required %d", len(merged.Checkpoint.ValidatorSignatures), requiredSigners)
	}
	sort.Slice(merged.Checkpoint.ValidatorSignatures, func(i, j int) bool {
		return merged.Checkpoint.ValidatorSignatures[i].PublicKey < merged.Checkpoint.ValidatorSignatures[j].PublicKey
	})
	for index := range merged.Proofs {
		if requiredSigners > 0 && len(merged.Proofs[index].OracleSignatures) < requiredSigners {
			return Artifact{}, fmt.Errorf("proof %s signatures %d below required %d", merged.Proofs[index].EventID, len(merged.Proofs[index].OracleSignatures), requiredSigners)
		}
		sort.Slice(merged.Proofs[index].OracleSignatures, func(i, j int) bool {
			return merged.Proofs[index].OracleSignatures[i].PublicKey < merged.Proofs[index].OracleSignatures[j].PublicKey
		})
		checkpointCopy := merged.Checkpoint
		checkpointCopy.ValidatorSignatures = append([]Signature(nil), merged.Checkpoint.ValidatorSignatures...)
		merged.Proofs[index].CheckpointID = merged.Checkpoint.CheckpointID
		merged.Proofs[index].FinalityCheckpoint = &checkpointCopy
	}
	if err := ValidateArtifact(merged); err != nil {
		return Artifact{}, fmt.Errorf("merged artifact invalid: %w", err)
	}
	return merged, nil
}
