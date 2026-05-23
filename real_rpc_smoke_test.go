package main

import (
	"bytes"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type realSmokeTxResp struct {
	TxID string `json:"tx_id"`
}

type realSmokeTxStatus struct {
	State       string `json:"state"`
	Height      uint64 `json:"height"`
	IsFinalized bool   `json:"is_finalized"`
}

type realSmokeStatus struct {
	Height       uint64 `json:"height"`
	MempoolDepth int    `json:"mempool_depth"`
}

type realSmokeBalance struct {
	Balance int `json:"balance"`
}

func TestRealRPCFaucetAndFiveUserTransfers(t *testing.T) {
	if strings.TrimSpace(os.Getenv("MSC_REAL_RPC_SMOKE")) != "1" {
		t.Skip("set MSC_REAL_RPC_SMOKE=1 to run against a live local RPC node")
	}
	rpc := strings.TrimRight(strings.TrimSpace(os.Getenv("MSC_REAL_RPC_URL")), "/")
	if rpc == "" {
		rpc = "http://127.0.0.1:26658"
	}
	client := &http.Client{Timeout: 8 * time.Second}
	password := "real-rpc-smoke-pass"

	users := make([]SecureWallet, 5)
	for i := range users {
		pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
		if err != nil {
			t.Fatalf("generate user %d: %v", i, err)
		}
		enc, err := EncryptPrivateKey(priv, password)
		if err != nil {
			t.Fatalf("encrypt user %d: %v", i, err)
		}
		users[i] = SecureWallet{
			Address:   AddressFromPublicKey(pub),
			PublicKey: hex.EncodeToString(pub),
			Crypto:    enc,
		}
	}

	faucetIDs := make([]string, 0, len(users))
	for i, user := range users {
		resp := realSmokeTxResp{}
		postJSON(t, client, rpc+"/faucet", map[string]any{
			"address": user.Address,
			"amount":  100,
			"coin":    CoinSymbol,
		}, &resp)
		if resp.TxID == "" {
			t.Fatalf("faucet user %d returned empty tx_id", i)
		}
		faucetIDs = append(faucetIDs, resp.TxID)
	}
	for _, txID := range faucetIDs {
		waitRealSmokeTxFinalized(t, client, rpc, txID, 90*time.Second)
	}
	waitRealSmokeMempoolClear(t, client, rpc, 45*time.Second)

	for i, user := range users {
		bal := getRealSmokeBalance(t, client, rpc, user.Address)
		if bal < 100 {
			t.Fatalf("user %d faucet balance=%d, want >=100", i, bal)
		}
	}

	transferIDs := make([]string, 0, len(users))
	for i, user := range users {
		to := users[(i+1)%len(users)].Address
		tx, err := BuildSignedTxSecure(user, password, to, 7, 0, CoinSymbol)
		if err != nil {
			t.Fatalf("build transfer user %d: %v", i, err)
		}
		resp := realSmokeTxResp{}
		postJSON(t, client, rpc+"/submitTx", tx, &resp)
		if resp.TxID == "" {
			t.Fatalf("transfer user %d returned empty tx_id", i)
		}
		transferIDs = append(transferIDs, resp.TxID)
	}
	for _, txID := range transferIDs {
		waitRealSmokeTxFinalized(t, client, rpc, txID, 90*time.Second)
	}
	waitRealSmokeMempoolClear(t, client, rpc, 45*time.Second)

	fee := ComputeTxFee(7)
	for i, user := range users {
		bal := getRealSmokeBalance(t, client, rpc, user.Address)
		want := 100 - fee
		if bal != want {
			t.Fatalf("user %d final balance=%d, want %d", i, bal, want)
		}
	}
}

func postJSON(t *testing.T, client *http.Client, url string, in any, out any) {
	t.Helper()
	body, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal %s: %v", url, err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request %s: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		t.Fatalf("post %s status=%d body=%s", url, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
}

func getJSON(t *testing.T, client *http.Client, url string, out any) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("get %s status=%d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func waitRealSmokeTxFinalized(t *testing.T, client *http.Client, rpc string, txID string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var st realSmokeTxStatus
		getJSON(t, client, fmt.Sprintf("%s/tx/status?tx_id=%s", rpc, txID), &st)
		if st.State == "confirmed" && st.IsFinalized {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("tx %s not finalized within %s", txID, timeout)
}

func waitRealSmokeMempoolClear(t *testing.T, client *http.Client, rpc string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var st realSmokeStatus
		getJSON(t, client, rpc+"/status", &st)
		if st.MempoolDepth == 0 {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("mempool did not clear within %s", timeout)
}

func getRealSmokeBalance(t *testing.T, client *http.Client, rpc string, address string) int {
	t.Helper()
	var bal realSmokeBalance
	getJSON(t, client, fmt.Sprintf("%s/balance?address=%s&coin=%s&state=finalized", rpc, address, CoinSymbol), &bal)
	return bal.Balance
}
