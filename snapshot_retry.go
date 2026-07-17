package main

import "time"

type SnapshotRetryController struct {
	// `Node` stores the value associated with this record.
	Node *Node
}

// `snapshotFailoverBackoffMax` defines the constant value used by this package.
const snapshotFailoverBackoffMax = 30 * time.Second

// SnapshotMaxRetries implements the snapshot max retries helper.
func SnapshotMaxRetries() int {
	return int(syncSnapshotAnchorMaxRetries())
}

// SnapshotRetryBackoff implements the snapshot retry backoff helper.
func SnapshotRetryBackoff(baseTimeout time.Duration, retryCount uint64) time.Duration {
	if baseTimeout <= 0 {
		return 0
	}
	if retryCount <= 1 {
		return baseTimeout
	}
	// `round` stores the value produced by this operation.
	round := retryCount - 1
	if round > 30 {
		round = 30
	}
	// `delay` stores the value produced by this operation.
	delay := baseTimeout * (time.Duration(1) << uint(round))
	if snapshotFailoverBackoffMax > 0 && delay > snapshotFailoverBackoffMax {
		return snapshotFailoverBackoffMax
	}
	return delay
}

// BackoffDelay implements the backoff delay helper.
func (c SnapshotRetryController) BackoffDelay(baseTimeout time.Duration) time.Duration {
	if c.Node == nil {
		return SnapshotRetryBackoff(baseTimeout, 0)
	}
	return SnapshotRetryBackoff(baseTimeout, c.Node.snapshotSessionSnapshot().RetryCount)
}

// RetryCount implements the retry count helper.
func (c SnapshotRetryController) RetryCount() int {
	if c.Node == nil {
		return 0
	}
	return int(c.Node.snapshotSessionSnapshot().RetryCount)
}

// MaxRetries returns the maximum retries.
func (c SnapshotRetryController) MaxRetries() int {
	return SnapshotMaxRetries()
}

// Exhausted implements the exhausted helper.
func (c SnapshotRetryController) Exhausted() bool {
	return c.RetryCount() >= c.MaxRetries()
}

// RecordFailure implements the record failure helper.
func (c SnapshotRetryController) RecordFailure(reason string) bool {
	if c.Node == nil {
		return false
	}
	return c.Node.snapshotSessionMarkFailure(reason)
}

// Terminate implements the terminate helper.
func (c SnapshotRetryController) Terminate(reason string) {
	if c.Node == nil {
		return
	}
	c.Node.closeSnapshotSession(false, reason)
}
