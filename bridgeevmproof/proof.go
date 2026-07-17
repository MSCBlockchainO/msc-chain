package bridgeevmproof

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

const (
	Version            = "msc-evm-receipt-proof-v1"
	maxProofNodes      = 128
	maxProofNodeBytes  = 1 << 20
	maxTotalProofBytes = 4 << 20
	maxReceiptLogs     = 1 << 16
)

// Proof binds a transaction hash and one transaction-local receipt log to the
// canonical transaction and receipt roots in an EVM block header.
type Proof struct {
	Version          string   `json:"version"`
	TransactionIndex uint64   `json:"transaction_index"`
	ReceiptLogIndex  uint64   `json:"receipt_log_index"`
	TransactionNodes []string `json:"transaction_proof_nodes"`
	ReceiptNodes     []string `json:"receipt_proof_nodes"`
}

// Builder holds canonical block tries so multiple bridge events in one block
// can receive proofs without rebuilding both tries for every event.
type Builder struct {
	transactions    types.Transactions
	receipts        types.Receipts
	transactionTrie *trie.Trie
	receiptTrie     *trie.Trie
	transactionRoot common.Hash
	receiptRoot     common.Hash
}

func buildTrie(list types.DerivableList) (*trie.Trie, common.Hash, error) {
	database := triedb.NewDatabase(rawdb.NewMemoryDatabase(), nil)
	tree := trie.NewEmpty(database)
	var encoded bytes.Buffer
	for index := 0; index < list.Len(); index++ {
		encoded.Reset()
		list.EncodeIndex(index, &encoded)
		if encoded.Len() == 0 {
			return nil, common.Hash{}, fmt.Errorf("EVM trie value %d is empty", index)
		}
		key := rlp.AppendUint64(nil, uint64(index))
		value := append([]byte(nil), encoded.Bytes()...)
		if err := tree.Update(key, value); err != nil {
			return nil, common.Hash{}, fmt.Errorf("update EVM trie index %d: %w", index, err)
		}
	}
	return tree, tree.Hash(), nil
}

func NewBuilder(transactions types.Transactions, receipts types.Receipts) (*Builder, error) {
	if len(transactions) == 0 || len(transactions) != len(receipts) {
		return nil, errors.New("EVM block transactions and receipts must have the same non-zero length")
	}
	for index := range transactions {
		if transactions[index] == nil || receipts[index] == nil {
			return nil, fmt.Errorf("EVM transaction or receipt %d is nil", index)
		}
		if transactions[index].Type() != receipts[index].Type {
			return nil, fmt.Errorf("EVM transaction and receipt type mismatch at index %d", index)
		}
		if receipts[index].TxHash != (common.Hash{}) && receipts[index].TxHash != transactions[index].Hash() {
			return nil, fmt.Errorf("EVM receipt transaction hash mismatch at index %d", index)
		}
	}
	transactionTrie, transactionRoot, err := buildTrie(transactions)
	if err != nil {
		return nil, err
	}
	receiptTrie, receiptRoot, err := buildTrie(receipts)
	if err != nil {
		return nil, err
	}
	return &Builder{
		transactions: transactions, receipts: receipts,
		transactionTrie: transactionTrie, receiptTrie: receiptTrie,
		transactionRoot: transactionRoot, receiptRoot: receiptRoot,
	}, nil
}

func (builder *Builder) TransactionRoot() string {
	if builder == nil {
		return ""
	}
	return strings.TrimPrefix(builder.transactionRoot.Hex(), "0x")
}

func (builder *Builder) ReceiptRoot() string {
	if builder == nil {
		return ""
	}
	return strings.TrimPrefix(builder.receiptRoot.Hex(), "0x")
}

func proofNodes(tree *trie.Trie, index uint64) ([]string, error) {
	proofDB := memorydb.New()
	if err := tree.Prove(rlp.AppendUint64(nil, index), proofDB); err != nil {
		return nil, fmt.Errorf("build EVM trie proof: %w", err)
	}
	iterator := proofDB.NewIterator(nil, nil)
	defer iterator.Release()
	nodes := make([]string, 0, proofDB.Len())
	for iterator.Next() {
		nodes = append(nodes, "0x"+hex.EncodeToString(iterator.Value()))
	}
	if err := iterator.Error(); err != nil {
		return nil, fmt.Errorf("iterate EVM trie proof: %w", err)
	}
	if len(nodes) == 0 || len(nodes) > maxProofNodes {
		return nil, fmt.Errorf("EVM trie proof node count %d is invalid", len(nodes))
	}
	// memorydb iterates by hash already, but sorting makes the wire invariant explicit.
	sort.Slice(nodes, func(i, j int) bool {
		left, _ := hex.DecodeString(strings.TrimPrefix(nodes[i], "0x"))
		right, _ := hex.DecodeString(strings.TrimPrefix(nodes[j], "0x"))
		return bytes.Compare(crypto.Keccak256(left), crypto.Keccak256(right)) < 0
	})
	return nodes, nil
}

func (builder *Builder) Proof(transactionIndex, receiptLogIndex uint64) (Proof, error) {
	if builder == nil || transactionIndex >= uint64(len(builder.transactions)) {
		return Proof{}, errors.New("EVM proof transaction index is outside the block")
	}
	receipt := builder.receipts[transactionIndex]
	if receiptLogIndex >= uint64(len(receipt.Logs)) {
		return Proof{}, errors.New("EVM proof log index is outside the receipt")
	}
	transactionNodes, err := proofNodes(builder.transactionTrie, transactionIndex)
	if err != nil {
		return Proof{}, err
	}
	receiptNodes, err := proofNodes(builder.receiptTrie, transactionIndex)
	if err != nil {
		return Proof{}, err
	}
	return Proof{
		Version: Version, TransactionIndex: transactionIndex, ReceiptLogIndex: receiptLogIndex,
		TransactionNodes: transactionNodes, ReceiptNodes: receiptNodes,
	}, nil
}

type trackingProofDB struct {
	nodes map[string][]byte
	used  map[string]struct{}
}

func (database *trackingProofDB) Has(key []byte) (bool, error) {
	_, exists := database.nodes[string(key)]
	if exists {
		database.used[string(key)] = struct{}{}
	}
	return exists, nil
}

func (database *trackingProofDB) Get(key []byte) ([]byte, error) {
	value, exists := database.nodes[string(key)]
	if !exists {
		return nil, errors.New("EVM proof node not found")
	}
	database.used[string(key)] = struct{}{}
	return append([]byte(nil), value...), nil
}

var _ ethdb.KeyValueReader = (*trackingProofDB)(nil)

func decodeProofNodes(values []string) (*trackingProofDB, error) {
	if len(values) == 0 || len(values) > maxProofNodes {
		return nil, fmt.Errorf("EVM trie proof node count %d is invalid", len(values))
	}
	database := &trackingProofDB{nodes: make(map[string][]byte, len(values)), used: make(map[string]struct{}, len(values))}
	totalBytes := 0
	var previous common.Hash
	for index, value := range values {
		if value != strings.ToLower(strings.TrimSpace(value)) || !strings.HasPrefix(value, "0x") || len(value) <= 2 || len(value)%2 != 0 {
			return nil, fmt.Errorf("EVM proof node %d is not canonical lowercase 0x hex", index)
		}
		raw, err := hex.DecodeString(value[2:])
		if err != nil || len(raw) > maxProofNodeBytes {
			return nil, fmt.Errorf("EVM proof node %d is invalid", index)
		}
		totalBytes += len(raw)
		if totalBytes > maxTotalProofBytes {
			return nil, errors.New("EVM trie proof exceeds size limit")
		}
		hash := crypto.Keccak256Hash(raw)
		if index > 0 && bytes.Compare(previous[:], hash[:]) >= 0 {
			return nil, errors.New("EVM proof nodes must be unique and ordered by hash")
		}
		previous = hash
		database.nodes[string(hash[:])] = raw
	}
	return database, nil
}

func canonicalRoot(value string) (common.Hash, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimPrefix(value, "0x")
	if len(value) != 64 {
		return common.Hash{}, errors.New("EVM trie root must be a 32-byte hex value")
	}
	raw, err := hex.DecodeString(value)
	if err != nil {
		return common.Hash{}, errors.New("EVM trie root must be a 32-byte hex value")
	}
	return common.BytesToHash(raw), nil
}

func verifyTrieValue(root common.Hash, index uint64, nodes []string) ([]byte, error) {
	database, err := decodeProofNodes(nodes)
	if err != nil {
		return nil, err
	}
	value, err := trie.VerifyProof(root, rlp.AppendUint64(nil, index), database)
	if err != nil {
		return nil, fmt.Errorf("verify EVM trie proof: %w", err)
	}
	if len(value) == 0 {
		return nil, errors.New("EVM trie proof returned an empty value")
	}
	if len(database.used) != len(database.nodes) {
		return nil, errors.New("EVM trie proof contains unused nodes")
	}
	return value, nil
}

// Verify checks both block tries and returns the consensus log committed by the
// receipt. Callers must additionally decode that log against their bridge ABI.
func Verify(proof Proof, transactionRoot, receiptRoot, sourceTxHash string) (*types.Transaction, *types.Receipt, *types.Log, error) {
	if proof.Version != Version {
		return nil, nil, nil, errors.New("unsupported EVM receipt proof version")
	}
	if proof.ReceiptLogIndex >= maxReceiptLogs {
		return nil, nil, nil, errors.New("EVM receipt log index exceeds limit")
	}
	txRoot, err := canonicalRoot(transactionRoot)
	if err != nil {
		return nil, nil, nil, err
	}
	receiptsRoot, err := canonicalRoot(receiptRoot)
	if err != nil {
		return nil, nil, nil, err
	}
	transactionValue, err := verifyTrieValue(txRoot, proof.TransactionIndex, proof.TransactionNodes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("transaction inclusion: %w", err)
	}
	transaction := new(types.Transaction)
	if err := transaction.UnmarshalBinary(transactionValue); err != nil {
		return nil, nil, nil, fmt.Errorf("decode proven EVM transaction: %w", err)
	}
	wantHash := strings.ToLower(strings.TrimSpace(sourceTxHash))
	if !common.IsHexHash(wantHash) || transaction.Hash() != common.HexToHash(wantHash) {
		return nil, nil, nil, errors.New("proven EVM transaction hash mismatch")
	}
	receiptValue, err := verifyTrieValue(receiptsRoot, proof.TransactionIndex, proof.ReceiptNodes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("receipt inclusion: %w", err)
	}
	receipt := new(types.Receipt)
	if err := receipt.UnmarshalBinary(receiptValue); err != nil {
		return nil, nil, nil, fmt.Errorf("decode proven EVM receipt: %w", err)
	}
	if receipt.Type != transaction.Type() {
		return nil, nil, nil, errors.New("proven EVM transaction and receipt type mismatch")
	}
	if len(receipt.PostState) != 0 || receipt.Status != types.ReceiptStatusSuccessful {
		return nil, nil, nil, errors.New("proven EVM receipt is not a successful status receipt")
	}
	if proof.ReceiptLogIndex >= uint64(len(receipt.Logs)) || receipt.Logs[proof.ReceiptLogIndex] == nil {
		return nil, nil, nil, errors.New("proven EVM receipt log index is invalid")
	}
	return transaction, receipt, receipt.Logs[proof.ReceiptLogIndex], nil
}
