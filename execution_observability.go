package main

import (
	"errors"
	"strings"
	"sync/atomic"
	"time"
)

type executionTelemetrySnapshot struct {
	DurationMs      uint64
	QueueDepth      int64
	RejectedTxTotal uint64
	RejectedReason  string
	LastHeight      uint64
}

var executionTelemetry struct {
	durationMs      atomic.Uint64
	queueDepth      atomic.Int64
	rejectedTxTotal atomic.Uint64
	rejectedReason  atomic.Value
	lastHeight      atomic.Uint64
}

func executionRejectedReason(err error) string {
	if err == nil {
		return ""
	}
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	reason := strings.TrimSpace(err.Error())
	if len(reason) > 160 {
		reason = reason[:160]
	}
	return reason
}

func beginExecutionObservation(height uint64) func(error) {
	executionTelemetry.queueDepth.Add(1)
	started := time.Now()
	return func(err error) {
		executionTelemetry.queueDepth.Add(-1)
		executionTelemetry.durationMs.Store(durationMillisForMetrics(time.Since(started)))
		if err != nil {
			executionTelemetry.rejectedTxTotal.Add(1)
			executionTelemetry.rejectedReason.Store(executionRejectedReason(err))
			return
		}
		executionTelemetry.lastHeight.Store(height)
	}
}

func currentExecutionTelemetry() executionTelemetrySnapshot {
	reason := ""
	if value := executionTelemetry.rejectedReason.Load(); value != nil {
		reason, _ = value.(string)
	}
	return executionTelemetrySnapshot{
		DurationMs:      executionTelemetry.durationMs.Load(),
		QueueDepth:      executionTelemetry.queueDepth.Load(),
		RejectedTxTotal: executionTelemetry.rejectedTxTotal.Load(),
		RejectedReason:  reason,
		LastHeight:      executionTelemetry.lastHeight.Load(),
	}
}

func (n *Node) populateExecutionProtocolRuntimeStatus(out *RuntimeStatusSnapshot) {
	if n == nil || out == nil {
		return
	}
	telemetry := currentExecutionTelemetry()
	out.ExecutionDurationMs = telemetry.DurationMs
	out.ExecutionQueueDepth = telemetry.QueueDepth
	out.ExecutionRejectedTxTotal = telemetry.RejectedTxTotal
	out.ExecutionRejectedReason = telemetry.RejectedReason
	out.LastExecutedHeight = telemetry.LastHeight
	out.ExecutionVersion = blockProtocolVersionV1
	if n.Blockchain != nil {
		block := n.Blockchain.LastBlock()
		if block.ProtocolVersion > 0 {
			out.ExecutionVersion = block.ProtocolVersion
		}
		out.ExecutionFeatureBitmap = block.FeatureBitmap
		out.DTLV2ActivationHeight = block.DTLV2ActivationHeight
		out.ValidatorSetVersion = block.ValidatorSetVersion
		out.CommitteeHash = block.CommitteeHash
		out.DTLStateRoot = block.DTLStateRoot
		out.DTLReceiptsRoot = block.DTLReceiptsRoot
		if out.LastExecutedHeight == 0 {
			out.LastExecutedHeight = block.ID
		}
	}
	if out.Height > out.FinalizedHeight {
		out.FinalityLagBlocks = out.Height - out.FinalizedHeight
	}
	obs := n.observabilityStatsSnapshot()
	out.ReplayErrors = obs.ReplayFailures
	out.SnapshotRestoreCount = obs.SnapshotApplyTotal
	out.AutoHealCount = obs.AutoHealActionsTotal
}
