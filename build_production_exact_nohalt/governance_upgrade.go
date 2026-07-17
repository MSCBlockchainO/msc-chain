package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type GovernanceProposalKind string

const (
	GovernanceProposalValidatorUpgrade GovernanceProposalKind = "validator"
	GovernanceProposalTreasury         GovernanceProposalKind = "treasury"
	GovernanceProposalProtocolUpgrade  GovernanceProposalKind = "protocol_upgrade"
	GovernanceProposalEmergency        GovernanceProposalKind = "emergency"
	GovernanceProposalEmergencyPause   GovernanceProposalKind = "emergency_pause"
)

type GovernanceProposalStatus string

const (
	GovernanceProposalPending   GovernanceProposalStatus = "pending"
	GovernanceProposalActive    GovernanceProposalStatus = "active"
	GovernanceProposalApproved  GovernanceProposalStatus = "approved"
	GovernanceProposalRejected  GovernanceProposalStatus = "rejected"
	GovernanceProposalScheduled GovernanceProposalStatus = "scheduled"
	GovernanceProposalApplied   GovernanceProposalStatus = "applied"
)

type GovernanceVoteChoice string

const (
	GovernanceVoteYes     GovernanceVoteChoice = "yes"
	GovernanceVoteNo      GovernanceVoteChoice = "no"
	GovernanceVoteAbstain GovernanceVoteChoice = "abstain"
)

const (
	ProtocolGateValidatorSetActivationModelV2 = "validator_set_activation_model_v2_height"
	ProtocolGateValidatorSetCommitmentV2      = "validator_set_commitment_v2_height"
	ProtocolGateValidatorSetHashV3            = "validator_set_hash_v3_height"
	ProtocolGateSyncSnapshotCheckpointV2      = "sync_snapshot_checkpoint_v2_height"
	ProtocolGateDTLV2                         = "dtl_v2_activation_height"

	defaultProtocolUpgradeActivationDelay uint64 = 10
	defaultEmergencyPauseMaxBlocks        uint64 = 1000
)

var governanceStateDBKey = []byte("governance:state:v1")

type GovernanceVote struct {
	Voter     string               `json:"voter"`
	Choice    GovernanceVoteChoice `json:"choice"`
	Weight    int                  `json:"weight"`
	Height    uint64               `json:"height"`
	Signature string               `json:"signature,omitempty"`
}

type GovernanceTally struct {
	Yes      int `json:"yes"`
	No       int `json:"no"`
	Abstain  int `json:"abstain"`
	Total    int `json:"total"`
	Required int `json:"required"`
}

type GovernanceProposal struct {
	ID                   string                    `json:"id"`
	Kind                 GovernanceProposalKind    `json:"kind"`
	Title                string                    `json:"title,omitempty"`
	Description          string                    `json:"description,omitempty"`
	Proposer             string                    `json:"proposer,omitempty"`
	CreatedHeight        uint64                    `json:"created_height"`
	VotingStartHeight    uint64                    `json:"voting_start_height"`
	VotingEndHeight      uint64                    `json:"voting_end_height"`
	ActivationHeight     uint64                    `json:"activation_height"`
	Target               string                    `json:"target,omitempty"`
	Amount               int64                     `json:"amount,omitempty"`
	TreasuryRecipient    string                    `json:"treasury_recipient,omitempty"`
	TreasuryAmount       int64                     `json:"treasury_amount,omitempty"`
	UpgradeName          string                    `json:"upgrade_name,omitempty"`
	UpgradeVersion       string                    `json:"upgrade_version,omitempty"`
	ProtocolChanges      map[string]uint64         `json:"protocol_changes,omitempty"`
	RollbackAllowed      bool                      `json:"rollback_allowed,omitempty"`
	Emergency            bool                      `json:"emergency,omitempty"`
	PauseUntilHeight     uint64                    `json:"pause_until_height,omitempty"`
	PauseReason          string                    `json:"pause_reason,omitempty"`
	Status               GovernanceProposalStatus  `json:"status"`
	Votes                map[string]GovernanceVote `json:"votes,omitempty"`
	Tally                GovernanceTally           `json:"tally"`
	AppliedAtHeight      uint64                    `json:"applied_at_height,omitempty"`
	DeterministicPayload string                    `json:"deterministic_payload,omitempty"`
}

type ProtocolUpgrade struct {
	Name             string            `json:"name"`
	Version          string            `json:"version"`
	ActivationHeight uint64            `json:"activation_height"`
	ProtocolChanges  map[string]uint64 `json:"protocol_changes,omitempty"`
	ProposalID       string            `json:"proposal_id,omitempty"`
	RollbackAllowed  bool              `json:"rollback_allowed,omitempty"`
	Emergency        bool              `json:"emergency,omitempty"`
	Activated        bool              `json:"activated,omitempty"`
	ActivatedHeight  uint64            `json:"activated_height,omitempty"`
}

type ProtocolUpgradeManager struct {
	CurrentVersion      string                     `json:"current_version"`
	MinActivationDelay  uint64                     `json:"min_activation_delay"`
	Scheduled           []ProtocolUpgrade          `json:"scheduled,omitempty"`
	Activated           map[string]ProtocolUpgrade `json:"activated,omitempty"`
	LastActivatedHeight uint64                     `json:"last_activated_height,omitempty"`
}

type GovernanceEmergencyPauseState struct {
	Active           bool   `json:"active"`
	ProposalID       string `json:"proposal_id,omitempty"`
	Reason           string `json:"reason,omitempty"`
	ActivatedHeight  uint64 `json:"activated_height,omitempty"`
	PauseUntilHeight uint64 `json:"pause_until_height,omitempty"`
	ReleasedHeight   uint64 `json:"released_height,omitempty"`
}

type GovernanceState struct {
	Proposals       map[string]*GovernanceProposal `json:"proposals"`
	UpgradeManager  ProtocolUpgradeManager         `json:"upgrade_manager"`
	TreasuryBalance int64                          `json:"treasury_balance"`
	EmergencyPause  GovernanceEmergencyPauseState  `json:"emergency_pause"`
}

type GovernanceMetricsSnapshot struct {
	ProposalsTotal       int
	ProposalsActive      int
	ProposalsApproved    int
	ProposalsRejected    int
	ProposalsScheduled   int
	ProposalsApplied     int
	TreasuryBalance      int64
	ProtocolScheduled    int
	ProtocolActivated    int
	EmergencyPauseActive bool
	EmergencyPauseUntil  uint64
}

func NewGovernanceState() *GovernanceState {
	return &GovernanceState{
		Proposals:      make(map[string]*GovernanceProposal),
		UpgradeManager: NewProtocolUpgradeManager(),
	}
}

func NewProtocolUpgradeManager() ProtocolUpgradeManager {
	return ProtocolUpgradeManager{
		CurrentVersion:     Version,
		MinActivationDelay: defaultProtocolUpgradeActivationDelay,
		Scheduled:          make([]ProtocolUpgrade, 0),
		Activated:          make(map[string]ProtocolUpgrade),
	}
}

func (s *GovernanceState) ensure() {
	if s.Proposals == nil {
		s.Proposals = make(map[string]*GovernanceProposal)
	}
	s.UpgradeManager.ensure()
}

func (m *ProtocolUpgradeManager) ensure() {
	if strings.TrimSpace(m.CurrentVersion) == "" {
		m.CurrentVersion = Version
	}
	if m.MinActivationDelay == 0 {
		m.MinActivationDelay = defaultProtocolUpgradeActivationDelay
	}
	if m.Scheduled == nil {
		m.Scheduled = make([]ProtocolUpgrade, 0)
	}
	if m.Activated == nil {
		m.Activated = make(map[string]ProtocolUpgrade)
	}
}

func (s *GovernanceState) SubmitProposal(proposal GovernanceProposal) (string, error) {
	if s == nil {
		return "", errors.New("governance state is nil")
	}
	s.ensure()
	normalizeGovernanceProposal(&proposal)
	if err := validateGovernanceProposal(proposal); err != nil {
		return "", err
	}
	if proposal.ID == "" {
		proposal.ID = hashGovernanceProposal(proposal)
	}
	if _, exists := s.Proposals[proposal.ID]; exists {
		return "", fmt.Errorf("governance proposal already exists: %s", proposal.ID)
	}
	if proposal.Status == "" {
		proposal.Status = GovernanceProposalActive
	}
	if proposal.Votes == nil {
		proposal.Votes = make(map[string]GovernanceVote)
	}
	proposal.DeterministicPayload = hashGovernanceProposal(proposal)
	copyProposal := proposal
	s.Proposals[proposal.ID] = &copyProposal
	return proposal.ID, nil
}

func (s *GovernanceState) CastVote(proposalID string, voter string, choice GovernanceVoteChoice, height uint64, registry map[string]ValidatorRecord) error {
	if s == nil {
		return errors.New("governance state is nil")
	}
	s.ensure()
	proposalID = strings.TrimSpace(proposalID)
	p, ok := s.Proposals[proposalID]
	if !ok || p == nil {
		return fmt.Errorf("governance proposal not found: %s", proposalID)
	}
	if p.Status != GovernanceProposalActive && p.Status != GovernanceProposalPending {
		return fmt.Errorf("governance proposal is not active: %s", p.Status)
	}
	if height < p.VotingStartHeight {
		return errors.New("governance voting has not started")
	}
	if p.VotingEndHeight > 0 && height > p.VotingEndHeight {
		return errors.New("governance voting has ended")
	}
	voter = normalizeValidatorID(voter)
	if voter == "" {
		return errors.New("governance voter is empty")
	}
	if !containsValidatorID(governanceAuthorityIDs(registry), voter) {
		return fmt.Errorf("governance voter is not authorized: %s", voter)
	}
	choice = normalizeGovernanceVoteChoice(choice)
	if choice == "" {
		return errors.New("invalid governance vote choice")
	}
	if p.Votes == nil {
		p.Votes = make(map[string]GovernanceVote)
	}
	if _, exists := p.Votes[voter]; exists {
		return fmt.Errorf("governance voter already voted: %s", voter)
	}
	p.Votes[voter] = GovernanceVote{
		Voter:  voter,
		Choice: choice,
		Weight: 1,
		Height: height,
	}
	p.Status = GovernanceProposalActive
	p.Tally = tallyGovernanceVotes(p, registry)
	return nil
}

func (s *GovernanceState) FinalizeProposal(proposalID string, height uint64, registry map[string]ValidatorRecord) (GovernanceTally, error) {
	if s == nil {
		return GovernanceTally{}, errors.New("governance state is nil")
	}
	s.ensure()
	p, ok := s.Proposals[strings.TrimSpace(proposalID)]
	if !ok || p == nil {
		return GovernanceTally{}, fmt.Errorf("governance proposal not found: %s", proposalID)
	}
	tally := tallyGovernanceVotes(p, registry)
	p.Tally = tally
	if tally.Required == 0 {
		p.Status = GovernanceProposalRejected
		return tally, errors.New("governance has no voting authority")
	}
	if tally.Yes >= tally.Required {
		p.Status = GovernanceProposalApproved
		return tally, nil
	}
	if p.VotingEndHeight > 0 && height >= p.VotingEndHeight {
		p.Status = GovernanceProposalRejected
		return tally, nil
	}
	p.Status = GovernanceProposalActive
	return tally, nil
}

func (s *GovernanceState) ApplyApprovedProposal(proposalID string, height uint64) error {
	if s == nil {
		return errors.New("governance state is nil")
	}
	s.ensure()
	p, ok := s.Proposals[strings.TrimSpace(proposalID)]
	if !ok || p == nil {
		return fmt.Errorf("governance proposal not found: %s", proposalID)
	}
	if p.Status != GovernanceProposalApproved && p.Status != GovernanceProposalScheduled {
		return fmt.Errorf("governance proposal is not approved: %s", p.Status)
	}
	if p.ActivationHeight > 0 && height < p.ActivationHeight {
		if p.Kind == GovernanceProposalProtocolUpgrade || p.Kind == GovernanceProposalEmergency {
			if err := s.scheduleProposalUpgrade(p, height); err != nil {
				return err
			}
			p.Status = GovernanceProposalScheduled
			return nil
		}
		return fmt.Errorf("governance proposal activation height not reached: %d", p.ActivationHeight)
	}

	switch p.Kind {
	case GovernanceProposalTreasury:
		amount := p.TreasuryAmount
		if amount == 0 {
			amount = p.Amount
		}
		if amount <= 0 {
			return errors.New("treasury proposal amount must be positive")
		}
		if s.TreasuryBalance < amount {
			return errors.New("treasury balance insufficient")
		}
		s.TreasuryBalance -= amount
	case GovernanceProposalProtocolUpgrade, GovernanceProposalEmergency:
		if err := s.scheduleProposalUpgrade(p, height); err != nil {
			return err
		}
		if _, err := s.UpgradeManager.ActivateDue(height); err != nil {
			return err
		}
	case GovernanceProposalEmergencyPause:
		if err := s.activateEmergencyPause(p, height); err != nil {
			return err
		}
	case GovernanceProposalValidatorUpgrade:
		// Validator add/remove execution remains handled by TxValidatorUpdate.
		// This governance path records the approved intent and final activation.
	default:
		return fmt.Errorf("unsupported governance proposal kind: %s", p.Kind)
	}
	p.Status = GovernanceProposalApplied
	p.AppliedAtHeight = height
	return nil
}

func (s *GovernanceState) activateEmergencyPause(p *GovernanceProposal, height uint64) error {
	if s == nil || p == nil {
		return errors.New("governance emergency pause proposal is nil")
	}
	if p.PauseUntilHeight == 0 {
		return errors.New("emergency pause until height is required")
	}
	if p.PauseUntilHeight <= height {
		return fmt.Errorf("emergency pause until height must be above current height: current=%d until=%d", height, p.PauseUntilHeight)
	}
	reason := strings.TrimSpace(p.PauseReason)
	if reason == "" {
		reason = strings.TrimSpace(p.Title)
	}
	s.EmergencyPause = GovernanceEmergencyPauseState{
		Active:           true,
		ProposalID:       strings.TrimSpace(p.ID),
		Reason:           reason,
		ActivatedHeight:  height,
		PauseUntilHeight: p.PauseUntilHeight,
	}
	return nil
}

func (s *GovernanceState) EmergencyPauseActiveAt(height uint64) bool {
	if s == nil || !s.EmergencyPause.Active {
		return false
	}
	if s.EmergencyPause.PauseUntilHeight == 0 {
		return true
	}
	if height == 0 {
		return true
	}
	return height <= s.EmergencyPause.PauseUntilHeight
}

func (s *GovernanceState) ExpireEmergencyPause(height uint64) bool {
	if s == nil || !s.EmergencyPause.Active || height == 0 {
		return false
	}
	if s.EmergencyPause.PauseUntilHeight == 0 || height <= s.EmergencyPause.PauseUntilHeight {
		return false
	}
	s.EmergencyPause.Active = false
	s.EmergencyPause.ReleasedHeight = height
	return true
}

func (s *GovernanceState) scheduleProposalUpgrade(p *GovernanceProposal, currentHeight uint64) error {
	if s == nil || p == nil {
		return errors.New("governance upgrade proposal is nil")
	}
	if p.Kind != GovernanceProposalProtocolUpgrade && p.Kind != GovernanceProposalEmergency {
		return nil
	}
	baseHeight := currentHeight
	if p.CreatedHeight > 0 {
		baseHeight = p.CreatedHeight
	}
	return s.UpgradeManager.Schedule(ProtocolUpgrade{
		Name:             p.UpgradeName,
		Version:          p.UpgradeVersion,
		ActivationHeight: p.ActivationHeight,
		ProtocolChanges:  copyProtocolGateMap(p.ProtocolChanges),
		ProposalID:       p.ID,
		RollbackAllowed:  p.RollbackAllowed,
		Emergency:        p.Kind == GovernanceProposalEmergency || p.Emergency,
	}, baseHeight)
}

func (m *ProtocolUpgradeManager) Schedule(upgrade ProtocolUpgrade, currentHeight uint64) error {
	m.ensure()
	normalizeProtocolUpgrade(&upgrade)
	if upgrade.Name == "" {
		return errors.New("protocol upgrade name is required")
	}
	if upgrade.Version == "" {
		upgrade.Version = m.CurrentVersion
	}
	if upgrade.ActivationHeight == 0 {
		return errors.New("protocol upgrade activation height is required")
	}
	if !upgrade.Emergency && upgrade.ActivationHeight < currentHeight+m.MinActivationDelay {
		return fmt.Errorf("protocol upgrade activation height must be at least %d", currentHeight+m.MinActivationDelay)
	}
	if err := validateProtocolGateChanges(upgrade.ProtocolChanges, upgrade.RollbackAllowed); err != nil {
		return err
	}
	key := protocolUpgradeKey(upgrade)
	if _, exists := m.Activated[key]; exists {
		return nil
	}
	for _, scheduled := range m.Scheduled {
		if protocolUpgradeKey(scheduled) == key {
			return nil
		}
	}
	m.Scheduled = append(m.Scheduled, upgrade)
	sort.Slice(m.Scheduled, func(i, j int) bool {
		if m.Scheduled[i].ActivationHeight != m.Scheduled[j].ActivationHeight {
			return m.Scheduled[i].ActivationHeight < m.Scheduled[j].ActivationHeight
		}
		return protocolUpgradeKey(m.Scheduled[i]) < protocolUpgradeKey(m.Scheduled[j])
	})
	return nil
}

func (m *ProtocolUpgradeManager) ActivateDue(height uint64) ([]ProtocolUpgrade, error) {
	m.ensure()
	activated := make([]ProtocolUpgrade, 0)
	remaining := m.Scheduled[:0]
	for _, upgrade := range m.Scheduled {
		if upgrade.ActivationHeight > height {
			remaining = append(remaining, upgrade)
			continue
		}
		if err := validateProtocolGateChanges(upgrade.ProtocolChanges, upgrade.RollbackAllowed); err != nil {
			return nil, err
		}
		for name, gateHeight := range upgrade.ProtocolChanges {
			if err := applyProtocolGateHeight(name, gateHeight); err != nil {
				return nil, err
			}
		}
		upgrade.Activated = true
		upgrade.ActivatedHeight = height
		if upgrade.Version != "" {
			m.CurrentVersion = upgrade.Version
		}
		m.LastActivatedHeight = height
		m.Activated[protocolUpgradeKey(upgrade)] = upgrade
		activated = append(activated, upgrade)
	}
	m.Scheduled = remaining
	return activated, nil
}

func (s *GovernanceState) Hash() string {
	if s == nil {
		return ""
	}
	s.ensure()
	raw, err := json.Marshal(governanceStateCanonical{s})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (n *Node) PersistGovernanceState(state *GovernanceState) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return errors.New("node meta db unavailable")
	}
	if state == nil {
		state = NewGovernanceState()
	}
	state.ensure()
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return n.DB.Meta.Update(func(txn *Txn) error {
		return txn.Set(governanceStateDBKey, raw)
	})
}

func (n *Node) LoadGovernanceState() (*GovernanceState, error) {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return nil, errors.New("node meta db unavailable")
	}
	out := NewGovernanceState()
	err := n.DB.Meta.View(func(txn *Txn) error {
		item, err := txn.Get(governanceStateDBKey)
		if err != nil {
			if errors.Is(err, ErrKeyNotFound) {
				return nil
			}
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, out)
		})
	})
	if err != nil {
		return nil, err
	}
	out.ensure()
	return out, nil
}

func (n *Node) governanceMetricsSnapshot() GovernanceMetricsSnapshot {
	if n == nil {
		return GovernanceMetricsSnapshot{}
	}
	state, err := n.LoadGovernanceState()
	if err != nil || state == nil {
		return GovernanceMetricsSnapshot{}
	}
	state.ensure()
	out := GovernanceMetricsSnapshot{
		ProposalsTotal:    len(state.Proposals),
		TreasuryBalance:   state.TreasuryBalance,
		ProtocolScheduled: len(state.UpgradeManager.Scheduled),
		ProtocolActivated: len(state.UpgradeManager.Activated),
	}
	height := uint64(0)
	if n.Blockchain != nil {
		if h := n.Blockchain.Height(); h > 0 {
			height = uint64(h)
		}
	}
	out.EmergencyPauseActive = state.EmergencyPauseActiveAt(height)
	out.EmergencyPauseUntil = state.EmergencyPause.PauseUntilHeight
	for _, proposal := range state.Proposals {
		if proposal == nil {
			continue
		}
		switch proposal.Status {
		case GovernanceProposalPending, GovernanceProposalActive:
			out.ProposalsActive++
		case GovernanceProposalApproved:
			out.ProposalsApproved++
		case GovernanceProposalRejected:
			out.ProposalsRejected++
		case GovernanceProposalScheduled:
			out.ProposalsScheduled++
		case GovernanceProposalApplied:
			out.ProposalsApplied++
		}
	}
	return out
}

func governanceAuthorityIDs(registry map[string]ValidatorRecord) []string {
	governance := make([]string, 0)
	active := make([]string, 0)
	for id, rec := range registry {
		normalized := normalizeValidatorID(rec.ID)
		if normalized == "" {
			normalized = normalizeValidatorID(id)
		}
		if normalized == "" || rec.Status != ValidatorActive {
			continue
		}
		active = append(active, normalized)
		if rec.GovernanceSigner {
			governance = append(governance, normalized)
		}
	}
	if len(governance) > 0 {
		return canonicalValidatorIDs(governance)
	}
	if len(active) > 0 {
		return canonicalValidatorIDs(active)
	}
	return canonicalValidatorIDs(ConfigAuthCoreValidators)
}

func tallyGovernanceVotes(p *GovernanceProposal, registry map[string]ValidatorRecord) GovernanceTally {
	authorities := governanceAuthorityIDs(registry)
	required := coreRegistryRequiredSignatures(len(authorities), 0)
	if required == 0 {
		required = strictExecSupermajority(len(authorities))
	}
	allowed := make(map[string]struct{}, len(authorities))
	for _, id := range authorities {
		allowed[id] = struct{}{}
	}
	tally := GovernanceTally{Required: required}
	if p == nil {
		return tally
	}
	for voter, vote := range p.Votes {
		normalized := normalizeValidatorID(voter)
		if _, ok := allowed[normalized]; !ok {
			continue
		}
		weight := vote.Weight
		if weight <= 0 {
			weight = 1
		}
		switch normalizeGovernanceVoteChoice(vote.Choice) {
		case GovernanceVoteYes:
			tally.Yes += weight
		case GovernanceVoteNo:
			tally.No += weight
		case GovernanceVoteAbstain:
			tally.Abstain += weight
		default:
			continue
		}
		tally.Total += weight
	}
	return tally
}

func normalizeGovernanceProposal(p *GovernanceProposal) {
	if p == nil {
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.Title = strings.TrimSpace(p.Title)
	p.Description = strings.TrimSpace(p.Description)
	p.Proposer = normalizeValidatorID(p.Proposer)
	p.Target = strings.TrimSpace(p.Target)
	p.TreasuryRecipient = strings.TrimSpace(p.TreasuryRecipient)
	p.UpgradeName = strings.TrimSpace(p.UpgradeName)
	p.UpgradeVersion = strings.TrimSpace(p.UpgradeVersion)
	p.Kind = GovernanceProposalKind(strings.TrimSpace(string(p.Kind)))
	if p.Kind == GovernanceProposalEmergency || p.Kind == GovernanceProposalEmergencyPause {
		p.Emergency = true
	}
	p.PauseReason = strings.TrimSpace(p.PauseReason)
	if p.ProtocolChanges != nil {
		p.ProtocolChanges = copyProtocolGateMap(p.ProtocolChanges)
	}
	if p.Votes == nil {
		p.Votes = make(map[string]GovernanceVote)
	}
}

func validateGovernanceProposal(p GovernanceProposal) error {
	switch p.Kind {
	case GovernanceProposalValidatorUpgrade, GovernanceProposalTreasury, GovernanceProposalProtocolUpgrade, GovernanceProposalEmergency, GovernanceProposalEmergencyPause:
	default:
		return fmt.Errorf("unsupported governance proposal kind: %s", p.Kind)
	}
	if p.VotingEndHeight > 0 && p.VotingStartHeight > p.VotingEndHeight {
		return errors.New("governance voting window is invalid")
	}
	if p.Kind == GovernanceProposalTreasury {
		amount := p.TreasuryAmount
		if amount == 0 {
			amount = p.Amount
		}
		if amount <= 0 {
			return errors.New("treasury proposal amount must be positive")
		}
		if p.TreasuryRecipient == "" && p.Target == "" {
			return errors.New("treasury proposal recipient is required")
		}
	}
	if p.Kind == GovernanceProposalProtocolUpgrade || p.Kind == GovernanceProposalEmergency {
		if strings.TrimSpace(p.UpgradeName) == "" {
			return errors.New("protocol upgrade name is required")
		}
		if len(p.ProtocolChanges) == 0 {
			return errors.New("protocol upgrade changes are required")
		}
		if err := validateProtocolGateChanges(p.ProtocolChanges, p.RollbackAllowed); err != nil {
			return err
		}
	}
	if p.Kind == GovernanceProposalEmergencyPause {
		activation := p.ActivationHeight
		if activation == 0 {
			activation = p.CreatedHeight
		}
		if p.PauseUntilHeight == 0 {
			return errors.New("emergency pause until height is required")
		}
		if activation > 0 && p.PauseUntilHeight <= activation {
			return errors.New("emergency pause until height must be above activation height")
		}
		if activation > 0 && defaultEmergencyPauseMaxBlocks > 0 && p.PauseUntilHeight-activation > defaultEmergencyPauseMaxBlocks {
			return fmt.Errorf("emergency pause window exceeds max blocks: got=%d max=%d", p.PauseUntilHeight-activation, defaultEmergencyPauseMaxBlocks)
		}
	}
	return nil
}

func normalizeGovernanceVoteChoice(choice GovernanceVoteChoice) GovernanceVoteChoice {
	switch GovernanceVoteChoice(strings.ToLower(strings.TrimSpace(string(choice)))) {
	case GovernanceVoteYes:
		return GovernanceVoteYes
	case GovernanceVoteNo:
		return GovernanceVoteNo
	case GovernanceVoteAbstain:
		return GovernanceVoteAbstain
	default:
		return ""
	}
}

func normalizeProtocolUpgrade(upgrade *ProtocolUpgrade) {
	if upgrade == nil {
		return
	}
	upgrade.Name = strings.TrimSpace(upgrade.Name)
	upgrade.Version = strings.TrimSpace(upgrade.Version)
	upgrade.ProposalID = strings.TrimSpace(upgrade.ProposalID)
	upgrade.ProtocolChanges = copyProtocolGateMap(upgrade.ProtocolChanges)
}

func protocolUpgradeKey(upgrade ProtocolUpgrade) string {
	name := strings.ToLower(strings.TrimSpace(upgrade.Name))
	version := strings.ToLower(strings.TrimSpace(upgrade.Version))
	proposalID := strings.TrimSpace(upgrade.ProposalID)
	if proposalID != "" {
		return proposalID
	}
	return name + ":" + version
}

func validateProtocolGateChanges(changes map[string]uint64, rollbackAllowed bool) error {
	for rawName, proposed := range changes {
		name := normalizeProtocolGateName(rawName)
		current, ok := currentProtocolGateHeight(name)
		if !ok {
			return fmt.Errorf("unknown protocol gate: %s", rawName)
		}
		if current > 0 && proposed < current && !rollbackAllowed {
			return fmt.Errorf("protocol gate rollback rejected for %s: current=%d proposed=%d", name, current, proposed)
		}
	}
	return nil
}

func currentProtocolGateHeight(name string) (uint64, bool) {
	switch normalizeProtocolGateName(name) {
	case ProtocolGateValidatorSetActivationModelV2:
		return ValidatorSetActivationModelV2Height, true
	case ProtocolGateValidatorSetCommitmentV2:
		return ValidatorSetCommitmentV2Height, true
	case ProtocolGateValidatorSetHashV3:
		return ValidatorSetHashV3Height, true
	case ProtocolGateSyncSnapshotCheckpointV2:
		return SyncSnapshotCheckpointV2Height, true
	case ProtocolGateDTLV2:
		return ConfigDTLV2ActivationHeight, true
	default:
		return 0, false
	}
}

func applyProtocolGateHeight(name string, height uint64) error {
	switch normalizeProtocolGateName(name) {
	case ProtocolGateValidatorSetActivationModelV2:
		ValidatorSetActivationModelV2Height = height
	case ProtocolGateValidatorSetCommitmentV2:
		ValidatorSetCommitmentV2Height = height
	case ProtocolGateValidatorSetHashV3:
		ValidatorSetHashV3Height = height
	case ProtocolGateSyncSnapshotCheckpointV2:
		SyncSnapshotCheckpointV2Height = height
	case ProtocolGateDTLV2:
		ConfigDTLV2ActivationHeight = height
	default:
		return fmt.Errorf("unknown protocol gate: %s", name)
	}
	return nil
}

func normalizeProtocolGateName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "validator_set_activation_model_v2", ProtocolGateValidatorSetActivationModelV2:
		return ProtocolGateValidatorSetActivationModelV2
	case "validator_set_commitment_v2", ProtocolGateValidatorSetCommitmentV2:
		return ProtocolGateValidatorSetCommitmentV2
	case "validator_set_hash_v3", ProtocolGateValidatorSetHashV3:
		return ProtocolGateValidatorSetHashV3
	case "sync_snapshot_checkpoint_v2", ProtocolGateSyncSnapshotCheckpointV2:
		return ProtocolGateSyncSnapshotCheckpointV2
	case "dtl_v2", "dtl_contracts_v2", ProtocolGateDTLV2:
		return ProtocolGateDTLV2
	default:
		return name
	}
}

func copyProtocolGateMap(src map[string]uint64) map[string]uint64 {
	if src == nil {
		return nil
	}
	out := make(map[string]uint64, len(src))
	for key, value := range src {
		out[normalizeProtocolGateName(key)] = value
	}
	return out
}

func hashGovernanceProposal(p GovernanceProposal) string {
	p.ID = ""
	p.DeterministicPayload = ""
	raw, err := json.Marshal(governanceProposalCanonical{Proposal: p})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type governanceProposalCanonical struct {
	Proposal GovernanceProposal `json:"proposal"`
}

type governanceStateCanonical struct {
	State *GovernanceState `json:"state"`
}
