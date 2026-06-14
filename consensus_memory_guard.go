package main

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"
)

const (
	ConsensusMemoryAuditInterval        = 30 * time.Second
	ConsensusMemoryFutureEpochWindow    = 128
	ExecPoolMaxEpochs                   = 256
	ExecPoolMaxScopesPerEpoch           = 512
	ExecPoolMaxResultsPerScope          = 64
	ExecPoolMaxSignersPerScope          = MaxPeers * 4
	AcceptedProposalBlocksMaxKeys       = AcceptedProposalMaxKeys
	ValidatorStatusMinCap               = MaxPeers * 4
	ValidatorStatusProtectedReserveKeys = MaxPeers
)

type consensusMemoryPruneStats struct {
	ExecPoolEpochs         int
	ExecPoolScopes         int
	ExecPoolResults        int
	ExecPoolSigners        int
	AcceptedProposalBlocks int
	ValidatorStatus        int
}

func (n *Node) startConsensusMemoryGuard(ctx context.Context) {
	if n == nil {
		return
	}
	interval := ConsensusMemoryAuditInterval
	if interval <= 0 {
		interval = time.Minute
	}
	n.auditConsensusMemory("startup")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.shutdownCh:
			return
		case <-ticker.C:
			n.auditConsensusMemory("periodic")
		}
	}
}

func (n *Node) auditConsensusMemory(reason string) {
	if n == nil {
		return
	}
	pruned := n.enforceConsensusMemoryCaps()
	stats := n.MapStats()
	log.Printf("[CONSENSUS-MEMORY-AUDIT] reason=%s exec_pool_epochs=%d exec_pool_pool_epochs=%d exec_pool_results=%d exec_pool_result_signers=%d exec_pool_signer_scopes=%d exec_pool_signers=%d exec_pool_choice_scopes=%d exec_pool_choices=%d accepted_proposal_blocks=%d validator_status=%d pruned_exec_epochs=%d pruned_exec_scopes=%d pruned_exec_results=%d pruned_exec_signers=%d pruned_accepted_blocks=%d pruned_validator_status=%d",
		strings.TrimSpace(reason),
		stats.ExecPoolEpochs,
		stats.ExecPoolPoolEpochs,
		stats.ExecPoolResults,
		stats.ExecPoolResultSigners,
		stats.ExecPoolSignerScopes,
		stats.ExecPoolSigners,
		stats.ExecPoolChoiceScopes,
		stats.ExecPoolChoices,
		stats.AcceptedProposalBlocks,
		stats.ValidatorStatusCount,
		pruned.ExecPoolEpochs,
		pruned.ExecPoolScopes,
		pruned.ExecPoolResults,
		pruned.ExecPoolSigners,
		pruned.AcceptedProposalBlocks,
		pruned.ValidatorStatus,
	)
}

func (n *Node) enforceConsensusMemoryCaps() consensusMemoryPruneStats {
	var pruned consensusMemoryPruneStats
	if n == nil {
		return pruned
	}
	committedHeight := n.committedReplayFenceHeight()
	protectedScopes := n.consensusMemoryProtectedExecScopes()

	ExecPool.mu.Lock()
	pruned.add(pruneExecPoolLocked(committedHeight, protectedScopes))
	ExecPool.mu.Unlock()

	n.execResultsMu.Lock()
	pruned.AcceptedProposalBlocks += n.pruneAcceptedProposalBlocksGlobalLocked(committedHeight)
	n.execResultsMu.Unlock()

	protectedValidators := n.consensusMemoryProtectedValidatorIDs(committedHeight + 1)
	n.validatorMu.Lock()
	pruned.ValidatorStatus += n.pruneValidatorStatusLocked(protectedValidators)
	n.validatorMu.Unlock()

	return pruned
}

func (p *consensusMemoryPruneStats) add(other consensusMemoryPruneStats) {
	if p == nil {
		return
	}
	p.ExecPoolEpochs += other.ExecPoolEpochs
	p.ExecPoolScopes += other.ExecPoolScopes
	p.ExecPoolResults += other.ExecPoolResults
	p.ExecPoolSigners += other.ExecPoolSigners
	p.AcceptedProposalBlocks += other.AcceptedProposalBlocks
	p.ValidatorStatus += other.ValidatorStatus
}

func (n *Node) consensusMemoryProtectedExecScopes() map[uint64]map[string]bool {
	protected := make(map[uint64]map[string]bool)
	if n == nil {
		return protected
	}
	add := func(heightKey string, proposalKey string) {
		proposalKey = strings.TrimSpace(proposalKey)
		if proposalKey == "" {
			return
		}
		height := uint64(0)
		if h, _, _, _, _, ok := proposalVoteKeyParts(proposalKey); ok {
			height = h
		} else if h, ok := parseHeightPrefix(strings.TrimSpace(heightKey)); ok {
			height = h
		}
		if height == 0 {
			return
		}
		scope := execPoolScopeKey(height, proposalKey)
		if scope == "" {
			return
		}
		if protected[height] == nil {
			protected[height] = make(map[string]bool)
		}
		protected[height][scope] = true
	}
	n.execResultsMu.Lock()
	for heightKey, proposalKey := range n.acceptedProposal {
		add(heightKey, proposalKey)
	}
	for heightKey, proposalKey := range n.quorumLockedProposal {
		add(heightKey, proposalKey)
	}
	n.execResultsMu.Unlock()
	return protected
}

func (n *Node) consensusMemoryProtectedValidatorIDs(height uint64) map[string]bool {
	protected := make(map[string]bool)
	if n == nil {
		return protected
	}
	add := func(id string) {
		id = normalizeValidatorID(id)
		if id != "" {
			protected[id] = true
		}
	}
	add(n.ID)
	for _, id := range n.GenesisValidators {
		add(id)
	}
	if height > 0 {
		for _, id := range n.GetConsensusValidators(int(height)) {
			add(id)
		}
		for _, id := range n.GetConsensusValidators(int(height + 1)) {
			add(id)
		}
	}
	n.validatorSetMu.RLock()
	for _, id := range n.currentValidators {
		add(id)
	}
	for id := range n.pendingValidators {
		add(id)
	}
	for id := range n.pendingValidatorRemovals {
		add(id)
	}
	n.validatorSetMu.RUnlock()
	GlobalValidatorRegistry.mu.RLock()
	for id, rec := range GlobalValidatorRegistry.records {
		switch rec.Status {
		case ValidatorActive, ValidatorPending:
			add(id)
		}
	}
	GlobalValidatorRegistry.mu.RUnlock()
	return protected
}

func (n *Node) pruneAcceptedProposalBlocksGlobalLocked(committedHeight uint64) int {
	if n == nil || n.acceptedProposalBlocks == nil {
		return 0
	}
	pruned := 0
	for key, block := range n.acceptedProposalBlocks {
		if committedHeight > 0 && block.ID > 0 && block.ID <= committedHeight {
			delete(n.acceptedProposalBlocks, key)
			pruned++
		}
	}
	if AcceptedProposalBlocksMaxKeys <= 0 || len(n.acceptedProposalBlocks) <= AcceptedProposalBlocksMaxKeys {
		return pruned
	}
	protected := make(map[string]bool)
	for _, proposalKey := range n.acceptedProposal {
		if proposalKey = strings.TrimSpace(proposalKey); proposalKey != "" {
			protected[proposalKey] = true
		}
	}
	for _, proposalKey := range n.quorumLockedProposal {
		if proposalKey = strings.TrimSpace(proposalKey); proposalKey != "" {
			protected[proposalKey] = true
		}
	}
	type entry struct {
		key       string
		height    uint64
		round     uint32
		protected bool
	}
	entries := make([]entry, 0, len(n.acceptedProposalBlocks))
	for key, block := range n.acceptedProposalBlocks {
		entries = append(entries, entry{
			key:       key,
			height:    block.ID,
			round:     block.Round,
			protected: protected[key],
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].protected != entries[j].protected {
			return entries[i].protected
		}
		if entries[i].height != entries[j].height {
			return entries[i].height > entries[j].height
		}
		if entries[i].round != entries[j].round {
			return entries[i].round > entries[j].round
		}
		return entries[i].key < entries[j].key
	})
	for _, entry := range entries {
		if len(n.acceptedProposalBlocks) <= AcceptedProposalBlocksMaxKeys {
			break
		}
		if entry.protected {
			continue
		}
		delete(n.acceptedProposalBlocks, entry.key)
		pruned++
	}
	return pruned
}

func (n *Node) pruneValidatorStatusLocked(protected map[string]bool) int {
	if n == nil || n.validatorStatus == nil {
		return 0
	}
	limit := ValidatorStatusMinCap
	if needed := len(protected) + ValidatorStatusProtectedReserveKeys; needed > limit {
		limit = needed
	}
	if limit <= 0 || len(n.validatorStatus) <= limit {
		return 0
	}
	type entry struct {
		id       string
		active   bool
		height   uint64
		lastSeen time.Time
	}
	entries := make([]entry, 0, len(n.validatorStatus))
	for id, st := range n.validatorStatus {
		normID := normalizeValidatorID(id)
		if protected[normID] {
			continue
		}
		e := entry{id: id}
		if st != nil {
			e.active = st.Active || st.Enabled || st.ConsensusReadyKnown
			e.height = st.Height
			if st.ReportedHeight > e.height {
				e.height = st.ReportedHeight
			}
			if st.FinalizedHeight > e.height {
				e.height = st.FinalizedHeight
			}
			if st.ExecEpoch > e.height {
				e.height = st.ExecEpoch
			}
			if st.ValidatorSetHeight > e.height {
				e.height = st.ValidatorSetHeight
			}
			e.lastSeen = st.LastSeen
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].active != entries[j].active {
			return !entries[i].active
		}
		if !entries[i].lastSeen.Equal(entries[j].lastSeen) {
			return entries[i].lastSeen.Before(entries[j].lastSeen)
		}
		if entries[i].height != entries[j].height {
			return entries[i].height < entries[j].height
		}
		return entries[i].id < entries[j].id
	})
	pruned := 0
	for _, entry := range entries {
		if len(n.validatorStatus) <= limit {
			break
		}
		delete(n.validatorStatus, entry.id)
		pruned++
	}
	return pruned
}

func populateExecPoolMapStats(stats *MapStats) {
	if stats == nil {
		return
	}
	ExecPool.mu.Lock()
	defer ExecPool.mu.Unlock()
	stats.ExecPoolEpochs = len(execPoolEpochUnionLocked())
	stats.ExecPoolPoolEpochs = len(ExecPool.pool)
	for _, byResult := range ExecPool.pool {
		stats.ExecPoolResults += len(byResult)
		for _, bySigner := range byResult {
			stats.ExecPoolResultSigners += len(bySigner)
		}
	}
	for _, byScope := range ExecPool.signers {
		stats.ExecPoolSignerScopes += len(byScope)
		for _, signers := range byScope {
			stats.ExecPoolSigners += len(signers)
		}
	}
	for _, byScope := range ExecPool.choice {
		stats.ExecPoolChoiceScopes += len(byScope)
		for _, choices := range byScope {
			stats.ExecPoolChoices += len(choices)
		}
	}
	for _, byScope := range ExecPool.frozen {
		stats.ExecPoolFrozenScopes += len(byScope)
	}
	for _, bySigner := range ExecPool.epochChoice {
		stats.ExecPoolEpochChoices += len(bySigner)
	}
	for _, bySigner := range ExecPool.commitChoice {
		stats.ExecPoolCommitChoices += len(bySigner)
	}
}

func ensureExecPoolTopMapsLocked() {
	if ExecPool.pool == nil {
		ExecPool.pool = make(map[uint64]map[string]map[string]ExecutionResult)
	}
	if ExecPool.txMerkle == nil {
		ExecPool.txMerkle = make(map[uint64]map[string]string)
	}
	if ExecPool.frozen == nil {
		ExecPool.frozen = make(map[uint64]map[string]string)
	}
	if ExecPool.signers == nil {
		ExecPool.signers = make(map[uint64]map[string]map[string]bool)
	}
	if ExecPool.choice == nil {
		ExecPool.choice = make(map[uint64]map[string]map[string]string)
	}
	if ExecPool.epochChoice == nil {
		ExecPool.epochChoice = make(map[uint64]map[string]string)
	}
	if ExecPool.commitChoice == nil {
		ExecPool.commitChoice = make(map[uint64]map[string]string)
	}
}

func ensureExecPoolEpochMapsLocked(epoch uint64) {
	ensureExecPoolTopMapsLocked()
	if epoch == 0 {
		return
	}
	if ExecPool.pool[epoch] == nil {
		ExecPool.pool[epoch] = make(map[string]map[string]ExecutionResult)
	}
	if ExecPool.txMerkle[epoch] == nil {
		ExecPool.txMerkle[epoch] = make(map[string]string)
	}
	if ExecPool.frozen[epoch] == nil {
		ExecPool.frozen[epoch] = make(map[string]string)
	}
	if ExecPool.signers[epoch] == nil {
		ExecPool.signers[epoch] = make(map[string]map[string]bool)
	}
	if ExecPool.choice[epoch] == nil {
		ExecPool.choice[epoch] = make(map[string]map[string]string)
	}
	if ExecPool.epochChoice[epoch] == nil {
		ExecPool.epochChoice[epoch] = make(map[string]string)
	}
	if ExecPool.commitChoice[epoch] == nil {
		ExecPool.commitChoice[epoch] = make(map[string]string)
	}
}

func ensureExecPoolScopeMapsLocked(epoch uint64, scope string) {
	ensureExecPoolEpochMapsLocked(epoch)
	if epoch == 0 || scope == "" {
		return
	}
	if ExecPool.signers[epoch][scope] == nil {
		ExecPool.signers[epoch][scope] = make(map[string]bool)
	}
	if ExecPool.choice[epoch][scope] == nil {
		ExecPool.choice[epoch][scope] = make(map[string]string)
	}
}

func execPoolEpochUnionLocked() map[uint64]bool {
	epochs := make(map[uint64]bool)
	for h := range ExecPool.pool {
		epochs[h] = true
	}
	for h := range ExecPool.txMerkle {
		epochs[h] = true
	}
	for h := range ExecPool.frozen {
		epochs[h] = true
	}
	for h := range ExecPool.signers {
		epochs[h] = true
	}
	for h := range ExecPool.choice {
		epochs[h] = true
	}
	for h := range ExecPool.epochChoice {
		epochs[h] = true
	}
	for h := range ExecPool.commitChoice {
		epochs[h] = true
	}
	return epochs
}

func execPoolScopeFromResultKey(scopedExecKey string) string {
	scopedExecKey = strings.TrimSpace(scopedExecKey)
	idx := strings.LastIndex(scopedExecKey, "|")
	if idx <= 0 {
		return scopedExecKey
	}
	return scopedExecKey[:idx]
}

func execPoolScopeKnownLocked(epoch uint64, scope string) bool {
	if epoch == 0 || scope == "" {
		return false
	}
	if byScope := ExecPool.frozen[epoch]; byScope != nil {
		if _, ok := byScope[scope]; ok {
			return true
		}
	}
	if byScope := ExecPool.signers[epoch]; byScope != nil {
		if _, ok := byScope[scope]; ok {
			return true
		}
	}
	if byScope := ExecPool.choice[epoch]; byScope != nil {
		if _, ok := byScope[scope]; ok {
			return true
		}
	}
	if byResult := ExecPool.pool[epoch]; byResult != nil {
		prefix := scope + "|"
		for key := range byResult {
			if strings.HasPrefix(key, prefix) {
				return true
			}
		}
	}
	return false
}

func execPoolScopeSetLocked(epoch uint64) map[string]bool {
	scopes := make(map[string]bool)
	for scope := range ExecPool.frozen[epoch] {
		scopes[scope] = true
	}
	for scope := range ExecPool.signers[epoch] {
		scopes[scope] = true
	}
	for scope := range ExecPool.choice[epoch] {
		scopes[scope] = true
	}
	for key := range ExecPool.pool[epoch] {
		if scope := execPoolScopeFromResultKey(key); scope != "" {
			scopes[scope] = true
		}
	}
	return scopes
}

func execPoolResultKnownLocked(epoch uint64, scopedExecKey string) bool {
	if epoch == 0 || scopedExecKey == "" {
		return false
	}
	if byResult := ExecPool.pool[epoch]; byResult != nil {
		if _, ok := byResult[scopedExecKey]; ok {
			return true
		}
	}
	if byMerkle := ExecPool.txMerkle[epoch]; byMerkle != nil {
		if _, ok := byMerkle[scopedExecKey]; ok {
			return true
		}
	}
	return false
}

func execPoolResultCountLocked(epoch uint64, scopedExecKey string) int {
	if byResult := ExecPool.pool[epoch]; byResult != nil {
		return len(byResult[scopedExecKey])
	}
	return 0
}

func execPoolResultCountForScopeLocked(epoch uint64, scope string) int {
	if epoch == 0 || scope == "" {
		return 0
	}
	count := 0
	prefix := scope + "|"
	for key := range ExecPool.pool[epoch] {
		if strings.HasPrefix(key, prefix) {
			count++
		}
	}
	return count
}

func execPoolSignerKnownLocked(epoch uint64, scope string, signer string) bool {
	signer = normalizeValidatorID(signer)
	if epoch == 0 || scope == "" || signer == "" {
		return false
	}
	if byScope := ExecPool.signers[epoch]; byScope != nil {
		if signers := byScope[scope]; signers != nil && signers[signer] {
			return true
		}
	}
	if byScope := ExecPool.choice[epoch]; byScope != nil {
		if choices := byScope[scope]; choices != nil {
			if _, ok := choices[signer]; ok {
				return true
			}
		}
	}
	prefix := scope + "|"
	for key, results := range ExecPool.pool[epoch] {
		if strings.HasPrefix(key, prefix) {
			if _, ok := results[signer]; ok {
				return true
			}
		}
	}
	return false
}

func execPoolSignerCountForScopeLocked(epoch uint64, scope string) int {
	if epoch == 0 || scope == "" {
		return 0
	}
	signers := make(map[string]bool)
	if byScope := ExecPool.signers[epoch]; byScope != nil {
		for signer := range byScope[scope] {
			signers[normalizeValidatorID(signer)] = true
		}
	}
	if byScope := ExecPool.choice[epoch]; byScope != nil {
		for signer := range byScope[scope] {
			signers[normalizeValidatorID(signer)] = true
		}
	}
	prefix := scope + "|"
	for key, results := range ExecPool.pool[epoch] {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		for signer := range results {
			signers[normalizeValidatorID(signer)] = true
		}
	}
	delete(signers, "")
	return len(signers)
}

func execPoolTxMerkleLocked(epoch uint64, scopedExecKey string) (string, bool) {
	if byMerkle := ExecPool.txMerkle[epoch]; byMerkle != nil {
		value, ok := byMerkle[scopedExecKey]
		return value, ok
	}
	return "", false
}

func execPoolChoiceLocked(epoch uint64, scope string, signer string) (string, bool) {
	if byScope := ExecPool.choice[epoch]; byScope != nil {
		if choices := byScope[scope]; choices != nil {
			value, ok := choices[signer]
			return value, ok
		}
	}
	return "", false
}

func execPoolCanAdmitVoteLocked(epoch uint64, scope string, scopedExecKey string, signer string) bool {
	if epoch == 0 || scope == "" || scopedExecKey == "" || normalizeValidatorID(signer) == "" {
		return false
	}
	if !execPoolEpochUnionLocked()[epoch] && ExecPoolMaxEpochs > 0 && len(execPoolEpochUnionLocked()) >= ExecPoolMaxEpochs {
		return false
	}
	if !execPoolScopeKnownLocked(epoch, scope) && ExecPoolMaxScopesPerEpoch > 0 && len(execPoolScopeSetLocked(epoch)) >= ExecPoolMaxScopesPerEpoch {
		return false
	}
	if !execPoolResultKnownLocked(epoch, scopedExecKey) && ExecPoolMaxResultsPerScope > 0 && execPoolResultCountForScopeLocked(epoch, scope) >= ExecPoolMaxResultsPerScope {
		return false
	}
	if !execPoolSignerKnownLocked(epoch, scope, signer) && ExecPoolMaxSignersPerScope > 0 && execPoolSignerCountForScopeLocked(epoch, scope) >= ExecPoolMaxSignersPerScope {
		return false
	}
	return true
}

func pruneExecPoolLocked(committedHeight uint64, protectedScopes map[uint64]map[string]bool) consensusMemoryPruneStats {
	var pruned consensusMemoryPruneStats
	ensureExecPoolTopMapsLocked()
	for epoch := range execPoolEpochUnionLocked() {
		if committedHeight > 0 && epoch <= committedHeight {
			deleteExecPoolEpochLocked(epoch)
			pruned.ExecPoolEpochs++
			continue
		}
		if committedHeight > 0 && epoch > committedHeight+ConsensusMemoryFutureEpochWindow && !execPoolEpochProtected(epoch, protectedScopes) {
			deleteExecPoolEpochLocked(epoch)
			pruned.ExecPoolEpochs++
		}
	}
	pruned.add(pruneExecPoolEpochCountLocked(committedHeight, protectedScopes))
	for epoch := range execPoolEpochUnionLocked() {
		pruned.add(pruneExecPoolScopesForEpochLocked(epoch, protectedScopes[epoch]))
		pruned.add(pruneExecPoolResultsForEpochLocked(epoch))
		pruned.add(pruneExecPoolSignersForEpochLocked(epoch))
		pruneExecPoolEmptyEpochLocked(epoch)
	}
	return pruned
}

func execPoolEpochProtected(epoch uint64, protectedScopes map[uint64]map[string]bool) bool {
	return len(protectedScopes[epoch]) > 0
}

func pruneExecPoolEpochCountLocked(committedHeight uint64, protectedScopes map[uint64]map[string]bool) consensusMemoryPruneStats {
	var pruned consensusMemoryPruneStats
	if ExecPoolMaxEpochs <= 0 {
		return pruned
	}
	epochs := make([]uint64, 0, len(execPoolEpochUnionLocked()))
	for epoch := range execPoolEpochUnionLocked() {
		if execPoolEpochProtected(epoch, protectedScopes) {
			continue
		}
		epochs = append(epochs, epoch)
	}
	sort.Slice(epochs, func(i, j int) bool {
		if committedHeight > 0 {
			di := epochDistanceFromFence(epochs[i], committedHeight)
			dj := epochDistanceFromFence(epochs[j], committedHeight)
			if di != dj {
				return di > dj
			}
		}
		return epochs[i] < epochs[j]
	})
	for len(execPoolEpochUnionLocked()) > ExecPoolMaxEpochs && len(epochs) > 0 {
		epoch := epochs[0]
		epochs = epochs[1:]
		deleteExecPoolEpochLocked(epoch)
		pruned.ExecPoolEpochs++
	}
	return pruned
}

func epochDistanceFromFence(epoch uint64, fence uint64) uint64 {
	if epoch >= fence {
		return epoch - fence
	}
	return fence - epoch
}

func pruneExecPoolScopesForEpochLocked(epoch uint64, protected map[string]bool) consensusMemoryPruneStats {
	var pruned consensusMemoryPruneStats
	if ExecPoolMaxScopesPerEpoch <= 0 {
		return pruned
	}
	scopes := execPoolScopeSetLocked(epoch)
	if len(scopes) <= ExecPoolMaxScopesPerEpoch {
		return pruned
	}
	type scopeEntry struct {
		scope        string
		protected    bool
		frozen       bool
		results      int
		signers      int
		highestRound uint32
	}
	entries := make([]scopeEntry, 0, len(scopes))
	for scope := range scopes {
		entry := scopeEntry{
			scope:     scope,
			protected: protected[scope],
			results:   execPoolResultCountForScopeLocked(epoch, scope),
			signers:   execPoolSignerCountForScopeLocked(epoch, scope),
		}
		if frozen := strings.TrimSpace(ExecPool.frozen[epoch][scope]); frozen != "" {
			entry.frozen = true
		}
		prefix := scope + "|"
		for key, results := range ExecPool.pool[epoch] {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			for _, res := range results {
				if res.Round > entry.highestRound {
					entry.highestRound = res.Round
				}
			}
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].protected != entries[j].protected {
			return entries[i].protected
		}
		if entries[i].frozen != entries[j].frozen {
			return entries[i].frozen
		}
		if entries[i].signers != entries[j].signers {
			return entries[i].signers > entries[j].signers
		}
		if entries[i].results != entries[j].results {
			return entries[i].results > entries[j].results
		}
		if entries[i].highestRound != entries[j].highestRound {
			return entries[i].highestRound > entries[j].highestRound
		}
		return entries[i].scope < entries[j].scope
	})
	for _, entry := range entries {
		if len(execPoolScopeSetLocked(epoch)) <= ExecPoolMaxScopesPerEpoch {
			break
		}
		if entry.protected {
			continue
		}
		deleteExecPoolScopeLocked(epoch, entry.scope)
		pruned.ExecPoolScopes++
	}
	return pruned
}

func pruneExecPoolResultsForEpochLocked(epoch uint64) consensusMemoryPruneStats {
	var pruned consensusMemoryPruneStats
	if ExecPoolMaxResultsPerScope <= 0 {
		return pruned
	}
	for scope := range execPoolScopeSetLocked(epoch) {
		keys := execPoolResultKeysForScopeLocked(epoch, scope)
		if len(keys) <= ExecPoolMaxResultsPerScope {
			continue
		}
		sort.Slice(keys, func(i, j int) bool {
			li := len(ExecPool.pool[epoch][keys[i]])
			lj := len(ExecPool.pool[epoch][keys[j]])
			if li != lj {
				return li > lj
			}
			return keys[i] < keys[j]
		})
		for len(keys) > ExecPoolMaxResultsPerScope {
			key := keys[len(keys)-1]
			keys = keys[:len(keys)-1]
			delete(ExecPool.pool[epoch], key)
			delete(ExecPool.txMerkle[epoch], key)
			pruned.ExecPoolResults++
		}
	}
	return pruned
}

func pruneExecPoolSignersForEpochLocked(epoch uint64) consensusMemoryPruneStats {
	var pruned consensusMemoryPruneStats
	if ExecPoolMaxSignersPerScope <= 0 {
		return pruned
	}
	for scope := range execPoolScopeSetLocked(epoch) {
		signers := execPoolSignersForScopeLocked(epoch, scope)
		if len(signers) <= ExecPoolMaxSignersPerScope {
			continue
		}
		sort.Strings(signers)
		for len(signers) > ExecPoolMaxSignersPerScope {
			signer := signers[len(signers)-1]
			signers = signers[:len(signers)-1]
			deleteExecPoolSignerFromScopeLocked(epoch, scope, signer)
			pruned.ExecPoolSigners++
		}
	}
	return pruned
}

func execPoolResultKeysForScopeLocked(epoch uint64, scope string) []string {
	keys := make([]string, 0)
	prefix := scope + "|"
	for key := range ExecPool.pool[epoch] {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	return keys
}

func execPoolSignersForScopeLocked(epoch uint64, scope string) []string {
	seen := make(map[string]bool)
	if byScope := ExecPool.signers[epoch]; byScope != nil {
		for signer := range byScope[scope] {
			if signer = normalizeValidatorID(signer); signer != "" {
				seen[signer] = true
			}
		}
	}
	if byScope := ExecPool.choice[epoch]; byScope != nil {
		for signer := range byScope[scope] {
			if signer = normalizeValidatorID(signer); signer != "" {
				seen[signer] = true
			}
		}
	}
	prefix := scope + "|"
	for key, results := range ExecPool.pool[epoch] {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		for signer := range results {
			if signer = normalizeValidatorID(signer); signer != "" {
				seen[signer] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for signer := range seen {
		out = append(out, signer)
	}
	return out
}

func deleteExecPoolEpochLocked(epoch uint64) {
	delete(ExecPool.pool, epoch)
	delete(ExecPool.txMerkle, epoch)
	delete(ExecPool.frozen, epoch)
	delete(ExecPool.signers, epoch)
	delete(ExecPool.choice, epoch)
	delete(ExecPool.epochChoice, epoch)
	delete(ExecPool.commitChoice, epoch)
}

func deleteExecPoolScopeLocked(epoch uint64, scope string) {
	if epoch == 0 || scope == "" {
		return
	}
	prefix := scope + "|"
	for key := range ExecPool.pool[epoch] {
		if strings.HasPrefix(key, prefix) {
			delete(ExecPool.pool[epoch], key)
		}
	}
	for key := range ExecPool.txMerkle[epoch] {
		if strings.HasPrefix(key, prefix) {
			delete(ExecPool.txMerkle[epoch], key)
		}
	}
	delete(ExecPool.frozen[epoch], scope)
	delete(ExecPool.signers[epoch], scope)
	delete(ExecPool.choice[epoch], scope)
	for signer, value := range ExecPool.epochChoice[epoch] {
		if strings.HasPrefix(value, prefix) {
			delete(ExecPool.epochChoice[epoch], signer)
		}
	}
	for signer, value := range ExecPool.commitChoice[epoch] {
		if strings.TrimSpace(value) == scope {
			delete(ExecPool.commitChoice[epoch], signer)
		}
	}
	pruneExecPoolEmptyEpochLocked(epoch)
}

func deleteExecPoolSignerFromScopeLocked(epoch uint64, scope string, signer string) {
	signer = normalizeValidatorID(signer)
	if epoch == 0 || scope == "" || signer == "" {
		return
	}
	if byScope := ExecPool.signers[epoch]; byScope != nil {
		if signers := byScope[scope]; signers != nil {
			delete(signers, signer)
			if len(signers) == 0 {
				delete(byScope, scope)
			}
		}
	}
	if byScope := ExecPool.choice[epoch]; byScope != nil {
		if choices := byScope[scope]; choices != nil {
			delete(choices, signer)
			if len(choices) == 0 {
				delete(byScope, scope)
			}
		}
	}
	prefix := scope + "|"
	for key, results := range ExecPool.pool[epoch] {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		delete(results, signer)
		if len(results) == 0 {
			delete(ExecPool.pool[epoch], key)
			delete(ExecPool.txMerkle[epoch], key)
		}
	}
	delete(ExecPool.epochChoice[epoch], signer)
	if strings.TrimSpace(ExecPool.commitChoice[epoch][signer]) == scope {
		delete(ExecPool.commitChoice[epoch], signer)
	}
	pruneExecPoolEmptyEpochLocked(epoch)
}

func pruneExecPoolEmptyEpochLocked(epoch uint64) {
	if epoch == 0 {
		return
	}
	if len(ExecPool.pool[epoch]) == 0 {
		delete(ExecPool.pool, epoch)
	}
	if len(ExecPool.txMerkle[epoch]) == 0 {
		delete(ExecPool.txMerkle, epoch)
	}
	if len(ExecPool.frozen[epoch]) == 0 {
		delete(ExecPool.frozen, epoch)
	}
	if len(ExecPool.signers[epoch]) == 0 {
		delete(ExecPool.signers, epoch)
	}
	if len(ExecPool.choice[epoch]) == 0 {
		delete(ExecPool.choice, epoch)
	}
	if len(ExecPool.epochChoice[epoch]) == 0 {
		delete(ExecPool.epochChoice, epoch)
	}
	if len(ExecPool.commitChoice[epoch]) == 0 {
		delete(ExecPool.commitChoice, epoch)
	}
}
