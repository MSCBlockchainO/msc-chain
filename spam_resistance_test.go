package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func deterministicSpamSender(index int) (addr string, pubHex string, priv ed25519.PrivateKey) {
	seed := sha256.Sum256([]byte(fmt.Sprintf("spam-sender-%06d", index)))
	priv = ed25519.NewKeyFromSeed(seed[:])
	pub := priv.Public().(ed25519.PublicKey)
	return AddressFromPublicKey(pub), hex.EncodeToString(pub), priv
}

func makeSpamTransferTx(ledger *Ledger, from string, pubHex string, priv ed25519.PrivateKey, nonce int, expiryBase int64) Transaction {
	tx := Transaction{
		From:      from,
		To:        fmt.Sprintf("spam-receiver-%06d", nonce),
		Amount:    1,
		Nonce:     nonce,
		PublicKey: pubHex,
		Expiry:    expiryBase + int64(nonce),
		Type:      TxTransfer,
		ChainID:   ChainID,
		Coin:      CoinSymbol,
	}
	tx.Fee = requiredFeeForTxWithLedger(ledger, tx)
	tx.Signature = hex.EncodeToString(ed25519.Sign(priv, TxPayload(tx)))
	tx.ID = ComputeTxID(tx)
	return tx
}

func TestSpamResistanceHundredKTxFloodMempoolProposerConsensusTiming(t *testing.T) {
	oldDebugConsensus := DebugConsensus
	DebugConsensus = false
	t.Cleanup(func() { DebugConsensus = oldDebugConsensus })

	resetTxAbuseForTest(t)
	pendingSpendMu.Lock()
	oldPendingSpend := PendingSpend
	PendingSpend = map[string]int{}
	pendingSpendMu.Unlock()
	t.Cleanup(func() {
		pendingSpendMu.Lock()
		PendingSpend = oldPendingSpend
		pendingSpendMu.Unlock()
		resetTxAbuseForTest(t)
	})

	node := newTestNodeForResultGossip(t, filepath.Join(t.TempDir(), "node"), []string{"A", "B", "C", "D"})
	ledger := NewLedger()
	const senderCount = 1000
	const txPerSender = 100
	const attempts = senderCount * txPerSender
	if attempts < 100000 {
		t.Fatalf("test setup must flood at least 100k tx attempts, got=%d", attempts)
	}

	senders := make([]struct {
		addr string
		pub  string
		priv ed25519.PrivateKey
	}, senderCount)
	for i := 0; i < senderCount; i++ {
		addr, pub, priv := deterministicSpamSender(i)
		senders[i] = struct {
			addr string
			pub  string
			priv ed25519.PrivateKey
		}{addr: addr, pub: pub, priv: priv}
		addBalance(&ledger, CoinSymbol, addr, 1_000_000)
	}
	node.Ledger = ledger.Clone()
	node.setExecutionLedger(ledger.Clone())

	expiryBase := time.Now().Add(5 * time.Minute).Unix()
	accepted := 0
	rejected := 0
	var firstAccepted Transaction
	start := time.Now()
	for senderIdx := 0; senderIdx < senderCount; senderIdx++ {
		sender := senders[senderIdx]
		for nonce := 1; nonce <= txPerSender; nonce++ {
			tx := makeSpamTransferTx(&ledger, sender.addr, sender.pub, sender.priv, nonce, expiryBase)
			ok, reason := node.Mempool.AddTransaction(tx, ledger, node.Blockchain.Height())
			if ok {
				accepted++
				if firstAccepted.ID == "" {
					firstAccepted = tx
				}
				continue
			}
			rejected++
			if accepted < MaxMempoolSize {
				t.Fatalf("tx rejected before mempool cap: accepted=%d sender=%d nonce=%d reason=%q", accepted, senderIdx, nonce, reason)
			}
		}
	}
	floodDuration := time.Since(start)

	if accepted != MaxMempoolSize {
		t.Fatalf("mempool accepted beyond/under cap: got=%d want=%d rejected=%d", accepted, MaxMempoolSize, rejected)
	}
	if rejected != attempts-MaxMempoolSize {
		t.Fatalf("unexpected reject count: got=%d want=%d", rejected, attempts-MaxMempoolSize)
	}
	if floodDuration > 30*time.Second {
		t.Fatalf("100k flood path too slow: duration=%s", floodDuration)
	}

	node.Mempool.mu.Lock()
	depth := len(node.Mempool.Transactions)
	indexDepth := len(node.Mempool.txByID)
	nonceIndexDepth := len(node.Mempool.txBySenderNonce)
	pendingSenders := len(node.Mempool.pendingCountBySender)
	nextNonceFirst := node.Mempool.nextNonceBySender[mempoolSenderKey(senders[0].addr)]
	node.Mempool.mu.Unlock()
	if depth != MaxMempoolSize || indexDepth != depth || nonceIndexDepth != depth {
		t.Fatalf("mempool index drift after flood: depth=%d txByID=%d senderNonce=%d", depth, indexDepth, nonceIndexDepth)
	}
	if pendingSenders != MaxMempoolSize/MaxTxPerAddress {
		t.Fatalf("unexpected pending sender count: got=%d want=%d", pendingSenders, MaxMempoolSize/MaxTxPerAddress)
	}
	if nextNonceFirst != MaxTxPerAddress+1 {
		t.Fatalf("next nonce drift after flood: got=%d want=%d", nextNonceFirst, MaxTxPerAddress+1)
	}
	if !node.Mempool.HasTx(firstAccepted.ID) {
		t.Fatalf("accepted tx missing from mempool index: %s", firstAccepted.ID)
	}

	epoch := node.currentEpoch()
	validators := node.freezeValidatorSetForHeight(epoch, node.GetConsensusValidators(int(epoch)))
	round := node.proposedRoundForHeight(epoch)
	expectedProposer := node.consensusLeaderForHeightRound(epoch, round, validators)
	buildStart := time.Now()
	block := node.BuildLeaderBlock(epoch)
	buildDuration := time.Since(buildStart)
	if buildDuration > 15*time.Second {
		t.Fatalf("leader block build exceeded max consensus time: duration=%s", buildDuration)
	}
	if block.Proposer != expectedProposer {
		t.Fatalf("proposer drift under tx flood: got=%s want=%s", block.Proposer, expectedProposer)
	}
	if got, want := len(block.Transactions), GlobalConfig.MaxTxPerBlock; got != want {
		t.Fatalf("leader block tx batch unstable: got=%d want=%d", got, want)
	}
	if block.MempoolRoot != ComputeMempoolRoot(block.Transactions) {
		t.Fatal("leader block mempool root mismatch after flood")
	}
	if block.Timestamp != int64(SystemTimeUnits(LogicalTimeForEpoch(block.ID))) {
		t.Fatalf("consensus logical timestamp drift: got=%d want=%d",
			block.Timestamp,
			int64(SystemTimeUnits(LogicalTimeForEpoch(block.ID))),
		)
	}

	again := node.BuildLeaderBlock(epoch)
	if again.Proposer != block.Proposer || again.MempoolRoot != block.MempoolRoot || len(again.Transactions) != len(block.Transactions) {
		t.Fatalf("leader proposal unstable across repeated builds: first proposer=%s txs=%d root=%s second proposer=%s txs=%d root=%s",
			block.Proposer, len(block.Transactions), ShortHash(block.MempoolRoot),
			again.Proposer, len(again.Transactions), ShortHash(again.MempoolRoot),
		)
	}

	node.Mempool.RemoveIncluded(block.Transactions)
	node.Mempool.mu.Lock()
	remaining := len(node.Mempool.Transactions)
	remainingIndex := len(node.Mempool.txByID)
	remainingNonceIndex := len(node.Mempool.txBySenderNonce)
	node.Mempool.mu.Unlock()
	if remaining != MaxMempoolSize-len(block.Transactions) || remainingIndex != remaining || remainingNonceIndex != remaining {
		t.Fatalf("mempool removal/index unstable after proposal: remaining=%d txByID=%d senderNonce=%d want=%d",
			remaining,
			remainingIndex,
			remainingNonceIndex,
			MaxMempoolSize-len(block.Transactions),
		)
	}
}
