package bridgetronproof

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

const (
	Version             = "msc-tron-transaction-proof-v1"
	maxTransactions     = 10_000
	maxTransactionBytes = 1 << 20
	maxRawDataBytes     = 1 << 20
	maxSignatures       = 32
	maxSignatureBytes   = 256
	maxResults          = 32
	maxResultBytes      = 1 << 20
)

type Sibling struct {
	Position string `json:"position"`
	Hash     string `json:"hash"`
}

type Proof struct {
	Version             string    `json:"version"`
	TransactionIndex    uint64    `json:"transaction_index"`
	TransactionCount    uint64    `json:"transaction_count"`
	TransactionBytesHex string    `json:"transaction_bytes_hex"`
	Siblings            []Sibling `json:"siblings,omitempty"`
}

func normalizeHash(value string) string {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if len(value) != sha256.Size*2 {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func transactionRawData(transaction []byte) ([]byte, error) {
	if len(transaction) == 0 || len(transaction) > maxTransactionBytes {
		return nil, errors.New("transaction bytes missing or too large")
	}
	remaining := transaction
	var rawData []byte
	lastField := protowire.Number(0)
	signatures, results := 0, 0
	for len(remaining) > 0 {
		number, wireType, tagBytes := protowire.ConsumeTag(remaining)
		if tagBytes < 0 || number < lastField || wireType != protowire.BytesType {
			return nil, errors.New("transaction protobuf fields are malformed or non-canonical")
		}
		remaining = remaining[tagBytes:]
		value, valueBytes := protowire.ConsumeBytes(remaining)
		if valueBytes < 0 {
			return nil, errors.New("transaction protobuf bytes field is malformed")
		}
		remaining = remaining[valueBytes:]
		lastField = number
		switch number {
		case 1:
			if rawData != nil || len(value) == 0 || len(value) > maxRawDataBytes {
				return nil, errors.New("transaction raw_data field is missing, duplicate, or too large")
			}
			rawData = append([]byte(nil), value...)
		case 2:
			signatures++
			if signatures > maxSignatures || len(value) == 0 || len(value) > maxSignatureBytes {
				return nil, errors.New("transaction signature set is invalid")
			}
		case 5:
			results++
			if results > maxResults || len(value) > maxResultBytes {
				return nil, errors.New("transaction result set is invalid")
			}
		default:
			return nil, fmt.Errorf("unsupported transaction protobuf field %d", number)
		}
	}
	if rawData == nil {
		return nil, errors.New("transaction raw_data field is required")
	}
	return rawData, nil
}

func treeLevels(transactions [][]byte) ([][][]byte, error) {
	if len(transactions) == 0 || len(transactions) > maxTransactions {
		return nil, errors.New("transaction list must be non-empty and bounded")
	}
	leaves := make([][]byte, len(transactions))
	for index, transaction := range transactions {
		if _, err := transactionRawData(transaction); err != nil {
			return nil, fmt.Errorf("transaction %d: %w", index, err)
		}
		hash := sha256.Sum256(transaction)
		leaves[index] = hash[:]
	}
	levels := [][][]byte{leaves}
	for len(levels[len(levels)-1]) > 1 {
		current := levels[len(levels)-1]
		next := make([][]byte, 0, (len(current)+1)/2)
		for index := 0; index < len(current); index += 2 {
			if index+1 == len(current) {
				next = append(next, append([]byte(nil), current[index]...))
				continue
			}
			pair := append(append(make([]byte, 0, sha256.Size*2), current[index]...), current[index+1]...)
			hash := sha256.Sum256(pair)
			next = append(next, hash[:])
		}
		levels = append(levels, next)
	}
	return levels, nil
}

func BuildProofs(transactions [][]byte, targets []int) (map[int]Proof, string, error) {
	levels, err := treeLevels(transactions)
	if err != nil {
		return nil, "", err
	}
	proofs := make(map[int]Proof, len(targets))
	for _, target := range targets {
		if target < 0 || target >= len(transactions) {
			return nil, "", fmt.Errorf("transaction proof target %d is out of range", target)
		}
		if _, exists := proofs[target]; exists {
			continue
		}
		index := target
		siblings := make([]Sibling, 0, len(levels)-1)
		for levelIndex := 0; levelIndex < len(levels)-1; levelIndex++ {
			level := levels[levelIndex]
			if index%2 == 1 {
				siblings = append(siblings, Sibling{Position: "left", Hash: hex.EncodeToString(level[index-1])})
			} else if index+1 < len(level) {
				siblings = append(siblings, Sibling{Position: "right", Hash: hex.EncodeToString(level[index+1])})
			}
			index /= 2
		}
		proofs[target] = Proof{
			Version: Version, TransactionIndex: uint64(target), TransactionCount: uint64(len(transactions)),
			TransactionBytesHex: hex.EncodeToString(transactions[target]), Siblings: siblings,
		}
	}
	root := levels[len(levels)-1][0]
	return proofs, hex.EncodeToString(root), nil
}

func Build(transactions [][]byte, target int) (Proof, string, error) {
	proofs, root, err := BuildProofs(transactions, []int{target})
	if err != nil {
		return Proof{}, "", err
	}
	return proofs[target], root, nil
}

func Verify(proof Proof, expectedRoot, expectedTransactionID string) error {
	root := normalizeHash(expectedRoot)
	txID := normalizeHash(expectedTransactionID)
	if proof.Version != Version || root == "" || txID == "" || proof.TransactionCount == 0 ||
		proof.TransactionCount > maxTransactions || proof.TransactionIndex >= proof.TransactionCount {
		return errors.New("transaction proof context is invalid")
	}
	transactionHex := strings.TrimPrefix(strings.TrimSpace(proof.TransactionBytesHex), "0x")
	if len(transactionHex) == 0 || len(transactionHex) > maxTransactionBytes*2 {
		return errors.New("transaction proof bytes missing or too large")
	}
	transaction, err := hex.DecodeString(transactionHex)
	if err != nil {
		return errors.New("transaction proof bytes must be hex")
	}
	rawData, err := transactionRawData(transaction)
	if err != nil {
		return err
	}
	rawHash := sha256.Sum256(rawData)
	if hex.EncodeToString(rawHash[:]) != txID {
		return errors.New("transaction proof raw_data does not match source transaction ID")
	}
	leaf := sha256.Sum256(transaction)
	current := leaf[:]
	index, count, siblingIndex := proof.TransactionIndex, proof.TransactionCount, 0
	for count > 1 {
		hasSibling := index%2 == 1 || index+1 < count
		if hasSibling {
			if siblingIndex >= len(proof.Siblings) {
				return errors.New("transaction proof is missing a Merkle sibling")
			}
			sibling := proof.Siblings[siblingIndex]
			siblingIndex++
			siblingHash := normalizeHash(sibling.Hash)
			siblingBytes, err := hex.DecodeString(siblingHash)
			if siblingHash == "" || err != nil {
				return errors.New("transaction proof sibling hash is invalid")
			}
			position := "right"
			if index%2 == 1 {
				position = "left"
			}
			if sibling.Position != position {
				return errors.New("transaction proof sibling position is invalid")
			}
			pair := make([]byte, 0, sha256.Size*2)
			if position == "left" {
				pair = append(pair, siblingBytes...)
				pair = append(pair, current...)
			} else {
				pair = append(pair, current...)
				pair = append(pair, siblingBytes...)
			}
			hash := sha256.Sum256(pair)
			current = hash[:]
		}
		index /= 2
		count = (count + 1) / 2
	}
	if siblingIndex != len(proof.Siblings) {
		return errors.New("transaction proof contains unused Merkle siblings")
	}
	if hex.EncodeToString(current) != root {
		return errors.New("transaction proof does not match txTrieRoot")
	}
	return nil
}
