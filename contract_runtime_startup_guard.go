package main

import (
	"fmt"
	"strings"
)

// ensureNoDTLContractHistory implements the ensure no dtl contract history helper.
func (n *Node) ensureNoDTLContractHistory() error {
	if n == nil {
		return nil
	}
	if n.Blockchain != nil {
		n.Blockchain.mu.RLock()
		// `blocks` stores the block data handled by this operation.
		blocks := append([]Block(nil), n.Blockchain.Blocks...)
		n.Blockchain.mu.RUnlock()
		// `blk` tracks the current values while iterating.
		for _, blk := range blocks {
			// `tx` and `ok` store whether the related condition is satisfied.
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
