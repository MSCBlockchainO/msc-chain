package bridgegate

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"sort"
	"strings"
	"time"

	"msc-chain/bridgeobserver"
	"msc-chain/bridgereconcile"
)

func matchRoleAssignments(manifest Manifest, deployment tronDeployment) error {
	contract := deployment.Contract
	if manifest.Roles.Governance.Address != contract.Governance ||
		manifest.Roles.Guardian.Address != contract.Guardian ||
		manifest.Roles.ReleaseExecutor.Address != contract.ReleaseExecutor ||
		len(manifest.Roles.ReleaseCommittee) != len(contract.CommitteeMembers) {
		return errors.New("role assignment manifest does not match deployment")
	}
	want := append([]string(nil), contract.CommitteeMembers...)
	got := make([]string, 0, len(manifest.Roles.ReleaseCommittee))
	for _, role := range manifest.Roles.ReleaseCommittee {
		got = append(got, role.Address)
	}
	sort.Strings(want)
	sort.Strings(got)
	for index := range want {
		if want[index] != got[index] {
			return errors.New("release committee role mapping does not match deployment")
		}
	}
	return nil
}

func matchObserverConfig(config bridgeobserver.TronConfig, deployment tronDeployment) error {
	if config.SourceChainID != deployment.Network.ChainID || config.GenesisBlockID != deployment.Network.GenesisBlockID ||
		config.BridgeContract != deployment.Contract.Address || config.AssetDenom != deployment.Route.AssetDenom ||
		config.OriginAsset != deployment.Route.TokenAddress || config.AssetDecimals != deployment.Route.Decimals ||
		config.Confirmations != deployment.Network.MinConfirmations || config.APIQuorum < 2 || len(config.APIURLs) < 3 ||
		config.AllowInsecureHTTP {
		return errors.New("observer config does not match the audited deployment or production quorum policy")
	}
	if _, err := productionProviderHosts(config.APIURLs); err != nil {
		return fmt.Errorf("observer providers: %w", err)
	}
	return nil
}

func validateObserverArtifact(
	artifact bridgeobserver.Artifact,
	manifest Manifest,
	deployment tronDeployment,
	config bridgeobserver.TronConfig,
	now time.Time,
) error {
	if err := bridgeobserver.ValidateArtifact(artifact); err != nil {
		return err
	}
	checkpoint := artifact.Checkpoint
	if artifact.ChainType != "tron" || checkpoint.SourceChainID != bridgeobserver.TronMainnetChainID ||
		checkpoint.ObservedHeight < checkpoint.Height || checkpoint.ObservedHeight-checkpoint.Height+1 < bridgeobserver.TronMainnetConfirmations {
		return errors.New("artifact does not prove the canonical TRON mainnet finality boundary")
	}
	transactionRoot := normalizeHash64(checkpoint.TransactionRoot)
	if transactionRoot == "" || normalizeHash64(checkpoint.ReceiptRoot) != transactionRoot || normalizeHash64(checkpoint.StateRoot) == "" {
		return errors.New("artifact checkpoint does not bind complete TRON transaction and state roots")
	}
	maxAge := time.Duration(manifest.MaxObserverAgeSeconds) * time.Second
	for label, timestamp := range map[string]int64{
		"observation": artifact.ObservedAtUnix,
		"checkpoint":  checkpoint.IssuedAtUnix,
	} {
		observed := time.Unix(timestamp, 0)
		if timestamp <= 0 || observed.After(now.Add(5*time.Minute)) || observed.Before(now.Add(-maxAge)) {
			return fmt.Errorf("%s timestamp is stale or from the future", label)
		}
	}
	if artifact.Evidence.Queried < 3 || artifact.Evidence.Required < 2 || artifact.Evidence.Agreed < artifact.Evidence.Required {
		return errors.New("artifact does not carry production API quorum evidence")
	}
	if !endpointHashesMatchConfig(artifact.Evidence.EndpointHashes, config.APIURLs) {
		return errors.New("artifact API evidence does not match the reviewed observer providers")
	}
	allowed := make(map[string]string, len(manifest.SourceObservers.Members))
	for _, member := range manifest.SourceObservers.Members {
		allowed[normalizeEd25519PublicKey(member.PublicKey)] = bridgeobserver.NormalizeRegistryID(member.SignerID)
	}
	if err := validateObserverSignatures(checkpoint.ValidatorSignatures, allowed, int(manifest.SourceObservers.Threshold)); err != nil {
		return fmt.Errorf("checkpoint committee: %w", err)
	}
	for index, proof := range artifact.Proofs {
		if proof.EventContract != deployment.Contract.Address || proof.OriginAsset != deployment.Route.TokenAddress ||
			proof.AssetDenom != deployment.Route.AssetDenom || proof.SourceChainID != deployment.Network.ChainID {
			return fmt.Errorf("proof %d does not match audited route identity", index)
		}
		if err := validateObserverSignatures(proof.OracleSignatures, allowed, int(manifest.SourceObservers.Threshold)); err != nil {
			return fmt.Errorf("proof %d observer committee: %w", index, err)
		}
	}
	if len(artifact.Proofs) == 0 && checkpoint.EventRoot != bridgeobserver.EmptyEventRoot() {
		return errors.New("pre-activation observer evidence must use the canonical empty event root")
	}
	return nil
}

func validateObserverSignatures(signatures []bridgeobserver.Signature, allowed map[string]string, threshold int) error {
	if len(signatures) < threshold {
		return fmt.Errorf("signature threshold not met: got %d need %d", len(signatures), threshold)
	}
	seen := make(map[string]struct{}, len(signatures))
	for _, signature := range signatures {
		publicKey := normalizeEd25519PublicKey(signature.PublicKey)
		expectedSigner, exists := allowed[publicKey]
		if !exists || expectedSigner != bridgeobserver.NormalizeRegistryID(signature.Signer) {
			return errors.New("signature uses an unapproved observer identity")
		}
		if _, exists := seen[publicKey]; exists {
			return errors.New("observer signature key is duplicated")
		}
		seen[publicKey] = struct{}{}
	}
	return nil
}

func validateReconcilerConfig(config bridgereconcile.Config, manifest Manifest, deployment tronDeployment) error {
	if config.Version != bridgereconcile.ConfigVersion || len(config.Routes) != 1 || !config.AutoPause ||
		config.FailureThreshold < 2 || config.FailureThreshold > 5 ||
		!adminTokenEnvPattern.MatchString(config.AdminTokenEnv) || config.AllowInsecureMSCHTTP {
		return errors.New("reconciler production schema, auto-pause, threshold, or token environment is invalid")
	}
	accountingURL, err := url.Parse(config.MSCAccountingURL)
	if err != nil || accountingURL.Scheme != "https" || accountingURL.Host == "" {
		return errors.New("reconciler MSC accounting endpoint must use production HTTPS")
	}
	adminURL, err := url.Parse(config.AdminSettingsURL)
	if err != nil || adminURL.Scheme != accountingURL.Scheme || adminURL.Host != accountingURL.Host ||
		adminURL.Path != "/bridge/admin/settings" || adminURL.RawQuery != "" || adminURL.Fragment != "" {
		return errors.New("reconciler admin pause endpoint must use the exact MSC accounting origin")
	}
	if duration, err := time.ParseDuration(config.MaxAccountingAge); err != nil || duration <= 0 || duration > 2*time.Minute {
		return errors.New("reconciler max accounting age must be at most two minutes")
	}
	if duration, err := time.ParseDuration(config.PollInterval); err != nil || duration <= 0 || duration > 30*time.Second {
		return errors.New("reconciler poll interval must be at most 30 seconds")
	}
	route := config.Routes[0]
	if route.ChainType != "tron" || route.RouteID != manifest.RouteID || route.SourceChainID != deployment.Network.ChainID ||
		route.ExpectedGenesisID != deployment.Network.GenesisBlockID || route.ExpectedVaultCodeHash != deployment.Contract.RuntimeCodeHash ||
		route.ExpectedTokenCodeHash != deployment.Route.TokenRuntimeCodeHash || route.VaultAddress != deployment.Contract.Address ||
		route.TokenAddress != deployment.Route.TokenAddress || route.SourceDecimals != deployment.Route.Decimals ||
		!route.ExpectedRouteEnabled || route.ExpectedMinAmountRaw != deployment.Route.MinAmountRaw ||
		route.ExpectedMaxAmountRaw != deployment.Route.MaxAmountRaw || route.ExpectedDailyLockRaw != deployment.Route.DailyLockLimitRaw ||
		route.Confirmations < bridgeobserver.TronMainnetConfirmations || route.RPCQuorum < 2 || len(route.RPCURLs) < 3 {
		return errors.New("reconciler route does not match the audited deployment")
	}
	if _, err := productionProviderHosts(route.RPCURLs); err != nil {
		return fmt.Errorf("reconciler providers: %w", err)
	}
	return nil
}

func productionProviderHosts(values []string) (map[string]struct{}, error) {
	hosts := make(map[string]struct{}, len(values))
	for _, value := range values {
		parsed, err := url.Parse(strings.TrimSpace(value))
		host := strings.ToLower(parsed.Hostname())
		if err != nil || parsed.Scheme != "https" || host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
			strings.Contains(strings.ToLower(value), "replace") || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
			strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".example") || strings.HasSuffix(host, ".invalid") || strings.HasSuffix(host, ".test") {
			return nil, errors.New("provider must be a non-placeholder production HTTPS host")
		}
		if _, exists := hosts[host]; exists {
			return nil, fmt.Errorf("provider host %s is duplicated", host)
		}
		hosts[host] = struct{}{}
	}
	return hosts, nil
}

func validateProviderSeparation(observerURLs, reconcilerURLs []string) error {
	observers, err := productionProviderHosts(observerURLs)
	if err != nil {
		return err
	}
	reconcilers, err := productionProviderHosts(reconcilerURLs)
	if err != nil {
		return err
	}
	for host := range observers {
		if _, exists := reconcilers[host]; exists {
			return fmt.Errorf("observer and reconciler reuse provider host %s", host)
		}
	}
	return nil
}

func validateReconciliation(
	report bridgereconcile.Report,
	dtl DTLAuthoritySnapshot,
	config bridgereconcile.Config,
	manifest Manifest,
	deployment tronDeployment,
	now time.Time,
) (bridgereconcile.AssetReport, bridgereconcile.RouteReport, error) {
	if report.Version != bridgereconcile.ReportVersion || !report.Healthy || report.CriticalDeficit || !report.BridgePaused ||
		report.MSCHeight == 0 || normalizeHash64(report.MSCStateRoot) == "" || len(report.Assets) != 1 {
		return bridgereconcile.AssetReport{}, bridgereconcile.RouteReport{}, errors.New("reconciliation must be healthy, deficit-free, globally paused, and state-root bound")
	}
	checked := time.Unix(report.CheckedAtUnix, 0)
	maxAge := time.Duration(manifest.MaxReconciliationAgeSeconds) * time.Second
	if report.CheckedAtUnix <= 0 || checked.After(now.Add(30*time.Second)) || checked.Before(now.Add(-maxAge)) ||
		report.MSCAccountingAgeSeconds < 0 || uint64(report.MSCAccountingAgeSeconds) > manifest.MaxReconciliationAgeSeconds {
		return bridgereconcile.AssetReport{}, bridgereconcile.RouteReport{}, errors.New("reconciliation report is stale or from the future")
	}
	asset := report.Assets[0]
	if asset.LocalTokenID != config.Routes[0].LocalTokenID || asset.LocalTokenID != dtl.TokenID ||
		asset.Decimals != deployment.Route.Decimals || asset.Status != "healthy" || asset.DeficitRaw != "0" || len(asset.Routes) != 1 ||
		!asset.TokenPaused || asset.MaxSupplyRaw != dtl.MaxSupplyRaw || asset.AuthorityThreshold != dtl.AuthorityThreshold {
		return bridgereconcile.AssetReport{}, bridgereconcile.RouteReport{}, errors.New("reconciled wrapped asset identity or health is invalid")
	}
	wrapped, wrappedOK := nonnegativeRaw(asset.WrappedSupplyRaw)
	backing, backingOK := nonnegativeRaw(asset.TrackedBackingRaw)
	if !wrappedOK || !backingOK || backing.Cmp(wrapped) < 0 {
		return bridgereconcile.AssetReport{}, bridgereconcile.RouteReport{}, errors.New("reconciled source backing is below wrapped supply")
	}
	route := asset.Routes[0]
	tracked, trackedOK := nonnegativeRaw(route.TrackedEscrowRaw)
	balance, balanceOK := nonnegativeRaw(route.VaultTokenBalanceRaw)
	if route.RouteID != manifest.RouteID || route.SourceChainID != deployment.Network.ChainID || route.Status != "healthy" ||
		route.BlockHeight <= deployment.Contract.DeploymentBlock || normalizeHash64(route.BlockHash) == "" ||
		route.RPCQueried < 3 || route.RPCAgreed < 2 || len(route.EndpointHashes) != route.RPCAgreed ||
		route.VaultBalanceDeficitRaw != "0" || !route.VaultPaused || !trackedOK || !balanceOK || balance.Cmp(tracked) < 0 {
		return bridgereconcile.AssetReport{}, bridgereconcile.RouteReport{}, errors.New("reconciled TRON vault state or API quorum is invalid")
	}
	if !route.TokenRouteEnabled || route.TokenRouteMinAmountRaw != deployment.Route.MinAmountRaw ||
		route.TokenRouteMaxAmountRaw != deployment.Route.MaxAmountRaw ||
		route.TokenRouteDailyLockRaw != deployment.Route.DailyLockLimitRaw ||
		route.TokenRouteDailyUnlockRaw != deployment.Route.DailyUnlockLimitRaw {
		return bridgereconcile.AssetReport{}, bridgereconcile.RouteReport{}, errors.New("reconciled TRON token route is disabled or does not match audited limits")
	}
	if !validEndpointHashes(route.EndpointHashes) {
		return bridgereconcile.AssetReport{}, bridgereconcile.RouteReport{}, errors.New("reconciliation endpoint hash evidence is invalid")
	}
	if !endpointHashesMatchConfig(route.EndpointHashes, config.Routes[0].RPCURLs) {
		return bridgereconcile.AssetReport{}, bridgereconcile.RouteReport{}, errors.New("reconciliation API evidence does not match the reviewed providers")
	}
	return asset, route, nil
}

func validateDTLSnapshot(snapshot DTLAuthoritySnapshot, manifest Manifest, asset bridgereconcile.AssetReport, report bridgereconcile.Report, now time.Time) error {
	if snapshot.Version != DTLSnapshotVersion || snapshot.CapturedAtUnix <= 0 ||
		time.Unix(snapshot.CapturedAtUnix, 0).After(now.Add(30*time.Second)) ||
		time.Unix(snapshot.CapturedAtUnix, 0).Before(now.Add(-time.Duration(manifest.MaxReconciliationAgeSeconds)*time.Second)) ||
		snapshot.MSCHeight != report.MSCHeight || normalizeHash64(snapshot.MSCStateRoot) != normalizeHash64(report.MSCStateRoot) ||
		snapshot.TokenID != asset.LocalTokenID || snapshot.LocalDenom != "mscUSDT" || snapshot.Symbol != asset.Symbol ||
		snapshot.Decimals != 6 || !snapshot.Paused || snapshot.TotalSupplyRaw != asset.WrappedSupplyRaw {
		return errors.New("DTL authority snapshot is stale or not bound to reconciled MSC state")
	}
	total, totalOK := nonnegativeRaw(snapshot.TotalSupplyRaw)
	maximum, maximumOK := nonnegativeRaw(snapshot.MaxSupplyRaw)
	if !totalOK || !maximumOK || maximum.Sign() <= 0 || total.Cmp(maximum) > 0 ||
		snapshot.AuthorityThreshold != manifest.DTLAuthorities.Threshold ||
		len(snapshot.AuthoritySigners) != len(manifest.DTLAuthorities.Members) {
		return errors.New("DTL wrapped-token supply or authority threshold is invalid")
	}
	want := make([]string, 0, len(manifest.DTLAuthorities.Members))
	for _, member := range manifest.DTLAuthorities.Members {
		want = append(want, normalizeMSCAddress(member.Address))
	}
	got := make([]string, 0, len(snapshot.AuthoritySigners))
	for _, signer := range snapshot.AuthoritySigners {
		signer = normalizeMSCAddress(signer)
		if signer == "" {
			return errors.New("DTL authority snapshot contains an invalid signer")
		}
		got = append(got, signer)
	}
	sort.Strings(want)
	sort.Strings(got)
	for index := range want {
		if want[index] != got[index] {
			return errors.New("DTL authority snapshot does not match manifest committee")
		}
	}
	if err := validateDTLSnapshotAttestations(snapshot, manifest); err != nil {
		return err
	}
	return nil
}

func DTLSnapshotSigningBytes(snapshot DTLAuthoritySnapshot) ([]byte, error) {
	signers := make([]string, 0, len(snapshot.AuthoritySigners))
	for _, signer := range snapshot.AuthoritySigners {
		normalized := normalizeMSCAddress(signer)
		if normalized == "" {
			return nil, errors.New("DTL snapshot signer is invalid")
		}
		signers = append(signers, normalized)
	}
	sort.Strings(signers)
	payload := struct {
		Version            string   `json:"version"`
		CapturedAtUnix     int64    `json:"captured_at_unix"`
		MSCHeight          uint64   `json:"msc_height"`
		MSCStateRoot       string   `json:"msc_state_root"`
		TokenID            string   `json:"token_id"`
		LocalDenom         string   `json:"local_denom"`
		Symbol             string   `json:"symbol"`
		Decimals           uint8    `json:"decimals"`
		TotalSupplyRaw     string   `json:"total_supply_raw"`
		MaxSupplyRaw       string   `json:"max_supply_raw"`
		Paused             bool     `json:"paused"`
		AuthoritySigners   []string `json:"authority_signers"`
		AuthorityThreshold uint16   `json:"authority_threshold"`
	}{
		Version: snapshot.Version, CapturedAtUnix: snapshot.CapturedAtUnix, MSCHeight: snapshot.MSCHeight,
		MSCStateRoot: normalizeHash64(snapshot.MSCStateRoot), TokenID: strings.TrimSpace(snapshot.TokenID),
		LocalDenom: strings.TrimSpace(snapshot.LocalDenom), Symbol: strings.TrimSpace(snapshot.Symbol), Decimals: snapshot.Decimals,
		TotalSupplyRaw: strings.TrimSpace(snapshot.TotalSupplyRaw), MaxSupplyRaw: strings.TrimSpace(snapshot.MaxSupplyRaw),
		Paused: snapshot.Paused, AuthoritySigners: signers, AuthorityThreshold: snapshot.AuthorityThreshold,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return append([]byte("MSC|BRIDGE_DTL_SNAPSHOT|v1|"), raw...), nil
}

func validateDTLSnapshotAttestations(snapshot DTLAuthoritySnapshot, manifest Manifest) error {
	if len(snapshot.Attestations) < int(manifest.DTLAuthorities.Threshold) ||
		len(snapshot.Attestations) > len(manifest.DTLAuthorities.Members) {
		return errors.New("DTL snapshot attestation threshold is not met")
	}
	message, err := DTLSnapshotSigningBytes(snapshot)
	if err != nil {
		return err
	}
	allowed := make(map[string]string, len(manifest.DTLAuthorities.Members))
	for _, member := range manifest.DTLAuthorities.Members {
		allowed[normalizeEd25519PublicKey(member.PublicKey)] = normalizeMSCAddress(member.Address)
	}
	seen := make(map[string]struct{}, len(snapshot.Attestations))
	for _, attestation := range snapshot.Attestations {
		publicKey := normalizeEd25519PublicKey(attestation.PublicKey)
		address := normalizeMSCAddress(attestation.Address)
		if publicKey == "" || address == "" || allowed[publicKey] != address || mscAddressFromEd25519PublicKey(publicKey) != address {
			return errors.New("DTL snapshot attestation uses an unapproved authority identity")
		}
		if _, exists := seen[publicKey]; exists {
			return errors.New("DTL snapshot attestation key is duplicated")
		}
		signature, decodeErr := hex.DecodeString(strings.TrimSpace(attestation.Signature))
		key, keyErr := hex.DecodeString(publicKey)
		if decodeErr != nil || keyErr != nil || len(signature) != ed25519.SignatureSize ||
			!ed25519.Verify(ed25519.PublicKey(key), message, signature) {
			return errors.New("DTL snapshot attestation signature is invalid")
		}
		seen[publicKey] = struct{}{}
	}
	return nil
}

func validatePauseDrill(drill PauseDrill, routeID string, now time.Time) error {
	completed := time.Unix(drill.CompletedAtUnix, 0)
	if drill.Version != PauseDrillVersion || drill.RouteID != routeID || drill.CompletedAtUnix <= 0 ||
		completed.After(now.Add(5*time.Minute)) || completed.Before(now.Add(-30*24*time.Hour)) || !drill.Passed ||
		!drill.BridgePauseObserved || !drill.MintRejectedWhilePaused || !drill.UnlockRejectedWhilePaused ||
		!drill.VerificationRemainedAvailable || !drill.HumanApprovalRequiredToResume {
		return errors.New("pause drill evidence is incomplete, stale, or failed")
	}
	if err := validateTypedEvidence(drill.Evidence, []string{
		"msc_pause", "tron_pause", "paused_rejections", "resume_approval",
	}); err != nil {
		return fmt.Errorf("pause drill evidence is incomplete: %w", err)
	}
	return nil
}

func validateSoakReport(report SoakReport, routeID string, committeeSize int, now time.Time) error {
	duration := report.CompletedAtUnix - report.StartedAtUnix
	minimumChecks := uint64(72 * 60)
	if report.Version != SoakReportVersion || report.RouteID != routeID || report.StartedAtUnix <= 0 || duration < 72*3600 ||
		report.CompletedAtUnix > now.Add(5*time.Minute).Unix() || report.CompletedAtUnix < now.Add(-30*24*time.Hour).Unix() ||
		!report.Passed || int(report.ObserverCount) < committeeSize || report.ReconciliationChecks < minimumChecks ||
		report.CompletedDeposits < 3 || report.CompletedWithdrawals < 3 || report.CriticalDeficits != 0 ||
		report.UnknownReconciliations != 0 || report.AccountingDriftEvents != 0 || report.DuplicateExecutionEvents != 0 ||
		!report.ReorgDrillPassed || !report.ReplayDrillPassed || !report.RotationDrillPassed ||
		!report.DailyLimitDrillPassed || !report.CrashRecoveryDrillPassed {
		return errors.New("72-hour soak or required adversarial drills are incomplete or failed")
	}
	if err := validateTypedEvidence(report.Evidence, []string{
		"reconciliation_log", "end_to_end_transfers", "reorg_drill", "replay_drill",
		"rotation_drill", "daily_limit_drill", "crash_recovery_drill",
	}); err != nil {
		return fmt.Errorf("72-hour soak evidence is incomplete: %w", err)
	}
	return nil
}

func validateTypedEvidence(evidence []TypedEvidenceRef, requiredKinds []string) error {
	if len(evidence) != len(requiredKinds) {
		return fmt.Errorf("requires exactly %d typed attachments", len(requiredKinds))
	}
	required := make(map[string]struct{}, len(requiredKinds))
	for _, kind := range requiredKinds {
		required[kind] = struct{}{}
	}
	seen := make(map[string]struct{}, len(evidence))
	for _, item := range evidence {
		kind := strings.TrimSpace(item.Kind)
		if _, exists := required[kind]; !exists {
			return fmt.Errorf("unexpected attachment kind %q", kind)
		}
		if _, exists := seen[kind]; exists {
			return fmt.Errorf("attachment kind %q is duplicated", kind)
		}
		if strings.TrimSpace(item.Path) == "" || normalizeSHA256(item.SHA256) == "" {
			return fmt.Errorf("attachment kind %q requires a path and non-zero sha256", kind)
		}
		seen[kind] = struct{}{}
	}
	for _, kind := range requiredKinds {
		if _, exists := seen[kind]; !exists {
			return fmt.Errorf("attachment kind %q is missing", kind)
		}
	}
	return nil
}

func nonnegativeRaw(value string) (*big.Int, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") || (len(value) > 1 && value[0] == '0') {
		return nil, false
	}
	parsed, ok := new(big.Int).SetString(value, 10)
	return parsed, ok && parsed.Sign() >= 0
}

func normalizeHash64(value string) string {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if len(value) != 64 || strings.Trim(value, "0") == "" {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func validEndpointHashes(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) != 16 {
			return false
		}
		if _, err := hex.DecodeString(value); err != nil {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return true
}

func endpointHashesMatchConfig(values, endpoints []string) bool {
	expected := make(map[string]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		expected[bridgeobserver.HashString(endpoint)[:16]] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if _, exists := expected[value]; !exists {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	return len(values) > 0
}
