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
	// `GovernanceProposalValidatorUpgrade` defines the constant value used by this package.
	GovernanceProposalValidatorUpgrade GovernanceProposalKind = "validator"
	// `GovernanceProposalTreasury` defines the constant value used by this package.
	GovernanceProposalTreasury GovernanceProposalKind = "treasury"
	// `GovernanceProposalProtocolUpgrade` defines the constant value used by this package.
	GovernanceProposalProtocolUpgrade GovernanceProposalKind = "protocol_upgrade"
	// `GovernanceProposalEmergency` defines the constant value used by this package.
	GovernanceProposalEmergency GovernanceProposalKind = "emergency"
	// `GovernanceProposalEmergencyPause` defines the constant value used by this package.
	GovernanceProposalEmergencyPause GovernanceProposalKind = "emergency_pause"
)

type GovernanceProposalStatus string

const (
	// `GovernanceProposalPending` defines the constant value used by this package.
	GovernanceProposalPending GovernanceProposalStatus = "pending"
	// `GovernanceProposalActive` defines the constant value used by this package.
	GovernanceProposalActive GovernanceProposalStatus = "active"
	// `GovernanceProposalApproved` defines the constant value used by this package.
	GovernanceProposalApproved GovernanceProposalStatus = "approved"
	// `GovernanceProposalRejected` defines the constant value used by this package.
	GovernanceProposalRejected GovernanceProposalStatus = "rejected"
	// `GovernanceProposalScheduled` defines the constant value used by this package.
	GovernanceProposalScheduled GovernanceProposalStatus = "scheduled"
	// `GovernanceProposalApplied` defines the constant value used by this package.
	GovernanceProposalApplied GovernanceProposalStatus = "applied"
)

type GovernanceVoteChoice string

const (
	// `GovernanceVoteYes` defines the constant value used by this package.
	GovernanceVoteYes GovernanceVoteChoice = "yes"
	// `GovernanceVoteNo` defines the constant value used by this package.
	GovernanceVoteNo GovernanceVoteChoice = "no"
	// `GovernanceVoteAbstain` defines the constant value used by this package.
	GovernanceVoteAbstain GovernanceVoteChoice = "abstain"
)

const (
	// `ProtocolGateValidatorSetActivationModelV2` defines the constant value used by this package.
	ProtocolGateValidatorSetActivationModelV2 = "validator_set_activation_model_v2_height"
	// `ProtocolGateValidatorSetCommitmentV2` defines the constant value used by this package.
	ProtocolGateValidatorSetCommitmentV2 = "validator_set_commitment_v2_height"
	// `ProtocolGateValidatorSetHashV3` defines the constant value used by this package.
	ProtocolGateValidatorSetHashV3 = "validator_set_hash_v3_height"
	// `ProtocolGateSyncSnapshotCheckpointV2` defines the constant value used by this package.
	ProtocolGateSyncSnapshotCheckpointV2 = "sync_snapshot_checkpoint_v2_height"
	// `ProtocolGateDTLV2` defines the constant value used by this package.
	ProtocolGateDTLV2 = "dtl_v2_activation_height"
	// `ProtocolGateSupplyCapV2` records governance-approved max supply changes.
	ProtocolGateSupplyCapV2 = "supply_cap_v2_activation_height"

	// `defaultProtocolUpgradeActivationDelay` defines the constant value used by this package.
	defaultProtocolUpgradeActivationDelay uint64 = 10
	// `defaultEmergencyPauseMaxBlocks` defines the constant value used by this package.
	defaultEmergencyPauseMaxBlocks uint64 = 1000
)

// `governanceStateDBKey` stores the key used to access the related value.
var governanceStateDBKey = []byte("governance:state:v1")

type GovernanceVote struct {
	// `Voter` stores the value associated with this record.
	Voter string `json:"voter"`
	// `Choice` stores the value associated with this record.
	Choice GovernanceVoteChoice `json:"choice"`
	// `Weight` stores the value associated with this record.
	Weight int `json:"weight"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"signature,omitempty"`
}

type GovernanceTally struct {
	// `Yes` stores the value associated with this record.
	Yes int `json:"yes"`
	// `No` stores the value associated with this record.
	No int `json:"no"`
	// `Abstain` stores the value associated with this record.
	Abstain int `json:"abstain"`
	// `Total` stores the measured quantity used by this operation.
	Total int `json:"total"`
	// `Required` stores the request data being processed.
	Required int `json:"required"`
}

type GovernanceProposal struct {
	// `ID` stores the current position in the related collection.
	ID string `json:"id"`
	// `Kind` stores the value associated with this record.
	Kind GovernanceProposalKind `json:"kind"`
	// `Title` stores the value associated with this record.
	Title string `json:"title,omitempty"`
	// `Description` stores the value associated with this record.
	Description string `json:"description,omitempty"`
	// `Proposer` stores the value associated with this record.
	Proposer string `json:"proposer,omitempty"`
	// `CreatedHeight` stores the value associated with this record.
	CreatedHeight uint64 `json:"created_height"`
	// `VotingStartHeight` stores the value associated with this record.
	VotingStartHeight uint64 `json:"voting_start_height"`
	// `VotingEndHeight` stores the value associated with this record.
	VotingEndHeight uint64 `json:"voting_end_height"`
	// `ActivationHeight` stores the value associated with this record.
	ActivationHeight uint64 `json:"activation_height"`
	// `Target` stores the value associated with this record.
	Target string `json:"target,omitempty"`
	// `Amount` stores the value associated with this record.
	Amount int64 `json:"amount,omitempty"`
	// `TreasuryRecipient` stores the value associated with this record.
	TreasuryRecipient string `json:"treasury_recipient,omitempty"`
	// `TreasuryAmount` stores the value associated with this record.
	TreasuryAmount int64 `json:"treasury_amount,omitempty"`
	// `UpgradeName` stores the value associated with this record.
	UpgradeName string `json:"upgrade_name,omitempty"`
	// `UpgradeVersion` stores the value associated with this record.
	UpgradeVersion string `json:"upgrade_version,omitempty"`
	// `ProtocolChanges` stores the value associated with this record.
	ProtocolChanges map[string]uint64 `json:"protocol_changes,omitempty"`
	// `RequestedMaxSupply` stores an explicit max-supply change request.
	RequestedMaxSupply int64 `json:"requested_max_supply,omitempty"`
	// `PreviousMaxSupply` stores the cap the proposal is allowed to replace.
	PreviousMaxSupply int64 `json:"previous_max_supply,omitempty"`
	// `SupplyChangeReason` stores the public rationale for a supply cap change.
	SupplyChangeReason string `json:"supply_change_reason,omitempty"`
	// `RollbackAllowed` stores whether the related condition is satisfied.
	RollbackAllowed bool `json:"rollback_allowed,omitempty"`
	// `Emergency` stores the value associated with this record.
	Emergency bool `json:"emergency,omitempty"`
	// `PauseUntilHeight` stores the value associated with this record.
	PauseUntilHeight uint64 `json:"pause_until_height,omitempty"`
	// `PauseReason` stores the value associated with this record.
	PauseReason string `json:"pause_reason,omitempty"`
	// `Status` stores the value associated with this record.
	Status GovernanceProposalStatus `json:"status"`
	// `Votes` stores the value associated with this record.
	Votes map[string]GovernanceVote `json:"votes,omitempty"`
	// `Tally` stores the value associated with this record.
	Tally GovernanceTally `json:"tally"`
	// `AppliedAtHeight` stores the value associated with this record.
	AppliedAtHeight uint64 `json:"applied_at_height,omitempty"`
	// `DeterministicPayload` stores the value associated with this record.
	DeterministicPayload string `json:"deterministic_payload,omitempty"`
}

type ProtocolUpgrade struct {
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Version` stores the value associated with this record.
	Version string `json:"version"`
	// `ActivationHeight` stores the value associated with this record.
	ActivationHeight uint64 `json:"activation_height"`
	// `ProtocolChanges` stores the value associated with this record.
	ProtocolChanges map[string]uint64 `json:"protocol_changes,omitempty"`
	// `ProposalID` stores the value associated with this record.
	ProposalID string `json:"proposal_id,omitempty"`
	// `RollbackAllowed` stores whether the related condition is satisfied.
	RollbackAllowed bool `json:"rollback_allowed,omitempty"`
	// `Emergency` stores the value associated with this record.
	Emergency bool `json:"emergency,omitempty"`
	// `Activated` stores the value associated with this record.
	Activated bool `json:"activated,omitempty"`
	// `ActivatedHeight` stores the value associated with this record.
	ActivatedHeight uint64 `json:"activated_height,omitempty"`
}

type SupplyChangeRecord struct {
	// `ProposalID` stores the governance proposal that authorized this record.
	ProposalID string `json:"proposal_id"`
	// `PreviousMaxSupply` stores the cap before the upgrade.
	PreviousMaxSupply int64 `json:"previous_max_supply"`
	// `NewMaxSupply` stores the cap after the upgrade.
	NewMaxSupply int64 `json:"new_max_supply"`
	// `Reason` stores the public rationale carried by the proposal.
	Reason string `json:"reason,omitempty"`
	// `ProtocolVersion` stores the binary/protocol version required.
	ProtocolVersion string `json:"protocol_version"`
	// `ActivationHeight` stores the timelocked activation height.
	ActivationHeight uint64 `json:"activation_height"`
	// `AppliedAtHeight` stores the height that wrote the record.
	AppliedAtHeight uint64 `json:"applied_at_height"`
	// `Tally` stores the quorum proof used when the proposal was applied.
	Tally GovernanceTally `json:"tally"`
}

type ProtocolUpgradeManager struct {
	// `CurrentVersion` stores the value associated with this record.
	CurrentVersion string `json:"current_version"`
	// `MinActivationDelay` stores the value associated with this record.
	MinActivationDelay uint64 `json:"min_activation_delay"`
	// `Scheduled` stores the value associated with this record.
	Scheduled []ProtocolUpgrade `json:"scheduled,omitempty"`
	// `Activated` stores the value associated with this record.
	Activated map[string]ProtocolUpgrade `json:"activated,omitempty"`
	// `LastActivatedHeight` stores the value associated with this record.
	LastActivatedHeight uint64 `json:"last_activated_height,omitempty"`
}

type GovernanceEmergencyPauseState struct {
	// `Active` stores the value associated with this record.
	Active bool `json:"active"`
	// `ProposalID` stores the value associated with this record.
	ProposalID string `json:"proposal_id,omitempty"`
	// `Reason` stores the value associated with this record.
	Reason string `json:"reason,omitempty"`
	// `ActivatedHeight` stores the value associated with this record.
	ActivatedHeight uint64 `json:"activated_height,omitempty"`
	// `PauseUntilHeight` stores the value associated with this record.
	PauseUntilHeight uint64 `json:"pause_until_height,omitempty"`
	// `ReleasedHeight` stores the value associated with this record.
	ReleasedHeight uint64 `json:"released_height,omitempty"`
}

type GovernanceState struct {
	// `Proposals` stores the value associated with this record.
	Proposals map[string]*GovernanceProposal `json:"proposals"`
	// `UpgradeManager` stores the value associated with this record.
	UpgradeManager ProtocolUpgradeManager `json:"upgrade_manager"`
	// `TreasuryBalance` stores the value associated with this record.
	TreasuryBalance int64 `json:"treasury_balance"`
	// `EmergencyPause` stores the value associated with this record.
	EmergencyPause GovernanceEmergencyPauseState `json:"emergency_pause"`
	// `CurrentMaxSupply` stores the active governance-recognized supply cap.
	CurrentMaxSupply int64 `json:"current_max_supply"`
	// `SupplyChangeRecords` stores explicit supply-cap governance records.
	SupplyChangeRecords []SupplyChangeRecord `json:"supply_change_records,omitempty"`
}

type GovernanceMetricsSnapshot struct {
	// `ProposalsTotal` stores the measured quantity used by this operation.
	ProposalsTotal int
	// `ProposalsActive` stores the value associated with this record.
	ProposalsActive int
	// `ProposalsApproved` stores the value associated with this record.
	ProposalsApproved int
	// `ProposalsRejected` stores the value associated with this record.
	ProposalsRejected int
	// `ProposalsScheduled` stores the value associated with this record.
	ProposalsScheduled int
	// `ProposalsApplied` stores the value associated with this record.
	ProposalsApplied int
	// `TreasuryBalance` stores the value associated with this record.
	TreasuryBalance int64
	// `ProtocolScheduled` stores the value associated with this record.
	ProtocolScheduled int
	// `ProtocolActivated` stores the value associated with this record.
	ProtocolActivated int
	// `EmergencyPauseActive` stores the value associated with this record.
	EmergencyPauseActive bool
	// `EmergencyPauseUntil` stores the value associated with this record.
	EmergencyPauseUntil uint64
}

// NewGovernanceState creates a new governance state.
func NewGovernanceState() *GovernanceState {
	return &GovernanceState{
		Proposals:           make(map[string]*GovernanceProposal),
		UpgradeManager:      NewProtocolUpgradeManager(),
		CurrentMaxSupply:    FixedTotalSupply,
		SupplyChangeRecords: make([]SupplyChangeRecord, 0),
	}
}

// NewProtocolUpgradeManager creates a new protocol upgrade manager.
func NewProtocolUpgradeManager() ProtocolUpgradeManager {
	return ProtocolUpgradeManager{
		CurrentVersion:     Version,
		MinActivationDelay: defaultProtocolUpgradeActivationDelay,
		Scheduled:          make([]ProtocolUpgrade, 0),
		Activated:          make(map[string]ProtocolUpgrade),
	}
}

// ensure implements the ensure helper.
func (s *GovernanceState) ensure() {
	if s.Proposals == nil {
		s.Proposals = make(map[string]*GovernanceProposal)
	}
	if s.CurrentMaxSupply == 0 {
		s.CurrentMaxSupply = FixedTotalSupply
	}
	if s.SupplyChangeRecords == nil {
		s.SupplyChangeRecords = make([]SupplyChangeRecord, 0)
	}
	s.UpgradeManager.ensure()
}

// ensure implements the ensure helper.
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

// SubmitProposal implements the submit proposal helper.
func (s *GovernanceState) SubmitProposal(proposal GovernanceProposal) (string, error) {
	if s == nil {
		return "", errors.New("governance state is nil")
	}
	s.ensure()
	normalizeGovernanceProposal(&proposal)
	// `err` stores the error produced by this operation.
	if err := validateGovernanceProposal(proposal); err != nil {
		return "", err
	}
	if proposal.RequestedMaxSupply > 0 {
		if proposal.PreviousMaxSupply == 0 {
			proposal.PreviousMaxSupply = s.CurrentMaxSupply
		}
		if err := s.validateSupplyChangeProposal(proposal); err != nil {
			return "", err
		}
	}
	if proposal.ID == "" {
		proposal.ID = hashGovernanceProposal(proposal)
	}
	// `exists` stores whether the related condition is satisfied.
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
	// `copyProposal` stores the value produced by this operation.
	copyProposal := proposal
	s.Proposals[proposal.ID] = &copyProposal
	return proposal.ID, nil
}

// CastVote implements the cast vote helper.
func (s *GovernanceState) CastVote(proposalID string, voter string, choice GovernanceVoteChoice, height uint64, registry map[string]ValidatorRecord) error {
	if s == nil {
		return errors.New("governance state is nil")
	}
	s.ensure()
	proposalID = strings.TrimSpace(proposalID)
	// `p` and `ok` store whether the related condition is satisfied.
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
	// `exists` stores whether the related condition is satisfied.
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

// FinalizeProposal implements the finalize proposal helper.
func (s *GovernanceState) FinalizeProposal(proposalID string, height uint64, registry map[string]ValidatorRecord) (GovernanceTally, error) {
	if s == nil {
		return GovernanceTally{}, errors.New("governance state is nil")
	}
	s.ensure()
	// `p` and `ok` store whether the related condition is satisfied.
	p, ok := s.Proposals[strings.TrimSpace(proposalID)]
	if !ok || p == nil {
		return GovernanceTally{}, fmt.Errorf("governance proposal not found: %s", proposalID)
	}
	// `tally` stores the value produced by this operation.
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

// ApplyApprovedProposal applies approved proposal.
func (s *GovernanceState) ApplyApprovedProposal(proposalID string, height uint64) error {
	if s == nil {
		return errors.New("governance state is nil")
	}
	s.ensure()
	// `p` and `ok` store whether the related condition is satisfied.
	p, ok := s.Proposals[strings.TrimSpace(proposalID)]
	if !ok || p == nil {
		return fmt.Errorf("governance proposal not found: %s", proposalID)
	}
	if p.Status != GovernanceProposalApproved && p.Status != GovernanceProposalScheduled {
		return fmt.Errorf("governance proposal is not approved: %s", p.Status)
	}
	if p.ActivationHeight > 0 && height < p.ActivationHeight {
		if p.Kind == GovernanceProposalProtocolUpgrade || p.Kind == GovernanceProposalEmergency {
			if p.RequestedMaxSupply > 0 {
				if err := s.validateSupplyChangeProposal(*p); err != nil {
					return err
				}
			}
			// `err` stores the error produced by this operation.
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
		// `amount` stores the value produced by this operation.
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
		if p.RequestedMaxSupply > 0 {
			if err := s.validateApprovedSupplyChange(p, height); err != nil {
				return err
			}
		}
		// `err` stores the error produced by this operation.
		if err := s.scheduleProposalUpgrade(p, height); err != nil {
			return err
		}
		// `err` stores the error produced by this operation.
		if _, err := s.UpgradeManager.ActivateDue(height); err != nil {
			return err
		}
		if p.RequestedMaxSupply > 0 {
			s.recordSupplyCapChange(p, height)
		}
	case GovernanceProposalEmergencyPause:
		// `err` stores the error produced by this operation.
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

// validateSupplyChangeProposal enforces that a max-supply increase cannot be
// hidden inside ordinary governance and cannot exceed the compiled protocol cap.
func (s *GovernanceState) validateSupplyChangeProposal(p GovernanceProposal) error {
	if p.RequestedMaxSupply == 0 {
		return nil
	}
	if s == nil {
		return errors.New("governance state is nil")
	}
	s.ensure()
	if p.RequestedMaxSupply < 0 {
		return errors.New("requested max supply must be positive")
	}
	if p.Kind != GovernanceProposalProtocolUpgrade {
		return errors.New("max supply increase requires a protocol upgrade proposal")
	}
	if p.Emergency || p.RollbackAllowed {
		return errors.New("max supply increase cannot be emergency or rollback-enabled")
	}
	if strings.TrimSpace(p.UpgradeVersion) == "" {
		return errors.New("max supply increase requires a new protocol version")
	}
	if strings.EqualFold(strings.TrimSpace(p.UpgradeVersion), strings.TrimSpace(s.UpgradeManager.CurrentVersion)) {
		return errors.New("max supply increase requires a protocol version upgrade")
	}
	if strings.TrimSpace(p.SupplyChangeReason) == "" {
		return errors.New("max supply increase requires an explicit supply change reason")
	}
	if p.PreviousMaxSupply != s.CurrentMaxSupply {
		return fmt.Errorf("max supply proposal previous cap mismatch: got=%d want=%d", p.PreviousMaxSupply, s.CurrentMaxSupply)
	}
	if p.RequestedMaxSupply <= s.CurrentMaxSupply {
		return fmt.Errorf("max supply proposal must increase cap: current=%d requested=%d", s.CurrentMaxSupply, p.RequestedMaxSupply)
	}
	if p.RequestedMaxSupply > FixedTotalSupply {
		return fmt.Errorf("max supply proposal requires protocol binary with requested cap: requested=%d compiled=%d", p.RequestedMaxSupply, FixedTotalSupply)
	}
	if p.VotingEndHeight == 0 {
		return errors.New("max supply increase requires a bounded voting window")
	}
	if p.ActivationHeight == 0 || p.ActivationHeight <= p.VotingEndHeight {
		return errors.New("max supply increase requires activation after voting end")
	}
	if p.ActivationHeight < p.VotingEndHeight+s.UpgradeManager.MinActivationDelay {
		return fmt.Errorf("max supply increase timelock must be at least %d blocks after voting end", s.UpgradeManager.MinActivationDelay)
	}
	gateHeight, ok := p.ProtocolChanges[ProtocolGateSupplyCapV2]
	if !ok || gateHeight != p.ActivationHeight {
		return errors.New("max supply increase requires matching supply cap protocol gate")
	}
	return nil
}

func (s *GovernanceState) validateApprovedSupplyChange(p *GovernanceProposal, height uint64) error {
	if s == nil || p == nil || p.RequestedMaxSupply == 0 {
		return nil
	}
	if p.Tally.Required <= 0 || p.Tally.Yes < p.Tally.Required {
		return errors.New("max supply increase requires quorum approval")
	}
	if p.ActivationHeight == 0 || height < p.ActivationHeight {
		return errors.New("max supply increase timelock not reached")
	}
	return s.validateSupplyChangeProposal(*p)
}

func (s *GovernanceState) recordSupplyCapChange(p *GovernanceProposal, height uint64) {
	if s == nil || p == nil || p.RequestedMaxSupply == 0 {
		return
	}
	for _, record := range s.SupplyChangeRecords {
		if strings.EqualFold(strings.TrimSpace(record.ProposalID), strings.TrimSpace(p.ID)) {
			s.CurrentMaxSupply = record.NewMaxSupply
			return
		}
	}
	record := SupplyChangeRecord{
		ProposalID:        strings.TrimSpace(p.ID),
		PreviousMaxSupply: s.CurrentMaxSupply,
		NewMaxSupply:      p.RequestedMaxSupply,
		Reason:            strings.TrimSpace(p.SupplyChangeReason),
		ProtocolVersion:   strings.TrimSpace(p.UpgradeVersion),
		ActivationHeight:  p.ActivationHeight,
		AppliedAtHeight:   height,
		Tally:             p.Tally,
	}
	s.SupplyChangeRecords = append(s.SupplyChangeRecords, record)
	s.CurrentMaxSupply = p.RequestedMaxSupply
}

// activateEmergencyPause implements the activate emergency pause helper.
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
	// `reason` stores the value produced by this operation.
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

// EmergencyPauseActiveAt implements the emergency pause active at helper.
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

// ExpireEmergencyPause implements the expire emergency pause helper.
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

// scheduleProposalUpgrade implements the schedule proposal upgrade helper.
func (s *GovernanceState) scheduleProposalUpgrade(p *GovernanceProposal, currentHeight uint64) error {
	if s == nil || p == nil {
		return errors.New("governance upgrade proposal is nil")
	}
	if p.Kind != GovernanceProposalProtocolUpgrade && p.Kind != GovernanceProposalEmergency {
		return nil
	}
	// `baseHeight` stores the value produced by this operation.
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

// Schedule implements the schedule helper.
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
	// `err` stores the error produced by this operation.
	if err := validateProtocolGateChanges(upgrade.ProtocolChanges, upgrade.RollbackAllowed); err != nil {
		return err
	}
	// `key` stores the key used to access the related value.
	key := protocolUpgradeKey(upgrade)
	// `exists` stores whether the related condition is satisfied.
	if _, exists := m.Activated[key]; exists {
		return nil
	}
	// `scheduled` tracks the current values while iterating.
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

// ActivateDue implements the activate due helper.
func (m *ProtocolUpgradeManager) ActivateDue(height uint64) ([]ProtocolUpgrade, error) {
	m.ensure()
	// `activated` stores the value produced by this operation.
	activated := make([]ProtocolUpgrade, 0)
	// `remaining` stores the value produced by this operation.
	remaining := m.Scheduled[:0]
	// `upgrade` tracks the current values while iterating.
	for _, upgrade := range m.Scheduled {
		if upgrade.ActivationHeight > height {
			remaining = append(remaining, upgrade)
			continue
		}
		// `err` stores the error produced by this operation.
		if err := validateProtocolGateChanges(upgrade.ProtocolChanges, upgrade.RollbackAllowed); err != nil {
			return nil, err
		}
		// `name` and `gateHeight` track the current values while iterating.
		for name, gateHeight := range upgrade.ProtocolChanges {
			// `err` stores the error produced by this operation.
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

// Hash implements the hash helper.
func (s *GovernanceState) Hash() string {
	if s == nil {
		return ""
	}
	s.ensure()
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(governanceStateCanonical{s})
	if err != nil {
		return ""
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// PersistGovernanceState persists governance state.
func (n *Node) PersistGovernanceState(state *GovernanceState) error {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return errors.New("node meta db unavailable")
	}
	if state == nil {
		state = NewGovernanceState()
	}
	state.ensure()
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return n.DB.Meta.Update(func(txn *Txn) error {
		return txn.Set(governanceStateDBKey, raw)
	})
}

// LoadGovernanceState loads governance state.
func (n *Node) LoadGovernanceState() (*GovernanceState, error) {
	if n == nil || n.DB == nil || n.DB.Meta == nil {
		return nil, errors.New("node meta db unavailable")
	}
	// `out` stores the result produced by this operation.
	out := NewGovernanceState()
	// `err` stores the error produced by this operation.
	err := n.DB.Meta.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
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

// governanceMetricsSnapshot implements the governance metrics snapshot helper.
func (n *Node) governanceMetricsSnapshot() GovernanceMetricsSnapshot {
	if n == nil {
		return GovernanceMetricsSnapshot{}
	}
	// `state` and `err` store the error produced by this operation.
	state, err := n.LoadGovernanceState()
	if err != nil || state == nil {
		return GovernanceMetricsSnapshot{}
	}
	state.ensure()
	// `out` stores the result produced by this operation.
	out := GovernanceMetricsSnapshot{
		ProposalsTotal:    len(state.Proposals),
		TreasuryBalance:   state.TreasuryBalance,
		ProtocolScheduled: len(state.UpgradeManager.Scheduled),
		ProtocolActivated: len(state.UpgradeManager.Activated),
	}
	// `height` stores the value produced by this operation.
	height := uint64(0)
	if n.Blockchain != nil {
		// `h` stores the value produced by this operation.
		if h := n.Blockchain.Height(); h > 0 {
			height = uint64(h)
		}
	}
	out.EmergencyPauseActive = state.EmergencyPauseActiveAt(height)
	out.EmergencyPauseUntil = state.EmergencyPause.PauseUntilHeight
	// `proposal` tracks the current values while iterating.
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

// governanceAuthorityIDs implements the governance authority i ds helper.
func governanceAuthorityIDs(registry map[string]ValidatorRecord) []string {
	// `governance` stores the value produced by this operation.
	governance := make([]string, 0)
	// `active` stores the value produced by this operation.
	active := make([]string, 0)
	// `id` and `rec` track the current position in the related collection.
	for id, rec := range registry {
		// `normalized` stores the value produced by this operation.
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

// tallyGovernanceVotes implements the tally governance votes helper.
func tallyGovernanceVotes(p *GovernanceProposal, registry map[string]ValidatorRecord) GovernanceTally {
	// `authorities` stores the value produced by this operation.
	authorities := governanceAuthorityIDs(registry)
	// `required` stores the request data being processed.
	required := coreRegistryRequiredSignatures(len(authorities), 0)
	if required == 0 {
		required = strictExecSupermajority(len(authorities))
	}
	// `allowed` stores whether the related condition is satisfied.
	allowed := make(map[string]struct{}, len(authorities))
	// `id` tracks the current position in the related collection.
	for _, id := range authorities {
		allowed[id] = struct{}{}
	}
	// `tally` stores the value produced by this operation.
	tally := GovernanceTally{Required: required}
	if p == nil {
		return tally
	}
	// `voter` and `vote` track the current values while iterating.
	for voter, vote := range p.Votes {
		// `normalized` stores the value produced by this operation.
		normalized := normalizeValidatorID(voter)
		// `ok` stores whether the related condition is satisfied.
		if _, ok := allowed[normalized]; !ok {
			continue
		}
		// `weight` stores the value produced by this operation.
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

// normalizeGovernanceProposal normalizes governance proposal.
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
	p.SupplyChangeReason = strings.TrimSpace(p.SupplyChangeReason)
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

// validateGovernanceProposal validates governance proposal.
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
		// `amount` stores the value produced by this operation.
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
	if p.RequestedMaxSupply < 0 {
		return errors.New("requested max supply must be positive")
	}
	if p.RequestedMaxSupply > 0 {
		if p.Kind != GovernanceProposalProtocolUpgrade {
			return errors.New("max supply increase requires a protocol upgrade proposal")
		}
		if p.Emergency || p.RollbackAllowed {
			return errors.New("max supply increase cannot be emergency or rollback-enabled")
		}
		if strings.TrimSpace(p.UpgradeVersion) == "" {
			return errors.New("max supply increase requires a new protocol version")
		}
		if strings.TrimSpace(p.SupplyChangeReason) == "" {
			return errors.New("max supply increase requires an explicit supply change reason")
		}
		if p.VotingEndHeight == 0 {
			return errors.New("max supply increase requires a bounded voting window")
		}
		if p.ActivationHeight == 0 || p.ActivationHeight <= p.VotingEndHeight {
			return errors.New("max supply increase requires activation after voting end")
		}
		if gateHeight, ok := p.ProtocolChanges[ProtocolGateSupplyCapV2]; !ok || gateHeight != p.ActivationHeight {
			return errors.New("max supply increase requires matching supply cap protocol gate")
		}
	}
	if p.Kind == GovernanceProposalProtocolUpgrade || p.Kind == GovernanceProposalEmergency {
		if strings.TrimSpace(p.UpgradeName) == "" {
			return errors.New("protocol upgrade name is required")
		}
		if len(p.ProtocolChanges) == 0 {
			return errors.New("protocol upgrade changes are required")
		}
		// `err` stores the error produced by this operation.
		if err := validateProtocolGateChanges(p.ProtocolChanges, p.RollbackAllowed); err != nil {
			return err
		}
	}
	if p.Kind == GovernanceProposalEmergencyPause {
		// `activation` stores the value produced by this operation.
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

// normalizeGovernanceVoteChoice normalizes governance vote choice.
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

// normalizeProtocolUpgrade normalizes protocol upgrade.
func normalizeProtocolUpgrade(upgrade *ProtocolUpgrade) {
	if upgrade == nil {
		return
	}
	upgrade.Name = strings.TrimSpace(upgrade.Name)
	upgrade.Version = strings.TrimSpace(upgrade.Version)
	upgrade.ProposalID = strings.TrimSpace(upgrade.ProposalID)
	upgrade.ProtocolChanges = copyProtocolGateMap(upgrade.ProtocolChanges)
}

// protocolUpgradeKey implements the protocol upgrade key helper.
func protocolUpgradeKey(upgrade ProtocolUpgrade) string {
	// `name` stores the value produced by this operation.
	name := strings.ToLower(strings.TrimSpace(upgrade.Name))
	// `version` stores the value produced by this operation.
	version := strings.ToLower(strings.TrimSpace(upgrade.Version))
	// `proposalID` stores the value produced by this operation.
	proposalID := strings.TrimSpace(upgrade.ProposalID)
	if proposalID != "" {
		return proposalID
	}
	return name + ":" + version
}

// validateProtocolGateChanges validates protocol gate changes.
func validateProtocolGateChanges(changes map[string]uint64, rollbackAllowed bool) error {
	// `rawName` and `proposed` track the current values while iterating.
	for rawName, proposed := range changes {
		// `name` stores the value produced by this operation.
		name := normalizeProtocolGateName(rawName)
		// `current` and `ok` store whether the related condition is satisfied.
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

// currentProtocolGateHeight returns current protocol gate height.
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
	case ProtocolGateSupplyCapV2:
		return SupplyCapV2ActivationHeight, true
	default:
		return 0, false
	}
}

// applyProtocolGateHeight applies protocol gate height.
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
	case ProtocolGateSupplyCapV2:
		SupplyCapV2ActivationHeight = height
	default:
		return fmt.Errorf("unknown protocol gate: %s", name)
	}
	return nil
}

// normalizeProtocolGateName normalizes protocol gate name.
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
	case "supply_cap_v2", "max_supply_v2", ProtocolGateSupplyCapV2:
		return ProtocolGateSupplyCapV2
	default:
		return name
	}
}

// copyProtocolGateMap copies protocol gate map.
func copyProtocolGateMap(src map[string]uint64) map[string]uint64 {
	if src == nil {
		return nil
	}
	// `out` stores the result produced by this operation.
	out := make(map[string]uint64, len(src))
	// `key` and `value` track the key used to access the related value.
	for key, value := range src {
		out[normalizeProtocolGateName(key)] = value
	}
	return out
}

// hashGovernanceProposal implements the hash governance proposal helper.
func hashGovernanceProposal(p GovernanceProposal) string {
	p.ID = ""
	p.DeterministicPayload = ""
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.Marshal(governanceProposalCanonical{Proposal: p})
	if err != nil {
		return ""
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type governanceProposalCanonical struct {
	// `Proposal` stores the value associated with this record.
	Proposal GovernanceProposal `json:"proposal"`
}

type governanceStateCanonical struct {
	// `State` stores the value associated with this record.
	State *GovernanceState `json:"state"`
}
