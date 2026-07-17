package bridgegate

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	"msc-chain/bridgeobserver"
)

const releaseApprovalDomain = "MSC|BRIDGE_RELEASE_APPROVAL|v1|"

// ReleaseApprovalSigningBytes returns the exact domain-separated bytes that
// the TRON release committee approves for a production manifest.
func ReleaseApprovalSigningBytes(manifestSHA256 string) ([]byte, error) {
	manifestSHA256 = normalizeSHA256(manifestSHA256)
	if manifestSHA256 == "" {
		return nil, errors.New("release approval requires a non-zero manifest sha256")
	}
	return []byte(releaseApprovalDomain + manifestSHA256), nil
}

// ReleaseApprovalDigest returns the legacy Keccak-256 digest signed by TRON
// secp256k1 keys. Signatures are recoverable r || s || v values.
func ReleaseApprovalDigest(manifestSHA256 string) ([]byte, error) {
	payload, err := ReleaseApprovalSigningBytes(manifestSHA256)
	if err != nil {
		return nil, err
	}
	return ethcrypto.Keccak256(payload), nil
}

// ReleaseApprovalPayloadFile hashes the exact manifest bytes and returns the
// signing payload and digest without rewriting or canonicalizing the file.
func ReleaseApprovalPayloadFile(manifestPath string) (string, []byte, []byte, error) {
	manifestPath = strings.TrimSpace(manifestPath)
	if manifestPath == "" {
		return "", nil, nil, errors.New("manifest path is required")
	}
	absolute, err := filepath.Abs(filepath.Clean(manifestPath))
	if err != nil {
		return "", nil, nil, err
	}
	raw, err := fileBytes(absolute, maxManifestBytes)
	if err != nil {
		return "", nil, nil, err
	}
	var manifest Manifest
	if err := strictJSON(raw, &manifest); err != nil {
		return "", nil, nil, fmt.Errorf("decode manifest: %w", err)
	}
	if manifest.Version != ManifestVersion || manifest.RouteID != "usdt-tron-mainnet" || strings.TrimSpace(manifest.ReleaseApprovalPath) == "" {
		return "", nil, nil, errors.New("manifest is not a TRON production release manifest with an approval path")
	}
	manifestHash := sha256Hex(raw)
	payload, err := ReleaseApprovalSigningBytes(manifestHash)
	if err != nil {
		return "", nil, nil, err
	}
	digest, err := ReleaseApprovalDigest(manifestHash)
	if err != nil {
		return "", nil, nil, err
	}
	return manifestHash, payload, digest, nil
}

func validateReleaseApproval(approval TronReleaseApproval, manifestSHA256 string, manifest Manifest, deployment tronDeployment) error {
	manifestSHA256 = normalizeSHA256(manifestSHA256)
	if approval.Version != ReleaseApprovalVersion || normalizeSHA256(approval.ManifestSHA256) != manifestSHA256 {
		return errors.New("release approval version or manifest sha256 is invalid")
	}
	threshold := int(deployment.Contract.CommitteeThreshold)
	if threshold < 4 || len(approval.Signatures) < threshold || len(approval.Signatures) > len(deployment.Contract.CommitteeMembers) {
		return errors.New("release approval does not meet the deployed committee threshold")
	}
	digest, err := ReleaseApprovalDigest(manifestSHA256)
	if err != nil {
		return err
	}
	allowed := make(map[string]struct{}, len(deployment.Contract.CommitteeMembers))
	for _, member := range deployment.Contract.CommitteeMembers {
		address := bridgeobserver.NormalizeTronAddress(member)
		if address == "" {
			return errors.New("deployed release committee contains an invalid address")
		}
		allowed[address] = struct{}{}
	}
	for _, role := range manifest.Roles.ReleaseCommittee {
		address := bridgeobserver.NormalizeTronAddress(role.Address)
		if _, exists := allowed[address]; !exists {
			return errors.New("manifest release committee does not match the deployed committee")
		}
	}

	seen := make(map[string]struct{}, len(approval.Signatures))
	for _, item := range approval.Signatures {
		claimed := bridgeobserver.NormalizeTronAddress(item.Address)
		if claimed == "" {
			return errors.New("release approval claims an invalid TRON address")
		}
		if _, exists := allowed[claimed]; !exists {
			return errors.New("release approval signer is not in the deployed committee")
		}
		if _, exists := seen[claimed]; exists {
			return errors.New("release approval signer is duplicated")
		}
		signature, err := decodeRecoverableSignature(item.Signature)
		if err != nil {
			return fmt.Errorf("release approval signature for %s: %w", claimed, err)
		}
		publicKey, err := ethcrypto.SigToPub(digest, signature)
		if err != nil {
			return fmt.Errorf("release approval signature for %s is not recoverable", claimed)
		}
		recovered, err := bridgeobserver.TronAddressFromTVMHex(hex.EncodeToString(ethcrypto.PubkeyToAddress(*publicKey).Bytes()))
		if err != nil || recovered != claimed {
			return fmt.Errorf("release approval signature does not recover claimed signer %s", claimed)
		}
		seen[claimed] = struct{}{}
	}
	if len(seen) < threshold {
		return errors.New("release approval does not meet the deployed committee threshold")
	}
	return nil
}

func decodeRecoverableSignature(value string) ([]byte, error) {
	value = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), "0x")
	if len(value) != 130 {
		return nil, errors.New("signature must be exactly 65-byte r||s||v hex")
	}
	signature, err := hex.DecodeString(value)
	if err != nil {
		return nil, errors.New("signature must be hexadecimal")
	}
	switch signature[64] {
	case 27, 28:
		signature[64] -= 27
	case 0, 1:
	default:
		return nil, errors.New("signature recovery id must be 0, 1, 27, or 28")
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	if !ethcrypto.ValidateSignatureValues(signature[64], r, s, true) {
		return nil, errors.New("signature has invalid or non-canonical secp256k1 values")
	}
	return signature, nil
}
