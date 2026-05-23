package main

import "time"

type SnapshotRetryController struct {
	Node *Node
}

const snapshotFailoverBackoffMax = 30 * time.Second

func SnapshotMaxRetries() int {
	return int(syncSnapshotAnchorMaxRetries())
}

func SnapshotRetryBackoff(baseTimeout time.Duration, retryCount uint64) time.Duration {
	if baseTimeout <= 0 {
		return 0
	}
	if retryCount <= 1 {
		return baseTimeout
	}
	round := retryCount - 1
	if round > 30 {
		round = 30
	}
	delay := baseTimeout * (time.Duration(1) << uint(round))
	if snapshotFailoverBackoffMax > 0 && delay > snapshotFailoverBackoffMax {
		return snapshotFailoverBackoffMax
	}
	return delay
}

func (c SnapshotRetryController) BackoffDelay(baseTimeout time.Duration) time.Duration {
	if c.Node == nil {
		return SnapshotRetryBackoff(baseTimeout, 0)
	}
	return SnapshotRetryBackoff(baseTimeout, c.Node.snapshotSessionSnapshot().RetryCount)
}

func (c SnapshotRetryController) RetryCount() int {
	if c.Node == nil {
		return 0
	}
	return int(c.Node.snapshotSessionSnapshot().RetryCount)
}

func (c SnapshotRetryController) MaxRetries() int {
	return SnapshotMaxRetries()
}

func (c SnapshotRetryController) Exhausted() bool {
	return c.RetryCount() >= c.MaxRetries()
}

func (c SnapshotRetryController) RecordFailure(reason string) bool {
	if c.Node == nil {
		return false
	}
	return c.Node.snapshotSessionMarkFailure(reason)
}

func (c SnapshotRetryController) Terminate(reason string) {
	if c.Node == nil {
		return
	}
	c.Node.closeSnapshotSession(false, reason)
}
