package main

import (
	"strings"
	"testing"
)

func TestIsRemovedEVMRPCMethodCompatSubset(t *testing.T) {
	prev := ConfigDTLCompatRPCSubsetEnabled
	ConfigDTLCompatRPCSubsetEnabled = false
	if !isRemovedEVMRPCMethod("msc_call") {
		t.Fatalf("msc_call should be blocked when compat subset is disabled")
	}

	ConfigDTLCompatRPCSubsetEnabled = true
	t.Cleanup(func() {
		ConfigDTLCompatRPCSubsetEnabled = prev
	})

	allowed := []string{
		"msc_chainId",
		"msc_blockNumber",
		"msc_call",
		"msc_estimateGas",
		"msc_getLogs",
		"msc_getTransactionReceipt",
		"msc_getCode",
		"msc_getStorageAt",
	}
	for _, method := range allowed {
		if isRemovedEVMRPCMethod(method) {
			t.Fatalf("%s should be allowed under compat subset", method)
		}
	}
	if !isRemovedEVMRPCMethod("msc_sendRawTransaction") {
		t.Fatalf("msc_sendRawTransaction must remain blocked")
	}
	if !isRemovedEVMRPCMethod("eth_chainId") {
		t.Fatalf("eth_* methods must be blocked")
	}
}

func TestDecodeDTLEthCallArgs(t *testing.T) {
	method := DTLLogicPackABIMethod{
		Name: "balanceOf",
		Args: []DTLLogicPackArg{
			{Name: "owner", Type: "address"},
		},
		Returns: []string{"u64"},
	}
	selector := dtlABIMethodSelectorHex(method)
	if selector == "" {
		t.Fatalf("selector must not be empty")
	}
	word, err := abiEncodeAddressWord("0x1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("encode address word: %v", err)
	}
	callDataHex := "0x" + selector + word
	callData, err := decodeHexBytes(callDataHex)
	if err != nil {
		t.Fatalf("decode calldata: %v", err)
	}
	args, err := decodeDTLEthCallArgs(method, callData)
	if err != nil {
		t.Fatalf("decode args: %v", err)
	}
	got := strings.TrimSpace(args["owner"])
	want := "0x1111111111111111111111111111111111111111"
	if !strings.EqualFold(got, want) {
		t.Fatalf("owner mismatch: got %s want %s", got, want)
	}
}

func TestEncodeDTLEthCallResultU64(t *testing.T) {
	method := DTLLogicPackABIMethod{
		Name:    "totalSupply",
		Returns: []string{"u64"},
	}
	out, err := encodeDTLEthCallResult(method, dtlLogicCallResult{Kind: "u64", U64: 42})
	if err != nil {
		t.Fatalf("encode result: %v", err)
	}
	if !strings.HasSuffix(strings.ToLower(out), "2a") {
		t.Fatalf("unexpected encoded output: %s", out)
	}
}

func TestDTLStorageBySlot(t *testing.T) {
	contract := &DTLContractState{
		Storage: map[string]string{
			"count": "7",
			"label": "Talha",
		},
	}
	slotByHash := dtlStorageSlotHash("count")
	out := dtlStorageBySlot(contract, slotByHash)
	n, err := abiDecodeUint256(out)
	if err != nil {
		t.Fatalf("decode slot value: %v", err)
	}
	if n.Uint64() != 7 {
		t.Fatalf("slot value mismatch: got %d want 7", n.Uint64())
	}

	outIndex := dtlStorageBySlot(contract, "0x0")
	n2, err := abiDecodeUint256(outIndex)
	if err != nil {
		t.Fatalf("decode index slot value: %v", err)
	}
	if n2.Uint64() != 7 {
		t.Fatalf("index slot value mismatch: got %d want 7", n2.Uint64())
	}

	unknown := dtlStorageBySlot(contract, "0x9999")
	if !strings.EqualFold(unknown, zeroEVMWordHex) {
		t.Fatalf("unknown slot expected zero word, got %s", unknown)
	}
}
