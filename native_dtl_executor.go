package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	dtlcore "msc-chain/dtl"
	statecore "msc-chain/state"
)

// NativeDTLState is the complete state owned by the native DTL executor.
//
// UsedBridgeEvents remains projected onto Ledger.UsedBridgeEvents at the
// persistence boundary so existing snapshots and state-root encoding stay
// wire-compatible. It is nevertheless DTL execution state: consensus never
// reads or mutates it to select leaders, committees, votes, or quorum.
type NativeDTLState struct {
	State            *DTLState
	UsedBridgeEvents map[string]uint64
}

// NativeDTLExecutionInput is the entire deterministic input visible to DTL.
// The executor deliberately receives no Node, network, mempool, validator,
// clock, randomness, local configuration, or consensus runtime object.
type NativeDTLExecutionInput struct {
	Context             dtlcore.Context
	OrderedTransactions []Transaction
	Height              uint64
	Version             dtlcore.Version
}

// NativeDTLReceipt describes one ordered DTL state transition without exposing
// the resulting mutable state to consensus.
type NativeDTLReceipt struct {
	TxHash        string
	PreStateRoot  string
	PostStateRoot string
	Logs          []DTLEventLog
	ResourceUsage dtlcore.Usage
}

// NativeDTLExecutionResult is the sole DTL executor output.
type NativeDTLExecutionResult struct {
	Next     NativeDTLState
	Receipts []NativeDTLReceipt
	Usage    dtlcore.Usage
}

func addDTLResourceUsage(total *dtlcore.Usage, delta dtlcore.Usage) {
	if total == nil {
		return
	}
	total.Reads += delta.Reads
	total.Writes += delta.Writes
	total.Events += delta.Events
	total.Steps += delta.Steps
	total.StorageBytes += delta.StorageBytes
	total.Fee += delta.Fee
}

// DTLExecutor defines the one-way execution boundary used by block execution.
type DTLExecutor interface {
	Execute(NativeDTLExecutionInput) (NativeDTLExecutionResult, error)
}

// NativeDTLExecutor is stateless. Consensus paths instantiate this value
// directly, so a mutable/local implementation cannot alter block replay.
type NativeDTLExecutor struct{}

var _ DTLExecutor = NativeDTLExecutor{}

// DTLExecutorV1 and DTLExecutorV2 are distinct protocol implementations even
// while most operations intentionally share transition code. Version choice is
// supplied by the block's committed feature envelope, never local config.
type DTLExecutorV1 struct{}
type DTLExecutorV2 struct{}

var _ dtlcore.Executor[NativeDTLExecutionInput, NativeDTLExecutionResult] = DTLExecutorV1{}
var _ dtlcore.Executor[NativeDTLExecutionInput, NativeDTLExecutionResult] = DTLExecutorV2{}

var nativeDTLStateKey = []byte("dtl/native-state/v1")

type nativeDTLStateEnvelope struct {
	State            *DTLState         `json:"state"`
	UsedBridgeEvents map[string]uint64 `json:"used_bridge_events,omitempty"`
}

func encodeNativeDTLState(state NativeDTLState) ([]byte, error) {
	state = cloneNativeDTLState(state)
	return json.Marshal(nativeDTLStateEnvelope{
		State:            state.State,
		UsedBridgeEvents: state.UsedBridgeEvents,
	})
}

func decodeNativeDTLState(reader statecore.StateReader) (NativeDTLState, error) {
	if reader == nil {
		return NativeDTLState{}, fmt.Errorf("dtl: state reader required")
	}
	raw, err := reader.Get(nativeDTLStateKey)
	if err != nil {
		return NativeDTLState{}, fmt.Errorf("dtl: read native state: %w", err)
	}
	var envelope nativeDTLStateEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return NativeDTLState{}, fmt.Errorf("dtl: decode native state: %w", err)
	}
	return cloneNativeDTLState(NativeDTLState{
		State:            envelope.State,
		UsedBridgeEvents: envelope.UsedBridgeEvents,
	}), nil
}

// newNativeDTLExecutionContext creates an atomic state overlay. The executor
// receives only read/write capabilities; the caller retains commit authority.
func newNativeDTLExecutionContext(parent NativeDTLState, height uint64) (dtlcore.Context, error) {
	base := statecore.NewMemory()
	raw, err := encodeNativeDTLState(parent)
	if err != nil {
		return dtlcore.Context{}, err
	}
	if err := base.Set(nativeDTLStateKey, raw); err != nil {
		return dtlcore.Context{}, err
	}
	overlay := statecore.NewOverlay(base)
	return dtlcore.Context{
		Height: height,
		Reader: overlay,
		Writer: overlay,
	}, nil
}

func nativeDTLExecutionInput(
	parent NativeDTLState,
	transactions []Transaction,
	height uint64,
	version dtlcore.Version,
) (NativeDTLExecutionInput, error) {
	ctx, err := newNativeDTLExecutionContext(parent, height)
	if err != nil {
		return NativeDTLExecutionInput{}, err
	}
	return NativeDTLExecutionInput{
		Context:             ctx,
		OrderedTransactions: append([]Transaction(nil), transactions...),
		Height:              height,
		Version:             version,
	}, nil
}

func nativeDTLExecutorVersion(v2 bool) dtlcore.Version {
	if v2 {
		return dtlcore.VersionV2
	}
	return dtlcore.VersionV1
}

func cloneNativeDTLState(src NativeDTLState) NativeDTLState {
	out := NativeDTLState{
		State:            cloneDTLState(src.State),
		UsedBridgeEvents: make(map[string]uint64, len(src.UsedBridgeEvents)),
	}
	if out.State == nil {
		out.State = NewDTLState()
	}
	for rawKey, height := range src.UsedBridgeEvents {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if key == "" {
			continue
		}
		if existing, ok := out.UsedBridgeEvents[key]; !ok || height < existing {
			out.UsedBridgeEvents[key] = height
		}
	}
	return out
}

func nativeDTLStateFromLedger(ledger Ledger) NativeDTLState {
	return cloneNativeDTLState(NativeDTLState{
		State:            ledger.DTL,
		UsedBridgeEvents: ledger.UsedBridgeEvents,
	})
}

func projectNativeDTLStateToLedger(ledger *Ledger, state NativeDTLState) {
	if ledger == nil {
		return
	}
	state = cloneNativeDTLState(state)
	ledger.DTL = state.State
	ledger.UsedBridgeEvents = state.UsedBridgeEvents
}

// hashNativeDTLState commits only DTL-owned state. The legacy Ledger hash keeps
// using its established byte layout; this root is for executor receipts and
// isolation tests, not a silent state-root protocol upgrade.
func hashNativeDTLState(state NativeDTLState) string {
	state = cloneNativeDTLState(state)
	var b strings.Builder
	bridgeKeys := make([]string, 0, len(state.UsedBridgeEvents))
	for key := range state.UsedBridgeEvents {
		bridgeKeys = append(bridgeKeys, key)
	}
	sort.Strings(bridgeKeys)
	for _, key := range bridgeKeys {
		b.WriteString("bridge_event|")
		b.WriteString(key)
		b.WriteString("=")
		b.WriteString(strconv.FormatUint(state.UsedBridgeEvents[key], 10))
		b.WriteString(";")
	}
	appendDTLStateHashMaterial(&b, state.State)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func cloneDTLEventLogs(logs []DTLEventLog) []DTLEventLog {
	if len(logs) == 0 {
		return nil
	}
	out := make([]DTLEventLog, 0, len(logs))
	for _, logEntry := range logs {
		copyEntry := logEntry
		copyEntry.Topics = append([]string(nil), logEntry.Topics...)
		out = append(out, copyEntry)
	}
	return out
}

func executeNativeDTL(ctx dtlcore.Context, input NativeDTLExecutionInput, v2 bool) (NativeDTLExecutionResult, error) {
	if input.Height == 0 {
		input.Height = ctx.Height
	}
	if ctx.Height != 0 && input.Height != ctx.Height {
		return NativeDTLExecutionResult{}, fmt.Errorf("dtl: execution height mismatch")
	}
	if ctx.Reader == nil || ctx.Writer == nil {
		return NativeDTLExecutionResult{}, fmt.Errorf("dtl: state capabilities required")
	}
	next, err := decodeNativeDTLState(ctx.Reader)
	if err != nil {
		return NativeDTLExecutionResult{}, err
	}
	receipts := make([]NativeDTLReceipt, 0, len(input.OrderedTransactions))
	totalUsage := dtlcore.Usage{}
	for _, tx := range input.OrderedTransactions {
		if tx.Type != TxDTL {
			return NativeDTLExecutionResult{}, fmt.Errorf("dtl: non-dtl transaction in executor input: %s", tx.ID)
		}
		preRoot := hashNativeDTLState(next)
		preLogCount := len(next.State.EventLogs)
		if err := validateNativeDTLTransactionVersion(&next, tx, input.Height, v2); err != nil {
			return NativeDTLExecutionResult{}, err
		}
		if err := applyNativeDTLTransactionVersion(&next, tx, input.Height, v2); err != nil {
			return NativeDTLExecutionResult{}, err
		}
		logs := []DTLEventLog(nil)
		if preLogCount < len(next.State.EventLogs) {
			logs = cloneDTLEventLogs(next.State.EventLogs[preLogCount:])
		}
		postRoot := hashNativeDTLState(next)
		usage, err := deterministicDTLResourceUsage(tx, logs, !strings.EqualFold(preRoot, postRoot))
		if err != nil {
			return NativeDTLExecutionResult{}, err
		}
		addDTLResourceUsage(&totalUsage, usage)
		receipts = append(receipts, NativeDTLReceipt{
			TxHash:        strings.TrimSpace(tx.ID),
			PreStateRoot:  preRoot,
			PostStateRoot: postRoot,
			Logs:          logs,
			ResourceUsage: usage,
		})
	}
	raw, err := encodeNativeDTLState(next)
	if err != nil {
		return NativeDTLExecutionResult{}, err
	}
	if err := ctx.Writer.Set(nativeDTLStateKey, raw); err != nil {
		return NativeDTLExecutionResult{}, fmt.Errorf("dtl: publish native state: %w", err)
	}
	published, err := decodeNativeDTLState(ctx.Reader)
	if err != nil {
		return NativeDTLExecutionResult{}, fmt.Errorf("dtl: verify published native state: %w", err)
	}
	return NativeDTLExecutionResult{Next: published, Receipts: receipts, Usage: totalUsage}, nil
}

func (DTLExecutorV1) Version() dtlcore.Version { return dtlcore.VersionV1 }

func (DTLExecutorV1) Execute(ctx dtlcore.Context, input NativeDTLExecutionInput) (NativeDTLExecutionResult, error) {
	input.Version = dtlcore.VersionV1
	return executeNativeDTL(ctx, input, false)
}

func (DTLExecutorV2) Version() dtlcore.Version { return dtlcore.VersionV2 }

func (DTLExecutorV2) Execute(ctx dtlcore.Context, input NativeDTLExecutionInput) (NativeDTLExecutionResult, error) {
	input.Version = dtlcore.VersionV2
	return executeNativeDTL(ctx, input, true)
}

// Execute dispatches to the exact executor version committed by the block.
func (NativeDTLExecutor) Execute(input NativeDTLExecutionInput) (NativeDTLExecutionResult, error) {
	version := input.Version
	if version == 0 {
		version = dtlcore.VersionV1
	}
	dispatcher := dtlcore.Dispatcher[NativeDTLExecutionInput, NativeDTLExecutionResult]{
		V1: DTLExecutorV1{},
		V2: DTLExecutorV2{},
	}
	return dispatcher.Execute(version, input.Context, input)
}
