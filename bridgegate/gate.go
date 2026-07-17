package bridgegate

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"msc-chain/bridgeobserver"
	"msc-chain/bridgereconcile"
)

var adminTokenEnvPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,127}$`)

func EvaluateFile(ctx context.Context, manifestPath string, options Options) (GateReport, error) {
	if !options.VerifyPublication {
		return GateReport{}, errors.New("production gate requires remote audit publication verification")
	}
	manifestPath = strings.TrimSpace(manifestPath)
	if manifestPath == "" {
		return GateReport{}, errors.New("manifest path is required")
	}
	manifestPath, err := filepath.Abs(filepath.Clean(manifestPath))
	if err != nil {
		return GateReport{}, errors.New("manifest path is invalid")
	}
	rawManifest, err := fileBytes(manifestPath, maxManifestBytes)
	if err != nil {
		return GateReport{}, fmt.Errorf("read manifest: %w", err)
	}
	var manifest Manifest
	if err := strictJSON(rawManifest, &manifest); err != nil {
		return GateReport{}, fmt.Errorf("decode manifest: %w", err)
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := validateManifest(manifest, now); err != nil {
		return GateReport{}, err
	}
	manifestHash := sha256Hex(rawManifest)

	baseDirectory := filepath.Dir(manifestPath)
	type loaded struct {
		raw  []byte
		path string
	}
	load := func(label string, reference EvidenceRef, maximum int64) (loaded, error) {
		raw, path, loadErr := loadEvidence(baseDirectory, reference, maximum)
		if loadErr != nil {
			return loaded{}, fmt.Errorf("%s evidence: %w", label, loadErr)
		}
		return loaded{raw: raw, path: path}, nil
	}
	approvalRaw, approvalPath, err := loadBundleFile(baseDirectory, manifest.ReleaseApprovalPath, maxJSONBytes)
	if err != nil {
		return GateReport{}, fmt.Errorf("release approval: %w", err)
	}
	approvalFile := loaded{raw: approvalRaw, path: approvalPath}

	deploymentFile, err := load("deployment", manifest.Deployment, maxJSONBytes)
	if err != nil {
		return GateReport{}, err
	}
	observerConfigFile, err := load("observer config", manifest.ObserverConfig, maxJSONBytes)
	if err != nil {
		return GateReport{}, err
	}
	observerArtifactFile, err := load("observer artifact", manifest.ObserverArtifact, maxJSONBytes)
	if err != nil {
		return GateReport{}, err
	}
	reconcilerConfigFile, err := load("reconciler config", manifest.ReconcilerConfig, maxJSONBytes)
	if err != nil {
		return GateReport{}, err
	}
	reconciliationFile, err := load("reconciliation report", manifest.ReconciliationReport, maxJSONBytes)
	if err != nil {
		return GateReport{}, err
	}
	dtlFile, err := load("DTL authority snapshot", manifest.DTLAuthoritySnapshot, maxJSONBytes)
	if err != nil {
		return GateReport{}, err
	}
	auditFile, err := load("audit report", manifest.AuditReport.EvidenceRef, maxEvidenceBytes)
	if err != nil {
		return GateReport{}, err
	}
	pauseFile, err := load("pause drill", manifest.PauseDrill, maxJSONBytes)
	if err != nil {
		return GateReport{}, err
	}
	soakFile, err := load("soak report", manifest.SoakReport, maxJSONBytes)
	if err != nil {
		return GateReport{}, err
	}
	runbookFile, err := load("incident runbook", manifest.IncidentRunbook, maxEvidenceBytes)
	if err != nil {
		return GateReport{}, err
	}
	paths := []string{
		manifestPath, approvalFile.path,
		deploymentFile.path, observerConfigFile.path, observerArtifactFile.path, reconcilerConfigFile.path,
		reconciliationFile.path, dtlFile.path, auditFile.path, pauseFile.path, soakFile.path, runbookFile.path,
	}
	if !uniqueStrings(paths) {
		return GateReport{}, errors.New("every production bundle reference must identify a distinct file")
	}
	if len(runbookFile.raw) < 512 {
		return GateReport{}, errors.New("incident runbook evidence is too small to be operationally useful")
	}
	if len(auditFile.raw) < 1024 {
		return GateReport{}, errors.New("independent audit report evidence is too small")
	}

	deployment, err := decodeTronDeployment(deploymentFile.raw)
	if err != nil {
		return GateReport{}, err
	}
	if err := validateTronDeployment(deployment, now); err != nil {
		return GateReport{}, fmt.Errorf("deployment: %w", err)
	}
	if err := matchRoleAssignments(manifest, deployment); err != nil {
		return GateReport{}, err
	}
	var releaseApproval TronReleaseApproval
	if err := strictJSON(approvalFile.raw, &releaseApproval); err != nil {
		return GateReport{}, fmt.Errorf("release approval: %w", err)
	}
	if err := validateReleaseApproval(releaseApproval, manifestHash, manifest, deployment); err != nil {
		return GateReport{}, fmt.Errorf("release approval: %w", err)
	}
	if deployment.Route.RouteID != manifest.RouteID || deployment.Route.AuditReference != manifest.AuditReport.URL {
		return GateReport{}, errors.New("manifest route or published audit does not match deployment")
	}

	var observerConfig bridgeobserver.TronConfig
	if err := strictJSON(observerConfigFile.raw, &observerConfig); err != nil {
		return GateReport{}, fmt.Errorf("observer config: %w", err)
	}
	observerConfig, err = bridgeobserver.ValidateTronConfig(observerConfig)
	if err != nil {
		return GateReport{}, fmt.Errorf("observer config: %w", err)
	}
	if err := matchObserverConfig(observerConfig, deployment); err != nil {
		return GateReport{}, err
	}

	var artifact bridgeobserver.Artifact
	if err := strictJSON(observerArtifactFile.raw, &artifact); err != nil {
		return GateReport{}, fmt.Errorf("observer artifact: %w", err)
	}
	if err := validateObserverArtifact(artifact, manifest, deployment, observerConfig, now); err != nil {
		return GateReport{}, fmt.Errorf("observer artifact: %w", err)
	}

	var reconcilerConfig bridgereconcile.Config
	if err := strictJSON(reconcilerConfigFile.raw, &reconcilerConfig); err != nil {
		return GateReport{}, fmt.Errorf("reconciler config: %w", err)
	}
	reconciler, err := bridgereconcile.New(reconcilerConfig)
	if err != nil {
		return GateReport{}, fmt.Errorf("reconciler config: %w", err)
	}
	reconcilerConfig = reconciler.Config()
	if err := validateReconcilerConfig(reconcilerConfig, manifest, deployment); err != nil {
		return GateReport{}, err
	}
	if err := validateProviderSeparation(observerConfig.APIURLs, reconcilerConfig.Routes[0].RPCURLs); err != nil {
		return GateReport{}, fmt.Errorf("independent provider separation: %w", err)
	}

	var reconciliation bridgereconcile.Report
	if err := strictJSON(reconciliationFile.raw, &reconciliation); err != nil {
		return GateReport{}, fmt.Errorf("reconciliation report: %w", err)
	}
	var dtlSnapshot DTLAuthoritySnapshot
	if err := strictJSON(dtlFile.raw, &dtlSnapshot); err != nil {
		return GateReport{}, fmt.Errorf("DTL authority snapshot: %w", err)
	}
	asset, routeReport, err := validateReconciliation(reconciliation, dtlSnapshot, reconcilerConfig, manifest, deployment, now)
	if err != nil {
		return GateReport{}, err
	}
	if err := validateDTLSnapshot(dtlSnapshot, manifest, asset, reconciliation, now); err != nil {
		return GateReport{}, err
	}

	var pauseDrill PauseDrill
	if err := strictJSON(pauseFile.raw, &pauseDrill); err != nil {
		return GateReport{}, fmt.Errorf("pause drill: %w", err)
	}
	if err := validatePauseDrill(pauseDrill, manifest.RouteID, now); err != nil {
		return GateReport{}, err
	}
	var soak SoakReport
	if err := strictJSON(soakFile.raw, &soak); err != nil {
		return GateReport{}, fmt.Errorf("soak report: %w", err)
	}
	if err := validateSoakReport(soak, manifest.RouteID, len(manifest.SourceObservers.Members), now); err != nil {
		return GateReport{}, err
	}
	drillEvidenceFiles := 0
	loadDrillEvidence := func(label string, evidence []TypedEvidenceRef) error {
		for _, item := range evidence {
			file, loadErr := load(label+" "+item.Kind, item.EvidenceRef, maxEvidenceBytes)
			if loadErr != nil {
				return loadErr
			}
			paths = append(paths, file.path)
			drillEvidenceFiles++
		}
		return nil
	}
	if err := loadDrillEvidence("pause drill", pauseDrill.Evidence); err != nil {
		return GateReport{}, err
	}
	if err := loadDrillEvidence("soak report", soak.Evidence); err != nil {
		return GateReport{}, err
	}
	if !uniqueStrings(paths) {
		return GateReport{}, errors.New("every production bundle reference must identify a distinct file")
	}

	if manifest.AuditReport.Auditor == "" || manifest.AuditReport.PublishedAtUnix <= 0 ||
		manifest.AuditReport.PublishedAtUnix > now.Add(5*time.Minute).Unix() {
		return GateReport{}, errors.New("audit publisher or publication time is invalid")
	}
	if _, err := publicationURL(manifest.AuditReport.URL); err != nil {
		return GateReport{}, fmt.Errorf("audit report: %w", err)
	}
	if err := verifyPublishedHash(ctx, options.HTTPClient, manifest.AuditReport.URL, normalizeSHA256(manifest.AuditReport.SHA256)); err != nil {
		return GateReport{}, fmt.Errorf("audit publication: %w", err)
	}

	checks := []string{
		"manifest_fresh_and_hash_pinned",
		"manifest_approved_by_tron_release_threshold",
		"canonical_tron_mainnet_deployment_paused",
		"observer_config_three_independent_apis",
		"observer_checkpoint_threshold_and_native_proofs_when_events_exist",
		"source_dtl_release_operators_separated",
		"reconciler_auto_pause_configured",
		"reconciliation_healthy_and_state_bound",
		"dtl_mint_authority_5_of_4_paused",
		"external_audit_hash_verified",
		"pause_drill_passed_with_raw_evidence",
		"72_hour_soak_and_adversarial_drills_passed_with_raw_evidence",
		"incident_runbook_hash_pinned",
		"external_audit_publication_fetched",
	}
	sort.Strings(checks)
	return GateReport{
		Version: ReportVersion, Passed: true, CheckedAtUnix: now.Unix(), ManifestSHA256: manifestHash,
		ReleaseApprovalSHA256: sha256Hex(approvalFile.raw), ReleaseApprovalSignatures: len(releaseApproval.Signatures),
		DrillEvidenceFiles: drillEvidenceFiles,
		RouteID:            manifest.RouteID, SourceCommit: strings.ToLower(manifest.SourceCommit), ReleaseTag: manifest.ReleaseTag,
		VaultAddress: deployment.Contract.Address, DeploymentTransactionHash: deployment.Contract.DeploymentTxHash,
		CheckpointID: artifact.Checkpoint.CheckpointID, SourceHeight: artifact.Checkpoint.Height,
		SourceObservedHeight: artifact.Checkpoint.ObservedHeight, ObserverSignatures: len(artifact.Checkpoint.ValidatorSignatures),
		DTLAttestations: len(dtlSnapshot.Attestations),
		MSCHeight:       reconciliation.MSCHeight, MSCStateRoot: reconciliation.MSCStateRoot,
		ReconciliationSourceHeight: routeReport.BlockHeight, AuditSHA256: normalizeSHA256(manifest.AuditReport.SHA256),
		SoakHours: uint64((soak.CompletedAtUnix - soak.StartedAtUnix) / 3600), Checks: checks,
	}, nil
}

func validateManifest(manifest Manifest, now time.Time) error {
	if manifest.Version != ManifestVersion || manifest.RouteID != "usdt-tron-mainnet" {
		return errors.New("manifest version or TRON mainnet route is invalid")
	}
	if manifest.CreatedAtUnix <= 0 || manifest.CreatedAtUnix > now.Add(5*time.Minute).Unix() ||
		manifest.CreatedAtUnix < now.Add(-24*time.Hour).Unix() || manifest.ExpiresAtUnix <= now.Unix() ||
		manifest.ExpiresAtUnix > manifest.CreatedAtUnix+86400 {
		return errors.New("manifest must be created within 24 hours and expire within 24 hours")
	}
	commit := strings.ToLower(strings.TrimSpace(manifest.SourceCommit))
	if (len(commit) != 40 && len(commit) != 64) || strings.Trim(commit, "0") == "" {
		return errors.New("source_commit must be a non-zero 40- or 64-character git object ID")
	}
	if _, err := hex.DecodeString(commit); err != nil {
		return errors.New("source_commit must be hexadecimal")
	}
	if bridgeobserver.NormalizeRegistryID(manifest.ReleaseTag) == "" ||
		strings.TrimSpace(manifest.ReleaseApprovalPath) == "" ||
		manifest.MaxObserverAgeSeconds < 60 || manifest.MaxObserverAgeSeconds > 900 ||
		manifest.MaxReconciliationAgeSeconds < 30 || manifest.MaxReconciliationAgeSeconds > 300 {
		return errors.New("release tag or evidence freshness policy is invalid")
	}
	return validateCommitteesAndRoles(manifest)
}

func validateCommitteesAndRoles(manifest Manifest) error {
	if len(manifest.SourceObservers.Members) < 5 || manifest.SourceObservers.Threshold < 4 ||
		int(manifest.SourceObservers.Threshold) > len(manifest.SourceObservers.Members) ||
		int(manifest.SourceObservers.Threshold) < (2*len(manifest.SourceObservers.Members)+2)/3 {
		return errors.New("source observer committee must contain at least five members with >=2/3 and >=4 threshold")
	}
	if len(manifest.DTLAuthorities.Members) < 5 || manifest.DTLAuthorities.Threshold < 4 ||
		int(manifest.DTLAuthorities.Threshold) > len(manifest.DTLAuthorities.Members) ||
		int(manifest.DTLAuthorities.Threshold) < (2*len(manifest.DTLAuthorities.Members)+2)/3 {
		return errors.New("DTL authority committee must contain at least five members with >=2/3 and >=4 threshold")
	}
	operatorIDs := make(map[string]struct{})
	addOperator := func(value string) error {
		value = bridgeobserver.NormalizeRegistryID(value)
		if value == "" {
			return errors.New("operator ID is invalid")
		}
		if _, exists := operatorIDs[value]; exists {
			return fmt.Errorf("operator %s holds more than one bridge trust role", value)
		}
		operatorIDs[value] = struct{}{}
		return nil
	}
	observerKeys, observerSigners := make(map[string]struct{}), make(map[string]struct{})
	for _, member := range manifest.SourceObservers.Members {
		if err := addOperator(member.OperatorID); err != nil {
			return err
		}
		publicKey := normalizeEd25519PublicKey(member.PublicKey)
		signerID := bridgeobserver.NormalizeRegistryID(member.SignerID)
		if publicKey == "" || signerID == "" {
			return errors.New("source observer signer ID or Ed25519 public key is invalid")
		}
		if _, exists := observerKeys[publicKey]; exists {
			return errors.New("source observer public key is duplicated")
		}
		if _, exists := observerSigners[signerID]; exists {
			return errors.New("source observer signer ID is duplicated")
		}
		observerKeys[publicKey], observerSigners[signerID] = struct{}{}, struct{}{}
	}
	dtlAddresses := make(map[string]struct{})
	dtlKeys := make(map[string]struct{})
	for _, member := range manifest.DTLAuthorities.Members {
		if err := addOperator(member.OperatorID); err != nil {
			return err
		}
		address := normalizeMSCAddress(member.Address)
		if address == "" {
			return errors.New("DTL authority address is invalid")
		}
		if _, exists := dtlAddresses[address]; exists {
			return errors.New("DTL authority address is duplicated")
		}
		publicKey := normalizeEd25519PublicKey(member.PublicKey)
		if publicKey == "" || mscAddressFromEd25519PublicKey(publicKey) != address {
			return errors.New("DTL authority public key does not derive its MSC address")
		}
		if _, exists := observerKeys[publicKey]; exists {
			return errors.New("DTL authority key overlaps the source observer committee")
		}
		if _, exists := dtlKeys[publicKey]; exists {
			return errors.New("DTL authority public key is duplicated")
		}
		dtlAddresses[address] = struct{}{}
		dtlKeys[publicKey] = struct{}{}
	}
	roles := []TronRoleBinding{manifest.Roles.Governance, manifest.Roles.Guardian, manifest.Roles.ReleaseExecutor}
	roles = append(roles, manifest.Roles.ReleaseCommittee...)
	if len(manifest.Roles.ReleaseCommittee) < 5 {
		return errors.New("release committee role mapping must contain at least five members")
	}
	tronAddresses := make(map[string]struct{})
	for _, role := range roles {
		if err := addOperator(role.OperatorID); err != nil {
			return err
		}
		address := bridgeobserver.NormalizeTronAddress(role.Address)
		if address == "" {
			return errors.New("TRON privileged role address is invalid")
		}
		if _, exists := tronAddresses[address]; exists {
			return errors.New("TRON privileged role address is duplicated")
		}
		tronAddresses[address] = struct{}{}
	}
	return nil
}

func normalizeEd25519PublicKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 64 || strings.Trim(value, "0") == "" {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func normalizeMSCAddress(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 45 || !strings.HasPrefix(value, "msc") || strings.Trim(value[3:], "0") == "" {
		return ""
	}
	raw, err := hex.DecodeString(value[3:])
	if err != nil || len(raw) != 21 || raw[0] != 0x01 {
		return ""
	}
	return value
}

func mscAddressFromEd25519PublicKey(value string) string {
	raw, err := hex.DecodeString(normalizeEd25519PublicKey(value))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return ""
	}
	payload := append([]byte("MSC-ADDR|91938|"), raw...)
	first := sha256.Sum256(payload)
	second := sha256.Sum256(first[:])
	addressPayload := append([]byte{0x01}, second[:20]...)
	return "msc" + hex.EncodeToString(addressPayload)
}

func uniqueStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}
