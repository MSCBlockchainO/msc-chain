package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	executioncore "msc-chain/execution"
	registrycore "msc-chain/registry"
	statecore "msc-chain/state"
)

// BlockExecutionAuthority is the immutable, committed authority snapshot
// prepared outside the execution engine. It contains no Node/runtime handle.
type BlockExecutionAuthority struct {
	ValidatorRegistry map[string]ValidatorRecord
	ValidatorUpdates  *validatorUpdateExecutionContext
	Registry          registrycore.Snapshot
}

func cloneValidatorUpdateExecutionContext(src *validatorUpdateExecutionContext) *validatorUpdateExecutionContext {
	if src == nil {
		return nil
	}
	out := &validatorUpdateExecutionContext{
		height:               src.height,
		expectedRegistryHash: strings.TrimSpace(src.expectedRegistryHash),
		registrySnapshot:     copyValidatorRegistrySnapshot(src.registrySnapshot),
		activeValidators:     append([]string(nil), src.activeValidators...),
		governanceIDs:        append([]string(nil), src.governanceIDs...),
		governancePubs:       make(map[string]ed25519.PublicKey, len(src.governancePubs)),
		pendingAdds:          copyUint64Map(src.pendingAdds),
		pendingRemovals:      copyUint64Map(src.pendingRemovals),
	}
	for id, pub := range src.governancePubs {
		out.governancePubs[id] = append([]byte(nil), pub...)
	}
	return out
}

func (a BlockExecutionAuthority) clone() BlockExecutionAuthority {
	return BlockExecutionAuthority{
		ValidatorRegistry: copyValidatorRegistrySnapshot(a.ValidatorRegistry),
		ValidatorUpdates:  cloneValidatorUpdateExecutionContext(a.ValidatorUpdates),
		Registry:          a.Registry,
	}
}

// BlockExecutionInput is the complete one-way input to block execution.
type BlockExecutionInput struct {
	Context   executioncore.Context
	Block     Block
	Authority BlockExecutionAuthority
}

var executionLedgerStateKey = []byte("execution/ledger/v1")

type executionLedgerStateReader struct{ ledger Ledger }

func (r executionLedgerStateReader) Get(key []byte) ([]byte, error) {
	if string(key) != string(executionLedgerStateKey) {
		return nil, statecore.ErrNotFound
	}
	raw, err := json.Marshal(r.ledger.Clone())
	if err != nil {
		return nil, fmt.Errorf("execution: encode parent ledger: %w", err)
	}
	return raw, nil
}

func newExecutionStateContext(parent Ledger) executioncore.Context {
	overlay := statecore.NewOverlay(executionLedgerStateReader{ledger: parent.Clone()})
	return executioncore.Context{Reader: overlay, Writer: overlay}
}

func blockExecutionInput(parent Ledger, block Block, authority BlockExecutionAuthority) BlockExecutionInput {
	return BlockExecutionInput{
		Context:   newExecutionStateContext(parent),
		Block:     block,
		Authority: authority,
	}
}

func decodeExecutionLedger(reader statecore.StateReader) (Ledger, error) {
	if reader == nil {
		return Ledger{}, fmt.Errorf("execution: state reader required")
	}
	raw, err := reader.Get(executionLedgerStateKey)
	if err != nil {
		return Ledger{}, fmt.Errorf("execution: read parent ledger: %w", err)
	}
	var ledger Ledger
	if err := json.Unmarshal(raw, &ledger); err != nil {
		return Ledger{}, fmt.Errorf("execution: decode parent ledger: %w", err)
	}
	return ledger.Clone(), nil
}

func publishExecutionLedger(ctx executioncore.Context, ledger Ledger) (Ledger, error) {
	if ctx.Writer == nil {
		return Ledger{}, fmt.Errorf("execution: state writer required")
	}
	raw, err := json.Marshal(ledger.Clone())
	if err != nil {
		return Ledger{}, fmt.Errorf("execution: encode next ledger: %w", err)
	}
	if err := ctx.Writer.Set(executionLedgerStateKey, raw); err != nil {
		return Ledger{}, fmt.Errorf("execution: publish next ledger: %w", err)
	}
	published, err := decodeExecutionLedger(ctx.Reader)
	if err != nil {
		return Ledger{}, fmt.Errorf("execution: verify published ledger: %w", err)
	}
	return published, nil
}

// ExecutionCommitments are the only execution facts consensus needs to seal
// or verify. NextLedger is handed to the state persistence coordinator, not to
// leader/committee/quorum selection.
type ExecutionCommitments = executioncore.Commitment

// BlockExecutionResult contains state-manager output plus commitment-only
// consensus output.
type BlockExecutionResult struct {
	NextLedger  Ledger
	Receipts    []StateReceipt
	Commitments ExecutionCommitments
}

// BlockExecutionEngine is implemented without Node, network, mempool, votes,
// clocks, randomness, or local configuration.
type BlockExecutionEngine interface {
	ExecuteBlock(BlockExecutionInput) (BlockExecutionResult, error)
}

// DeterministicBlockExecutionEngine executes MSC transfer/stake/update rules
// and delegates native token transitions to NativeDTLExecutor.
type DeterministicBlockExecutionEngine struct{}

var _ BlockExecutionEngine = DeterministicBlockExecutionEngine{}

type rejectedExecutionTransaction struct {
	Transaction Transaction
	Err         error
}

type candidateExecutionResult struct {
	Accepted []Transaction
	Rejected []rejectedExecutionTransaction
	Ledger   Ledger
	Receipts []StateReceipt
	Err      error
}

func executeTransactionWithAuthority(
	authority *BlockExecutionAuthority,
	ledger *Ledger,
	tx Transaction,
	block Block,
) (Ledger, error) {
	if ledger == nil {
		return Ledger{}, fmt.Errorf("execution parent ledger required")
	}
	registry := map[string]ValidatorRecord(nil)
	var updateCtx *validatorUpdateExecutionContext
	if authority != nil {
		registry = authority.ValidatorRegistry
		updateCtx = authority.ValidatorUpdates
		if updateCtx != nil && len(updateCtx.registrySnapshot) > 0 {
			registry = updateCtx.registrySnapshot
		}
	}
	if authority != nil && authority.Registry != nil && (tx.Type == TxStake || tx.Type == TxValidatorUpdate) {
		if authority.Registry.Height() != 0 && authority.Registry.Height() != block.ID {
			return Ledger{}, fmt.Errorf("validator registry height mismatch")
		}
		if block.ValidatorSetVersion > 0 && authority.Registry.Version() != block.ValidatorSetVersion {
			return Ledger{}, fmt.Errorf("validator registry version mismatch")
		}
		if expected := strings.TrimSpace(block.ValidatorRegistryHash); expected != "" &&
			!strings.EqualFold(expected, strings.TrimSpace(authority.Registry.Hash())) {
			return Ledger{}, fmt.Errorf("validator registry commitment mismatch")
		}
	}
	if tx.Type == TxStake && len(registry) == 0 {
		return Ledger{}, fmt.Errorf("validator registry snapshot unavailable for stake execution")
	}
	if tx.Type == TxValidatorUpdate && updateCtx == nil {
		return Ledger{}, fmt.Errorf("validator updates disabled")
	}
	next, err := executeTransactionWithValidatorRegistryProtocol(
		ledger,
		tx,
		int(block.ID),
		registry,
		block.ProtocolVersion,
		block.FeatureBitmap,
	)
	if err != nil {
		return Ledger{}, err
	}
	if tx.Type == TxValidatorUpdate {
		if err := updateCtx.validateAndApply(tx, &next); err != nil {
			return Ledger{}, err
		}
	}
	return next, nil
}

func stateReceiptForExecutedTransaction(
	tx Transaction,
	preStateHash string,
	next Ledger,
	preDTLLogCount int,
	txIndex int,
	height uint64,
) (StateReceipt, error) {
	meta, metaOK := deriveDTLReceiptMetadata(tx, &next)
	if transactionHasDTLEnvelope(tx) && !metaOK {
		return StateReceipt{}, fmt.Errorf("dtl receipt metadata unavailable: %s", tx.ID)
	}
	logs := buildReceiptDTLLogs(next, preDTLLogCount, tx.ID, txIndex, height)
	usage := dtlResourceUsageZero()
	if transactionHasDTLEnvelope(tx) {
		var err error
		usage, err = deterministicDTLResourceUsage(tx, logs, true)
		if err != nil {
			return StateReceipt{}, err
		}
	}
	return StateReceipt{
		TxHash:             tx.ID,
		PreStateHash:       preStateHash,
		PostStateHash:      HashLedger(next),
		DTLTxType:          meta.DTLTxType,
		ContractID:         meta.ContractID,
		ContractStandard:   meta.ContractStandard,
		ContractInterfaces: append([]string(nil), meta.ContractInterfaces...),
		ABIHash:            meta.ABIHash,
		Upgradeable:        meta.Upgradeable,
		ProxyTarget:        meta.ProxyTarget,
		OracleFeedID:       meta.OracleFeedID,
		HealthFactor:       meta.HealthFactor,
		RouteHops:          meta.RouteHops,
		RouteTokenIn:       meta.RouteTokenIn,
		RouteTokenOut:      meta.RouteTokenOut,
		Logs:               logs,
		DTLReads:           usage.Reads,
		DTLWrites:          usage.Writes,
		DTLEvents:          usage.Events,
		DTLSteps:           usage.Steps,
		DTLStorageBytes:    usage.StorageBytes,
		DTLResourceFee:     usage.Fee,
	}, nil
}

func executeCandidateTransactions(input BlockExecutionInput) candidateExecutionResult {
	parent, err := decodeExecutionLedger(input.Context.Reader)
	if err != nil {
		return candidateExecutionResult{Err: err}
	}
	ledger := parent.Clone()
	authority := input.Authority.clone()
	accepted := make([]Transaction, 0, len(input.Block.Transactions))
	rejected := make([]rejectedExecutionTransaction, 0)
	receipts := make([]StateReceipt, 0, len(input.Block.Transactions))
	for _, tx := range input.Block.Transactions {
		preStateHash := HashLedger(ledger)
		preDTLLogCount := 0
		if ledger.DTL != nil {
			preDTLLogCount = len(ledger.DTL.EventLogs)
		}
		next, err := executeTransactionWithAuthority(&authority, &ledger, tx, input.Block)
		if err != nil {
			rejected = append(rejected, rejectedExecutionTransaction{Transaction: tx, Err: err})
			continue
		}
		receipt, err := stateReceiptForExecutedTransaction(
			tx,
			preStateHash,
			next,
			preDTLLogCount,
			len(accepted),
			input.Block.ID,
		)
		if err != nil {
			rejected = append(rejected, rejectedExecutionTransaction{Transaction: tx, Err: err})
			continue
		}
		ledger = next
		accepted = append(accepted, tx)
		receipts = append(receipts, receipt)
	}
	return candidateExecutionResult{
		Accepted: accepted,
		Rejected: rejected,
		Ledger:   ledger,
		Receipts: receipts,
	}
}

// ComputeExecutionFeeRoot commits the ordered coin/fee effects without making
// execution fee accounting visible to leader or quorum logic.
func ComputeExecutionFeeRoot(txs []Transaction) string {
	if len(txs) == 0 {
		return ""
	}
	leaves := make([]string, 0, len(txs))
	for i, tx := range txs {
		material := fmt.Sprintf("%d|%s|%s|%d", i, strings.TrimSpace(tx.ID), normalizeCoin(tx.Coin), tx.Fee)
		sum := sha256.Sum256([]byte(material))
		leaves = append(leaves, hex.EncodeToString(sum[:]))
	}
	return merkleRootFromHexLeaves(leaves)
}

// ComputeExecutionEventRoot commits ordered DTL event logs independently from
// receipts. Empty event sets deliberately use the legacy empty-root encoding.
func ComputeExecutionEventRoot(receipts []StateReceipt) string {
	leaves := make([]string, 0)
	for receiptIndex, receipt := range receipts {
		for logIndex, event := range receipt.Logs {
			material := fmt.Sprintf(
				"%d|%d|%s|%s|%s|%s|%d|%d|%d",
				receiptIndex,
				logIndex,
				strings.TrimSpace(receipt.TxHash),
				strings.TrimSpace(event.ContractID),
				strings.Join(event.Topics, "\x1f"),
				strings.TrimSpace(event.Data),
				event.BlockHeight,
				event.TxIndex,
				event.LogIndex,
			)
			sum := sha256.Sum256([]byte(material))
			leaves = append(leaves, hex.EncodeToString(sum[:]))
		}
	}
	return merkleRootFromHexLeaves(leaves)
}

// ComputeDTLReceiptsRoot commits only native-DTL receipts. This root is kept
// separate so consensus can verify DTL output without reading DTL state.
func ComputeDTLReceiptsRoot(receipts []StateReceipt) string {
	leaves := make([]string, 0)
	for index, receipt := range receipts {
		if strings.TrimSpace(receipt.DTLTxType) == "" {
			continue
		}
		material := fmt.Sprintf(
			"%d|%s|%s|%s|%s|%s|%s|%d|%d|%d|%d|%d|%d|%d",
			index,
			strings.TrimSpace(receipt.TxHash),
			strings.TrimSpace(receipt.PreStateHash),
			strings.TrimSpace(receipt.PostStateHash),
			strings.TrimSpace(receipt.DTLTxType),
			strings.TrimSpace(receipt.ContractID),
			strings.TrimSpace(receipt.ABIHash),
			len(receipt.Logs),
			receipt.DTLReads,
			receipt.DTLWrites,
			receipt.DTLEvents,
			receipt.DTLSteps,
			receipt.DTLStorageBytes,
			receipt.DTLResourceFee,
		)
		sum := sha256.Sum256([]byte(material))
		leaves = append(leaves, hex.EncodeToString(sum[:]))
	}
	return merkleRootFromHexLeaves(leaves)
}

// committedExecutionRoot gives empty ordered sets an explicit domain-separated
// commitment without changing legacy helper semantics for historical blocks.
func committedExecutionRoot(domain string, root string) string {
	if root = strings.TrimSpace(root); root != "" {
		return root
	}
	sum := sha256.Sum256([]byte("MSC_EXECUTION_EMPTY_V1\x00" + strings.TrimSpace(domain)))
	return hex.EncodeToString(sum[:])
}

func (DeterministicBlockExecutionEngine) ExecuteBlock(input BlockExecutionInput) (BlockExecutionResult, error) {
	if input.Block.ID == 0 {
		return BlockExecutionResult{}, fmt.Errorf("execution block height required")
	}
	candidates := executeCandidateTransactions(input)
	if candidates.Err != nil {
		return BlockExecutionResult{}, candidates.Err
	}
	if len(candidates.Rejected) > 0 {
		first := candidates.Rejected[0]
		return BlockExecutionResult{}, fmt.Errorf("transaction %s rejected: %w", first.Transaction.ID, first.Err)
	}
	if len(candidates.Accepted) != len(input.Block.Transactions) {
		return BlockExecutionResult{}, fmt.Errorf("execution transaction count mismatch")
	}
	parent, err := decodeExecutionLedger(input.Context.Reader)
	if err != nil {
		return BlockExecutionResult{}, err
	}
	if _, err := validateSupplyTransition(input.Block.ID, &parent, &candidates.Ledger, SupplyChange{}); err != nil {
		return BlockExecutionResult{}, err
	}
	published, err := publishExecutionLedger(input.Context, candidates.Ledger)
	if err != nil {
		return BlockExecutionResult{}, err
	}
	candidates.Ledger = published
	rootVersion := executionStateRootVersionForHeight(input.Block.ID)
	executionHash := HashLedger(candidates.Ledger)
	commitments := ExecutionCommitments{
		StateRoot:       ComputeExecHashVersioned(input.Block, executionHash, rootVersion),
		ReceiptsRoot:    committedExecutionRoot("receipts", ComputeReceiptRoot(candidates.Receipts)),
		EventRoot:       committedExecutionRoot("events", ComputeExecutionEventRoot(candidates.Receipts)),
		ExecutionHash:   executionHash,
		FeeRoot:         committedExecutionRoot("fees", ComputeExecutionFeeRoot(candidates.Accepted)),
		DTLStateRoot:    hashNativeDTLState(nativeDTLStateFromLedger(candidates.Ledger)),
		DTLReceiptsRoot: committedExecutionRoot("dtl_receipts", ComputeDTLReceiptsRoot(candidates.Receipts)),
	}
	return BlockExecutionResult{
		NextLedger:  candidates.Ledger.Clone(),
		Receipts:    append([]StateReceipt(nil), candidates.Receipts...),
		Commitments: commitments,
	}, nil
}

func executionReceiptsEqual(expected []StateReceipt, actual []StateReceipt, protocolVersion ...uint32) bool {
	if len(expected) != len(actual) {
		return false
	}
	compareDTLResources := len(protocolVersion) > 0 && protocolVersion[0] > 0
	for i := range expected {
		left := expected[i]
		right := actual[i]
		if strings.TrimSpace(left.TxHash) != strings.TrimSpace(right.TxHash) ||
			!strings.EqualFold(strings.TrimSpace(left.PreStateHash), strings.TrimSpace(right.PreStateHash)) ||
			!strings.EqualFold(strings.TrimSpace(left.PostStateHash), strings.TrimSpace(right.PostStateHash)) ||
			strings.TrimSpace(left.DTLTxType) != strings.TrimSpace(right.DTLTxType) ||
			strings.TrimSpace(left.ContractID) != strings.TrimSpace(right.ContractID) ||
			strings.TrimSpace(left.RuntimeMode) != strings.TrimSpace(right.RuntimeMode) ||
			strings.TrimSpace(left.ContractStandard) != strings.TrimSpace(right.ContractStandard) ||
			!dtlContractInterfacesEqual(left.ContractInterfaces, right.ContractInterfaces) ||
			!strings.EqualFold(strings.TrimSpace(left.ABIHash), strings.TrimSpace(right.ABIHash)) ||
			left.Upgradeable != right.Upgradeable ||
			strings.TrimSpace(left.ProxyTarget) != strings.TrimSpace(right.ProxyTarget) ||
			strings.TrimSpace(left.OracleFeedID) != strings.TrimSpace(right.OracleFeedID) ||
			left.HealthFactor != right.HealthFactor ||
			left.RouteHops != right.RouteHops ||
			strings.TrimSpace(left.RouteTokenIn) != strings.TrimSpace(right.RouteTokenIn) ||
			strings.TrimSpace(left.RouteTokenOut) != strings.TrimSpace(right.RouteTokenOut) ||
			strings.TrimSpace(left.BytecodeFormat) != strings.TrimSpace(right.BytecodeFormat) ||
			!strings.EqualFold(strings.TrimSpace(left.BytecodeHash), strings.TrimSpace(right.BytecodeHash)) ||
			left.BytecodeSize != right.BytecodeSize ||
			strings.TrimSpace(left.Compiler) != strings.TrimSpace(right.Compiler) ||
			!strings.EqualFold(strings.TrimSpace(left.SourceHash), strings.TrimSpace(right.SourceHash)) ||
			!dtlEventLogsEqual(left.Logs, right.Logs) {
			return false
		}
		if compareDTLResources && (left.DTLReads != right.DTLReads ||
			left.DTLWrites != right.DTLWrites ||
			left.DTLEvents != right.DTLEvents ||
			left.DTLSteps != right.DTLSteps ||
			left.DTLStorageBytes != right.DTLStorageBytes ||
			left.DTLResourceFee != right.DTLResourceFee) {
			return false
		}
	}
	return true
}
