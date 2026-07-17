package bridgegate

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"golang.org/x/crypto/sha3"

	"msc-chain/bridgeobserver"
	"msc-chain/bridgereconcile"
)

func decodeTronDeployment(raw []byte) (tronDeployment, error) {
	var envelope tronDeploymentEnvelope
	if err := strictJSON(raw, &envelope); err != nil {
		return tronDeployment{}, fmt.Errorf("deployment record: %w", err)
	}
	deployment := tronDeployment{Envelope: envelope}
	if err := strictJSON(envelope.Network, &deployment.Network); err != nil {
		return tronDeployment{}, fmt.Errorf("deployment network: %w", err)
	}
	if err := strictJSON(envelope.Contract, &deployment.Contract); err != nil {
		return tronDeployment{}, fmt.Errorf("deployment contract: %w", err)
	}
	if err := strictJSON(envelope.Route, &deployment.Route); err != nil {
		return tronDeployment{}, fmt.Errorf("deployment route: %w", err)
	}
	deployment.Actions = make([]tronGovernanceAction, len(envelope.GovernanceActions))
	for index, rawAction := range envelope.GovernanceActions {
		if err := strictJSON(rawAction, &deployment.Actions[index]); err != nil {
			return tronDeployment{}, fmt.Errorf("deployment governance action %d: %w", index, err)
		}
	}
	if err := strictJSON(envelope.ObserverConfig, &deployment.ObserverConfig); err != nil {
		return tronDeployment{}, fmt.Errorf("deployment observer config: %w", err)
	}
	if err := strictJSON(envelope.ReconcilerConfig, &deployment.ReconcilerConfig); err != nil {
		return tronDeployment{}, fmt.Errorf("deployment reconciler config: %w", err)
	}
	return deployment, nil
}

func normalizeRuntimeHash(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 66 || !strings.HasPrefix(value, "0x") || strings.Trim(value[2:], "0") == "" {
		return ""
	}
	if _, err := hex.DecodeString(value[2:]); err != nil {
		return ""
	}
	return value
}

func normalizeTronTransactionHash(value string) string {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if len(value) != 64 || strings.Trim(value, "0") == "" {
		return ""
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ""
	}
	return value
}

func positiveRaw(value string) (*big.Int, bool) {
	value = strings.TrimSpace(value)
	if value == "" || (len(value) > 1 && value[0] == '0') || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") {
		return nil, false
	}
	parsed, ok := new(big.Int).SetString(value, 10)
	return parsed, ok && parsed.Sign() > 0
}

func keccakString(value string) string {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write([]byte(value))
	return "0x" + hex.EncodeToString(hasher.Sum(nil))
}

func validateTronDeployment(deployment tronDeployment, now time.Time) error {
	if deployment.Envelope.Version != TronDeploymentVersion || deployment.Envelope.Testnet {
		return errors.New("deployment must be the production TRON v2 record")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(deployment.Envelope.CreatedAt))
	if err != nil || createdAt.IsZero() || createdAt.After(now.Add(5*time.Minute)) {
		return errors.New("deployment created_at is invalid or from the future")
	}
	if len(deployment.Envelope.GovernanceActions) != 2 {
		return errors.New("deployment must contain the two ordered paused activation actions")
	}

	network := deployment.Network
	if network.Label != "tron-mainnet" || network.ChainID != bridgeobserver.TronMainnetChainID ||
		strings.ToLower(network.TIP712ChainID) != "0x2b6653dc" ||
		strings.ToLower(network.GenesisBlockID) != bridgeobserver.TronMainnetGenesisBlockID ||
		network.ChainName != "TRON Mainnet" || network.NativeSymbol != "TRX" ||
		strings.TrimRight(network.ExplorerURL, "/") != "https://tronscan.org" ||
		network.MinConfirmations < bridgeobserver.TronMainnetConfirmations {
		return errors.New("deployment is not pinned to canonical TRON mainnet identity")
	}
	if _, ok := positiveRaw(network.NetworkMaxFeeLimitSun); !ok {
		return errors.New("deployment network maximum fee is invalid")
	}
	if _, ok := positiveRaw(network.ReleaseFeeLimitSun); !ok {
		return errors.New("deployment release fee is invalid")
	}

	contract := deployment.Contract
	if bridgeobserver.NormalizeTronAddress(contract.Address) == "" ||
		bridgeobserver.NormalizeTronAddress(contract.Governance) == "" ||
		bridgeobserver.NormalizeTronAddress(contract.Guardian) == "" ||
		bridgeobserver.NormalizeTronAddress(contract.ReleaseExecutor) == "" ||
		normalizeTronTransactionHash(contract.DeploymentTxHash) == "" || contract.DeploymentBlock == 0 ||
		normalizeRuntimeHash(contract.RuntimeCodeHash) == "" || !strings.HasPrefix(contract.Compiler, "0.8.26+") ||
		strings.ToLower(contract.TVMTarget) != "cancun" || !contract.TIP712 || !contract.Paused ||
		contract.DefaultAdminDelaySeconds < 86400 || contract.MSCSourceChainID != "91938" ||
		strings.ToLower(contract.MSCSourceChainHash) != keccakString("91938") || contract.CommitteeEpoch == 0 {
		return errors.New("deployment contract identity, bytecode, role, delay, or paused state is invalid")
	}
	if len(contract.CommitteeMembers) < 5 || contract.CommitteeThreshold < 4 ||
		int(contract.CommitteeThreshold) > len(contract.CommitteeMembers) ||
		int(contract.CommitteeThreshold) < (2*len(contract.CommitteeMembers)+2)/3 {
		return errors.New("deployment release committee must contain at least five members with >=2/3 and >=4 threshold")
	}
	seenAddresses := map[string]struct{}{
		bridgeobserver.NormalizeTronAddress(contract.Governance):      {},
		bridgeobserver.NormalizeTronAddress(contract.Guardian):        {},
		bridgeobserver.NormalizeTronAddress(contract.ReleaseExecutor): {},
	}
	if len(seenAddresses) != 3 {
		return errors.New("deployment governance, guardian, and executor must be distinct")
	}
	for _, member := range contract.CommitteeMembers {
		if bridgeobserver.NormalizeTronAddress(member) == "" {
			return errors.New("deployment release committee contains an invalid TRON address")
		}
		normalizedMember := bridgeobserver.NormalizeTronAddress(member)
		if _, exists := seenAddresses[normalizedMember]; exists {
			return errors.New("deployment release committee overlaps another privileged role")
		}
		seenAddresses[normalizedMember] = struct{}{}
	}

	route := deployment.Route
	minAmount, minOK := positiveRaw(route.MinAmountRaw)
	maxAmount, maxOK := positiveRaw(route.MaxAmountRaw)
	dailyLock, lockOK := positiveRaw(route.DailyLockLimitRaw)
	dailyUnlock, unlockOK := positiveRaw(route.DailyUnlockLimitRaw)
	if route.RouteID != "usdt-tron-mainnet" || route.ExecutionAdapter != "tron_vault_v1" ||
		route.AssetDenom != "USDT-TRON" || route.Symbol != "USDT" ||
		route.TokenAddress != bridgeobserver.TronMainnetUSDTAddress || route.LocalDenom != "mscUSDT" ||
		route.Decimals != 6 || normalizeRuntimeHash(route.TokenRuntimeCodeHash) == "" ||
		route.TokenSymbolVerified != "USDT" || route.TokenDecimalsVerified != 6 ||
		!minOK || !maxOK || !lockOK || !unlockOK || minAmount.Cmp(maxAmount) > 0 ||
		dailyLock.Cmp(maxAmount) < 0 || dailyUnlock.Cmp(maxAmount) < 0 {
		return errors.New("deployment route identity, token evidence, or limits are invalid")
	}
	if _, err := publicationURL(route.AuditReference); err != nil {
		return fmt.Errorf("deployment audit reference: %w", err)
	}
	if bridgeobserver.NormalizeTronAddress(contract.Address) == bridgeobserver.NormalizeTronAddress(route.TokenAddress) {
		return errors.New("deployment vault and token addresses overlap")
	}
	if err := validateGovernanceActions(deployment); err != nil {
		return err
	}

	embeddedObserver := deployment.ObserverConfig
	if embeddedObserver.SourceChainID != network.ChainID || embeddedObserver.GenesisBlockID != network.GenesisBlockID ||
		embeddedObserver.BridgeContract != contract.Address || embeddedObserver.AssetDenom != route.AssetDenom ||
		embeddedObserver.OriginAsset != route.TokenAddress || embeddedObserver.AssetDecimals != route.Decimals ||
		embeddedObserver.Confirmations != network.MinConfirmations || embeddedObserver.APIQuorum < 2 || len(embeddedObserver.APIURLs) < 3 {
		return errors.New("embedded observer template does not match deployment")
	}
	embeddedReconciler := deployment.ReconcilerConfig
	if embeddedReconciler.Version != bridgereconcile.ConfigVersion || len(embeddedReconciler.Routes) != 1 {
		return errors.New("embedded reconciler template version or route count is invalid")
	}
	embeddedRoute := embeddedReconciler.Routes[0]
	if embeddedRoute.ChainType != "tron" || embeddedRoute.RouteID != route.RouteID ||
		embeddedRoute.SourceChainID != network.ChainID || embeddedRoute.ExpectedGenesisID != network.GenesisBlockID ||
		embeddedRoute.ExpectedVaultCodeHash != contract.RuntimeCodeHash ||
		embeddedRoute.ExpectedTokenCodeHash != route.TokenRuntimeCodeHash ||
		embeddedRoute.VaultAddress != contract.Address || embeddedRoute.TokenAddress != route.TokenAddress ||
		embeddedRoute.SourceDecimals != route.Decimals || !embeddedRoute.ExpectedRouteEnabled ||
		embeddedRoute.ExpectedMinAmountRaw != route.MinAmountRaw || embeddedRoute.ExpectedMaxAmountRaw != route.MaxAmountRaw ||
		embeddedRoute.ExpectedDailyLockRaw != route.DailyLockLimitRaw || embeddedRoute.ExpectedDailyUnlockRaw != route.DailyUnlockLimitRaw ||
		embeddedRoute.RPCQuorum < 2 || len(embeddedRoute.RPCURLs) < 3 {
		return errors.New("embedded reconciler template does not match deployment")
	}
	return nil
}

func validateGovernanceActions(deployment tronDeployment) error {
	if len(deployment.Actions) != 2 {
		return errors.New("deployment governance actions are incomplete")
	}
	expectedSetRoute, err := setTokenRouteCalldata(deployment.Route)
	if err != nil {
		return err
	}
	expected := []struct {
		order    uint8
		label    string
		selector string
		calldata string
	}{
		{order: 1, label: "Configure TRC20 route while vault is paused", selector: "setTokenRoute(address,(bool,uint256,uint256,uint256,uint256))", calldata: expectedSetRoute},
		{order: 2, label: "Unpause only after observer and MSC route registration pass", selector: "unpause()", calldata: methodSelectorHex("unpause()")},
	}
	maximumFee, _ := positiveRaw(deployment.Network.NetworkMaxFeeLimitSun)
	for index, action := range deployment.Actions {
		want := expected[index]
		body := action.Body
		if action.Order != want.order || action.Label != want.label || action.Endpoint != "/wallet/triggersmartcontract" || action.Method != "POST" ||
			body.OwnerAddress != deployment.Contract.Governance || body.ContractAddress != deployment.Contract.Address ||
			body.FunctionSelector != want.selector || body.CallValue != 0 || !body.Visible || body.FeeLimit == 0 ||
			action.Calldata != want.calldata || body.Parameter != want.calldata[8:] {
			return fmt.Errorf("deployment governance action %d does not match the exact paused activation sequence", index+1)
		}
		if new(big.Int).SetUint64(body.FeeLimit).Cmp(maximumFee) > 0 {
			return fmt.Errorf("deployment governance action %d exceeds network fee limit", index+1)
		}
		if index > 0 && body.FeeLimit != deployment.Actions[0].Body.FeeLimit {
			return errors.New("deployment governance actions must use one reviewed fee limit")
		}
	}
	return nil
}

func methodSelectorHex(signature string) string {
	hasher := sha3.NewLegacyKeccak256()
	_, _ = hasher.Write([]byte(signature))
	return hex.EncodeToString(hasher.Sum(nil)[:4])
}

func abiWord(value *big.Int) (string, error) {
	if value == nil || value.Sign() < 0 || value.BitLen() > 256 {
		return "", errors.New("value does not fit one ABI uint256 word")
	}
	return fmt.Sprintf("%064x", value), nil
}

func setTokenRouteCalldata(route tronDeploymentRoute) (string, error) {
	tokenHex, err := bridgeobserver.TronAddressToTVMHex(route.TokenAddress)
	if err != nil {
		return "", err
	}
	tokenWord := strings.Repeat("0", 24) + strings.TrimPrefix(tokenHex, "0x")
	values := []string{route.MinAmountRaw, route.MaxAmountRaw, route.DailyLockLimitRaw, route.DailyUnlockLimitRaw}
	words := []string{tokenWord, fmt.Sprintf("%064x", 1)}
	for _, value := range values {
		parsed, ok := positiveRaw(value)
		if !ok {
			return "", errors.New("route limit cannot be encoded for governance")
		}
		word, err := abiWord(parsed)
		if err != nil {
			return "", err
		}
		words = append(words, word)
	}
	return methodSelectorHex("setTokenRoute(address,(bool,uint256,uint256,uint256,uint256))") + strings.Join(words, ""), nil
}
