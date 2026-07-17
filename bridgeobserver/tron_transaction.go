package bridgeobserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
	"msc-chain/bridgetronproof"
)

const (
	maxTronBlockTransactions = 10_000
	maxTronRawDataBytes      = 1 << 20
	maxTronSignatures        = 32
	maxTronSignatureBytes    = 256
	maxTronResultBytes       = 1 << 20
)

type tronBlockTransaction struct {
	TxID       string            `json:"txID"`
	RawDataHex string            `json:"raw_data_hex"`
	Signatures []string          `json:"signature"`
	Results    []json.RawMessage `json:"ret"`
}

func appendTronBytesField(out []byte, number protowire.Number, value []byte) []byte {
	out = protowire.AppendTag(out, number, protowire.BytesType)
	return protowire.AppendBytes(out, value)
}

func appendTronVarintField(out []byte, number protowire.Number, value int64) []byte {
	if value == 0 {
		return out
	}
	out = protowire.AppendTag(out, number, protowire.VarintType)
	return protowire.AppendVarint(out, uint64(value))
}

func takeTronJSONField(fields map[string]json.RawMessage, names ...string) (json.RawMessage, bool, error) {
	var value json.RawMessage
	found := ""
	for _, name := range names {
		candidate, exists := fields[name]
		if !exists {
			continue
		}
		delete(fields, name)
		if found != "" {
			return nil, false, fmt.Errorf("duplicate aliases %q and %q", found, name)
		}
		found, value = name, candidate
	}
	return value, found != "", nil
}

func tronJSONInt64(raw json.RawMessage) (int64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var decoded string
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return 0, err
		}
		value = decoded
	}
	if value == "" || strings.ContainsAny(value, ".eE+") {
		return 0, errors.New("must be a base-10 integer")
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errors.New("must fit signed 64-bit integer")
	}
	return parsed, nil
}

func tronJSONString(raw json.RawMessage) (string, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", errors.New("must be a string")
	}
	return value, nil
}

func tronEnumValue(raw json.RawMessage, values map[string]int64) (int64, error) {
	if value, err := tronJSONInt64(raw); err == nil {
		for _, allowed := range values {
			if value == allowed {
				return value, nil
			}
		}
		return 0, fmt.Errorf("unknown numeric enum value %d", value)
	}
	value, err := tronJSONString(raw)
	if err != nil {
		return 0, err
	}
	parsed, exists := values[strings.ToUpper(strings.TrimSpace(value))]
	if !exists {
		return 0, fmt.Errorf("unknown enum value %q", value)
	}
	return parsed, nil
}

func appendTronResultInt(fields map[string]json.RawMessage, out []byte, number protowire.Number, names ...string) ([]byte, error) {
	raw, exists, err := takeTronJSONField(fields, names...)
	if err != nil || !exists {
		return out, err
	}
	value, err := tronJSONInt64(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", names[0], err)
	}
	return appendTronVarintField(out, number, value), nil
}

func sortedTronJSONKeys(fields map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func tronJSONHexBytes(raw json.RawMessage, name string) ([]byte, error) {
	value, err := tronJSONString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "0x"))
	if err != nil {
		return nil, fmt.Errorf("%s must be hex", name)
	}
	return decoded, nil
}

func encodeTronOrderDetail(raw json.RawMessage) ([]byte, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, errors.New("order detail must be an object")
	}
	out := make([]byte, 0, len(raw))
	for _, field := range []struct {
		name   string
		number protowire.Number
	}{{"makerOrderId", 1}, {"takerOrderId", 2}} {
		value, exists, err := takeTronJSONField(fields, field.name)
		if err != nil {
			return nil, err
		}
		if exists {
			rawID, err := tronJSONHexBytes(value, field.name)
			if err != nil {
				return nil, err
			}
			out = appendTronBytesField(out, field.number, rawID)
		}
	}
	var err error
	if out, err = appendTronResultInt(fields, out, 3, "fillSellQuantity", "fill_sell_quantity"); err != nil {
		return nil, err
	}
	if out, err = appendTronResultInt(fields, out, 4, "fillBuyQuantity", "fill_buy_quantity"); err != nil {
		return nil, err
	}
	if len(fields) != 0 {
		return nil, fmt.Errorf("unsupported order detail field %q", sortedTronJSONKeys(fields)[0])
	}
	return out, nil
}

func encodeTronTransactionResult(raw json.RawMessage) ([]byte, error) {
	if len(raw) == 0 || len(raw) > maxTronResultBytes {
		return nil, errors.New("transaction result missing or too large")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, errors.New("transaction result must be an object")
	}
	out := make([]byte, 0, len(raw))
	var err error
	if out, err = appendTronResultInt(fields, out, 1, "fee"); err != nil {
		return nil, err
	}
	if value, exists, err := takeTronJSONField(fields, "ret"); err != nil {
		return nil, err
	} else if exists {
		enum, err := tronEnumValue(value, map[string]int64{"SUCESS": 0, "SUCCESS": 0, "FAILED": 1})
		if err != nil {
			return nil, fmt.Errorf("ret: %w", err)
		}
		out = appendTronVarintField(out, 2, enum)
	}
	if value, exists, err := takeTronJSONField(fields, "contractRet", "contract_ret"); err != nil {
		return nil, err
	} else if exists {
		enum, err := tronEnumValue(value, map[string]int64{
			"DEFAULT": 0, "SUCCESS": 1, "REVERT": 2, "BAD_JUMP_DESTINATION": 3,
			"OUT_OF_MEMORY": 4, "PRECOMPILED_CONTRACT": 5, "STACK_TOO_SMALL": 6,
			"STACK_TOO_LARGE": 7, "ILLEGAL_OPERATION": 8, "STACK_OVERFLOW": 9,
			"OUT_OF_ENERGY": 10, "OUT_OF_TIME": 11, "JVM_STACK_OVER_FLOW": 12,
			"UNKNOWN": 13, "TRANSFER_FAILED": 14, "INVALID_CODE": 15,
		})
		if err != nil {
			return nil, fmt.Errorf("contractRet: %w", err)
		}
		out = appendTronVarintField(out, 3, enum)
	}
	if value, exists, err := takeTronJSONField(fields, "assetIssueID", "asset_issue_id"); err != nil {
		return nil, err
	} else if exists {
		assetID, err := tronJSONString(value)
		if err != nil {
			return nil, fmt.Errorf("assetIssueID: %w", err)
		}
		if assetID != "" {
			out = appendTronBytesField(out, 14, []byte(assetID))
		}
	}
	integerFields := []struct {
		number protowire.Number
		names  []string
	}{
		{15, []string{"withdraw_amount", "withdrawAmount"}},
		{16, []string{"unfreeze_amount", "unfreezeAmount"}},
		{18, []string{"exchange_received_amount", "exchangeReceivedAmount"}},
		{19, []string{"exchange_inject_another_amount", "exchangeInjectAnotherAmount"}},
		{20, []string{"exchange_withdraw_another_amount", "exchangeWithdrawAnotherAmount"}},
		{21, []string{"exchange_id", "exchangeId"}},
		{22, []string{"shielded_transaction_fee", "shieldedTransactionFee"}},
	}
	for _, field := range integerFields {
		out, err = appendTronResultInt(fields, out, field.number, field.names...)
		if err != nil {
			return nil, err
		}
	}
	if value, exists, err := takeTronJSONField(fields, "orderId", "order_id"); err != nil {
		return nil, err
	} else if exists {
		orderID, err := tronJSONHexBytes(value, "orderId")
		if err != nil {
			return nil, err
		}
		if len(orderID) != 0 {
			out = appendTronBytesField(out, 25, orderID)
		}
	}
	if value, exists, err := takeTronJSONField(fields, "orderDetails", "order_details"); err != nil {
		return nil, err
	} else if exists {
		var details []json.RawMessage
		if err := json.Unmarshal(value, &details); err != nil {
			return nil, errors.New("orderDetails must be an array")
		}
		for _, detail := range details {
			encoded, err := encodeTronOrderDetail(detail)
			if err != nil {
				return nil, err
			}
			out = appendTronBytesField(out, 26, encoded)
		}
	}
	if out, err = appendTronResultInt(fields, out, 27, "withdraw_expire_amount", "withdrawExpireAmount"); err != nil {
		return nil, err
	}
	if value, exists, err := takeTronJSONField(fields, "cancel_unfreezeV2_amount", "cancelUnfreezeV2Amount"); err != nil {
		return nil, err
	} else if exists {
		var entries map[string]json.RawMessage
		if err := json.Unmarshal(value, &entries); err != nil {
			return nil, errors.New("cancel_unfreezeV2_amount must be an object")
		}
		for _, key := range sortedTronJSONKeys(entries) {
			amount, err := tronJSONInt64(entries[key])
			if err != nil {
				return nil, fmt.Errorf("cancel_unfreezeV2_amount[%q]: %w", key, err)
			}
			entry := appendTronBytesField(nil, 1, []byte(key))
			entry = appendTronVarintField(entry, 2, amount)
			out = appendTronBytesField(out, 28, entry)
		}
	}
	if len(fields) != 0 {
		return nil, fmt.Errorf("unsupported transaction result field %q", sortedTronJSONKeys(fields)[0])
	}
	return out, nil
}

func encodeTronBlockTransaction(transaction tronBlockTransaction) ([]byte, error) {
	rawData, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(transaction.RawDataHex), "0x"))
	if err != nil || len(rawData) == 0 || len(rawData) > maxTronRawDataBytes {
		return nil, errors.New("raw_data_hex missing, invalid, or too large")
	}
	rawHash := sha256.Sum256(rawData)
	if normalizeTronHash(transaction.TxID) != hex.EncodeToString(rawHash[:]) {
		return nil, errors.New("txID does not match SHA-256 raw_data_hex")
	}
	if len(transaction.Signatures) > maxTronSignatures {
		return nil, errors.New("too many transaction signatures")
	}
	encoded := appendTronBytesField(nil, 1, rawData)
	for index, signatureHex := range transaction.Signatures {
		signature, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(signatureHex), "0x"))
		if err != nil || len(signature) == 0 || len(signature) > maxTronSignatureBytes {
			return nil, fmt.Errorf("signature %d must be bounded non-empty hex", index)
		}
		encoded = appendTronBytesField(encoded, 2, signature)
	}
	for index, result := range transaction.Results {
		resultBytes, err := encodeTronTransactionResult(result)
		if err != nil {
			return nil, fmt.Errorf("result %d: %w", index, err)
		}
		encoded = appendTronBytesField(encoded, 5, resultBytes)
	}
	return encoded, nil
}

func tronTransactionMerkleHash(transaction tronBlockTransaction) ([]byte, error) {
	encoded, err := encodeTronBlockTransaction(transaction)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func tronMerkleRoot(hashes [][]byte) ([]byte, error) {
	if len(hashes) == 0 {
		return make([]byte, sha256.Size), nil
	}
	level := make([][]byte, len(hashes))
	for index, hash := range hashes {
		if len(hash) != sha256.Size {
			return nil, fmt.Errorf("Merkle hash %d must be 32 bytes", index)
		}
		level[index] = append([]byte(nil), hash...)
	}
	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for index := 0; index < len(level); index += 2 {
			if index+1 == len(level) {
				next = append(next, level[index])
				continue
			}
			pair := append(append(make([]byte, 0, sha256.Size*2), level[index]...), level[index+1]...)
			hash := sha256.Sum256(pair)
			next = append(next, hash[:])
		}
		level = next
	}
	return level[0], nil
}

func tronTransactionMerkleRoot(transactions []tronBlockTransaction) (string, error) {
	if len(transactions) == 0 || len(transactions) > maxTronBlockTransactions {
		return "", errors.New("transaction list must be non-empty and bounded")
	}
	hashes := make([][]byte, len(transactions))
	for index, transaction := range transactions {
		hash, err := tronTransactionMerkleHash(transaction)
		if err != nil {
			return "", fmt.Errorf("transaction %d: %w", index, err)
		}
		hashes[index] = hash
	}
	root, err := tronMerkleRoot(hashes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(root), nil
}

func attachTronTransactionProofs(proofs []Proof, block tronBlock) error {
	transactions := make([][]byte, len(block.Transactions))
	transactionIndexes := make(map[string]int, len(block.Transactions))
	for index, transaction := range block.Transactions {
		encoded, err := encodeTronBlockTransaction(transaction)
		if err != nil {
			return fmt.Errorf("encode transaction %d: %w", index, err)
		}
		transactions[index] = encoded
		transactionIndexes[normalizeTronHash(transaction.TxID)] = index
	}
	targets := make([]int, 0, len(proofs))
	proofTargets := make([]int, len(proofs))
	for index, proof := range proofs {
		target, exists := transactionIndexes[normalizeTronHash(proof.SourceTxHash)]
		if !exists {
			return fmt.Errorf("event transaction %s is absent from the solidified block", proof.SourceTxHash)
		}
		targets = append(targets, target)
		proofTargets[index] = target
	}
	nativeProofs, root, err := bridgetronproof.BuildProofs(transactions, targets)
	if err != nil {
		return fmt.Errorf("build Tron transaction proofs: %w", err)
	}
	if root != normalizeTronHash(block.BlockHeader.RawData.TransactionRoot) {
		return errors.New("built Tron transaction proof root does not match txTrieRoot")
	}
	for index, target := range proofTargets {
		nativeProof := nativeProofs[target]
		proofs[index].TronTransactionProof = &nativeProof
	}
	return nil
}
