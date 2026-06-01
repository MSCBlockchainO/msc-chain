package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withBridgeTestConfig(t *testing.T, fn func()) {
	t.Helper()
	oldEnabled := BridgeEnabled
	oldMode := BridgeMode
	oldIBC := BridgeIBCStyleEnabled
	oldLightRequired := BridgeLightClientRequired
	oldConfirmations := BridgeRequiredConfirmations
	oldOracleQuorum := BridgeOracleQuorum
	oldChains := append([]BridgeChainConfig(nil), BridgeChains...)
	oldAssets := append([]BridgeAssetConfig(nil), BridgeAssets...)
	defer func() {
		BridgeEnabled = oldEnabled
		BridgeMode = oldMode
		BridgeIBCStyleEnabled = oldIBC
		BridgeLightClientRequired = oldLightRequired
		BridgeRequiredConfirmations = oldConfirmations
		BridgeOracleQuorum = oldOracleQuorum
		BridgeChains = oldChains
		BridgeAssets = oldAssets
	}()
	fn()
}

func testBridgeProof() BridgeProof {
	return BridgeProof{
		Version:         BridgeProtocolVersion,
		SourceChainID:   "external-1",
		EventID:         "deposit-42",
		AssetDenom:      "wEXT",
		OriginAsset:     "EXT",
		Recipient:       "MSC01abc",
		Amount:          "100",
		SourceHeight:    10,
		ConfirmedHeight: 16,
		OracleSignatures: []BridgeOracleSignature{
			{Signer: "oracle-a", Signature: "sig-a"},
			{Signer: "oracle-b", Signature: "sig-b"},
			{Signer: "oracle-c", Signature: "sig-c"},
		},
	}
}

func TestBridgeProofRejectedWhenDisabled(t *testing.T) {
	withBridgeTestConfig(t, func() {
		BridgeEnabled = false
		BridgeMode = BridgeModeDisabled

		result := VerifyBridgeProof(testBridgeProof())
		if result.Accepted {
			t.Fatalf("expected disabled bridge proof to be rejected")
		}
		if result.Reason != "bridge_disabled" {
			t.Fatalf("unexpected reject reason: %q", result.Reason)
		}
	})
}

func TestBridgeOracleQuorumProofAcceptedWhenConfigured(t *testing.T) {
	withBridgeTestConfig(t, func() {
		BridgeEnabled = true
		BridgeMode = BridgeModeVerificationOnly
		BridgeLightClientRequired = false
		BridgeOracleQuorum = 3
		BridgeRequiredConfirmations = 6
		BridgeChains = []BridgeChainConfig{{
			ChainID:          "external-1",
			Name:             "External Test Chain",
			ChainType:        "msc-compatible",
			TrustModel:       BridgeTrustOracleQuorum,
			MinConfirmations: 6,
		}}
		BridgeAssets = []BridgeAssetConfig{{
			Denom:        "wEXT",
			OriginChain:  "external-1",
			OriginAsset:  "EXT",
			LocalDenom:   "wEXT",
			Decimals:     18,
			EscrowPolicy: "locked_escrow_or_verified_burn",
		}}

		result := VerifyBridgeProof(testBridgeProof())
		if !result.Accepted {
			t.Fatalf("expected proof accepted, got reason=%q result=%+v", result.Reason, result)
		}
		if result.Verification != "oracle_quorum_syntax" {
			t.Fatalf("unexpected verification mode: %q", result.Verification)
		}
		if result.OracleSigners != 3 {
			t.Fatalf("unexpected oracle signer count: %d", result.OracleSigners)
		}
	})
}

func TestBridgeRequiresLightClientProofByDefault(t *testing.T) {
	withBridgeTestConfig(t, func() {
		BridgeEnabled = true
		BridgeMode = BridgeModeVerificationOnly
		BridgeLightClientRequired = true
		BridgeRequiredConfirmations = 6
		BridgeChains = []BridgeChainConfig{{
			ChainID:          "external-1",
			Name:             "External Test Chain",
			ChainType:        "msc-compatible",
			TrustModel:       BridgeTrustLightClient,
			MinConfirmations: 6,
		}}
		BridgeAssets = []BridgeAssetConfig{{
			Denom:        "wEXT",
			OriginChain:  "external-1",
			OriginAsset:  "EXT",
			LocalDenom:   "wEXT",
			Decimals:     18,
			EscrowPolicy: "locked_escrow_or_verified_burn",
		}}

		result := VerifyBridgeProof(testBridgeProof())
		if result.Accepted {
			t.Fatalf("expected proof without SPV data to be rejected")
		}
		if result.Reason != "light_client_proof_required" {
			t.Fatalf("unexpected reject reason: %q", result.Reason)
		}
	})
}

func TestBridgeConfigParsing(t *testing.T) {
	withBridgeTestConfig(t, func() {
		enabled := true
		lightRequired := true
		confirmations := uint64(32)
		quorum := uint16(4)
		changed := applyBridgeConfig(BridgeConfig{
			Enabled:               &enabled,
			Mode:                  BridgeModeVerificationOnly,
			LightClientRequired:   &lightRequired,
			RequiredConfirmations: &confirmations,
			OracleQuorum:          &quorum,
			Chains:                StringList{"external-1|External|ibc|hybrid|32|https://light.example"},
			Assets:                StringList{"wEXT|external-1|EXT|wEXT|18|escrow"},
		})
		if !changed {
			t.Fatalf("expected config change")
		}
		if !BridgeEnabled || BridgeMode != BridgeModeVerificationOnly || !BridgeLightClientRequired {
			t.Fatalf("bridge globals not applied")
		}
		if len(BridgeChains) != 1 || BridgeChains[0].TrustModel != BridgeTrustHybrid {
			t.Fatalf("unexpected parsed chains: %+v", BridgeChains)
		}
		if len(BridgeAssets) != 1 || BridgeAssets[0].Denom != "wEXT" {
			t.Fatalf("unexpected parsed assets: %+v", BridgeAssets)
		}
	})
}

func TestBridgeStatusAndVerifyHandlers(t *testing.T) {
	withBridgeTestConfig(t, func() {
		oldRequireWallet := ConfigAuthRequireWallet
		oldAPIToken := apiToken
		oldRequireRead := ConfigRPCRequireAuthForReadEndpoints
		defer func() {
			ConfigAuthRequireWallet = oldRequireWallet
			apiToken = oldAPIToken
			ConfigRPCRequireAuthForReadEndpoints = oldRequireRead
		}()
		ConfigAuthRequireWallet = true
		ConfigRPCRequireAuthForReadEndpoints = false
		apiToken = ""

		BridgeEnabled = true
		BridgeMode = BridgeModeVerificationOnly
		BridgeLightClientRequired = false
		BridgeRequiredConfirmations = 6
		BridgeChains = []BridgeChainConfig{{
			ChainID:          "external-1",
			Name:             "External Test Chain",
			ChainType:        "msc-compatible",
			TrustModel:       BridgeTrustOracleQuorum,
			MinConfirmations: 6,
		}}
		BridgeAssets = []BridgeAssetConfig{{
			Denom:        "wEXT",
			OriginChain:  "external-1",
			OriginAsset:  "EXT",
			LocalDenom:   "wEXT",
			Decimals:     18,
			EscrowPolicy: "locked_escrow_or_verified_burn",
		}}

		s := NewServer(&Node{
			ID:         "TEST",
			Role:       "full",
			Blockchain: &Blockchain{Blocks: []Block{}},
			Ledger: Ledger{
				Balances:               map[string]int{},
				Nonces:                 map[string]int{},
				Stakes:                 map[string]StakeLock{},
				ValidatorRewardWallets: map[string]string{},
			},
		})
		statusReq := httptest.NewRequest(http.MethodGet, "/bridge/status", nil)
		statusW := httptest.NewRecorder()
		s.handleBridgeStatus(statusW, statusReq)
		if statusW.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", statusW.Code, statusW.Body.String())
		}
		var status BridgeStatus
		if err := json.Unmarshal(statusW.Body.Bytes(), &status); err != nil {
			t.Fatalf("decode bridge status: %v", err)
		}
		if !status.Enabled || len(status.RegisteredChains) != 1 || len(status.RegisteredAssets) != 1 {
			t.Fatalf("unexpected status: %+v", status)
		}

		body, err := json.Marshal(testBridgeProof())
		if err != nil {
			t.Fatalf("marshal proof: %v", err)
		}
		verifyReq := httptest.NewRequest(http.MethodPost, "/bridge/verify", bytes.NewReader(body))
		verifyW := httptest.NewRecorder()
		s.handleBridgeVerify(verifyW, verifyReq)
		if verifyW.Code != http.StatusOK {
			t.Fatalf("expected verify 200, got %d body=%s", verifyW.Code, verifyW.Body.String())
		}
		var result BridgeVerificationResult
		if err := json.Unmarshal(verifyW.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode bridge verify: %v", err)
		}
		if !result.Accepted {
			t.Fatalf("expected accepted verification, got %+v", result)
		}
	})
}
