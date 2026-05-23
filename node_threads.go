package main

import (
	"sync"
	"sync/atomic"
)

// NodeTaskThread is a dedicated serialized worker lane for a specific class of
// node work. We keep one queue per lane so consensus, execution, and sync work
// stop contending on arbitrary ad-hoc goroutines.
type NodeTaskThread struct {
	name string
	node *Node

	mu     sync.Mutex
	cond   *sync.Cond
	queue  []func()
	closed bool

	executed uint64
}

func newNodeTaskThread(node *Node, name string) *NodeTaskThread {
	thread := &NodeTaskThread{
		name:  name,
		node:  node,
		queue: make([]func(), 0, 16),
	}
	thread.cond = sync.NewCond(&thread.mu)
	return thread
}

func (t *NodeTaskThread) Start() {
	if t == nil || t.node == nil {
		return
	}
	t.node.wg.Add(1)
	go func() {
		defer t.node.wg.Done()
		for {
			task := t.next()
			if task == nil {
				return
			}
			runGuarded("node_thread:"+t.name, task)
			atomic.AddUint64(&t.executed, 1)
		}
	}()
}

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
	task := t.queue[0]
	t.queue[0] = nil
	t.queue = t.queue[1:]
	return task
}

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

func (t *NodeTaskThread) Schedule(task func()) bool {
	return t.Submit(task)
}

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

func (t *NodeTaskThread) ExecutedCount() uint64 {
	if t == nil {
		return 0
	}
	return atomic.LoadUint64(&t.executed)
}

func (t *NodeTaskThread) PendingCount() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.queue)
}

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

func (n *Node) scheduleConsensusTask(task func()) bool {
	if n == nil || task == nil {
		return false
	}
	n.ensureDedicatedThreads()
	return n.ConsensusThread != nil && n.ConsensusThread.Submit(task)
}

func (n *Node) scheduleConsensusPriorityTask(task func()) bool {
	if n == nil || task == nil {
		return false
	}
	n.ensureDedicatedThreads()
	return n.ConsensusThread != nil && n.ConsensusThread.SubmitPriority(task)
}

func (n *Node) scheduleExecutionTask(task func()) bool {
	if n == nil || task == nil {
		return false
	}
	n.ensureDedicatedThreads()
	return n.ExecutionThread != nil && n.ExecutionThread.Submit(task)
}

func (n *Node) scheduleSyncTask(task func()) bool {
	if n == nil || task == nil {
		return false
	}
	n.ensureDedicatedThreads()
	return n.SyncThread != nil && n.SyncThread.Submit(task)
}
