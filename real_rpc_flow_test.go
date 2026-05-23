package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func realRPCPostJSON(t *testing.T, base, path string, req any, out any) (int, string) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	resp, err := http.Post(strings.TrimRight(base, "/")+path, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", path, err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	_, _ = raw.ReadFrom(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && out != nil {
		if err := json.Unmarshal(raw.Bytes(), out); err != nil {
			t.Fatalf("decode %s response %q: %v", path, raw.String(), err)
		}
	}
	return resp.StatusCode, raw.String()
}

func realRPCGetJSON(t *testing.T, base, path string, out any) int {
	t.Helper()
	resp, err := http.Get(strings.TrimRight(base, "/") + path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s response: %v", path, err)
		}
	}
	return resp.StatusCode
}

func waitRealBalance(t *testing.T, base, addr string, minBalance int) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		var bal struct {
			Balance int `json:"balance"`
		}
		code := realRPCGetJSON(t, base, "/balance?address="+addr+"&coin="+CoinSymbol, &bal)
		if code == http.StatusOK && bal.Balance >= minBalance {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("balance for %s did not reach %d", addr, minBalance)
}

func realPendingNonce(t *testing.T, base, addr string) int {
	t.Helper()
	var out struct {
		Nonce int `json:"nonce"`
	}
	code := realRPCGetJSON(t, base, "/nonce?address="+addr, &out)
	if code != http.StatusOK {
		t.Fatalf("nonce endpoint returned %d", code)
	}
	return out.Nonce
}

func waitRealStake(t *testing.T, base, addr, validatorID string, minStake int) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		var out struct {
			ValidatorID string `json:"validator_id"`
			Stake       int    `json:"stake"`
		}
		code := realRPCGetJSON(t, base, "/wallet/status?address="+addr, &out)
		if code == http.StatusOK && strings.EqualFold(out.ValidatorID, validatorID) && out.Stake >= minStake {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("stake for %s -> %s did not reach %d", addr, validatorID, minStake)
}

func TestRealRPCTransferStakeAndLockedUnstakeFlow(t *testing.T) {
	base := strings.TrimSpace(os.Getenv("MSC_REAL_RPC"))
	if base == "" {
		t.Skip("set MSC_REAL_RPC to run real RPC flow")
	}
	validatorID := normalizeValidatorID(os.Getenv("MSC_REAL_VALIDATOR_ID"))
	validatorPubKey := strings.TrimSpace(os.Getenv("MSC_REAL_VALIDATOR_PUBKEY"))
	if validatorID == "" || validatorPubKey == "" {
		t.Skip("set MSC_REAL_VALIDATOR_ID and MSC_REAL_VALIDATOR_PUBKEY to run stake flow")
	}

	password := "real-rpc-flow-password"
	sender := newSecureWalletForFlowTest(t, password)
	receiver := newSecureWalletForFlowTest(t, password)

	var faucetResp map[string]any
	code, text := realRPCPostJSON(t, base, "/faucet", map[string]any{
		"address": sender.Address,
		"amount":  700,
		"coin":    CoinSymbol,
	}, &faucetResp)
	if code != http.StatusOK {
		t.Fatalf("faucet returned %d: %s", code, text)
	}
	waitRealBalance(t, base, sender.Address, 700)

	transfer, err := BuildSignedTxSecure(sender, password, receiver.Address, 300, realPendingNonce(t, base, sender.Address), CoinSymbol)
	if err != nil {
		t.Fatalf("build transfer: %v", err)
	}
	var submitResp map[string]any
	code, text = realRPCPostJSON(t, base, "/submitTx", transfer, &submitResp)
	if code != http.StatusOK {
		t.Fatalf("transfer submit returned %d: %s", code, text)
	}
	waitRealBalance(t, base, receiver.Address, 300)

	stake, err := BuildSignedStakeTxSecure(receiver, password, validatorID, validatorPubKey, 100, realPendingNonce(t, base, receiver.Address), CoinSymbol, minUnstakeEpochs())
	if err != nil {
		t.Fatalf("build stake: %v", err)
	}
	code, text = realRPCPostJSON(t, base, "/submitTx", stake, &submitResp)
	if code != http.StatusOK {
		t.Fatalf("stake submit returned %d: %s", code, text)
	}
	waitRealStake(t, base, receiver.Address, validatorID, 100)

	unstake, err := BuildSignedUnstakeTxSecure(receiver, password, validatorID, 50, realPendingNonce(t, base, receiver.Address), CoinSymbol)
	if err != nil {
		t.Fatalf("build unstake: %v", err)
	}
	code, text = realRPCPostJSON(t, base, "/submitTx", unstake, nil)
	if code == http.StatusOK {
		t.Fatalf("locked unstake unexpectedly accepted")
	}
	if !strings.Contains(strings.ToLower(text), "stake still locked") {
		t.Fatalf("locked unstake returned %d with unexpected body: %s", code, text)
	}
	fmt.Printf("real rpc flow ok sender=%s receiver=%s validator=%s\n", sender.Address, receiver.Address, validatorID)
}
