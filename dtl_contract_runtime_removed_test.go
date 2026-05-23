package main

import (
	"strings"
	"testing"
)

func TestParseDTLTxTypeRejectsContractKinds(t *testing.T) {
	for _, raw := range []string{string(DTLTxContractDeploy), string(DTLTxContractCall)} {
		if _, err := parseDTLTxType(raw); err == nil {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}
}

func TestValidateDTLTransactionRejectsContractKinds(t *testing.T) {
	ledger := NewLedger()
	tx := Transaction{
		Type:       TxDTL,
		DTLTxType:  string(DTLTxContractDeploy),
		DTLPayload: "{}",
	}
	err := validateDTLTransaction(&ledger, tx, 1)
	if err == nil {
		t.Fatalf("expected contract tx validation failure")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "contract runtime removed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureNoDTLContractHistoryFailsOnChainTx(t *testing.T) {
	n := &Node{
		Blockchain: &Blockchain{
			Blocks: []Block{
				{
					ID: 1,
					Transactions: []Transaction{
						{
							ID:        "tx-contract",
							Type:      TxDTL,
							DTLTxType: string(DTLTxContractCall),
						},
					},
				},
			},
		},
	}
	err := n.ensureNoDTLContractHistory()
	if err == nil {
		t.Fatalf("expected startup history gate failure")
	}
}

func TestEnsureNoDTLContractHistoryFailsOnLedgerContracts(t *testing.T) {
	ledger := NewLedger()
	ledger.DTL.Contracts["contract_1"] = &DTLContractState{ContractID: "contract_1"}
	n := &Node{Ledger: ledger}
	err := n.ensureNoDTLContractHistory()
	if err == nil {
		t.Fatalf("expected startup contract state gate failure")
	}
}
