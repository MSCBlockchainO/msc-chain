package main

import "testing"

func TestHydrateEVMExecutionCodeFromLedger(t *testing.T) {
	contract := "0x000000000000000000000000000000000000c0de"
	ledger := Ledger{
		EVMCode: map[string]string{
			normalizeEVMAddressKey(contract): "0x60006000f3",
		},
	}

	tx := Transaction{
		Type: TxEVM,
		From: "MSC_CALLER",
		To:   contract,
	}

	resolved, err := hydrateEVMExecutionCode(&ledger, tx)
	if err != nil {
		t.Fatalf("hydrate failed: %v", err)
	}
	if normalizeEVMHexData(resolved.EVMCode) != "0x60006000f3" {
		t.Fatalf("unexpected resolved code: %s", resolved.EVMCode)
	}
}

func TestExecuteTransactionUsesStoredContractCode(t *testing.T) {
	contract := "0x000000000000000000000000000000000000c0de"
	caller := "MSC_CALLER"

	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, caller, 10_000)
	ledger.EVMCode[normalizeEVMAddressKey(contract)] = "0x60006000f3"

	tx := Transaction{
		Type:   TxEVM,
		From:   caller,
		To:     contract,
		Nonce:  1,
		Amount: 0,
		Fee:    ComputeEVMFee(0),
		Coin:   CoinSymbol,
	}

	next, err := ExecuteTransaction(&ledger, tx, 1)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}
	if got := next.EVMState[evmStateKey(tx)]; got == "" {
		t.Fatalf("evm state hash not written")
	}
}

func TestExecuteTransactionStatefulSStorePersists(t *testing.T) {
	contract := "0x000000000000000000000000000000000000c0de"
	caller := "MSC_CALLER"

	ledger := NewLedger()
	addBalance(&ledger, CoinSymbol, caller, 10_000)
	// Runtime bytecode: SSTORE(0x00, 0x01); RETURN(0,0)
	ledger.EVMCode[normalizeEVMAddressKey(contract)] = "0x600160005560006000f3"

	tx := Transaction{
		Type:   TxEVM,
		From:   caller,
		To:     contract,
		Nonce:  1,
		Amount: 0,
		Fee:    ComputeEVMFee(0),
		Coin:   CoinSymbol,
	}

	next, err := ExecuteTransaction(&ledger, tx, 1)
	if err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	contractKey := normalizeEVMAddressKey(contract)
	slots := next.EVMStorage[contractKey]
	if slots == nil {
		t.Fatalf("missing contract storage")
	}
	slot0 := normalizeEVMStorageSlotKey("0x0")
	got := normalizeEVMStorageValue(slots[slot0])
	want := normalizeEVMStorageValue("0x1")
	if got != want {
		t.Fatalf("unexpected slot0 value: got=%s want=%s", got, want)
	}
}
