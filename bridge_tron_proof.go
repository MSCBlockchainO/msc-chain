package main

import "msc-chain/bridgetronproof"

type BridgeTronTransactionProof = bridgetronproof.Proof

func verifyBridgeTronTransactionProof(proof BridgeProof, checkpoint BridgeFinalityCheckpoint) string {
	if proof.TronTransactionProof == nil || checkpoint.TransactionRoot == "" {
		return "canonical_tron_transaction_proof_required"
	}
	if err := bridgetronproof.Verify(*proof.TronTransactionProof, checkpoint.TransactionRoot, proof.SourceTxHash); err != nil {
		return "canonical_tron_transaction_proof_invalid: " + err.Error()
	}
	return ""
}
