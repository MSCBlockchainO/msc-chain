package main

import (
	"bytes"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"msc-chain/bridgeevmproof"
)

type BridgeEVMReceiptProof = bridgeevmproof.Proof

const (
	bridgeEVMLockEventSignature   = "Locked(address,address,bytes,uint256)"
	bridgeEVMUnlockEventSignature = "Unlocked(bytes32,address,address,uint256)"
)

func mustBridgeEVMABIType(name string) abi.Type {
	typeValue, err := abi.NewType(name, "", nil)
	if err != nil {
		panic(err)
	}
	return typeValue
}

var (
	bridgeEVMBytesType   = mustBridgeEVMABIType("bytes")
	bridgeEVMUint256Type = mustBridgeEVMABIType("uint256")
	bridgeEVMLockDataABI = abi.Arguments{
		{Type: bridgeEVMBytesType},
		{Type: bridgeEVMUint256Type},
	}
	bridgeEVMUnlockDataABI = abi.Arguments{{Type: bridgeEVMUint256Type}}
	bridgeEVMLockTopic     = crypto.Keccak256Hash([]byte(bridgeEVMLockEventSignature))
	bridgeEVMUnlockTopic   = crypto.Keccak256Hash([]byte(bridgeEVMUnlockEventSignature))
)

func bridgeEVMTopicAddress(topic common.Hash) (common.Address, bool) {
	for _, value := range topic[:12] {
		if value != 0 {
			return common.Address{}, false
		}
	}
	return common.BytesToAddress(topic[12:]), true
}

func bridgeEVMCanonicalABI(arguments abi.Arguments, data []byte) ([]any, bool) {
	values, err := arguments.Unpack(data)
	if err != nil {
		return nil, false
	}
	repacked, err := arguments.Pack(values...)
	return values, err == nil && bytes.Equal(repacked, data)
}

func verifyBridgeEVMLockLog(proof BridgeProof, asset BridgeAssetConfig, topics []common.Hash, data []byte) string {
	if len(topics) != 3 || topics[0] != bridgeEVMLockTopic {
		return "canonical_evm_lock_topics_mismatch"
	}
	token, tokenOK := bridgeEVMTopicAddress(topics[1])
	sender, senderOK := bridgeEVMTopicAddress(topics[2])
	if !tokenOK || !senderOK || sender == (common.Address{}) || !common.IsHexAddress(asset.OriginAsset) || token != common.HexToAddress(asset.OriginAsset) {
		return "canonical_evm_lock_token_or_sender_mismatch"
	}
	values, canonical := bridgeEVMCanonicalABI(bridgeEVMLockDataABI, data)
	if !canonical || len(values) != 2 {
		return "canonical_evm_lock_data_invalid"
	}
	recipient, recipientOK := values[0].([]byte)
	amount, amountOK := values[1].(*big.Int)
	expectedAmount, err := bridgeDecimalUnits(proof.Amount, asset.Decimals)
	if !recipientOK || !amountOK || amount == nil || err != nil || expectedAmount.Sign() <= 0 ||
		!bytes.Equal(recipient, []byte(strings.TrimSpace(proof.Recipient))) || amount.Cmp(expectedAmount) != 0 {
		return "canonical_evm_lock_payload_mismatch"
	}
	return ""
}

func verifyBridgeEVMUnlockLog(proof BridgeProof, asset BridgeAssetConfig, topics []common.Hash, data []byte) string {
	if len(topics) != 4 || topics[0] != bridgeEVMUnlockTopic {
		return "canonical_evm_unlock_topics_mismatch"
	}
	withdrawalID := canonicalBridgeWithdrawalID(proof.WithdrawalID)
	token, tokenOK := bridgeEVMTopicAddress(topics[2])
	recipient, recipientOK := bridgeEVMTopicAddress(topics[3])
	if withdrawalID == "" || topics[1] != common.HexToHash(withdrawalID) || !tokenOK || !recipientOK ||
		!common.IsHexAddress(asset.OriginAsset) || token != common.HexToAddress(asset.OriginAsset) ||
		!common.IsHexAddress(proof.Recipient) || recipient != common.HexToAddress(proof.Recipient) {
		return "canonical_evm_unlock_identity_mismatch"
	}
	values, canonical := bridgeEVMCanonicalABI(bridgeEVMUnlockDataABI, data)
	if !canonical || len(values) != 1 {
		return "canonical_evm_unlock_data_invalid"
	}
	amount, amountOK := values[0].(*big.Int)
	expectedAmount, err := bridgeDecimalUnits(proof.Amount, asset.Decimals)
	if !amountOK || amount == nil || err != nil || expectedAmount.Sign() <= 0 || amount.Cmp(expectedAmount) != 0 {
		return "canonical_evm_unlock_amount_mismatch"
	}
	return ""
}

// verifyBridgeEVMReceiptProof proves the source tx and successful receipt at
// the same transaction index, then binds the proven log to the bridge payload.
func verifyBridgeEVMReceiptProof(proof BridgeProof, asset BridgeAssetConfig, checkpoint BridgeFinalityCheckpoint) string {
	if proof.EVMReceiptProof == nil || proof.LogIndex != proof.EVMReceiptProof.ReceiptLogIndex {
		return "canonical_evm_receipt_log_index_mismatch"
	}
	_, _, provenLog, err := bridgeevmproof.Verify(
		*proof.EVMReceiptProof,
		checkpoint.TransactionRoot,
		checkpoint.ReceiptRoot,
		proof.SourceTxHash,
	)
	if err != nil {
		return "canonical_evm_receipt_proof_invalid: " + err.Error()
	}
	if !common.IsHexAddress(proof.EventContract) || provenLog.Address != common.HexToAddress(proof.EventContract) {
		return "canonical_evm_event_contract_mismatch"
	}
	switch bridgeProofEventType(proof) {
	case "lock":
		return verifyBridgeEVMLockLog(proof, asset, provenLog.Topics, provenLog.Data)
	case "unlock":
		return verifyBridgeEVMUnlockLog(proof, asset, provenLog.Topics, provenLog.Data)
	default:
		return fmt.Sprintf("canonical_evm_event_type_unsupported: %s", bridgeProofEventType(proof))
	}
}
