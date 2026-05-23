package main

import "testing"

func TestComputeNextEVMBaseFeeDirection(t *testing.T) {
	current := 1000
	target := evmTargetTxPerBlock()

	same := computeNextEVMBaseFee(current, target)
	if same != current {
		t.Fatalf("expected unchanged base fee at target: got=%d want=%d", same, current)
	}

	up := computeNextEVMBaseFee(current, target+1)
	if up <= current {
		t.Fatalf("expected base fee increase above target: got=%d current=%d", up, current)
	}

	down := computeNextEVMBaseFee(current, 0)
	if down >= current {
		t.Fatalf("expected base fee decrease below target: got=%d current=%d", down, current)
	}
}

func TestRequiredEVMFeeForLedgerScalesWithBaseFee(t *testing.T) {
	ledger := NewLedger()
	ledger.EVMState[evmStateKeyBaseFee] = "1000"

	if got := requiredEVMFeeForLedger(&ledger, DefaultEVMGasLimit); got != 1000 {
		t.Fatalf("unexpected default gas fee: got=%d want=1000", got)
	}

	if got := requiredEVMFeeForLedger(&ledger, DefaultEVMGasLimit/2); got != 500 {
		t.Fatalf("unexpected half-gas fee: got=%d want=500", got)
	}
}

func TestApplyBlockStateUpdatesEVMBaseFee(t *testing.T) {
	ledger := GenesisLedger()
	before := evmBaseFeeFromLedger(&ledger)

	block := Block{ID: 1, Transactions: []Transaction{}}
	next, err := ApplyBlockState(ledger, block)
	if err != nil {
		t.Fatalf("ApplyBlockState failed: %v", err)
	}

	got := evmBaseFeeFromLedger(&next)
	want := computeNextEVMBaseFee(before, 0)
	if got != want {
		t.Fatalf("unexpected next base fee: got=%d want=%d", got, want)
	}
	if next.EVMState[evmStateKeyBaseFee] == "" {
		t.Fatalf("expected base fee marker in evm state")
	}
}

func TestApplyBlockStateEVMFeeSplitBurnAndTip(t *testing.T) {
	ledger := GenesisLedger()
	sender := "MSC_EVM_TEST_SENDER"
	proposer := "MSC_EVM_TEST_PROPOSER"
	addBalance(&ledger, CoinSymbol, sender, 10_000)

	tx := Transaction{
		ID:          "evm-fee-split-1",
		Type:        TxEVM,
		From:        sender,
		Nonce:       1,
		Amount:      0,
		Coin:        CoinSymbol,
		EVMCode:     "0x00",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
	}
	required := requiredEVMFeeForLedger(&ledger, tx.EVMGasLimit)
	tip := 37
	tx.Fee = required + tip

	beforeSender := getBalance(ledger, CoinSymbol, sender)
	beforeTreasury := getBalance(ledger, CoinSymbol, TREASURY_ADDRESS)
	beforeProposer := getBalance(ledger, CoinSymbol, proposer)
	beforeSupply := currentCoinSupply(&ledger, CoinSymbol)

	block := Block{
		ID:           1,
		Proposer:     proposer,
		Transactions: []Transaction{tx},
	}
	next, err := ApplyBlockState(ledger, block)
	if err != nil {
		t.Fatalf("ApplyBlockState failed: %v", err)
	}

	afterSender := getBalance(next, CoinSymbol, sender)
	if got, want := beforeSender-afterSender, tx.Fee; got != want {
		t.Fatalf("unexpected sender fee debit: got=%d want=%d", got, want)
	}

	afterTreasury := getBalance(next, CoinSymbol, TREASURY_ADDRESS)
	if afterTreasury != beforeTreasury {
		t.Fatalf("unexpected treasury delta for evm fee split: got=%d want=%d", afterTreasury, beforeTreasury)
	}

	proposerRecipient := resolveValidatorRecipient(&next, proposer)
	afterProposer := getBalance(next, CoinSymbol, proposerRecipient)
	if got, want := afterProposer-beforeProposer, tip; got != want {
		t.Fatalf("unexpected proposer tip credit: got=%d want=%d", got, want)
	}

	afterSupply := currentCoinSupply(&next, CoinSymbol)
	if got, want := beforeSupply-afterSupply, int64(required); got != want {
		t.Fatalf("unexpected burned base fee: got=%d want=%d", got, want)
	}
}
