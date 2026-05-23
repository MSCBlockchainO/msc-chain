package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	persistentPeersFile        = "peers.json"
	persistentValidatorsFile   = "validators.json"
	validatorFreezeJournalFile = "validator_freeze_journal.jsonl"
)

type ValidatorFreezeJournalEntry struct {
	Height           uint64   `json:"height"`
	ValidatorSetHash string   `json:"validator_set_hash"`
	Validators       []string `json:"validators"`
	RecordedAtUnix   int64    `json:"recorded_at_unix,omitempty"`
}

type startupPeerReconcileEvent struct {
	Base      string
	OldPeerID string
	NewPeerID string
}

func nodeDataPath(baseDir, nodeID string) string {
	return filepath.Join(baseDir, "node_"+nodeID)
}

func mergePeerLists(a, b []string) []string {
	uniq := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))

	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := uniq[p]; ok {
			return
		}
		uniq[p] = struct{}{}
		out = append(out, p)
	}

	for _, p := range a {
		add(p)
	}
	for _, p := range b {
		add(p)
	}

	sort.Strings(out)
	return out
}

func splitPeerAddress(addr string) (base string, peerID string, hasPeerID bool) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", "", false
	}

	idx := strings.Index(addr, "/p2p/")
	if idx < 0 {
		return addr, "", false
	}

	base = strings.TrimSpace(addr[:idx])
	peerID = strings.TrimSpace(addr[idx+len("/p2p/"):])
	if slash := strings.Index(peerID, "/"); slash >= 0 {
		peerID = strings.TrimSpace(peerID[:slash])
	}
	if base == "" || peerID == "" {
		return addr, "", false
	}
	return base, peerID, true
}

// sanitizePeerListWithPreferred keeps one peer ID per transport endpoint
// (e.g. /ip4/127.0.0.1/tcp/7001), preferring entries from preferred.
func sanitizePeerListWithPreferred(peers []string, preferred []string) []string {
	if len(peers) == 0 && len(preferred) == 0 {
		return nil
	}

	preferredByBase := make(map[string]string, len(preferred))
	preferredAddrByBase := make(map[string]string, len(preferred))
	for _, addr := range preferred {
		base, pid, hasPID := splitPeerAddress(addr)
		if !hasPID {
			continue
		}
		preferredByBase[base] = pid
		preferredAddrByBase[base] = fmt.Sprintf("%s/p2p/%s", base, pid)
	}

	out := make([]string, 0, len(peers)+len(preferred))
	seen := make(map[string]struct{}, len(peers)+len(preferred))
	claimedBase := make(map[string]string, len(peers)+len(preferred))

	add := func(raw string) {
		addr := strings.TrimSpace(raw)
		if addr == "" {
			return
		}
		base, pid, hasPID := splitPeerAddress(addr)
		if hasPID {
			if prefPID, ok := preferredByBase[base]; ok && prefPID != pid {
				return
			}
			if existing, ok := claimedBase[base]; ok && existing != addr {
				return
			}
			claimedBase[base] = addr
		} else {
			// If this endpoint is pinned to a preferred /p2p/ identity, drop non-/p2p/ form.
			if _, ok := preferredByBase[base]; ok {
				return
			}
			if existing, ok := claimedBase[base]; ok && existing != addr {
				return
			}
			claimedBase[base] = addr
		}

		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}

	// Pin preferred identities first so conflicting stale entries are rejected.
	for _, addr := range preferredAddrByBase {
		add(addr)
	}
	for _, addr := range peers {
		add(addr)
	}

	sort.Strings(out)
	return out
}

func reconcileLocalhostPeerIDsWithPersisted(peers []string, persisted []string) ([]string, []startupPeerReconcileEvent) {
	if len(peers) == 0 || len(persisted) == 0 {
		return peers, nil
	}

	out := append([]string(nil), peers...)
	configByBase := make(map[string]string, len(peers))
	for _, addr := range peers {
		base, _, hasPeerID := splitPeerAddress(addr)
		if !hasPeerID || !isLocalhostPeerAddr(addr) {
			continue
		}
		if _, exists := configByBase[base]; exists {
			continue
		}
		configByBase[base] = strings.TrimSpace(addr)
	}

	events := make([]startupPeerReconcileEvent, 0)
	for _, addr := range persisted {
		base, peerID, hasPeerID := splitPeerAddress(addr)
		if !hasPeerID || !isLocalhostPeerAddr(addr) {
			continue
		}
		configAddr, ok := configByBase[base]
		if !ok {
			continue
		}
		_, configPeerID, configHasPeerID := splitPeerAddress(configAddr)
		if !configHasPeerID || strings.EqualFold(strings.TrimSpace(configPeerID), strings.TrimSpace(peerID)) {
			continue
		}
		out = replacePeerAddrForBase(out, strings.TrimSpace(addr))
		configByBase[base] = strings.TrimSpace(addr)
		events = append(events, startupPeerReconcileEvent{
			Base:      base,
			OldPeerID: configPeerID,
			NewPeerID: peerID,
		})
	}

	return out, events
}

func loadPersistentPeers(baseDir, nodeID string) []string {
	path := filepath.Join(nodeDataPath(baseDir, nodeID), persistentPeersFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var peers []string
	if err := json.Unmarshal(data, &peers); err != nil {
		return nil
	}
	return peers
}

func savePersistentPeers(baseDir, nodeID string, peers []string) error {
	path := filepath.Join(nodeDataPath(baseDir, nodeID), persistentPeersFile)
	data, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func loadPersistentValidators(baseDir, nodeID string) []string {
	path := filepath.Join(nodeDataPath(baseDir, nodeID), persistentValidatorsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var validators []string
	if err := json.Unmarshal(data, &validators); err != nil {
		return nil
	}
	return validators
}

func savePersistentValidators(baseDir, nodeID string, validators []string) error {
	path := filepath.Join(nodeDataPath(baseDir, nodeID), persistentValidatorsFile)
	data, err := json.MarshalIndent(validators, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func loadValidatorFreezeJournal(baseDir, nodeID string) []ValidatorFreezeJournalEntry {
	path := filepath.Join(nodeDataPath(baseDir, nodeID), validatorFreezeJournalFile)
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	out := make([]ValidatorFreezeJournalEntry, 0, 64)
	sc := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e ValidatorFreezeJournalEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue
		}
		out = append(out, e)
	}
	return out
}

func appendValidatorFreezeJournal(baseDir, nodeID string, entry ValidatorFreezeJournalEntry) error {
	path := filepath.Join(nodeDataPath(baseDir, nodeID), validatorFreezeJournalFile)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (n *Node) replayValidatorFreezeJournal() int {
	if n == nil {
		return 0
	}
	entries := loadValidatorFreezeJournal(n.DataDir, n.ID)
	if len(entries) == 0 {
		return 0
	}

	chainHeight := uint64(0)
	if n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}
	maxHeight := chainHeight + 1
	type validatedFreezeEntry struct {
		height     uint64
		hash       string
		validators []string
	}
	validated := make([]validatedFreezeEntry, 0, len(entries))

	for _, e := range entries {
		if e.Height == 0 || len(e.Validators) == 0 || e.Height > maxHeight {
			continue
		}
		vals := canonicalValidatorIDs(e.Validators)
		if len(vals) == 0 {
			continue
		}
		hash := strings.TrimSpace(e.ValidatorSetHash)
		if hash == "" {
			hash = ValidatorSetHash(vals)
		}
		if !n.validatorSetIDsMatchCommittedHash(e.Height, hash, vals) {
			continue
		}
		// Startup hardening: never replay a frozen set entry that disagrees with
		// an already-persisted chain block hash at the same height. This prevents
		// stale local journals from poisoning validator-set expectations after restarts.
		if n.Blockchain != nil {
			if blk, ok := n.Blockchain.GetBlock(e.Height); ok {
				chainHash := strings.TrimSpace(blk.ValidatorSetHash)
				if chainHash != "" && !strings.EqualFold(chainHash, hash) {
					continue
				}
			}
		}
		validated = append(validated, validatedFreezeEntry{
			height:     e.Height,
			hash:       hash,
			validators: vals,
		})
	}

	applied := 0
	n.validatorSetMu.Lock()
	if n.frozenValidatorsByHeight == nil {
		n.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if n.frozenValidatorHashByHeight == nil {
		n.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	for _, e := range validated {
		n.frozenValidatorsByHeight[e.height] = append([]string{}, e.validators...)
		n.frozenValidatorHashByHeight[e.height] = e.hash
		if e.height > n.freezeJournalLastHeight || (e.height == n.freezeJournalLastHeight && !strings.EqualFold(e.hash, n.freezeJournalLastHash)) {
			n.freezeJournalLastHeight = e.height
			n.freezeJournalLastHash = e.hash
		}
		applied++
	}
	n.validatorSetMu.Unlock()

	if DebugConsensus && applied > 0 {
		fmt.Printf("[SET-FREEZE-REPLAY] entries=%d applied=%d last_h=%d hash=%s\n",
			len(entries), applied, n.freezeJournalLastHeight, ShortHash(n.freezeJournalLastHash))
	}
	return applied
}

// syncFrozenValidatorSetHashesFromChain seeds frozen validator-set hashes from
// persisted chain blocks so restart logic stays chain-deterministic even if
// local freeze journal entries are stale.
func (n *Node) syncFrozenValidatorSetHashesFromChain() int {
	if n == nil || n.Blockchain == nil {
		return 0
	}

	tip := n.Blockchain.Height()
	if tip == 0 {
		return 0
	}

	applied := 0
	materialized := 0
	missingHeights := make([]uint64, 0, 16)
	droppedStaleHeights := make(map[uint64]bool)
	const recentMaterializationWindow uint64 = 128
	shouldMaterializeMissingSet := func(height uint64) bool {
		if height == 0 {
			return false
		}
		if tip <= recentMaterializationWindow {
			return true
		}
		if height == 1 || height == tip {
			return true
		}
		return height+recentMaterializationWindow >= tip
	}
	n.validatorSetMu.Lock()
	if n.frozenValidatorsByHeight == nil {
		n.frozenValidatorsByHeight = make(map[uint64][]string)
	}
	if n.frozenValidatorHashByHeight == nil {
		n.frozenValidatorHashByHeight = make(map[uint64]string)
	}
	findContiguousCarryValidators := func(height uint64, wantHash string) []string {
		wantHash = strings.TrimSpace(wantHash)
		if height == 0 || wantHash == "" {
			return nil
		}
		for prev := height; prev > 0; prev-- {
			prevHash := strings.TrimSpace(n.frozenValidatorHashByHeight[prev])
			if prevHash == "" {
				continue
			}
			if !strings.EqualFold(prevHash, wantHash) {
				break
			}
			if vals := canonicalValidatorIDs(n.frozenValidatorsByHeight[prev]); len(vals) > 0 {
				return vals
			}
		}
		return nil
	}
	findForwardContiguousCarryValidators := func(height uint64, wantHash string) []string {
		wantHash = strings.TrimSpace(wantHash)
		if height == 0 || wantHash == "" || height > tip {
			return nil
		}
		for next := height; next <= tip; next++ {
			nextHash := strings.TrimSpace(n.frozenValidatorHashByHeight[next])
			if nextHash == "" {
				continue
			}
			if !strings.EqualFold(nextHash, wantHash) {
				break
			}
			if vals := canonicalValidatorIDs(n.frozenValidatorsByHeight[next]); len(vals) > 0 {
				return vals
			}
			if next == ^uint64(0) {
				break
			}
		}
		return nil
	}
	for h := uint64(1); h <= tip; h++ {
		blk, ok := n.Blockchain.GetBlock(h)
		if !ok {
			continue
		}
		chainHash := strings.TrimSpace(blk.ValidatorSetHash)
		if chainHash == "" {
			continue
		}
		existingHash := strings.TrimSpace(n.frozenValidatorHashByHeight[h])
		// If frozen validator IDs disagree with chain hash, drop the stale list.
		if vals := canonicalValidatorIDs(n.frozenValidatorsByHeight[h]); len(vals) > 0 {
			if !strings.EqualFold(existingHash, chainHash) && !strings.EqualFold(ValidatorSetHash(vals), chainHash) {
				if !n.stateHistoryPrunedForHeight(h) {
					delete(n.frozenValidatorsByHeight, h)
					droppedStaleHeights[h] = true
				}
			}
		}
		if !strings.EqualFold(existingHash, chainHash) {
			n.frozenValidatorHashByHeight[h] = chainHash
			if h > n.freezeJournalLastHeight || (h == n.freezeJournalLastHeight && !strings.EqualFold(chainHash, n.freezeJournalLastHash)) {
				n.freezeJournalLastHeight = h
				n.freezeJournalLastHash = chainHash
			}
			applied++
		}
		if len(n.frozenValidatorsByHeight[h]) == 0 && !droppedStaleHeights[h] {
			if carry := findContiguousCarryValidators(h-1, chainHash); len(carry) > 0 {
				n.frozenValidatorsByHeight[h] = append([]string{}, carry...)
			} else if carry := findForwardContiguousCarryValidators(h+1, chainHash); len(carry) > 0 {
				n.frozenValidatorsByHeight[h] = append([]string{}, carry...)
			} else if shouldMaterializeMissingSet(h) {
				missingHeights = append(missingHeights, h)
			}
		}
	}
	n.validatorSetMu.Unlock()

	for _, h := range missingHeights {
		if len(n.frozenValidatorsForHeight(h)) > 0 {
			continue
		}
		validators, resolvedHash, _, ok := n.resolveCommittedValidatorSetForHeight(h)
		if !ok || len(validators) == 0 {
			continue
		}
		validators = canonicalValidatorIDs(validators)
		if len(validators) == 0 {
			continue
		}
		expectedHash, _ := n.frozenValidatorSetHash(h)
		expectedHash = strings.TrimSpace(expectedHash)
		if expectedHash != "" {
			if _, ok := n.validatorSetCandidateMatchesTarget(h, expectedHash, validators, nil); !ok {
				continue
			}
		}
		n.validatorSetMu.Lock()
		if len(n.frozenValidatorsByHeight[h]) == 0 {
			n.frozenValidatorsByHeight[h] = append([]string{}, validators...)
			if strings.TrimSpace(n.frozenValidatorHashByHeight[h]) == "" {
				resolvedHash = strings.TrimSpace(resolvedHash)
				if resolvedHash == "" {
					resolvedHash = strings.TrimSpace(n.preferredValidatorSetHashForHeight(h, validators, nil))
				}
				if resolvedHash == "" {
					resolvedHash = ValidatorSetHash(validators)
				}
				n.frozenValidatorHashByHeight[h] = resolvedHash
				if h > n.freezeJournalLastHeight || (h == n.freezeJournalLastHeight && !strings.EqualFold(resolvedHash, n.freezeJournalLastHash)) {
					n.freezeJournalLastHeight = h
					n.freezeJournalLastHash = resolvedHash
				}
			}
			materialized++
		}
		n.validatorSetMu.Unlock()
	}

	if DebugConsensus && (applied > 0 || materialized > 0) {
		fmt.Printf("[SET-FREEZE-CHAIN] applied=%d materialized=%d tip_h=%d hash=%s\n",
			applied, materialized, tip, ShortHash(n.freezeJournalLastHash))
	}
	return applied
}

func (n *Node) persistValidatorFreezeEntry(height uint64, validators []string, hash string) {
	if n == nil || height == 0 {
		return
	}
	vals := canonicalValidatorIDs(validators)
	if len(vals) == 0 {
		return
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		hash = ValidatorSetHash(vals)
	}

	n.validatorSetMu.Lock()
	if n.freezeJournalLastHeight == height && strings.EqualFold(n.freezeJournalLastHash, hash) {
		n.validatorSetMu.Unlock()
		return
	}
	n.freezeJournalLastHeight = height
	n.freezeJournalLastHash = hash
	n.validatorSetMu.Unlock()

	entry := ValidatorFreezeJournalEntry{
		Height:           height,
		ValidatorSetHash: hash,
		Validators:       append([]string{}, vals...),
		RecordedAtUnix:   time.Now().Unix(),
	}
	if err := appendValidatorFreezeJournal(n.DataDir, n.ID, entry); err != nil && (DebugConsensus || DebugSync) {
		fmt.Printf("WARN freeze journal append failed: h=%d err=%v\n", height, err)
	}
}

func (n *Node) collectPeerMultiaddrs() []string {
	uniq := make(map[string]struct{})

	for _, p := range n.persistentPeersSnapshot() {
		if p == "" {
			continue
		}
		uniq[p] = struct{}{}
	}

	ValidatorAddrBook.mu.RLock()
	for _, addr := range ValidatorAddrBook.m {
		if addr == "" {
			continue
		}
		uniq[addr] = struct{}{}
	}
	ValidatorAddrBook.mu.RUnlock()

	if n.Host != nil {
		for _, pid := range n.Host.Network().Peers() {
			addrs := n.Host.Peerstore().Addrs(pid)
			for _, addr := range addrs {
				full := fmt.Sprintf("%s/p2p/%s", addr.String(), pid.String())
				uniq[full] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(uniq))
	for p := range uniq {
		out = append(out, p)
	}
	return sanitizePeerListWithPreferred(out, n.trustedPeerMultiaddrs())
}

func (n *Node) persistLocalState() error {
	peers := n.collectPeerMultiaddrs()
	validators := n.getValidatorList()

	if err := savePersistentPeers(n.DataDir, n.ID, peers); err != nil {
		return err
	}
	if err := savePersistentValidators(n.DataDir, n.ID, validators); err != nil {
		return err
	}
	return nil
}
