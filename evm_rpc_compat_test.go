package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"
)

func TestJSONRPCCompatBasicMethods(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 1, BlockHash: "abcd", Timestamp: 1},
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	chainReq := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`))
	chainRec := httptest.NewRecorder()
	s.handleJSONRPC(chainRec, chainReq)
	chainBody := chainRec.Body.String()
	if chainRec.Code != 200 {
		t.Fatalf("eth_chainId status=%d body=%s", chainRec.Code, chainBody)
	}
	if !strings.Contains(chainBody, `"result":"0x`) {
		t.Fatalf("eth_chainId unexpected body: %s", chainBody)
	}

	heightReq := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_blockNumber","params":[]}`))
	heightRec := httptest.NewRecorder()
	s.handleJSONRPC(heightRec, heightReq)
	heightBody := heightRec.Body.String()
	if heightRec.Code != 200 {
		t.Fatalf("eth_blockNumber status=%d body=%s", heightRec.Code, heightBody)
	}
	if !strings.Contains(heightBody, `"result":"0x1"`) {
		t.Fatalf("eth_blockNumber unexpected body: %s", heightBody)
	}
}

func TestJSONRPCCompatGetTransactionCountPendingIncludesMempool(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	sender := "0x0000000000000000000000000000000000001111"
	other := "0x0000000000000000000000000000000000002222"

	ledger := Ledger{
		Balances: map[string]int{},
		Nonces:   map[string]int{},
		Stakes:   map[string]StakeLock{},
	}
	setNonce(&ledger, sender, 5)
	setNonce(&ledger, other, 1)

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1},
				},
			},
			Ledger: ledger,
			Mempool: Mempool{
				Transactions: []Transaction{
					{ID: "0x" + strings.Repeat("b", 64), From: sender, Nonce: 6, Type: TxTransfer},
					{ID: "0x" + strings.Repeat("c", 64), From: sender, Nonce: 7, Type: TxTransfer},
					{ID: "0x" + strings.Repeat("d", 64), From: other, Nonce: 9, Type: TxTransfer},
				},
				SeenTxIDs: map[string]bool{},
			},
		},
	}

	reqLatest := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionCount","params":["`+sender+`","latest"]}`))
	recLatest := httptest.NewRecorder()
	s.handleJSONRPC(recLatest, reqLatest)
	if recLatest.Code != 200 {
		t.Fatalf("eth_getTransactionCount latest status=%d body=%s", recLatest.Code, recLatest.Body.String())
	}
	if !strings.Contains(strings.ToLower(recLatest.Body.String()), `"result":"0x5"`) {
		t.Fatalf("eth_getTransactionCount latest unexpected body: %s", recLatest.Body.String())
	}

	reqPending := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_getTransactionCount","params":["`+sender+`","pending"]}`))
	recPending := httptest.NewRecorder()
	s.handleJSONRPC(recPending, reqPending)
	if recPending.Code != 200 {
		t.Fatalf("eth_getTransactionCount pending status=%d body=%s", recPending.Code, recPending.Body.String())
	}
	if !strings.Contains(strings.ToLower(recPending.Body.String()), `"result":"0x7"`) {
		t.Fatalf("eth_getTransactionCount pending unexpected body: %s", recPending.Body.String())
	}
}

func TestJSONRPCCompatReceiptCumulativeGasUsed(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	tx1 := Transaction{
		ID:     "0x" + strings.Repeat("1", 64),
		From:   "0x0000000000000000000000000000000000001111",
		To:     "0x0000000000000000000000000000000000002222",
		Amount: 1,
		Fee:    1,
		Type:   TxTransfer,
	}
	tx2 := Transaction{
		ID:     "0x" + strings.Repeat("2", 64),
		From:   "0x0000000000000000000000000000000000001111",
		To:     "0x0000000000000000000000000000000000003333",
		Amount: 1,
		Fee:    1,
		Type:   TxTransfer,
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{
						ID:           12,
						BlockHash:    "0x" + strings.Repeat("f", 64),
						Timestamp:    1,
						Transactions: []Transaction{tx1, tx2},
					},
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["`+tx2.ID+`"]}`))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_getTransactionReceipt status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), `"gasused":"0x5208"`) {
		t.Fatalf("receipt gasUsed unexpected body: %s", rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), `"cumulativegasused":"0xa410"`) {
		t.Fatalf("receipt cumulativeGasUsed unexpected body: %s", rec.Body.String())
	}
}

func TestJSONRPCCompatEthAccountsReturnsDevKeyAddress(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	t.Setenv("MSC_EVM_DEV_PRIVKEY", hex.EncodeToString(ethcrypto.FromECDSA(key)))
	wantAddr := strings.ToLower(ethcrypto.PubkeyToAddress(key.PublicKey).Hex())

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1}},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_accounts","params":[]}`))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_accounts status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := strings.ToLower(rec.Body.String())
	if !strings.Contains(body, wantAddr) {
		t.Fatalf("eth_accounts missing dev address: want=%s body=%s", wantAddr, rec.Body.String())
	}
}

func TestJSONRPCCompatSendTransactionWithDevSigner(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	prevSubmitAuth := ConfigRPCRequireAuthForSubmitEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	ConfigRPCRequireAuthForSubmitEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
		ConfigRPCRequireAuthForSubmitEndpoints = prevSubmitAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	t.Setenv("MSC_EVM_DEV_PRIVKEY", hex.EncodeToString(ethcrypto.FromECDSA(key)))
	sender := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()

	ledger := Ledger{
		Balances: map[string]int{},
		Nonces:   map[string]int{},
		Stakes:   map[string]StakeLock{},
		EVMState: map[string]string{},
	}
	addBalance(&ledger, CoinSymbol, sender, 1_000_000)

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1}},
			},
			Ledger: ledger,
			Mempool: Mempool{
				Transactions: []Transaction{},
				SeenTxIDs:    map[string]bool{},
			},
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_sendTransaction","params":[{"from":"` + sender + `","to":"0x000000000000000000000000000000000000dEaD","gas":"0x7a120","value":"0x0","data":"0x"}]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_sendTransaction status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_sendTransaction error: %+v", resp.Error)
	}
	txHash, ok := resp.Result.(string)
	if !ok || strings.TrimSpace(txHash) == "" {
		t.Fatalf("missing tx hash result: %#v", resp.Result)
	}
	loc := s.findTxByHash(txHash)
	if !loc.Found || !loc.Pending {
		t.Fatalf("submitted tx not found in mempool by hash")
	}
	if loc.Tx.Type != TxEVM {
		t.Fatalf("expected TxEVM, got %d", loc.Tx.Type)
	}
	if strings.TrimSpace(loc.Tx.EVMRawTx) == "" {
		t.Fatalf("expected non-empty evm_raw_tx in mempool tx")
	}
}

func TestJSONRPCCompatSendTransactionWithDevSignerDynamicFeeType(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	prevSubmitAuth := ConfigRPCRequireAuthForSubmitEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	ConfigRPCRequireAuthForSubmitEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
		ConfigRPCRequireAuthForSubmitEndpoints = prevSubmitAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	t.Setenv("MSC_EVM_DEV_PRIVKEY", hex.EncodeToString(ethcrypto.FromECDSA(key)))
	sender := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()

	ledger := Ledger{
		Balances: map[string]int{},
		Nonces:   map[string]int{},
		Stakes:   map[string]StakeLock{},
		EVMState: map[string]string{},
	}
	addBalance(&ledger, CoinSymbol, sender, 1_000_000)

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1}},
			},
			Ledger: ledger,
			Mempool: Mempool{
				Transactions: []Transaction{},
				SeenTxIDs:    map[string]bool{},
			},
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_sendTransaction","params":[{"from":"` + sender + `","to":"0x000000000000000000000000000000000000dEaD","type":"0x2","gas":"0x7a120","maxFeePerGas":"0x6fc23ac00","maxPriorityFeePerGas":"0x77359400","accessList":[{"address":"0x000000000000000000000000000000000000beef","storageKeys":["0x1"]}],"value":"0x0","data":"0x"}]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_sendTransaction dynamic status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_sendTransaction dynamic error: %+v", resp.Error)
	}
	txHash, ok := resp.Result.(string)
	if !ok || strings.TrimSpace(txHash) == "" {
		t.Fatalf("missing tx hash result: %#v", resp.Result)
	}
	loc := s.findTxByHash(txHash)
	if !loc.Found || !loc.Pending {
		t.Fatalf("submitted dynamic tx not found in mempool by hash")
	}
	if strings.TrimSpace(loc.Tx.EVMRawTx) == "" {
		t.Fatalf("expected non-empty evm_raw_tx in mempool tx")
	}

	rawBytes, err := decodeHexBytes(loc.Tx.EVMRawTx)
	if err != nil {
		t.Fatalf("decode raw tx: %v", err)
	}
	var typed ethtypes.Transaction
	if err := typed.UnmarshalBinary(rawBytes); err != nil {
		t.Fatalf("unmarshal typed raw tx: %v", err)
	}
	if got := typed.Type(); got != ethtypes.DynamicFeeTxType {
		t.Fatalf("unexpected tx type: got=%d want=%d", got, ethtypes.DynamicFeeTxType)
	}
	if len(typed.AccessList()) != 1 {
		t.Fatalf("unexpected access list length: got=%d want=1", len(typed.AccessList()))
	}
}

func TestJSONRPCCompatSendTransactionUsesDemandBasedFee(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	prevSubmitAuth := ConfigRPCRequireAuthForSubmitEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	ConfigRPCRequireAuthForSubmitEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
		ConfigRPCRequireAuthForSubmitEndpoints = prevSubmitAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	t.Setenv("MSC_EVM_DEV_PRIVKEY", hex.EncodeToString(ethcrypto.FromECDSA(key)))
	sender := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()

	ledger := Ledger{
		Balances: map[string]int{},
		Nonces:   map[string]int{},
		Stakes:   map[string]StakeLock{},
		EVMState: map[string]string{},
	}
	addBalance(&ledger, CoinSymbol, sender, 1_000_000)

	demandTxs := make([]Transaction, 250)
	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1, Transactions: demandTxs}},
			},
			Ledger: ledger,
			Mempool: Mempool{
				Transactions: []Transaction{},
				SeenTxIDs:    map[string]bool{},
			},
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_sendTransaction","params":[{"from":"` + sender + `","to":"0x000000000000000000000000000000000000dEaD","gas":"0x7a120","value":"0x0","data":"0x"}]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_sendTransaction status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_sendTransaction error: %+v", resp.Error)
	}
	txHash, ok := resp.Result.(string)
	if !ok || strings.TrimSpace(txHash) == "" {
		t.Fatalf("missing tx hash result: %#v", resp.Result)
	}
	loc := s.findTxByHash(txHash)
	if !loc.Found || !loc.Pending {
		t.Fatalf("submitted tx not found in mempool by hash")
	}

	baseFee := ComputeEVMFee(DefaultEVMGasLimit)
	wantDemandFee := ComputeEVMFeeWithDemand(DefaultEVMGasLimit, len(demandTxs))
	if loc.Tx.Fee != wantDemandFee {
		t.Fatalf("unexpected demand fee: got=%d want=%d", loc.Tx.Fee, wantDemandFee)
	}
	if loc.Tx.Fee <= baseFee {
		t.Fatalf("expected demand fee > base fee: got=%d base=%d", loc.Tx.Fee, baseFee)
	}
}

func TestJSONRPCCompatFeeHistoryUsesDeterministicBaseFee(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	genesis := GenesisLedger()
	block1 := Block{ID: 1, BlockHash: "0x" + strings.Repeat("1", 64), Timestamp: 1}
	ledger1, err := ApplyBlockState(genesis, block1)
	if err != nil {
		t.Fatalf("apply block1: %v", err)
	}
	block2 := Block{ID: 2, PrevHash: block1.BlockHash, BlockHash: "0x" + strings.Repeat("2", 64), Timestamp: 2}
	ledger2, err := ApplyBlockState(ledger1, block2)
	if err != nil {
		t.Fatalf("apply block2: %v", err)
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 0, BlockHash: "0x" + strings.Repeat("0", 64), Timestamp: 0},
					block1,
					block2,
				},
			},
			Ledger: ledger2,
		},
	}

	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_feeHistory","params":["0x2","latest",[50]]}`))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_feeHistory status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_feeHistory error: %+v", resp.Error)
	}

	raw, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var got struct {
		OldestBlock   string   `json:"oldestBlock"`
		BaseFeePerGas []string `json:"baseFeePerGas"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode feeHistory result: %v", err)
	}
	if got.OldestBlock != encodeRPCQuantityUint64(1) {
		t.Fatalf("unexpected oldestBlock: got=%s want=%s", got.OldestBlock, encodeRPCQuantityUint64(1))
	}
	if len(got.BaseFeePerGas) != 3 {
		t.Fatalf("unexpected baseFeePerGas length: got=%d want=3", len(got.BaseFeePerGas))
	}

	wantBase1 := encodeRPCQuantityInt(evmBaseFeeFromLedger(&genesis))
	wantBase2 := encodeRPCQuantityInt(evmBaseFeeFromLedger(&ledger1))
	wantNext := encodeRPCQuantityInt(evmBaseFeeFromLedger(&ledger2))
	if got.BaseFeePerGas[0] != wantBase1 || got.BaseFeePerGas[1] != wantBase2 || got.BaseFeePerGas[2] != wantNext {
		t.Fatalf("unexpected baseFeePerGas: got=%v want=[%s %s %s]", got.BaseFeePerGas, wantBase1, wantBase2, wantNext)
	}
}

func TestJSONRPCCompatEVMFeeBreakdownFields(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	genesis := GenesisLedger()
	baseFee := computeEVMFeeFromBase(evmBaseFeeFromLedger(&genesis), DefaultEVMGasLimit)
	priorityFee := 19
	totalFee := baseFee + priorityFee

	tx := Transaction{
		ID:          "0x" + strings.Repeat("7", 64),
		From:        "MSC_FEE_TEST_FROM",
		To:          "",
		Amount:      0,
		Nonce:       1,
		Fee:         totalFee,
		Type:        TxEVM,
		EVMCode:     "0x00",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}
	block := Block{
		ID:           1,
		BlockHash:    "0x" + strings.Repeat("a", 64),
		PrevHash:     "0x" + strings.Repeat("0", 64),
		Timestamp:    1,
		Type:         BlockTypeTime,
		Proposer:     "A",
		Transactions: []Transaction{tx},
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 0, BlockHash: "0x" + strings.Repeat("0", 64), Timestamp: 0, Type: BlockTypeGenesis},
					block,
				},
			},
			Ledger: genesis,
			Mempool: Mempool{
				Transactions: []Transaction{},
				SeenTxIDs:    map[string]bool{},
			},
		},
	}

	reqTx := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionByHash","params":["`+tx.ID+`"]}`))
	recTx := httptest.NewRecorder()
	s.handleJSONRPC(recTx, reqTx)
	if recTx.Code != 200 {
		t.Fatalf("eth_getTransactionByHash status=%d body=%s", recTx.Code, recTx.Body.String())
	}

	var txResp jsonRPCResponse
	if err := json.Unmarshal(recTx.Body.Bytes(), &txResp); err != nil {
		t.Fatalf("decode tx response: %v", err)
	}
	if txResp.Error != nil {
		t.Fatalf("eth_getTransactionByHash error: %+v", txResp.Error)
	}
	txObj, ok := txResp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected tx object: %#v", txResp.Result)
	}
	if got, want := txObj["mscBaseFee"], encodeRPCQuantityInt(baseFee); got != want {
		t.Fatalf("unexpected mscBaseFee: got=%v want=%s", got, want)
	}
	if got, want := txObj["mscPriorityFee"], encodeRPCQuantityInt(priorityFee); got != want {
		t.Fatalf("unexpected mscPriorityFee: got=%v want=%s", got, want)
	}
	if got, want := txObj["mscFeePaid"], encodeRPCQuantityInt(totalFee); got != want {
		t.Fatalf("unexpected mscFeePaid: got=%v want=%s", got, want)
	}

	reqReceipt := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_getTransactionReceipt","params":["`+tx.ID+`"]}`))
	recReceipt := httptest.NewRecorder()
	s.handleJSONRPC(recReceipt, reqReceipt)
	if recReceipt.Code != 200 {
		t.Fatalf("eth_getTransactionReceipt status=%d body=%s", recReceipt.Code, recReceipt.Body.String())
	}

	var receiptResp jsonRPCResponse
	if err := json.Unmarshal(recReceipt.Body.Bytes(), &receiptResp); err != nil {
		t.Fatalf("decode receipt response: %v", err)
	}
	if receiptResp.Error != nil {
		t.Fatalf("eth_getTransactionReceipt error: %+v", receiptResp.Error)
	}
	receipt, ok := receiptResp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected receipt object: %#v", receiptResp.Result)
	}
	if got, want := receipt["mscBaseFee"], encodeRPCQuantityInt(baseFee); got != want {
		t.Fatalf("unexpected receipt mscBaseFee: got=%v want=%s", got, want)
	}
	if got, want := receipt["mscPriorityFee"], encodeRPCQuantityInt(priorityFee); got != want {
		t.Fatalf("unexpected receipt mscPriorityFee: got=%v want=%s", got, want)
	}
	if got, want := receipt["mscFeePaid"], encodeRPCQuantityInt(totalFee); got != want {
		t.Fatalf("unexpected receipt mscFeePaid: got=%v want=%s", got, want)
	}
	if got, want := receipt["effectiveGasPrice"], encodeRPCQuantityInt(totalFee); got != want {
		t.Fatalf("unexpected effectiveGasPrice: got=%v want=%s", got, want)
	}
}

func TestJSONRPCCompatSignTransactionWithDevSigner(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	prevSubmitAuth := ConfigRPCRequireAuthForSubmitEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	ConfigRPCRequireAuthForSubmitEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
		ConfigRPCRequireAuthForSubmitEndpoints = prevSubmitAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	t.Setenv("MSC_EVM_DEV_PRIVKEY", hex.EncodeToString(ethcrypto.FromECDSA(key)))
	sender := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1}},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_signTransaction","params":[{"from":"` + sender + `","to":"0x000000000000000000000000000000000000dEaD","type":"0x2","gas":"0x7a120","maxFeePerGas":"0x6fc23ac00","maxPriorityFeePerGas":"0x77359400","accessList":[{"address":"0x000000000000000000000000000000000000beef","storageKeys":["0x1"]}],"value":"0x0","data":"0x"}]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_signTransaction status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_signTransaction error: %+v", resp.Error)
	}

	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result shape: %#v", resp.Result)
	}
	rawTx, _ := result["raw"].(string)
	if strings.TrimSpace(rawTx) == "" {
		t.Fatalf("missing raw tx in sign result: %#v", result)
	}
	txObj, ok := result["tx"].(map[string]any)
	if !ok {
		t.Fatalf("missing tx object in sign result: %#v", result)
	}
	if gotFrom, _ := txObj["from"].(string); !strings.EqualFold(gotFrom, sender) {
		t.Fatalf("unexpected signed tx from: got=%q want=%q", gotFrom, sender)
	}

	rawBytes, err := decodeHexBytes(rawTx)
	if err != nil {
		t.Fatalf("decode signed raw tx: %v", err)
	}
	var typed ethtypes.Transaction
	if err := typed.UnmarshalBinary(rawBytes); err != nil {
		t.Fatalf("unmarshal signed raw tx: %v", err)
	}
	if got := typed.Type(); got != ethtypes.DynamicFeeTxType {
		t.Fatalf("unexpected signed tx type: got=%d want=%d", got, ethtypes.DynamicFeeTxType)
	}
	signer := ethtypes.LatestSignerForChainID(chainIDBigInt())
	from, err := ethtypes.Sender(signer, &typed)
	if err != nil {
		t.Fatalf("recover sender: %v", err)
	}
	if !strings.EqualFold(from.Hex(), sender) {
		t.Fatalf("signed tx sender mismatch: got=%s want=%s", from.Hex(), sender)
	}
}

func TestJSONRPCCompatGetProofBasicShape(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	contract := "0x000000000000000000000000000000000000c0de"
	slot := "0x1"
	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1}}},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
				EVMCode: map[string]string{
					normalizeEVMAddressKey(contract): "0x60006000f3",
				},
				EVMStorage: map[string]map[string]string{
					normalizeEVMAddressKey(contract): {
						normalizeEVMStorageSlotKey(slot): normalizeEVMStorageValue("0x2a"),
					},
				},
			},
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_getProof","params":["` + contract + `",["` + slot + `"],"latest"]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_getProof status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), `"codehash":"0x`) {
		t.Fatalf("missing codeHash in proof: %s", rec.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), `"storageproof"`) {
		t.Fatalf("missing storageProof in proof: %s", rec.Body.String())
	}
}

func TestJSONRPCCompatSignMethodsWithDevSigner(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	t.Setenv("MSC_EVM_DEV_PRIVKEY", hex.EncodeToString(ethcrypto.FromECDSA(key)))
	addr := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1}}},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	// eth_sign
	req1 := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_sign","params":["`+addr+`","0x68656c6c6f"]}`))
	rec1 := httptest.NewRecorder()
	s.handleJSONRPC(rec1, req1)
	if rec1.Code != 200 {
		t.Fatalf("eth_sign status=%d body=%s", rec1.Code, rec1.Body.String())
	}
	var r1 jsonRPCResponse
	if err := json.Unmarshal(rec1.Body.Bytes(), &r1); err != nil {
		t.Fatalf("decode eth_sign: %v", err)
	}
	if r1.Error != nil {
		t.Fatalf("eth_sign error: %+v", r1.Error)
	}
	sig1, _ := r1.Result.(string)
	if !strings.HasPrefix(strings.ToLower(sig1), "0x") || len(sig1) != 132 {
		t.Fatalf("invalid eth_sign signature: %q", sig1)
	}

	// personal_sign
	req2 := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"personal_sign","params":["0x68656c6c6f","`+addr+`"]}`))
	rec2 := httptest.NewRecorder()
	s.handleJSONRPC(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("personal_sign status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var r2 jsonRPCResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &r2); err != nil {
		t.Fatalf("decode personal_sign: %v", err)
	}
	if r2.Error != nil {
		t.Fatalf("personal_sign error: %+v", r2.Error)
	}
	sig2, _ := r2.Result.(string)
	if !strings.HasPrefix(strings.ToLower(sig2), "0x") || len(sig2) != 132 {
		t.Fatalf("invalid personal_sign signature: %q", sig2)
	}

	typed := `{"types":{"EIP712Domain":[{"name":"name","type":"string"}],"Mail":[{"name":"contents","type":"string"}]},"primaryType":"Mail","domain":{"name":"MSC"},"message":{"contents":"hello"}}`
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "eth_signTypedData_v4",
		"params":  []any{addr, typed},
	}
	body3, _ := json.Marshal(payload)
	req3 := httptest.NewRequest("POST", "/rpc", bytes.NewReader(body3))
	rec3 := httptest.NewRecorder()
	s.handleJSONRPC(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("eth_signTypedData_v4 status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	var r3 jsonRPCResponse
	if err := json.Unmarshal(rec3.Body.Bytes(), &r3); err != nil {
		t.Fatalf("decode eth_signTypedData_v4: %v", err)
	}
	if r3.Error != nil {
		t.Fatalf("eth_signTypedData_v4 error: %+v", r3.Error)
	}
	sig3, _ := r3.Result.(string)
	if !strings.HasPrefix(strings.ToLower(sig3), "0x") || len(sig3) != 132 {
		t.Fatalf("invalid eth_signTypedData_v4 signature: %q", sig3)
	}
}

func TestJSONRPCCompatCreateAccessListAndBlobBaseFee(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1}}},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
				EVMCode: map[string]string{
					normalizeEVMAddressKey("0x000000000000000000000000000000000000c0de"): "0x60006000f3",
				},
			},
		},
	}

	reqAccess := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_createAccessList","params":[{"from":"0x0000000000000000000000000000000000001111","to":"0x000000000000000000000000000000000000c0de","data":"0x"}]}`))
	recAccess := httptest.NewRecorder()
	s.handleJSONRPC(recAccess, reqAccess)
	if recAccess.Code != 200 {
		t.Fatalf("eth_createAccessList status=%d body=%s", recAccess.Code, recAccess.Body.String())
	}
	if !strings.Contains(strings.ToLower(recAccess.Body.String()), `"accesslist"`) {
		t.Fatalf("eth_createAccessList unexpected body: %s", recAccess.Body.String())
	}

	reqBlob := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_blobBaseFee","params":[]}`))
	recBlob := httptest.NewRecorder()
	s.handleJSONRPC(recBlob, reqBlob)
	if recBlob.Code != 200 {
		t.Fatalf("eth_blobBaseFee status=%d body=%s", recBlob.Code, recBlob.Body.String())
	}
	if !strings.Contains(strings.ToLower(recBlob.Body.String()), `"result":"0x0"`) {
		t.Fatalf("eth_blobBaseFee unexpected body: %s", recBlob.Body.String())
	}
}

func TestJSONRPCCompatMSC21Methods(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	// Runtime bytecode that returns uint256(42) for any call.
	tokenCode := "0x602a60005260206000f3"
	tokenAddr := "0x000000000000000000000000000000000000c0de"
	ownerAddr := "0x0000000000000000000000000000000000001111"
	spenderAddr := "0x0000000000000000000000000000000000002222"

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1}},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
				EVMCode: map[string]string{
					normalizeEVMAddressKey(tokenAddr): tokenCode,
				},
			},
		},
	}

	reqInfo := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"msc21_tokenInfo","params":["`+tokenAddr+`"]}`))
	recInfo := httptest.NewRecorder()
	s.handleJSONRPC(recInfo, reqInfo)
	if recInfo.Code != 200 {
		t.Fatalf("msc21_tokenInfo status=%d body=%s", recInfo.Code, recInfo.Body.String())
	}
	bodyInfo := strings.ToLower(recInfo.Body.String())
	if !strings.Contains(bodyInfo, `"standard":"msc-21"`) || !strings.Contains(bodyInfo, `"totalsupply":"0x2a"`) {
		t.Fatalf("msc21_tokenInfo unexpected body: %s", recInfo.Body.String())
	}

	reqBalance := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"msc21_balanceOf","params":["`+tokenAddr+`","`+ownerAddr+`"]}`))
	recBalance := httptest.NewRecorder()
	s.handleJSONRPC(recBalance, reqBalance)
	if recBalance.Code != 200 {
		t.Fatalf("msc21_balanceOf status=%d body=%s", recBalance.Code, recBalance.Body.String())
	}
	if !strings.Contains(strings.ToLower(recBalance.Body.String()), `"result":"0x2a"`) {
		t.Fatalf("msc21_balanceOf unexpected body: %s", recBalance.Body.String())
	}

	reqAllowance := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"msc21_allowance","params":["`+tokenAddr+`","`+ownerAddr+`","`+spenderAddr+`"]}`))
	recAllowance := httptest.NewRecorder()
	s.handleJSONRPC(recAllowance, reqAllowance)
	if recAllowance.Code != 200 {
		t.Fatalf("msc21_allowance status=%d body=%s", recAllowance.Code, recAllowance.Body.String())
	}
	if !strings.Contains(strings.ToLower(recAllowance.Body.String()), `"result":"0x2a"`) {
		t.Fatalf("msc21_allowance unexpected body: %s", recAllowance.Body.String())
	}

	reqIsToken := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"msc21_isToken","params":["`+tokenAddr+`"]}`))
	recIsToken := httptest.NewRecorder()
	s.handleJSONRPC(recIsToken, reqIsToken)
	if recIsToken.Code != 200 {
		t.Fatalf("msc21_isToken status=%d body=%s", recIsToken.Code, recIsToken.Body.String())
	}
	if !strings.Contains(strings.ToLower(recIsToken.Body.String()), `"result":true`) {
		t.Fatalf("msc21_isToken unexpected body: %s", recIsToken.Body.String())
	}
}

func TestJSONRPCCompatMSC721AndMSC1155Methods(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	// Runtime bytecode that returns uint256(42) for any call.
	// This is enough to validate RPC plumbing + ABI decoding paths.
	tokenCode := "0x602a60005260206000f3"
	tokenAddr := "0x000000000000000000000000000000000000c0df"
	ownerAddr := "0x0000000000000000000000000000000000001111"
	operatorAddr := "0x0000000000000000000000000000000000002222"

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{{ID: 1, BlockHash: "0x" + strings.Repeat("a", 64), Timestamp: 1}},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
				EVMCode: map[string]string{
					normalizeEVMAddressKey(tokenAddr): tokenCode,
				},
			},
		},
	}

	req721Info := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"msc721_tokenInfo","params":["`+tokenAddr+`"]}`))
	rec721Info := httptest.NewRecorder()
	s.handleJSONRPC(rec721Info, req721Info)
	if rec721Info.Code != 200 {
		t.Fatalf("msc721_tokenInfo status=%d body=%s", rec721Info.Code, rec721Info.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec721Info.Body.String()), `"standard":"msc-721"`) {
		t.Fatalf("msc721_tokenInfo unexpected body: %s", rec721Info.Body.String())
	}

	req721Bal := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"msc721_balanceOf","params":["`+tokenAddr+`","`+ownerAddr+`"]}`))
	rec721Bal := httptest.NewRecorder()
	s.handleJSONRPC(rec721Bal, req721Bal)
	if rec721Bal.Code != 200 {
		t.Fatalf("msc721_balanceOf status=%d body=%s", rec721Bal.Code, rec721Bal.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec721Bal.Body.String()), `"result":"0x2a"`) {
		t.Fatalf("msc721_balanceOf unexpected body: %s", rec721Bal.Body.String())
	}

	req721Owner := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"msc721_ownerOf","params":["`+tokenAddr+`","0x1"]}`))
	rec721Owner := httptest.NewRecorder()
	s.handleJSONRPC(rec721Owner, req721Owner)
	if rec721Owner.Code != 200 {
		t.Fatalf("msc721_ownerOf status=%d body=%s", rec721Owner.Code, rec721Owner.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec721Owner.Body.String()), `"result":"0x000000000000000000000000000000000000002a"`) {
		t.Fatalf("msc721_ownerOf unexpected body: %s", rec721Owner.Body.String())
	}

	req721IsToken := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"msc721_isToken","params":["`+tokenAddr+`"]}`))
	rec721IsToken := httptest.NewRecorder()
	s.handleJSONRPC(rec721IsToken, req721IsToken)
	if rec721IsToken.Code != 200 {
		t.Fatalf("msc721_isToken status=%d body=%s", rec721IsToken.Code, rec721IsToken.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec721IsToken.Body.String()), `"result":true`) {
		t.Fatalf("msc721_isToken unexpected body: %s", rec721IsToken.Body.String())
	}

	req1155Info := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":5,"method":"msc1155_tokenInfo","params":["`+tokenAddr+`"]}`))
	rec1155Info := httptest.NewRecorder()
	s.handleJSONRPC(rec1155Info, req1155Info)
	if rec1155Info.Code != 200 {
		t.Fatalf("msc1155_tokenInfo status=%d body=%s", rec1155Info.Code, rec1155Info.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec1155Info.Body.String()), `"standard":"msc-1155"`) {
		t.Fatalf("msc1155_tokenInfo unexpected body: %s", rec1155Info.Body.String())
	}

	req1155Bal := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":6,"method":"msc1155_balanceOf","params":["`+tokenAddr+`","`+ownerAddr+`","0x1"]}`))
	rec1155Bal := httptest.NewRecorder()
	s.handleJSONRPC(rec1155Bal, req1155Bal)
	if rec1155Bal.Code != 200 {
		t.Fatalf("msc1155_balanceOf status=%d body=%s", rec1155Bal.Code, rec1155Bal.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec1155Bal.Body.String()), `"result":"0x2a"`) {
		t.Fatalf("msc1155_balanceOf unexpected body: %s", rec1155Bal.Body.String())
	}

	req1155Approved := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"msc1155_isApprovedForAll","params":["`+tokenAddr+`","`+ownerAddr+`","`+operatorAddr+`"]}`))
	rec1155Approved := httptest.NewRecorder()
	s.handleJSONRPC(rec1155Approved, req1155Approved)
	if rec1155Approved.Code != 200 {
		t.Fatalf("msc1155_isApprovedForAll status=%d body=%s", rec1155Approved.Code, rec1155Approved.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec1155Approved.Body.String()), `"result":true`) {
		t.Fatalf("msc1155_isApprovedForAll unexpected body: %s", rec1155Approved.Body.String())
	}

	req1155IsToken := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":8,"method":"msc1155_isToken","params":["`+tokenAddr+`"]}`))
	rec1155IsToken := httptest.NewRecorder()
	s.handleJSONRPC(rec1155IsToken, req1155IsToken)
	if rec1155IsToken.Code != 200 {
		t.Fatalf("msc1155_isToken status=%d body=%s", rec1155IsToken.Code, rec1155IsToken.Body.String())
	}
	if !strings.Contains(strings.ToLower(rec1155IsToken.Body.String()), `"result":true`) {
		t.Fatalf("msc1155_isToken unexpected body: %s", rec1155IsToken.Body.String())
	}
}

func TestJSONRPCCompatHistoricalStateByBlockTag(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	fee := ComputeEVMFee(DefaultEVMGasLimit)
	sender := "MSC_TEST_SENDER"
	fundAmount := 1_000_000
	fundTx := Transaction{
		ID:      "0x" + strings.Repeat("f", 64),
		From:    USER_REWARD_POOL,
		To:      sender,
		Amount:  fundAmount,
		Nonce:   1,
		Fee:     ComputeTxFee(fundAmount),
		Type:    TxFaucet,
		Coin:    CoinSymbol,
		ChainID: ChainID,
	}
	tx1 := Transaction{
		ID:          "0x" + strings.Repeat("1", 64),
		From:        sender,
		To:          "",
		Amount:      0,
		Nonce:       1,
		Fee:         fee,
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}
	contract := deriveEVMContractAddress(tx1)
	slotWord := strings.Repeat("0", 63) + "1"
	valWord := strings.Repeat("0", 62) + "2a"
	tx2 := Transaction{
		ID:          "0x" + strings.Repeat("2", 64),
		From:        sender,
		To:          contract,
		Amount:      0,
		Nonce:       2,
		Fee:         fee,
		Type:        TxEVM,
		EVMInput:    "0x" + slotWord + valWord,
		EVMGasLimit: DefaultEVMGasLimit,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}

	block1 := Block{
		ID:           1,
		BlockHash:    "0x" + strings.Repeat("a", 64),
		PrevHash:     "0x" + strings.Repeat("0", 64),
		Timestamp:    1,
		Type:         BlockTypeTime,
		Proposer:     "A",
		Transactions: []Transaction{fundTx, tx1},
	}
	block2 := Block{
		ID:           2,
		BlockHash:    "0x" + strings.Repeat("b", 64),
		PrevHash:     block1.BlockHash,
		Timestamp:    2,
		Type:         BlockTypeTime,
		Proposer:     "B",
		Transactions: []Transaction{tx2},
	}

	genesis := GenesisLedger()
	ledger1, err := ApplyBlockState(genesis, block1)
	if err != nil {
		t.Fatalf("apply block1 failed: %v", err)
	}
	ledger2, err := ApplyBlockState(ledger1, block2)
	if err != nil {
		t.Fatalf("apply block2 failed: %v", err)
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 0, BlockHash: "0x" + strings.Repeat("0", 64), Timestamp: 0, Type: BlockTypeGenesis},
					block1,
					block2,
				},
			},
			Ledger: ledger2,
			Mempool: Mempool{
				Transactions: []Transaction{},
				SeenTxIDs:    map[string]bool{},
			},
		},
	}

	addrHex := toEVMHexAddress(sender)
	reqNonce1 := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionCount","params":["`+addrHex+`","0x1"]}`))
	recNonce1 := httptest.NewRecorder()
	s.handleJSONRPC(recNonce1, reqNonce1)
	if recNonce1.Code != 200 {
		t.Fatalf("eth_getTransactionCount@1 status=%d body=%s", recNonce1.Code, recNonce1.Body.String())
	}
	if !strings.Contains(strings.ToLower(recNonce1.Body.String()), `"result":"0x1"`) {
		t.Fatalf("eth_getTransactionCount@1 unexpected body: %s", recNonce1.Body.String())
	}

	reqNonce2 := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_getTransactionCount","params":["`+addrHex+`","0x2"]}`))
	recNonce2 := httptest.NewRecorder()
	s.handleJSONRPC(recNonce2, reqNonce2)
	if recNonce2.Code != 200 {
		t.Fatalf("eth_getTransactionCount@2 status=%d body=%s", recNonce2.Code, recNonce2.Body.String())
	}
	if !strings.Contains(strings.ToLower(recNonce2.Body.String()), `"result":"0x2"`) {
		t.Fatalf("eth_getTransactionCount@2 unexpected body: %s", recNonce2.Body.String())
	}

	slot := "0x1"
	reqStorage1 := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"eth_getStorageAt","params":["`+contract+`","`+slot+`","0x1"]}`))
	recStorage1 := httptest.NewRecorder()
	s.handleJSONRPC(recStorage1, reqStorage1)
	if recStorage1.Code != 200 {
		t.Fatalf("eth_getStorageAt@1 status=%d body=%s", recStorage1.Code, recStorage1.Body.String())
	}
	if !strings.Contains(strings.ToLower(recStorage1.Body.String()), strings.ToLower(zeroEVMWordHex)) {
		t.Fatalf("eth_getStorageAt@1 unexpected body: %s", recStorage1.Body.String())
	}

	reqStorage2 := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"eth_getStorageAt","params":["`+contract+`","`+slot+`","0x2"]}`))
	recStorage2 := httptest.NewRecorder()
	s.handleJSONRPC(recStorage2, reqStorage2)
	if recStorage2.Code != 200 {
		t.Fatalf("eth_getStorageAt@2 status=%d body=%s", recStorage2.Code, recStorage2.Body.String())
	}
	wantStorage := normalizeEVMStorageValue("0x2a")
	if !strings.Contains(strings.ToLower(recStorage2.Body.String()), strings.ToLower(wantStorage)) {
		t.Fatalf("eth_getStorageAt@2 unexpected body: %s", recStorage2.Body.String())
	}
}

func TestJSONRPCCompatDebugTraceMethods(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	tx := Transaction{
		ID:          "0x" + strings.Repeat("9", 64),
		EVMTxHash:   "0x" + strings.Repeat("a", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "0x000000000000000000000000000000000000c0de",
		Amount:      0,
		Nonce:       1,
		Fee:         ComputeEVMFee(DefaultEVMGasLimit),
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}
	block := Block{
		ID:           6,
		BlockHash:    "0x" + strings.Repeat("b", 64),
		PrevHash:     "0x" + strings.Repeat("a", 64),
		Timestamp:    1,
		Type:         BlockTypeTime,
		Proposer:     "A",
		Transactions: []Transaction{tx},
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 0, BlockHash: "0x" + strings.Repeat("0", 64), Timestamp: 0, Type: BlockTypeGenesis},
					block,
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	reqTx := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"debug_traceTransaction","params":["`+tx.EVMTxHash+`"]}`))
	recTx := httptest.NewRecorder()
	s.handleJSONRPC(recTx, reqTx)
	if recTx.Code != 200 {
		t.Fatalf("debug_traceTransaction status=%d body=%s", recTx.Code, recTx.Body.String())
	}
	if !strings.Contains(strings.ToLower(recTx.Body.String()), `"result"`) ||
		!strings.Contains(strings.ToLower(recTx.Body.String()), `"gasused"`) {
		t.Fatalf("debug_traceTransaction unexpected body: %s", recTx.Body.String())
	}

	reqByNum := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"debug_traceBlockByNumber","params":["0x6"]}`))
	recByNum := httptest.NewRecorder()
	s.handleJSONRPC(recByNum, reqByNum)
	if recByNum.Code != 200 {
		t.Fatalf("debug_traceBlockByNumber status=%d body=%s", recByNum.Code, recByNum.Body.String())
	}
	if !strings.Contains(strings.ToLower(recByNum.Body.String()), `"result":[`) {
		t.Fatalf("debug_traceBlockByNumber unexpected body: %s", recByNum.Body.String())
	}

	reqByHash := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"debug_traceBlockByHash","params":["`+block.BlockHash+`"]}`))
	recByHash := httptest.NewRecorder()
	s.handleJSONRPC(recByHash, reqByHash)
	if recByHash.Code != 200 {
		t.Fatalf("debug_traceBlockByHash status=%d body=%s", recByHash.Code, recByHash.Body.String())
	}
	if !strings.Contains(strings.ToLower(recByHash.Body.String()), `"result":[`) {
		t.Fatalf("debug_traceBlockByHash unexpected body: %s", recByHash.Body.String())
	}
}

func TestJSONRPCCompatDebugTraceCallAndReplayTransaction(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	tx := Transaction{
		ID:          "0x" + strings.Repeat("7", 64),
		EVMTxHash:   "0x" + strings.Repeat("8", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "0x000000000000000000000000000000000000c0de",
		Amount:      0,
		Nonce:       1,
		Fee:         ComputeEVMFee(DefaultEVMGasLimit),
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}
	block := Block{
		ID:           9,
		BlockHash:    "0x" + strings.Repeat("9", 64),
		PrevHash:     "0x" + strings.Repeat("0", 64),
		Timestamp:    1,
		Type:         BlockTypeTime,
		Proposer:     "A",
		Transactions: []Transaction{tx},
	}
	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 0, BlockHash: "0x" + strings.Repeat("0", 64), Timestamp: 0, Type: BlockTypeGenesis},
					block,
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
				EVMCode: map[string]string{
					normalizeEVMAddressKey(tx.To): "0x60006000f3",
				},
			},
		},
	}

	reqTraceCall := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"debug_traceCall","params":[{"from":"0x0000000000000000000000000000000000001111","to":"0x000000000000000000000000000000000000c0de","data":"0x"},"latest",{}]}`))
	recTraceCall := httptest.NewRecorder()
	s.handleJSONRPC(recTraceCall, reqTraceCall)
	if recTraceCall.Code != 200 {
		t.Fatalf("debug_traceCall status=%d body=%s", recTraceCall.Code, recTraceCall.Body.String())
	}
	bodyTraceCall := strings.ToLower(recTraceCall.Body.String())
	if !strings.Contains(bodyTraceCall, `"result"`) || !strings.Contains(bodyTraceCall, `"gasused"`) {
		t.Fatalf("debug_traceCall unexpected body: %s", recTraceCall.Body.String())
	}

	reqReplay := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"trace_replayTransaction","params":["`+tx.EVMTxHash+`",["trace","vmTrace","stateDiff"]]}`))
	recReplay := httptest.NewRecorder()
	s.handleJSONRPC(recReplay, reqReplay)
	if recReplay.Code != 200 {
		t.Fatalf("trace_replayTransaction status=%d body=%s", recReplay.Code, recReplay.Body.String())
	}
	bodyReplay := strings.ToLower(recReplay.Body.String())
	if !strings.Contains(bodyReplay, `"trace"`) || !strings.Contains(bodyReplay, `"vmtrace"`) || !strings.Contains(bodyReplay, `"statediff"`) {
		t.Fatalf("trace_replayTransaction unexpected body: %s", recReplay.Body.String())
	}
}

func TestJSONRPCCompatTraceTransactionCallAndBlock(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	tx := Transaction{
		ID:          "0x" + strings.Repeat("a", 64),
		EVMTxHash:   "0x" + strings.Repeat("b", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "0x000000000000000000000000000000000000c0de",
		Amount:      0,
		Nonce:       1,
		Fee:         ComputeEVMFee(DefaultEVMGasLimit),
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}
	block := Block{
		ID:           11,
		BlockHash:    "0x" + strings.Repeat("c", 64),
		PrevHash:     "0x" + strings.Repeat("d", 64),
		Timestamp:    1,
		Type:         BlockTypeTime,
		Proposer:     "A",
		Transactions: []Transaction{tx},
	}
	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 0, BlockHash: "0x" + strings.Repeat("0", 64), Timestamp: 0, Type: BlockTypeGenesis},
					block,
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
				EVMCode: map[string]string{
					normalizeEVMAddressKey(tx.To): "0x60006000f3",
				},
			},
		},
	}

	reqTraceTx := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"trace_transaction","params":["`+tx.EVMTxHash+`"]}`))
	recTraceTx := httptest.NewRecorder()
	s.handleJSONRPC(recTraceTx, reqTraceTx)
	if recTraceTx.Code != 200 {
		t.Fatalf("trace_transaction status=%d body=%s", recTraceTx.Code, recTraceTx.Body.String())
	}
	bodyTraceTx := strings.ToLower(recTraceTx.Body.String())
	if !strings.Contains(bodyTraceTx, `"result":[`) || !strings.Contains(bodyTraceTx, `"transactionhash":"`+strings.ToLower(tx.EVMTxHash)+`"`) {
		t.Fatalf("trace_transaction unexpected body: %s", recTraceTx.Body.String())
	}

	reqTraceCall := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"trace_call","params":[{"from":"0x0000000000000000000000000000000000001111","to":"0x000000000000000000000000000000000000c0de","data":"0x"},["trace","vmTrace","stateDiff"],"latest"]}`))
	recTraceCall := httptest.NewRecorder()
	s.handleJSONRPC(recTraceCall, reqTraceCall)
	if recTraceCall.Code != 200 {
		t.Fatalf("trace_call status=%d body=%s", recTraceCall.Code, recTraceCall.Body.String())
	}
	bodyTraceCall := strings.ToLower(recTraceCall.Body.String())
	if !strings.Contains(bodyTraceCall, `"trace"`) || !strings.Contains(bodyTraceCall, `"vmtrace"`) || !strings.Contains(bodyTraceCall, `"statediff"`) {
		t.Fatalf("trace_call unexpected body: %s", recTraceCall.Body.String())
	}

	reqTraceBlock := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"trace_block","params":["0xb"]}`))
	recTraceBlock := httptest.NewRecorder()
	s.handleJSONRPC(recTraceBlock, reqTraceBlock)
	if recTraceBlock.Code != 200 {
		t.Fatalf("trace_block status=%d body=%s", recTraceBlock.Code, recTraceBlock.Body.String())
	}
	bodyTraceBlock := strings.ToLower(recTraceBlock.Body.String())
	if !strings.Contains(bodyTraceBlock, `"result":[`) || !strings.Contains(bodyTraceBlock, `"blocknumber":"0xb"`) {
		t.Fatalf("trace_block unexpected body: %s", recTraceBlock.Body.String())
	}
}

func TestJSONRPCCompatTraceFilterAndTraceGet(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	fromA := "0x00000000000000000000000000000000000000a1"
	fromB := "0x00000000000000000000000000000000000000b2"
	toA := "0x000000000000000000000000000000000000c0de"

	tx1 := Transaction{
		ID:          "0x" + strings.Repeat("1", 64),
		EVMTxHash:   "0x" + strings.Repeat("2", 64),
		From:        fromA,
		To:          toA,
		Amount:      0,
		Nonce:       1,
		Fee:         ComputeEVMFee(DefaultEVMGasLimit),
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}
	tx2 := Transaction{
		ID:          "0x" + strings.Repeat("3", 64),
		EVMTxHash:   "0x" + strings.Repeat("4", 64),
		From:        fromB,
		To:          toA,
		Amount:      0,
		Nonce:       2,
		Fee:         ComputeEVMFee(DefaultEVMGasLimit),
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}

	block1 := Block{
		ID:           0x11,
		BlockHash:    "0x" + strings.Repeat("a", 64),
		PrevHash:     "0x" + strings.Repeat("0", 64),
		Timestamp:    1,
		Type:         BlockTypeTime,
		Proposer:     "A",
		Transactions: []Transaction{tx1},
	}
	block2 := Block{
		ID:           0x12,
		BlockHash:    "0x" + strings.Repeat("b", 64),
		PrevHash:     block1.BlockHash,
		Timestamp:    2,
		Type:         BlockTypeTime,
		Proposer:     "B",
		Transactions: []Transaction{tx2},
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 0, BlockHash: "0x" + strings.Repeat("0", 64), Timestamp: 0, Type: BlockTypeGenesis},
					block1,
					block2,
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	reqFilter := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"trace_filter","params":[{"fromBlock":"0x11","toBlock":"0x12","fromAddress":["`+fromA+`"],"after":"0x0","count":"0x10"}]}`))
	recFilter := httptest.NewRecorder()
	s.handleJSONRPC(recFilter, reqFilter)
	if recFilter.Code != 200 {
		t.Fatalf("trace_filter status=%d body=%s", recFilter.Code, recFilter.Body.String())
	}
	bodyFilter := strings.ToLower(recFilter.Body.String())
	if !strings.Contains(bodyFilter, `"result":[`) || !strings.Contains(bodyFilter, strings.ToLower(tx1.EVMTxHash)) || strings.Contains(bodyFilter, strings.ToLower(tx2.EVMTxHash)) {
		t.Fatalf("trace_filter unexpected body: %s", recFilter.Body.String())
	}

	reqGetRoot := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"trace_get","params":["`+tx1.EVMTxHash+`",[]]}`))
	recGetRoot := httptest.NewRecorder()
	s.handleJSONRPC(recGetRoot, reqGetRoot)
	if recGetRoot.Code != 200 {
		t.Fatalf("trace_get root status=%d body=%s", recGetRoot.Code, recGetRoot.Body.String())
	}
	bodyGetRoot := strings.ToLower(recGetRoot.Body.String())
	if !strings.Contains(bodyGetRoot, `"transactionhash":"`+strings.ToLower(tx1.EVMTxHash)+`"`) {
		t.Fatalf("trace_get root unexpected body: %s", recGetRoot.Body.String())
	}

	reqGetSub := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"trace_get","params":["`+tx1.EVMTxHash+`",[0]]}`))
	recGetSub := httptest.NewRecorder()
	s.handleJSONRPC(recGetSub, reqGetSub)
	if recGetSub.Code != 200 {
		t.Fatalf("trace_get subtrace status=%d body=%s", recGetSub.Code, recGetSub.Body.String())
	}
	var subResp jsonRPCResponse
	if err := json.Unmarshal(recGetSub.Body.Bytes(), &subResp); err != nil {
		t.Fatalf("trace_get subtrace decode response: %v", err)
	}
	if subResp.Error != nil {
		t.Fatalf("trace_get subtrace returned error: %+v", subResp.Error)
	}
	if subResp.Result != nil {
		t.Fatalf("trace_get subtrace expected nil result, got: %#v", subResp.Result)
	}
}

func TestJSONRPCCompatSendRawTransaction(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	prevSubmitAuth := ConfigRPCRequireAuthForSubmitEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	ConfigRPCRequireAuthForSubmitEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
		ConfigRPCRequireAuthForSubmitEndpoints = prevSubmitAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	chainID := chainIDBigInt()
	to := common.HexToAddress("0x000000000000000000000000000000000000dEaD")
	unsigned := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(0),
		Gas:      500000,
		GasPrice: big.NewInt(1),
		Data:     []byte{0x60, 0x00, 0x60, 0x00},
	})
	signed, err := ethtypes.SignTx(unsigned, ethtypes.LatestSignerForChainID(chainID), key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	rawBytes, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal raw tx: %v", err)
	}
	rawHex := "0x" + hex.EncodeToString(rawBytes)

	sender := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()
	ledger := Ledger{
		Balances: map[string]int{},
		Nonces:   map[string]int{},
		Stakes:   map[string]StakeLock{},
		EVMState: map[string]string{},
	}
	addBalance(&ledger, CoinSymbol, sender, 1_000_000)

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 1, BlockHash: "abcd", Timestamp: 1},
				},
			},
			Ledger: ledger,
			Mempool: Mempool{
				SeenTxIDs: map[string]bool{},
			},
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_sendRawTransaction","params":["` + rawHex + `"]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_sendRawTransaction status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_sendRawTransaction error: %+v", resp.Error)
	}
	resultHash, ok := resp.Result.(string)
	if !ok || strings.TrimSpace(resultHash) == "" {
		t.Fatalf("missing tx hash result: %#v", resp.Result)
	}
	if !strings.EqualFold(resultHash, signed.Hash().Hex()) {
		t.Fatalf("unexpected tx hash result: got=%s want=%s", resultHash, signed.Hash().Hex())
	}

	loc := s.findTxByHash(resultHash)
	if !loc.Found || !loc.Pending {
		t.Fatalf("submitted raw tx not found in mempool by eth hash")
	}
	if loc.Tx.Type != TxEVM {
		t.Fatalf("expected TxEVM, got %d", loc.Tx.Type)
	}
}

func TestJSONRPCCompatSendRawTransactionUsesCalldataForDeployedContract(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	prevSubmitAuth := ConfigRPCRequireAuthForSubmitEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	ConfigRPCRequireAuthForSubmitEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
		ConfigRPCRequireAuthForSubmitEndpoints = prevSubmitAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	chainID := chainIDBigInt()
	to := common.HexToAddress("0x000000000000000000000000000000000000c0de")
	unsigned := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    0,
		To:       &to,
		Value:    big.NewInt(0),
		Gas:      500000,
		GasPrice: big.NewInt(1),
		Data:     []byte{0xab, 0xcd, 0xef},
	})
	signed, err := ethtypes.SignTx(unsigned, ethtypes.LatestSignerForChainID(chainID), key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	rawBytes, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal raw tx: %v", err)
	}
	rawHex := "0x" + hex.EncodeToString(rawBytes)

	sender := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()
	ledger := Ledger{
		Balances: map[string]int{},
		Nonces:   map[string]int{},
		Stakes:   map[string]StakeLock{},
		EVMState: map[string]string{},
		EVMCode: map[string]string{
			normalizeEVMAddressKey(to.Hex()): "0x60006000f3",
		},
		EVMStorage: map[string]map[string]string{},
	}
	addBalance(&ledger, CoinSymbol, sender, 1_000_000)

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 1, BlockHash: "abcd", Timestamp: 1},
				},
			},
			Ledger: ledger,
			Mempool: Mempool{
				SeenTxIDs: map[string]bool{},
			},
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_sendRawTransaction","params":["` + rawHex + `"]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_sendRawTransaction status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_sendRawTransaction error: %+v", resp.Error)
	}

	loc := s.findTxByHash(signed.Hash().Hex())
	if !loc.Found || !loc.Pending {
		t.Fatalf("submitted raw tx not found in mempool")
	}
	if normalizeHexData(loc.Tx.EVMCode) != "0x60006000f3" {
		t.Fatalf("expected hydrated deployed evm_code, got=%q", loc.Tx.EVMCode)
	}
	if got := normalizeHexData(loc.Tx.EVMInput); got != "0xabcdef" {
		t.Fatalf("unexpected evm_input: %s", got)
	}
}

func TestJSONRPCCompatTypedRawTxFieldsInTxObjectAndReceipt(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	chainID := chainIDBigInt()
	to := common.HexToAddress("0x000000000000000000000000000000000000c0de")
	tipCap := big.NewInt(2_000_000_000)
	feeCap := big.NewInt(30_000_000_000)
	unsigned := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     7,
		GasTipCap: tipCap,
		GasFeeCap: feeCap,
		Gas:       210000,
		To:        &to,
		Value:     big.NewInt(123),
		Data:      []byte{0xab, 0xcd},
	})
	signed, err := ethtypes.SignTx(unsigned, ethtypes.LatestSignerForChainID(chainID), key)
	if err != nil {
		t.Fatalf("sign tx: %v", err)
	}
	rawBytes, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal raw tx: %v", err)
	}
	rawHex := "0x" + hex.EncodeToString(rawBytes)

	sender := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()
	tx := Transaction{
		ID:          "0x" + strings.Repeat("e", 64),
		EVMTxHash:   signed.Hash().Hex(),
		EVMRawTx:    rawHex,
		From:        sender,
		To:          to.Hex(),
		Amount:      123,
		Nonce:       8,
		Fee:         ComputeEVMFee(210000),
		Type:        TxEVM,
		EVMCode:     "0x00",
		EVMInput:    "0xabcd",
		EVMGasLimit: 210000,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}
	block := Block{
		ID:           3,
		BlockHash:    "0x" + strings.Repeat("f", 64),
		PrevHash:     "0x" + strings.Repeat("a", 64),
		Timestamp:    1,
		Type:         BlockTypeTime,
		Proposer:     "A",
		Transactions: []Transaction{tx},
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 0, BlockHash: "0x" + strings.Repeat("0", 64), Timestamp: 0, Type: BlockTypeGenesis},
					block,
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	reqTx := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionByHash","params":["`+tx.EVMTxHash+`"]}`))
	recTx := httptest.NewRecorder()
	s.handleJSONRPC(recTx, reqTx)
	if recTx.Code != 200 {
		t.Fatalf("eth_getTransactionByHash status=%d body=%s", recTx.Code, recTx.Body.String())
	}
	bodyTx := strings.ToLower(recTx.Body.String())
	if !strings.Contains(bodyTx, `"type":"0x2"`) ||
		!strings.Contains(bodyTx, `"maxfeepergas":"`) ||
		!strings.Contains(bodyTx, `"maxpriorityfeepergas":"`) ||
		!strings.Contains(bodyTx, `"v":"0x`) {
		t.Fatalf("eth_getTransactionByHash typed/raw fields missing: %s", recTx.Body.String())
	}

	reqReceipt := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_getTransactionReceipt","params":["`+tx.EVMTxHash+`"]}`))
	recReceipt := httptest.NewRecorder()
	s.handleJSONRPC(recReceipt, reqReceipt)
	if recReceipt.Code != 200 {
		t.Fatalf("eth_getTransactionReceipt status=%d body=%s", recReceipt.Code, recReceipt.Body.String())
	}
	bodyReceipt := strings.ToLower(recReceipt.Body.String())
	if !strings.Contains(bodyReceipt, `"type":"0x2"`) || !strings.Contains(bodyReceipt, `"effectivegasprice":"0x`) {
		t.Fatalf("eth_getTransactionReceipt typed fields missing: %s", recReceipt.Body.String())
	}
}

func TestJSONRPCCompatGetRawTransactionMethods(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	key, err := ethcrypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	chainID := chainIDBigInt()
	to := common.HexToAddress("0x000000000000000000000000000000000000c0de")

	mkSigned := func(nonce uint64, value int64, data []byte) (*ethtypes.Transaction, string) {
		t.Helper()
		unsigned := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     nonce,
			GasTipCap: big.NewInt(2_000_000_000),
			GasFeeCap: big.NewInt(30_000_000_000),
			Gas:       210000,
			To:        &to,
			Value:     big.NewInt(value),
			Data:      data,
		})
		signed, err := ethtypes.SignTx(unsigned, ethtypes.LatestSignerForChainID(chainID), key)
		if err != nil {
			t.Fatalf("sign tx: %v", err)
		}
		rawBytes, err := signed.MarshalBinary()
		if err != nil {
			t.Fatalf("marshal raw tx: %v", err)
		}
		return signed, "0x" + hex.EncodeToString(rawBytes)
	}

	blockSigned, blockRaw := mkSigned(7, 123, []byte{0xab, 0xcd})
	pendingSigned, pendingRaw := mkSigned(8, 456, []byte{0x12, 0x34})
	sender := ethcrypto.PubkeyToAddress(key.PublicKey).Hex()

	blockTx := Transaction{
		ID:          "0x" + strings.Repeat("e", 64),
		EVMTxHash:   blockSigned.Hash().Hex(),
		EVMRawTx:    blockRaw,
		From:        sender,
		To:          to.Hex(),
		Amount:      123,
		Nonce:       8,
		Fee:         ComputeEVMFee(210000),
		Type:        TxEVM,
		EVMCode:     "0x00",
		EVMInput:    "0xabcd",
		EVMGasLimit: 210000,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}
	pendingTx := Transaction{
		ID:          "0x" + strings.Repeat("d", 64),
		EVMTxHash:   pendingSigned.Hash().Hex(),
		EVMRawTx:    pendingRaw,
		From:        sender,
		To:          to.Hex(),
		Amount:      456,
		Nonce:       9,
		Fee:         ComputeEVMFee(210000),
		Type:        TxEVM,
		EVMCode:     "0x00",
		EVMInput:    "0x1234",
		EVMGasLimit: 210000,
		Coin:        CoinSymbol,
		ChainID:     ChainID,
	}

	block := Block{
		ID:           5,
		BlockHash:    "0x" + strings.Repeat("f", 64),
		PrevHash:     "0x" + strings.Repeat("a", 64),
		Timestamp:    1,
		Type:         BlockTypeTime,
		Proposer:     "A",
		Transactions: []Transaction{blockTx},
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 0, BlockHash: "0x" + strings.Repeat("0", 64), Timestamp: 0, Type: BlockTypeGenesis},
					block,
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
			Mempool: Mempool{
				Transactions: []Transaction{pendingTx},
				SeenTxIDs: map[string]bool{
					strings.ToLower(strings.TrimSpace(pendingTx.ID)): true,
				},
			},
		},
	}

	assertRaw := func(body, wantRaw string) {
		t.Helper()
		var resp jsonRPCResponse
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Error != nil {
			t.Fatalf("rpc error: %+v body=%s", resp.Error, body)
		}
		got, ok := resp.Result.(string)
		if !ok {
			t.Fatalf("expected raw tx string, got=%#v", resp.Result)
		}
		if !strings.EqualFold(got, wantRaw) {
			t.Fatalf("raw tx mismatch: got=%s want=%s", got, wantRaw)
		}
	}

	reqByHash := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getRawTransactionByHash","params":["`+blockTx.EVMTxHash+`"]}`))
	recByHash := httptest.NewRecorder()
	s.handleJSONRPC(recByHash, reqByHash)
	if recByHash.Code != 200 {
		t.Fatalf("eth_getRawTransactionByHash status=%d body=%s", recByHash.Code, recByHash.Body.String())
	}
	assertRaw(recByHash.Body.String(), blockRaw)

	reqByBlockHash := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_getRawTransactionByBlockHashAndIndex","params":["`+block.BlockHash+`","0x0"]}`))
	recByBlockHash := httptest.NewRecorder()
	s.handleJSONRPC(recByBlockHash, reqByBlockHash)
	if recByBlockHash.Code != 200 {
		t.Fatalf("eth_getRawTransactionByBlockHashAndIndex status=%d body=%s", recByBlockHash.Code, recByBlockHash.Body.String())
	}
	assertRaw(recByBlockHash.Body.String(), blockRaw)

	reqByBlockNum := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"eth_getRawTransactionByBlockNumberAndIndex","params":["0x5","0x0"]}`))
	recByBlockNum := httptest.NewRecorder()
	s.handleJSONRPC(recByBlockNum, reqByBlockNum)
	if recByBlockNum.Code != 200 {
		t.Fatalf("eth_getRawTransactionByBlockNumberAndIndex status=%d body=%s", recByBlockNum.Code, recByBlockNum.Body.String())
	}
	assertRaw(recByBlockNum.Body.String(), blockRaw)

	reqPending := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"eth_getRawTransactionByBlockNumberAndIndex","params":["pending","0x0"]}`))
	recPending := httptest.NewRecorder()
	s.handleJSONRPC(recPending, reqPending)
	if recPending.Code != 200 {
		t.Fatalf("eth_getRawTransactionByBlockNumberAndIndex pending status=%d body=%s", recPending.Code, recPending.Body.String())
	}
	assertRaw(recPending.Body.String(), pendingRaw)
}

func TestJSONRPCCompatGetCodeAndStorage(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	contract := strings.ToLower(common.HexToAddress("0x000000000000000000000000000000000000c0De").Hex())
	code := "0x6001600055"
	slot := "0x1"
	value := "0x2a"

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 1, BlockHash: "abcd", Timestamp: 1},
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
				EVMState: map[string]string{},
				EVMCode: map[string]string{
					contract: code,
				},
				EVMStorage: map[string]map[string]string{
					contract: {
						normalizeEVMStorageSlotKey(slot): normalizeEVMStorageValue(value),
					},
				},
			},
		},
	}

	codeReq := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getCode","params":["`+contract+`","latest"]}`))
	codeRec := httptest.NewRecorder()
	s.handleJSONRPC(codeRec, codeReq)
	if codeRec.Code != 200 {
		t.Fatalf("eth_getCode status=%d body=%s", codeRec.Code, codeRec.Body.String())
	}
	if !strings.Contains(strings.ToLower(codeRec.Body.String()), `"result":"`+strings.ToLower(code)+`"`) {
		t.Fatalf("eth_getCode unexpected body: %s", codeRec.Body.String())
	}

	storageReq := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_getStorageAt","params":["`+contract+`","`+slot+`","latest"]}`))
	storageRec := httptest.NewRecorder()
	s.handleJSONRPC(storageRec, storageReq)
	if storageRec.Code != 200 {
		t.Fatalf("eth_getStorageAt status=%d body=%s", storageRec.Code, storageRec.Body.String())
	}
	want := normalizeEVMStorageValue(value)
	if !strings.Contains(strings.ToLower(storageRec.Body.String()), `"result":"`+strings.ToLower(want)+`"`) {
		t.Fatalf("eth_getStorageAt unexpected body: %s", storageRec.Body.String())
	}
}

func TestJSONRPCCompatReceiptIncludesContractAddressAndLogs(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	tx := Transaction{
		ID:          strings.Repeat("a", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "",
		Amount:      0,
		Nonce:       1,
		Fee:         ComputeEVMFee(DefaultEVMGasLimit),
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMTxHash:   "0x" + strings.Repeat("b", 64),
		EVMGasLimit: DefaultEVMGasLimit,
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{
						ID:           1,
						BlockHash:    "0x" + strings.Repeat("c", 64),
						Timestamp:    1,
						Transactions: []Transaction{tx},
					},
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["` + tx.EVMTxHash + `"]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_getTransactionReceipt status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("eth_getTransactionReceipt error: %+v", resp.Error)
	}
	data, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected receipt result type: %#v", resp.Result)
	}
	if contractAddress, ok := data["contractAddress"].(string); !ok || strings.TrimSpace(contractAddress) == "" {
		t.Fatalf("expected contractAddress in receipt, got %#v", data["contractAddress"])
	}
	logs, ok := data["logs"].([]any)
	if !ok || len(logs) == 0 {
		t.Fatalf("expected non-empty logs, got %#v", data["logs"])
	}
	if bloom, ok := data["logsBloom"].(string); !ok || !strings.HasPrefix(strings.ToLower(bloom), "0x") || len(bloom) <= 2 {
		t.Fatalf("expected logsBloom, got %#v", data["logsBloom"])
	}
}

func TestJSONRPCCompatGetTransactionByBlockIndex(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	block := Block{
		ID:        9,
		BlockHash: "0x" + strings.Repeat("a", 64),
		Timestamp: 1,
		Transactions: []Transaction{
			{
				ID:     "0x" + strings.Repeat("1", 64),
				From:   "0x0000000000000000000000000000000000001111",
				To:     "0x0000000000000000000000000000000000002222",
				Amount: 1,
				Type:   TxTransfer,
			},
			{
				ID:          "0x" + strings.Repeat("2", 64),
				EVMTxHash:   "0x" + strings.Repeat("b", 64),
				From:        "0x0000000000000000000000000000000000003333",
				To:          "0x0000000000000000000000000000000000004444",
				Type:        TxEVM,
				EVMCode:     "0x60006000f3",
				EVMInput:    "0x",
				EVMGasLimit: DefaultEVMGasLimit,
			},
		},
	}
	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{Blocks: []Block{block}},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	reqHash := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionByBlockHashAndIndex","params":["`+block.BlockHash+`","0x1"]}`))
	recHash := httptest.NewRecorder()
	s.handleJSONRPC(recHash, reqHash)
	if recHash.Code != 200 {
		t.Fatalf("eth_getTransactionByBlockHashAndIndex status=%d body=%s", recHash.Code, recHash.Body.String())
	}
	var respHash jsonRPCResponse
	if err := json.Unmarshal(recHash.Body.Bytes(), &respHash); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if respHash.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", respHash.Error)
	}
	objByHash, ok := respHash.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result: %#v", respHash.Result)
	}
	if got := strings.ToLower(objByHash["hash"].(string)); got != strings.ToLower(block.Transactions[1].EVMTxHash) {
		t.Fatalf("unexpected tx hash by block hash/index: got=%s want=%s", got, block.Transactions[1].EVMTxHash)
	}

	reqNum := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_getTransactionByBlockNumberAndIndex","params":["0x9","0x0"]}`))
	recNum := httptest.NewRecorder()
	s.handleJSONRPC(recNum, reqNum)
	if recNum.Code != 200 {
		t.Fatalf("eth_getTransactionByBlockNumberAndIndex status=%d body=%s", recNum.Code, recNum.Body.String())
	}
	var respNum jsonRPCResponse
	if err := json.Unmarshal(recNum.Body.Bytes(), &respNum); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if respNum.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", respNum.Error)
	}
	objByNum, ok := respNum.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result: %#v", respNum.Result)
	}
	if got := strings.ToLower(objByNum["hash"].(string)); got != strings.ToLower(block.Transactions[0].ID) {
		t.Fatalf("unexpected tx hash by block number/index: got=%s want=%s", got, block.Transactions[0].ID)
	}
}

func TestJSONRPCCompatPendingBlockAndTransactions(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	latest := Block{
		ID:        5,
		BlockHash: "0x" + strings.Repeat("a", 64),
		Timestamp: 1,
	}
	tx1 := Transaction{
		ID:          "0x" + strings.Repeat("1", 64),
		EVMTxHash:   "0x" + strings.Repeat("2", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "0x0000000000000000000000000000000000002222",
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
	}
	tx2 := Transaction{
		ID:          "0x" + strings.Repeat("3", 64),
		EVMTxHash:   "0x" + strings.Repeat("4", 64),
		From:        "0x0000000000000000000000000000000000003333",
		To:          "0x0000000000000000000000000000000000004444",
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
	}

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{Blocks: []Block{latest}},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
			Mempool: Mempool{
				Transactions: []Transaction{tx1, tx2},
				SeenTxIDs:    map[string]bool{},
			},
		},
	}

	reqPendingBlock := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_getBlockByNumber","params":["pending",false]}`))
	recPendingBlock := httptest.NewRecorder()
	s.handleJSONRPC(recPendingBlock, reqPendingBlock)
	if recPendingBlock.Code != 200 {
		t.Fatalf("eth_getBlockByNumber pending status=%d body=%s", recPendingBlock.Code, recPendingBlock.Body.String())
	}
	var respPendingBlock jsonRPCResponse
	if err := json.Unmarshal(recPendingBlock.Body.Bytes(), &respPendingBlock); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if respPendingBlock.Error != nil {
		t.Fatalf("rpc error: %+v", respPendingBlock.Error)
	}
	blockObj, ok := respPendingBlock.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected block result: %#v", respPendingBlock.Result)
	}
	if blockObj["hash"] != nil {
		t.Fatalf("expected pending block hash=nil, got %#v", blockObj["hash"])
	}
	if got, _ := blockObj["number"].(string); strings.ToLower(got) != "0x6" {
		t.Fatalf("unexpected pending block number: %s", got)
	}
	if got, _ := blockObj["parentHash"].(string); !strings.EqualFold(got, normalizeHexHash(latest.BlockHash)) {
		t.Fatalf("unexpected pending parent hash: %s", got)
	}
	if txs, ok := blockObj["transactions"].([]any); !ok || len(txs) != 2 {
		t.Fatalf("unexpected pending block tx list: %#v", blockObj["transactions"])
	}

	reqPendingBlockFull := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_getBlockByNumber","params":["pending",true]}`))
	recPendingBlockFull := httptest.NewRecorder()
	s.handleJSONRPC(recPendingBlockFull, reqPendingBlockFull)
	if recPendingBlockFull.Code != 200 {
		t.Fatalf("eth_getBlockByNumber pending full status=%d body=%s", recPendingBlockFull.Code, recPendingBlockFull.Body.String())
	}
	var respPendingBlockFull jsonRPCResponse
	if err := json.Unmarshal(recPendingBlockFull.Body.Bytes(), &respPendingBlockFull); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	fullObj, ok := respPendingBlockFull.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected pending full block result: %#v", respPendingBlockFull.Result)
	}
	fullTxs, ok := fullObj["transactions"].([]any)
	if !ok || len(fullTxs) != 2 {
		t.Fatalf("unexpected pending full tx list: %#v", fullObj["transactions"])
	}
	firstTx, ok := fullTxs[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first pending tx object: %#v", fullTxs[0])
	}
	if firstTx["blockHash"] != nil {
		t.Fatalf("expected pending tx blockHash=nil, got %#v", firstTx["blockHash"])
	}

	reqPendingCount := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"eth_getBlockTransactionCountByNumber","params":["pending"]}`))
	recPendingCount := httptest.NewRecorder()
	s.handleJSONRPC(recPendingCount, reqPendingCount)
	if recPendingCount.Code != 200 {
		t.Fatalf("eth_getBlockTransactionCountByNumber pending status=%d body=%s", recPendingCount.Code, recPendingCount.Body.String())
	}
	if !strings.Contains(strings.ToLower(recPendingCount.Body.String()), `"result":"0x2"`) {
		t.Fatalf("unexpected pending tx count body: %s", recPendingCount.Body.String())
	}

	reqPendingByIdx := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"eth_getTransactionByBlockNumberAndIndex","params":["pending","0x1"]}`))
	recPendingByIdx := httptest.NewRecorder()
	s.handleJSONRPC(recPendingByIdx, reqPendingByIdx)
	if recPendingByIdx.Code != 200 {
		t.Fatalf("eth_getTransactionByBlockNumberAndIndex pending status=%d body=%s", recPendingByIdx.Code, recPendingByIdx.Body.String())
	}
	var respPendingByIdx jsonRPCResponse
	if err := json.Unmarshal(recPendingByIdx.Body.Bytes(), &respPendingByIdx); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	objByIdx, ok := respPendingByIdx.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected pending tx by index result: %#v", respPendingByIdx.Result)
	}
	if got, _ := objByIdx["hash"].(string); !strings.EqualFold(got, tx2.EVMTxHash) {
		t.Fatalf("unexpected pending tx by index hash: got=%s want=%s", got, tx2.EVMTxHash)
	}
	if objByIdx["blockHash"] != nil {
		t.Fatalf("expected pending tx by index blockHash=nil, got %#v", objByIdx["blockHash"])
	}

	reqPendingList := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":5,"method":"eth_pendingTransactions","params":[]}`))
	recPendingList := httptest.NewRecorder()
	s.handleJSONRPC(recPendingList, reqPendingList)
	if recPendingList.Code != 200 {
		t.Fatalf("eth_pendingTransactions status=%d body=%s", recPendingList.Code, recPendingList.Body.String())
	}
	var respPendingList jsonRPCResponse
	if err := json.Unmarshal(recPendingList.Body.Bytes(), &respPendingList); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	pendingList, ok := respPendingList.Result.([]any)
	if !ok || len(pendingList) != 2 {
		t.Fatalf("unexpected eth_pendingTransactions result: %#v", respPendingList.Result)
	}

	reqPendingReceipts := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":6,"method":"eth_getBlockReceipts","params":["pending"]}`))
	recPendingReceipts := httptest.NewRecorder()
	s.handleJSONRPC(recPendingReceipts, reqPendingReceipts)
	if recPendingReceipts.Code != 200 {
		t.Fatalf("eth_getBlockReceipts pending status=%d body=%s", recPendingReceipts.Code, recPendingReceipts.Body.String())
	}
	if !strings.Contains(strings.ToLower(recPendingReceipts.Body.String()), `"result":[]`) {
		t.Fatalf("unexpected pending receipts body: %s", recPendingReceipts.Body.String())
	}
}

func TestJSONRPCCompatGetLogsFiltersByAddressAndTopics(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	tx1 := Transaction{
		ID:          "0x" + strings.Repeat("3", 64),
		EVMTxHash:   "0x" + strings.Repeat("c", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "0x000000000000000000000000000000000000c0de",
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x" + strings.Repeat("0", 63) + "1" + strings.Repeat("0", 63) + "2",
		EVMGasLimit: DefaultEVMGasLimit,
	}
	tx2 := Transaction{
		ID:          "0x" + strings.Repeat("4", 64),
		EVMTxHash:   "0x" + strings.Repeat("d", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "0x000000000000000000000000000000000000beef",
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
	}
	block := Block{
		ID:           7,
		BlockHash:    "0x" + strings.Repeat("e", 64),
		Timestamp:    1,
		Transactions: []Transaction{tx1, tx2},
	}
	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{Blocks: []Block{block}},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	wantAddress := strings.ToLower(common.HexToAddress(tx1.To).Hex())
	wantTopic := evmExecTopic0().Hex()
	body := `{"jsonrpc":"2.0","id":1,"method":"eth_getLogs","params":[{"fromBlock":"0x7","toBlock":"0x7","address":"` + wantAddress + `","topics":["` + wantTopic + `"]}]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_getLogs status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resp.Error)
	}
	logs, ok := resp.Result.([]any)
	if !ok {
		t.Fatalf("unexpected logs result: %#v", resp.Result)
	}
	if len(logs) != 1 {
		t.Fatalf("expected exactly one filtered log, got=%d %#v", len(logs), logs)
	}
	first, ok := logs[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected log shape: %#v", logs[0])
	}
	if got := strings.ToLower(first["address"].(string)); got != wantAddress {
		t.Fatalf("unexpected log address: got=%s want=%s", got, wantAddress)
	}
	if got := strings.ToLower(first["transactionHash"].(string)); got != strings.ToLower(tx1.EVMTxHash) {
		t.Fatalf("unexpected transactionHash: got=%s want=%s", got, tx1.EVMTxHash)
	}
}

func TestJSONRPCCompatReceiptLogIndexSequentialInBlock(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	tx1 := Transaction{
		ID:          "0x" + strings.Repeat("5", 64),
		EVMTxHash:   "0x" + strings.Repeat("a", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "0x000000000000000000000000000000000000c0de",
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
	}
	tx2 := Transaction{
		ID:          "0x" + strings.Repeat("6", 64),
		EVMTxHash:   "0x" + strings.Repeat("b", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "0x000000000000000000000000000000000000c0de",
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
	}
	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{
						ID:           11,
						BlockHash:    "0x" + strings.Repeat("c", 64),
						Timestamp:    1,
						Transactions: []Transaction{tx1, tx2},
					},
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_getTransactionReceipt","params":["` + tx2.EVMTxHash + `"]}`
	req := httptest.NewRequest("POST", "/rpc", strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.handleJSONRPC(rec, req)
	if rec.Code != 200 {
		t.Fatalf("eth_getTransactionReceipt status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp jsonRPCResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resp.Error)
	}
	receipt, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected receipt result type: %#v", resp.Result)
	}
	logs, ok := receipt["logs"].([]any)
	if !ok || len(logs) == 0 {
		t.Fatalf("expected receipt logs, got %#v", receipt["logs"])
	}
	firstLog, ok := logs[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected log shape: %#v", logs[0])
	}
	if got := strings.ToLower(firstLog["logIndex"].(string)); got != "0x1" {
		t.Fatalf("expected sequential block logIndex for second tx, got=%s", got)
	}
}

func TestJSONRPCCompatFilterLifecycle(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	tx := Transaction{
		ID:          "0x" + strings.Repeat("7", 64),
		EVMTxHash:   "0x" + strings.Repeat("e", 64),
		From:        "0x0000000000000000000000000000000000001111",
		To:          "0x000000000000000000000000000000000000c0de",
		Type:        TxEVM,
		EVMCode:     "0x60006000f3",
		EVMInput:    "0x",
		EVMGasLimit: DefaultEVMGasLimit,
	}
	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{
						ID:           12,
						BlockHash:    "0x" + strings.Repeat("f", 64),
						Timestamp:    1,
						Transactions: []Transaction{tx},
					},
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	newFilterReq := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"eth_newFilter","params":[{"fromBlock":"0x0","toBlock":"latest","address":"`+strings.ToLower(tx.To)+`"}]}`))
	newFilterRec := httptest.NewRecorder()
	s.handleJSONRPC(newFilterRec, newFilterReq)
	if newFilterRec.Code != 200 {
		t.Fatalf("eth_newFilter status=%d body=%s", newFilterRec.Code, newFilterRec.Body.String())
	}
	var newFilterResp jsonRPCResponse
	if err := json.Unmarshal(newFilterRec.Body.Bytes(), &newFilterResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if newFilterResp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", newFilterResp.Error)
	}
	filterID, ok := newFilterResp.Result.(string)
	if !ok || !strings.HasPrefix(strings.ToLower(filterID), "0x") {
		t.Fatalf("invalid filter id: %#v", newFilterResp.Result)
	}

	getFilterLogsReq := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"eth_getFilterLogs","params":["`+filterID+`"]}`))
	getFilterLogsRec := httptest.NewRecorder()
	s.handleJSONRPC(getFilterLogsRec, getFilterLogsReq)
	if getFilterLogsRec.Code != 200 {
		t.Fatalf("eth_getFilterLogs status=%d body=%s", getFilterLogsRec.Code, getFilterLogsRec.Body.String())
	}
	var getFilterLogsResp jsonRPCResponse
	if err := json.Unmarshal(getFilterLogsRec.Body.Bytes(), &getFilterLogsResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if getFilterLogsResp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", getFilterLogsResp.Error)
	}
	logs, ok := getFilterLogsResp.Result.([]any)
	if !ok || len(logs) == 0 {
		t.Fatalf("expected logs from eth_getFilterLogs, got %#v", getFilterLogsResp.Result)
	}

	getChangesBody := `{"jsonrpc":"2.0","id":3,"method":"eth_getFilterChanges","params":["` + filterID + `"]}`
	getChangesReq := httptest.NewRequest("POST", "/rpc", strings.NewReader(getChangesBody))
	getChangesRec := httptest.NewRecorder()
	s.handleJSONRPC(getChangesRec, getChangesReq)
	if getChangesRec.Code != 200 {
		t.Fatalf("eth_getFilterChanges status=%d body=%s", getChangesRec.Code, getChangesRec.Body.String())
	}
	var getChangesResp jsonRPCResponse
	if err := json.Unmarshal(getChangesRec.Body.Bytes(), &getChangesResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if getChangesResp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", getChangesResp.Error)
	}
	changes, ok := getChangesResp.Result.([]any)
	if !ok || len(changes) != 0 {
		t.Fatalf("expected empty initial changes, got %#v", getChangesResp.Result)
	}

	// Add one new block that matches the same log filter, then poll again.
	s.Node.Blockchain.Blocks = append(s.Node.Blockchain.Blocks, Block{
		ID:        13,
		BlockHash: "0x" + strings.Repeat("a", 64),
		Timestamp: 2,
		Transactions: []Transaction{
			{
				ID:          "0x" + strings.Repeat("8", 64),
				EVMTxHash:   "0x" + strings.Repeat("9", 64),
				From:        tx.From,
				To:          tx.To,
				Type:        TxEVM,
				EVMCode:     tx.EVMCode,
				EVMInput:    tx.EVMInput,
				EVMGasLimit: tx.EVMGasLimit,
			},
		},
	})

	getChangesReq2 := httptest.NewRequest("POST", "/rpc", strings.NewReader(getChangesBody))
	getChangesRec2 := httptest.NewRecorder()
	s.handleJSONRPC(getChangesRec2, getChangesReq2)
	if getChangesRec2.Code != 200 {
		t.Fatalf("second eth_getFilterChanges status=%d body=%s", getChangesRec2.Code, getChangesRec2.Body.String())
	}
	var getChangesResp2 jsonRPCResponse
	if err := json.Unmarshal(getChangesRec2.Body.Bytes(), &getChangesResp2); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if getChangesResp2.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", getChangesResp2.Error)
	}
	changes2, ok := getChangesResp2.Result.([]any)
	if !ok || len(changes2) == 0 {
		t.Fatalf("expected non-empty changes after new block, got %#v", getChangesResp2.Result)
	}

	uninstallReq := httptest.NewRequest("POST", "/rpc", strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"eth_uninstallFilter","params":["`+filterID+`"]}`))
	uninstallRec := httptest.NewRecorder()
	s.handleJSONRPC(uninstallRec, uninstallReq)
	if uninstallRec.Code != 200 {
		t.Fatalf("eth_uninstallFilter status=%d body=%s", uninstallRec.Code, uninstallRec.Body.String())
	}
	var uninstallResp jsonRPCResponse
	if err := json.Unmarshal(uninstallRec.Body.Bytes(), &uninstallResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if uninstallResp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", uninstallResp.Error)
	}
	if ok, okType := uninstallResp.Result.(bool); !okType || !ok {
		t.Fatalf("expected uninstall true, got %#v", uninstallResp.Result)
	}
}

func TestJSONRPCCompatWebSocketSubscribeNewHeads(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 1, BlockHash: "0x" + strings.Repeat("1", 64), Timestamp: 1},
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleJSONRPCWS)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_subscribe",
		"params":  []any{"newHeads"},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var subscribeResp map[string]any
	if err := conn.ReadJSON(&subscribeResp); err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	subID, ok := subscribeResp["result"].(string)
	if !ok || !strings.HasPrefix(strings.ToLower(subID), "0x") {
		t.Fatalf("invalid subscribe result: %#v", subscribeResp)
	}

	s.Node.Blockchain.mu.Lock()
	s.Node.Blockchain.Blocks = append(s.Node.Blockchain.Blocks, Block{
		ID:        2,
		BlockHash: "0x" + strings.Repeat("2", 64),
		Timestamp: 2,
	})
	s.Node.Blockchain.mu.Unlock()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var notif map[string]any
	if err := conn.ReadJSON(&notif); err != nil {
		t.Fatalf("read subscription notification: %v", err)
	}
	if got := notif["method"]; got != "eth_subscription" {
		t.Fatalf("unexpected notification method: %#v", notif)
	}
	params, ok := notif["params"].(map[string]any)
	if !ok {
		t.Fatalf("missing params: %#v", notif)
	}
	if got := strings.ToLower(params["subscription"].(string)); got != strings.ToLower(subID) {
		t.Fatalf("unexpected subscription id: got=%s want=%s", got, subID)
	}
	result, ok := params["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing subscription result: %#v", params["result"])
	}
	if got := strings.ToLower(result["number"].(string)); got != "0x2" {
		t.Fatalf("unexpected new head number: %s", got)
	}
}

func TestJSONRPCCompatWebSocketSubscribePendingTransactionsFullTx(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 1, BlockHash: "0x" + strings.Repeat("3", 64), Timestamp: 1},
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
			Mempool: Mempool{
				Transactions: []Transaction{},
				SeenTxIDs:    map[string]bool{},
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleJSONRPCWS)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_subscribe",
		"params":  []any{"newPendingTransactions", true},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var subscribeResp map[string]any
	if err := conn.ReadJSON(&subscribeResp); err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	if _, ok := subscribeResp["result"].(string); !ok {
		t.Fatalf("invalid subscribe response: %#v", subscribeResp)
	}

	tx := Transaction{
		ID:     "0x" + strings.Repeat("9", 64),
		From:   "0x0000000000000000000000000000000000001111",
		To:     "0x0000000000000000000000000000000000002222",
		Amount: 1,
		Fee:    1,
		Type:   TxTransfer,
	}
	s.Node.Mempool.mu.Lock()
	s.Node.Mempool.Transactions = append(s.Node.Mempool.Transactions, tx)
	s.Node.Mempool.mu.Unlock()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var notif map[string]any
	if err := conn.ReadJSON(&notif); err != nil {
		t.Fatalf("read subscription notification: %v", err)
	}
	params, ok := notif["params"].(map[string]any)
	if !ok {
		t.Fatalf("missing params in notification: %#v", notif)
	}
	result, ok := params["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected full tx object result, got %#v", params["result"])
	}
	gotHash, _ := result["hash"].(string)
	if !strings.EqualFold(gotHash, tx.ID) {
		t.Fatalf("unexpected pending tx hash: got=%s want=%s", gotHash, tx.ID)
	}
}

func TestJSONRPCCompatWebSocketSubscribeSyncing(t *testing.T) {
	prevReadAuth := ConfigRPCRequireAuthForReadEndpoints
	ConfigRPCRequireAuthForReadEndpoints = false
	t.Cleanup(func() {
		ConfigRPCRequireAuthForReadEndpoints = prevReadAuth
	})

	s := &Server{
		Node: &Node{
			Blockchain: &Blockchain{
				Blocks: []Block{
					{ID: 1, BlockHash: "0x" + strings.Repeat("4", 64), Timestamp: 1},
				},
			},
			Ledger: Ledger{
				Balances: map[string]int{},
				Nonces:   map[string]int{},
				Stakes:   map[string]StakeLock{},
			},
			Consensus: &ConsensusState{
				Syncing:    false,
				SyncTarget: 0,
			},
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleJSONRPCWS)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteJSON(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "eth_subscribe",
		"params":  []any{"syncing"},
	}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var subscribeResp map[string]any
	if err := conn.ReadJSON(&subscribeResp); err != nil {
		t.Fatalf("read subscribe response: %v", err)
	}
	if _, ok := subscribeResp["result"].(string); !ok {
		t.Fatalf("invalid subscribe response: %#v", subscribeResp)
	}

	s.Node.Consensus.mu.Lock()
	s.Node.Consensus.Syncing = true
	s.Node.Consensus.SyncTarget = 50
	s.Node.Consensus.mu.Unlock()

	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var notif map[string]any
	if err := conn.ReadJSON(&notif); err != nil {
		t.Fatalf("read syncing notification: %v", err)
	}
	params, ok := notif["params"].(map[string]any)
	if !ok {
		t.Fatalf("missing params in notification: %#v", notif)
	}
	result, ok := params["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected syncing map result, got %#v", params["result"])
	}
	if got, _ := result["highestBlock"].(string); strings.ToLower(got) != "0x32" {
		t.Fatalf("unexpected highestBlock: %s", got)
	}
}
