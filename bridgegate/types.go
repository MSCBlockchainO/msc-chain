package bridgegate

import (
	"encoding/json"

	"msc-chain/bridgeobserver"
	"msc-chain/bridgereconcile"
)

const (
	ManifestVersion        = "msc-bridge-tron-production-gate-v1"
	ReportVersion          = "msc-bridge-tron-production-gate-report-v1"
	DTLSnapshotVersion     = "msc-bridge-dtl-authority-snapshot-v1"
	PauseDrillVersion      = "msc-bridge-pause-drill-v1"
	SoakReportVersion      = "msc-bridge-soak-report-v1"
	ReleaseApprovalVersion = "msc-bridge-release-approval-v1"
	TronDeploymentVersion  = "msc-bridge-tron-deployment-v2"
)

type EvidenceRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type TypedEvidenceRef struct {
	Kind string `json:"kind"`
	EvidenceRef
}

type PublishedEvidenceRef struct {
	EvidenceRef
	URL             string `json:"url"`
	Auditor         string `json:"auditor"`
	PublishedAtUnix int64  `json:"published_at_unix"`
}

type ObserverMember struct {
	OperatorID string `json:"operator_id"`
	SignerID   string `json:"signer_id"`
	PublicKey  string `json:"public_key"`
}

type DTLAuthorityMember struct {
	OperatorID string `json:"operator_id"`
	Address    string `json:"address"`
	PublicKey  string `json:"public_key"`
}

type DTLSnapshotAttestation struct {
	Address   string `json:"address"`
	PublicKey string `json:"public_key"`
	Signature string `json:"signature"`
}

type TronRoleBinding struct {
	OperatorID string `json:"operator_id"`
	Address    string `json:"address"`
}

type ObserverCommittee struct {
	Threshold uint16           `json:"threshold"`
	Members   []ObserverMember `json:"members"`
}

type DTLAuthorityCommittee struct {
	Threshold uint16               `json:"threshold"`
	Members   []DTLAuthorityMember `json:"members"`
}

type RoleAssignments struct {
	Governance       TronRoleBinding   `json:"governance"`
	Guardian         TronRoleBinding   `json:"guardian"`
	ReleaseExecutor  TronRoleBinding   `json:"release_executor"`
	ReleaseCommittee []TronRoleBinding `json:"release_committee"`
}

type Manifest struct {
	Version                     string                `json:"version"`
	RouteID                     string                `json:"route_id"`
	CreatedAtUnix               int64                 `json:"created_at_unix"`
	ExpiresAtUnix               int64                 `json:"expires_at_unix"`
	SourceCommit                string                `json:"source_commit"`
	ReleaseTag                  string                `json:"release_tag"`
	MaxObserverAgeSeconds       uint64                `json:"max_observer_age_seconds"`
	MaxReconciliationAgeSeconds uint64                `json:"max_reconciliation_age_seconds"`
	ReleaseApprovalPath         string                `json:"release_approval_path"`
	Deployment                  EvidenceRef           `json:"deployment"`
	ObserverConfig              EvidenceRef           `json:"observer_config"`
	ObserverArtifact            EvidenceRef           `json:"observer_artifact"`
	ReconcilerConfig            EvidenceRef           `json:"reconciler_config"`
	ReconciliationReport        EvidenceRef           `json:"reconciliation_report"`
	DTLAuthoritySnapshot        EvidenceRef           `json:"dtl_authority_snapshot"`
	AuditReport                 PublishedEvidenceRef  `json:"audit_report"`
	PauseDrill                  EvidenceRef           `json:"pause_drill"`
	SoakReport                  EvidenceRef           `json:"soak_report"`
	IncidentRunbook             EvidenceRef           `json:"incident_runbook"`
	SourceObservers             ObserverCommittee     `json:"source_observers"`
	DTLAuthorities              DTLAuthorityCommittee `json:"dtl_authorities"`
	Roles                       RoleAssignments       `json:"roles"`
}

type TronReleaseApproval struct {
	Version        string                         `json:"version"`
	ManifestSHA256 string                         `json:"manifest_sha256"`
	Signatures     []TronReleaseApprovalSignature `json:"signatures"`
}

type TronReleaseApprovalSignature struct {
	Address   string `json:"address"`
	Signature string `json:"signature"`
}

type DTLAuthoritySnapshot struct {
	Version            string                   `json:"version"`
	CapturedAtUnix     int64                    `json:"captured_at_unix"`
	MSCHeight          uint64                   `json:"msc_height"`
	MSCStateRoot       string                   `json:"msc_state_root"`
	TokenID            string                   `json:"token_id"`
	LocalDenom         string                   `json:"local_denom"`
	Symbol             string                   `json:"symbol"`
	Decimals           uint8                    `json:"decimals"`
	TotalSupplyRaw     string                   `json:"total_supply_raw"`
	MaxSupplyRaw       string                   `json:"max_supply_raw"`
	Paused             bool                     `json:"paused"`
	AuthoritySigners   []string                 `json:"authority_signers"`
	AuthorityThreshold uint16                   `json:"authority_threshold"`
	Attestations       []DTLSnapshotAttestation `json:"attestations"`
}

type PauseDrill struct {
	Version                       string             `json:"version"`
	RouteID                       string             `json:"route_id"`
	CompletedAtUnix               int64              `json:"completed_at_unix"`
	Passed                        bool               `json:"passed"`
	BridgePauseObserved           bool               `json:"bridge_pause_observed"`
	MintRejectedWhilePaused       bool               `json:"mint_rejected_while_paused"`
	UnlockRejectedWhilePaused     bool               `json:"unlock_rejected_while_paused"`
	VerificationRemainedAvailable bool               `json:"verification_remained_available"`
	HumanApprovalRequiredToResume bool               `json:"human_approval_required_to_resume"`
	Evidence                      []TypedEvidenceRef `json:"evidence"`
}

type SoakReport struct {
	Version                  string             `json:"version"`
	RouteID                  string             `json:"route_id"`
	StartedAtUnix            int64              `json:"started_at_unix"`
	CompletedAtUnix          int64              `json:"completed_at_unix"`
	Passed                   bool               `json:"passed"`
	ObserverCount            uint16             `json:"observer_count"`
	ReconciliationChecks     uint64             `json:"reconciliation_checks"`
	CompletedDeposits        uint64             `json:"completed_deposits"`
	CompletedWithdrawals     uint64             `json:"completed_withdrawals"`
	CriticalDeficits         uint64             `json:"critical_deficits"`
	UnknownReconciliations   uint64             `json:"unknown_reconciliations"`
	AccountingDriftEvents    uint64             `json:"accounting_drift_events"`
	DuplicateExecutionEvents uint64             `json:"duplicate_execution_events"`
	ReorgDrillPassed         bool               `json:"reorg_drill_passed"`
	ReplayDrillPassed        bool               `json:"replay_drill_passed"`
	RotationDrillPassed      bool               `json:"rotation_drill_passed"`
	DailyLimitDrillPassed    bool               `json:"daily_limit_drill_passed"`
	CrashRecoveryDrillPassed bool               `json:"crash_recovery_drill_passed"`
	Evidence                 []TypedEvidenceRef `json:"evidence"`
}

type GateReport struct {
	Version                    string   `json:"version"`
	Passed                     bool     `json:"passed"`
	CheckedAtUnix              int64    `json:"checked_at_unix"`
	ManifestSHA256             string   `json:"manifest_sha256"`
	ReleaseApprovalSHA256      string   `json:"release_approval_sha256"`
	ReleaseApprovalSignatures  int      `json:"release_approval_signatures"`
	DrillEvidenceFiles         int      `json:"drill_evidence_files"`
	RouteID                    string   `json:"route_id"`
	SourceCommit               string   `json:"source_commit"`
	ReleaseTag                 string   `json:"release_tag"`
	VaultAddress               string   `json:"vault_address"`
	DeploymentTransactionHash  string   `json:"deployment_transaction_hash"`
	CheckpointID               string   `json:"checkpoint_id"`
	SourceHeight               uint64   `json:"source_height"`
	SourceObservedHeight       uint64   `json:"source_observed_height"`
	ObserverSignatures         int      `json:"observer_signatures"`
	DTLAttestations            int      `json:"dtl_attestations"`
	MSCHeight                  uint64   `json:"msc_height"`
	MSCStateRoot               string   `json:"msc_state_root"`
	ReconciliationSourceHeight uint64   `json:"reconciliation_source_height"`
	AuditSHA256                string   `json:"audit_sha256"`
	SoakHours                  uint64   `json:"soak_hours"`
	Checks                     []string `json:"checks"`
}

type tronDeploymentEnvelope struct {
	Version           string            `json:"version"`
	CreatedAt         string            `json:"created_at"`
	Testnet           bool              `json:"testnet"`
	Network           json.RawMessage   `json:"network"`
	Contract          json.RawMessage   `json:"contract"`
	Route             json.RawMessage   `json:"route"`
	GovernanceActions []json.RawMessage `json:"governance_actions"`
	ObserverConfig    json.RawMessage   `json:"observer_config"`
	ReconcilerConfig  json.RawMessage   `json:"reconciler_config"`
}

type tronDeploymentNetwork struct {
	Label                 string `json:"label"`
	ChainID               string `json:"chain_id"`
	TIP712ChainID         string `json:"tip712_chain_id"`
	GenesisBlockID        string `json:"genesis_block_id"`
	ChainName             string `json:"chain_name"`
	NativeSymbol          string `json:"native_symbol"`
	ExplorerURL           string `json:"explorer_url"`
	MinConfirmations      uint64 `json:"min_confirmations"`
	NetworkMaxFeeLimitSun string `json:"network_max_fee_limit_sun"`
	ReleaseFeeLimitSun    string `json:"release_fee_limit_sun"`
}

type tronDeploymentContract struct {
	Address                  string   `json:"address"`
	ReleaseExecutor          string   `json:"release_executor"`
	DeploymentTxHash         string   `json:"deployment_tx_hash"`
	DeploymentBlock          uint64   `json:"deployment_block"`
	RuntimeCodeHash          string   `json:"runtime_code_hash"`
	Compiler                 string   `json:"compiler"`
	TVMTarget                string   `json:"tvm_target"`
	TIP712                   bool     `json:"tip712"`
	Paused                   bool     `json:"paused"`
	DefaultAdminDelaySeconds uint64   `json:"default_admin_delay_seconds"`
	Governance               string   `json:"governance"`
	Guardian                 string   `json:"guardian"`
	MSCSourceChainID         string   `json:"msc_source_chain_id"`
	MSCSourceChainHash       string   `json:"msc_source_chain_hash"`
	CommitteeEpoch           uint64   `json:"committee_epoch"`
	CommitteeThreshold       uint16   `json:"committee_threshold"`
	CommitteeMembers         []string `json:"committee_members"`
}

type tronDeploymentRoute struct {
	RouteID               string `json:"route_id"`
	ExecutionAdapter      string `json:"execution_adapter"`
	AssetDenom            string `json:"asset_denom"`
	Symbol                string `json:"symbol"`
	TokenAddress          string `json:"token_address"`
	LocalDenom            string `json:"local_denom"`
	Decimals              uint8  `json:"decimals"`
	MinAmountRaw          string `json:"min_amount_raw"`
	MaxAmountRaw          string `json:"max_amount_raw"`
	DailyLockLimitRaw     string `json:"daily_lock_limit_raw"`
	DailyUnlockLimitRaw   string `json:"daily_unlock_limit_raw"`
	MinAmount             string `json:"min_amount"`
	MaxAmount             string `json:"max_amount"`
	DailyLockLimit        string `json:"daily_lock_limit"`
	DailyUnlockLimit      string `json:"daily_unlock_limit"`
	AuditReference        string `json:"audit_reference"`
	TokenRuntimeCodeHash  string `json:"token_runtime_code_hash"`
	TokenSymbolVerified   string `json:"token_symbol_verified"`
	TokenDecimalsVerified uint8  `json:"token_decimals_verified"`
}

type tronGovernanceActionBody struct {
	OwnerAddress     string `json:"owner_address"`
	ContractAddress  string `json:"contract_address"`
	FunctionSelector string `json:"function_selector"`
	Parameter        string `json:"parameter"`
	FeeLimit         uint64 `json:"fee_limit"`
	CallValue        uint64 `json:"call_value"`
	Visible          bool   `json:"visible"`
}

type tronGovernanceAction struct {
	Order    uint8                    `json:"order"`
	Label    string                   `json:"label"`
	Endpoint string                   `json:"endpoint"`
	Method   string                   `json:"method"`
	Body     tronGovernanceActionBody `json:"body"`
	Calldata string                   `json:"calldata"`
}

type tronDeployment struct {
	Envelope         tronDeploymentEnvelope
	Network          tronDeploymentNetwork
	Contract         tronDeploymentContract
	Route            tronDeploymentRoute
	Actions          []tronGovernanceAction
	ObserverConfig   bridgeobserver.TronConfig
	ReconcilerConfig bridgereconcile.Config
}
