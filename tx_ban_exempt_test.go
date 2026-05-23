package main

import "testing"

func resetTxAbuseForTest(t *testing.T) {
	t.Helper()
	txAbuseMu.Lock()
	TxAbuse = map[string]*TxAbuseRecord{}
	txAbuseMu.Unlock()
}

func TestTxBanExemptsEVMHexAddress(t *testing.T) {
	resetTxAbuseForTest(t)

	evmAddr := "0x5B38Da6a701c568545dCfcB03FcB875f56beddC4"
	for i := 0; i < 5; i++ {
		RecordFakeTxAttempt(evmAddr)
	}

	if err := EnforceTxBan(evmAddr); err != nil {
		t.Fatalf("expected EVM address to be ban-exempt, got error: %v", err)
	}

	txAbuseMu.Lock()
	defer txAbuseMu.Unlock()
	if len(TxAbuse) != 0 {
		t.Fatalf("expected no tx abuse records for EVM exempt address, got=%d", len(TxAbuse))
	}
}

func TestTxBanStillAppliesToInternalAddresses(t *testing.T) {
	resetTxAbuseForTest(t)

	addr := "MSC01b27"
	RecordFakeTxAttempt(addr)

	if err := EnforceTxBan(addr); err == nil {
		t.Fatal("expected ban enforcement for internal address, got nil")
	}
}
