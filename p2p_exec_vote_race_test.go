package main

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestRecordExecResultGlobalDedupesConcurrentSameSigner(t *testing.T) {
	oldPool := ExecPool.pool
	oldTxMerkle := ExecPool.txMerkle
	oldFrozen := ExecPool.frozen
	oldSigners := ExecPool.signers
	oldChoice := ExecPool.choice
	oldEpochChoice := ExecPool.epochChoice
	ExecPool.mu.Lock()
	ExecPool.pool = make(map[uint64]map[string]map[string]ExecutionResult)
	ExecPool.txMerkle = make(map[uint64]map[string]string)
	ExecPool.frozen = make(map[uint64]map[string]string)
	ExecPool.signers = make(map[uint64]map[string]map[string]bool)
	ExecPool.choice = make(map[uint64]map[string]map[string]string)
	ExecPool.epochChoice = make(map[uint64]map[string]string)
	ExecPool.mu.Unlock()
	t.Cleanup(func() {
		ExecPool.mu.Lock()
		ExecPool.pool = oldPool
		ExecPool.txMerkle = oldTxMerkle
		ExecPool.frozen = oldFrozen
		ExecPool.signers = oldSigners
		ExecPool.choice = oldChoice
		ExecPool.epochChoice = oldEpochChoice
		ExecPool.mu.Unlock()
	})

	const workers = 64
	epoch := uint64(101)
	execHash := "exec-root"
	txMerkle := "tx-root"
	proposalKey := proposalVoteKey(epoch, 0, "block-hash", txMerkle, execHash)
	result := ExecutionResult{
		Height:     epoch,
		Round:      0,
		BlockHash:  "block-hash",
		Signer:     "A",
		ResultHash: execHash,
		TxMerkle:   txMerkle,
		Signature:  "sig",
	}

	start := make(chan struct{})
	counts := make(chan int, workers)
	var wg sync.WaitGroup
	var okCount int32
	var equivocationCount int32
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			count, ok, equivocation := recordExecResultGlobal(epoch, proposalKey, execHash, txMerkle, result)
			if ok {
				atomic.AddInt32(&okCount, 1)
			}
			if equivocation {
				atomic.AddInt32(&equivocationCount, 1)
			}
			counts <- count
		}()
	}
	close(start)
	wg.Wait()
	close(counts)

	if got := atomic.LoadInt32(&okCount); got != 1 {
		t.Fatalf("same signer credited %d times, want 1", got)
	}
	if got := atomic.LoadInt32(&equivocationCount); got != 0 {
		t.Fatalf("same vote reported equivocation %d times, want 0", got)
	}
	for count := range counts {
		if count > 1 {
			t.Fatalf("same signer inflated execution quorum count to %d", count)
		}
	}
	if got := getExecCountGlobal(epoch, proposalKey, execHash, txMerkle); got != 1 {
		t.Fatalf("stored execution vote count=%d, want 1", got)
	}
	if !execVoteCreditedGlobal(epoch, proposalKey, "A", execHash, txMerkle) {
		t.Fatalf("credited execution vote was not visible through replay guard")
	}
}

func TestProposalVoteKeyIgnoresVolatileStateRoot(t *testing.T) {
	emptyRootKey := proposalVoteKey(7, 2, "block-hash", "tx-root", "")
	populatedRootKey := proposalVoteKey(7, 2, "block-hash", "tx-root", "state-root")
	if emptyRootKey != populatedRootKey {
		t.Fatalf("proposal key changed after state root population: empty=%q populated=%q", emptyRootKey, populatedRootKey)
	}
	_, _, _, _, parsedRoot, ok := proposalVoteKeyParts(populatedRootKey)
	if !ok {
		t.Fatalf("proposal key no longer parses: %q", populatedRootKey)
	}
	if parsedRoot != "" {
		t.Fatalf("proposal key must not carry volatile state root, got %q", parsedRoot)
	}
}

func TestRecordExecResultGlobalMergesLegacyStateRootProposalKey(t *testing.T) {
	oldPool := ExecPool.pool
	oldTxMerkle := ExecPool.txMerkle
	oldFrozen := ExecPool.frozen
	oldSigners := ExecPool.signers
	oldChoice := ExecPool.choice
	oldEpochChoice := ExecPool.epochChoice
	ExecPool.mu.Lock()
	ExecPool.pool = make(map[uint64]map[string]map[string]ExecutionResult)
	ExecPool.txMerkle = make(map[uint64]map[string]string)
	ExecPool.frozen = make(map[uint64]map[string]string)
	ExecPool.signers = make(map[uint64]map[string]map[string]bool)
	ExecPool.choice = make(map[uint64]map[string]map[string]string)
	ExecPool.epochChoice = make(map[uint64]map[string]string)
	ExecPool.mu.Unlock()
	t.Cleanup(func() {
		ExecPool.mu.Lock()
		ExecPool.pool = oldPool
		ExecPool.txMerkle = oldTxMerkle
		ExecPool.frozen = oldFrozen
		ExecPool.signers = oldSigners
		ExecPool.choice = oldChoice
		ExecPool.epochChoice = oldEpochChoice
		ExecPool.mu.Unlock()
	})

	epoch := uint64(8)
	blockHash := "block-hash"
	txMerkle := "tx-root"
	execHash := "state-root"
	legacyRootKey := "8|0|block-hash|tx-root|state-root"
	stableKey := proposalVoteKey(epoch, 0, blockHash, txMerkle, "")

	count, ok, equivocation := recordExecResultGlobal(epoch, legacyRootKey, execHash, txMerkle, ExecutionResult{
		Height:     epoch,
		BlockHash:  blockHash,
		Signer:     "A",
		ResultHash: execHash,
		TxMerkle:   txMerkle,
	})
	if !ok || equivocation || count != 1 {
		t.Fatalf("legacy-key vote not credited correctly: count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}

	count, ok, equivocation = recordExecResultGlobal(epoch, stableKey, execHash, txMerkle, ExecutionResult{
		Height:     epoch,
		BlockHash:  blockHash,
		Signer:     "A",
		ResultHash: execHash,
		TxMerkle:   txMerkle,
	})
	if ok || equivocation || count != 1 {
		t.Fatalf("same signer was re-credited across legacy/stable keys: count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}

	count, ok, equivocation = recordExecResultGlobal(epoch, stableKey, execHash, txMerkle, ExecutionResult{
		Height:     epoch,
		BlockHash:  blockHash,
		Signer:     "B",
		ResultHash: execHash,
		TxMerkle:   txMerkle,
	})
	if !ok || equivocation || count != 2 {
		t.Fatalf("distinct signer did not merge into same proposal bucket: count=%d ok=%t equivocation=%t", count, ok, equivocation)
	}
}
