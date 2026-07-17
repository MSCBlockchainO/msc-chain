package main

import (
	"sync"
	"sync/atomic"
)

// NodeTaskThread is a dedicated serialized worker lane for a specific class of
// node work. We keep one queue per lane so consensus, execution, and sync work
// stop contending on arbitrary ad-hoc goroutines.
type NodeTaskThread struct {
	// `name` stores the value associated with this record.
	name string
	// `node` stores the value associated with this record.
	node *Node

	// `mu` stores the synchronization state protecting shared data.
	mu     sync.Mutex
	// `cond` stores the value associated with this record.
	cond   *sync.Cond
	// `queue` stores the value associated with this record.
	queue  []func()
	// `closed` stores the value associated with this record.
	closed bool

	// `executed` stores the value associated with this record.
	executed uint64
}

// newNodeTaskThread implements the new node task thread helper.
func newNodeTaskThread(node *Node, name string) *NodeTaskThread {
	// `thread` stores the value produced by this operation.
	thread := &NodeTaskThread{
		name:  name,
		node:  node,
		queue: make([]func(), 0, 16),
	}
	thread.cond = sync.NewCond(&thread.mu)
	return thread
}

// Start implements the start helper.
func (t *NodeTaskThread) Start() {
	if t == nil || t.node == nil {
		return
	}
	t.node.wg.Add(1)
	go func() {
		defer t.node.wg.Done()
		for {
			// `task` stores the value produced by this operation.
			task := t.next()
			if task == nil {
				return
			}
			runGuarded("node_thread:"+t.name, task)
			atomic.AddUint64(&t.executed, 1)
		}
	}()
}

// next implements the next helper.
func (t *NodeTaskThread) next() func() {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for len(t.queue) == 0 && !t.closed && (t.node == nil || !t.node.isShuttingDown()) {
		t.cond.Wait()
	}
	if len(t.queue) == 0 {
		return nil
	}
	// `task` stores the value produced by this operation.
	task := t.queue[0]
	t.queue[0] = nil
	t.queue = t.queue[1:]
	return task
}

// Submit implements the submit helper.
func (t *NodeTaskThread) Submit(task func()) bool {
	if t == nil || task == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || (t.node != nil && t.node.isShuttingDown()) {
		return false
	}
	t.queue = append(t.queue, task)
	t.cond.Signal()
	return true
}

// SubmitPriority implements the submit priority helper.
func (t *NodeTaskThread) SubmitPriority(task func()) bool {
	if t == nil || task == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || (t.node != nil && t.node.isShuttingDown()) {
		return false
	}
	t.queue = append(t.queue, nil)
	copy(t.queue[1:], t.queue[:len(t.queue)-1])
	t.queue[0] = task
	t.cond.Signal()
	return true
}

// Schedule implements the schedule helper.
func (t *NodeTaskThread) Schedule(task func()) bool {
	return t.Submit(task)
}

// Close implements the close helper.
func (t *NodeTaskThread) Close() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.closed = true
	// Drop any queued work on shutdown so post-cancel tasks cannot touch
	// databases, pubsub topics, or consensus state after teardown begins.
	for i := range t.queue {
		t.queue[i] = nil
	}
	t.queue = nil
	t.cond.Broadcast()
	t.mu.Unlock()
}

// ExecutedCount implements the executed count helper.
func (t *NodeTaskThread) ExecutedCount() uint64 {
	if t == nil {
		return 0
	}
	return atomic.LoadUint64(&t.executed)
}

// PendingCount implements the pending count helper.
func (t *NodeTaskThread) PendingCount() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.queue)
}

// ensureDedicatedThreads implements the ensure dedicated threads helper.
func (n *Node) ensureDedicatedThreads() {
	if n == nil {
		return
	}
	n.threadInitOnce.Do(func() {
		if n.shutdownCh == nil {
			n.shutdownCh = make(chan struct{})
		}
		if n.closeChan == nil {
			n.closeChan = make(chan struct{})
		}
		n.ConsensusThread = newNodeTaskThread(n, "consensus")
		n.ExecutionThread = newNodeTaskThread(n, "execution")
		n.SyncThread = newNodeTaskThread(n, "sync")
		n.ConsensusThread.Start()
		n.ExecutionThread.Start()
		n.SyncThread.Start()
	})
}

// stopDedicatedThreads implements the stop dedicated threads helper.
func (n *Node) stopDedicatedThreads() {
	if n == nil {
		return
	}
	if n.ConsensusThread != nil {
		n.ConsensusThread.Close()
	}
	if n.ExecutionThread != nil {
		n.ExecutionThread.Close()
	}
	if n.SyncThread != nil {
		n.SyncThread.Close()
	}
}

// scheduleConsensusTask implements the schedule consensus task helper.
func (n *Node) scheduleConsensusTask(task func()) bool {
	if n == nil || task == nil {
		return false
	}
	n.ensureDedicatedThreads()
	return n.ConsensusThread != nil && n.ConsensusThread.Submit(task)
}

// scheduleConsensusPriorityTask implements the schedule consensus priority task helper.
func (n *Node) scheduleConsensusPriorityTask(task func()) bool {
	if n == nil || task == nil {
		return false
	}
	n.ensureDedicatedThreads()
	return n.ConsensusThread != nil && n.ConsensusThread.SubmitPriority(task)
}

// scheduleExecutionTask implements the schedule execution task helper.
func (n *Node) scheduleExecutionTask(task func()) bool {
	if n == nil || task == nil {
		return false
	}
	n.ensureDedicatedThreads()
	return n.ExecutionThread != nil && n.ExecutionThread.Submit(task)
}

// scheduleSyncTask implements the schedule sync task helper.
func (n *Node) scheduleSyncTask(task func()) bool {
	if n == nil || task == nil {
		return false
	}
	n.ensureDedicatedThreads()
	return n.SyncThread != nil && n.SyncThread.Submit(task)
}
