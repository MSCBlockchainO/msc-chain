package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

func (n *Node) HandleFraud(block Block) {
	proposer := block.Proposer

	// Slash proposer
	n.SlashValidator(proposer)

	// Rollback block
	n.Blockchain.Revert(block.ID)

	fmt.Println("ðŸš¨ FRAUD DETECTED | Proposer slashed:", proposer)
}
func (bc *Blockchain) Revert(height uint64) {

	bc.mu.Lock()
	defer bc.mu.Unlock()

	if len(bc.Blocks) == 0 {
		return
	}

	// Only revert tip
	last := bc.Blocks[len(bc.Blocks)-1]
	if last.ID != height {
		return
	}

	bc.Blocks = bc.Blocks[:len(bc.Blocks)-1]

	if DebugConsensus {
		newHeight := uint64(0)
		if len(bc.Blocks) > 0 {
			newHeight = bc.Blocks[len(bc.Blocks)-1].ID
		}
		fmt.Printf("â†©ï¸ Blockchain reverted to height %d\n", newHeight)
	}
}

func (n *Node) SlashValidator(addr string) {
	n.slashValidatorForReason(addr, "invalid_block", 0)
}

func (n *Node) slashValidatorForReason(addr string, reason string, evidenceHeight uint64) {
	height := uint64(0)
	if n != nil && n.Blockchain != nil {
		height = n.Blockchain.Height()
	}
	if evidenceHeight > 0 {
		height = evidenceHeight
	}
	reason = canonicalMisbehaviorReason(reason)
	if reason == "" {
		reason = "invalid_block"
	}

	// =============================
	// 1ï¸âƒ£ Remove validator locally
	// =============================
	n.validatorMu.Lock()
	delete(n.validatorStatus, addr)
	n.validatorMu.Unlock()

	// =============================
	// 2ï¸âƒ£ Participation penalty
	// =============================
	participationMu.Lock()
	if p, ok := Participation[addr]; ok && n != nil && n.Blockchain != nil {
		p.InvalidBlocks++
		p.CooldownUntil = uint64(n.Blockchain.Height() + 10) // ðŸš« 10 blocks ban
		p.CooldownUntil = height + 10
		p.LastSeen = time.Now()
		p.Reputation -= 20
		if p.Reputation < 0 {
			p.Reputation = 0
		}
	}
	participationMu.Unlock()

	// =============================
	// 3ï¸âƒ£ Leader / proposer cooldown
	// =============================
	ValidatorCooldown[addr] = int(height) + 10
	ApplyValidatorPenalty(addr, reason, height)

	// =============================
	// 4ï¸âƒ£ Burn a portion of delegated stake
	// =============================
	burnedTotal := 0
	targetID := strings.TrimSpace(strings.ToUpper(addr))
	if targetID != "" && SlashStakeBurnBPS > 0 {
		burnedTotal = n.burnValidatorStakeByBPS(targetID, SlashStakeBurnBPS)
	}
	if burnedTotal > 0 {
		ApplyValidatorStake(addr, -int64(burnedTotal), height)
	}
	confiscatedCoins := int64(0)
	confiscatedBurn := int64(0)
	if targetID != "" {
		confiscatedCoins, confiscatedBurn = n.forfeitSlashedValidatorBalance(targetID)
	}

	if DebugConsensus {
		fmt.Printf(
			"ðŸ©¸ Validator slashed | %s banned until height %d | burned_stake=%d | confiscated=%d | burned_confiscated=%d\n",
			ShortID(addr),
			height+10,
			burnedTotal,
			confiscatedCoins,
			confiscatedBurn,
		)
	}
}

func (n *Node) forfeitSlashedValidatorBalance(validatorID string) (int64, int64) {
	if n == nil {
		return 0, 0
	}
	targetID := normalizeValidatorID(validatorID)
	if targetID == "" {
		return 0, 0
	}

	ledger := n.currentExecutionLedgerClone()
	holder := canonicalAddressKey(resolveValidatorRecipient(&ledger, targetID))
	if holder == "" {
		return 0, 0
	}

	bal := int64(getBalance(ledger, CoinSymbol, holder))
	if bal <= 0 {
		return 0, 0
	}

	setBalance(&ledger, CoinSymbol, holder, 0)
	addBalance(&ledger, CoinSymbol, TREASURY_ADDRESS, int(bal))
	burned := burnCoinsFromAddress(&ledger, CoinSymbol, TREASURY_ADDRESS, bal)
	n.setExecutionLedger(ledger)
	return bal, burned
}

func (n *Node) TryAlternativeChain(block Block) {

	fmt.Printf("ðŸ”„ Evaluating alternative chain at height %d\n", block.ID)

	// =====================================================
	// 1ï¸âƒ£ CHECK QUEUED FORK BLOCKS (SAME HEIGHT)
	// =====================================================
	n.forkMu.RLock()
	candidates, exists := n.ForkBlocks[block.ID]
	if exists {
		candidates = append([]Block(nil), candidates...)
	}
	n.forkMu.RUnlock()
	if exists {

		for _, alt := range candidates {

			// Must connect to our current chain
			if alt.PrevHash != n.Blockchain.LastBlock().BlockHash {
				continue
			}

			// ðŸ”’ EXECUTION VERIFICATION (MODEL-3)
			if err := n.VerifyBlock(alt, n.Blockchain); err != nil {

				fmt.Println("ðŸš¨ Fork block failed execution check")
				n.HandleFraud(alt)
				continue
			}

			fmt.Println("âœ… Switching to execution-valid fork")
			_ = n.ReceiveBlock(alt, n.Blockchain)
			return
		}
	}

	// =====================================================
	// 2ï¸âƒ£ NO VALID LOCAL FORK â†’ REQUEST REMOTE CHAIN
	// =====================================================
	start := int(maxUint64(0, block.ID-50))

	fmt.Printf(
		"ðŸ“¡ Requesting chain sync [%d â†’ %d]\n",
		start,
		block.ID,
	)

	n.RequestBlocks(start, int(block.ID))
}
func (n *Node) RequestBlocks(from, to int) {

	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	for _, pid := range n.Host.Network().Peers() {

		stream, err := n.Host.NewStream(
			ctx,
			pid,
			BlockSyncProtocol,
		)
		if err != nil {
			continue
		}

		enc := json.NewEncoder(stream)
		dec := json.NewDecoder(stream)

		req := BlockRequest{
			From:         uint64(from),
			To:           uint64(to),
			WantSnapshot: true,
		}

		// =================================================
		// SEND REQUEST
		// =================================================
		if err := enc.Encode(req); err != nil {
			stream.Close()
			continue
		}

		var resp BlockResponse
		if err := dec.Decode(&resp); err != nil {
			stream.Close()
			continue
		}

		if resp.Snapshot != nil && resp.Snapshot.Height > 0 {
			n.ApplySnapshotForSync(*resp.Snapshot)
		}
		if resp.ExecPool != nil {
			n.mergeExecPoolSnapshot(*resp.ExecPool)
		}

		// =================================================
		// VERIFY EACH BLOCK (MODEL-3)
		// =================================================
		var lastApplied uint64
		for _, b := range resp.Blocks {
			if err := n.VerifyBlock(b, n.Blockchain); err != nil {

				fmt.Println("ðŸš¨ Received invalid block during sync")
				n.HandleFraud(b)
				continue
			}

			_ = n.ReceiveBlock(b, n.Blockchain)
			lastApplied = b.ID
		}
		if lastApplied > 0 {
			n.sendBlockAck(pid, lastApplied)
		}

		stream.Close()
		return // success from first honest peer
	}
}

func HashResult(result int) []byte {
	h := sha256.Sum256(
		[]byte(strconv.Itoa(result)),
	)
	return h[:]
}

func VerifyTx(task Task, receipt Receipt) bool {

	expected := ExecuteTask(task)
	if expected != receipt.Output {
		return false
	}

	regenerated := GenerateReceipt(task, receipt.Output)
	return regenerated.Hash == receipt.Hash
}
func NewBlockchain() Blockchain {
	genesis := Block{
		ID:        0,
		Type:      BlockTypeGenesis, // ðŸ”¥ FIX
		BlockHash: GenesisHash,
		Timestamp: 0,
		BlockTime: LogicalTimeForEpoch(0),
	}

	return Blockchain{
		Blocks: []Block{genesis},
	}
}
func GenerateReceipt(task Task, output int) Receipt {

	data := fmt.Sprintf("%s:%d:%d", task.TaskID, task.Input, output)
	hash := sha256.Sum256([]byte(data))

	return Receipt{
		TaskID: task.TaskID,
		input:  task.Input,
		Output: output,
		Hash:   hex.EncodeToString(hash[:]),
	}
}

func FinalizeBlock(
	id uint64,
	blockType BlockType, // ðŸ”¥ FIX
	task Task,
	result int,
	prevHash string,
	proposer string,
) Block {
	lt := LogicalTimeForEpoch(id)
	block := Block{
		ID:         id,
		Type:       blockType,
		Task:       task,
		Result:     result,
		ResultHash: HashResult(result),
		PrevHash:   prevHash,
		Proposer:   proposer,
		Timestamp:  int64(SystemTimeUnits(lt)),
		BlockTime:  lt,
	}

	block.BlockHash = HashBlock(block)
	return block
}

func BuildRandomSeed(
	prevBlockHash string,
	stateRoot string,
	mempoolHash string,
) []byte {

	data := prevBlockHash + stateRoot + mempoolHash
	hash := sha256.Sum256([]byte(data))
	return hash[:]
}

func SelectValidator(
	seed []byte,
	candidates []ValidatorCandidate,
) ValidatorCandidate {

	var winner ValidatorCandidate
	bestScore := math.MaxFloat64

	for _, c := range candidates {

		vrfOut := sha256.Sum256(
			append(seed, c.PubKey...),
		)

		raw := binary.BigEndian.Uint64(vrfOut[:8])

		// ðŸ”¥ Reputation-weighted randomness
		rep := float64(c.Reputation + 1)
		score := float64(raw) / rep

		if score < bestScore {
			bestScore = score
			winner = c
		}
	}

	return winner
}

func TryFinalizeBlock(
	n *Node,
	block Block,
) bool {

	// 1ï¸âƒ£ Re-execute task locally
	localResult := ExecuteTask(block.Task)
	localHash := HashResult(localResult)

	// 2ï¸âƒ£ Execution mismatch = fraud
	if !bytes.Equal(localHash, block.ResultHash) {
		fmt.Println("ðŸš¨ EXECUTION MISMATCH â€” FRAUD DETECTED")
		n.HandleFraud(block)
		return false
	}

	// 3ï¸âƒ£ Hash integrity check
	expectedHash := HashBlock(block)
	if expectedHash != block.BlockHash {
		fmt.Println("ðŸš¨ BLOCK HASH MISMATCH")
		n.HandleFraud(block)
		return false
	}

	// 4ï¸âƒ£ Finalize locally (deterministic)
	n.Blockchain.AddBlock(block)

	fmt.Println("âœ… BLOCK FINALIZED (EXECUTION CONSENSUS) @ HEIGHT", block.ID)
	return true
}
func (bc *Blockchain) AddBlock(block Block) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.Blocks = append(bc.Blocks, block)
}

func (n *Node) ProduceTaskBlock(task Task) Block {
	result := ExecuteTask(task)
	resultHash := HashResult(result)
	epoch := n.Blockchain.Height() + 1
	lt := LogicalTimeForEpoch(epoch)
	validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))

	block := Block{
		ID:        epoch,
		PrevHash:  n.Blockchain.LastBlock().BlockHash,
		Timestamp: int64(SystemTimeUnits(lt)),
		BlockTime: lt,
		Type:      BlockTypeTask,

		Proposer:   n.ValidatorKey.ID,
		Task:       task,
		Result:     result,
		ResultHash: resultHash,
	}
	block.ValidatorSetHash = n.validatorSetHashFromFinalizedSnapshot(epoch, validators)
	n.fillBlockNextValidatorSetCommitment(&block)

	// ðŸ”’ Bind everything cryptographically
	block.BlockHash = HashBlock(block)

	// ðŸ”‘ Proposer signature (identity only, NOT voting)
	n.SignBlock(&block)

	return block
}
func VerifyBlockSignature(block Block) bool {
	if len(block.Signature) != ed25519.SignatureSize {
		return false
	}

	proposerID := normalizeValidatorID(block.Proposer)
	// 1ï¸âƒ£ Build candidate pubkey list.
	// Runtime map can be updated by peer-hello overrides; keep genesis fallback
	// so historical blocks remain verifiable after key changes.
	candidates := make([]ed25519.PublicKey, 0, 4)
	addCandidate := func(pk ed25519.PublicKey) {
		if len(pk) != ed25519.PublicKeySize {
			return
		}
		for _, existing := range candidates {
			if bytes.Equal(existing, pk) {
				return
			}
		}
		copied := make([]byte, len(pk))
		copy(copied, pk)
		candidates = append(candidates, ed25519.PublicKey(copied))
	}
	validatorPubKeysMu.RLock()
	runtimeNorm, runtimeNormOK := ValidatorPubKeys[proposerID]
	runtimeRaw, runtimeRawOK := ValidatorPubKeys[block.Proposer]
	genNorm, genNormOK := GenesisValidatorPubKeys[proposerID]
	genRaw, genRawOK := GenesisValidatorPubKeys[block.Proposer]
	addCandidate(runtimeNorm)
	addCandidate(runtimeRaw)
	addCandidate(genNorm)
	addCandidate(genRaw)
	validatorPubKeysMu.RUnlock()
	if len(candidates) == 0 {
		if DebugConsensus {
			fmt.Println("Unknown proposer public key:", block.Proposer)
		}
		return false
	}

	// 2ï¸âƒ£ Recompute canonical hashes.
	// Proposal signatures are created at TickExec. Finalized blocks can carry
	// the same signature while moving to TickFinalize, so we accept either hash.
	hashes := []string{HashBlock(block)}
	if block.BlockTime.Tick == TickFinalize && block.BlockTime.Epoch > 0 {
		proposal := block
		proposal.BlockTime = LogicalTimeForEpochTick(block.BlockTime.Epoch, TickExec)
		proposal.Timestamp = int64(SystemTimeUnits(proposal.BlockTime))
		clearFinalityCommitments(&proposal)
		proposalHash := HashBlock(proposal)
		if proposalHash != hashes[0] {
			hashes = append(hashes, proposalHash)
		}
	}

	// Backward-compatible signing payloads:
	// legacy builds may sign raw hash bytes; current builds sign hash string bytes.
	payloads := make([][]byte, 0, len(hashes)*2)
	seenPayload := make(map[string]struct{}, len(hashes)*2)
	addPayload := func(msg []byte) {
		if len(msg) == 0 {
			return
		}
		key := string(msg)
		if _, ok := seenPayload[key]; ok {
			return
		}
		seenPayload[key] = struct{}{}
		cp := make([]byte, len(msg))
		copy(cp, msg)
		payloads = append(payloads, cp)
	}
	for _, hash := range hashes {
		addPayload([]byte(hash))
		if raw, err := hex.DecodeString(hash); err == nil && len(raw) > 0 {
			addPayload(raw)
		}
	}

	// 3ï¸âƒ£ Verify ed25519 signature against accepted payloads and key candidates.
	for _, pub := range candidates {
		for _, msg := range payloads {
			if ed25519.Verify(pub, msg, block.Signature) {
				return true
			}
		}
	}
	if DebugConsensus {
		fmt.Printf("âŒ Block signature verification failed | proposer=%s height=%d hash=%s candidates=%d runtime_norm=%t runtime_raw=%t genesis_norm=%t genesis_raw=%t\n",
			ShortID(proposerID), block.ID, ShortHash(block.BlockHash), len(candidates),
			runtimeNormOK, runtimeRawOK, genNormOK, genRawOK)
	}
	return false
}

func verifyBlockSignatureWithCandidates(block Block, candidates []ed25519.PublicKey) bool {
	if len(block.Signature) != ed25519.SignatureSize {
		return false
	}
	if len(candidates) == 0 {
		return false
	}
	uniq := make([]ed25519.PublicKey, 0, len(candidates))
	addCandidate := func(pk ed25519.PublicKey) {
		if len(pk) != ed25519.PublicKeySize {
			return
		}
		for _, existing := range uniq {
			if bytes.Equal(existing, pk) {
				return
			}
		}
		copied := make([]byte, len(pk))
		copy(copied, pk)
		uniq = append(uniq, ed25519.PublicKey(copied))
	}
	for _, pk := range candidates {
		addCandidate(pk)
	}
	if len(uniq) == 0 {
		return false
	}

	hashes := []string{HashBlock(block)}
	if block.BlockTime.Tick == TickFinalize && block.BlockTime.Epoch > 0 {
		proposal := block
		proposal.BlockTime = LogicalTimeForEpochTick(block.BlockTime.Epoch, TickExec)
		proposal.Timestamp = int64(SystemTimeUnits(proposal.BlockTime))
		clearFinalityCommitments(&proposal)
		proposalHash := HashBlock(proposal)
		if proposalHash != hashes[0] {
			hashes = append(hashes, proposalHash)
		}
	}

	payloads := make([][]byte, 0, len(hashes)*2)
	seenPayload := make(map[string]struct{}, len(hashes)*2)
	addPayload := func(msg []byte) {
		if len(msg) == 0 {
			return
		}
		key := string(msg)
		if _, ok := seenPayload[key]; ok {
			return
		}
		seenPayload[key] = struct{}{}
		cp := make([]byte, len(msg))
		copy(cp, msg)
		payloads = append(payloads, cp)
	}
	for _, hash := range hashes {
		addPayload([]byte(hash))
		if raw, err := hex.DecodeString(hash); err == nil && len(raw) > 0 {
			addPayload(raw)
		}
	}

	for _, pub := range uniq {
		for _, msg := range payloads {
			if ed25519.Verify(pub, msg, block.Signature) {
				return true
			}
		}
	}
	return false
}

func HashBlock(block Block) string {

	txIDs := make([]string, 0, len(block.Transactions))
	for _, tx := range block.Transactions {
		txIDs = append(txIDs, tx.ID)
	}
	sort.Strings(txIDs)

	sysTime := SystemTimeUnits(block.BlockTime)
	quorumPolicyData := ""
	if strings.TrimSpace(block.QuorumPolicyVersion) != "" ||
		strings.TrimSpace(block.ConsensusMode) != "" ||
		block.ActiveReadyCount > 0 ||
		block.RequiredQuorum > 0 ||
		block.StrictQuorum > 0 {
		quorumPolicyData = fmt.Sprintf(
			"|q=%s|mode=%s|ready=%d|required=%d|strict=%d",
			strings.TrimSpace(block.QuorumPolicyVersion),
			strings.TrimSpace(block.ConsensusMode),
			block.ActiveReadyCount,
			block.RequiredQuorum,
			block.StrictQuorum,
		)
	}
	if finalityData := blockFinalityHashData(block); finalityData != "" {
		quorumPolicyData += finalityData
	}
	if promotionData := blockPromotionWindowHashData(block); promotionData != "" {
		quorumPolicyData += promotionData
	}
	data := ""
	if validatorSetCommitmentV2EnabledAt(block.ID) {
		activationHeight := canonicalActivationHeight(block.NextValidatorSetHeight, block.ActivationHeight)
		registryHash := strings.TrimSpace(block.ValidatorRegistryHash)
		validatorSetRoot := strings.TrimSpace(block.ValidatorSetRoot)
		nextValidatorSetRoot := strings.TrimSpace(block.NextValidatorSetRoot)
		if registryHash != "" {
			if validatorSetRoot != "" {
				if nextValidatorSetRoot != "" {
					data = fmt.Sprintf(
						"%d|%s|%s|%d|%s|%s|%s|%s|%s|%s|%d|%s|%s|%s|%x%s",
						block.ID,
						block.PrevHash,
						strings.Join(txIDs, ","),
						sysTime,
						block.Proposer,
						block.Type,
						block.StateRoot,
						block.MempoolRoot,
						block.ValidatorSetHash,
						validatorSetRoot,
						activationHeight,
						strings.TrimSpace(block.NextValidatorSetHash),
						nextValidatorSetRoot,
						registryHash,
						block.ResultHash,
						quorumPolicyData,
					)
				} else {
					data = fmt.Sprintf(
						"%d|%s|%s|%d|%s|%s|%s|%s|%s|%s|%d|%s|%s|%x%s",
						block.ID,
						block.PrevHash,
						strings.Join(txIDs, ","),
						sysTime,
						block.Proposer,
						block.Type,
						block.StateRoot,
						block.MempoolRoot,
						block.ValidatorSetHash,
						validatorSetRoot,
						activationHeight,
						strings.TrimSpace(block.NextValidatorSetHash),
						registryHash,
						block.ResultHash,
						quorumPolicyData,
					)
				}
			} else {
				if nextValidatorSetRoot != "" {
					data = fmt.Sprintf(
						"%d|%s|%s|%d|%s|%s|%s|%s|%s|%d|%s|%s|%s|%x%s",
						block.ID,
						block.PrevHash,
						strings.Join(txIDs, ","),
						sysTime,
						block.Proposer,
						block.Type,
						block.StateRoot,
						block.MempoolRoot,
						block.ValidatorSetHash,
						activationHeight,
						strings.TrimSpace(block.NextValidatorSetHash),
						nextValidatorSetRoot,
						registryHash,
						block.ResultHash,
						quorumPolicyData,
					)
				} else {
					data = fmt.Sprintf(
						"%d|%s|%s|%d|%s|%s|%s|%s|%s|%d|%s|%s|%x%s",
						block.ID,
						block.PrevHash,
						strings.Join(txIDs, ","),
						sysTime,
						block.Proposer,
						block.Type,
						block.StateRoot,
						block.MempoolRoot,
						block.ValidatorSetHash,
						activationHeight,
						strings.TrimSpace(block.NextValidatorSetHash),
						registryHash,
						block.ResultHash,
						quorumPolicyData,
					)
				}
			}
		} else {
			// Backward compatibility: blocks produced before registry commitment
			// activation did not include this field in post-fork hash payload.
			if validatorSetRoot != "" {
				if nextValidatorSetRoot != "" {
					data = fmt.Sprintf(
						"%d|%s|%s|%d|%s|%s|%s|%s|%s|%s|%d|%s|%s|%x%s",
						block.ID,
						block.PrevHash,
						strings.Join(txIDs, ","),
						sysTime,
						block.Proposer,
						block.Type,
						block.StateRoot,
						block.MempoolRoot,
						block.ValidatorSetHash,
						validatorSetRoot,
						activationHeight,
						strings.TrimSpace(block.NextValidatorSetHash),
						nextValidatorSetRoot,
						block.ResultHash,
						quorumPolicyData,
					)
				} else {
					data = fmt.Sprintf(
						"%d|%s|%s|%d|%s|%s|%s|%s|%s|%s|%d|%s|%x%s",
						block.ID,
						block.PrevHash,
						strings.Join(txIDs, ","),
						sysTime,
						block.Proposer,
						block.Type,
						block.StateRoot,
						block.MempoolRoot,
						block.ValidatorSetHash,
						validatorSetRoot,
						activationHeight,
						strings.TrimSpace(block.NextValidatorSetHash),
						block.ResultHash,
						quorumPolicyData,
					)
				}
			} else {
				if nextValidatorSetRoot != "" {
					data = fmt.Sprintf(
						"%d|%s|%s|%d|%s|%s|%s|%s|%s|%d|%s|%s|%x%s",
						block.ID,
						block.PrevHash,
						strings.Join(txIDs, ","),
						sysTime,
						block.Proposer,
						block.Type,
						block.StateRoot,
						block.MempoolRoot,
						block.ValidatorSetHash,
						activationHeight,
						strings.TrimSpace(block.NextValidatorSetHash),
						nextValidatorSetRoot,
						block.ResultHash,
						quorumPolicyData,
					)
				} else {
					data = fmt.Sprintf(
						"%d|%s|%s|%d|%s|%s|%s|%s|%s|%d|%s|%x%s",
						block.ID,
						block.PrevHash,
						strings.Join(txIDs, ","),
						sysTime,
						block.Proposer,
						block.Type,
						block.StateRoot,
						block.MempoolRoot,
						block.ValidatorSetHash,
						activationHeight,
						strings.TrimSpace(block.NextValidatorSetHash),
						block.ResultHash,
						quorumPolicyData,
					)
				}
			}
		}
	} else {
		data = fmt.Sprintf(
			"%d|%s|%s|%d|%s|%s|%s|%s|%s|%s|%x%s",
			block.ID,
			block.PrevHash,
			strings.Join(txIDs, ","),
			sysTime,
			block.Proposer,
			block.Type,
			block.StateRoot,
			block.MempoolRoot,
			block.ValidatorSetHash,
			block.Task.TaskID,
			block.ResultHash,
			quorumPolicyData,
		)
	}

	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// SystemTimeUnits converts logical time into deterministic system time units.
func SystemTimeUnits(t LogicalClock) uint64 {
	return t.Epoch*GlobalConfig.TicksPerEpoch + t.Tick
}

// LogicalTimeForEpoch creates canonical logical time for a new epoch.
func LogicalTimeForEpoch(epoch uint64) LogicalClock {
	if epoch == 0 {
		return LogicalClock{Epoch: 0, Tick: 0}
	}
	return LogicalClock{Epoch: epoch, Tick: TickExec}
}

// LogicalTimeForEpochTick creates logical time for a specific tick within an epoch.
func LogicalTimeForEpochTick(epoch uint64, tick uint64) LogicalClock {
	return LogicalClock{Epoch: epoch, Tick: tick}
}
func ExecuteTask(task Task) int {
	return task.Input +
		task.Input*task.Input*task.Input +
		task.Input*task.Input +
		task.Input*task.Input*task.Input*task.Input +
		task.Input*task.Input +
		task.Input*task.Input*task.Input +
		task.Input*task.Input +
		task.Input*task.Input
}

func VerifyExecution(task Task, receipt Receipt) bool {
	expected := ExecuteTask(task)

	if receipt.Output != expected {
		return false
	}

	// Optional: gas / fee / height binding
	if receipt.TaskID != task.TaskID {
		return false
	}

	return true
}
func RewardDistribute(total int) Reward {
	if total <= 0 {
		return Reward{}
	}

	return Reward{
		Worker:     total * 60 / 100,
		User:       total * 10 / 100,
		Owner:      total * 15 / 100,
		Validators: total * 15 / 100,
	}
}
func NewLedger() Ledger {
	return Ledger{
		Balances:                 map[string]int{},
		Nonces:                   make(map[string]int),
		Stakes:                   make(map[string]StakeLock),
		ValidatorRewardWallets:   make(map[string]string),
		EVMState:                 make(map[string]string),
		EVMCode:                  make(map[string]string),
		EVMStorage:               make(map[string]map[string]string),
		DTL:                      NewDTLState(),
		UsedValidatorUpdateCerts: make(map[string]uint64),
	}
}

func stakeKey(addr, validatorID string) string {
	return canonicalAddressKey(addr) + "|" + validatorID
}

func ensureStakeMap(ledger *Ledger) {
	if ledger.Stakes == nil {
		ledger.Stakes = make(map[string]StakeLock)
	}
}

func normalizeRewardValidatorID(validatorID string) string {
	return normalizeValidatorID(validatorID)
}

func ensureRewardWalletMap(ledger *Ledger) {
	if ledger.ValidatorRewardWallets == nil {
		ledger.ValidatorRewardWallets = make(map[string]string)
	}
}

func pinnedGenesisRewardWallet(validatorID string) (string, bool) {
	_, rewardWallet, ok := trustedGenesisWalletBindingForValidator(validatorID)
	if !ok {
		return "", false
	}
	addr := canonicalAddressKey(rewardWallet)
	if addr == "" {
		return "", false
	}
	return addr, true
}

func setValidatorRewardWallet(ledger *Ledger, validatorID, walletAddr string) {
	if ledger == nil {
		return
	}
	id := normalizeRewardValidatorID(validatorID)
	addr := canonicalAddressKey(walletAddr)
	if id == "" || addr == "" {
		return
	}
	ensureRewardWalletMap(ledger)
	if pinnedAddr, ok := pinnedGenesisRewardWallet(id); ok {
		if !addressesEqual(addr, pinnedAddr) {
			return
		}
		ledger.ValidatorRewardWallets[id] = pinnedAddr
		return
	}
	ledger.ValidatorRewardWallets[id] = addr
}

func validatorStakeTotals(ledger *Ledger, validatorID string) map[string]int {
	totals := make(map[string]int)
	if ledger == nil {
		return totals
	}
	targetID := normalizeRewardValidatorID(validatorID)
	if targetID == "" {
		return totals
	}
	ensureStakeMap(ledger)

	for key, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}

		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		walletAddr := canonicalAddressKey(parts[0])
		if walletAddr == "" {
			continue
		}

		recValidatorID := normalizeRewardValidatorID(rec.ValidatorID)
		if recValidatorID == "" {
			recValidatorID = normalizeRewardValidatorID(parts[1])
		}
		if recValidatorID == "" || recValidatorID != targetID {
			continue
		}
		totals[walletAddr] += rec.Amount
	}

	return totals
}

func pickDeterministicTopStakeWallet(totals map[string]int) (string, bool) {
	if len(totals) == 0 {
		return "", false
	}

	wallets := make([]string, 0, len(totals))
	for addr := range totals {
		wallets = append(wallets, addr)
	}
	sort.Strings(wallets)

	bestAddr := ""
	bestStake := -1
	for _, addr := range wallets {
		stake := totals[addr]
		if stake > bestStake {
			bestStake = stake
			bestAddr = addr
		}
	}
	if bestAddr == "" || bestStake <= 0 {
		return "", false
	}
	return bestAddr, true
}

// refreshValidatorRewardWalletBinding keeps validator->reward-wallet mapping valid.
// If current binding is missing/invalid it deterministically rebinds to the top staked wallet.
func refreshValidatorRewardWalletBinding(ledger *Ledger, validatorID string) {
	if ledger == nil {
		return
	}
	targetID := normalizeRewardValidatorID(validatorID)
	if targetID == "" {
		return
	}
	ensureRewardWalletMap(ledger)
	if pinnedAddr, ok := pinnedGenesisRewardWallet(targetID); ok {
		ledger.ValidatorRewardWallets[targetID] = pinnedAddr
		return
	}

	totals := validatorStakeTotals(ledger, targetID)
	if len(totals) == 0 {
		delete(ledger.ValidatorRewardWallets, targetID)
		return
	}

	if bound := strings.TrimSpace(ledger.ValidatorRewardWallets[targetID]); bound != "" {
		if totals[bound] > 0 {
			return
		}
	}

	if bestAddr, ok := pickDeterministicTopStakeWallet(totals); ok {
		ledger.ValidatorRewardWallets[targetID] = bestAddr
		return
	}
	delete(ledger.ValidatorRewardWallets, targetID)
}

// walletBoundValidator returns a different validator ID already bound to this wallet (if any).
func walletBoundValidator(ledger *Ledger, walletAddr, validatorID string) (string, bool) {
	if ledger == nil || walletAddr == "" {
		return "", false
	}
	targetAddr := canonicalAddressKey(walletAddr)
	if targetAddr == "" {
		return "", false
	}
	ensureStakeMap(ledger)
	for k, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}
		parts := strings.SplitN(k, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if !addressesEqual(parts[0], targetAddr) {
			continue
		}
		vid := strings.TrimSpace(parts[1])
		if vid == "" {
			continue
		}
		if validatorID != "" && strings.EqualFold(vid, validatorID) {
			continue
		}
		return vid, true
	}
	return "", false
}

// validatorRewardWallet resolves a deterministic reward recipient wallet for a validator.
// Selection rule: highest active staked amount; tie-break lexicographically by wallet address.
func validatorRewardWallet(ledger *Ledger, validatorID string) (string, bool) {
	if ledger == nil {
		if addr, ok := genesisRewardWallet(validatorID); ok {
			return addr, true
		}
		return "", false
	}
	targetID := normalizeRewardValidatorID(validatorID)
	if targetID == "" {
		return "", false
	}
	ensureStakeMap(ledger)
	if pinnedAddr, ok := pinnedGenesisRewardWallet(targetID); ok {
		return pinnedAddr, true
	}

	// 1) Explicit binding set by deterministic state transition.
	if bound := strings.TrimSpace(ledger.ValidatorRewardWallets[targetID]); bound != "" {
		totals := validatorStakeTotals(ledger, targetID)
		if len(totals) == 0 || totals[bound] > 0 {
			return bound, true
		}
	}

	// 2) Fallback to deterministic top staker.
	totals := validatorStakeTotals(ledger, targetID)

	if len(totals) == 0 {
		if addr, ok := genesisRewardWallet(targetID); ok {
			return addr, true
		}
		return "", false
	}
	if bestAddr, ok := pickDeterministicTopStakeWallet(totals); ok {
		return bestAddr, true
	}
	if addr, ok := genesisRewardWallet(targetID); ok {
		return addr, true
	}
	return "", false
}

func minUnstakeEpochs() uint64 {
	secondsPerEpoch := GlobalConfig.BlockTime.Seconds()
	if secondsPerEpoch <= 0 {
		secondsPerEpoch = 300
	}
	totalSeconds := float64(MinUnstakeMonths) * float64(DaysPerMonth) * 24 * 3600
	return uint64(math.Ceil(totalSeconds / secondsPerEpoch))
}
func ExecuteTransaction(
	ledger *Ledger,
	tx Transaction,
	height int,
) (Ledger, error) {
	if ledger == nil {
		return Ledger{}, errors.New("ledger is nil")
	}
	if ledger.Balances == nil {
		ledger.Balances = make(map[string]int)
	}
	if ledger.Nonces == nil {
		ledger.Nonces = make(map[string]int)
	}
	ensureStakeMap(ledger)
	ensureEVMStateMap(ledger)
	ensureEVMCodeMap(ledger)
	ensureEVMStorageMap(ledger)
	ensureDTLState(ledger)
	ensureValidatorUpdateCertLedgerState(ledger)

	if tx.Type == TxFaucet {
		if !IsTestnet {
			return *ledger, errors.New("faucet disabled on mainnet")
		}
		if tx.From != USER_REWARD_POOL {
			return *ledger, errors.New("faucet source invalid")
		}
	}
	if tx.Type == TxEVM {
		return *ledger, errors.New("evm/vm removed permanently")
	}

	// =====================================
	// 1ï¸âƒ£ NONCE VALIDATION
	// =====================================
	expectedNonce := getNonce(*ledger, tx.From) + 1
	if tx.Nonce != expectedNonce {
		return *ledger, fmt.Errorf(
			"invalid nonce: got %d, expected %d",
			tx.Nonce,
			expectedNonce,
		)
	}

	// =====================================
	// 2ï¸âƒ£ BALANCE CHECK / APPLY
	// =====================================
	if tx.Type == TxEVM {
		if tx.Amount < 0 {
			return *ledger, errors.New("invalid amount or fee")
		}
		resolvedTx, err := hydrateEVMExecutionCode(ledger, tx)
		if err != nil {
			return *ledger, err
		}
		if err := validateEVMTransaction(resolvedTx); err != nil {
			return *ledger, err
		}
		tx = resolvedTx
	} else if tx.Type == TxDTL {
		if err := validateDTLTransaction(ledger, tx, uint64(height)); err != nil {
			return *ledger, err
		}
	} else if tx.Type == TxValidatorUpdate {
		if err := validatorUpdateEnvelopeBasicError(tx, ledger, uint64(height)); err != nil {
			return *ledger, err
		}
	} else if tx.Amount <= 0 {
		return *ledger, errors.New("invalid amount or fee")
	}

	coin := normalizeCoin(tx.Coin)
	if !AllowedCoins[coin] {
		return *ledger, errors.New("unsupported coin")
	}

	requiredFee := requiredFeeForTxWithLedger(ledger, tx)
	if tx.Type == TxEVM {
		if tx.Fee < requiredFee {
			return *ledger, fmt.Errorf(
				"invalid fee: got %d minimum %d",
				tx.Fee,
				requiredFee,
			)
		}
	} else if tx.Type == TxDTL {
		if err := validateDTLFeeBounds(tx.Fee, requiredFee); err != nil {
			return *ledger, err
		}
	} else if tx.Fee != requiredFee {
		return *ledger, fmt.Errorf(
			"invalid fee: got %d expected %d",
			tx.Fee,
			requiredFee,
		)
	}

	switch tx.Type {
	case TxValidatorUpdate:
		if requiredFee > 0 {
			if getBalance(*ledger, coin, tx.From) < requiredFee {
				return *ledger, errors.New("insufficient balance")
			}
			addBalance(ledger, coin, tx.From, -requiredFee)
		}
		addBalance(ledger, coin, TREASURY_ADDRESS, requiredFee)

	case TxStake:
		validatorID := strings.TrimSpace(tx.To)
		if validatorID == "" {
			return *ledger, errors.New("missing validator id")
		}
		if other, ok := walletBoundValidator(ledger, tx.From, validatorID); ok {
			return *ledger, fmt.Errorf("wallet already bound to validator %s", other)
		}
		lockEpochs := tx.StakeEpochs
		if lockEpochs == 0 {
			lockEpochs = DefaultStakeLockEpochs
		}
		minEpochs := minUnstakeEpochs()
		if lockEpochs < minEpochs {
			return *ledger, fmt.Errorf("stake lock too short: min %d epochs", minEpochs)
		}
		if _, err := validateStakeConsensusPubKey(tx, GlobalValidatorRegistry.Snapshot()); err != nil {
			return *ledger, err
		}
		if err := validateValidatorMinimumStakeAfterTx(ledger, tx); err != nil {
			return *ledger, err
		}
		totalCost := tx.Amount + requiredFee
		if getBalance(*ledger, coin, tx.From) < totalCost {
			return *ledger, errors.New("insufficient balance")
		}

		addBalance(ledger, coin, tx.From, -totalCost)
		addBalance(ledger, coin, TREASURY_ADDRESS, requiredFee)

		lockUntil := uint64(height) + lockEpochs
		key := stakeKey(tx.From, validatorID)
		rec := ledger.Stakes[key]
		rec.ValidatorID = validatorID
		if normalizedPubKey := normalizeConsensusPubKeyHex(tx.ValidatorPubKey); normalizedPubKey != "" {
			rec.ConsensusPubKey = normalizedPubKey
		}
		rec.Amount += tx.Amount
		if lockUntil > rec.LockedUntil {
			rec.LockedUntil = lockUntil
		}
		ledger.Stakes[key] = rec
		setValidatorRewardWallet(ledger, validatorID, tx.From)
		refreshValidatorRewardWalletBinding(ledger, validatorID)

	case TxUnstake:
		validatorID := strings.TrimSpace(tx.To)
		if validatorID == "" {
			return *ledger, errors.New("missing validator id")
		}
		key := stakeKey(tx.From, validatorID)
		rec, ok := ledger.Stakes[key]
		if !ok || rec.Amount < tx.Amount {
			return *ledger, errors.New("insufficient staked balance")
		}
		if uint64(height) < rec.LockedUntil {
			return *ledger, errors.New("stake still locked")
		}
		if tx.Amount <= requiredFee {
			return *ledger, errors.New("unstake amount must exceed fee")
		}

		rec.Amount -= tx.Amount
		if rec.Amount <= 0 {
			delete(ledger.Stakes, key)
		} else {
			ledger.Stakes[key] = rec
		}

		addBalance(ledger, coin, tx.From, tx.Amount-requiredFee)
		addBalance(ledger, coin, TREASURY_ADDRESS, requiredFee)
		refreshValidatorRewardWalletBinding(ledger, validatorID)

	case TxEVM:
		return *ledger, errors.New("evm/vm removed permanently")

	case TxDTL:
		if getBalance(*ledger, coin, tx.From) < requiredFee {
			return *ledger, errors.New("insufficient balance")
		}
		if err := applyDTLTransaction(ledger, tx, height); err != nil {
			return *ledger, err
		}
		addBalance(ledger, coin, tx.From, -requiredFee)
		addBalance(ledger, coin, TREASURY_ADDRESS, requiredFee)

	default:
		totalCost := tx.Amount + requiredFee
		if getBalance(*ledger, coin, tx.From) < totalCost {
			return *ledger, errors.New("insufficient balance")
		}

		addBalance(ledger, coin, tx.From, -totalCost)
		addBalance(ledger, coin, tx.To, tx.Amount)
		addBalance(ledger, coin, TREASURY_ADDRESS, requiredFee)
	}

	// =====================================
	// 3ï¸âƒ£ UPDATE NONCE
	// =====================================
	setNonce(ledger, tx.From, tx.Nonce)

	return *ledger, nil
}

func HashMempool(m *Mempool) string {
	ids := make([]string, 0, len(m.Transactions))
	for _, tx := range m.Transactions {
		ids = append(ids, tx.ID)
	}
	sort.Strings(ids)

	var b strings.Builder
	for _, id := range ids {
		b.WriteString(id)
		b.WriteString(";")
	}

	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:])
}
func SettleReward(
	ledger Ledger,
	user string,
	worker string,
	validators []string,
	owner string,
	total int,
) Ledger {

	if total <= 0 {
		return ledger
	}

	if len(validators) == 0 {
		// ðŸ”’ No validators â†’ no distribution
		return ledger
	}

	workerReward := total * 60 / 100
	userReward := total * 10 / 100
	ownerReward := total * 15 / 100
	validatorPool := total - (workerReward + userReward + ownerReward)

	addBalance(&ledger, CoinSymbol, worker, workerReward)
	addBalance(&ledger, CoinSymbol, user, userReward)
	addBalance(&ledger, CoinSymbol, owner, ownerReward)

	perValidator := validatorPool / len(validators)
	for _, v := range validators {
		addBalance(&ledger, CoinSymbol, v, perValidator)
	}

	return ledger
}
func (m *Mempool) ValidateTransaction(
	tx Transaction,
	ledger *Ledger,
) error {

	if ledger == nil {
		return errors.New("ledger is nil")
	}
	ensureStakeMap(ledger)
	ensureEVMStateMap(ledger)
	ensureEVMCodeMap(ledger)
	ensureEVMStorageMap(ledger)
	ensureDTLState(ledger)
	ensureValidatorUpdateCertLedgerState(ledger)

	if tx.Type == TxFaucet {
		if !IsTestnet {
			return errors.New("faucet disabled on mainnet")
		}
		if tx.From != USER_REWARD_POOL {
			return errors.New("faucet source invalid")
		}
	}
	if tx.Type == TxEVM {
		return errors.New("evm/vm removed permanently")
	}

	// -----------------------------------
	// 1ï¸âƒ£ BASIC SANITY
	// -----------------------------------
	if tx.Type == TxEVM {
		if tx.Amount < 0 {
			return errors.New("invalid amount")
		}
		resolvedTx, err := hydrateEVMExecutionCode(ledger, tx)
		if err != nil {
			return err
		}
		if err := validateEVMTransaction(resolvedTx); err != nil {
			return err
		}
		tx = resolvedTx
	} else if tx.Type == TxDTL {
		if err := validateDTLTransaction(ledger, tx, 0); err != nil {
			return err
		}
	} else if tx.Type == TxValidatorUpdate {
		if err := validatorUpdateEnvelopeBasicError(tx, ledger, 1); err != nil {
			return err
		}
	} else if tx.Amount <= 0 {
		return errors.New("invalid amount")
	}

	coin := normalizeCoin(tx.Coin)
	if !AllowedCoins[coin] {
		return fmt.Errorf("unsupported coin: %s", coin)
	}

	requiredFee := requiredFeeForTxWithLedger(ledger, tx)
	if tx.Type == TxEVM {
		if tx.Fee < requiredFee {
			return fmt.Errorf("invalid fee: got %d minimum %d", tx.Fee, requiredFee)
		}
	} else if tx.Type == TxDTL {
		if err := validateDTLFeeBounds(tx.Fee, requiredFee); err != nil {
			return err
		}
	} else if tx.Fee != requiredFee {
		return fmt.Errorf("invalid fee: got %d expected %d", tx.Fee, requiredFee)
	}

	switch tx.Type {
	case TxStake:
		validatorID := strings.TrimSpace(tx.To)
		if validatorID == "" {
			return errors.New("missing validator id")
		}
		if other, ok := walletBoundValidator(ledger, tx.From, validatorID); ok {
			return fmt.Errorf("wallet already bound to validator %s", other)
		}
		lockEpochs := tx.StakeEpochs
		if lockEpochs == 0 {
			lockEpochs = DefaultStakeLockEpochs
		}
		if lockEpochs < minUnstakeEpochs() {
			return errors.New("stake lock too short")
		}
		if _, err := validateStakeConsensusPubKey(tx, GlobalValidatorRegistry.Snapshot()); err != nil {
			return err
		}
		if err := validateValidatorMinimumStakeAfterTx(ledger, tx); err != nil {
			return err
		}
	case TxUnstake:
		validatorID := strings.TrimSpace(tx.To)
		if validatorID == "" {
			return errors.New("missing validator id")
		}
		if tx.Amount <= requiredFee {
			return errors.New("unstake amount must exceed fee")
		}
		key := stakeKey(tx.From, validatorID)
		if rec, ok := ledger.Stakes[key]; !ok || rec.Amount < tx.Amount {
			return errors.New("insufficient staked balance")
		}
	case TxDTL:
		// No additional account/validator checks.
	}

	// -----------------------------------
	// 2ï¸âƒ£ CHAIN BINDING (ANTI-REPLAY)
	// -----------------------------------
	if tx.ChainID != ChainID {
		return fmt.Errorf("invalid chain id: %s", tx.ChainID)
	}

	now := time.Now().Unix()
	if tx.Expiry <= now {
		return errors.New("transaction expired")
	}
	if MaxTxTTLSeconds > 0 && tx.Expiry-now > int64(MaxTxTTLSeconds) {
		return errors.New("transaction expiry too far")
	}

	// -----------------------------------
	// 3ï¸âƒ£ ADDRESS â†” PUBLIC KEY CHECK
	// -----------------------------------
	if tx.Type == TxFaucet {
		if strings.TrimSpace(tx.PublicKey) != "" || strings.TrimSpace(tx.Signature) != "" {
			return errors.New("faucet tx must not include signature")
		}
	} else if isEVMRawTransaction(tx) {
		if err := validateEVMRawTransactionBinding(tx); err != nil {
			return err
		}
	} else {
		pubKeyBytes, err := hex.DecodeString(tx.PublicKey)
		if err != nil {
			return errors.New("invalid public key encoding")
		}

		if !AddressMatchesPublicKey(tx.From, ed25519.PublicKey(pubKeyBytes)) {
			return errors.New("address/public key mismatch")
		}

		pubKey, err := DecodePublicKey(tx.PublicKey)
		if err != nil {
			return errors.New("invalid public key")
		}

		sig, err := DecodeSignature(tx.Signature)
		if err != nil {
			return errors.New("invalid signature encoding")
		}

		// -----------------------------------
		// 4ï¸âƒ£ SIGNATURE VERIFY (DETERMINISTIC)
		// -----------------------------------
		payload := TxPayload(tx)
		if !ed25519.Verify(pubKey, payload, sig) {
			// Backward compatibility for older wallet payload layout.
			legacyPayload := TxPayloadLegacy(tx)
			if !ed25519.Verify(pubKey, legacyPayload, sig) {
				return errors.New("signature verification failed")
			}
		}
	}

	// -----------------------------------
	// 5ï¸âƒ£ BALANCE CHECK (STATE)
	// -----------------------------------
	required := tx.Amount + tx.Fee
	switch tx.Type {
	case TxUnstake:
		required = 0
	case TxEVM:
		required = tx.Fee
	case TxDTL:
		required = tx.Fee
	}

	if required > 0 && getBalance(*ledger, coin, tx.From) < required {
		return errors.New("insufficient balance")
	}

	return nil
}

func validateValidatorUpdateTx(tx Transaction, ledger *Ledger) bool {
	return validatorUpdateEnvelopeBasicError(tx, ledger, 1) == nil
}

func matchesLegacySignedTxID(tx Transaction, providedID string) bool {
	providedID = strings.TrimSpace(providedID)
	if providedID == "" {
		return false
	}
	sigHex := strings.TrimSpace(tx.Signature)
	if sigHex == "" {
		return false
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) == 0 {
		return false
	}
	payload := TxPayload(tx)
	legacy := sha256.Sum256(append(payload, sig...))
	if strings.EqualFold(providedID, hex.EncodeToString(legacy[:])) {
		return true
	}
	legacyPayload := TxPayloadLegacy(tx)
	legacyLegacy := sha256.Sum256(append(legacyPayload, sig...))
	return strings.EqualFold(providedID, hex.EncodeToString(legacyLegacy[:]))
}

func normalizeIncomingTx(tx *Transaction) {
	if tx == nil {
		return
	}
	tx.ID = strings.TrimSpace(tx.ID)
	tx.From = strings.TrimSpace(tx.From)
	tx.To = strings.TrimSpace(tx.To)
	tx.PublicKey = strings.TrimSpace(tx.PublicKey)
	tx.Signature = strings.TrimSpace(tx.Signature)
	tx.Coin = normalizeCoin(tx.Coin)
	tx.ChainID = strings.TrimSpace(tx.ChainID)
	tx.EVMCode = strings.TrimSpace(tx.EVMCode)
	tx.EVMInput = strings.TrimSpace(tx.EVMInput)
	tx.EVMRawTx = strings.TrimSpace(tx.EVMRawTx)
	tx.EVMTxHash = strings.TrimSpace(tx.EVMTxHash)
	tx.DTLTxType = strings.TrimSpace(tx.DTLTxType)
	tx.DTLTokenID = strings.TrimSpace(tx.DTLTokenID)
	tx.DTLPayload = strings.TrimSpace(tx.DTLPayload)
	tx.DTLGovernanceCert = strings.TrimSpace(tx.DTLGovernanceCert)
	if tx.ValidatorUpdateCert != nil {
		normalizeValidatorUpdateCert(tx.ValidatorUpdateCert)
	}
	if tx.ChainID == "" {
		tx.ChainID = ChainID
	}
}

func validateTransactionShape(tx Transaction) error {
	if len(tx.ID) > 0 {
		if len(tx.ID) != MaxTxIDHexLen {
			return errors.New("invalid tx id length")
		}
		if _, err := hex.DecodeString(tx.ID); err != nil {
			return errors.New("invalid tx id encoding")
		}
	}
	if len(tx.From) > MaxTxAddressLen {
		return errors.New("from address too long")
	}
	if len(tx.To) > MaxTxAddressLen {
		return errors.New("to address too long")
	}
	if len(tx.Coin) > MaxTxCoinLen {
		return errors.New("coin symbol too long")
	}
	if len(tx.ChainID) > MaxTxChainIDLen {
		return errors.New("chain id too long")
	}
	if len(stripHexPrefix(tx.EVMCode)) > MaxTxEVMCodeHexLen {
		return errors.New("evm_code too long")
	}
	if len(stripHexPrefix(tx.EVMInput)) > MaxTxEVMInputHexLen {
		return errors.New("evm_input too long")
	}
	if len(stripHexPrefix(tx.EVMRawTx)) > MaxTxEVMRawHexLen {
		return errors.New("evm_raw_tx too long")
	}
	if txHash := stripHexPrefix(tx.EVMTxHash); txHash != "" {
		if len(txHash) != MaxTxEVMHashHexLen {
			return errors.New("invalid evm_tx_hash length")
		}
		if _, err := hex.DecodeString(txHash); err != nil {
			return errors.New("invalid evm_tx_hash encoding")
		}
	}
	if len(tx.DTLTxType) > MaxTxDTLTypeLen {
		return errors.New("dtl_tx_type too long")
	}
	if len(tx.DTLTokenID) > MaxTxDTLTokenIDLen {
		return errors.New("dtl_token_id too long")
	}
	if len(tx.DTLPayload) > MaxTxDTLPayloadLen {
		return errors.New("dtl_payload too long")
	}
	if len(tx.DTLGovernanceCert) > MaxTxDTLGCertLen {
		return errors.New("dtl_governance_cert too long")
	}
	if tx.ValidatorUpdateCert != nil {
		if err := validatorUpdateCertShapeError(tx.ValidatorUpdateCert); err != nil {
			return err
		}
	}
	if tx.Type == TxDTL {
		if tx.DTLTxType == "" {
			return errors.New("missing dtl_tx_type")
		}
		if tx.DTLPayload == "" {
			return errors.New("missing dtl_payload")
		}
	}
	if tx.Type != TxFaucet && !isEVMRawTransaction(tx) {
		if len(tx.PublicKey) != MaxTxPubKeyHexLen {
			return errors.New("invalid public key length")
		}
		if len(tx.Signature) != MaxTxSignatureHexLen {
			return errors.New("invalid signature length")
		}
	}
	return nil
}

func (n *Node) ReceiveTransaction(tx Transaction) bool {
	ok, _ := n.ReceiveTransactionWithReason(tx)
	return ok
}

func (n *Node) ReceiveTransactionWithReason(tx Transaction) (bool, string) {
	normalizeIncomingTx(&tx)
	if err := validateTransactionShape(tx); err != nil {
		return false, err.Error()
	}
	// Keep MSC<->EVM alias mapping fresh so MetaMask/Remix transfers can
	// resolve back to native MSC wallet addresses.
	executionLedger := n.currentExecutionLedgerClone()
	if n != nil {
		registerLedgerAddressAlias(&executionLedger, tx.From)
		registerLedgerAddressAlias(&executionLedger, tx.To)
		n.setExecutionLedger(executionLedger)
	}

	canonicalID := ComputeTxID(tx)
	legacyCanonicalID := ComputeTxIDLegacy(tx)
	if tx.ID == "" {
		tx.ID = canonicalID
	} else if !strings.EqualFold(tx.ID, canonicalID) {
		switch {
		case strings.EqualFold(tx.ID, legacyCanonicalID):
			tx.ID = canonicalID
		case matchesLegacySignedTxID(tx, tx.ID):
			tx.ID = canonicalID
		default:
			return false, "tx id mismatch"
		}
	}

	// =====================================================
	// 1ï¸âƒ£ DUPLICATE PROTECTION (GLOBAL)
	// =====================================================
	if n.hasSeenTx(canonicalID) {
		return false, "duplicate transaction"
	}

	// =====================================================
	// 1.5ï¸âƒ£ PER-SENDER RATE LIMIT
	// =====================================================
	if !allowTxFrom(tx.From) {
		return false, "sender rate limit exceeded"
	}

	// =====================================================
	// 2ï¸âƒ£ CRYPTO + STATE VALIDATION
	// =====================================================
	if err := n.Mempool.ValidateTransaction(tx, &executionLedger); err != nil {
		if DebugConsensus {
			fmt.Println("âŒ TX rejected:", err.Error())
		}
		return false, err.Error()
	}

	// =====================================================
	// 3ï¸âƒ£ ADD TO MEMPOOL (DETERMINISTIC)
	// =====================================================
	currentHeight := uint64(0)
	if n.Blockchain != nil {
		currentHeight = n.Blockchain.Height()
	}
	if finalized := n.getFinalizedHeight(); finalized > currentHeight {
		currentHeight = finalized
	}
	if tx.Type == TxValidatorUpdate {
		execHeight := currentHeight + 1
		ctx := n.newValidatorUpdateExecutionContext(execHeight)
		if ctx == nil {
			return false, "validator updates disabled"
		}
		ledgerCopy := executionLedger.Clone()
		if _, err := ExecuteTransactionWithNodeContext(n, ctx, &ledgerCopy, tx, int(execHeight)); err != nil {
			return false, err.Error()
		}
	}
	if ok, reason := n.Mempool.AddTransaction(tx, executionLedger, currentHeight); !ok {
		if reason == "" {
			reason = "mempool rejected transaction"
		}
		return false, reason
	}

	// =====================================================
	// 4ï¸âƒ£ MARK AS SEEN (AFTER ACCEPT)
	// =====================================================
	n.markTxSeen(canonicalID)

	if DebugConsensus {
		fmt.Printf("ðŸ’¸ TX accepted | id=%s\n", canonicalID)
	}

	// =====================================================
	// 5ï¸âƒ£ GOSSIP (NO TRUST ASSUMED)
	// =====================================================
	n.BroadcastTx(tx)

	return true, ""
}

func appendLedgerHashMaterial(b *strings.Builder, ledger Ledger) {
	if b == nil {
		return
	}

	keys := make([]string, 0, len(ledger.Balances))
	for k := range ledger.Balances {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(strconv.Itoa(ledger.Balances[k]))
		b.WriteString(";")
	}

	if len(ledger.Stakes) > 0 {
		stakeKeys := make([]string, 0, len(ledger.Stakes))
		for k := range ledger.Stakes {
			stakeKeys = append(stakeKeys, k)
		}
		sort.Strings(stakeKeys)
		for _, k := range stakeKeys {
			rec := ledger.Stakes[k]
			b.WriteString("stake|")
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(strconv.Itoa(rec.Amount))
			b.WriteString("@")
			b.WriteString(strconv.FormatUint(rec.LockedUntil, 10))
			b.WriteString(";")
		}
	}

	if len(ledger.ValidatorRewardWallets) > 0 {
		rewardCanonical := make(map[string]string, len(ledger.ValidatorRewardWallets))
		for rawVID, rawAddr := range ledger.ValidatorRewardWallets {
			norm := normalizeRewardValidatorID(rawVID)
			addr := strings.TrimSpace(rawAddr)
			if norm == "" || addr == "" {
				continue
			}
			if existing, ok := rewardCanonical[norm]; !ok || addr < existing {
				rewardCanonical[norm] = addr
			}
		}

		rewardKeys := make([]string, 0, len(rewardCanonical))
		for vid := range rewardCanonical {
			norm := normalizeRewardValidatorID(vid)
			if norm == "" {
				continue
			}
			rewardKeys = append(rewardKeys, norm)
		}
		sort.Strings(rewardKeys)
		for _, vid := range rewardKeys {
			addr := rewardCanonical[vid]
			b.WriteString("reward|")
			b.WriteString(vid)
			b.WriteString("=")
			b.WriteString(addr)
			b.WriteString(";")
		}
	}

	if len(ledger.UsedValidatorUpdateCerts) > 0 {
		certKeys := make([]string, 0, len(ledger.UsedValidatorUpdateCerts))
		for key := range ledger.UsedValidatorUpdateCerts {
			key = strings.ToLower(strings.TrimSpace(key))
			if len(key) != 64 {
				continue
			}
			certKeys = append(certKeys, key)
		}
		sort.Strings(certKeys)
		for _, key := range certKeys {
			b.WriteString("validator_update_cert|")
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(strconv.FormatUint(ledger.UsedValidatorUpdateCerts[key], 10))
			b.WriteString(";")
		}
	}

	if len(ledger.EVMState) > 0 {
		evmKeys := make([]string, 0, len(ledger.EVMState))
		for k := range ledger.EVMState {
			if strings.TrimSpace(k) == "" {
				continue
			}
			evmKeys = append(evmKeys, k)
		}
		sort.Strings(evmKeys)
		for _, k := range evmKeys {
			v := strings.TrimSpace(ledger.EVMState[k])
			b.WriteString("evm|")
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(v)
			b.WriteString(";")
		}
	}

	if len(ledger.EVMCode) > 0 {
		codeKeys := make([]string, 0, len(ledger.EVMCode))
		for k := range ledger.EVMCode {
			if strings.TrimSpace(k) == "" {
				continue
			}
			codeKeys = append(codeKeys, k)
		}
		sort.Strings(codeKeys)
		for _, k := range codeKeys {
			v := normalizeEVMHexData(ledger.EVMCode[k])
			b.WriteString("evm_code|")
			b.WriteString(strings.ToLower(strings.TrimSpace(k)))
			b.WriteString("=")
			b.WriteString(v)
			b.WriteString(";")
		}
	}

	if len(ledger.EVMStorage) > 0 {
		contractKeys := make([]string, 0, len(ledger.EVMStorage))
		for k := range ledger.EVMStorage {
			if strings.TrimSpace(k) == "" {
				continue
			}
			contractKeys = append(contractKeys, k)
		}
		sort.Strings(contractKeys)
		for _, contract := range contractKeys {
			slotMap := ledger.EVMStorage[contract]
			if len(slotMap) == 0 {
				continue
			}
			slotKeys := make([]string, 0, len(slotMap))
			for slot := range slotMap {
				if strings.TrimSpace(slot) == "" {
					continue
				}
				slotKeys = append(slotKeys, slot)
			}
			sort.Strings(slotKeys)
			for _, slot := range slotKeys {
				val := normalizeEVMStorageValue(slotMap[slot])
				b.WriteString("evm_storage|")
				b.WriteString(strings.ToLower(strings.TrimSpace(contract)))
				b.WriteString("|")
				b.WriteString(normalizeEVMStorageSlotKey(slot))
				b.WriteString("=")
				b.WriteString(val)
				b.WriteString(";")
			}
		}
	}

	appendDTLStateHashMaterial(b, ledger.DTL)
}

func canonicalLedgerHashMaterial(ledger Ledger) string {
	var b strings.Builder
	appendLedgerHashMaterial(&b, ledger)
	return b.String()
}

func HashLedger(ledger Ledger) string {
	material := canonicalLedgerHashMaterial(ledger)
	hash := sha256.Sum256([]byte(material))
	return hex.EncodeToString(hash[:])
}

func LedgerStateMerkleRoot(ledger Ledger) string {
	material := canonicalLedgerHashMaterial(ledger)
	if strings.TrimSpace(material) == "" {
		sum := sha256.Sum256([]byte("ledger:empty"))
		return hex.EncodeToString(sum[:])
	}
	parts := strings.Split(material, ";")
	leaves := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sum := sha256.Sum256([]byte(part))
		leaves = append(leaves, hex.EncodeToString(sum[:]))
	}
	return merkleRootFromHexLeaves(leaves)
}

func normalizeCoin(coin string) string {
	if coin == "" {
		return CoinSymbol
	}
	return coin
}

func balanceKey(coin, addr string) string {
	return normalizeCoin(coin) + "|" + canonicalAddressKey(addr)
}

func nonceKey(addr string) string {
	return canonicalAddressKey(addr)
}

func getNonce(ledger Ledger, addr string) int {
	return ledger.Nonces[nonceKey(addr)]
}

func setNonce(ledger *Ledger, addr string, nonce int) {
	if ledger == nil {
		return
	}
	if ledger.Nonces == nil {
		ledger.Nonces = make(map[string]int)
	}
	ledger.Nonces[nonceKey(addr)] = nonce
}

func getBalance(ledger Ledger, coin, addr string) int {
	return ledger.Balances[balanceKey(coin, addr)]
}

func addBalance(ledger *Ledger, coin, addr string, delta int) {
	key := balanceKey(coin, addr)
	ledger.Balances[key] += delta
}

func currentCoinSupply(ledger *Ledger, coin string) int64 {
	if ledger == nil {
		return 0
	}
	symbol := normalizeCoin(coin)
	prefix := symbol + "|"
	total := int64(0)
	for key, amount := range ledger.Balances {
		if strings.HasPrefix(key, prefix) {
			total += int64(amount)
		}
	}
	// Stake locks are maintained in MSC units.
	if symbol == CoinSymbol {
		for _, rec := range ledger.Stakes {
			if rec.Amount > 0 {
				total += int64(rec.Amount)
			}
		}
	}
	if total < 0 {
		return 0
	}
	return total
}

func effectiveBurnFloorSupply() int64 {
	if BurnStopSupply < 0 {
		return 0
	}
	return BurnStopSupply
}

func burnCapacityForCoin(ledger *Ledger, coin string) int64 {
	if ledger == nil {
		return 0
	}
	current := currentCoinSupply(ledger, coin)
	floor := effectiveBurnFloorSupply()
	if current <= floor {
		return 0
	}
	return current - floor
}

func burnCoinsFromAddress(ledger *Ledger, coin, addr string, amount int64) int64 {
	if ledger == nil || amount <= 0 || strings.TrimSpace(addr) == "" {
		return 0
	}
	capacity := burnCapacityForCoin(ledger, coin)
	if capacity <= 0 {
		return 0
	}
	if amount > capacity {
		amount = capacity
	}
	bal := int64(getBalance(*ledger, coin, addr))
	if bal <= 0 {
		return 0
	}
	if amount > bal {
		amount = bal
	}
	if amount <= 0 {
		return 0
	}
	addBalance(ledger, coin, addr, -int(amount))
	return amount
}

func setBalance(ledger *Ledger, coin, addr string, amount int) {
	key := balanceKey(coin, addr)
	ledger.Balances[key] = amount
}

func ComputeTxFee(amount int) int {
	if amount <= 0 {
		return 0
	}
	minBps := GlobalConfig.MinFeeBps
	maxBps := GlobalConfig.MaxFeeBps
	floorAmt := GlobalConfig.FeeFloorAmount
	ceilAmt := GlobalConfig.FeeCeilAmount

	if minBps <= 0 {
		minBps = 1
	}
	if maxBps < minBps {
		maxBps = minBps
	}
	if floorAmt <= 0 {
		floorAmt = 1
	}
	if ceilAmt <= floorAmt {
		ceilAmt = floorAmt
	}

	bps := minBps
	if amount > floorAmt && maxBps > minBps {
		if amount >= ceilAmt {
			bps = maxBps
		} else {
			bps = minBps + (amount-floorAmt)*(maxBps-minBps)/(ceilAmt-floorAmt)
		}
	}

	fee := amount * bps / 10000
	if fee < 1 {
		fee = 1
	}
	return fee
}

func execQuorumRequired(total int) int {
	if total <= 0 {
		return 0
	}
	pct := GlobalConfig.ExecQuorumPct
	if pct <= 0 {
		pct = 60
	}
	if pct > 100 {
		pct = 100
	}
	required := (total*pct + 99) / 100
	if required < 1 {
		required = 1
	}
	return required
}

func (cs *ConsensusState) FinalizeHeight(committedHeight uint64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// ðŸ”’ Safety: only finalize current height
	if committedHeight != cs.Height {
		return
	}

	// âŒ Votes no longer exist (MODEL-2 / MODEL-3)
	delete(cs.Proposals, cs.Height)
	cs.clearActiveExecutionViewLocked()

	// âœ… Move to next height
	cs.Height++
	cs.Round = 0
	cs.Phase = PhasePropose
	cs.RoundStart = time.Now()

	if DebugConsensus {
		fmt.Printf("âœ… Height finalized â†’ %d\n", cs.Height)
	}
}
func ShortHash(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}

func checkRateLimit(peerAddr string, msgType string) bool {
	limiterMu.Lock()
	defer limiterMu.Unlock()

	// MODEL-3: deterministic + bounded keyspace
	if len(peerAddr) > 128 {
		return false
	}

	key := peerAddr + ":" + msgType

	limiter, exists := messageLimiter[key]
	if !exists {
		// 100 msgs/sec with small burst
		limiter = rate.NewLimiter(
			rate.Every(time.Second/100),
			20,
		)
		messageLimiter[key] = limiter
	}
	messageLimiterLastSeen[key] = time.Now()

	return limiter.Allow()
}

func allowTxFrom(addr string) bool {
	addr = canonicalAddressKey(addr)
	if addr == "" || MaxTxPerSecondPerSender <= 0 {
		return true
	}

	key := "txsender:" + addr

	limiterMu.Lock()
	defer limiterMu.Unlock()

	limiter, exists := messageLimiter[key]
	if !exists {
		interval := time.Second / time.Duration(MaxTxPerSecondPerSender)
		if interval <= 0 {
			interval = time.Second
		}
		limiter = rate.NewLimiter(rate.Every(interval), MaxTxPerSecondPerSender)
		messageLimiter[key] = limiter
	}
	messageLimiterLastSeen[key] = time.Now()

	return limiter.Allow()
}
func (n *Node) Shutdown() error {
	if n == nil {
		return nil
	}

	n.shutdownOnce.Do(func() {
		n.CancelRootContext()
		if n.shutdownCh != nil {
			close(n.shutdownCh)
		}
		if n.closeChan != nil {
			close(n.closeChan)
		}
	})

	// Idempotent extra cancel to ensure all context-aware loops observe stop.
	n.CancelRootContext()
	n.stopDedicatedThreads()

	// Cancel active subscriptions before transport teardown.
	if n.BlockSubscription != nil {
		n.BlockSubscription.Cancel()
		n.BlockSubscription = nil
	}
	if n.TxSubscription != nil {
		n.TxSubscription.Cancel()
		n.TxSubscription = nil
	}
	if n.ConsensusSub != nil {
		n.ConsensusSub.Cancel()
		n.ConsensusSub = nil
	}
	if n.ValidatorSub != nil {
		n.ValidatorSub.Cancel()
		n.ValidatorSub = nil
	}

	// Close topics to stop publication goroutines deterministically.
	if n.TopicBlocks != nil {
		_ = n.TopicBlocks.Close()
		n.TopicBlocks = nil
	}
	if n.TopicProposal != nil {
		_ = n.TopicProposal.Close()
		n.TopicProposal = nil
	}
	if n.TopicVote != nil {
		_ = n.TopicVote.Close()
		n.TopicVote = nil
	}
	if n.BlockTopic != nil {
		_ = n.BlockTopic.Close()
		n.BlockTopic = nil
	}
	if n.TxTopic != nil {
		_ = n.TxTopic.Close()
		n.TxTopic = nil
	}
	if n.VoteTopic != nil {
		_ = n.VoteTopic.Close()
		n.VoteTopic = nil
	}
	if n.ProposalTopic != nil {
		_ = n.ProposalTopic.Close()
		n.ProposalTopic = nil
	}
	if n.ValidatorTopic != nil {
		_ = n.ValidatorTopic.Close()
		n.ValidatorTopic = nil
	}
	if n.ConsensusTopic != nil {
		_ = n.ConsensusTopic.Close()
		n.ConsensusTopic = nil
	}

	// Close libp2p host after channels/context are canceled.
	if n.Host != nil {
		_ = n.Host.Close()
		n.Host = nil
	}

	// Wait for tracked worker goroutines, but do not block forever.
	waitDone := make(chan struct{})
	go func() {
		n.wg.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(5 * time.Second):
		log.Printf("[WARN] shutdown wait timeout: background goroutines still draining")
	}

	if err := n.persistLocalState(); err != nil {
		if DebugNet {
			fmt.Printf("WARN failed to persist node state: %v\n", err)
		}
	}

	// PubSub has no explicit close API; nil references after host close.
	n.PubSub = nil

	// Close databases.
	if n.DB != nil {
		_ = n.DB.Close()
	}

	log.Println("Node shutdown completed cleanly")
	return nil
}

func (n *Node) ShutdownWithReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "unspecified"
	}
	log.Printf("[SHUTDOWN] requested: reason=%s", reason)
	return n.Shutdown()
}

func (n *Node) applyStartupConsensusRecovery() {
	if n == nil {
		return
	}
	tip := uint64(0)
	if n.Blockchain != nil {
		tip = n.Blockchain.Height()
	}

	heights := []uint64{tip, tip + 1, n.currentEpoch()}
	seen := make(map[uint64]struct{}, len(heights))
	type recoveredValidatorSet struct {
		validators []string
		hash       string
	}
	recoveredByHeight := make(map[uint64]recoveredValidatorSet, len(heights))
	for _, h := range heights {
		if h == 0 {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		if validators, setHash, _, ok := n.resolveCommittedValidatorSetForHeight(h); ok && len(validators) > 0 {
			recoveredByHeight[h] = recoveredValidatorSet{
				validators: append([]string{}, canonicalValidatorIDs(validators)...),
				hash:       strings.TrimSpace(setHash),
			}
			continue
		}
		validators := canonicalValidatorIDs(n.consensusValidatorsForHeight(h))
		// Startup hardening: do not synthesize non-genesis validator sets from
		// genesis fallback at existing chain heights.
		if len(validators) == 0 && h <= 1 && len(n.GenesisValidators) > 0 {
			validators = canonicalValidatorIDs(append([]string{}, n.GenesisValidators...))
		}
		if len(validators) > 0 {
			recoveredByHeight[h] = recoveredValidatorSet{
				validators: append([]string{}, validators...),
				hash:       strings.TrimSpace(n.preferredValidatorSetHashForHeight(h, validators, nil)),
			}
		}
	}

	n.validatorSetMu.Lock()
	if n.frozenValidatorsByHeight == nil {
		n.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if n.frozenValidatorHashByHeight == nil {
		n.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	n.committeeByHeight = make(map[uint64][]string)
	n.committeeHashByHeight = make(map[uint64]string)
	n.committeeLiveByHeight = make(map[uint64]map[string]bool)
	n.safeModeUntilByHeight = make(map[uint64]time.Time)
	n.safeModeWindowByHeight = make(map[uint64]time.Duration)
	n.safeModeObservedDelays = make([]time.Duration, 0, postBlockSafeModeHistoryLimit())
	n.clearImmediateRoundStart(0)
	n.clearPostBlockSafeModeGate(0)
	n.eligibleSortedValidators = nil
	n.eligibleIndexVersion = 0
	for h, recovered := range recoveredByHeight {
		validators := canonicalValidatorIDs(recovered.validators)
		if len(validators) == 0 {
			continue
		}
		targetHash := strings.TrimSpace(recovered.hash)
		existing := canonicalValidatorIDs(n.frozenValidatorsByHeight[h])
		existingMatches := len(existing) > 0
		if targetHash != "" && existingMatches {
			_, existingMatches = n.validatorSetCandidateMatchesTarget(h, targetHash, existing, nil)
		}
		if len(existing) == 0 || !existingMatches {
			n.frozenValidatorsByHeight[h] = append([]string{}, validators...)
			existing = validators
		}
		if targetHash == "" {
			targetHash = strings.TrimSpace(n.preferredValidatorSetHashForHeight(h, existing, nil))
		}
		if targetHash != "" {
			n.frozenValidatorHashByHeight[h] = targetHash
		} else if strings.TrimSpace(n.frozenValidatorHashByHeight[h]) == "" {
			n.frozenValidatorHashByHeight[h] = ValidatorSetHash(existing)
		}
		n.committeeByHeight[h] = append([]string{}, existing...)
		n.committeeHashByHeight[h] = strings.TrimSpace(n.frozenValidatorHashByHeight[h])
		if n.committeeHashByHeight[h] == "" {
			n.committeeHashByHeight[h] = ValidatorSetHash(existing)
		}
	}
	n.validatorSetMu.Unlock()

	n.peerStateMu.Lock()
	n.peerSuspectAt = make(map[string]time.Time)
	n.quarantineUntil = make(map[string]time.Time)
	n.peerSyncOnlyUntil = make(map[string]time.Time)
	n.peerSyncOnlyClass = make(map[string]string)
	n.peerSyncOnlyLastDropLog = make(map[string]time.Time)
	n.peerDriftState = make(map[string]PeerDriftState)
	n.peerStateMu.Unlock()

	n.invalidProposerMu.Lock()
	n.invalidProposerSeen = make(map[uint64]map[string]int)
	n.invalidProposerStrikes = make(map[string]ExecMismatchTracker)
	n.invalidProposerPeerStrikes = make(map[string]ExecMismatchTracker)
	n.invalidProposerMu.Unlock()

	n.execResultsMu.Lock()
	n.execMismatch = make(map[string]ExecMismatchTracker)
	n.execVoteSeen = make(map[string]time.Time)
	n.execResultsMu.Unlock()

	n.validatorSetMismatchMu.Lock()
	n.validatorSetMismatchCnt = 0
	n.validatorSetMismatchSince = time.Time{}
	n.validatorSetMismatchHeight = 0
	n.validatorSetMismatchExpected = ""
	n.validatorSetMismatchGot = ""
	n.validatorSetRepairKey = ""
	n.validatorSetRepairAt = time.Time{}
	n.validatorSetRepairWindow = time.Time{}
	n.validatorSetRepairAttempts = 0
	n.validatorSetRepairBackoffTil = time.Time{}
	n.validatorSetMismatchMu.Unlock()

	n.startupRecoveryApplied = true
	log.Printf("[AUTOHEAL] startup_recovery_applied height=%d frozen_sets=%d", tip, len(n.frozenValidatorsByHeight))
}

func (n *Node) createSnapshotWithLedger(
	height uint64,
	blockHash string,
	snapshotLedger Ledger,
	ledgerStage string,
) (err error) {
	started := time.Now()
	defer func() {
		n.observeSnapshotOperation("create", height, time.Since(started), err == nil)
	}()
	if n == nil || n.Blockchain == nil {
		return errors.New("snapshot_blockchain_unavailable")
	}
	block := n.Blockchain.LastBlock()
	if block.ID != height {
		if b, ok := n.LoadBlock(int(height)); ok {
			block = b
		}
	}
	if blockHash == "" && block.BlockHash != "" {
		blockHash = block.BlockHash
	}
	snapshotLedger = snapshotLedger.Clone()
	ledgerHash := HashLedger(snapshotLedger)
	if stateRoot := strings.TrimSpace(block.StateRoot); stateRoot != "" {
		computedRoot := ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
		if !strings.EqualFold(stateRoot, strings.TrimSpace(computedRoot)) {
			return fmt.Errorf("execution_snapshot_ledger_unavailable height=%d", height)
		}
	}
	stateRoot := block.StateRoot
	if stateRoot == "" {
		stateRoot = ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
	}

	validators := make(map[string]bool)
	pendingValidators := make(map[string]uint64)
	pendingValidatorRemovals := make(map[string]uint64)
	validatorSetHeight := uint64(0)
	// Snapshots should carry the validator set for the *next* height.
	// Consensus uses snapshot(h) to derive validators for height h+1.
	nextHeight := height + 1
	anchorSetHash := strings.TrimSpace(block.NextValidatorSetHash)
	if anchorSetHash == "" {
		anchorSetHash = strings.TrimSpace(block.ValidatorSetHash)
	}
	list, validatorSetSource, err := n.resolveSnapshotValidatorListWithSource(nextHeight, block)
	if err != nil {
		return err
	}
	validatorSetSource = normalizeCommittedValidatorAuthoritySource(validatorSetSource)
	if validatorSetSource == "none" {
		validatorSetSource = "chain_parent_commitment"
	}
	n.validatorSetMu.RLock()
	for id, act := range n.pendingValidators {
		norm := normalizeValidatorID(id)
		if norm == "" || act == 0 {
			continue
		}
		if existing, ok := pendingValidators[norm]; !ok || act < existing {
			pendingValidators[norm] = act
		}
	}
	for id, act := range n.pendingValidatorRemovals {
		norm := normalizeValidatorID(id)
		if norm == "" || act == 0 {
			continue
		}
		if existing, ok := pendingValidatorRemovals[norm]; !ok || act < existing {
			pendingValidatorRemovals[norm] = act
		}
	}
	validatorSetHeight = n.validatorSetHeight
	n.validatorSetMu.RUnlock()
	for _, v := range list {
		validators[v] = true
	}
	nextValidatorSetHash := strings.TrimSpace(block.NextValidatorSetHash)
	nextValidatorSetRoot := strings.TrimSpace(block.NextValidatorSetRoot)
	nextValidatorSetHeight := blockActivationHeight(block)
	if nextValidatorSetHeight == 0 {
		nextValidatorSetHeight = nextHeight
	}
	registrySnapshot := n.validatorRegistrySnapshotForHeight(nextHeight)
	registryHash := strings.TrimSpace(ValidatorRegistrySnapshotHash(registrySnapshot))
	if len(registrySnapshot) == 0 && height > 0 {
		if committedRegistry, committedHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(height); ok && (source == "live_tip_runtime_repair" || source == "tip_snapshot_repair") {
			registrySnapshot = committedRegistry
			registryHash = strings.TrimSpace(committedHash)
		}
	}
	if len(registrySnapshot) == 0 {
		chainHeight := uint64(0)
		if n.Blockchain != nil {
			chainHeight = n.Blockchain.Height()
		}
		if nextHeight <= 2 || chainHeight <= 1 {
			if DebugConsensus {
				if n.shouldLogLivenessReason(fmt.Sprintf("registry_bootstrap_snapshot:%d", nextHeight), livenessReasonLogCooldown) {
					fmt.Printf("[REGISTRY-BOOTSTRAP] height=%d chain_height=%d reason=snapshot_create\n",
						nextHeight, chainHeight)
				}
			}
			registrySnapshot = copyValidatorRegistrySnapshot(GlobalValidatorRegistry.Snapshot())
			registryHash = strings.TrimSpace(ValidatorRegistrySnapshotHash(registrySnapshot))
		}
	}
	if registryHash == "" {
		registryHash = strings.TrimSpace(ValidatorRegistrySnapshotHash(registrySnapshot))
	}
	resolvedSetHash := strings.TrimSpace(n.validatorSetHashFromFinalizedSnapshot(nextHeight, list))
	if resolvedSetHash == "" {
		resolvedSetHash = strings.TrimSpace(validatorSetHashFromSnapshotForHeight(nextHeight, list, registrySnapshot))
	}
	if anchorSetHash != "" {
		resolvedSetHash = anchorSetHash
	}
	if anchorSetHash != "" && !strings.EqualFold(resolvedSetHash, anchorSetHash) {
		return fmt.Errorf("snapshot_validator_set_hash_mismatch height=%d expected=%s got=%s", height, anchorSetHash, resolvedSetHash)
	}
	if nextValidatorSetHash == "" {
		nextValidatorSetHash = resolvedSetHash
	}
	stateValidators := onChainValidatorsFromRegistrySnapshot(registrySnapshot, pendingValidators, height)
	setRoot := ValidatorSetMerkleRoot(height, list, registrySnapshot)
	currentSetHash := strings.TrimSpace(resolvedSetHash)
	if nextValidatorSetRoot == "" &&
		setRoot != "" &&
		nextValidatorSetHash != "" &&
		strings.EqualFold(strings.TrimSpace(nextValidatorSetHash), currentSetHash) {
		nextValidatorSetRoot = setRoot
	}
	finalizedHash := ""
	if block.FinalizedHeight > 0 {
		finalizedHash = strings.TrimSpace(block.BlockHash)
	}
	snapshot := StateSnapshot{
		Version:         SnapshotVersion,
		Height:          height,
		BlockHash:       blockHash,
		StateRoot:       stateRoot,
		StateMerkleRoot: LedgerStateMerkleRoot(snapshotLedger),
		LedgerHash:      ledgerHash,
		LedgerStage:     ledgerStage,
		GenesisHash:     GenesisHash,
		PrevHash:        block.PrevHash,

		// âœ… Ledger is safe to snapshot
		Ledger: snapshotLedger,

		Validators:                validators,
		ValidatorSetHash:          resolvedSetHash,
		ValidatorSetSource:        validatorSetSource,
		ValidatorSetRoot:          setRoot,
		PendingValidators:         pendingValidators,
		PendingValidatorRemovals:  pendingValidatorRemovals,
		ValidatorSetHeight:        validatorSetHeight,
		NextValidatorSetHash:      nextValidatorSetHash,
		NextValidatorSetSource:    validatorSetSource,
		NextValidatorSetRoot:      nextValidatorSetRoot,
		NextValidatorSetHeight:    nextValidatorSetHeight,
		ActivationHeight:          nextValidatorSetHeight,
		ValidatorRegistry:         registrySnapshot,
		StateValidators:           stateValidators,
		ValidatorRegistryHash:     registryHash,
		CheckpointHeight:          snapshotCheckpointHeightFor(height),
		CheckpointDomain:          syncSnapshotCheckpointDomain(),
		FinalizedEpoch:            block.FinalizedEpoch,
		FinalizedHeight:           block.FinalizedHeight,
		FinalizedHash:             finalizedHash,
		FinalizedStateRoot:        strings.TrimSpace(block.FinalizedStateRoot),
		FinalizedValidatorSetHash: strings.TrimSpace(block.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot: strings.TrimSpace(block.FinalizedValidatorSetRoot),
		EpochAnchorHash:           strings.TrimSpace(block.EpochAnchorHash),
		PreviousEpochAnchorHash:   strings.TrimSpace(block.PreviousEpochAnchorHash),
		FinalityRoot:              strings.TrimSpace(block.FinalityRoot),
		FinalityCertificate:       copyFinalizedEpochCertificate(block.FinalityCertificate),

		Timestamp: time.Now().Unix(),
	}
	n.attachPromotionWindowStateToSnapshot(&snapshot)
	populateSnapshotDerivedFields(&snapshot)
	n.attachSnapshotCheckpointProof(&snapshot)
	snapshot.SnapshotHash = snapshotCanonicalHash(&snapshot)
	return n.storeCommittedStateSnapshotRecord(&snapshot, "create_snapshot")
}

func (n *Node) CreateSnapshot(
	height uint64,
	blockHash string,
) error {

	// MODEL-2 / MODEL-3:
	// Snapshot = execution cache ONLY
	snapshotLedger := Ledger{}
	ledgerStage := ""
	block := Block{}
	blockOK := false
	if n != nil && n.Blockchain != nil && height > 0 {
		block, blockOK = n.Blockchain.GetBlock(height)
	}
	ledgerMatchesBlock := func(ledger Ledger) bool {
		if !blockOK || strings.TrimSpace(block.StateRoot) == "" {
			return ledgerHasInitializedBacking(ledger)
		}
		ledgerHash := HashLedger(ledger)
		expectedRoot := ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
		return strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(block.StateRoot))
	}
	if cachedLedger, ok := n.cachedExecutionSnapshotLedger(height); ok {
		if ledgerMatchesBlock(cachedLedger) {
			snapshotLedger = cachedLedger
			ledgerStage = snapshotLedgerStageExecution
		}
	}
	if !ledgerHasInitializedBacking(snapshotLedger) {
		if snap, _, ok := n.resolveTrustedExecutionSnapshotFromStorage(height); ok && snap != nil && ledgerMatchesBlock(snap.Ledger) {
			snapshotLedger = snap.Ledger.Clone()
			ledgerStage = snapshotLedgerStageExecution
			n.cacheExecutionSnapshotLedger(height, snapshotLedger)
		}
	}
	// A snapshot worker can observe Blockchain.Height immediately after
	// ProcessBlock advances the tip but before the commit tail caches the
	// execution-stage ledger. Prefer the already verified live ledger before
	// falling back to historical replay; replay at every checkpoint can starve
	// consensus and make otherwise healthy validators repeatedly catch up.
	if !ledgerHasInitializedBacking(snapshotLedger) {
		liveLedger := n.currentExecutionLedgerClone()
		if ledgerMatchesBlock(liveLedger) {
			snapshotLedger = liveLedger
			ledgerStage = snapshotLedgerStageExecution
			n.cacheExecutionSnapshotLedger(height, snapshotLedger)
		}
	}
	if !ledgerHasInitializedBacking(snapshotLedger) && n.startupExecutionSnapshotCanRebuildLocally(height) {
		if err := n.rebuildTrustedExecutionSnapshotsUpTo(height); err != nil {
			return err
		}
		if cachedLedger, ok := n.cachedExecutionSnapshotLedger(height); ok && ledgerMatchesBlock(cachedLedger) {
			snapshotLedger = cachedLedger
			ledgerStage = snapshotLedgerStageExecution
		} else if snap, _, ok := n.resolveTrustedExecutionSnapshotFromStorage(height); ok && snap != nil && ledgerMatchesBlock(snap.Ledger) {
			snapshotLedger = snap.Ledger.Clone()
			ledgerStage = snapshotLedgerStageExecution
			n.cacheExecutionSnapshotLedger(height, snapshotLedger)
		}
	}
	if !ledgerHasInitializedBacking(snapshotLedger) {
		return fmt.Errorf("execution_snapshot_ledger_unavailable height=%d", height)
	}
	return n.createSnapshotWithLedger(height, blockHash, snapshotLedger, ledgerStage)
}

func (n *Node) cacheExecutionSnapshotLedger(height uint64, ledger Ledger) {
	if n == nil || height == 0 {
		return
	}
	n.snapshotExecutionLedgerMu.Lock()
	defer n.snapshotExecutionLedgerMu.Unlock()
	if n.snapshotExecutionLedgerByHeight == nil {
		n.snapshotExecutionLedgerByHeight = make(map[uint64]Ledger)
	}
	n.snapshotExecutionLedgerByHeight[height] = ledger.Clone()
	cacheDepth := n.ledgerMemoryCacheDepth()
	removed := 0
	for h := range n.snapshotExecutionLedgerByHeight {
		if h+cacheDepth <= height {
			delete(n.snapshotExecutionLedgerByHeight, h)
			removed++
		}
	}
	maybeReleaseMemoryAfterLedgerCachePrune(removed, height)
}

func (n *Node) cachedExecutionSnapshotLedger(height uint64) (Ledger, bool) {
	if n == nil || height == 0 {
		return Ledger{}, false
	}
	n.snapshotExecutionLedgerMu.Lock()
	defer n.snapshotExecutionLedgerMu.Unlock()
	if n.snapshotExecutionLedgerByHeight == nil {
		return Ledger{}, false
	}
	ledger, ok := n.snapshotExecutionLedgerByHeight[height]
	if !ok {
		return Ledger{}, false
	}
	return ledger.Clone(), true
}

func (n *Node) resolveSnapshotValidatorListWithSource(nextHeight uint64, block Block) ([]string, string, error) {
	targetHash := strings.TrimSpace(block.NextValidatorSetHash)
	if targetHash == "" {
		targetHash = strings.TrimSpace(block.ValidatorSetHash)
	}
	registrySnapshot := n.validatorRegistrySnapshotForHeight(nextHeight)
	candidateMatchesTarget := func(values []string) ([]string, bool) {
		return n.validatorSetCandidateMatchesTarget(nextHeight, targetHash, values, registrySnapshot)
	}
	if ctx := n.blockValidatorUpdatePlanContext(block); ctx != nil {
		if planned := ctx.plannedValidatorsForHeight(nextHeight); len(planned) > 0 {
			if matched, ok := n.validatorSetCandidateMatchesTarget(nextHeight, targetHash, planned, ctx.registrySnapshot); ok {
				return matched, "chain_parent_commitment", nil
			}
		}
	}
	if resolved, _, resolvedSource, ok := n.resolveCommittedValidatorSetForHeight(nextHeight); ok && len(resolved) > 0 {
		if matched, ok := candidateMatchesTarget(resolved); ok {
			source := normalizeCommittedValidatorAuthoritySource(resolvedSource)
			if source == "none" {
				source = "chain_parent_commitment"
			}
			return matched, source, nil
		}
	}
	if committed, ok := blockValidatorSetFromSignatures(block); ok {
		if matched, ok := candidateMatchesTarget(committed); ok {
			return matched, "chain_parent_commitment", nil
		}
	}
	list := n.GetConsensusValidators(int(nextHeight))
	if matched, ok := candidateMatchesTarget(list); ok {
		return matched, "chain_parent_commitment", nil
	}
	if frozen := n.frozenValidatorsForHeight(nextHeight); len(frozen) > 0 {
		if matched, ok := candidateMatchesTarget(frozen); ok {
			return matched, "chain_parent_commitment", nil
		}
	}
	if block.ID > 0 {
		if frozen := n.frozenValidatorsForHeight(block.ID); len(frozen) > 0 {
			if matched, ok := candidateMatchesTarget(frozen); ok {
				return matched, "chain_parent_commitment", nil
			}
		}
	}
	if targetHash != "" {
		if frozen := n.frozenValidatorsForCommittedHash(targetHash, nextHeight, block.ID); len(frozen) > 0 {
			return frozen, "chain_parent_commitment", nil
		}
	}
	if nextHeight > 1 {
		parentHeight := nextHeight - 1
		if parentRegistry, _, _, ok := n.resolveCommittedValidatorRegistrySnapshot(parentHeight); ok && len(parentRegistry) > 0 {
			parentSet := canonicalValidatorIDsFromMapKeys(parentRegistry)
			if matched, ok := candidateMatchesTarget(parentSet); ok {
				return matched, "registry_verified", nil
			}
		}
	}
	legacyBlockCommitment := strings.TrimSpace(block.NextValidatorSetHash) == "" && blockActivationHeight(block) == 0
	if legacyBlockCommitment {
		if boot := n.currentValidatorSetSnapshot(); len(boot) > 0 {
			if matched, ok := candidateMatchesTarget(boot); ok {
				return matched, "genesis_bootstrap", nil
			}
		}
	}
	list = nil
	if len(list) == 0 {
		chainHeight := uint64(0)
		if n != nil && n.Blockchain != nil {
			chainHeight = n.Blockchain.Height()
		}
		// Bootstrap compatibility only: allow runtime/genesis seed during
		// earliest chain heights where historical committed signatures may
		// still be unavailable in-memory.
		if chainHeight <= 1 {
			if boot := n.currentValidatorSetSnapshot(); len(boot) > 0 {
				if matched, ok := candidateMatchesTarget(boot); ok {
					return matched, "genesis_bootstrap", nil
				}
			}
			if len(list) == 0 && nextHeight <= 2 && len(n.GenesisValidators) > 0 {
				if matched, ok := candidateMatchesTarget(n.GenesisValidators); ok {
					return matched, "genesis_bootstrap", nil
				}
			}
		}
	}
	if targetHash != "" {
		reconstructCandidates := make([][]string, 0, 3)
		if committed, ok := blockValidatorSetFromSignatures(block); ok && len(committed) > 0 {
			reconstructCandidates = append(reconstructCandidates, committed)
		}
		if len(n.GenesisValidators) > 0 {
			reconstructCandidates = append(reconstructCandidates, n.GenesisValidators)
		}
		validatorPubKeysMu.RLock()
		if len(GenesisValidatorPubKeys) > 0 {
			reconstructCandidates = append(reconstructCandidates, canonicalValidatorIDsFromMapKeys(GenesisValidatorPubKeys))
		}
		validatorPubKeysMu.RUnlock()
		if registryIDs := canonicalValidatorIDsFromMapKeys(GlobalValidatorRegistry.Snapshot()); len(registryIDs) > 0 {
			reconstructCandidates = append(reconstructCandidates, registryIDs)
		}
		if boot := n.currentValidatorSetSnapshot(); len(boot) > 0 {
			reconstructCandidates = append(reconstructCandidates, boot)
		}
		if reconstructed, ok := n.reconstructValidatorSetCandidateForTarget(nextHeight, targetHash, registrySnapshot, reconstructCandidates...); ok {
			return reconstructed, "chain_parent_commitment", nil
		}
		for _, candidate := range reconstructCandidates {
			if matched, ok := candidateMatchesTarget(candidate); ok {
				return matched, "chain_parent_commitment", nil
			}
		}
	}
	if targetHash != "" {
		return nil, "none", fmt.Errorf("snapshot_validator_set_unresolved next_height=%d target_hash=%s", nextHeight, targetHash)
	}
	if matched, ok := candidateMatchesTarget(list); ok {
		return matched, "genesis_bootstrap", nil
	}
	if len(list) == 0 {
		return nil, "none", fmt.Errorf("snapshot_validator_set_unresolved next_height=%d", nextHeight)
	}
	return canonicalValidatorIDs(list), "genesis_bootstrap", nil
}

func (n *Node) resolveSnapshotValidatorList(nextHeight uint64, block Block) ([]string, error) {
	list, _, err := n.resolveSnapshotValidatorListWithSource(nextHeight, block)
	return list, err
}

func (n *Node) GetSnapshot(height uint64) (snap *StateSnapshot, err error) {
	started := time.Now()
	defer func() {
		n.observeSnapshotOperation("load", height, time.Since(started), err == nil)
	}()
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	key := []byte(fmt.Sprintf("snapshot:%d", height))
	return readSnapshotFromStores(n.DB.SnapshotStoresForRead(), key)
}

func appendUniqueSnapshotAnchorCandidate(candidates []Block, seen map[string]struct{}, candidate Block) []Block {
	key := strings.TrimSpace(candidate.BlockHash) + "|" +
		strings.TrimSpace(candidate.StateRoot) + "|" +
		strings.TrimSpace(candidate.ValidatorSetHash) + "|" +
		strings.TrimSpace(candidate.NextValidatorSetHash) + "|" +
		strings.TrimSpace(candidate.ValidatorRegistryHash) + "|" +
		strings.TrimSpace(candidate.PromotionWindowHash)
	if _, ok := seen[key]; ok {
		return candidates
	}
	seen[key] = struct{}{}
	return append(candidates, candidate)
}

func (n *Node) localSnapshotAnchorCandidates(height uint64) []Block {
	if n == nil || height == 0 {
		return nil
	}
	candidates := make([]Block, 0, 2)
	seen := make(map[string]struct{}, 2)
	if n.Blockchain != nil {
		if blk, ok := n.Blockchain.GetBlock(height); ok {
			candidates = appendUniqueSnapshotAnchorCandidate(candidates, seen, blk)
		}
	}
	if blk, ok := n.LoadBlock(int(height)); ok {
		candidates = appendUniqueSnapshotAnchorCandidate(candidates, seen, blk)
	}
	return candidates
}

func (n *Node) snapshotMatchesLocalAnchorDetailed(snapshot *StateSnapshot) (bool, string) {
	if n == nil || snapshot == nil {
		return false, "snapshot_metadata_invalid"
	}
	if strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)) == "" {
		return false, "registry_hash_mismatch"
	}
	if ok, reason := n.verifySnapshotAgainstLocalBlockDetailed(snapshot); !ok {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "anchor_verification_failed"
		}
		return false, reason
	}
	candidates := n.localSnapshotAnchorCandidates(snapshot.Height)
	if len(candidates) == 0 {
		return false, "anchor_block_unavailable"
	}
	for _, blk := range candidates {
		if !strings.EqualFold(strings.TrimSpace(blk.BlockHash), strings.TrimSpace(snapshot.BlockHash)) {
			continue
		}
		expectedRegistry := strings.TrimSpace(blk.ValidatorRegistryHash)
		if expectedRegistry != "" && !strings.EqualFold(strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)), expectedRegistry) {
			return false, "registry_hash_mismatch"
		}
		expectedPromotionWindow := strings.TrimSpace(blk.PromotionWindowHash)
		if expectedPromotionWindow != "" && !strings.EqualFold(strings.TrimSpace(snapshotPromotionWindowHash(snapshot)), expectedPromotionWindow) {
			return false, "promotion_window_hash_mismatch"
		}
		return true, ""
	}
	return false, "block_hash_mismatch"
}

func (n *Node) snapshotMatchesLocalAnchor(snapshot *StateSnapshot) bool {
	ok, _ := n.snapshotMatchesLocalAnchorDetailed(snapshot)
	return ok
}

const retainedCommittedSnapshotCount = 3

func (n *Node) protectedCommittedSnapshotMinHeight(maxHeight uint64) uint64 {
	if maxHeight == 0 && n != nil && n.Blockchain != nil {
		maxHeight = n.Blockchain.Height()
	}
	if maxHeight == 0 {
		return 0
	}
	if retainedCommittedSnapshotCount <= 1 || maxHeight <= retainedCommittedSnapshotCount {
		return 1
	}
	return maxHeight - retainedCommittedSnapshotCount + 1
}

func (n *Node) shouldProtectCommittedSnapshotHeight(height uint64, maxHeight uint64) bool {
	if n == nil || height == 0 {
		return false
	}
	minHeight := n.protectedCommittedSnapshotMinHeight(maxHeight)
	return minHeight > 0 && height >= minHeight
}

func (n *Node) deleteStoredSnapshotHeight(height uint64) error {
	if n == nil || n.DB == nil || height == 0 {
		return nil
	}
	key := []byte(fmt.Sprintf("snapshot:%d", height))
	latestKey := []byte("snapshot:latest")
	for _, store := range n.DB.SnapshotStoresForRead() {
		if err := store.Update(func(txn *Txn) error {
			if err := txn.Delete(key); err != nil && !errors.Is(err, ErrKeyNotFound) {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
	}
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		if err := store.Update(func(txn *Txn) error {
			item, err := txn.Get(latestKey)
			if err == nil {
				var current []byte
				if err := item.Value(func(val []byte) error {
					current = append([]byte{}, val...)
					return nil
				}); err != nil {
					return err
				}
				if bytes.Equal(current, key) {
					if err := txn.Delete(latestKey); err != nil && !errors.Is(err, ErrKeyNotFound) {
						return err
					}
				}
				return nil
			}
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) refreshLatestSnapshotPointer() error {
	if n == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil
	}
	key, _, err := n.FindLatestSnapshotKey()
	if err != nil {
		if !errors.Is(err, ErrKeyNotFound) {
			return err
		}
		for _, store := range n.DB.SnapshotMetaStoresForRead() {
			if err := store.Update(func(txn *Txn) error {
				if err := txn.Delete([]byte("snapshot:latest")); err != nil && !errors.Is(err, ErrKeyNotFound) {
					return err
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}
	return n.DB.SnapshotMetaStore().Update(func(txn *Txn) error {
		return txn.Set([]byte("snapshot:latest"), key)
	})
}

func (n *Node) scrubInvalidStoredSnapshots(maxHeight uint64) (int, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return 0, nil
	}
	chainHeight := uint64(0)
	if n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}
	if chainHeight == 0 {
		return 0, nil
	}
	if maxHeight == 0 || maxHeight > chainHeight {
		maxHeight = chainHeight
	}
	if maxHeight == 0 {
		return 0, nil
	}

	invalidHeights := make(map[uint64]struct{})
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), maxHeight)
	if err != nil {
		return 0, err
	}
	for _, candidate := range keys {
		snapshot, err := readSnapshotFromStores(n.DB.SnapshotStoresForRead(), candidate.key)
		if err != nil || snapshot == nil || !n.snapshotMatchesLocalAnchor(snapshot) {
			invalidHeights[candidate.height] = struct{}{}
		}
	}
	if len(invalidHeights) == 0 {
		return 0, nil
	}
	heights := make([]int, 0, len(invalidHeights))
	for height := range invalidHeights {
		heights = append(heights, int(height))
	}
	sort.Ints(heights)
	removed := 0
	for _, height := range heights {
		if n.shouldProtectCommittedSnapshotHeight(uint64(height), maxHeight) {
			continue
		}
		if err := n.deleteStoredSnapshotHeight(uint64(height)); err != nil {
			return removed, err
		}
		removed++
	}
	if err := n.refreshLatestSnapshotPointer(); err != nil {
		return removed, err
	}
	return removed, nil
}

func (n *Node) pruneStoredSnapshotsAboveHeight(height uint64) (int, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return 0, nil
	}
	var heights []uint64
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
	if err != nil {
		return 0, err
	}
	for _, key := range keys {
		if key.height > height {
			heights = append(heights, key.height)
		}
	}
	if len(heights) == 0 {
		return 0, nil
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	removed := 0
	for _, h := range heights {
		if err := n.deleteStoredSnapshotHeight(h); err != nil {
			return removed, err
		}
		removed++
	}
	if err := n.pruneSnapshotMetaAboveHeight(height); err != nil {
		return removed, err
	}
	if err := n.pruneSnapshotDeltasAboveHeight(height); err != nil {
		return removed, err
	}
	if err := n.clearStaleTipSnapshotRecordsAboveHeight(height); err != nil {
		return removed, err
	}
	if err := n.refreshLatestSnapshotPointer(); err != nil {
		return removed, err
	}
	return removed, nil
}

func (n *Node) verifiedStoredSnapshotAtOrBelow(targetHeight uint64) (*StateSnapshot, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	searchHeight := targetHeight
	if searchHeight == 0 {
		if n.Blockchain != nil {
			searchHeight = n.Blockchain.Height()
		}
	}
	if searchHeight == 0 {
		return nil, ErrKeyNotFound
	}

	for searchHeight > 0 {
		snapshot, err := n.GetSnapshotAtOrBelow(searchHeight)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			return nil, ErrKeyNotFound
		}
		if n.snapshotMatchesLocalAnchor(snapshot) {
			return snapshot, nil
		}
		if err := n.deleteStoredSnapshotHeight(snapshot.Height); err != nil {
			return nil, err
		}
		if err := n.refreshLatestSnapshotPointer(); err != nil {
			return nil, err
		}
		if snapshot.Height <= 1 {
			break
		}
		searchHeight = snapshot.Height - 1
	}
	return nil, ErrKeyNotFound
}

func (n *Node) GetLatestSnapshot() (*StateSnapshot, error) {
	if n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	var key []byte
	var lastErr error
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		err := store.View(func(txn *Txn) error {
			item, err := txn.Get([]byte("snapshot:latest"))
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
				key = append([]byte{}, val...)
				return nil
			})
		})
		if err == nil && len(key) > 0 {
			return n.GetSnapshotKey(key)
		}
		if !errors.Is(err, ErrKeyNotFound) {
			return nil, err
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrKeyNotFound
}

// GetSnapshotAtOrBelow returns the highest snapshot height <= targetHeight.
// If targetHeight is 0, it returns the latest snapshot.
func (n *Node) GetSnapshotAtOrBelow(targetHeight uint64) (*StateSnapshot, error) {
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	if targetHeight == 0 {
		return n.GetLatestSnapshot()
	}

	var bestKey []byte
	var bestHeight uint64
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), targetHeight)
	if err != nil {
		return nil, err
	}
	for _, candidate := range keys {
		if candidate.height >= bestHeight {
			bestHeight = candidate.height
			bestKey = append([]byte{}, candidate.key...)
		}
	}
	if len(bestKey) == 0 {
		return nil, fmt.Errorf("snapshot not found")
	}
	return n.GetSnapshotKey(bestKey)
}

// FindLatestSnapshotKey scans the DB for the highest snapshot height if
// the snapshot:latest pointer is missing or corrupted.
func (n *Node) FindLatestSnapshotKey() ([]byte, uint64, error) {
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, 0, fmt.Errorf("snapshot db not initialized")
	}
	var bestKey []byte
	var bestHeight uint64
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
	if err != nil {
		return nil, 0, err
	}
	for _, candidate := range keys {
		if candidate.height > bestHeight {
			bestHeight = candidate.height
			bestKey = append([]byte{}, candidate.key...)
		}
	}
	if bestHeight == 0 || len(bestKey) == 0 {
		return nil, 0, ErrKeyNotFound
	}
	return bestKey, bestHeight, nil
}

// LoadBestSnapshot returns the latest snapshot, falling back to a full scan
// if the snapshot:latest pointer is missing.
func (n *Node) LoadBestSnapshot() (*StateSnapshot, error) {
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	snap, err := n.GetLatestSnapshot()
	if err == nil && snap != nil {
		return snap, nil
	}
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		if fallback, scanErr := n.loadBestReadableSnapshotAtOrBelow(0); scanErr == nil && fallback != nil {
			return fallback, nil
		}
		return nil, err
	}
	return n.loadBestReadableSnapshotAtOrBelow(0)
}

func (n *Node) GetSnapshotKey(key []byte) (*StateSnapshot, error) {
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	return readSnapshotFromStores(n.DB.SnapshotStoresForRead(), key)
}

func snapshotAnchorBlock(snapshot StateSnapshot) Block {
	nextActivation := snapshotActivationHeight(&snapshot)
	if nextActivation == 0 {
		nextActivation = snapshot.Height + 1
	}
	nextHash := strings.TrimSpace(snapshot.NextValidatorSetHash)
	if nextHash == "" {
		nextHash = strings.TrimSpace(snapshot.ValidatorSetHash)
	}
	anchor := Block{
		ID:                        snapshot.Height,
		Height:                    snapshot.Height,
		BlockHash:                 strings.TrimSpace(snapshot.BlockHash),
		PrevHash:                  strings.TrimSpace(snapshot.PrevHash),
		Type:                      BlockTypeReceipt,
		BlockTime:                 LogicalTimeForEpoch(snapshot.Height),
		StateRoot:                 strings.TrimSpace(snapshot.StateRoot),
		ValidatorSetHash:          strings.TrimSpace(snapshot.ValidatorSetHash),
		ValidatorSetRoot:          strings.TrimSpace(snapshot.ValidatorSetRoot),
		ValidatorRegistryHash:     strings.TrimSpace(snapshotValidatorRegistryHash(&snapshot)),
		PromotionWindowHash:       strings.TrimSpace(snapshotPromotionWindowHash(&snapshot)),
		NextValidatorSetHash:      nextHash,
		NextValidatorSetRoot:      strings.TrimSpace(snapshot.NextValidatorSetRoot),
		NextValidatorSetHeight:    nextActivation,
		ActivationHeight:          nextActivation,
		FinalizedEpoch:            snapshot.FinalizedEpoch,
		FinalizedHeight:           snapshot.FinalizedHeight,
		FinalizedStateRoot:        strings.TrimSpace(snapshot.FinalizedStateRoot),
		FinalizedValidatorSetHash: strings.TrimSpace(snapshot.FinalizedValidatorSetHash),
		FinalizedValidatorSetRoot: strings.TrimSpace(snapshot.FinalizedValidatorSetRoot),
		EpochAnchorHash:           strings.TrimSpace(snapshot.EpochAnchorHash),
		PreviousEpochAnchorHash:   strings.TrimSpace(snapshot.PreviousEpochAnchorHash),
		FinalityRoot:              strings.TrimSpace(snapshot.FinalityRoot),
		FinalityCertificate:       copyFinalizedEpochCertificate(snapshot.FinalityCertificate),
		Signatures:                validatorsFromSnapshot(&snapshot),
	}
	if cert := snapshot.FinalityCertificate; cert != nil {
		anchor.ConsensusMode = strings.TrimSpace(cert.ConsensusMode)
		anchor.QuorumPolicyVersion = strings.TrimSpace(cert.QuorumPolicyVersion)
		anchor.ActiveReadyCount = cert.ActiveReadyCount
		anchor.RequiredQuorum = cert.RequiredQuorum
		anchor.StrictQuorum = cert.StrictQuorum
		if len(anchor.Signatures) == 0 {
			anchor.Signatures = canonicalValidatorIDs(cert.Signers)
		}
	}
	if strings.TrimSpace(anchor.NextValidatorSetRoot) == "" &&
		strings.TrimSpace(anchor.ValidatorSetRoot) != "" &&
		strings.EqualFold(strings.TrimSpace(anchor.NextValidatorSetHash), strings.TrimSpace(anchor.ValidatorSetHash)) {
		anchor.NextValidatorSetRoot = strings.TrimSpace(anchor.ValidatorSetRoot)
	}
	anchor.Timestamp = int64(SystemTimeUnits(anchor.BlockTime))
	return anchor
}

func (n *Node) loadDurableBlock(height uint64) (Block, bool) {
	if n == nil || height == 0 {
		return Block{}, false
	}
	var blk Block
	if n.DB != nil && n.DB.Blocks != nil {
		err := n.DB.Blocks.View(func(txn *Txn) error {
			item, err := txn.Get([]byte(fmt.Sprintf("block:%d", height)))
			if err != nil {
				return err
			}
			return item.Value(func(v []byte) error {
				plain, derr := decryptDBValue(v)
				if derr != nil {
					return derr
				}
				return json.Unmarshal(plain, &blk)
			})
		})
		if err == nil && blk.ID == height && strings.TrimSpace(blk.BlockHash) != "" {
			return blk, true
		}
	}
	if blk, ok := n.loadBlockFile(height); ok {
		return blk, true
	}
	return Block{}, false
}

func (n *Node) ensureSnapshotAnchorBlockStored(anchor Block) {
	if n == nil || anchor.ID == 0 || strings.TrimSpace(anchor.BlockHash) == "" {
		return
	}
	if existing, ok := n.loadDurableBlock(anchor.ID); ok &&
		strings.EqualFold(strings.TrimSpace(existing.BlockHash), strings.TrimSpace(anchor.BlockHash)) &&
		strings.TrimSpace(existing.StateRoot) != "" {
		n.StoreBlock(existing)
		return
	}
	n.StoreBlock(anchor)
}

func (n *Node) persistAppliedSnapshotExecutionAuthority(snapshot StateSnapshot, reason string) bool {
	if n == nil || snapshot.Height == 0 {
		return false
	}
	anchor, ok := n.snapshotAnchorBlockForLedgerReplay(snapshot)
	if !ok {
		return false
	}
	ledgerHash := HashLedger(snapshot.Ledger)
	expectedRoot := ComputeExecHashVersioned(anchor, ledgerHash, executionStateRootVersionForHeight(anchor.ID))
	if expectedRoot == "" ||
		!strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(anchor.StateRoot)) ||
		!strings.EqualFold(strings.TrimSpace(snapshot.StateRoot), strings.TrimSpace(anchor.StateRoot)) {
		return false
	}
	upgraded := cloneStateSnapshot(&snapshot)
	if upgraded == nil {
		return false
	}
	upgraded.LedgerStage = snapshotLedgerStageExecution
	populateSnapshotDerivedFields(upgraded)
	if err := n.storeCommittedStateSnapshotRecord(upgraded, "snapshot_apply_execution_upgrade"); err != nil {
		log.Printf("[WARN] applied snapshot execution authority persist failed height=%d reason=%s err=%v",
			snapshot.Height, strings.TrimSpace(reason), err)
		return false
	}
	log.Printf("[SNAPSHOT-ANCHOR] status=execution_snapshot_stored height=%d reason=%s",
		snapshot.Height, strings.TrimSpace(reason))
	return true
}

func (n *Node) ApplySnapshotForSync(snapshot StateSnapshot) (applied bool) {
	started := time.Now()
	defer func() {
		n.observeSnapshotOperation("apply", snapshot.Height, time.Since(started), applied)
	}()
	if snapshot.Height == 0 {
		return
	}
	populateSnapshotDerivedFields(&snapshot)
	if reason := n.snapshotLocalFinalityRejectReason(&snapshot); reason != "" {
		log.Printf("[SNAPSHOT-REJECT] reason=%s local_finalized=%d snapshot=%d hash=%s",
			reason,
			n.getFinalizedHeight(),
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	resumeLedger := snapshot.Ledger.Clone()
	prevSyncing := false
	prevPaused := false
	prevTarget := uint64(0)
	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		prevSyncing = n.Consensus.Syncing
		prevPaused = n.Consensus.Paused
		prevTarget = n.Consensus.SyncTarget
		n.Consensus.Syncing = true
		n.Consensus.Paused = true
		n.Consensus.mu.Unlock()
		defer func() {
			n.Consensus.mu.Lock()
			n.Consensus.Syncing = prevSyncing
			n.Consensus.Paused = prevPaused
			n.Consensus.SyncTarget = prevTarget
			n.Consensus.mu.Unlock()
			if !prevSyncing {
				n.replayQueuedExecutionVotes()
			}
			if !prevSyncing && !prevPaused {
				n.clearImmediateRoundStart(0)
				// Sync exit must refresh validator liveness immediately so peers do
				// not wait for the periodic heartbeat window to restore quorum view.
				n.requestHeartbeatBroadcast(true)
				// Snapshot apply already established the new committed tip, so kick
				// round 0 directly instead of waiting for the consensus ticker.
				n.startNextRoundImmediatelyWithReason(snapshot.Height+1, resumeLedger.Clone(), "snapshot_sync")
			}
		}()
	}

	// Anchor chain tip to snapshot height so subsequent blocks can apply.
	anchor := snapshotAnchorBlock(snapshot)
	shouldStoreAnchor := false
	n.Blockchain.mu.Lock()
	currentHeight := uint64(0)
	if len(n.Blockchain.Blocks) > 0 {
		currentHeight = n.Blockchain.Blocks[len(n.Blockchain.Blocks)-1].ID
	}
	if currentHeight < snapshot.Height {
		n.Blockchain.Blocks = []Block{anchor}
		shouldStoreAnchor = true
	} else if currentHeight == snapshot.Height {
		if ln := len(n.Blockchain.Blocks); ln > 0 &&
			strings.EqualFold(strings.TrimSpace(n.Blockchain.Blocks[ln-1].BlockHash), strings.TrimSpace(snapshot.BlockHash)) {
			shouldStoreAnchor = true
		}
	}
	n.Blockchain.mu.Unlock()

	n.applySnapshotValidators(snapshot)
	n.applySnapshotValidatorTransitions(snapshot)
	n.applySnapshotValidatorRegistry(snapshot)
	if shouldStoreAnchor {
		n.ensureSnapshotAnchorBlockStored(anchor)
	}
	n.persistAppliedSnapshotExecutionAuthority(snapshot, "snapshot_sync")
	resumeLedger = n.applySnapshotExecutionTipLedger(snapshot, "snapshot_sync")
	n.snapshotEpochValidators(snapshot.Height + 1)

	// Align committed height with snapshot (authoritative state).
	n.commitMu.Lock()
	if n.committedHeight < snapshot.Height {
		n.committedHeight = snapshot.Height
	}
	if n.lastCommitHeight < snapshot.Height {
		n.lastCommitHeight = snapshot.Height
		n.lastCommitAt = time.Now()
	} else if n.lastCommitAt.IsZero() && n.committedHeight > 0 {
		n.lastCommitHeight = n.committedHeight
		n.lastCommitAt = time.Now()
	}
	if n.committed == nil {
		n.committed = make(map[uint64]string)
	}
	n.committed[snapshot.Height] = snapshot.BlockHash
	if n.finalizedHeight < snapshot.Height {
		n.finalizedHeight = snapshot.Height
	}
	n.commitMu.Unlock()
	n.persistSnapshotCommitSafety(Block{
		ID:        snapshot.Height,
		Height:    snapshot.Height,
		BlockHash: snapshot.BlockHash,
		PrevHash:  snapshot.PrevHash,
	}, "snapshot_sync")

	n.setLogicalTick(snapshot.Height+1, TickExec)
	applied = true
	return applied
}

// ApplySnapshotForRecovery force-applies a snapshot even if local height is higher.
// This is used for auto-heal when local state diverges.
func (n *Node) ApplySnapshotForRecovery(snapshot StateSnapshot) (applied bool) {
	started := time.Now()
	defer func() {
		n.observeSnapshotOperation("apply", snapshot.Height, time.Since(started), applied)
	}()
	if snapshot.Height == 0 {
		return
	}
	populateSnapshotDerivedFields(&snapshot)
	if reason := n.snapshotLocalFinalityRejectReason(&snapshot); reason != "" {
		log.Printf("[SNAPSHOT-REJECT] reason=%s local_finalized=%d snapshot=%d hash=%s",
			reason,
			n.getFinalizedHeight(),
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	resumeLedger := snapshot.Ledger.Clone()
	currentHeight := uint64(0)
	if n != nil && n.Blockchain != nil {
		currentHeight = n.Blockchain.Height()
	}
	if currentHeight > 0 && snapshot.Height < currentHeight {
		log.Printf("[SNAPSHOT-REJECT] reason=height_regression local=%d snapshot=%d hash=%s",
			currentHeight,
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	legacyTransitionSnapshot := len(snapshot.PendingValidators) == 0 &&
		len(snapshot.PendingValidatorRemovals) == 0 &&
		snapshot.ValidatorSetHeight == 0
	prevPendingValidators := make(map[string]uint64)
	prevPendingRemovals := make(map[string]uint64)
	prevValidatorSetHeight := uint64(0)
	if legacyTransitionSnapshot {
		n.validatorSetMu.RLock()
		for id, act := range n.pendingValidators {
			prevPendingValidators[id] = act
		}
		for id, act := range n.pendingValidatorRemovals {
			prevPendingRemovals[id] = act
		}
		prevValidatorSetHeight = n.validatorSetHeight
		n.validatorSetMu.RUnlock()
	}
	// Fast path: avoid re-applying an identical recovery snapshot repeatedly.
	// Re-anchoring the same height/hash causes recovery loops without progress.
	localHeight := n.Blockchain.Height()
	if localHeight == snapshot.Height {
		tipHash := ""
		n.Blockchain.mu.RLock()
		if ln := len(n.Blockchain.Blocks); ln > 0 {
			last := n.Blockchain.Blocks[ln-1]
			if last.ID == snapshot.Height {
				tipHash = last.BlockHash
			}
		}
		n.Blockchain.mu.RUnlock()

		if tipHash == snapshot.BlockHash {
			n.commitMu.Lock()
			committedHash := ""
			if n.committed != nil {
				committedHash = n.committed[snapshot.Height]
			}
			committedHeight := n.committedHeight
			n.commitMu.Unlock()
			if committedHeight >= snapshot.Height && committedHash == snapshot.BlockHash {
				n.applySnapshotValidators(snapshot)
				n.applySnapshotValidatorTransitions(snapshot)
				n.applySnapshotValidatorRegistry(snapshot)
				n.snapshotEpochValidators(snapshot.Height + 1)
				n.ensureSnapshotAnchorBlockStored(snapshotAnchorBlock(snapshot))
				n.persistAppliedSnapshotExecutionAuthority(snapshot, "snapshot_recovery_same_height")
				n.applySnapshotExecutionTipLedger(snapshot, "snapshot_recovery_same_height")
				applied = true
				return
			}
		}
	}

	prevSyncing := false
	prevPaused := false
	prevTarget := uint64(0)
	if n.Consensus != nil {
		n.Consensus.mu.Lock()
		prevSyncing = n.Consensus.Syncing
		prevPaused = n.Consensus.Paused
		prevTarget = n.Consensus.SyncTarget
		n.Consensus.Syncing = true
		n.Consensus.Paused = true
		n.Consensus.mu.Unlock()
		defer func() {
			n.Consensus.mu.Lock()
			n.Consensus.Syncing = prevSyncing
			n.Consensus.Paused = prevPaused
			n.Consensus.SyncTarget = prevTarget
			n.Consensus.mu.Unlock()
			if !prevSyncing {
				n.replayQueuedExecutionVotes()
			}
			if !prevSyncing && !prevPaused {
				n.clearImmediateRoundStart(0)
				// Recovery exit should re-advertise validator liveness before
				// resuming consensus so lagging peers refresh quorum promptly.
				n.requestHeartbeatBroadcast(true)
				// Recovery snapshots should resume consensus at the next height
				// immediately; round 0 must not wait for the periodic loop.
				n.startNextRoundImmediatelyWithReason(snapshot.Height+1, resumeLedger.Clone(), "snapshot_recovery")
			}
		}()
	}

	n.applyMu.Lock()
	defer n.applyMu.Unlock()

	anchor := snapshotAnchorBlock(snapshot)

	n.Blockchain.mu.Lock()
	n.Blockchain.Blocks = []Block{anchor}
	n.Blockchain.mu.Unlock()

	n.pruneBlocksAboveHeight(snapshot.Height)
	n.resetTransientStateForRecovery(snapshot.Height)

	n.applySnapshotValidators(snapshot)
	n.applySnapshotValidatorTransitions(snapshot)
	n.pruneFrozenValidatorStateBefore(snapshot.Height)
	if legacyTransitionSnapshot {
		n.validatorSetMu.Lock()
		// Keep pre-recovery transition queues when old snapshots don't carry them.
		if len(prevPendingValidators) > 0 {
			n.pendingValidators = prevPendingValidators
		}
		if len(prevPendingRemovals) > 0 {
			n.pendingValidatorRemovals = prevPendingRemovals
		}
		if prevValidatorSetHeight > n.validatorSetHeight {
			n.validatorSetHeight = prevValidatorSetHeight
		}
		n.validatorSetMu.Unlock()
	}
	n.applySnapshotValidatorRegistry(snapshot)
	n.ensureSnapshotAnchorBlockStored(anchor)
	n.persistAppliedSnapshotExecutionAuthority(snapshot, "snapshot_recovery")
	resumeLedger = n.applySnapshotExecutionTipLedger(snapshot, "snapshot_recovery")
	n.snapshotEpochValidators(snapshot.Height + 1)
	n.syncFrozenValidatorSetHashesFromChain()

	n.commitMu.Lock()
	n.committed = map[uint64]string{snapshot.Height: snapshot.BlockHash}
	n.committedHeight = snapshot.Height
	n.lastCommitHeight = snapshot.Height
	n.lastCommitAt = time.Now()
	if n.finalizedHeight < snapshot.Height {
		n.finalizedHeight = snapshot.Height
	}
	n.commitMu.Unlock()
	n.persistSnapshotCommitSafety(anchor, "snapshot_recovery")

	n.setLogicalTick(snapshot.Height+1, TickExec)
	n.hardResetConsensus(snapshot.Height + 1)

	if DebugConsensus {
		fmt.Printf("RECOVERY snapshot applied height=%d\n", snapshot.Height)
	}
	applied = true
	return applied
}

func (n *Node) pruneFrozenValidatorStateBefore(anchorHeight uint64) {
	if n == nil || anchorHeight == 0 {
		return
	}
	n.validatorSetMu.Lock()
	defer n.validatorSetMu.Unlock()
	for h := range n.frozenValidatorsByHeight {
		if h < anchorHeight {
			delete(n.frozenValidatorsByHeight, h)
		}
	}
	for h := range n.frozenValidatorHashByHeight {
		if h < anchorHeight {
			delete(n.frozenValidatorHashByHeight, h)
		}
	}
	for h := range n.epochValidators {
		if h < anchorHeight {
			delete(n.epochValidators, h)
		}
	}
}

func (n *Node) pruneBlocksAboveHeight(height uint64) {
	if n.DB == nil || n.DB.Blocks == nil {
		return
	}
	prefix := []byte("block:")
	_ = n.DB.Blocks.Update(func(txn *Txn) error {
		it := txn.NewIterator(DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := it.Item().Key()
			keyStr := string(key)
			if !strings.HasPrefix(keyStr, "block:") {
				continue
			}
			h, err := strconv.ParseUint(strings.TrimPrefix(keyStr, "block:"), 10, 64)
			if err != nil {
				continue
			}
			if h > height {
				if err := txn.Delete(key); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err := deleteBlockFilesAboveHeight(n.DataDir, n.ID, height); err != nil {
		log.Printf("[WARN] block file prune failed height=%d err=%v", height, err)
	}
}

func (n *Node) applySnapshotValidators(snapshot StateSnapshot) {
	if len(snapshot.Validators) == 0 {
		return
	}
	lockHeight := snapshot.Height
	if snapshot.ValidatorSetHeight > 0 {
		lockHeight = snapshot.ValidatorSetHeight
	}
	list := make([]string, 0, len(snapshot.Validators))
	for id := range snapshot.Validators {
		list = append(list, id)
	}
	list = canonicalValidatorIDs(list)
	n.validatorSetMu.Lock()
	if n.validatorSetHeight > 0 && lockHeight < n.validatorSetHeight {
		// Ignore stale snapshot validator sets once we've advanced.
		n.validatorSetMu.Unlock()
		return
	}
	if n.frozenValidatorsByHeight == nil {
		n.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if n.frozenValidatorHashByHeight == nil {
		n.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	n.frozenValidatorsByHeight[lockHeight] = append([]string{}, list...)
	hash := strings.TrimSpace(snapshot.ValidatorSetHash)
	if hash == "" {
		hash = ValidatorSetHash(list)
	}
	n.frozenValidatorHashByHeight[lockHeight] = hash
	n.validatorSetHeight = lockHeight
	n.validatorSetMu.Unlock()
	n.snapshotEpochValidators(snapshot.Height + 1)
}

func (n *Node) applySnapshotValidatorTransitions(snapshot StateSnapshot) {
	// Backward compatibility: older snapshots do not carry transition queues.
	if len(snapshot.PendingValidators) == 0 &&
		len(snapshot.PendingValidatorRemovals) == 0 &&
		snapshot.ValidatorSetHeight == 0 {
		return
	}

	pendingAdds := make(map[string]uint64, len(snapshot.PendingValidators))
	for id, act := range snapshot.PendingValidators {
		norm := normalizeValidatorID(id)
		if norm == "" || act == 0 {
			continue
		}
		if existing, ok := pendingAdds[norm]; !ok || act < existing {
			pendingAdds[norm] = act
		}
	}

	pendingRemovals := make(map[string]uint64, len(snapshot.PendingValidatorRemovals))
	for id, act := range snapshot.PendingValidatorRemovals {
		norm := normalizeValidatorID(id)
		if norm == "" || act == 0 {
			continue
		}
		if existing, ok := pendingRemovals[norm]; !ok || act < existing {
			pendingRemovals[norm] = act
		}
	}

	lockHeight := snapshot.ValidatorSetHeight
	if lockHeight == 0 {
		lockHeight = snapshot.Height
	}

	n.validatorSetMu.Lock()
	if n.validatorSetHeight > 0 && lockHeight < n.validatorSetHeight {
		n.validatorSetMu.Unlock()
		return
	}
	n.pendingValidators = pendingAdds
	n.pendingValidatorRemovals = pendingRemovals
	if lockHeight > 0 {
		n.validatorSetHeight = lockHeight
	}
	n.validatorSetMu.Unlock()
}

func (n *Node) LoadLatestSnapshot() error {
	snapshot, err := n.GetLatestSnapshot()
	if err != nil {
		return err
	}
	return n.applyLoadedSnapshot(snapshot)

	if n.DB == nil || n.DB.Meta == nil {
		return fmt.Errorf("meta db not initialized")
	}
	return n.DB.Meta.View(func(txn *Txn) error {
		item, err := txn.Get([]byte("snapshot:latest"))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return n.LoadSnapshotKey(val)
		})
	})
}

func (n *Node) LoadSnapshotKey(key []byte) error {
	snapshot, err := n.GetSnapshotKey(key)
	if err != nil {
		return err
	}
	return n.applyLoadedSnapshot(snapshot)

	return n.DB.State.View(func(txn *Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			plain, derr := decryptDBValue(val)
			if derr != nil {
				return derr
			}
			var snapshot StateSnapshot
			if err := json.Unmarshal(plain, &snapshot); err != nil {
				return err
			}
			if !snapshotSupportedVersion(snapshot.Version) {
				return fmt.Errorf("unsupported snapshot version %d", snapshot.Version)
			}
			populateSnapshotDerivedFields(&snapshot)
			if !snapshotHasValidMetadata(&snapshot) {
				return fmt.Errorf("invalid snapshot metadata at height %d", snapshot.Height)
			}
			if snapshot.Height > n.Blockchain.Height() {
				n.ApplySnapshotForSync(snapshot)
			} else {
				n.setExecutionLedger(snapshot.Ledger)
				n.cacheExecutionSnapshotLedger(snapshot.Height, snapshot.Ledger)
				n.markExecutionSnapshotReadyHeight(snapshot.Height)
			}
			n.applySnapshotValidators(snapshot)
			n.applySnapshotValidatorTransitions(snapshot)
			n.applySnapshotValidatorRegistry(snapshot)
			if DebugConsensus {
				fmt.Printf(
					"ðŸ§Š Latest snapshot loaded | height=%d | time=%d\n",
					snapshot.Height,
					snapshot.Timestamp,
				)
			}
			return nil
		})
	})
}

func (n *Node) LoadSnapshot(height uint64) error {
	snapshot, err := n.GetSnapshot(height)
	if err != nil {
		return err
	}
	if snapshot.Height != height {
		return fmt.Errorf("snapshot height mismatch: want %d got %d", height, snapshot.Height)
	}
	return n.applyLoadedSnapshot(snapshot)

	key := []byte(fmt.Sprintf("snapshot:%d", height))

	return n.DB.State.View(func(txn *Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			plain, derr := decryptDBValue(val)
			if derr != nil {
				return derr
			}

			var snapshot StateSnapshot
			if err := json.Unmarshal(plain, &snapshot); err != nil {
				return err
			}

			// =====================================================
			// ðŸ”’ SAFETY CHECKS (HARD)
			// =====================================================

			if !snapshotSupportedVersion(snapshot.Version) {
				return fmt.Errorf(
					"unsupported snapshot version %d",
					snapshot.Version,
				)
			}
			populateSnapshotDerivedFields(&snapshot)
			if !snapshotHasValidMetadata(&snapshot) {
				return fmt.Errorf("invalid snapshot metadata at height %d", snapshot.Height)
			}

			if snapshot.Height != height {
				return fmt.Errorf(
					"snapshot height mismatch: want %d got %d",
					height,
					snapshot.Height,
				)
			}

			if snapshot.Height > n.Blockchain.Height() {
				n.ApplySnapshotForSync(snapshot)
			} else {
				// =====================================================
				// ATOMIC RESTORE (LEDGER ONLY)
				// =====================================================
				n.setExecutionLedger(snapshot.Ledger)
				n.cacheExecutionSnapshotLedger(snapshot.Height, snapshot.Ledger)
				n.markExecutionSnapshotReadyHeight(snapshot.Height)
			}

			n.applySnapshotValidators(snapshot)
			n.applySnapshotValidatorTransitions(snapshot)
			n.applySnapshotValidatorRegistry(snapshot)

			if DebugConsensus {
				fmt.Printf(
					"ðŸ§Š Snapshot loaded (ledger only) | height=%d | time=%d\n",
					snapshot.Height,
					snapshot.Timestamp,
				)
			}

			return nil
		})
	})
}

func (n *Node) applyLoadedSnapshot(snapshot *StateSnapshot) error {
	if snapshot == nil {
		return ErrKeyNotFound
	}
	if !snapshotSupportedVersion(snapshot.Version) {
		return fmt.Errorf("unsupported snapshot version %d", snapshot.Version)
	}
	populateSnapshotDerivedFields(snapshot)
	if !snapshotHasValidMetadata(snapshot) {
		return fmt.Errorf("invalid snapshot metadata at height %d", snapshot.Height)
	}
	chainHeight := uint64(0)
	if n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}
	if n.Blockchain != nil && snapshot.Height > chainHeight {
		n.ApplySnapshotForSync(*snapshot)
	} else {
		n.setExecutionLedger(snapshot.Ledger)
		n.cacheExecutionSnapshotLedger(snapshot.Height, snapshot.Ledger)
		n.markExecutionSnapshotReadyHeight(snapshot.Height)
	}
	n.applySnapshotValidators(*snapshot)
	n.applySnapshotValidatorTransitions(*snapshot)
	n.applySnapshotValidatorRegistry(*snapshot)
	if DebugConsensus {
		fmt.Printf("Snapshot loaded | height=%d | time=%d\n", snapshot.Height, snapshot.Timestamp)
	}
	return nil
}

func (l Ledger) Clone() Ledger {
	copy := Ledger{
		Balances:                 make(map[string]int),
		Nonces:                   make(map[string]int),
		Stakes:                   make(map[string]StakeLock),
		ValidatorRewardWallets:   make(map[string]string),
		EVMState:                 make(map[string]string),
		EVMCode:                  make(map[string]string),
		EVMStorage:               make(map[string]map[string]string),
		DTL:                      cloneDTLState(l.DTL),
		UsedValidatorUpdateCerts: make(map[string]uint64),
	}
	for k, v := range l.Balances {
		copy.Balances[k] = v
	}
	for k, v := range l.Nonces {
		copy.Nonces[k] = v
	}
	for k, v := range l.Stakes {
		copy.Stakes[k] = v
	}
	for k, v := range l.ValidatorRewardWallets {
		copy.ValidatorRewardWallets[k] = v
	}
	for k, v := range l.EVMState {
		copy.EVMState[k] = v
	}
	for k, v := range l.EVMCode {
		copy.EVMCode[k] = v
	}
	for contract, slots := range l.EVMStorage {
		if slots == nil {
			continue
		}
		slotCopy := make(map[string]string, len(slots))
		for slot, value := range slots {
			slotCopy[slot] = value
		}
		copy.EVMStorage[contract] = slotCopy
	}
	for k, v := range l.UsedValidatorUpdateCerts {
		copy.UsedValidatorUpdateCerts[k] = v
	}
	return copy
}

func (n *Node) CopyValidators() map[string]bool {
	n.validatorMu.RLock()
	defer n.validatorMu.RUnlock()

	out := make(map[string]bool, len(n.validatorStatus))
	for k := range n.validatorStatus {
		out[k] = true
	}
	return out
}
func SortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (n *Node) ApplyValidatorsFromExecution(
	height uint64,
	newSet map[string]bool,
) error {

	// =====================================================
	// ðŸ”’ Execution-finalized rule (CONSENSUS CRITICAL)
	// =====================================================
	if height != uint64(n.Blockchain.Height()) {
		return fmt.Errorf("validator update rejected: height mismatch")
	}

	n.validatorMu.Lock()
	defer n.validatorMu.Unlock()

	now := time.Now()

	// =====================================================
	// ðŸ”„ Replace validator set atomically (STATUS MODEL)
	// =====================================================
	next := make(map[string]*ValidatorStatus, len(newSet))

	for id := range newSet {

		// Preserve existing status if present
		if old, ok := n.validatorStatus[id]; ok && old != nil {
			next[id] = old
			continue
		}

		// New validator joins
		next[id] = &ValidatorStatus{
			Height:   height,
			LastSeen: now,
		}
	}

	n.validatorStatus = next

	if DebugConsensus {
		fmt.Printf(
			"ðŸ” Validator set updated via execution @ height %d â†’ %v\n",
			height,
			SortedKeys(newSet),
		)
	}

	return nil
}

func (n *Node) PruneSnapshots(keepLast uint64) error {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil
	}
	if n.statePruningArchiveMode() {
		return nil
	}

	// =====================================================
	// MODEL-2 / MODEL-3:
	// Snapshots = execution checkpoints
	// Never prune active or last execution state
	// =====================================================

	base := n.getFinalizedHeight()
	if base == 0 {
		base = n.Blockchain.Height()
	}

	// Hard safety: never prune below genesis + current
	if base <= keepLast {
		return nil
	}

	minKeepHeight := base - keepLast

	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
	if err != nil {
		return err
	}
	for _, candidate := range keys {
		if candidate.height >= minKeepHeight {
			continue
		}
		if DebugConsensus {
			fmt.Printf("Pruning snapshot @ height %d\n", candidate.height)
		}
		if err := n.deleteStoredSnapshotHeight(candidate.height); err != nil {
			return err
		}
	}
	if err := n.pruneSnapshotMetaBelowHeight(minKeepHeight); err != nil {
		return err
	}
	if err := n.pruneSnapshotDeltasBelowHeight(minKeepHeight); err != nil {
		return err
	}
	if n.pruneExecutionSnapshotCacheBefore(minKeepHeight) > 0 {
		if err := n.recordStatePruneMarker("execution_cache", base, minKeepHeight, keepLast); err != nil {
			return err
		}
	}
	return n.recordStatePruneMarker("snapshot", base, minKeepHeight, keepLast)

	if err := n.DB.State.Update(func(txn *Txn) error {

		opts := DefaultIteratorOptions
		opts.PrefetchValues = false

		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("snapshot:")

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {

			item := it.Item()
			key := item.Key()

			// Expected key format: snapshot:<height>
			parts := bytes.Split(key, []byte(":"))
			if len(parts) != 2 {
				continue // malformed key â€” ignore
			}

			h, err := strconv.ParseUint(string(parts[1]), 10, 64)
			if err != nil {
				continue
			}

			// ðŸ”’ SAFETY RULES
			// - Never delete current height
			// - Never delete execution window
			if h >= minKeepHeight {
				continue
			}

			if DebugConsensus {
				fmt.Printf("ðŸ§¹ Pruning snapshot @ height %d\n", h)
			}

			if err := txn.Delete(key); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return err
	}
	if err := n.pruneSnapshotMetaBelowHeight(minKeepHeight); err != nil {
		return err
	}
	if err := n.pruneSnapshotDeltasBelowHeight(minKeepHeight); err != nil {
		return err
	}
	if n.pruneExecutionSnapshotCacheBefore(minKeepHeight) > 0 {
		if err := n.recordStatePruneMarker("execution_cache", base, minKeepHeight, keepLast); err != nil {
			return err
		}
	}
	return n.recordStatePruneMarker("snapshot", base, minKeepHeight, keepLast)
}

func (n *Node) PruneValidatorRegistrySnapshots(keepLast uint64) error {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil
	}
	if n.statePruningArchiveMode() {
		return nil
	}

	base := n.getFinalizedHeight()
	if base == 0 {
		base = n.Blockchain.Height()
	}
	if base <= keepLast {
		return nil
	}
	minKeepHeight := base - keepLast

	if err := n.DB.State.Update(func(txn *Txn) error {
		opts := DefaultIteratorOptions
		opts.PrefetchValues = false

		it := txn.NewIterator(opts)
		defer it.Close()

		prefixes := [][]byte{
			[]byte("validator_registry_snapshot:"),
			[]byte("registry_snapshot:"),
		}
		for _, prefix := range prefixes {
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				item := it.Item()
				key := item.Key()

				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				h, err := strconv.ParseUint(string(parts[1]), 10, 64)
				if err != nil {
					continue
				}
				if h >= minKeepHeight {
					continue
				}
				if DebugConsensus {
					fmt.Printf("Pruning validator registry snapshot @ height %d\n", h)
				}
				if err := txn.Delete(key); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return n.recordStatePruneMarker("registry", base, minKeepHeight, keepLast)
}

// ============================================
// ðŸ”¥ NEW HELPER FUNCTION
// ============================================
func (cs *ConsensusState) InitHeight(height uint64) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// =====================================================
	// MODEL-2 / MODEL-3:
	// Height = execution epoch
	// No voting, no signatures, no quorum
	// =====================================================

	cs.Height = height

	// Reset execution markers (if any)
	cs.Executed = false
	cs.Finalized = false
	cs.BlockHash = ""

	if DebugConsensus {
		fmt.Printf("ðŸ§® Consensus execution height initialized: %d\n", height)
	}
}

func (n *Node) waitForPubSubMesh() {

	// =====================================================
	// MODEL-2 / MODEL-3:
	// PubSub mesh is NOT a consensus requirement
	// =====================================================

	if n.PubSub == nil {
		return
	}

	// Ensure core topic exists (non-blocking)
	_, _ = n.PubSub.Join("msc-blocks")

	if DebugNet {
		fmt.Println("â„¹ï¸ PubSub mesh check skipped (execution-verified consensus)")
	}
}
