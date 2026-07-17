package main

import (
	"errors"
	"fmt"
	"strings"
)

var ErrDTLContractRuntimeRemoved = errors.New("dtl: contract runtime removed")

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

func firstDTLContractTxInBlock(block Block) (Transaction, bool) {
	for _, tx := range block.Transactions {
		if isDTLContractTransaction(tx) {
			return tx, true
		}
	}
	return Transaction{}, false
}

func (n *Node) ensureNoDTLContractHistory() error {
	if n == nil {
		return nil
	}
	if n.Blockchain != nil {
		n.Blockchain.mu.RLock()
		blocks := append([]Block(nil), n.Blockchain.Blocks...)
		n.Blockchain.mu.RUnlock()
		for _, blk := range blocks {
			if tx, ok := firstDTLContractTxInBlock(blk); ok {
				return fmt.Errorf(
					"dtl contract runtime removed: historical contract tx detected height=%d tx_id=%s dtl_tx_type=%s",
					blk.ID,
					strings.TrimSpace(tx.ID),
					strings.TrimSpace(tx.DTLTxType),
				)
			}
		}
	}
	if n.Ledger.DTL != nil && len(n.Ledger.DTL.Contracts) > 0 {
		return fmt.Errorf(
			"dtl contract runtime removed: historical contract state detected contracts=%d",
			len(n.Ledger.DTL.Contracts),
		)
	}
	return nil
}
