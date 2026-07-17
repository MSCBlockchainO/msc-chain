package main

import (
	"errors"
	"fmt"
	"strings"
)

// ErrDTLContractRuntimeRemoved is returned by every historical contract/VM
// entry point. Contract execution is not part of the MSC protocol.
var ErrDTLContractRuntimeRemoved = errors.New("dtl: contract runtime removed")

// dtlContractRuntimeRemoved is a deterministic consensus guard for removed
// contract execution. It deliberately has no configuration override.
func dtlContractRuntimeRemoved() bool {
	return true
}

func dtlContractRuntimeRemovedError(op string) error {
	op = strings.TrimSpace(op)
	if op == "" {
		return ErrDTLContractRuntimeRemoved
	}
	return fmt.Errorf("%w: %s", ErrDTLContractRuntimeRemoved, op)
}

func isDTLContractTxKind(kind DTLTxType) bool {
	return kind == DTLTxContractDeploy || kind == DTLTxContractCall
}

func isDTLContractTxTypeRaw(raw string) bool {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case string(DTLTxContractDeploy), string(DTLTxContractCall):
		return true
	default:
		return false
	}
}

func isDTLContractTransaction(tx Transaction) bool {
	return tx.Type == TxDTL && isDTLContractTxTypeRaw(tx.DTLTxType)
}

// firstDTLContractTxInBlock is startup-only migration/history scan support.
// It is not part of consensus block execution.
func firstDTLContractTxInBlock(block Block) (Transaction, bool) {
	for _, tx := range block.Transactions {
		if isDTLContractTransaction(tx) {
			return tx, true
		}
	}
	return Transaction{}, false
}

// normalizeDTLBytecodeFormat is retained only for canonical hashing of
// historical state. It does not decode or execute bytecode.
func normalizeDTLBytecodeFormat(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
