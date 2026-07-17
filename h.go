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

// HandleFraud handles fraud.
func (n *Node) HandleFraud(block Block) {
	// `proposer` stores the value produced by this operation.
	proposer := block.Proposer

	// Slash proposer
	n.SlashValidator(proposer)

	// Rollback block
	n.Blockchain.Revert(block.ID)

	fmt.Println("ðŸš¨ FRAUD DETECTED | Proposer slashed:", proposer)
}

// Revert implements the revert helper.
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
		// `newHeight` stores the value produced by this operation.
		newHeight := uint64(0)
		if len(bc.Blocks) > 0 {
			newHeight = bc.Blocks[len(bc.Blocks)-1].ID
		}
		fmt.Printf("â†©ï¸ Blockchain reverted to height %d\n", newHeight)
	}
}

// SlashValidator implements the slash validator helper.
func (n *Node) SlashValidator(addr string) {
	n.slashValidatorForReason(addr, "invalid_block", 0)
}

// slashValidatorForReason implements the slash validator for reason helper.
func (n *Node) slashValidatorForReason(addr string, reason string, evidenceHeight uint64) {
	// `height` stores the value produced by this operation.
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
	if reason != "exec_equivocation" {
		n.validatorMu.Lock()
		delete(n.validatorStatus, addr)
		n.validatorMu.Unlock()
	}

	// =============================
	// 2ï¸âƒ£ Participation penalty
	// =============================
	participationMu.Lock()
	// `p` and `ok` store whether the related condition is satisfied.
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
	// `targetID` stores the value produced by this operation.
	targetID := strings.TrimSpace(strings.ToUpper(addr))
	if targetID != "" && SlashStakeBurnBPS > 0 {
		burnedTotal = n.burnValidatorStakeByBPS(targetID, SlashStakeBurnBPS)
	}
	if burnedTotal > 0 {
		ApplyValidatorStake(addr, -int64(burnedTotal), height)
	}
	// `confiscatedCoins` stores the value produced by this operation.
	confiscatedCoins := int64(0)
	// `confiscatedBurn` stores the value produced by this operation.
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

// forfeitSlashedValidatorBalance implements the forfeit slashed validator balance helper.
func (n *Node) forfeitSlashedValidatorBalance(validatorID string) (int64, int64) {
	if n == nil {
		return 0, 0
	}
	// `targetID` stores the value produced by this operation.
	targetID := normalizeValidatorID(validatorID)
	if targetID == "" {
		return 0, 0
	}

	// `ledger` stores the value produced by this operation.
	ledger := n.currentExecutionLedgerClone()
	// `holder` stores the value produced by this operation.
	holder := canonicalAddressKey(resolveValidatorRecipient(&ledger, targetID))
	if holder == "" {
		return 0, 0
	}

	// `bal` stores the value produced by this operation.
	bal := int64(getBalance(ledger, CoinSymbol, holder))
	if bal <= 0 {
		return 0, 0
	}

	setBalance(&ledger, CoinSymbol, holder, 0)
	addBalance(&ledger, CoinSymbol, TREASURY_ADDRESS, int(bal))
	// `burned` stores the value produced by this operation.
	applied, err := ApplySupplyDelta(&ledger, SupplyDelta{
		Coin:       CoinSymbol,
		BurnFrom:   TREASURY_ADDRESS,
		BurnAmount: bal,
		Reason:     "slashed_validator_forfeit",
	})
	if err != nil {
		return bal, 0
	}
	n.setExecutionLedger(ledger)
	return bal, applied.Burned
}

// TryAlternativeChain implements the try alternative chain helper.
func (n *Node) TryAlternativeChain(block Block) {

	fmt.Printf("ðŸ”„ Evaluating alternative chain at height %d\n", block.ID)

	// =====================================================
	// 1ï¸âƒ£ CHECK QUEUED FORK BLOCKS (SAME HEIGHT)
	// =====================================================
	n.forkMu.RLock()
	// `candidates` and `exists` store whether the related condition is satisfied.
	candidates, exists := n.ForkBlocks[block.ID]
	if exists {
		candidates = append([]Block(nil), candidates...)
	}
	n.forkMu.RUnlock()
	if exists {

		// `alt` tracks the current values while iterating.
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

// RequestBlocks implements the request blocks helper.
func (n *Node) RequestBlocks(from, to int) {

	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	// `pid` tracks the current values while iterating.
	for _, pid := range n.Host.Network().Peers() {

		// `stream` and `err` store the error produced by this operation.
		stream, err := n.Host.NewStream(
			ctx,
			pid,
			BlockSyncProtocol,
		)
		if err != nil {
			continue
		}

		// `enc` stores the value produced by this operation.
		enc := json.NewEncoder(stream)
		// `dec` stores the value produced by this operation.
		dec := json.NewDecoder(stream)

		// `req` stores the request data being processed.
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

		// `resp` stores the response produced by this operation.
		var resp BlockResponse
		// `err` stores the error produced by this operation.
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
		// `b` tracks the current values while iterating.
		for _, b := range resp.Blocks {
			// `err` stores the error produced by this operation.
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

// HashResult hashes result.
func HashResult(result int) []byte {
	// `h` stores the value produced by this operation.
	h := sha256.Sum256(
		[]byte(strconv.Itoa(result)),
	)
	return h[:]
}

// VerifyTx verifies tx.
func VerifyTx(task Task, receipt Receipt) bool {

	// `expected` stores the value produced by this operation.
	expected := ExecuteTask(task)
	if expected != receipt.Output {
		return false
	}

	// `regenerated` stores the value produced by this operation.
	regenerated := GenerateReceipt(task, receipt.Output)
	return regenerated.Hash == receipt.Hash
}

// NewBlockchain creates a new blockchain.
func NewBlockchain() Blockchain {
	// `genesis` stores the value produced by this operation.
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

// GenerateReceipt implements the generate receipt helper.
func GenerateReceipt(task Task, output int) Receipt {

	// `data` stores the value produced by this operation.
	data := fmt.Sprintf("%s:%d:%d", task.TaskID, task.Input, output)
	// `hash` stores the digest used to identify or verify the related data.
	hash := sha256.Sum256([]byte(data))

	return Receipt{
		TaskID: task.TaskID,
		input:  task.Input,
		Output: output,
		Hash:   hex.EncodeToString(hash[:]),
	}
}

// FinalizeBlock implements the finalize block helper.
func FinalizeBlock(
	id uint64,
	blockType BlockType, // ðŸ”¥ FIX
	task Task,
	result int,
	prevHash string,
	proposer string,
) Block {
	// `lt` stores the value produced by this operation.
	lt := LogicalTimeForEpoch(id)
	// `block` stores the synchronization state protecting shared data.
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

// BuildRandomSeed builds random seed.
func BuildRandomSeed(
	prevBlockHash string,
	stateRoot string,
	mempoolHash string,
) []byte {

	// `data` stores the value produced by this operation.
	data := prevBlockHash + stateRoot + mempoolHash
	// `hash` stores the digest used to identify or verify the related data.
	hash := sha256.Sum256([]byte(data))
	return hash[:]
}

// SelectValidator selects validator.
func SelectValidator(
	seed []byte,
	candidates []ValidatorCandidate,
) ValidatorCandidate {

	// `winner` stores the value used by this operation.
	var winner ValidatorCandidate
	// `bestScore` stores the value produced by this operation.
	bestScore := math.MaxFloat64

	// `c` tracks the current values while iterating.
	for _, c := range candidates {

		// `vrfOut` stores the result produced by this operation.
		vrfOut := sha256.Sum256(
			append(seed, c.PubKey...),
		)

		// `raw` stores the value produced by this operation.
		raw := binary.BigEndian.Uint64(vrfOut[:8])

		// ðŸ”¥ Reputation-weighted randomness
		rep := float64(c.Reputation + 1)
		// `score` stores the value produced by this operation.
		score := float64(raw) / rep

		if score < bestScore {
			bestScore = score
			winner = c
		}
	}

	return winner
}

// TryFinalizeBlock implements the try finalize block helper.
func TryFinalizeBlock(
	n *Node,
	block Block,
) bool {

	// 1ï¸âƒ£ Re-execute task locally
	localResult := ExecuteTask(block.Task)
	// `localHash` stores the digest used to identify or verify the related data.
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

// AddBlock adds block.
func (bc *Blockchain) AddBlock(block Block) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	bc.Blocks = append(bc.Blocks, block)
}

// ProduceTaskBlock implements the produce task block helper.
func (n *Node) ProduceTaskBlock(task Task) Block {
	// `result` stores the result produced by this operation.
	result := ExecuteTask(task)
	// `resultHash` stores the digest used to identify or verify the related data.
	resultHash := HashResult(result)
	// `epoch` stores the value produced by this operation.
	epoch := n.Blockchain.Height() + 1
	// `lt` stores the value produced by this operation.
	lt := LogicalTimeForEpoch(epoch)
	// `validators` stores whether the related condition is satisfied.
	validators := n.freezeValidatorSetForHeight(epoch, n.GetConsensusValidators(int(epoch)))

	// `block` stores the synchronization state protecting shared data.
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

// VerifyBlockSignature verifies block signature.
func VerifyBlockSignature(block Block) bool {
	if len(block.Signature) != ed25519.SignatureSize {
		return false
	}

	// `proposerID` stores the value produced by this operation.
	proposerID := normalizeValidatorID(block.Proposer)
	// 1ï¸âƒ£ Build candidate pubkey list.
	// Runtime map can be updated by peer-hello overrides; keep genesis fallback
	// so historical blocks remain verifiable after key changes.
	candidates := make([]ed25519.PublicKey, 0, 4)
	// `addCandidate` stores the value produced by this operation.
	addCandidate := func(pk ed25519.PublicKey) {
		if len(pk) != ed25519.PublicKeySize {
			return
		}
		// `existing` tracks the current values while iterating.
		for _, existing := range candidates {
			if bytes.Equal(existing, pk) {
				return
			}
		}
		// `copied` stores the value produced by this operation.
		copied := make([]byte, len(pk))
		copy(copied, pk)
		candidates = append(candidates, ed25519.PublicKey(copied))
	}
	validatorPubKeysMu.RLock()
	// `runtimeNorm` and `runtimeNormOK` store whether the related condition is satisfied.
	runtimeNorm, runtimeNormOK := ValidatorPubKeys[proposerID]
	// `runtimeRaw` and `runtimeRawOK` store whether the related condition is satisfied.
	runtimeRaw, runtimeRawOK := ValidatorPubKeys[block.Proposer]
	// `genNorm` and `genNormOK` store whether the related condition is satisfied.
	genNorm, genNormOK := GenesisValidatorPubKeys[proposerID]
	// `genRaw` and `genRawOK` store whether the related condition is satisfied.
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
		for _, proposalHash := range executionVoteProposalHashCandidatesForFinalBlock(block) {
			hashes = appendUniqueConsensusHash(hashes, proposalHash)
		}
	}

	// Backward-compatible signing payloads:
	// legacy builds may sign raw hash bytes; current builds sign hash string bytes.
	payloads := make([][]byte, 0, len(hashes)*2)
	// `seenPayload` stores the value produced by this operation.
	seenPayload := make(map[string]struct{}, len(hashes)*2)
	// `addPayload` stores the value produced by this operation.
	addPayload := func(msg []byte) {
		if len(msg) == 0 {
			return
		}
		// `key` stores the key used to access the related value.
		key := string(msg)
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seenPayload[key]; ok {
			return
		}
		seenPayload[key] = struct{}{}
		// `cp` stores the value produced by this operation.
		cp := make([]byte, len(msg))
		copy(cp, msg)
		payloads = append(payloads, cp)
	}
	// `hash` tracks the digest used to identify or verify the related data.
	for _, hash := range hashes {
		addPayload([]byte(hash))
		// `raw` and `err` store the error produced by this operation.
		if raw, err := hex.DecodeString(hash); err == nil && len(raw) > 0 {
			addPayload(raw)
		}
	}

	// 3ï¸âƒ£ Verify ed25519 signature against accepted payloads and key candidates.
	for _, pub := range candidates {
		// `msg` tracks the current values while iterating.
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

// verifyBlockSignatureWithCandidates verifies block signature with candidates.
func verifyBlockSignatureWithCandidates(block Block, candidates []ed25519.PublicKey) bool {
	if len(block.Signature) != ed25519.SignatureSize {
		return false
	}
	if len(candidates) == 0 {
		return false
	}
	// `uniq` stores the value produced by this operation.
	uniq := make([]ed25519.PublicKey, 0, len(candidates))
	// `addCandidate` stores the value produced by this operation.
	addCandidate := func(pk ed25519.PublicKey) {
		if len(pk) != ed25519.PublicKeySize {
			return
		}
		// `existing` tracks the current values while iterating.
		for _, existing := range uniq {
			if bytes.Equal(existing, pk) {
				return
			}
		}
		// `copied` stores the value produced by this operation.
		copied := make([]byte, len(pk))
		copy(copied, pk)
		uniq = append(uniq, ed25519.PublicKey(copied))
	}
	// `pk` tracks the current values while iterating.
	for _, pk := range candidates {
		addCandidate(pk)
	}
	if len(uniq) == 0 {
		return false
	}

	// `hashes` stores the digest used to identify or verify the related data.
	hashes := []string{HashBlock(block)}
	if block.BlockTime.Tick == TickFinalize && block.BlockTime.Epoch > 0 {
		for _, proposalHash := range executionVoteProposalHashCandidatesForFinalBlock(block) {
			hashes = appendUniqueConsensusHash(hashes, proposalHash)
		}
	}

	// `payloads` stores the value produced by this operation.
	payloads := make([][]byte, 0, len(hashes)*2)
	// `seenPayload` stores the value produced by this operation.
	seenPayload := make(map[string]struct{}, len(hashes)*2)
	// `addPayload` stores the value produced by this operation.
	addPayload := func(msg []byte) {
		if len(msg) == 0 {
			return
		}
		// `key` stores the key used to access the related value.
		key := string(msg)
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seenPayload[key]; ok {
			return
		}
		seenPayload[key] = struct{}{}
		// `cp` stores the value produced by this operation.
		cp := make([]byte, len(msg))
		copy(cp, msg)
		payloads = append(payloads, cp)
	}
	// `hash` tracks the digest used to identify or verify the related data.
	for _, hash := range hashes {
		addPayload([]byte(hash))
		// `raw` and `err` store the error produced by this operation.
		if raw, err := hex.DecodeString(hash); err == nil && len(raw) > 0 {
			addPayload(raw)
		}
	}

	// `pub` tracks the current values while iterating.
	for _, pub := range uniq {
		// `msg` tracks the current values while iterating.
		for _, msg := range payloads {
			if ed25519.Verify(pub, msg, block.Signature) {
				return true
			}
		}
	}
	return false
}

// appendUniqueConsensusHash adds a non-empty hash once.
func appendUniqueConsensusHash(values []string, candidate string) []string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return values
	}
	for _, existing := range values {
		if strings.EqualFold(strings.TrimSpace(existing), candidate) {
			return values
		}
	}
	return append(values, candidate)
}

// HashBlock builds the deterministic consensus hash for a block's committed fields.
func HashBlock(block Block) string {

	// `txIDs` stores the transaction data handled by this operation.
	txIDs := make([]string, 0, len(block.Transactions))
	// `tx` tracks the transaction data handled by this operation.
	for _, tx := range block.Transactions {
		txIDs = append(txIDs, tx.ID)
	}
	sort.Strings(txIDs)

	// `sysTime` stores the value produced by this operation.
	sysTime := SystemTimeUnits(block.BlockTime)
	// `quorumPolicyData` stores the value produced by this operation.
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
	// `finalityData` stores the value produced by this operation.
	if finalityData := blockFinalityHashData(block); finalityData != "" {
		quorumPolicyData += finalityData
	}
	// `promotionData` stores the value produced by this operation.
	if promotionData := blockPromotionWindowHashData(block); promotionData != "" {
		quorumPolicyData += promotionData
	}
	if executionData := blockExecutionProtocolHashData(block); executionData != "" {
		quorumPolicyData += executionData
	}
	// `data` stores the value produced by this operation.
	data := ""
	if validatorSetCommitmentV2EnabledAt(block.ID) {
		// `activationHeight` stores the value produced by this operation.
		activationHeight := canonicalActivationHeight(block.NextValidatorSetHeight, block.ActivationHeight)
		// `registryHash` stores the digest used to identify or verify the related data.
		registryHash := strings.TrimSpace(block.ValidatorRegistryHash)
		// `validatorSetRoot` stores whether the related condition is satisfied.
		validatorSetRoot := strings.TrimSpace(block.ValidatorSetRoot)
		// `nextValidatorSetRoot` stores the digest used to identify or verify the related data.
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

	// `hash` stores the digest used to identify or verify the related data.
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// SystemTimeUnits converts logical time into deterministic system time units.
func SystemTimeUnits(t LogicalClock) uint64 {
	return t.Epoch*ConsensusTicksPerEpoch + t.Tick
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

// ExecuteTask implements the execute task helper.
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

// VerifyExecution verifies execution.
func VerifyExecution(task Task, receipt Receipt) bool {
	// `expected` stores the value produced by this operation.
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

// RewardDistribute implements the reward distribute helper.
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

// NewLedger creates a new ledger.
func NewLedger() Ledger {
	return Ledger{
		Balances:                 map[string]int{},
		Nonces:                   make(map[string]int),
		Stakes:                   make(map[string]StakeLock),
		ValidatorRewardWallets:   make(map[string]string),
		DTL:                      NewDTLState(),
		UsedValidatorUpdateCerts: make(map[string]uint64),
		UsedBridgeEvents:         make(map[string]uint64),
	}
}

// stakeKey implements the stake key helper.
func stakeKey(addr, validatorID string) string {
	return canonicalAddressKey(addr) + "|" + validatorID
}

// ensureStakeMap implements the ensure stake map helper.
func ensureStakeMap(ledger *Ledger) {
	if ledger.Stakes == nil {
		ledger.Stakes = make(map[string]StakeLock)
	}
}

// normalizeRewardValidatorID normalizes reward validator id.
func normalizeRewardValidatorID(validatorID string) string {
	return normalizeValidatorID(validatorID)
}

// ensureRewardWalletMap implements the ensure reward wallet map helper.
func ensureRewardWalletMap(ledger *Ledger) {
	if ledger.ValidatorRewardWallets == nil {
		ledger.ValidatorRewardWallets = make(map[string]string)
	}
}

// pinnedGenesisRewardWallet implements the pinned genesis reward wallet helper.
func pinnedGenesisRewardWallet(validatorID string) (string, bool) {
	// `rewardWallet` and `ok` store whether the related condition is satisfied.
	_, rewardWallet, ok := trustedGenesisWalletBindingForValidator(validatorID)
	if !ok {
		return "", false
	}
	// `addr` stores the address used by this operation.
	addr := canonicalAddressKey(rewardWallet)
	if addr == "" {
		return "", false
	}
	return addr, true
}

// setValidatorRewardWallet implements the set validator reward wallet helper.
func setValidatorRewardWallet(ledger *Ledger, validatorID, walletAddr string) {
	if ledger == nil {
		return
	}
	// `id` stores the current position in the related collection.
	id := normalizeRewardValidatorID(validatorID)
	// `addr` stores the address used by this operation.
	addr := canonicalAddressKey(walletAddr)
	if id == "" || addr == "" {
		return
	}
	ensureRewardWalletMap(ledger)
	// `pinnedAddr` and `ok` store whether the related condition is satisfied.
	if pinnedAddr, ok := pinnedGenesisRewardWallet(id); ok {
		if !addressesEqual(addr, pinnedAddr) {
			return
		}
		ledger.ValidatorRewardWallets[id] = pinnedAddr
		return
	}
	ledger.ValidatorRewardWallets[id] = addr
}

// validatorStakeTotals implements the validator stake totals helper.
func validatorStakeTotals(ledger *Ledger, validatorID string) map[string]int {
	// `totals` stores the measured quantity used by this operation.
	totals := make(map[string]int)
	if ledger == nil {
		return totals
	}
	// `targetID` stores the value produced by this operation.
	targetID := normalizeRewardValidatorID(validatorID)
	if targetID == "" {
		return totals
	}
	ensureStakeMap(ledger)

	// `key` and `rec` track the key used to access the related value.
	for key, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}

		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		// `walletAddr` stores the address used by this operation.
		walletAddr := canonicalAddressKey(parts[0])
		if walletAddr == "" {
			continue
		}

		// `recValidatorID` stores the value produced by this operation.
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

// pickDeterministicTopStakeWallet implements the pick deterministic top stake wallet helper.
func pickDeterministicTopStakeWallet(totals map[string]int) (string, bool) {
	if len(totals) == 0 {
		return "", false
	}

	// `wallets` stores the value produced by this operation.
	wallets := make([]string, 0, len(totals))
	// `addr` tracks the address used by this operation.
	for addr := range totals {
		wallets = append(wallets, addr)
	}
	sort.Strings(wallets)

	// `bestAddr` stores the address used by this operation.
	bestAddr := ""
	// `bestStake` stores the value produced by this operation.
	bestStake := -1
	// `addr` tracks the address used by this operation.
	for _, addr := range wallets {
		// `stake` stores the value produced by this operation.
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
	// `targetID` stores the value produced by this operation.
	targetID := normalizeRewardValidatorID(validatorID)
	if targetID == "" {
		return
	}
	ensureRewardWalletMap(ledger)
	// `pinnedAddr` and `ok` store whether the related condition is satisfied.
	if pinnedAddr, ok := pinnedGenesisRewardWallet(targetID); ok {
		ledger.ValidatorRewardWallets[targetID] = pinnedAddr
		return
	}

	// `totals` stores the measured quantity used by this operation.
	totals := validatorStakeTotals(ledger, targetID)
	if len(totals) == 0 {
		delete(ledger.ValidatorRewardWallets, targetID)
		return
	}

	// `bound` stores the value produced by this operation.
	if bound := strings.TrimSpace(ledger.ValidatorRewardWallets[targetID]); bound != "" {
		if totals[bound] > 0 {
			return
		}
	}

	// `bestAddr` and `ok` store whether the related condition is satisfied.
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
	// `targetAddr` stores the address used by this operation.
	targetAddr := canonicalAddressKey(walletAddr)
	if targetAddr == "" {
		return "", false
	}
	ensureStakeMap(ledger)
	// `k` and `rec` track the current values while iterating.
	for k, rec := range ledger.Stakes {
		if rec.Amount <= 0 {
			continue
		}
		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(k, "|", 2)
		if len(parts) != 2 {
			continue
		}
		if !addressesEqual(parts[0], targetAddr) {
			continue
		}
		// `vid` stores the value produced by this operation.
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
		// `addr` and `ok` store whether the related condition is satisfied.
		if addr, ok := genesisRewardWallet(validatorID); ok {
			return addr, true
		}
		return "", false
	}
	// `targetID` stores the value produced by this operation.
	targetID := normalizeRewardValidatorID(validatorID)
	if targetID == "" {
		return "", false
	}
	ensureStakeMap(ledger)
	// `pinnedAddr` and `ok` store whether the related condition is satisfied.
	if pinnedAddr, ok := pinnedGenesisRewardWallet(targetID); ok {
		return pinnedAddr, true
	}

	// 1) Explicit binding set by deterministic state transition.
	if bound := strings.TrimSpace(ledger.ValidatorRewardWallets[targetID]); bound != "" {
		// `totals` stores the measured quantity used by this operation.
		totals := validatorStakeTotals(ledger, targetID)
		if len(totals) == 0 || totals[bound] > 0 {
			return bound, true
		}
	}

	// 2) Fallback to deterministic top staker.
	totals := validatorStakeTotals(ledger, targetID)

	if len(totals) == 0 {
		// `addr` and `ok` store whether the related condition is satisfied.
		if addr, ok := genesisRewardWallet(targetID); ok {
			return addr, true
		}
		return "", false
	}
	// `bestAddr` and `ok` store whether the related condition is satisfied.
	if bestAddr, ok := pickDeterministicTopStakeWallet(totals); ok {
		return bestAddr, true
	}
	// `addr` and `ok` store whether the related condition is satisfied.
	if addr, ok := genesisRewardWallet(targetID); ok {
		return addr, true
	}
	return "", false
}

// minUnstakeEpochs converts the protocol unstake maturity window into consensus epochs.
func minUnstakeEpochs() uint64 {
	// `secondsPerEpoch` stores the value produced by this operation.
	secondsPerEpoch := ConsensusEpochDuration.Seconds()
	// `totalSeconds` stores the measured quantity used by this operation.
	totalSeconds := float64(MinUnstakeMonths) * float64(DaysPerMonth) * 24 * 3600
	return uint64(math.Ceil(totalSeconds / secondsPerEpoch))
}

// ExecuteTransaction applies one transaction using the current runtime validator registry snapshot.
func ExecuteTransaction(
	ledger *Ledger,
	tx Transaction,
	height int,
) (Ledger, error) {
	return executeTransactionWithValidatorRegistry(ledger, tx, height, GlobalValidatorRegistry.Snapshot())
}

// executeTransactionWithValidatorRegistry executes against an explicit
// validator-registry view. Consensus block paths pass the committed parent
// snapshot; the public wrapper retains runtime compatibility for non-block
// callers such as local mempool simulation.
func executeTransactionWithValidatorRegistry(
	ledger *Ledger,
	tx Transaction,
	height int,
	validatorRegistry map[string]ValidatorRecord,
) (Ledger, error) {
	return executeTransactionWithValidatorRegistryProtocol(ledger, tx, height, validatorRegistry, blockProtocolVersionV1, 0)
}

// executeTransactionWithValidatorRegistryProtocol is the consensus execution
// entrypoint. Protocol selection is carried by the block header.
func executeTransactionWithValidatorRegistryProtocol(
	ledger *Ledger,
	tx Transaction,
	height int,
	validatorRegistry map[string]ValidatorRecord,
	protocolVersion uint32,
	featureBitmap uint64,
) (Ledger, error) {
	if ledger == nil {
		return Ledger{}, errors.New("ledger is nil")
	}
	normalizeIncomingTx(&tx)
	if err := validateRemovedVMEnvelope(tx); err != nil {
		return *ledger, err
	}
	if !isProtocolChainID(tx.ChainID) {
		return *ledger, fmt.Errorf("invalid chain id: %s", tx.ChainID)
	}
	if ledger.Balances == nil {
		ledger.Balances = make(map[string]int)
	}
	if ledger.Nonces == nil {
		ledger.Nonces = make(map[string]int)
	}
	ensureStakeMap(ledger)
	ensureDTLState(ledger)
	ensureValidatorUpdateCertLedgerState(ledger)

	if tx.Type == TxFaucet {
		if !protocolFaucetEnabled() {
			return *ledger, errors.New("faucet disabled on mainnet")
		}
		if !isAllowedFaucetSource(tx.From) {
			return *ledger, errors.New("faucet source invalid")
		}
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
	if tx.Type == TxDTL {
		// `err` stores the error produced by this operation.
		if err := validateNativeDTLTransactionVersionForLedger(ledger, tx, uint64(height), blockDTLV2Enabled(protocolVersion, featureBitmap)); err != nil {
			return *ledger, err
		}
	} else if tx.Type == TxValidatorUpdate {
		// `err` stores the error produced by this operation.
		if err := validatorUpdateEnvelopeBasicError(tx, ledger, uint64(height)); err != nil {
			return *ledger, err
		}
	} else if tx.Amount <= 0 {
		return *ledger, errors.New("invalid amount or fee")
	}

	// `coin` stores the value produced by this operation.
	coin := normalizeCoin(tx.Coin)
	if !isProtocolCoinAllowed(coin) {
		return *ledger, errors.New("unsupported coin")
	}

	// `requiredFee` stores the request data being processed.
	requiredFee := requiredFeeForTxWithLedger(ledger, tx)
	if tx.Type == TxDTL {
		// `err` stores the error produced by this operation.
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
		// `validatorID` stores whether the related condition is satisfied.
		validatorID := strings.TrimSpace(tx.To)
		if validatorID == "" {
			return *ledger, errors.New("missing validator id")
		}
		// `other` and `ok` store whether the related condition is satisfied.
		if other, ok := walletBoundValidator(ledger, tx.From, validatorID); ok {
			return *ledger, fmt.Errorf("wallet already bound to validator %s", other)
		}
		// `lockEpochs` stores the synchronization state protecting shared data.
		lockEpochs := tx.StakeEpochs
		if lockEpochs == 0 {
			lockEpochs = DefaultStakeLockEpochs
		}
		// `minEpochs` stores the value produced by this operation.
		minEpochs := minUnstakeEpochs()
		if lockEpochs < minEpochs {
			return *ledger, fmt.Errorf("stake lock too short: min %d epochs", minEpochs)
		}
		// `consensusPubKey` and `err` store the error produced by this operation.
		consensusPubKey, err := validateStakeConsensusPubKey(tx, validatorRegistry)
		if err != nil {
			return *ledger, err
		}
		// `err` stores the error produced by this operation.
		if err := validateValidatorMinimumStakeAfterTx(ledger, tx); err != nil {
			return *ledger, err
		}
		// `totalCost` stores the measured quantity used by this operation.
		totalCost := tx.Amount + requiredFee
		if getBalance(*ledger, coin, tx.From) < totalCost {
			return *ledger, errors.New("insufficient balance")
		}

		addBalance(ledger, coin, tx.From, -totalCost)
		addBalance(ledger, coin, TREASURY_ADDRESS, requiredFee)

		// `lockUntil` stores the synchronization state protecting shared data.
		lockUntil := uint64(height) + lockEpochs
		// `key` stores the key used to access the related value.
		key := stakeKey(tx.From, validatorID)
		// `rec` stores the value produced by this operation.
		rec := ledger.Stakes[key]
		rec.ValidatorID = validatorID
		// `normalized` stores the value produced by this operation.
		if normalized := normalizeConsensusPubKeyHex(consensusPubKey); normalized != "" {
			rec.ConsensusPubKey = normalized
		}
		rec.Amount += tx.Amount
		if lockUntil > rec.LockedUntil {
			rec.LockedUntil = lockUntil
		}
		ledger.Stakes[key] = rec
		setValidatorRewardWallet(ledger, validatorID, tx.From)
		refreshValidatorRewardWalletBinding(ledger, validatorID)

	case TxUnstake:
		// `validatorID` stores whether the related condition is satisfied.
		validatorID := strings.TrimSpace(tx.To)
		if validatorID == "" {
			return *ledger, errors.New("missing validator id")
		}
		// `key` stores the key used to access the related value.
		key := stakeKey(tx.From, validatorID)
		// `rec` and `ok` store whether the related condition is satisfied.
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

	case TxDTL:
		if getBalance(*ledger, coin, tx.From) < requiredFee {
			return *ledger, errors.New("insufficient balance")
		}
		// `err` stores the error produced by this operation.
		if err := applyDTLTransactionVersion(ledger, tx, height, blockDTLV2Enabled(protocolVersion, featureBitmap)); err != nil {
			return *ledger, err
		}
		addBalance(ledger, coin, tx.From, -requiredFee)
		addBalance(ledger, coin, TREASURY_ADDRESS, requiredFee)

	default:
		// `totalCost` stores the measured quantity used by this operation.
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

// HashMempool hashes mempool.
func HashMempool(m *Mempool) string {
	// `ids` stores the current position in the related collection.
	ids := make([]string, 0, len(m.Transactions))
	// `tx` tracks the transaction data handled by this operation.
	for _, tx := range m.Transactions {
		ids = append(ids, tx.ID)
	}
	sort.Strings(ids)

	// `b` stores the value used by this operation.
	var b strings.Builder
	// `id` tracks the current position in the related collection.
	for _, id := range ids {
		b.WriteString(id)
		b.WriteString(";")
	}

	// `h` stores the value produced by this operation.
	h := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(h[:])
}

// SettleReward implements the settle reward helper.
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

	// `workerReward` stores the value produced by this operation.
	workerReward := total * 60 / 100
	// `userReward` stores the value produced by this operation.
	userReward := total * 10 / 100
	// `ownerReward` stores the value produced by this operation.
	ownerReward := total * 15 / 100
	// `validatorPool` stores whether the related condition is satisfied.
	validatorPool := total - (workerReward + userReward + ownerReward)

	change := SupplyChange{}
	change.Mint(&ledger, worker, int64(workerReward))
	change.Mint(&ledger, user, int64(userReward))
	change.Mint(&ledger, owner, int64(ownerReward))

	// `perValidator` stores the value produced by this operation.
	perValidator := validatorPool / len(validators)
	// `v` tracks the current values while iterating.
	for _, v := range validators {
		change.Mint(&ledger, v, int64(perValidator))
	}

	return ledger
}

// ValidateTransaction checks transaction shape, protocol coin authority, fees, balances, and signatures before mempool admission.
func (m *Mempool) ValidateTransaction(
	tx Transaction,
	ledger *Ledger,
) error {
	return m.ValidateTransactionAtHeight(tx, ledger, 0)
}

// ValidateTransactionAtHeight validates a transaction against the execution height used by block rules.
func (m *Mempool) ValidateTransactionAtHeight(
	tx Transaction,
	ledger *Ledger,
	executionHeight uint64,
) error {
	if ledger == nil {
		return errors.New("ledger is nil")
	}
	if err := validateRemovedVMEnvelope(tx); err != nil {
		return err
	}
	ensureStakeMap(ledger)
	ensureDTLState(ledger)
	ensureValidatorUpdateCertLedgerState(ledger)

	if tx.Type == TxFaucet {
		if !protocolFaucetEnabled() {
			return errors.New("faucet disabled on mainnet")
		}
		if !isAllowedFaucetSource(tx.From) {
			return errors.New("faucet source invalid")
		}
	}
	// -----------------------------------
	// 1ï¸âƒ£ BASIC SANITY
	// -----------------------------------
	if tx.Type == TxDTL {
		// `err` stores the error produced by this operation.
		if err := validateDTLTransaction(ledger, tx, executionHeight); err != nil {
			return err
		}
	} else if tx.Type == TxValidatorUpdate {
		if executionHeight == 0 {
			executionHeight = 1
		}
		// `err` stores the error produced by this operation.
		if err := validatorUpdateEnvelopeBasicError(tx, ledger, executionHeight); err != nil {
			return err
		}
	} else if tx.Amount <= 0 {
		return errors.New("invalid amount")
	}

	// `coin` stores the value produced by this operation.
	coin := normalizeCoin(tx.Coin)
	if !isProtocolCoinAllowed(coin) {
		return fmt.Errorf("unsupported coin: %s", coin)
	}

	// `requiredFee` stores the request data being processed.
	requiredFee := requiredFeeForTxWithLedger(ledger, tx)
	if tx.Type == TxDTL {
		// `err` stores the error produced by this operation.
		if err := validateDTLFeeBounds(tx.Fee, requiredFee); err != nil {
			return err
		}
	} else if tx.Fee != requiredFee {
		return fmt.Errorf("invalid fee: got %d expected %d", tx.Fee, requiredFee)
	}

	switch tx.Type {
	case TxStake:
		// `validatorID` stores whether the related condition is satisfied.
		validatorID := strings.TrimSpace(tx.To)
		if validatorID == "" {
			return errors.New("missing validator id")
		}
		// `other` and `ok` store whether the related condition is satisfied.
		if other, ok := walletBoundValidator(ledger, tx.From, validatorID); ok {
			return fmt.Errorf("wallet already bound to validator %s", other)
		}
		// `lockEpochs` stores the synchronization state protecting shared data.
		lockEpochs := tx.StakeEpochs
		if lockEpochs == 0 {
			lockEpochs = DefaultStakeLockEpochs
		}
		if lockEpochs < minUnstakeEpochs() {
			return errors.New("stake lock too short")
		}
		// `err` stores the error produced by this operation.
		if _, err := validateStakeConsensusPubKey(tx, GlobalValidatorRegistry.Snapshot()); err != nil {
			return err
		}
		// `err` stores the error produced by this operation.
		if err := validateValidatorMinimumStakeAfterTx(ledger, tx); err != nil {
			return err
		}
	case TxUnstake:
		// `validatorID` stores whether the related condition is satisfied.
		validatorID := strings.TrimSpace(tx.To)
		if validatorID == "" {
			return errors.New("missing validator id")
		}
		if tx.Amount <= requiredFee {
			return errors.New("unstake amount must exceed fee")
		}
		// `key` stores the key used to access the related value.
		key := stakeKey(tx.From, validatorID)
		// `rec` and `ok` store whether the related condition is satisfied.
		if rec, ok := ledger.Stakes[key]; !ok || rec.Amount < tx.Amount {
			return errors.New("insufficient staked balance")
		}
	case TxDTL:
		// No additional account/validator checks.
	}

	// -----------------------------------
	// 2ï¸âƒ£ CHAIN BINDING (ANTI-REPLAY)
	// -----------------------------------
	if !isProtocolChainID(tx.ChainID) {
		return fmt.Errorf("invalid chain id: %s", tx.ChainID)
	}

	// `now` stores the value produced by this operation.
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
	} else {
		// `pubKeyBytes` and `err` store the error produced by this operation.
		pubKeyBytes, err := hex.DecodeString(tx.PublicKey)
		if err != nil {
			return errors.New("invalid public key encoding")
		}

		if !AddressMatchesPublicKey(tx.From, ed25519.PublicKey(pubKeyBytes)) {
			return errors.New("address/public key mismatch")
		}

		// `pubKey` and `err` store the error produced by this operation.
		pubKey, err := DecodePublicKey(tx.PublicKey)
		if err != nil {
			return errors.New("invalid public key")
		}

		// `sig` and `err` store the error produced by this operation.
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
	case TxDTL:
		required = tx.Fee
	}

	if required > 0 && getBalance(*ledger, coin, tx.From) < required {
		return errors.New("insufficient balance")
	}

	return nil
}

// validateValidatorUpdateTx validates validator update tx.
func validateValidatorUpdateTx(tx Transaction, ledger *Ledger) bool {
	return validatorUpdateEnvelopeBasicError(tx, ledger, 1) == nil
}

// matchesLegacySignedTxID implements the matches legacy signed tx id helper.
func matchesLegacySignedTxID(tx Transaction, providedID string) bool {
	providedID = strings.TrimSpace(providedID)
	if providedID == "" {
		return false
	}
	// `sigHex` stores the value produced by this operation.
	sigHex := strings.TrimSpace(tx.Signature)
	if sigHex == "" {
		return false
	}
	// `sig` and `err` store the error produced by this operation.
	sig, err := hex.DecodeString(sigHex)
	if err != nil || len(sig) == 0 {
		return false
	}
	// `payload` stores the value produced by this operation.
	payload := TxPayload(tx)
	// `legacy` stores the value produced by this operation.
	legacy := sha256.Sum256(append(payload, sig...))
	if strings.EqualFold(providedID, hex.EncodeToString(legacy[:])) {
		return true
	}
	// `legacyPayload` stores the value produced by this operation.
	legacyPayload := TxPayloadLegacy(tx)
	// `legacyLegacy` stores the value produced by this operation.
	legacyLegacy := sha256.Sum256(append(legacyPayload, sig...))
	return strings.EqualFold(providedID, hex.EncodeToString(legacyLegacy[:]))
}

// normalizeIncomingTx normalizes incoming tx.
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
	tx.DTLTxType = strings.TrimSpace(tx.DTLTxType)
	tx.DTLTokenID = strings.TrimSpace(tx.DTLTokenID)
	tx.DTLPayload = strings.TrimSpace(tx.DTLPayload)
	tx.DTLGovernanceCert = strings.TrimSpace(tx.DTLGovernanceCert)
	if tx.ValidatorUpdateCert != nil {
		normalizeValidatorUpdateCert(tx.ValidatorUpdateCert)
	}
	if tx.ChainID == "" {
		tx.ChainID = protocolChainID()
	}
}

// validateTransactionShape validates transaction shape.
func validateTransactionShape(tx Transaction) error {
	if len(tx.ID) > 0 {
		if len(tx.ID) != MaxTxIDHexLen {
			return errors.New("invalid tx id length")
		}
		// `err` stores the error produced by this operation.
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
		// `err` stores the error produced by this operation.
		if err := validatorUpdateCertShapeError(tx.ValidatorUpdateCert); err != nil {
			return err
		}
	}
	if tx.Type != TxDTL &&
		(tx.DTLTxType != "" ||
			tx.DTLTokenID != "" ||
			tx.DTLPayload != "" ||
			tx.DTLGovernanceCert != "") {
		return errors.New("dtl envelope requires dtl transaction type")
	}
	if tx.Type == TxDTL {
		if tx.DTLTxType == "" {
			return errors.New("missing dtl_tx_type")
		}
		if tx.DTLPayload == "" {
			return errors.New("missing dtl_payload")
		}
	}
	if tx.Type != TxFaucet {
		if len(tx.PublicKey) != MaxTxPubKeyHexLen {
			return errors.New("invalid public key length")
		}
		if len(tx.Signature) != MaxTxSignatureHexLen {
			return errors.New("invalid signature length")
		}
	}
	return nil
}

// transactionUsesRemovedVMEnvelope reports the permanently reserved legacy VM
// transaction type. Former envelope fields are no longer part of Transaction.
func transactionUsesRemovedVMEnvelope(tx Transaction) bool {
	return tx.Type == removedLegacyVMTxType
}

// validateRemovedVMEnvelope rejects any new transaction carrying
// the removed EVM/VM surface before it can mutate local mempool/execution state.
func validateRemovedVMEnvelope(tx Transaction) error {
	if transactionUsesRemovedVMEnvelope(tx) {
		return errors.New("evm/vm removed permanently")
	}
	return nil
}

// ReceiveTransaction implements the receive transaction helper.
func (n *Node) ReceiveTransaction(tx Transaction) bool {
	// `ok` stores whether the related condition is satisfied.
	ok, _ := n.ReceiveTransactionWithReason(tx)
	return ok
}

// ReceiveTransactionWithReason implements the receive transaction with reason helper.
func (n *Node) ReceiveTransactionWithReason(tx Transaction) (bool, string) {
	normalizeIncomingTx(&tx)
	// `err` stores the error produced by this operation.
	if err := validateRemovedVMEnvelope(tx); err != nil {
		return false, err.Error()
	}
	// `err` stores the error produced by this operation.
	if err := validateTransactionShape(tx); err != nil {
		return false, err.Error()
	}
	// Mempool admission must validate against a single immutable snapshot of the
	// authoritative MSC ledger. This snapshot is local working state only; it
	// does not register or derive any legacy EVM aliases.
	executionLedger := n.currentExecutionLedgerClone()
	// `currentHeight` stores the latest committed execution height.
	currentHeight := uint64(0)
	if n.Blockchain != nil {
		currentHeight = n.Blockchain.Height()
	}
	// `finalized` stores the finalized height used by this operation.
	if finalized := n.getFinalizedHeight(); finalized > currentHeight {
		currentHeight = finalized
	}
	// `execHeight` stores the height at which mempool-admitted txs will execute.
	execHeight := currentHeight + 1

	// `canonicalID` stores the value produced by this operation.
	canonicalID := ComputeTxID(tx)
	// `legacyCanonicalID` stores the value produced by this operation.
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
	if err := n.Mempool.ValidateTransactionAtHeight(tx, &executionLedger, execHeight); err != nil {
		if DebugConsensus {
			fmt.Println("âŒ TX rejected:", err.Error())
		}
		return false, err.Error()
	}

	// =====================================================
	// 3ï¸âƒ£ ADD TO MEMPOOL (DETERMINISTIC)
	// =====================================================
	if tx.Type == TxValidatorUpdate {
		// `ctx` stores the context controlling this operation.
		ctx := n.newValidatorUpdateExecutionContext(execHeight)
		if ctx == nil {
			return false, "validator updates disabled"
		}
		// `ledgerCopy` stores the value produced by this operation.
		ledgerCopy := executionLedger.Clone()
		// `err` stores the error produced by this operation.
		if _, err := ExecuteTransactionWithNodeContext(n, ctx, &ledgerCopy, tx, int(execHeight)); err != nil {
			return false, err.Error()
		}
	}
	// `ok` and `reason` store whether the related condition is satisfied.
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

// appendLedgerHashMaterial implements the append ledger hash material helper.
func appendLedgerHashMaterial(b *strings.Builder, ledger Ledger) {
	if b == nil {
		return
	}

	// `keys` stores the key used to access the related value.
	keys := make([]string, 0, len(ledger.Balances))
	// `k` tracks the current values while iterating.
	for k := range ledger.Balances {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// `k` tracks the current values while iterating.
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(strconv.Itoa(ledger.Balances[k]))
		b.WriteString(";")
	}

	if len(ledger.Stakes) > 0 {
		// `stakeKeys` stores the value produced by this operation.
		stakeKeys := make([]string, 0, len(ledger.Stakes))
		// `k` tracks the current values while iterating.
		for k := range ledger.Stakes {
			stakeKeys = append(stakeKeys, k)
		}
		sort.Strings(stakeKeys)
		// `k` tracks the current values while iterating.
		for _, k := range stakeKeys {
			// `rec` stores the value produced by this operation.
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
		// `rewardCanonical` stores the value produced by this operation.
		rewardCanonical := make(map[string]string, len(ledger.ValidatorRewardWallets))
		// `rawVID` and `rawAddr` track the address used by this operation.
		for rawVID, rawAddr := range ledger.ValidatorRewardWallets {
			// `norm` stores the value produced by this operation.
			norm := normalizeRewardValidatorID(rawVID)
			// `addr` stores the address used by this operation.
			addr := strings.TrimSpace(rawAddr)
			if norm == "" || addr == "" {
				continue
			}
			// `existing` and `ok` store whether the related condition is satisfied.
			if existing, ok := rewardCanonical[norm]; !ok || addr < existing {
				rewardCanonical[norm] = addr
			}
		}

		// `rewardKeys` stores the value produced by this operation.
		rewardKeys := make([]string, 0, len(rewardCanonical))
		// `vid` tracks the current values while iterating.
		for vid := range rewardCanonical {
			// `norm` stores the value produced by this operation.
			norm := normalizeRewardValidatorID(vid)
			if norm == "" {
				continue
			}
			rewardKeys = append(rewardKeys, norm)
		}
		sort.Strings(rewardKeys)
		// `vid` tracks the current values while iterating.
		for _, vid := range rewardKeys {
			// `addr` stores the address used by this operation.
			addr := rewardCanonical[vid]
			b.WriteString("reward|")
			b.WriteString(vid)
			b.WriteString("=")
			b.WriteString(addr)
			b.WriteString(";")
		}
	}

	if len(ledger.UsedValidatorUpdateCerts) > 0 {
		// `certKeys` stores the value produced by this operation.
		certKeys := make([]string, 0, len(ledger.UsedValidatorUpdateCerts))
		// `key` tracks the key used to access the related value.
		for key := range ledger.UsedValidatorUpdateCerts {
			key = strings.ToLower(strings.TrimSpace(key))
			if len(key) != 64 {
				continue
			}
			certKeys = append(certKeys, key)
		}
		sort.Strings(certKeys)
		// `key` tracks the key used to access the related value.
		for _, key := range certKeys {
			b.WriteString("validator_update_cert|")
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(strconv.FormatUint(ledger.UsedValidatorUpdateCerts[key], 10))
			b.WriteString(";")
		}
	}

	if len(ledger.UsedBridgeEvents) > 0 {
		bridgeCanonical := make(map[string]uint64, len(ledger.UsedBridgeEvents))
		for rawKey, height := range ledger.UsedBridgeEvents {
			key := strings.ToLower(strings.TrimSpace(rawKey))
			if key == "" {
				continue
			}
			if existing, ok := bridgeCanonical[key]; !ok || height < existing {
				bridgeCanonical[key] = height
			}
		}
		bridgeKeys := make([]string, 0, len(bridgeCanonical))
		for key := range bridgeCanonical {
			bridgeKeys = append(bridgeKeys, key)
		}
		sort.Strings(bridgeKeys)
		for _, key := range bridgeKeys {
			b.WriteString("bridge_event|")
			b.WriteString(key)
			b.WriteString("=")
			b.WriteString(strconv.FormatUint(bridgeCanonical[key], 10))
			b.WriteString(";")
		}
	}

	appendDTLStateHashMaterial(b, ledger.DTL)
}

// canonicalLedgerHashMaterial returns canonical ledger hash material.
func canonicalLedgerHashMaterial(ledger Ledger) string {
	// `b` stores the value used by this operation.
	var b strings.Builder
	appendLedgerHashMaterial(&b, ledger)
	return b.String()
}

// HashLedger returns the deterministic state hash for all consensus-relevant ledger material.
func HashLedger(ledger Ledger) string {
	// `material` stores the value produced by this operation.
	material := canonicalLedgerHashMaterial(ledger)
	// `hash` stores the digest used to identify or verify the related data.
	hash := sha256.Sum256([]byte(material))
	return hex.EncodeToString(hash[:])
}

// LedgerStateMerkleRoot implements the ledger state merkle root helper.
func LedgerStateMerkleRoot(ledger Ledger) string {
	// `material` stores the value produced by this operation.
	material := canonicalLedgerHashMaterial(ledger)
	if strings.TrimSpace(material) == "" {
		// `sum` stores the value produced by this operation.
		sum := sha256.Sum256([]byte("ledger:empty"))
		return hex.EncodeToString(sum[:])
	}
	// `parts` stores the value produced by this operation.
	parts := strings.Split(material, ";")
	// `leaves` stores the value produced by this operation.
	leaves := make([]string, 0, len(parts))
	// `part` tracks the current values while iterating.
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// `sum` stores the value produced by this operation.
		sum := sha256.Sum256([]byte(part))
		leaves = append(leaves, hex.EncodeToString(sum[:]))
	}
	return merkleRootFromHexLeaves(leaves)
}

// normalizeCoin normalizes coin.
func normalizeCoin(coin string) string {
	if coin == "" {
		return CoinSymbol
	}
	return coin
}

// isProtocolCoinAllowed limits consensus-valid coins to the immutable protocol coin set.
func isProtocolCoinAllowed(coin string) bool {
	switch normalizeCoin(coin) {
	case CoinSymbol, AltCoinSymbol:
		return true
	default:
		return false
	}
}

// balanceKey implements the balance key helper.
func balanceKey(coin, addr string) string {
	return normalizeCoin(coin) + "|" + canonicalAddressKey(addr)
}

// nonceKey implements the nonce key helper.
func nonceKey(addr string) string {
	return canonicalAddressKey(addr)
}

// getNonce implements the get nonce helper.
func getNonce(ledger Ledger, addr string) int {
	return ledger.Nonces[nonceKey(addr)]
}

// setNonce implements the set nonce helper.
func setNonce(ledger *Ledger, addr string, nonce int) {
	if ledger == nil {
		return
	}
	if ledger.Nonces == nil {
		ledger.Nonces = make(map[string]int)
	}
	ledger.Nonces[nonceKey(addr)] = nonce
}

// getBalance implements the get balance helper.
func getBalance(ledger Ledger, coin, addr string) int {
	return ledger.Balances[balanceKey(coin, addr)]
}

// addBalance implements the add balance helper.
func addBalance(ledger *Ledger, coin, addr string, delta int) {
	// `key` stores the key used to access the related value.
	key := balanceKey(coin, addr)
	ledger.Balances[key] += delta
}

// currentCoinSupply returns current coin supply.
func currentCoinSupply(ledger *Ledger, coin string) int64 {
	if ledger == nil {
		return 0
	}
	// `symbol` stores the value produced by this operation.
	symbol := normalizeCoin(coin)
	// `prefix` stores the value produced by this operation.
	prefix := symbol + "|"
	// `total` stores the measured quantity used by this operation.
	total := int64(0)
	// `key` and `amount` track the key used to access the related value.
	for key, amount := range ledger.Balances {
		if strings.HasPrefix(key, prefix) {
			total += int64(amount)
		}
	}
	// Stake locks are maintained in MSC units.
	if symbol == CoinSymbol {
		// `rec` tracks the current values while iterating.
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

const supplyCapRepairActivationHeight uint64 = 34380

func supplyCapSurplus(ledger *Ledger) int64 {
	if ledger == nil {
		return 0
	}
	current := currentCoinSupply(ledger, CoinSymbol)
	if current <= FixedTotalSupply {
		return 0
	}
	return current - FixedTotalSupply
}

func supplyCapRepairReserveAddresses() []string {
	return []string{
		USER_REWARD_POOL,
		OWNER_ADDRESS,
		COMMUNITY_POOL,
		FOUNDATION_ADDRESS,
		VALIDATOR_BOOTSTRAP_POOL,
		TREASURY_ADDRESS,
		OwnerAddress,
	}
}

func applySupplyCapRepair(ledger *Ledger, height uint64) int64 {
	if ledger == nil || height < supplyCapRepairActivationHeight {
		return 0
	}
	remaining := supplyCapSurplus(ledger)
	if remaining <= 0 {
		return 0
	}
	burned := int64(0)
	for _, addr := range supplyCapRepairReserveAddresses() {
		if remaining <= 0 {
			break
		}
		bal := int64(getBalance(*ledger, CoinSymbol, addr))
		if bal <= 0 {
			continue
		}
		burn := bal
		if burn > remaining {
			burn = remaining
		}
		applied, err := ApplySupplyDelta(ledger, SupplyDelta{
			Coin:       CoinSymbol,
			BurnFrom:   addr,
			BurnAmount: burn,
			Reason:     "supply_cap_repair",
		})
		if err != nil || applied.Burned <= 0 {
			continue
		}
		burned += applied.Burned
		remaining -= applied.Burned
	}
	return burned
}

func isAllowedFaucetSource(address string) bool {
	switch strings.TrimSpace(address) {
	case USER_REWARD_POOL, VALIDATOR_BOOTSTRAP_POOL:
		return true
	default:
		return false
	}
}

// effectiveBurnFloorSupply returns effective burn floor supply.
func effectiveBurnFloorSupply() int64 {
	// `floor` stores the value produced by this operation.
	floor := protocolBurnStopSupplyValue()
	if floor < 0 {
		return 0
	}
	return floor
}

// burnCapacityForCoin implements the burn capacity for coin helper.
func burnCapacityForCoin(ledger *Ledger, coin string) int64 {
	if ledger == nil {
		return 0
	}
	// `current` stores the value produced by this operation.
	current := currentCoinSupply(ledger, coin)
	// `floor` stores the value produced by this operation.
	floor := effectiveBurnFloorSupply()
	if current <= floor {
		return 0
	}
	return current - floor
}

// burnCoinsFromAddress implements the burn coins from address helper.
func burnCoinsFromAddress(ledger *Ledger, coin, addr string, amount int64) int64 {
	if ledger == nil || amount <= 0 || strings.TrimSpace(addr) == "" {
		return 0
	}
	// `capacity` stores the value produced by this operation.
	capacity := burnCapacityForCoin(ledger, coin)
	if capacity <= 0 {
		return 0
	}
	if amount > capacity {
		amount = capacity
	}
	// `bal` stores the value produced by this operation.
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

// setBalance implements the set balance helper.
func setBalance(ledger *Ledger, coin, addr string, amount int) {
	// `key` stores the key used to access the related value.
	key := balanceKey(coin, addr)
	ledger.Balances[key] = amount
}

// ComputeTxFee calculates the deterministic protocol fee for a transfer amount.
func ComputeTxFee(amount int) int {
	if amount <= 0 {
		return 0
	}
	// `minBps` stores the value produced by this operation.
	minBps := ConsensusMinFeeBPS
	// `maxBps` stores the value produced by this operation.
	maxBps := ConsensusMaxFeeBPS
	// `floorAmt` stores the value produced by this operation.
	floorAmt := ConsensusFeeFloorAmount
	// `ceilAmt` stores the value produced by this operation.
	ceilAmt := ConsensusFeeCeilAmount

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

	// `bps` stores the value produced by this operation.
	bps := minBps
	if amount > floorAmt && maxBps > minBps {
		if amount >= ceilAmt {
			bps = maxBps
		} else {
			bps = minBps + (amount-floorAmt)*(maxBps-minBps)/(ceilAmt-floorAmt)
		}
	}

	// `fee` stores the value produced by this operation.
	fee := amount * bps / 10000
	if fee < 1 {
		fee = 1
	}
	return fee
}

// execQuorumRequired returns the deterministic execution-result quorum for a validator count.
func execQuorumRequired(total int) int {
	if total <= 0 {
		return 0
	}
	// `pct` stores the value produced by this operation.
	pct := ConsensusExecQuorumPercent
	// `required` stores the request data being processed.
	required := (total*pct + 99) / 100
	if required < 1 {
		required = 1
	}
	return required
}

// FinalizeHeight implements the finalize height helper.
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

// ShortHash implements the short hash helper.
func ShortHash(hash string) string {
	if len(hash) <= 8 {
		return hash
	}
	return hash[:8]
}

const (
	trustedConsensusMessageRatePerSecond = 500
	trustedConsensusMessageBurst         = 200
)

// checkRateLimit implements the check rate limit helper.
func checkRateLimit(peerAddr string, msgType string) bool {
	return checkRateLimitForPeer(peerAddr, msgType, false)
}

// checkRateLimitForPeer applies wider local-pressure limits to trusted
// consensus links while keeping the public/untrusted limiter unchanged.
func checkRateLimitForPeer(peerAddr string, msgType string, trustedPeer bool) bool {
	limiterMu.Lock()
	defer limiterMu.Unlock()

	// MODEL-3: deterministic + bounded keyspace
	if len(peerAddr) > 128 {
		return false
	}

	// `key` stores the key used to access the related value.
	key := peerAddr + ":" + msgType
	ratePerSecond := 100
	burst := 20
	if trustedPeer && isTrustedConsensusRateLimitMessage(msgType) {
		key += ":trusted"
		ratePerSecond = trustedConsensusMessageRatePerSecond
		burst = trustedConsensusMessageBurst
	}

	// `limiter` and `exists` store whether the related condition is satisfied.
	limiter, exists := messageLimiter[key]
	if !exists {
		limiter = rate.NewLimiter(rate.Every(time.Second/time.Duration(ratePerSecond)), burst)
		messageLimiter[key] = limiter
	}
	messageLimiterLastSeen[key] = time.Now()

	return limiter.Allow()
}

func isTrustedConsensusRateLimitMessage(msgType string) bool {
	switch strings.TrimSpace(msgType) {
	case MsgCommit, MsgExecutionResult, MsgLeaderBlock, MsgValidatorAnnounce:
		return true
	default:
		return false
	}
}

// allowTxFrom implements the allow tx from helper.
func allowTxFrom(addr string) bool {
	addr = canonicalAddressKey(addr)
	if addr == "" || MaxTxPerSecondPerSender <= 0 {
		return true
	}

	// `key` stores the key used to access the related value.
	key := "txsender:" + addr

	limiterMu.Lock()
	defer limiterMu.Unlock()

	// `limiter` and `exists` store whether the related condition is satisfied.
	limiter, exists := messageLimiter[key]
	if !exists {
		// `interval` stores the value currently being processed.
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

// Shutdown implements the shutdown helper.
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

	// `err` stores the error produced by this operation.
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

// ShutdownWithReason implements the shutdown with reason helper.
func (n *Node) ShutdownWithReason(reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "unspecified"
	}
	log.Printf("[SHUTDOWN] requested: reason=%s", reason)
	return n.Shutdown()
}

// applyStartupConsensusRecovery applies startup consensus recovery.
func (n *Node) applyStartupConsensusRecovery() {
	if n == nil {
		return
	}
	// `tip` stores the value produced by this operation.
	tip := uint64(0)
	if n.Blockchain != nil {
		tip = n.Blockchain.Height()
	}

	// `heights` stores the value produced by this operation.
	heights := []uint64{tip, tip + 1, n.currentEpoch()}
	// `seen` stores the value produced by this operation.
	seen := make(map[uint64]struct{}, len(heights))
	type recoveredValidatorSet struct {
		// `validators` stores whether the related condition is satisfied.
		validators []string
		// `hash` stores the digest used to identify or verify the related data.
		hash string
	}
	// `recoveredByHeight` stores the value produced by this operation.
	recoveredByHeight := make(map[uint64]recoveredValidatorSet, len(heights))
	// `h` tracks the current values while iterating.
	for _, h := range heights {
		if h == 0 {
			continue
		}
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		// `validators`, `setHash`, and `ok` store whether the related condition is satisfied.
		if validators, setHash, _, ok := n.resolveCommittedValidatorSetForHeight(h); ok && len(validators) > 0 {
			recoveredByHeight[h] = recoveredValidatorSet{
				validators: append([]string{}, canonicalValidatorIDs(validators)...),
				hash:       strings.TrimSpace(setHash),
			}
			continue
		}
		// `validators` stores whether the related condition is satisfied.
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
	// `h` and `recovered` track the current values while iterating.
	for h, recovered := range recoveredByHeight {
		// `validators` stores whether the related condition is satisfied.
		validators := canonicalValidatorIDs(recovered.validators)
		if len(validators) == 0 {
			continue
		}
		// `targetHash` stores the digest used to identify or verify the related data.
		targetHash := strings.TrimSpace(recovered.hash)
		// `existing` stores the value produced by this operation.
		existing := canonicalValidatorIDs(n.frozenValidatorsByHeight[h])
		// `existingMatches` stores the value produced by this operation.
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

// createSnapshotWithLedger implements the create snapshot with ledger helper.
func (n *Node) createSnapshotWithLedger(
	height uint64,
	blockHash string,
	snapshotLedger Ledger,
	ledgerStage string,
) (err error) {
	// `started` stores the value produced by this operation.
	started := time.Now()
	defer func() {
		n.observeSnapshotOperation("create", height, time.Since(started), err == nil)
	}()
	if n == nil || n.Blockchain == nil {
		return errors.New("snapshot_blockchain_unavailable")
	}
	// `block` stores the synchronization state protecting shared data.
	block := n.Blockchain.LastBlock()
	if block.ID != height {
		// `b` and `ok` store whether the related condition is satisfied.
		if b, ok := n.LoadBlock(int(height)); ok {
			block = b
		}
	}
	if blockHash == "" && block.BlockHash != "" {
		blockHash = block.BlockHash
	}
	snapshotLedger = snapshotLedger.Clone()
	// `ledgerHash` stores the digest used to identify or verify the related data.
	ledgerHash := HashLedger(snapshotLedger)
	// `stateRoot` stores the digest used to identify or verify the related data.
	if stateRoot := strings.TrimSpace(block.StateRoot); stateRoot != "" {
		// `computedRoot` stores the digest used to identify or verify the related data.
		computedRoot := ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
		if !strings.EqualFold(stateRoot, strings.TrimSpace(computedRoot)) {
			return fmt.Errorf("execution_snapshot_ledger_unavailable height=%d", height)
		}
	}
	// `stateRoot` stores the digest used to identify or verify the related data.
	stateRoot := block.StateRoot
	if stateRoot == "" {
		stateRoot = ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
	}

	// `validators` stores whether the related condition is satisfied.
	validators := make(map[string]bool)
	// `pendingValidators` stores the value produced by this operation.
	pendingValidators := make(map[string]uint64)
	// `pendingValidatorRemovals` stores the value produced by this operation.
	pendingValidatorRemovals := make(map[string]uint64)
	// `validatorSetHeight` stores whether the related condition is satisfied.
	validatorSetHeight := uint64(0)
	// Snapshots should carry the validator set for the *next* height.
	// Consensus uses snapshot(h) to derive validators for height h+1.
	nextHeight := height + 1
	// `anchorSetHash` stores the digest used to identify or verify the related data.
	anchorSetHash := strings.TrimSpace(block.NextValidatorSetHash)
	if anchorSetHash == "" {
		anchorSetHash = strings.TrimSpace(block.ValidatorSetHash)
	}
	// `list`, `validatorSetSource`, and `err` store the error produced by this operation.
	list, validatorSetSource, err := n.resolveSnapshotValidatorListWithSource(nextHeight, block)
	if err != nil {
		return err
	}
	validatorSetSource = normalizeCommittedValidatorAuthoritySource(validatorSetSource)
	if validatorSetSource == "none" {
		validatorSetSource = "chain_parent_commitment"
	}
	n.validatorSetMu.RLock()
	// `id` and `act` track the current position in the related collection.
	for id, act := range n.pendingValidators {
		// `norm` stores the value produced by this operation.
		norm := normalizeValidatorID(id)
		if norm == "" || act == 0 {
			continue
		}
		// `existing` and `ok` store whether the related condition is satisfied.
		if existing, ok := pendingValidators[norm]; !ok || act < existing {
			pendingValidators[norm] = act
		}
	}
	// `id` and `act` track the current position in the related collection.
	for id, act := range n.pendingValidatorRemovals {
		// `norm` stores the value produced by this operation.
		norm := normalizeValidatorID(id)
		if norm == "" || act == 0 {
			continue
		}
		// `existing` and `ok` store whether the related condition is satisfied.
		if existing, ok := pendingValidatorRemovals[norm]; !ok || act < existing {
			pendingValidatorRemovals[norm] = act
		}
	}
	validatorSetHeight = n.validatorSetHeight
	n.validatorSetMu.RUnlock()
	// `v` tracks the current values while iterating.
	for _, v := range list {
		validators[v] = true
	}
	// `nextValidatorSetHash` stores the digest used to identify or verify the related data.
	nextValidatorSetHash := strings.TrimSpace(block.NextValidatorSetHash)
	// `nextValidatorSetRoot` stores the digest used to identify or verify the related data.
	nextValidatorSetRoot := strings.TrimSpace(block.NextValidatorSetRoot)
	// `nextValidatorSetHeight` stores the value produced by this operation.
	nextValidatorSetHeight := blockActivationHeight(block)
	if nextValidatorSetHeight == 0 {
		nextValidatorSetHeight = nextHeight
	}
	// `registrySnapshot` stores the value produced by this operation.
	registrySnapshot := n.validatorRegistrySnapshotForHeight(nextHeight)
	// `registryHash` stores the digest used to identify or verify the related data.
	registryHash := strings.TrimSpace(ValidatorRegistrySnapshotHash(registrySnapshot))
	if len(registrySnapshot) == 0 && height > 0 {
		// `committedRegistry`, `committedHash`, `source`, and `ok` store whether the related condition is satisfied.
		if committedRegistry, committedHash, source, ok := n.resolveCommittedValidatorRegistrySnapshot(height); ok && (source == "live_tip_runtime_repair" || source == "tip_snapshot_repair") {
			registrySnapshot = committedRegistry
			registryHash = strings.TrimSpace(committedHash)
		}
	}
	if len(registrySnapshot) == 0 {
		// `chainHeight` stores the value produced by this operation.
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
	// `resolvedSetHash` stores the digest used to identify or verify the related data.
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
	// `stateValidators` stores the value produced by this operation.
	stateValidators := onChainValidatorsFromRegistrySnapshot(registrySnapshot, pendingValidators, height)
	// `setRoot` stores the digest used to identify or verify the related data.
	setRoot := ValidatorSetMerkleRoot(height, list, registrySnapshot)
	// `currentSetHash` stores the digest used to identify or verify the related data.
	currentSetHash := strings.TrimSpace(resolvedSetHash)
	if nextValidatorSetRoot == "" &&
		setRoot != "" &&
		nextValidatorSetHash != "" &&
		strings.EqualFold(strings.TrimSpace(nextValidatorSetHash), currentSetHash) {
		nextValidatorSetRoot = setRoot
	}
	// `finalizedHash` stores the digest used to identify or verify the related data.
	finalizedHash := ""
	if block.FinalizedHeight > 0 {
		finalizedHash = strings.TrimSpace(block.BlockHash)
	}
	// `snapshot` stores the value produced by this operation.
	snapshot := StateSnapshot{
		Version:          SnapshotVersion,
		Height:           height,
		BlockHash:        blockHash,
		StateRoot:        stateRoot,
		StateMerkleRoot:  LedgerStateMerkleRoot(snapshotLedger),
		LedgerHash:       ledgerHash,
		LedgerStage:      ledgerStage,
		GenesisHash:      GenesisHash,
		PrevHash:         block.PrevHash,
		BlockProposer:    strings.TrimSpace(block.Proposer),
		BlockMempoolRoot: strings.TrimSpace(block.MempoolRoot),
		BlockEpoch:       block.BlockTime.Epoch,

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

// CreateSnapshot creates snapshot.
func (n *Node) CreateSnapshot(
	height uint64,
	blockHash string,
) error {

	// MODEL-2 / MODEL-3:
	// Snapshot = execution cache ONLY
	snapshotLedger := Ledger{}
	// `ledgerStage` stores the value produced by this operation.
	ledgerStage := ""
	// `block` stores the synchronization state protecting shared data.
	block := Block{}
	// `blockOK` stores whether the related condition is satisfied.
	blockOK := false
	if n != nil && n.Blockchain != nil && height > 0 {
		block, blockOK = n.Blockchain.GetBlock(height)
	}
	// `ledgerMatchesBlock` stores the synchronization state protecting shared data.
	ledgerMatchesBlock := func(ledger Ledger) bool {
		if !blockOK || strings.TrimSpace(block.StateRoot) == "" {
			return ledgerHasInitializedBacking(ledger)
		}
		// `ledgerHash` stores the digest used to identify or verify the related data.
		ledgerHash := HashLedger(ledger)
		// `expectedRoot` stores the digest used to identify or verify the related data.
		expectedRoot := ComputeExecHashVersioned(block, ledgerHash, executionStateRootVersionForHeight(block.ID))
		return strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(block.StateRoot))
	}
	// `cachedLedger` and `ok` store whether the related condition is satisfied.
	if cachedLedger, ok := n.cachedExecutionSnapshotLedger(height); ok {
		if ledgerMatchesBlock(cachedLedger) {
			snapshotLedger = cachedLedger
			ledgerStage = snapshotLedgerStageExecution
		}
	}
	if !ledgerHasInitializedBacking(snapshotLedger) {
		// `snap` and `ok` store whether the related condition is satisfied.
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
		// `liveLedger` stores the value produced by this operation.
		liveLedger := n.currentExecutionLedgerClone()
		if ledgerMatchesBlock(liveLedger) {
			snapshotLedger = liveLedger
			ledgerStage = snapshotLedgerStageExecution
			n.cacheExecutionSnapshotLedger(height, snapshotLedger)
		}
	}
	if !ledgerHasInitializedBacking(snapshotLedger) && n.startupExecutionSnapshotCanRebuildLocally(height) {
		// `err` stores the error produced by this operation.
		if err := n.rebuildTrustedExecutionSnapshotsUpTo(height); err != nil {
			return err
		}
		// `cachedLedger` and `ok` store whether the related condition is satisfied.
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

// cacheExecutionSnapshotLedger implements the cache execution snapshot ledger helper.
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
	// `cacheDepth` stores the value produced by this operation.
	cacheDepth := n.ledgerMemoryCacheDepth()
	// `removed` stores the value produced by this operation.
	removed := 0
	// `h` tracks the current values while iterating.
	for h := range n.snapshotExecutionLedgerByHeight {
		if h+cacheDepth <= height {
			delete(n.snapshotExecutionLedgerByHeight, h)
			removed++
		}
	}
	maybeReleaseMemoryAfterLedgerCachePrune(removed, height)
}

// cachedExecutionSnapshotLedger implements the cached execution snapshot ledger helper.
func (n *Node) cachedExecutionSnapshotLedger(height uint64) (Ledger, bool) {
	if n == nil || height == 0 {
		return Ledger{}, false
	}
	n.snapshotExecutionLedgerMu.Lock()
	defer n.snapshotExecutionLedgerMu.Unlock()
	if n.snapshotExecutionLedgerByHeight == nil {
		return Ledger{}, false
	}
	// `ledger` and `ok` store whether the related condition is satisfied.
	ledger, ok := n.snapshotExecutionLedgerByHeight[height]
	if !ok {
		return Ledger{}, false
	}
	return ledger.Clone(), true
}

// resolveSnapshotValidatorListWithSource implements the resolve snapshot validator list with source helper.
func (n *Node) resolveSnapshotValidatorListWithSource(nextHeight uint64, block Block) ([]string, string, error) {
	// `targetHash` stores the digest used to identify or verify the related data.
	targetHash := strings.TrimSpace(block.NextValidatorSetHash)
	if targetHash == "" {
		targetHash = strings.TrimSpace(block.ValidatorSetHash)
	}
	// `registrySnapshot` stores the value produced by this operation.
	registrySnapshot := n.validatorRegistrySnapshotForHeight(nextHeight)
	// `candidateMatchesTarget` stores the value produced by this operation.
	candidateMatchesTarget := func(values []string) ([]string, bool) {
		return n.validatorSetCandidateMatchesTarget(nextHeight, targetHash, values, registrySnapshot)
	}
	// `ctx` stores the context controlling this operation.
	if ctx := n.blockValidatorUpdatePlanContext(block); ctx != nil {
		// `planned` stores the value produced by this operation.
		if planned := ctx.plannedValidatorsForHeight(nextHeight); len(planned) > 0 {
			// `matched` and `ok` store whether the related condition is satisfied.
			if matched, ok := n.validatorSetCandidateMatchesTarget(nextHeight, targetHash, planned, ctx.registrySnapshot); ok {
				return matched, "chain_parent_commitment", nil
			}
		}
	}
	// `resolved`, `resolvedSource`, and `ok` store whether the related condition is satisfied.
	if resolved, _, resolvedSource, ok := n.resolveCommittedValidatorSetForHeight(nextHeight); ok && len(resolved) > 0 {
		// `matched` and `ok` store whether the related condition is satisfied.
		if matched, ok := candidateMatchesTarget(resolved); ok {
			// `source` stores the value produced by this operation.
			source := normalizeCommittedValidatorAuthoritySource(resolvedSource)
			if source == "none" {
				source = "chain_parent_commitment"
			}
			return matched, source, nil
		}
	}
	// `committed` and `ok` store whether the related condition is satisfied.
	if committed, ok := blockValidatorSetFromSignatures(block); ok {
		// `matched` and `ok` store whether the related condition is satisfied.
		if matched, ok := candidateMatchesTarget(committed); ok {
			return matched, "chain_parent_commitment", nil
		}
	}
	// `list` stores the value produced by this operation.
	list := n.GetConsensusValidators(int(nextHeight))
	// `matched` and `ok` store whether the related condition is satisfied.
	if matched, ok := candidateMatchesTarget(list); ok {
		return matched, "chain_parent_commitment", nil
	}
	// `frozen` stores the value produced by this operation.
	if frozen := n.frozenValidatorsForHeight(nextHeight); len(frozen) > 0 {
		// `matched` and `ok` store whether the related condition is satisfied.
		if matched, ok := candidateMatchesTarget(frozen); ok {
			return matched, "chain_parent_commitment", nil
		}
	}
	if block.ID > 0 {
		// `frozen` stores the value produced by this operation.
		if frozen := n.frozenValidatorsForHeight(block.ID); len(frozen) > 0 {
			// `matched` and `ok` store whether the related condition is satisfied.
			if matched, ok := candidateMatchesTarget(frozen); ok {
				return matched, "chain_parent_commitment", nil
			}
		}
	}
	if targetHash != "" {
		// `frozen` stores the value produced by this operation.
		if frozen := n.frozenValidatorsForCommittedHash(targetHash, nextHeight, block.ID); len(frozen) > 0 {
			return frozen, "chain_parent_commitment", nil
		}
	}
	if nextHeight > 1 {
		// `parentHeight` stores the value produced by this operation.
		parentHeight := nextHeight - 1
		// `parentRegistry` and `ok` store whether the related condition is satisfied.
		if parentRegistry, _, _, ok := n.resolveCommittedValidatorRegistrySnapshot(parentHeight); ok && len(parentRegistry) > 0 {
			// `parentSet` stores the value produced by this operation.
			parentSet := canonicalValidatorIDsFromMapKeys(parentRegistry)
			// `matched` and `ok` store whether the related condition is satisfied.
			if matched, ok := candidateMatchesTarget(parentSet); ok {
				return matched, "registry_verified", nil
			}
		}
	}
	// `legacyBlockCommitment` stores the value produced by this operation.
	legacyBlockCommitment := strings.TrimSpace(block.NextValidatorSetHash) == "" && blockActivationHeight(block) == 0
	if legacyBlockCommitment {
		// `boot` stores the value produced by this operation.
		if boot := n.currentValidatorSetSnapshot(); len(boot) > 0 {
			// `matched` and `ok` store whether the related condition is satisfied.
			if matched, ok := candidateMatchesTarget(boot); ok {
				return matched, "genesis_bootstrap", nil
			}
		}
	}
	list = nil
	if len(list) == 0 {
		// `chainHeight` stores the value produced by this operation.
		chainHeight := uint64(0)
		if n != nil && n.Blockchain != nil {
			chainHeight = n.Blockchain.Height()
		}
		// Bootstrap compatibility only: allow runtime/genesis seed during
		// earliest chain heights where historical committed signatures may
		// still be unavailable in-memory.
		if chainHeight <= 1 {
			// `boot` stores the value produced by this operation.
			if boot := n.currentValidatorSetSnapshot(); len(boot) > 0 {
				// `matched` and `ok` store whether the related condition is satisfied.
				if matched, ok := candidateMatchesTarget(boot); ok {
					return matched, "genesis_bootstrap", nil
				}
			}
			if len(list) == 0 && nextHeight <= 2 && len(n.GenesisValidators) > 0 {
				// `matched` and `ok` store whether the related condition is satisfied.
				if matched, ok := candidateMatchesTarget(n.GenesisValidators); ok {
					return matched, "genesis_bootstrap", nil
				}
			}
		}
	}
	if targetHash != "" {
		// `reconstructCandidates` stores the value produced by this operation.
		reconstructCandidates := make([][]string, 0, 3)
		// `committed` and `ok` store whether the related condition is satisfied.
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
		// `registryIDs` stores the value produced by this operation.
		if registryIDs := canonicalValidatorIDsFromMapKeys(GlobalValidatorRegistry.Snapshot()); len(registryIDs) > 0 {
			reconstructCandidates = append(reconstructCandidates, registryIDs)
		}
		// `boot` stores the value produced by this operation.
		if boot := n.currentValidatorSetSnapshot(); len(boot) > 0 {
			reconstructCandidates = append(reconstructCandidates, boot)
		}
		// `reconstructed` and `ok` store whether the related condition is satisfied.
		if reconstructed, ok := n.reconstructValidatorSetCandidateForTarget(nextHeight, targetHash, registrySnapshot, reconstructCandidates...); ok {
			return reconstructed, "chain_parent_commitment", nil
		}
		// `candidate` tracks the current values while iterating.
		for _, candidate := range reconstructCandidates {
			// `matched` and `ok` store whether the related condition is satisfied.
			if matched, ok := candidateMatchesTarget(candidate); ok {
				return matched, "chain_parent_commitment", nil
			}
		}
	}
	if targetHash != "" {
		return nil, "none", fmt.Errorf("snapshot_validator_set_unresolved next_height=%d target_hash=%s", nextHeight, targetHash)
	}
	// `matched` and `ok` store whether the related condition is satisfied.
	if matched, ok := candidateMatchesTarget(list); ok {
		return matched, "genesis_bootstrap", nil
	}
	if len(list) == 0 {
		return nil, "none", fmt.Errorf("snapshot_validator_set_unresolved next_height=%d", nextHeight)
	}
	return canonicalValidatorIDs(list), "genesis_bootstrap", nil
}

// resolveSnapshotValidatorList implements the resolve snapshot validator list helper.
func (n *Node) resolveSnapshotValidatorList(nextHeight uint64, block Block) ([]string, error) {
	// `list` and `err` store the error produced by this operation.
	list, _, err := n.resolveSnapshotValidatorListWithSource(nextHeight, block)
	return list, err
}

// GetSnapshot returns snapshot.
func (n *Node) GetSnapshot(height uint64) (snap *StateSnapshot, err error) {
	// `started` stores the value produced by this operation.
	started := time.Now()
	defer func() {
		n.observeSnapshotOperation("load", height, time.Since(started), err == nil)
	}()
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	// `key` stores the key used to access the related value.
	key := []byte(fmt.Sprintf("snapshot:%d", height))
	return readSnapshotFromStores(n.DB.SnapshotStoresForRead(), key)
}

// appendUniqueSnapshotAnchorCandidate implements the append unique snapshot anchor candidate helper.
func appendUniqueSnapshotAnchorCandidate(candidates []Block, seen map[string]struct{}, candidate Block) []Block {
	// `key` stores the key used to access the related value.
	key := strings.TrimSpace(candidate.BlockHash) + "|" +
		strings.TrimSpace(candidate.StateRoot) + "|" +
		strings.TrimSpace(candidate.ValidatorSetHash) + "|" +
		strings.TrimSpace(candidate.NextValidatorSetHash) + "|" +
		strings.TrimSpace(candidate.ValidatorRegistryHash) + "|" +
		strings.TrimSpace(candidate.PromotionWindowHash)
	// `ok` stores whether the related condition is satisfied.
	if _, ok := seen[key]; ok {
		return candidates
	}
	seen[key] = struct{}{}
	return append(candidates, candidate)
}

// localSnapshotAnchorCandidates implements the local snapshot anchor candidates helper.
func (n *Node) localSnapshotAnchorCandidates(height uint64) []Block {
	if n == nil || height == 0 {
		return nil
	}
	// `candidates` stores the value produced by this operation.
	candidates := make([]Block, 0, 2)
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, 2)
	if n.Blockchain != nil {
		// `blk` and `ok` store whether the related condition is satisfied.
		if blk, ok := n.Blockchain.GetBlock(height); ok {
			candidates = appendUniqueSnapshotAnchorCandidate(candidates, seen, blk)
		}
	}
	// `blk` and `ok` store whether the related condition is satisfied.
	if blk, ok := n.LoadBlock(int(height)); ok {
		candidates = appendUniqueSnapshotAnchorCandidate(candidates, seen, blk)
	}
	return candidates
}

// snapshotMatchesLocalAnchorDetailed implements the snapshot matches local anchor detailed helper.
func (n *Node) snapshotMatchesLocalAnchorDetailed(snapshot *StateSnapshot) (bool, string) {
	if n == nil || snapshot == nil {
		return false, "snapshot_metadata_invalid"
	}
	if strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)) == "" {
		return false, "registry_hash_mismatch"
	}
	// `ok` and `reason` store whether the related condition is satisfied.
	if ok, reason := n.verifySnapshotAgainstLocalBlockDetailed(snapshot); !ok {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			reason = "anchor_verification_failed"
		}
		return false, reason
	}
	// `candidates` stores the value produced by this operation.
	candidates := n.localSnapshotAnchorCandidates(snapshot.Height)
	if len(candidates) == 0 {
		return false, "anchor_block_unavailable"
	}
	// `blk` tracks the current values while iterating.
	for _, blk := range candidates {
		if !strings.EqualFold(strings.TrimSpace(blk.BlockHash), strings.TrimSpace(snapshot.BlockHash)) {
			continue
		}
		// `expectedRegistry` stores the value produced by this operation.
		expectedRegistry := strings.TrimSpace(blk.ValidatorRegistryHash)
		if expectedRegistry != "" && !strings.EqualFold(strings.TrimSpace(snapshotValidatorRegistryHash(snapshot)), expectedRegistry) {
			return false, "registry_hash_mismatch"
		}
		// `expectedPromotionWindow` stores the value produced by this operation.
		expectedPromotionWindow := strings.TrimSpace(blk.PromotionWindowHash)
		if expectedPromotionWindow != "" && !strings.EqualFold(strings.TrimSpace(snapshotPromotionWindowHash(snapshot)), expectedPromotionWindow) {
			return false, "promotion_window_hash_mismatch"
		}
		return true, ""
	}
	return false, "block_hash_mismatch"
}

// snapshotMatchesLocalAnchor implements the snapshot matches local anchor helper.
func (n *Node) snapshotMatchesLocalAnchor(snapshot *StateSnapshot) bool {
	// `ok` stores whether the related condition is satisfied.
	ok, _ := n.snapshotMatchesLocalAnchorDetailed(snapshot)
	return ok
}

// `retainedCommittedSnapshotCount` defines the measured quantity used by this operation.
const retainedCommittedSnapshotCount = 3

// protectedCommittedSnapshotMinHeight implements the protected committed snapshot min height helper.
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

// shouldProtectCommittedSnapshotHeight implements the should protect committed snapshot height helper.
func (n *Node) shouldProtectCommittedSnapshotHeight(height uint64, maxHeight uint64) bool {
	if n == nil || height == 0 {
		return false
	}
	if meta, err := n.loadSnapshotMetaRecord(height); err != nil || meta == nil || meta.Height != height {
		return false
	}
	// `minHeight` stores the value produced by this operation.
	minHeight := n.protectedCommittedSnapshotMinHeight(maxHeight)
	return minHeight > 0 && height >= minHeight
}

// deleteStoredSnapshotHeight implements the delete stored snapshot height helper.
func (n *Node) deleteStoredSnapshotHeight(height uint64) error {
	if n == nil || n.DB == nil || height == 0 {
		return nil
	}
	// `key` stores the key used to access the related value.
	key := []byte(fmt.Sprintf("snapshot:%d", height))
	// `latestKey` stores the key used to access the related value.
	latestKey := []byte("snapshot:latest")
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotStoresForRead() {
		// `err` stores the error produced by this operation.
		if err := store.Update(func(txn *Txn) error {
			// `err` stores the error produced by this operation.
			if err := txn.Delete(key); err != nil && !errors.Is(err, ErrKeyNotFound) {
				return err
			}
			return nil
		}); err != nil {
			return err
		}
	}
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		// `err` stores the error produced by this operation.
		if err := store.Update(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get(latestKey)
			if err == nil {
				// `current` stores the value used by this operation.
				var current []byte
				// `err` stores the error produced by this operation.
				if err := item.Value(func(val []byte) error {
					current = append([]byte{}, val...)
					return nil
				}); err != nil {
					return err
				}
				if bytes.Equal(current, key) {
					// `err` stores the error produced by this operation.
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

// refreshLatestSnapshotPointer implements the refresh latest snapshot pointer helper.
func (n *Node) refreshLatestSnapshotPointer() error {
	if n == nil || n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil
	}
	// `key` and `err` store the error produced by this operation.
	key, _, err := n.FindLatestSnapshotKey()
	if err != nil {
		if !errors.Is(err, ErrKeyNotFound) {
			return err
		}
		// `store` tracks the current values while iterating.
		for _, store := range n.DB.SnapshotMetaStoresForRead() {
			// `err` stores the error produced by this operation.
			if err := store.Update(func(txn *Txn) error {
				// `err` stores the error produced by this operation.
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

// scrubInvalidStoredSnapshots implements the scrub invalid stored snapshots helper.
func (n *Node) scrubInvalidStoredSnapshots(maxHeight uint64) (int, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return 0, nil
	}
	// `chainHeight` stores the value produced by this operation.
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

	// `invalidHeights` stores the current position in the related collection.
	invalidHeights := make(map[uint64]struct{})
	// `keys` and `err` store the error produced by this operation.
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), maxHeight)
	if err != nil {
		return 0, err
	}
	// `candidate` tracks the current values while iterating.
	for _, candidate := range keys {
		// `snapshot` and `err` store the error produced by this operation.
		snapshot, err := readSnapshotFromStores(n.DB.SnapshotStoresForRead(), candidate.key)
		if err != nil || snapshot == nil || !n.snapshotMatchesLocalAnchor(snapshot) {
			invalidHeights[candidate.height] = struct{}{}
		}
	}
	if len(invalidHeights) == 0 {
		return 0, nil
	}
	// `heights` stores the value produced by this operation.
	heights := make([]int, 0, len(invalidHeights))
	// `height` tracks the current values while iterating.
	for height := range invalidHeights {
		heights = append(heights, int(height))
	}
	sort.Ints(heights)
	// `removed` stores the value produced by this operation.
	removed := 0
	// `height` tracks the current values while iterating.
	for _, height := range heights {
		if n.shouldProtectCommittedSnapshotHeight(uint64(height), maxHeight) {
			continue
		}
		// `err` stores the error produced by this operation.
		if err := n.deleteStoredSnapshotHeight(uint64(height)); err != nil {
			return removed, err
		}
		removed++
	}
	// `err` stores the error produced by this operation.
	if err := n.refreshLatestSnapshotPointer(); err != nil {
		return removed, err
	}
	return removed, nil
}

// pruneStoredSnapshotsAboveHeight implements the prune stored snapshots above height helper.
func (n *Node) pruneStoredSnapshotsAboveHeight(height uint64) (int, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return 0, nil
	}
	// `heights` stores the value used by this operation.
	var heights []uint64
	// `keys` and `err` store the error produced by this operation.
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
	if err != nil {
		return 0, err
	}
	// `key` tracks the key used to access the related value.
	for _, key := range keys {
		if key.height > height {
			heights = append(heights, key.height)
		}
	}
	if len(heights) == 0 {
		return 0, nil
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	// `removed` stores the value produced by this operation.
	removed := 0
	// `h` tracks the current values while iterating.
	for _, h := range heights {
		// `err` stores the error produced by this operation.
		if err := n.deleteStoredSnapshotHeight(h); err != nil {
			return removed, err
		}
		removed++
	}
	// `err` stores the error produced by this operation.
	if err := n.pruneSnapshotMetaAboveHeight(height); err != nil {
		return removed, err
	}
	// `err` stores the error produced by this operation.
	if err := n.pruneSnapshotDeltasAboveHeight(height); err != nil {
		return removed, err
	}
	// `err` stores the error produced by this operation.
	if err := n.clearStaleTipSnapshotRecordsAboveHeight(height); err != nil {
		return removed, err
	}
	// `err` stores the error produced by this operation.
	if err := n.refreshLatestSnapshotPointer(); err != nil {
		return removed, err
	}
	return removed, nil
}

// verifiedStoredSnapshotAtOrBelow implements the verified stored snapshot at or below helper.
func (n *Node) verifiedStoredSnapshotAtOrBelow(targetHeight uint64) (*StateSnapshot, error) {
	if n == nil || n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	// `searchHeight` stores the value produced by this operation.
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
		// `snapshot` and `err` store the error produced by this operation.
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
		if n.shouldProtectCommittedSnapshotHeight(snapshot.Height, searchHeight) {
			if snapshot.Height <= 1 {
				break
			}
			searchHeight = snapshot.Height - 1
			continue
		}
		// `err` stores the error produced by this operation.
		if err := n.deleteStoredSnapshotHeight(snapshot.Height); err != nil {
			return nil, err
		}
		// `err` stores the error produced by this operation.
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

// GetLatestSnapshot returns latest snapshot.
func (n *Node) GetLatestSnapshot() (*StateSnapshot, error) {
	if n.DB == nil || n.DB.SnapshotMetaStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	// `key` stores the key used to access the related value.
	var key []byte
	// `lastErr` stores the error produced by this operation.
	var lastErr error
	// `store` tracks the current values while iterating.
	for _, store := range n.DB.SnapshotMetaStoresForRead() {
		// `err` stores the error produced by this operation.
		err := store.View(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
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

	// `bestKey` stores the key used to access the related value.
	var bestKey []byte
	// `bestHeight` stores the value used by this operation.
	var bestHeight uint64
	// `keys` and `err` store the error produced by this operation.
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), targetHeight)
	if err != nil {
		return nil, err
	}
	// `candidate` tracks the current values while iterating.
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
	// `bestKey` stores the key used to access the related value.
	var bestKey []byte
	// `bestHeight` stores the value used by this operation.
	var bestHeight uint64
	// `keys` and `err` store the error produced by this operation.
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
	if err != nil {
		return nil, 0, err
	}
	// `candidate` tracks the current values while iterating.
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
	// `snap` and `err` store the error produced by this operation.
	snap, err := n.GetLatestSnapshot()
	if err == nil && snap != nil {
		return snap, nil
	}
	if err != nil && !errors.Is(err, ErrKeyNotFound) {
		// `fallback` and `scanErr` store the error produced by this operation.
		if fallback, scanErr := n.loadBestReadableSnapshotAtOrBelow(0); scanErr == nil && fallback != nil {
			return fallback, nil
		}
		return nil, err
	}
	return n.loadBestReadableSnapshotAtOrBelow(0)
}

// GetSnapshotKey returns snapshot key.
func (n *Node) GetSnapshotKey(key []byte) (*StateSnapshot, error) {
	if n.DB == nil || n.DB.SnapshotStore() == nil {
		return nil, fmt.Errorf("snapshot db not initialized")
	}
	return readSnapshotFromStores(n.DB.SnapshotStoresForRead(), key)
}

// snapshotAnchorBlockType restores the reward-relevant class of legacy
// snapshots. These fields are already covered by snapshotCanonicalHash, so the
// inference cannot be changed without invalidating the signed snapshot.
func snapshotAnchorBlockType(snapshot StateSnapshot) BlockType {
	if snapshot.Height <= 1 {
		return BlockTypeGenesis
	}
	if strings.TrimSpace(snapshot.BlockProposer) == "" || snapshot.BlockEpoch == 0 {
		return BlockTypeReceipt
	}
	if strings.TrimSpace(snapshot.BlockMempoolRoot) == "" {
		return BlockTypeTime
	}
	return BlockTypeWork
}

// snapshotAnchorBlock implements the snapshot anchor block helper.
func snapshotAnchorBlock(snapshot StateSnapshot) Block {
	// `nextActivation` stores the value produced by this operation.
	nextActivation := snapshotActivationHeight(&snapshot)
	if nextActivation == 0 {
		nextActivation = snapshot.Height + 1
	}
	// `nextHash` stores the digest used to identify or verify the related data.
	nextHash := strings.TrimSpace(snapshot.NextValidatorSetHash)
	if nextHash == "" {
		nextHash = strings.TrimSpace(snapshot.ValidatorSetHash)
	}
	blockEpoch := snapshot.BlockEpoch
	if blockEpoch == 0 {
		blockEpoch = snapshot.Height
	}
	// `anchor` stores the value produced by this operation.
	anchor := Block{
		ID:                        snapshot.Height,
		Height:                    snapshot.Height,
		BlockHash:                 strings.TrimSpace(snapshot.BlockHash),
		PrevHash:                  strings.TrimSpace(snapshot.PrevHash),
		Proposer:                  strings.TrimSpace(snapshot.BlockProposer),
		MempoolRoot:               strings.TrimSpace(snapshot.BlockMempoolRoot),
		Type:                      snapshotAnchorBlockType(snapshot),
		BlockTime:                 LogicalTimeForEpoch(blockEpoch),
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
	// `cert` stores the value produced by this operation.
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

// loadDurableBlock implements the load durable block helper.
func (n *Node) loadDurableBlock(height uint64) (Block, bool) {
	if n == nil || height == 0 {
		return Block{}, false
	}
	// `blk` stores the value used by this operation.
	var blk Block
	if n.DB != nil && n.DB.Blocks != nil {
		// `err` stores the error produced by this operation.
		err := n.DB.Blocks.View(func(txn *Txn) error {
			// `item` and `err` store the error produced by this operation.
			item, err := txn.Get([]byte(fmt.Sprintf("block:%d", height)))
			if err != nil {
				return err
			}
			return item.Value(func(v []byte) error {
				// `plain` and `derr` store the error produced by this operation.
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
	// `blk` and `ok` store whether the related condition is satisfied.
	if blk, ok := n.loadBlockFile(height); ok {
		return blk, true
	}
	return Block{}, false
}

// ensureSnapshotAnchorBlockStored implements the ensure snapshot anchor block stored helper.
func (n *Node) ensureSnapshotAnchorBlockStored(anchor Block) {
	if n == nil || anchor.ID == 0 || strings.TrimSpace(anchor.BlockHash) == "" {
		return
	}
	// `existing` and `ok` store whether the related condition is satisfied.
	if existing, ok := n.loadDurableBlock(anchor.ID); ok &&
		strings.EqualFold(strings.TrimSpace(existing.BlockHash), strings.TrimSpace(anchor.BlockHash)) &&
		strings.EqualFold(strings.TrimSpace(existing.StateRoot), strings.TrimSpace(anchor.StateRoot)) &&
		strings.TrimSpace(existing.StateRoot) != "" {
		n.StoreBlock(existing)
		return
	}
	// Same-height recovery can already have the complete canonical block in
	// memory even when it has not reached the durable block store yet. Preserve
	// that block's type, transactions, signatures and execution commitments;
	// replacing it with the reduced snapshot-derived anchor would make
	// post-commit reward replay depend on incomplete metadata.
	if n.Blockchain != nil {
		if existing, ok := n.Blockchain.GetBlock(anchor.ID); ok &&
			strings.EqualFold(strings.TrimSpace(existing.BlockHash), strings.TrimSpace(anchor.BlockHash)) &&
			strings.EqualFold(strings.TrimSpace(existing.StateRoot), strings.TrimSpace(anchor.StateRoot)) &&
			strings.TrimSpace(existing.StateRoot) != "" {
			n.StoreBlock(existing)
			return
		}
	}
	n.StoreBlock(anchor)
}

// persistAppliedSnapshotExecutionAuthority implements the persist applied snapshot execution authority helper.
func (n *Node) persistAppliedSnapshotExecutionAuthority(snapshot StateSnapshot, reason string) bool {
	if n == nil || snapshot.Height == 0 {
		return false
	}
	// `anchor` and `ok` store whether the related condition is satisfied.
	anchor, ok := n.snapshotAnchorBlockForLedgerReplay(snapshot)
	if !ok {
		return false
	}
	// `ledgerHash` stores the digest used to identify or verify the related data.
	ledgerHash := HashLedger(snapshot.Ledger)
	// `expectedRoot` stores the digest used to identify or verify the related data.
	expectedRoot := ComputeExecHashVersioned(anchor, ledgerHash, executionStateRootVersionForHeight(anchor.ID))
	if expectedRoot == "" ||
		!strings.EqualFold(strings.TrimSpace(expectedRoot), strings.TrimSpace(anchor.StateRoot)) ||
		!strings.EqualFold(strings.TrimSpace(snapshot.StateRoot), strings.TrimSpace(anchor.StateRoot)) {
		return false
	}
	// `upgraded` stores the value produced by this operation.
	upgraded := cloneStateSnapshot(&snapshot)
	if upgraded == nil {
		return false
	}
	upgraded.LedgerStage = snapshotLedgerStageExecution
	upgraded.SnapshotHash = ""
	populateSnapshotDerivedFields(upgraded)
	// `err` stores the error produced by this operation.
	if err := n.storeCommittedStateSnapshotRecord(upgraded, "snapshot_apply_execution_upgrade"); err != nil {
		log.Printf("[WARN] applied snapshot execution authority persist failed height=%d reason=%s err=%v",
			snapshot.Height, strings.TrimSpace(reason), err)
		return false
	}
	log.Printf("[SNAPSHOT-ANCHOR] status=execution_snapshot_stored height=%d reason=%s",
		snapshot.Height, strings.TrimSpace(reason))
	return true
}

// ApplySnapshotForSync applies snapshot for sync.
func (n *Node) ApplySnapshotForSync(snapshot StateSnapshot) (applied bool) {
	// `started` stores the value produced by this operation.
	started := time.Now()
	defer func() {
		n.observeSnapshotOperation("apply", snapshot.Height, time.Since(started), applied)
	}()
	if n == nil || n.Blockchain == nil || snapshot.Height == 0 {
		return
	}
	localHeight := n.Blockchain.Height()
	if snapshot.Height <= localHeight {
		log.Printf("[SNAPSHOT-REJECT] reason=height_regression local=%d snapshot=%d hash=%s",
			localHeight,
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	preflightLedger := snapshot.Ledger.Clone()
	if err := validateRestoredLedgerSupplyAtHeight(&preflightLedger, "snapshot_sync_preflight", snapshot.Height); err != nil {
		log.Printf("[SNAPSHOT-REJECT] reason=supply_audit_failed height=%d hash=%s err=%v",
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
			err,
		)
		return
	}
	populateSnapshotDerivedFields(&snapshot)
	if reason := snapshotMetadataRejectReason(&snapshot); reason != "" {
		log.Printf("[SNAPSHOT-REJECT] reason=%s snapshot=%d hash=%s",
			reason,
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	if reason := snapshotApplyExecutionAuthorityRejectReason(&snapshot); reason != "" {
		log.Printf("[SNAPSHOT-REJECT] reason=%s snapshot=%d hash=%s",
			reason,
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	// `reason` stores the value produced by this operation.
	if reason := n.snapshotLocalFinalityRejectReason(&snapshot); reason != "" {
		log.Printf("[SNAPSHOT-REJECT] reason=%s local_finalized=%d snapshot=%d hash=%s",
			reason,
			n.getFinalizedHeight(),
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	// `resumeLedger` stores the result produced by this operation.
	resumeLedger := snapshot.Ledger.Clone()
	if err := validateRestoredLedgerSupplyAtHeight(&resumeLedger, "snapshot_sync", snapshot.Height); err != nil {
		log.Printf("[SNAPSHOT-REJECT] reason=supply_audit_failed height=%d hash=%s err=%v",
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
			err,
		)
		return
	}
	// `prevSyncing` stores the value produced by this operation.
	prevSyncing := false
	// `prevPaused` stores the value produced by this operation.
	prevPaused := false
	// `prevTarget` stores the value produced by this operation.
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
			if applied && !prevSyncing {
				n.replayQueuedExecutionVotes()
			}
			if applied && !prevSyncing && !prevPaused {
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
	// `shouldStoreAnchor` stores the value produced by this operation.
	shouldStoreAnchor := false
	n.Blockchain.mu.Lock()
	// `currentHeight` stores the value produced by this operation.
	currentHeight := uint64(0)
	if len(n.Blockchain.Blocks) > 0 {
		currentHeight = n.Blockchain.Blocks[len(n.Blockchain.Blocks)-1].ID
	}
	if currentHeight < snapshot.Height {
		n.Blockchain.Blocks = []Block{anchor}
		shouldStoreAnchor = true
	} else if currentHeight == snapshot.Height {
		// `ln` stores the value produced by this operation.
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

	if gate := n.recoveryVotingRejoinGate(snapshot.Height); !gate.Ready {
		log.Printf("[SNAPSHOT-POSTVERIFY] status=failed height=%d reason=%s runtime_ledger=%s execution_ledger=%s state_root=%s registry_hash=%s parent=%s tip=%s",
			snapshot.Height,
			strings.TrimSpace(gate.Reason),
			ShortHash(gate.RuntimeLedgerHash),
			ShortHash(gate.ExecutionLedgerHash),
			ShortHash(gate.StateRoot),
			ShortHash(gate.RegistryHash),
			ShortHash(gate.ParentHash),
			ShortHash(gate.TipHash),
		)
		return false
	}
	n.setLogicalTick(snapshot.Height+1, TickExec)
	applied = true
	return applied
}

// ApplySnapshotForRecovery force-applies a snapshot even if local height is higher.
// This is used for auto-heal when local state diverges.
func (n *Node) ApplySnapshotForRecovery(snapshot StateSnapshot) (applied bool) {
	// `started` stores the value produced by this operation.
	started := time.Now()
	defer func() {
		n.observeSnapshotOperation("apply", snapshot.Height, time.Since(started), applied)
	}()
	if n == nil || n.Blockchain == nil || snapshot.Height == 0 {
		return
	}
	preflightLedger := snapshot.Ledger.Clone()
	if err := validateRestoredLedgerSupplyAtHeight(&preflightLedger, "snapshot_recovery_preflight", snapshot.Height); err != nil {
		log.Printf("[SNAPSHOT-REJECT] reason=supply_audit_failed height=%d hash=%s err=%v",
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
			err,
		)
		return
	}
	populateSnapshotDerivedFields(&snapshot)
	if reason := snapshotMetadataRejectReason(&snapshot); reason != "" {
		log.Printf("[SNAPSHOT-REJECT] reason=%s snapshot=%d hash=%s",
			reason,
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	if reason := snapshotApplyExecutionAuthorityRejectReason(&snapshot); reason != "" {
		log.Printf("[SNAPSHOT-REJECT] reason=%s snapshot=%d hash=%s",
			reason,
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	// `reason` stores the value produced by this operation.
	if reason := n.snapshotLocalFinalityRejectReason(&snapshot); reason != "" {
		log.Printf("[SNAPSHOT-REJECT] reason=%s local_finalized=%d snapshot=%d hash=%s",
			reason,
			n.getFinalizedHeight(),
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
		)
		return
	}
	// `resumeLedger` stores the result produced by this operation.
	resumeLedger := snapshot.Ledger.Clone()
	if err := validateRestoredLedgerSupplyAtHeight(&resumeLedger, "snapshot_recovery", snapshot.Height); err != nil {
		log.Printf("[SNAPSHOT-REJECT] reason=supply_audit_failed height=%d hash=%s err=%v",
			snapshot.Height,
			ShortHash(snapshot.BlockHash),
			err,
		)
		return
	}
	// `currentHeight` stores the value produced by this operation.
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
	// `legacyTransitionSnapshot` stores the value produced by this operation.
	legacyTransitionSnapshot := len(snapshot.PendingValidators) == 0 &&
		len(snapshot.PendingValidatorRemovals) == 0 &&
		snapshot.ValidatorSetHeight == 0
	// `prevPendingValidators` stores the value produced by this operation.
	prevPendingValidators := make(map[string]uint64)
	// `prevPendingRemovals` stores the value produced by this operation.
	prevPendingRemovals := make(map[string]uint64)
	// `prevValidatorSetHeight` stores the value produced by this operation.
	prevValidatorSetHeight := uint64(0)
	if legacyTransitionSnapshot {
		n.validatorSetMu.RLock()
		// `id` and `act` track the current position in the related collection.
		for id, act := range n.pendingValidators {
			prevPendingValidators[id] = act
		}
		// `id` and `act` track the current position in the related collection.
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
		// `tipHash` stores the digest used to identify or verify the related data.
		tipHash := ""
		n.Blockchain.mu.RLock()
		// `ln` stores the value produced by this operation.
		if ln := len(n.Blockchain.Blocks); ln > 0 {
			// `last` stores the value produced by this operation.
			last := n.Blockchain.Blocks[ln-1]
			if last.ID == snapshot.Height {
				tipHash = last.BlockHash
			}
		}
		n.Blockchain.mu.RUnlock()

		if tipHash == snapshot.BlockHash {
			n.commitMu.Lock()
			// `committedHash` stores the digest used to identify or verify the related data.
			committedHash := ""
			if n.committed != nil {
				committedHash = n.committed[snapshot.Height]
			}
			// `committedHeight` stores the value produced by this operation.
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
				if gate := n.recoveryVotingRejoinGate(snapshot.Height); !gate.Ready {
					log.Printf("[SNAPSHOT-POSTVERIFY] status=failed height=%d reason=%s runtime_ledger=%s execution_ledger=%s state_root=%s registry_hash=%s parent=%s tip=%s",
						snapshot.Height,
						strings.TrimSpace(gate.Reason),
						ShortHash(gate.RuntimeLedgerHash),
						ShortHash(gate.ExecutionLedgerHash),
						ShortHash(gate.StateRoot),
						ShortHash(gate.RegistryHash),
						ShortHash(gate.ParentHash),
						ShortHash(gate.TipHash),
					)
					return false
				}
				applied = true
				return
			}
		}
	}

	// `prevSyncing` stores the value produced by this operation.
	prevSyncing := false
	// `prevPaused` stores the value produced by this operation.
	prevPaused := false
	// `prevTarget` stores the value produced by this operation.
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
			if applied && !prevSyncing {
				n.replayQueuedExecutionVotes()
			}
			if applied && !prevSyncing && !prevPaused {
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

	// `anchor` stores the value produced by this operation.
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

	if gate := n.recoveryVotingRejoinGate(snapshot.Height); !gate.Ready {
		log.Printf("[SNAPSHOT-POSTVERIFY] status=failed height=%d reason=%s runtime_ledger=%s execution_ledger=%s state_root=%s registry_hash=%s parent=%s tip=%s",
			snapshot.Height,
			strings.TrimSpace(gate.Reason),
			ShortHash(gate.RuntimeLedgerHash),
			ShortHash(gate.ExecutionLedgerHash),
			ShortHash(gate.StateRoot),
			ShortHash(gate.RegistryHash),
			ShortHash(gate.ParentHash),
			ShortHash(gate.TipHash),
		)
		return false
	}
	n.setLogicalTick(snapshot.Height+1, TickExec)
	n.hardResetConsensus(snapshot.Height + 1)

	if DebugConsensus {
		fmt.Printf("RECOVERY snapshot applied height=%d\n", snapshot.Height)
	}
	applied = true
	return applied
}

// pruneFrozenValidatorStateBefore implements the prune frozen validator state before helper.
func (n *Node) pruneFrozenValidatorStateBefore(anchorHeight uint64) {
	if n == nil || anchorHeight == 0 {
		return
	}
	n.validatorSetMu.Lock()
	defer n.validatorSetMu.Unlock()
	// `h` tracks the current values while iterating.
	for h := range n.frozenValidatorsByHeight {
		if h < anchorHeight {
			delete(n.frozenValidatorsByHeight, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range n.frozenValidatorHashByHeight {
		if h < anchorHeight {
			delete(n.frozenValidatorHashByHeight, h)
		}
	}
	// `h` tracks the current values while iterating.
	for h := range n.epochValidators {
		if h < anchorHeight {
			delete(n.epochValidators, h)
		}
	}
}

// pruneBlocksAboveHeight implements the prune blocks above height helper.
func (n *Node) pruneBlocksAboveHeight(height uint64) {
	if n.DB == nil || n.DB.Blocks == nil {
		return
	}
	// `prefix` stores the value produced by this operation.
	prefix := []byte("block:")
	_ = n.DB.Blocks.Update(func(txn *Txn) error {
		// `it` stores the current position in the related collection.
		it := txn.NewIterator(DefaultIteratorOptions)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			// `key` stores the key used to access the related value.
			key := it.Item().Key()
			// `keyStr` stores the key used to access the related value.
			keyStr := string(key)
			if !strings.HasPrefix(keyStr, "block:") {
				continue
			}
			// `h` and `err` store the error produced by this operation.
			h, err := strconv.ParseUint(strings.TrimPrefix(keyStr, "block:"), 10, 64)
			if err != nil {
				continue
			}
			if h > height {
				// `err` stores the error produced by this operation.
				if err := txn.Delete(key); err != nil {
					return err
				}
			}
		}
		return nil
	})
	// `err` stores the error produced by this operation.
	if err := deleteBlockFilesAboveHeight(n.DataDir, n.ID, height); err != nil {
		log.Printf("[WARN] block file prune failed height=%d err=%v", height, err)
	}
}

// applySnapshotValidators applies snapshot validators.
func (n *Node) applySnapshotValidators(snapshot StateSnapshot) {
	if len(snapshot.Validators) == 0 {
		return
	}
	// `lockHeight` stores the synchronization state protecting shared data.
	lockHeight := snapshot.Height
	if snapshot.ValidatorSetHeight > 0 {
		lockHeight = snapshot.ValidatorSetHeight
	}
	// `list` stores the value produced by this operation.
	list := make([]string, 0, len(snapshot.Validators))
	// `id` tracks the current position in the related collection.
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
	// `hash` stores the digest used to identify or verify the related data.
	hash := strings.TrimSpace(snapshot.ValidatorSetHash)
	if hash == "" {
		hash = ValidatorSetHash(list)
	}
	n.frozenValidatorHashByHeight[lockHeight] = hash
	n.validatorSetHeight = lockHeight
	n.validatorSetMu.Unlock()
	n.snapshotEpochValidators(snapshot.Height + 1)
}

// applySnapshotValidatorTransitions applies snapshot validator transitions.
func (n *Node) applySnapshotValidatorTransitions(snapshot StateSnapshot) {
	// Backward compatibility: older snapshots do not carry transition queues.
	if len(snapshot.PendingValidators) == 0 &&
		len(snapshot.PendingValidatorRemovals) == 0 &&
		snapshot.ValidatorSetHeight == 0 {
		return
	}

	// `pendingAdds` stores the value produced by this operation.
	pendingAdds := make(map[string]uint64, len(snapshot.PendingValidators))
	// `id` and `act` track the current position in the related collection.
	for id, act := range snapshot.PendingValidators {
		// `norm` stores the value produced by this operation.
		norm := normalizeValidatorID(id)
		if norm == "" || act == 0 {
			continue
		}
		// `existing` and `ok` store whether the related condition is satisfied.
		if existing, ok := pendingAdds[norm]; !ok || act < existing {
			pendingAdds[norm] = act
		}
	}

	// `pendingRemovals` stores the value produced by this operation.
	pendingRemovals := make(map[string]uint64, len(snapshot.PendingValidatorRemovals))
	// `id` and `act` track the current position in the related collection.
	for id, act := range snapshot.PendingValidatorRemovals {
		// `norm` stores the value produced by this operation.
		norm := normalizeValidatorID(id)
		if norm == "" || act == 0 {
			continue
		}
		// `existing` and `ok` store whether the related condition is satisfied.
		if existing, ok := pendingRemovals[norm]; !ok || act < existing {
			pendingRemovals[norm] = act
		}
	}

	// `lockHeight` stores the synchronization state protecting shared data.
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

// LoadLatestSnapshot loads latest snapshot.
func (n *Node) LoadLatestSnapshot() error {
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.GetLatestSnapshot()
	if err != nil {
		return err
	}
	return n.applyLoadedSnapshot(snapshot)
}

// LoadSnapshotKey loads snapshot key.
func (n *Node) LoadSnapshotKey(key []byte) error {
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.GetSnapshotKey(key)
	if err != nil {
		return err
	}
	return n.applyLoadedSnapshot(snapshot)
}

// LoadSnapshot loads snapshot.
func (n *Node) LoadSnapshot(height uint64) error {
	// `snapshot` and `err` store the error produced by this operation.
	snapshot, err := n.GetSnapshot(height)
	if err != nil {
		return err
	}
	if snapshot.Height != height {
		return fmt.Errorf("snapshot height mismatch: want %d got %d", height, snapshot.Height)
	}
	return n.applyLoadedSnapshot(snapshot)
}

// applyLoadedSnapshot applies loaded snapshot.
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
	if reason := snapshotApplyExecutionAuthorityRejectReason(snapshot); reason != "" {
		return fmt.Errorf("snapshot apply authority rejected at height %d: %s", snapshot.Height, reason)
	}
	// `chainHeight` stores the value produced by this operation.
	chainHeight := uint64(0)
	if n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}
	if snapshot.Height < chainHeight {
		return fmt.Errorf("snapshot height regression: local=%d snapshot=%d", chainHeight, snapshot.Height)
	}
	if n.Blockchain != nil && snapshot.Height > chainHeight {
		if !n.ApplySnapshotForSync(*snapshot) {
			return fmt.Errorf("snapshot apply rejected at height %d", snapshot.Height)
		}
	} else {
		ledger := snapshot.Ledger.Clone()
		if err := validateRestoredLedgerSupplyAtHeight(&ledger, "snapshot_load", snapshot.Height); err != nil {
			return err
		}
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

// Clone clones its operation.
func (l Ledger) Clone() Ledger {
	// `copy` stores the value produced by this operation.
	copy := Ledger{
		Balances:                 make(map[string]int),
		Nonces:                   make(map[string]int),
		Stakes:                   make(map[string]StakeLock),
		ValidatorRewardWallets:   make(map[string]string),
		DTL:                      cloneDTLState(l.DTL),
		UsedValidatorUpdateCerts: make(map[string]uint64),
		UsedBridgeEvents:         make(map[string]uint64),
	}
	// `k` and `v` track the current values while iterating.
	for k, v := range l.Balances {
		copy.Balances[k] = v
	}
	// `k` and `v` track the current values while iterating.
	for k, v := range l.Nonces {
		copy.Nonces[k] = v
	}
	// `k` and `v` track the current values while iterating.
	for k, v := range l.Stakes {
		copy.Stakes[k] = v
	}
	// `k` and `v` track the current values while iterating.
	for k, v := range l.ValidatorRewardWallets {
		copy.ValidatorRewardWallets[k] = v
	}
	// `k` and `v` track the current values while iterating.
	for k, v := range l.UsedValidatorUpdateCerts {
		copy.UsedValidatorUpdateCerts[k] = v
	}
	for k, v := range l.UsedBridgeEvents {
		copy.UsedBridgeEvents[k] = v
	}
	return copy
}

// CopyValidators copies validators.
func (n *Node) CopyValidators() map[string]bool {
	n.validatorMu.RLock()
	defer n.validatorMu.RUnlock()

	// `out` stores the result produced by this operation.
	out := make(map[string]bool, len(n.validatorStatus))
	// `k` tracks the current values while iterating.
	for k := range n.validatorStatus {
		out[k] = true
	}
	return out
}

// SortedKeys implements the sorted keys helper.
func SortedKeys(m map[string]bool) []string {
	// `keys` stores the key used to access the related value.
	keys := make([]string, 0, len(m))
	// `k` tracks the current values while iterating.
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ApplyValidatorsFromExecution applies validators from execution.
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

	// `now` stores the value produced by this operation.
	now := time.Now()

	// =====================================================
	// ðŸ”„ Replace validator set atomically (STATUS MODEL)
	// =====================================================
	next := make(map[string]*ValidatorStatus, len(newSet))

	// `id` tracks the current position in the related collection.
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

// PruneSnapshots prunes snapshots.
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

	// `minKeepHeight` stores the value produced by this operation.
	minKeepHeight := base - keepLast

	// `keys` and `err` store the error produced by this operation.
	keys, err := listSnapshotKeysFromStores(n.DB.SnapshotStoresForRead(), 0)
	if err != nil {
		return err
	}
	// `candidate` tracks the current values while iterating.
	for _, candidate := range keys {
		if candidate.height >= minKeepHeight {
			continue
		}
		if DebugConsensus {
			fmt.Printf("Pruning snapshot @ height %d\n", candidate.height)
		}
		// `err` stores the error produced by this operation.
		if err := n.deleteStoredSnapshotHeight(candidate.height); err != nil {
			return err
		}
	}
	// `err` stores the error produced by this operation.
	if err := n.pruneSnapshotMetaBelowHeight(minKeepHeight); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := n.pruneSnapshotDeltasBelowHeight(minKeepHeight); err != nil {
		return err
	}
	if n.pruneExecutionSnapshotCacheBefore(minKeepHeight) > 0 {
		// `err` stores the error produced by this operation.
		if err := n.recordStatePruneMarker("execution_cache", base, minKeepHeight, keepLast); err != nil {
			return err
		}
	}
	return n.recordStatePruneMarker("snapshot", base, minKeepHeight, keepLast)
}

// PruneValidatorRegistrySnapshots prunes validator registry snapshots.
func (n *Node) PruneValidatorRegistrySnapshots(keepLast uint64) error {
	if n == nil || n.DB == nil || n.DB.State == nil {
		return nil
	}
	if n.statePruningArchiveMode() {
		return nil
	}

	// `base` stores the value produced by this operation.
	base := n.getFinalizedHeight()
	if base == 0 {
		base = n.Blockchain.Height()
	}
	if base <= keepLast {
		return nil
	}
	// `minKeepHeight` stores the value produced by this operation.
	minKeepHeight := base - keepLast

	// `err` stores the error produced by this operation.
	if err := n.DB.State.Update(func(txn *Txn) error {
		// `opts` stores the value produced by this operation.
		opts := DefaultIteratorOptions
		opts.PrefetchValues = false

		// `it` stores the current position in the related collection.
		it := txn.NewIterator(opts)
		defer it.Close()

		// `prefixes` stores the value produced by this operation.
		prefixes := [][]byte{
			[]byte("validator_registry_snapshot:"),
			[]byte("registry_snapshot:"),
		}
		// `prefix` tracks the current values while iterating.
		for _, prefix := range prefixes {
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				// `item` stores the current position in the related collection.
				item := it.Item()
				// `key` stores the key used to access the related value.
				key := item.Key()

				// `parts` stores the value produced by this operation.
				parts := bytes.Split(key, []byte(":"))
				if len(parts) != 2 {
					continue
				}
				// `h` and `err` store the error produced by this operation.
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
				// `err` stores the error produced by this operation.
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

// waitForPubSubMesh implements the wait for pub sub mesh helper.
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
