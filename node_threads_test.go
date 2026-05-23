package main

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"
)

func waitForThreadExecuted(t *testing.T, thread *NodeTaskThread, baseline uint64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if thread != nil && thread.ExecutedCount() > baseline {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if thread == nil {
		t.Fatalf("expected dedicated thread to be initialized")
	}
	t.Fatalf("expected thread %q to execute queued work", thread.name)
}

func occupyConsensusThread(t *testing.T, node *Node) chan struct{} {
	t.Helper()
	started := make(chan struct{})
	release := make(chan struct{})
	if !node.ConsensusThread.Submit(func() {
		close(started)
		<-release
	}) {
		t.Fatalf("failed to schedule blocking consensus task")
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for blocking consensus task")
	}
	return release
}

func waitForPendingCount(t *testing.T, thread *NodeTaskThread, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if thread != nil && thread.PendingCount() == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if thread == nil {
		t.Fatalf("expected dedicated thread to be initialized")
	}
	t.Fatalf("expected thread %q pending count %d, got %d", thread.name, want, thread.PendingCount())
}

func TestEnsureDedicatedThreadsInitializesWorkers(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})

	if node.ConsensusThread == nil || node.ExecutionThread == nil || node.SyncThread == nil {
		t.Fatalf("expected consensus/execution/sync threads to be initialized")
	}

	consensusBase := node.ConsensusThread.ExecutedCount()
	executionBase := node.ExecutionThread.ExecutedCount()
	syncBase := node.SyncThread.ExecutedCount()

	consensusDone := make(chan struct{})
	executionDone := make(chan struct{})
	syncDone := make(chan struct{})

	if !node.ConsensusThread.Submit(func() { close(consensusDone) }) {
		t.Fatalf("failed to schedule consensus task")
	}
	if !node.ExecutionThread.Submit(func() { close(executionDone) }) {
		t.Fatalf("failed to schedule execution task")
	}
	if !node.SyncThread.Submit(func() { close(syncDone) }) {
		t.Fatalf("failed to schedule sync task")
	}

	waitForThreadExecuted(t, node.ConsensusThread, consensusBase)
	waitForThreadExecuted(t, node.ExecutionThread, executionBase)
	waitForThreadExecuted(t, node.SyncThread, syncBase)

	select {
	case <-consensusDone:
	default:
		t.Fatalf("expected consensus task to run")
	}
	select {
	case <-executionDone:
	default:
		t.Fatalf("expected execution task to run")
	}
	select {
	case <-syncDone:
	default:
		t.Fatalf("expected sync task to run")
	}
}

func TestAsyncPathsRouteToExecutionAndSyncThreads(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})

	executionBase := node.ExecutionThread.ExecutedCount()
	node.runPostBlockEffectsAsync(Block{ID: 1, Type: BlockTypeTime, Proposer: "A"}, NewLedger())
	waitForThreadExecuted(t, node.ExecutionThread, executionBase)

	syncBase := node.SyncThread.ExecutedCount()
	node.maybeSyncToBestObservedHeight("test")
	waitForThreadExecuted(t, node.SyncThread, syncBase)
}

func TestStartNextRoundImmediatelyRoutesToConsensusThread(t *testing.T) {
	oldResultGossipOnly := ResultGossipOnly
	t.Cleanup(func() {
		ResultGossipOnly = oldResultGossipOnly
	})
	ResultGossipOnly = true

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})

	consensusBase := node.ConsensusThread.ExecutedCount()
	node.startNextRoundImmediately(1, NewLedger())
	waitForThreadExecuted(t, node.ConsensusThread, consensusBase)
}

func TestStartNextRoundImmediatelyDedupesSameHeightWhileQueued(t *testing.T) {
	oldResultGossipOnly := ResultGossipOnly
	t.Cleanup(func() {
		ResultGossipOnly = oldResultGossipOnly
	})
	ResultGossipOnly = true

	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})

	started := make(chan struct{})
	release := make(chan struct{})
	if !node.ConsensusThread.Submit(func() {
		close(started)
		<-release
	}) {
		t.Fatalf("failed to schedule blocking consensus task")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for blocking consensus task")
	}

	node.startNextRoundImmediately(1, NewLedger())
	node.startNextRoundImmediately(1, NewLedger())

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if pending := node.ConsensusThread.PendingCount(); pending == 1 {
			close(release)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	close(release)
	t.Fatalf("expected one queued immediate round-start task, got %d", node.ConsensusThread.PendingCount())
}

func TestHandleConsensusEnvelopeRoutesExecutionResultToConsensusThread(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	release := occupyConsensusThread(t, node)
	defer close(release)

	payload, err := json.Marshal(ExecutionResultMsg{
		HeightHint:    1,
		RoundHint:     0,
		BlockHashHint: "block-1",
		ExecHash:      "exec-1",
		Signer:        "B",
	})
	if err != nil {
		t.Fatalf("failed to marshal execution result: %v", err)
	}
	data, err := json.Marshal(Message{Type: MsgExecutionResult, Data: payload})
	if err != nil {
		t.Fatalf("failed to marshal consensus envelope: %v", err)
	}

	if !node.handleConsensusEnvelope(data) {
		t.Fatalf("expected consensus envelope to be recognized")
	}
	waitForPendingCount(t, node.ConsensusThread, 1)
}

func TestSubmitLeaderBlockOnConsensusLaneQueuesWork(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	release := occupyConsensusThread(t, node)
	defer close(release)

	if !node.submitLeaderBlockOnConsensusLane(Block{
		ID:        1,
		Round:     0,
		Type:      BlockTypeTime,
		Proposer:  "B",
		BlockHash: "leader-1",
		PrevHash:  "genesis",
	}, "peer-B") {
		t.Fatalf("expected leader block to queue on consensus lane")
	}
	waitForPendingCount(t, node.ConsensusThread, 1)
}

func TestSubmitFinalBlockOnConsensusLaneQueuesWork(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A", "B", "C", "D"})
	release := occupyConsensusThread(t, node)
	defer close(release)

	if !node.submitFinalBlockOnConsensusLane(Block{
		ID:        1,
		Round:     0,
		Type:      BlockTypeTime,
		Proposer:  "B",
		BlockHash: "final-1",
		PrevHash:  "genesis",
	}) {
		t.Fatalf("expected final block to queue on consensus lane")
	}
	waitForPendingCount(t, node.ConsensusThread, 1)
}

func TestConsensusPriorityTaskRunsBeforeQueuedWork(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})

	started := make(chan struct{})
	release := make(chan struct{})
	order := make(chan string, 2)

	if !node.ConsensusThread.Submit(func() {
		close(started)
		<-release
	}) {
		t.Fatalf("failed to schedule first consensus task")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for first consensus task to start")
	}

	if !node.ConsensusThread.Submit(func() {
		order <- "normal"
	}) {
		t.Fatalf("failed to schedule normal consensus task")
	}
	if !node.ConsensusThread.SubmitPriority(func() {
		order <- "priority"
	}) {
		t.Fatalf("failed to schedule priority consensus task")
	}

	close(release)

	select {
	case first := <-order:
		if first != "priority" {
			t.Fatalf("expected priority task first, got %q", first)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for priority consensus task")
	}

	select {
	case second := <-order:
		if second != "normal" {
			t.Fatalf("expected normal task second, got %q", second)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for queued consensus task")
	}
}

func TestStopDedicatedThreadsDropsQueuedConsensusWork(t *testing.T) {
	node := newTestNodeForResultGossip(t, t.TempDir(), []string{"A"})

	started := make(chan struct{})
	release := make(chan struct{})
	var ranQueued atomic.Bool

	if !node.ConsensusThread.Submit(func() {
		close(started)
		<-release
	}) {
		t.Fatalf("failed to schedule first consensus task")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for first consensus task to start")
	}

	if !node.ConsensusThread.Submit(func() {
		ranQueued.Store(true)
	}) {
		t.Fatalf("failed to schedule queued consensus task")
	}

	node.stopDedicatedThreads()
	close(release)

	done := make(chan struct{})
	go func() {
		node.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for dedicated threads to stop")
	}

	if ranQueued.Load() {
		t.Fatalf("expected queued consensus work to be dropped during shutdown")
	}
}
