package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func waitRealTxConfirmed(t *testing.T, base, txID string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		var out struct {
			State string `json:"state"`
		}
		code := realRPCGetJSON(t, base, "/tx/status?tx_id="+txID, &out)
		if code == http.StatusOK && strings.EqualFold(out.State, "confirmed") {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("tx %s did not confirm", txID)
}

func realBalance(t *testing.T, base, addr string) int {
	t.Helper()
	var out struct {
		Balance int `json:"balance"`
	}
	code := realRPCGetJSON(t, base, "/balance?address="+addr+"&coin="+CoinSymbol+"&state=finalized", &out)
	if code != http.StatusOK {
		t.Fatalf("balance endpoint returned %d for %s", code, addr)
	}
	return out.Balance
}

func TestRealRPCFiveUserFaucetSendReceiveFlow(t *testing.T) {
	base := strings.TrimSpace(os.Getenv("MSC_REAL_RPC"))
	if base == "" {
		t.Skip("set MSC_REAL_RPC to run real five-user RPC flow")
	}

	const password = "real-five-user-flow-password"
	users := make([]SecureWallet, 5)
	for i := range users {
		users[i] = newSecureWalletForFlowTest(t, password)
	}

	for i, user := range users {
		var faucetResp map[string]any
		code, text := realRPCPostJSON(t, base, "/faucet", map[string]any{
			"address": user.Address,
			"amount":  700,
			"coin":    CoinSymbol,
		}, &faucetResp)
		if code != http.StatusOK {
			t.Fatalf("faucet user=%d returned %d: %s", i+1, code, text)
		}
		txID, _ := faucetResp["tx_id"].(string)
		if txID != "" {
			waitRealTxConfirmed(t, base, txID)
		}
		waitRealBalance(t, base, user.Address, 700)
	}

	type transferPlan struct {
		from   int
		to     int
		amount int
	}
	plans := []transferPlan{
		{from: 0, to: 1, amount: 25},
		{from: 1, to: 2, amount: 35},
		{from: 2, to: 3, amount: 45},
		{from: 3, to: 4, amount: 55},
		{from: 4, to: 0, amount: 65},
	}

	expected := make([]int, len(users))
	for i, user := range users {
		expected[i] = realBalance(t, base, user.Address)
	}

	for i, plan := range plans {
		tx, err := BuildSignedTxSecure(
			users[plan.from],
			password,
			users[plan.to].Address,
			plan.amount,
			realPendingNonce(t, base, users[plan.from].Address),
			CoinSymbol,
		)
		if err != nil {
			t.Fatalf("build transfer %d: %v", i+1, err)
		}
		var submitResp map[string]any
		code, text := realRPCPostJSON(t, base, "/submitTx", tx, &submitResp)
		if code != http.StatusOK {
			t.Fatalf("submit transfer %d returned %d: %s", i+1, code, text)
		}
		txID := tx.ID
		if got, _ := submitResp["tx_id"].(string); strings.TrimSpace(got) != "" {
			txID = got
		}
		waitRealTxConfirmed(t, base, txID)
		expected[plan.from] -= plan.amount + tx.Fee
		expected[plan.to] += plan.amount
		waitRealBalance(t, base, users[plan.to].Address, expected[plan.to])
	}

	for i, user := range users {
		got := realBalance(t, base, user.Address)
		if got != expected[i] {
			t.Fatalf("user %d balance mismatch: got=%d want=%d address=%s", i+1, got, expected[i], user.Address)
		}
		fmt.Printf("user%d=%s balance=%d\n", i+1, user.Address, got)
	}
}
