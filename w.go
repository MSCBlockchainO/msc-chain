package main

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tyler-smith/go-bip39"
	"golang.org/x/crypto/argon2"
	"golang.org/x/term"
)

const (
	// `keyEncryptionVersion` defines the key used to access the related value.
	keyEncryptionVersion = 2
	// `keyEncryptionKDF` defines the key used to access the related value.
	keyEncryptionKDF = "argon2id"
	// `keyEncryptionAAD` defines the key used to access the related value.
	keyEncryptionAAD = "MSC|encrypted-key|v2"

	// `defaultArgon2Time` defines the constant value used by this package.
	defaultArgon2Time uint32 = 3
	// `defaultArgon2Memory` defines the constant value used by this package.
	defaultArgon2Memory uint32 = 64 * 1024 // KiB (64 MiB)
	// `defaultArgon2Threads` defines the constant value used by this package.
	defaultArgon2Threads uint8 = 2

	// `encryptedKeySaltSize` defines the measured quantity used by this operation.
	encryptedKeySaltSize = 16

	// `validatorPasswordEnv` defines whether the related condition is satisfied.
	validatorPasswordEnv = "MSC_VALIDATOR_PASSWORD"
	// `validatorPasswordModeEnv` defines whether the related condition is satisfied.
	validatorPasswordModeEnv = "MSC_VALIDATOR_PASSWORD_MODE"
	// `coreKeyCreateOverrideEnv` defines the constant value used by this package.
	coreKeyCreateOverrideEnv = "MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE"
	// `validatorKeyCreateOverride` defines whether the related condition is satisfied.
	validatorKeyCreateOverride = "MSC_ALLOW_VALIDATOR_KEY_CREATE"

	// `validatorUnlockMaxAttempts` defines whether the related condition is satisfied.
	validatorUnlockMaxAttempts = 3
	// `validatorUnlockRetryDelay` defines whether the related condition is satisfied.
	validatorUnlockRetryDelay = 700 * time.Millisecond

	// `validatorKeyMetaFileName` defines whether the related condition is satisfied.
	validatorKeyMetaFileName = "validator.meta.json"
	// `validatorKeyBackupManifestFileName` defines whether the related condition is satisfied.
	validatorKeyBackupManifestFileName = "validator.backup.manifest.json"
	// `validatorKeyBackupFileName` defines whether the related condition is satisfied.
	validatorKeyBackupFileName = "validator.sec.bak"
	// `validatorPublicFileName` defines whether the related condition is satisfied.
	validatorPublicFileName = "validator.pub"
	// `validatorFingerprintLockFileName` defines whether the related condition is satisfied.
	validatorFingerprintLockFileName = "fingerprint.lock"
	// `autoValidatorKeyFileName` defines the auto-generated fresh-node validator key file.
	autoValidatorKeyFileName = "validator.key"
)

const (
	// `validatorPasswordModeFileOrPrompt` defines whether the related condition is satisfied.
	validatorPasswordModeFileOrPrompt = "file_or_prompt"
	// `validatorPasswordModePromptOnly` defines whether the related condition is satisfied.
	validatorPasswordModePromptOnly = "prompt_only"
	// `validatorPasswordModeEnvOnly` defines whether the related condition is satisfied.
	validatorPasswordModeEnvOnly = "env_only"
)

// IsFinalized reports whether finalized is true.
func IsFinalized(
	expectedStateRoot string,
	computedStateRoot string,
) bool {
	return expectedStateRoot == computedStateRoot
}

// DecodePublicKey decodes public key.
func DecodePublicKey(hexKey string) (ed25519.PublicKey, error) {
	// `b` and `err` store the error produced by this operation.
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, err
	}
	return ed25519.PublicKey(b), nil
}

// DecodeSignature decodes signature.
func DecodeSignature(hexSig string) ([]byte, error) {
	return hex.DecodeString(hexSig)
}

// BuildSignedTxSecure builds signed tx secure.
func BuildSignedTxSecure(
	w SecureWallet,
	password string,
	to string,
	amount int,
	currentNonce int,
	coin string,
) (Transaction, error) {

	// =====================================================
	// 🔓 DECRYPT PRIVATE KEY (HARD GATE)
	// =====================================================
	priv, err := DecryptPrivateKey(w, password)
	if err != nil {
		return Transaction{}, fmt.Errorf("wallet decryption failed")
	}
	defer ZeroMemory(priv)
	maybeUpgradeSecureWalletEncryption(w, priv, password)

	// =====================================================
	// 🧮 NONCE CONTROL (AUTHORITATIVE)
	// =====================================================
	nonce := currentNonce + 1

	// =====================================================
	// 🧾 BUILD TRANSACTION (UNSIGNED)
	// =====================================================
	tx := Transaction{
		From:      w.Address,
		To:        to,
		Amount:    amount,
		Nonce:     nonce,
		PublicKey: w.PublicKey,
		Fee:       ComputeTxFee(amount),
		Expiry:    time.Now().Add(2 * time.Minute).Unix(), // 🔒 anti-replay
		ChainID:   protocolChainID(),
		Coin:      normalizeCoin(coin),
	}

	// =====================================================
	// 🔐 PAYLOAD (DETERMINISTIC)
	// =====================================================
	payload := TxPayload(tx)

	// =====================================================
	// ✍️ SIGN (HASHED PAYLOAD)
	// =====================================================
	sig := Sign(priv, payload)
	tx.Signature = hex.EncodeToString(sig)

	// =====================================================
	// 🆔 TRANSACTION ID (DETERMINISTIC NETWORK FORMAT)
	// =====================================================
	tx.ID = ComputeTxID(tx)

	return tx, nil
}

// BuildSignedStakeTxSecure builds signed stake tx secure.
func BuildSignedStakeTxSecure(
	w SecureWallet,
	password string,
	validatorID string,
	validatorPubKey string,
	amount int,
	currentNonce int,
	coin string,
	lockEpochs uint64,
) (Transaction, error) {

	validatorID = strings.TrimSpace(validatorID)
	if validatorID == "" {
		return Transaction{}, fmt.Errorf("missing validator id")
	}
	if amount <= 0 {
		return Transaction{}, fmt.Errorf("invalid amount")
	}
	// `normalizedValidatorPubKey` stores the key used to access the related value.
	normalizedValidatorPubKey := normalizeConsensusPubKeyHex(validatorPubKey)
	if strings.TrimSpace(validatorPubKey) != "" && normalizedValidatorPubKey == "" {
		return Transaction{}, fmt.Errorf("invalid validator_pubkey")
	}

	// =====================================================
	// 🔓 DECRYPT PRIVATE KEY (HARD GATE)
	// =====================================================
	priv, err := DecryptPrivateKey(w, password)
	if err != nil {
		return Transaction{}, fmt.Errorf("wallet decryption failed")
	}
	defer ZeroMemory(priv)
	maybeUpgradeSecureWalletEncryption(w, priv, password)

	// =====================================================
	// 🧮 NONCE CONTROL (AUTHORITATIVE)
	// =====================================================
	nonce := currentNonce + 1

	// =====================================================
	// 🧾 BUILD TRANSACTION (UNSIGNED)
	// =====================================================
	tx := Transaction{
		From:            w.Address,
		To:              validatorID,
		Amount:          amount,
		Nonce:           nonce,
		PublicKey:       w.PublicKey,
		Fee:             ComputeTxFee(amount),
		Expiry:          time.Now().Add(2 * time.Minute).Unix(),
		ChainID:         protocolChainID(),
		Coin:            normalizeCoin(coin),
		Type:            TxStake,
		ValidatorPubKey: normalizedValidatorPubKey,
		StakeEpochs: func() uint64 {
			if lockEpochs > 0 {
				return lockEpochs
			}
			return DefaultStakeLockEpochs
		}(),
	}

	// =====================================================
	// 🔐 PAYLOAD (DETERMINISTIC)
	// =====================================================
	payload := TxPayload(tx)

	// =====================================================
	// ✍️ SIGN (HASHED PAYLOAD)
	// =====================================================
	sig := Sign(priv, payload)
	tx.Signature = hex.EncodeToString(sig)

	// =====================================================
	// 🆔 TRANSACTION ID (DETERMINISTIC NETWORK FORMAT)
	// =====================================================
	tx.ID = ComputeTxID(tx)

	return tx, nil
}

// BuildSignedUnstakeTxSecure builds signed unstake tx secure.
func BuildSignedUnstakeTxSecure(
	w SecureWallet,
	password string,
	validatorID string,
	amount int,
	currentNonce int,
	coin string,
) (Transaction, error) {

	validatorID = strings.TrimSpace(validatorID)
	if validatorID == "" {
		return Transaction{}, fmt.Errorf("missing validator id")
	}
	if amount <= 0 {
		return Transaction{}, fmt.Errorf("invalid amount")
	}

	// `priv` and `err` store the error produced by this operation.
	priv, err := DecryptPrivateKey(w, password)
	if err != nil {
		return Transaction{}, fmt.Errorf("wallet decryption failed")
	}
	defer ZeroMemory(priv)
	maybeUpgradeSecureWalletEncryption(w, priv, password)

	// `nonce` stores the value produced by this operation.
	nonce := currentNonce + 1

	// `tx` stores the transaction data handled by this operation.
	tx := Transaction{
		From:      w.Address,
		To:        validatorID,
		Amount:    amount,
		Nonce:     nonce,
		PublicKey: w.PublicKey,
		Fee:       ComputeTxFee(amount),
		Expiry:    time.Now().Add(2 * time.Minute).Unix(),
		ChainID:   protocolChainID(),
		Coin:      normalizeCoin(coin),
		Type:      TxUnstake,
	}

	// `payload` stores the value produced by this operation.
	payload := TxPayload(tx)
	// `sig` stores the value produced by this operation.
	sig := Sign(priv, payload)
	tx.Signature = hex.EncodeToString(sig)
	tx.ID = ComputeTxID(tx)

	return tx, nil
}

// BuildSignedValidatorUpdateTxSecure builds a governance-owned validator update tx.
func BuildSignedValidatorUpdateTxSecure(
	w SecureWallet,
	password string,
	action string,
	validatorID string,
	currentNonce int,
	coin string,
) (Transaction, error) {

	// `priv` and `err` store the error produced by this operation.
	priv, err := DecryptPrivateKey(w, password)
	if err != nil {
		return Transaction{}, fmt.Errorf("wallet decryption failed")
	}
	defer ZeroMemory(priv)
	maybeUpgradeSecureWalletEncryption(w, priv, password)

	if action != "add" && action != "activate" && action != "suspend" && action != "remove" {
		return Transaction{}, fmt.Errorf("invalid action")
	}
	if validatorID == "" {
		return Transaction{}, fmt.Errorf("missing validator id")
	}

	// `nonce` stores the value produced by this operation.
	nonce := currentNonce + 1
	// `target` stores the value produced by this operation.
	target := validatorUpdateAddPrefix + validatorID
	if action == "activate" {
		target = validatorUpdateActivatePrefix + validatorID
	} else if action == "suspend" {
		target = validatorUpdateSuspendPrefix + validatorID
	} else if action == "remove" {
		target = validatorUpdateRemovePrefix + validatorID
	}

	// `tx` stores the transaction data handled by this operation.
	tx := Transaction{
		From:      w.Address,
		To:        target,
		Amount:    0,
		Nonce:     nonce,
		PublicKey: w.PublicKey,
		Fee:       0,
		Expiry:    time.Now().Add(2 * time.Minute).Unix(),
		ChainID:   protocolChainID(),
		Coin:      normalizeCoin(coin),
		Type:      TxValidatorUpdate,
	}

	// `payload` stores the value produced by this operation.
	payload := TxPayload(tx)
	// `sig` stores the value produced by this operation.
	sig := Sign(priv, payload)
	tx.Signature = hex.EncodeToString(sig)

	tx.ID = ComputeTxID(tx)

	return tx, nil
}

// BuildValidatorUpdateCertSignatureSecure builds validator update cert signature secure.
func BuildValidatorUpdateCertSignatureSecure(
	w SecureWallet,
	password string,
	signerID string,
	action string,
	validatorID string,
	parentRegistryHash string,
	proposalNonce uint64,
	expiryHeight uint64,
) (ValidatorUpdateCertSignature, error) {

	// `priv` and `err` store the error produced by this operation.
	priv, err := DecryptPrivateKey(w, password)
	if err != nil {
		return ValidatorUpdateCertSignature{}, fmt.Errorf("wallet decryption failed")
	}
	defer ZeroMemory(priv)
	maybeUpgradeSecureWalletEncryption(w, priv, password)

	signerID = normalizeValidatorID(signerID)
	if signerID == "" {
		return ValidatorUpdateCertSignature{}, fmt.Errorf("missing signer id")
	}
	if action != "add" && action != "activate" && action != "suspend" && action != "remove" {
		return ValidatorUpdateCertSignature{}, fmt.Errorf("invalid action")
	}
	validatorID = normalizeValidatorID(validatorID)
	if validatorID == "" {
		return ValidatorUpdateCertSignature{}, fmt.Errorf("missing validator id")
	}
	parentRegistryHash = strings.ToLower(strings.TrimSpace(parentRegistryHash))
	if len(parentRegistryHash) != 64 {
		return ValidatorUpdateCertSignature{}, fmt.Errorf("invalid parent registry hash")
	}
	if proposalNonce == 0 {
		return ValidatorUpdateCertSignature{}, fmt.Errorf("missing proposal nonce")
	}
	if expiryHeight == 0 {
		return ValidatorUpdateCertSignature{}, fmt.Errorf("missing expiry height")
	}

	// `payload` stores the value produced by this operation.
	payload := validatorUpdateCertSigningPayload(
		protocolChainID(),
		action,
		validatorID,
		parentRegistryHash,
		proposalNonce,
		expiryHeight,
	)
	// `sig` stores the value produced by this operation.
	sig := Sign(priv, payload)
	return ValidatorUpdateCertSignature{
		SignerID: signerID,
		SigHex:   strings.ToLower(hex.EncodeToString(sig)),
	}, nil
}

// AttachValidatorUpdateCertificate implements the attach validator update certificate helper.
func AttachValidatorUpdateCertificate(tx *Transaction, cert *ValidatorUpdateCertificate, priv ed25519.PrivateKey) error {
	if tx == nil {
		return fmt.Errorf("tx is nil")
	}
	if tx.Type != TxValidatorUpdate {
		return fmt.Errorf("not a validator update tx")
	}
	if cert == nil {
		return fmt.Errorf("missing validator update cert")
	}
	if len(priv) != ed25519.PrivateKeySize {
		return fmt.Errorf("invalid private key")
	}
	// `certCopy` stores the value produced by this operation.
	certCopy := *cert
	normalizeValidatorUpdateCert(&certCopy)
	tx.ValidatorUpdateCert = &certCopy
	tx.Signature = strings.ToLower(hex.EncodeToString(Sign(priv, TxPayload(*tx))))
	tx.ID = ComputeTxID(*tx)
	return nil
}

// WalletToRPC implements the wallet to rpc helper.
func WalletToRPC(w Wallet) map[string]string {

	// `result` stores the result produced by this operation.
	result := make(map[string]string, 3)

	// =====================================================
	// 🔐 ADDRESS (AUTHORITATIVE IDENTITY)
	// =====================================================
	result["address"] = w.Address

	// =====================================================
	// 🔑 PUBLIC KEY (OPTIONAL, SAFE-ENCODED)
	// =====================================================
	if len(w.PublicKey) == ed25519.PublicKeySize {
		result["publicKey"] = hex.EncodeToString(w.PublicKey)
	} else {
		// Explicit empty value (RPC determinism)
		result["publicKey"] = ""
	}

	// =====================================================
	// 🧠 MODEL-3 EXTENSION HOOK (FUTURE SAFE)
	// =====================================================
	// Identity type helps RPC clients understand role
	result["type"] = "wallet"

	return result
}

// NewWallet generates keypair
func NewWallet() Wallet {
	// `pub` stores the value produced by this operation.
	pub, _, _ := ed25519.GenerateKey(cryptorand.Reader)
	// `address` stores the address used by this operation.
	address := AddressFromPublicKey(pub)
	return Wallet{
		PublicKey: pub,
		Address:   address,
	}
}

// walletPath implements the wallet path helper.
func walletPath() string {
	// `home` stores the value produced by this operation.
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".msc", "wallet.json")
}

// SaveWallet saves wallet.
func SaveWallet(w Wallet) error {
	os.MkdirAll(filepath.Dir(walletPath()), 0700)
	// `data` stores the value produced by this operation.
	data, _ := json.MarshalIndent(w, "", "  ")
	return os.WriteFile(walletPath(), data, 0600)
}

// LoadWallet loads wallet.
func LoadWallet() (Wallet, error) {
	// `data` and `err` store the error produced by this operation.
	data, err := os.ReadFile(walletPath())
	if err != nil {
		return Wallet{}, err
	}
	// `raw` stores the value used by this operation.
	var raw map[string]string
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(data, &raw); err != nil {
		return Wallet{}, err
	}
	return Wallet{
		Address: raw["address"],
	}, nil
}

// ensurePrivateDirectory implements the ensure private directory helper.
func ensurePrivateDirectory(path string) error {
	if path == "" {
		return errors.New("empty directory path")
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		// `err` stores the error produced by this operation.
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// ensurePrivateFilePermissions implements the ensure private file permissions helper.
func ensurePrivateFilePermissions(path string) error {
	if path == "" {
		return errors.New("empty file path")
	}
	if runtime.GOOS == "windows" {
		// Windows uses ACL semantics; POSIX mode-bit checks are not authoritative.
		return nil
	}
	// `fi` and `err` store the error produced by this operation.
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("permissions too open on %s: %o (expected 600)", path, fi.Mode().Perm())
	}
	return nil
}

// writePrivateFile implements the write private file helper.
func writePrivateFile(path string, data []byte) error {
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		// `err` stores the error produced by this operation.
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// isEncryptedKeyV2 implements the is encrypted key v2 helper.
func isEncryptedKeyV2(k EncryptedKey) bool {
	return k.Version >= keyEncryptionVersion && strings.EqualFold(strings.TrimSpace(k.KDF), keyEncryptionKDF)
}

// deriveKeyArgon2ID implements the derive key argon2 id helper.
func deriveKeyArgon2ID(password string, salt []byte, timeCost uint32, memoryKiB uint32, threads uint8) []byte {
	if timeCost == 0 {
		timeCost = defaultArgon2Time
	}
	if memoryKiB == 0 {
		memoryKiB = defaultArgon2Memory
	}
	if threads == 0 {
		threads = defaultArgon2Threads
	}
	return argon2.IDKey([]byte(password), salt, timeCost, memoryKiB, threads, 32)
}

// decryptPrivateKeyLegacy implements the decrypt private key legacy helper.
func decryptPrivateKeyLegacy(password string, salt []byte, nonce []byte, ciphertext []byte) (ed25519.PrivateKey, error) {
	// `key` stores the key used to access the related value.
	key := sha256.Sum256(append([]byte(password), salt...))
	// `block` and `err` store the error produced by this operation.
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	// `gcm` and `err` store the error produced by this operation.
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// `plain` and `err` store the error produced by this operation.
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("invalid password or corrupted wallet")
	}
	if len(plain) != ed25519.PrivateKeySize {
		ZeroMemory(plain)
		return nil, errors.New("invalid private key length")
	}
	// `out` stores the result produced by this operation.
	out := make([]byte, len(plain))
	copy(out, plain)
	ZeroMemory(plain)
	return ed25519.PrivateKey(out), nil
}

// decryptPrivateKeyV2 implements the decrypt private key v2 helper.
func decryptPrivateKeyV2(enc EncryptedKey, password string, salt []byte, nonce []byte, ciphertext []byte) (ed25519.PrivateKey, error) {
	// `key` stores the key used to access the related value.
	key := deriveKeyArgon2ID(password, salt, enc.Argon2Time, enc.Argon2MemoryKiB, enc.Argon2Threads)
	defer ZeroMemory(key)

	// `block` and `err` store the error produced by this operation.
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	// `gcm` and `err` store the error produced by this operation.
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// `plain` and `err` store the error produced by this operation.
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(keyEncryptionAAD))
	if err != nil {
		return nil, errors.New("invalid password or corrupted wallet")
	}
	if len(plain) != ed25519.PrivateKeySize {
		ZeroMemory(plain)
		return nil, errors.New("invalid private key length")
	}
	// `out` stores the result produced by this operation.
	out := make([]byte, len(plain))
	copy(out, plain)
	ZeroMemory(plain)
	return ed25519.PrivateKey(out), nil
}

// maybeUpgradeSecureWalletEncryption implements the maybe upgrade secure wallet encryption helper.
func maybeUpgradeSecureWalletEncryption(w SecureWallet, priv ed25519.PrivateKey, password string) {
	if isEncryptedKeyV2(w.Crypto) {
		return
	}
	// `enc` and `err` store the error produced by this operation.
	enc, err := EncryptPrivateKey(priv, password)
	if err != nil {
		return
	}
	w.Crypto = enc
	// `err` stores the error produced by this operation.
	if err := SaveSecureWallet(w); err == nil && DebugConsensus {
		fmt.Println("🔐 Secure wallet encryption upgraded to argon2id (v2)")
	}
}

// DecryptPrivateKey implements the decrypt private key helper.
func DecryptPrivateKey(w SecureWallet, password string) (ed25519.PrivateKey, error) {
	// `salt` and `err` store the error produced by this operation.
	salt, err := hex.DecodeString(w.Crypto.Salt)
	if err != nil {
		return nil, err
	}

	// `nonce` and `err` store the error produced by this operation.
	nonce, err := hex.DecodeString(w.Crypto.Nonce)
	if err != nil {
		return nil, err
	}

	// `ciphertext` and `err` store the error produced by this operation.
	ciphertext, err := hex.DecodeString(w.Crypto.Ciphertext)
	if err != nil {
		return nil, err
	}
	if isEncryptedKeyV2(w.Crypto) {
		return decryptPrivateKeyV2(w.Crypto, password, salt, nonce, ciphertext)
	}
	return decryptPrivateKeyLegacy(password, salt, nonce, ciphertext)
}

// ZeroMemory implements the zero memory helper.
func ZeroMemory(b []byte) {
	if b == nil {
		return
	}

	// =====================================================
	// 🔒 FORCE WRITE (ANTI-COMPILER OPTIMIZATION)
	// =====================================================
	for i := 0; i < len(b); i++ {
		b[i] = 0
	}

	// =====================================================
	// 🧠 MEMORY BARRIER (BEST-EFFORT)
	// =====================================================
	// Prevent compiler from reordering / skipping writes
	_ = b[0]
}

// SignSignature signs signature.
func SignSignature(
	priv ed25519.PrivateKey,
	payload []byte,
) []byte {

	// 🔒 Hard guard
	if len(priv) != ed25519.PrivateKeySize {
		return nil
	}

	// 🔐 Single canonical hash
	hash := sha256.Sum256(payload)

	// ✅ Cryptographic authority
	return ed25519.Sign(priv, hash[:])
}

// TxPayload returns deterministic signing bytes

// SecureWalletPath implements the secure wallet path helper.
func SecureWalletPath() string {
	// `home` stores the value produced by this operation.
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".msc", "secure_wallet.json")
}

// SaveSecureWallet saves secure wallet.
func SaveSecureWallet(w SecureWallet) error {
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(filepath.Dir(SecureWalletPath())); err != nil {
		return err
	}
	// `data` and `err` store the error produced by this operation.
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(SecureWalletPath(), data)
}

// EncryptPrivateKey implements the encrypt private key helper.
func EncryptPrivateKey(
	priv ed25519.PrivateKey,
	password string,
) (EncryptedKey, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return EncryptedKey{}, errors.New("invalid private key length")
	}
	if strings.TrimSpace(password) == "" {
		return EncryptedKey{}, errors.New("empty password")
	}
	// `salt` stores the value produced by this operation.
	salt := make([]byte, encryptedKeySaltSize)
	// `err` stores the error produced by this operation.
	if _, err := cryptorand.Read(salt); err != nil {
		return EncryptedKey{}, err
	}
	// `key` stores the key used to access the related value.
	key := deriveKeyArgon2ID(password, salt, defaultArgon2Time, defaultArgon2Memory, defaultArgon2Threads)
	defer ZeroMemory(key)

	// `block` and `err` store the error produced by this operation.
	block, err := aes.NewCipher(key)
	if err != nil {
		return EncryptedKey{}, err
	}
	// `gcm` and `err` store the error produced by this operation.
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedKey{}, err
	}
	// `nonce` stores the value produced by this operation.
	nonce := make([]byte, gcm.NonceSize())
	// `err` stores the error produced by this operation.
	if _, err := cryptorand.Read(nonce); err != nil {
		return EncryptedKey{}, err
	}
	// `ciphertext` stores the value produced by this operation.
	ciphertext := gcm.Seal(nil, nonce, priv, []byte(keyEncryptionAAD))
	return EncryptedKey{
		Version:         keyEncryptionVersion,
		KDF:             keyEncryptionKDF,
		Argon2Time:      defaultArgon2Time,
		Argon2MemoryKiB: defaultArgon2Memory,
		Argon2Threads:   defaultArgon2Threads,
		Ciphertext:      hex.EncodeToString(ciphertext),
		Nonce:           hex.EncodeToString(nonce),
		Salt:            hex.EncodeToString(salt),
	}, nil
}

// CreateSecureWalletWithPath creates secure wallet with path.
func CreateSecureWalletWithPath(
	password string,
	account uint32,
	change uint32,
	index uint32,
) (SecureWallet, []string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return SecureWallet{}, nil, errors.New("password required")
	}

	// `entropy` and `err` store the error produced by this operation.
	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	// `mnemonic` and `err` store the error produced by this operation.
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return SecureWallet{}, nil, err
	}

	// `seed` stores the value produced by this operation.
	seed := bip39.NewSeed(mnemonic, password)
	// `pub`, `priv`, `hd`, and `err` store the error produced by this operation.
	pub, priv, hd, err := deriveHDKeypairFromSeed(seed, account, change, index)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	defer ZeroMemory(priv)

	// `addr` stores the address used by this operation.
	addr := AddressFromPublicKey(pub)
	// `encrypted` and `err` store the error produced by this operation.
	encrypted, err := EncryptPrivateKey(priv, password)
	if err != nil {
		return SecureWallet{}, nil, fmt.Errorf("failed to encrypt wallet key: %w", err)
	}

	// `w` stores the value produced by this operation.
	w := SecureWallet{
		Address:   addr,
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encrypted,
		HD:        hd,
	}
	// `err` stores the error produced by this operation.
	if err := SaveSecureWallet(w); err != nil {
		return SecureWallet{}, nil, fmt.Errorf("failed to save secure wallet: %w", err)
	}
	return w, strings.Split(mnemonic, " "), nil
}

// CreateSecureWallet creates secure wallet.
func CreateSecureWallet(password string) (SecureWallet, []string) {
	// `w`, `mnemonic`, and `err` store the error produced by this operation.
	w, mnemonic, err := CreateSecureWalletWithPath(
		password,
		hdDefaultAccount,
		hdDefaultChange,
		hdDefaultIndex,
	)
	if err != nil {
		log.Printf("[WARN] CreateSecureWallet failed: %v", err)
		return SecureWallet{}, nil
	}
	return w, mnemonic
}

// RecoverWalletWithPath implements the recover wallet with path helper.
func RecoverWalletWithPath(
	mnemonic string,
	password string,
	account uint32,
	change uint32,
	index uint32,
) (SecureWallet, error) {
	mnemonic = strings.TrimSpace(mnemonic)
	password = strings.TrimSpace(password)
	if mnemonic == "" || password == "" {
		return SecureWallet{}, errors.New("mnemonic and password required")
	}
	if !bip39.IsMnemonicValid(mnemonic) {
		return SecureWallet{}, errors.New("invalid mnemonic")
	}

	// `seed` stores the value produced by this operation.
	seed := bip39.NewSeed(mnemonic, password)
	// `pub`, `priv`, `hd`, and `err` store the error produced by this operation.
	pub, priv, hd, err := deriveHDKeypairFromSeed(seed, account, change, index)
	if err != nil {
		return SecureWallet{}, err
	}
	defer ZeroMemory(priv)

	// `addr` stores the address used by this operation.
	addr := AddressFromPublicKey(pub)
	// `encrypted` and `err` store the error produced by this operation.
	encrypted, err := EncryptPrivateKey(priv, password)
	if err != nil {
		return SecureWallet{}, fmt.Errorf("failed to encrypt wallet key: %w", err)
	}
	return SecureWallet{
		Address:   addr,
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encrypted,
		HD:        hd,
	}, nil
}

// RecoverWallet implements the recover wallet helper.
func RecoverWallet(mnemonic, password string) SecureWallet {
	// `w` and `err` store the error produced by this operation.
	w, err := RecoverWalletWithPath(
		mnemonic,
		password,
		hdDefaultAccount,
		hdDefaultChange,
		hdDefaultIndex,
	)
	if err != nil {
		log.Printf("[WARN] RecoverWallet failed: %v", err)
		return SecureWallet{}
	}
	return w
}

// GenerateValidatorKey implements the generate validator key helper.
func GenerateValidatorKey(nodeID string) ValidatorKey {
	// `pub` and `priv` store the value produced by this operation.
	pub, priv, _ := ed25519.GenerateKey(cryptorand.Reader)

	return ValidatorKey{
		ID:         nodeID,
		PublicKey:  pub,
		PrivateKey: priv,
	}
}

type autoValidatorKeyFile struct {
	NodeID      string `json:"node_id"`
	ValidatorID string `json:"validator_id"`
	PublicKey   string `json:"public_key"`
	PrivateKey  string `json:"private_key"`
	KeyType     string `json:"key_type"`
	CreatedAt   string `json:"created_at"`
	Version     int    `json:"version"`
}

func loadAutoValidatorKeyFile(nodeID, path string) (ValidatorKey, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ValidatorKey{}, false, nil
		}
		return ValidatorKey{}, false, err
	}
	return parseAutoValidatorKeyFile(nodeID, path, raw)
}

func parseAutoValidatorKeyFile(nodeID, path string, raw []byte) (ValidatorKey, bool, error) {
	var stored autoValidatorKeyFile
	if err := json.Unmarshal(raw, &stored); err != nil {
		return ValidatorKey{}, true, fmt.Errorf("parse auto validator key %s: %w", path, err)
	}
	priv, err := hex.DecodeString(strings.TrimSpace(stored.PrivateKey))
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		return ValidatorKey{}, true, fmt.Errorf("auto validator key %s has invalid private_key", path)
	}
	pub, err := hex.DecodeString(strings.TrimSpace(stored.PublicKey))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return ValidatorKey{}, true, fmt.Errorf("auto validator key %s has invalid public_key", path)
	}
	privateKey := ed25519.PrivateKey(priv)
	derivedPub := privateKey.Public().(ed25519.PublicKey)
	if !bytes.Equal(derivedPub, ed25519.PublicKey(pub)) {
		return ValidatorKey{}, true, fmt.Errorf("auto validator key %s public/private mismatch", path)
	}
	return ValidatorKey{
		ID:         strings.TrimSpace(nodeID),
		PublicKey:  ed25519.PublicKey(pub),
		PrivateKey: privateKey,
	}, true, nil
}

func looksLikeAutoValidatorKeyFile(raw []byte) bool {
	var stored autoValidatorKeyFile
	if err := json.Unmarshal(raw, &stored); err != nil {
		return false
	}
	return strings.TrimSpace(stored.PrivateKey) != "" ||
		strings.TrimSpace(stored.PublicKey) != "" ||
		strings.TrimSpace(stored.KeyType) != ""
}

func createAutoValidatorKeyFile(nodeID, path string) (ValidatorKey, error) {
	key := GenerateValidatorKey(nodeID)
	if !isValidatorKeyUsable(key) {
		return ValidatorKey{}, errors.New("failed to generate auto validator key")
	}
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return ValidatorKey{}, err
	}
	stored := autoValidatorKeyFile{
		NodeID:      normalizeNodeIdentityID(nodeID),
		ValidatorID: validatorIDFromPublicKey(key.PublicKey),
		PublicKey:   hex.EncodeToString(key.PublicKey),
		PrivateKey:  hex.EncodeToString(key.PrivateKey),
		KeyType:     "ed25519",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
		Version:     nodeIdentityVersion,
	}
	raw, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return ValidatorKey{}, err
	}
	raw = append(raw, '\n')
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			loaded, found, loadErr := loadAutoValidatorKeyFile(nodeID, path)
			if loadErr != nil {
				return ValidatorKey{}, loadErr
			}
			if found {
				return loaded, nil
			}
		}
		return ValidatorKey{}, err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return ValidatorKey{}, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return ValidatorKey{}, err
	}
	if err := f.Close(); err != nil {
		return ValidatorKey{}, err
	}
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return key, nil
}

// fallbackValidatorKey implements the fallback validator key helper.
func fallbackValidatorKey(nodeID, reason string) ValidatorKey {
	recordValidatorKeyLoadMeta(nodeID, validatorKeyLoadMeta{
		Source:      "unavailable",
		IntegrityOK: false,
		ErrorReason: strings.TrimSpace(reason),
	})
	if reason != "" {
		log.Printf("[WARN] validator signer unavailable for %s: %s", nodeID, reason)
	} else {
		log.Printf("[WARN] validator signer unavailable for %s", nodeID)
	}
	return ValidatorKey{ID: nodeID}
}

// isValidatorKeyUsable implements the is validator key usable helper.
func isValidatorKeyUsable(v ValidatorKey) bool {
	return isValidatorSigningKeyUsable(v)
}

// envBool implements the env bool helper.
func envBool(name string) bool {
	// `value` stores the value currently being processed.
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// shouldAutoCreateValidatorKey implements the should auto create validator key helper.
func shouldAutoCreateValidatorKey(nodeID string) bool {
	// Never silently rotate validator identity. Require explicit operator intent.
	// Core validators keep their dedicated override for bootstrap safety.
	if isCoreValidator(nodeID) {
		return envBool(coreKeyCreateOverrideEnv)
	}
	return envBool(validatorKeyCreateOverride)
}

// hasExistingNodeState implements the has existing node state helper.
func hasExistingNodeState(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	// Only treat canonical chain data as "existing chain state".
	// Peer/validator caches can exist on fresh nodes and must not block key bootstrap.
	candidates := []string{
		filepath.Join(path, "ledger.json"),
		filepath.Join(path, "blocks.db"),
		filepath.Join(path, "state.db"),
	}
	// `candidate` tracks the current values while iterating.
	for _, candidate := range candidates {
		// `info` and `err` store the error produced by this operation.
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
			// `entries` and `readErr` store the error produced by this operation.
			entries, readErr := os.ReadDir(candidate)
			if readErr == nil && len(entries) > 0 {
				return true
			}
			continue
		}
		if info.Size() > 0 {
			return true
		}
	}
	return false
}

// validatorKeyFingerprint implements the validator key fingerprint helper.
func validatorKeyFingerprint(pub []byte) string {
	if len(pub) != ed25519.PublicKeySize {
		return ""
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

type validatorKeyMetaFile struct {
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id"`
	// `Fingerprint` stores the value associated with this record.
	Fingerprint string `json:"fingerprint"`
	// `CryptoVersion` stores the value associated with this record.
	CryptoVersion int `json:"crypto_version"`
	// `CreatedAt` stores the value associated with this record.
	CreatedAt int64 `json:"created_at"`
	// `UpdatedAt` stores the value associated with this record.
	UpdatedAt int64 `json:"updated_at"`
}

type validatorKeyBackupManifest struct {
	// `BackupPath` stores the value associated with this record.
	BackupPath string `json:"backup_path"`
	// `BackupSHA256` stores the value associated with this record.
	BackupSHA256 string `json:"backup_sha256"`
	// `Fingerprint` stores the value associated with this record.
	Fingerprint string `json:"fingerprint"`
	// `UpdatedAt` stores the value associated with this record.
	UpdatedAt int64 `json:"updated_at"`
	// `LastVerifiedAt` stores the value associated with this record.
	LastVerifiedAt int64 `json:"last_verified_at"`
}

type validatorKeyLoadMeta struct {
	// `Source` stores the value associated with this record.
	Source string
	// `IntegrityOK` stores whether the related condition is satisfied.
	IntegrityOK bool
	// `ErrorReason` stores the error produced by this operation.
	ErrorReason string
}

// `validatorKeyLoadState` stores whether the related condition is satisfied.
var validatorKeyLoadState = struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `m` stores the value associated with this record.
	m map[string]validatorKeyLoadMeta
}{
	m: make(map[string]validatorKeyLoadMeta),
}

// recordValidatorKeyLoadMeta implements the record validator key load meta helper.
func recordValidatorKeyLoadMeta(nodeID string, meta validatorKeyLoadMeta) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return
	}
	validatorKeyLoadState.mu.Lock()
	validatorKeyLoadState.m[id] = meta
	validatorKeyLoadState.mu.Unlock()
}

// getValidatorKeyLoadMeta implements the get validator key load meta helper.
func getValidatorKeyLoadMeta(nodeID string) (validatorKeyLoadMeta, bool) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return validatorKeyLoadMeta{}, false
	}
	validatorKeyLoadState.mu.RLock()
	// `meta` and `ok` store whether the related condition is satisfied.
	meta, ok := validatorKeyLoadState.m[id]
	validatorKeyLoadState.mu.RUnlock()
	return meta, ok
}

// validatorKeyMode implements the validator key mode helper.
func validatorKeyMode() string {
	if ValidatorMPCEnabled {
		return "mpc"
	}
	if ValidatorHSMEnabled {
		return "hsm"
	}
	if ValidatorKeyBackupRequired || strings.TrimSpace(ValidatorRequiredKeyFingerprint) != "" {
		return "strict"
	}
	return "warning"
}

// validatorKeyMetaPath implements the validator key meta path helper.
func validatorKeyMetaPath(nodePath string) string {
	return filepath.Join(nodePath, validatorKeyMetaFileName)
}

// validatorKeyBackupManifestPath implements the validator key backup manifest path helper.
func validatorKeyBackupManifestPath(nodePath string) string {
	return filepath.Join(nodePath, validatorKeyBackupManifestFileName)
}

// validatorPublicPath implements the validator public path helper.
func validatorPublicPath(nodePath string) string {
	return filepath.Join(nodePath, validatorPublicFileName)
}

// validatorFingerprintLockPath implements the validator fingerprint lock path helper.
func validatorFingerprintLockPath(nodePath string) string {
	return filepath.Join(nodePath, validatorFingerprintLockFileName)
}

// resolveValidatorKeyBackupDir implements the resolve validator key backup dir helper.
func resolveValidatorKeyBackupDir(nodePath string) string {
	// `dir` stores the value produced by this operation.
	dir := strings.TrimSpace(ValidatorKeyBackupDir)
	if dir == "" {
		dir = "secure-backups"
	}
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(nodePath, dir)
}

// defaultValidatorKeyBackupPath returns the default validator key backup path.
func defaultValidatorKeyBackupPath(nodePath string) string {
	return filepath.Join(resolveValidatorKeyBackupDir(nodePath), validatorKeyBackupFileName)
}

// toManifestPath implements the to manifest path helper.
func toManifestPath(basePath, targetPath string) string {
	targetPath = filepath.Clean(targetPath)
	// `rel` and `err` store the error produced by this operation.
	if rel, err := filepath.Rel(basePath, targetPath); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return targetPath
}

// fromManifestPath implements the from manifest path helper.
func fromManifestPath(basePath, targetPath string) string {
	targetPath = strings.TrimSpace(targetPath)
	if targetPath == "" {
		return ""
	}
	if filepath.IsAbs(targetPath) {
		return filepath.Clean(targetPath)
	}
	return filepath.Clean(filepath.Join(basePath, targetPath))
}

// fileSHA256Hex implements the file sha256 hex helper.
func fileSHA256Hex(path string) (string, error) {
	// `f` and `err` store the error produced by this operation.
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	// `h` stores the value produced by this operation.
	h := sha256.New()
	// `err` stores the error produced by this operation.
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeValidatorKeyMeta implements the write validator key meta helper.
func writeValidatorKeyMeta(nodeID, nodePath, fingerprint string, cryptoVersion int) error {
	// `metaPath` stores the value produced by this operation.
	metaPath := validatorKeyMetaPath(nodePath)
	// `now` stores the value produced by this operation.
	now := time.Now().Unix()
	// `meta` stores the value produced by this operation.
	meta := validatorKeyMetaFile{
		NodeID:        normalizeValidatorID(nodeID),
		Fingerprint:   strings.TrimSpace(fingerprint),
		CryptoVersion: cryptoVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	// `raw` and `err` store the error produced by this operation.
	if raw, err := os.ReadFile(metaPath); err == nil && len(raw) > 0 {
		// `prev` stores the value used by this operation.
		var prev validatorKeyMetaFile
		// `jsonErr` stores the error produced by this operation.
		if jsonErr := json.Unmarshal(raw, &prev); jsonErr == nil {
			if prev.CreatedAt > 0 {
				meta.CreatedAt = prev.CreatedAt
			}
		}
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(metaPath, raw)
}

// writeValidatorPublicKeyFile implements the write validator public key file helper.
func writeValidatorPublicKeyFile(nodePath string, pub []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid validator public key length")
	}
	// `path` stores the value produced by this operation.
	path := validatorPublicPath(nodePath)
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	// `payload` stores the value produced by this operation.
	payload := []byte(hex.EncodeToString(pub) + "\n")
	// `err` stores the error produced by this operation.
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		// `err` stores the error produced by this operation.
		if err := os.Chmod(path, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// enforceValidatorFingerprintLock implements the enforce validator fingerprint lock helper.
func enforceValidatorFingerprintLock(nodePath, fingerprint string) error {
	// `fp` stores the value produced by this operation.
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return errors.New("validator key fingerprint is empty")
	}
	// `lockPath` stores the synchronization state protecting shared data.
	lockPath := validatorFingerprintLockPath(nodePath)
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(lockPath)
	if err == nil {
		// `locked` stores the synchronization state protecting shared data.
		locked := strings.TrimSpace(string(raw))
		if locked == "" {
			return fmt.Errorf("empty fingerprint lock (%s)", lockPath)
		}
		if !strings.EqualFold(locked, fp) {
			return fmt.Errorf("validator fingerprint lock mismatch: locked=%s got=%s", locked, fp)
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return writePrivateFile(lockPath, []byte(fp+"\n"))
}

// readValidatorKeyBackupManifest implements the read validator key backup manifest helper.
func readValidatorKeyBackupManifest(nodePath string) (validatorKeyBackupManifest, error) {
	// `path` stores the value produced by this operation.
	path := validatorKeyBackupManifestPath(nodePath)
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		return validatorKeyBackupManifest{}, err
	}
	// `manifest` stores the value used by this operation.
	var manifest validatorKeyBackupManifest
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return validatorKeyBackupManifest{}, err
	}
	return manifest, nil
}

// writeValidatorKeyBackupManifest implements the write validator key backup manifest helper.
func writeValidatorKeyBackupManifest(nodePath string, manifest validatorKeyBackupManifest) error {
	// `path` stores the value produced by this operation.
	path := validatorKeyBackupManifestPath(nodePath)
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, raw)
}

// copyFilePrivate copies file private.
func copyFilePrivate(dstPath, srcPath string) error {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return writePrivateFile(dstPath, raw)
}

// refreshValidatorKeyArtifacts implements the refresh validator key artifacts helper.
func refreshValidatorKeyArtifacts(nodeID, nodePath, keyPath string, key ValidatorKey, crypto EncryptedKey) error {
	if !isValidatorKeyUsable(key) {
		return errors.New("validator key unusable")
	}
	// `fp` stores the value produced by this operation.
	fp := validatorKeyFingerprint(key.PublicKey)
	if fp == "" {
		return errors.New("failed to compute validator key fingerprint")
	}
	// `err` stores the error produced by this operation.
	if err := writeValidatorKeyMeta(nodeID, nodePath, fp, crypto.Version); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := writeValidatorPublicKeyFile(nodePath, key.PublicKey); err != nil {
		return err
	}

	// `backupPath` stores the value produced by this operation.
	backupPath := defaultValidatorKeyBackupPath(nodePath)
	// `err` stores the error produced by this operation.
	if err := copyFilePrivate(backupPath, keyPath); err != nil {
		return err
	}
	// `sum` and `err` store the error produced by this operation.
	sum, err := fileSHA256Hex(backupPath)
	if err != nil {
		return err
	}
	// `now` stores the value produced by this operation.
	now := time.Now().Unix()
	// `manifest` stores the value produced by this operation.
	manifest := validatorKeyBackupManifest{
		BackupPath:     toManifestPath(nodePath, backupPath),
		BackupSHA256:   sum,
		Fingerprint:    fp,
		UpdatedAt:      now,
		LastVerifiedAt: now,
	}
	// `err` stores the error produced by this operation.
	if err := writeValidatorKeyBackupManifest(nodePath, manifest); err != nil {
		return err
	}
	return nil
}

// validateValidatorBackup validates validator backup.
func validateValidatorBackup(nodePath string, fingerprint string) (bool, uint64, error) {
	// `manifest` and `err` store the error produced by this operation.
	manifest, err := readValidatorKeyBackupManifest(nodePath)
	// `manifestFound` stores whether the related condition is satisfied.
	manifestFound := err == nil
	// `backupPath` stores the value produced by this operation.
	backupPath := ""
	if manifestFound {
		backupPath = fromManifestPath(nodePath, manifest.BackupPath)
	}
	if strings.TrimSpace(backupPath) == "" {
		backupPath = defaultValidatorKeyBackupPath(nodePath)
	}
	// `info` and `err` store the error produced by this operation.
	info, err := os.Stat(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, fmt.Errorf("missing backup file %s", backupPath)
		}
		return false, 0, err
	}
	// `age` stores the value produced by this operation.
	age := time.Since(info.ModTime())
	if age < 0 {
		age = 0
	}
	// `ageSeconds` stores the value produced by this operation.
	ageSeconds := uint64(age / time.Second)
	if ValidatorKeyBackupMaxAgeHours > 0 {
		// `limit` stores the value produced by this operation.
		limit := time.Duration(ValidatorKeyBackupMaxAgeHours) * time.Hour
		if age > limit {
			return true, ageSeconds, fmt.Errorf("backup too old: age=%s max=%s", age.Truncate(time.Second), limit)
		}
	}
	if manifestFound {
		if strings.TrimSpace(manifest.BackupSHA256) != "" {
			// `sum` and `sumErr` store the error produced by this operation.
			sum, sumErr := fileSHA256Hex(backupPath)
			if sumErr != nil {
				return true, ageSeconds, sumErr
			}
			if !strings.EqualFold(strings.TrimSpace(sum), strings.TrimSpace(manifest.BackupSHA256)) {
				return true, ageSeconds, fmt.Errorf("backup checksum mismatch for %s", backupPath)
			}
		}
		if strings.TrimSpace(manifest.Fingerprint) != "" && strings.TrimSpace(fingerprint) != "" {
			if !strings.EqualFold(strings.TrimSpace(manifest.Fingerprint), strings.TrimSpace(fingerprint)) {
				return true, ageSeconds, fmt.Errorf("backup fingerprint mismatch: manifest=%s key=%s", manifest.Fingerprint, fingerprint)
			}
		}
	}
	return true, ageSeconds, nil
}

// restoreValidatorKeyFromBackup implements the restore validator key from backup helper.
func restoreValidatorKeyFromBackup(nodeID, nodePath, keyPath string) error {
	// `manifest` and `manifestErr` store the error produced by this operation.
	manifest, manifestErr := readValidatorKeyBackupManifest(nodePath)
	// `candidates` stores the value produced by this operation.
	candidates := make([]string, 0, 2)
	if manifestErr == nil {
		// `p` stores the value produced by this operation.
		if p := fromManifestPath(nodePath, manifest.BackupPath); strings.TrimSpace(p) != "" {
			candidates = append(candidates, p)
		}
	}
	// `defaultPath` stores the value produced by this operation.
	defaultPath := defaultValidatorKeyBackupPath(nodePath)
	if defaultPath != "" {
		// `duplicate` stores the value produced by this operation.
		duplicate := false
		// `c` tracks the current values while iterating.
		for _, c := range candidates {
			if filepath.Clean(c) == filepath.Clean(defaultPath) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, defaultPath)
		}
	}
	// `lastErr` stores the error produced by this operation.
	var lastErr error
	// `candidate` tracks the current values while iterating.
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		// `err` stores the error produced by this operation.
		if _, err := os.Stat(candidate); err != nil {
			lastErr = err
			continue
		}
		if manifestErr == nil && strings.EqualFold(filepath.Clean(candidate), filepath.Clean(fromManifestPath(nodePath, manifest.BackupPath))) {
			if strings.TrimSpace(manifest.BackupSHA256) != "" {
				// `sum` and `err` store the error produced by this operation.
				sum, err := fileSHA256Hex(candidate)
				if err != nil {
					lastErr = err
					continue
				}
				if !strings.EqualFold(strings.TrimSpace(sum), strings.TrimSpace(manifest.BackupSHA256)) {
					lastErr = fmt.Errorf("backup checksum mismatch for %s", candidate)
					continue
				}
			}
		}
		// `err` stores the error produced by this operation.
		if err := copyFilePrivate(keyPath, candidate); err != nil {
			lastErr = err
			continue
		}
		log.Printf("[KEY-RESTORE] validator=%s source=backup result=success backup=%s", nodeID, candidate)
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("no valid backup candidate found")
	}
	log.Printf("[KEY-RESTORE] validator=%s source=backup result=failed error=%v", nodeID, lastErr)
	return lastErr
}

// CollectValidatorKeyHealth implements the collect validator key health helper.
func CollectValidatorKeyHealth(nodeID, nodePath string, key ValidatorKey) ValidatorKeyHealth {
	// `out` stores the result produced by this operation.
	out := ValidatorKeyHealth{
		Loaded: isValidatorKeyUsable(key),
		Mode:   validatorKeyMode(),
	}
	out.Expected = strings.TrimSpace(ValidatorRequiredKeyFingerprint)
	if out.Loaded {
		out.Fingerprint = validatorKeyFingerprint(key.PublicKey)
	}
	if out.Expected == "" {
		out.Match = true
	} else {
		out.Match = out.Loaded && strings.EqualFold(out.Fingerprint, out.Expected)
	}
	// `meta` and `ok` store whether the related condition is satisfied.
	if meta, ok := getValidatorKeyLoadMeta(nodeID); ok {
		if strings.TrimSpace(meta.Source) != "" {
			out.Source = meta.Source
		}
		out.IntegrityOK = out.Loaded && meta.IntegrityOK
	} else {
		out.IntegrityOK = out.Loaded
	}
	if strings.TrimSpace(out.Source) == "" {
		if out.Loaded {
			out.Source = "existing"
		} else {
			out.Source = "unavailable"
		}
	}
	// `backupPresent` and `backupAge` store the value produced by this operation.
	backupPresent, backupAge, _ := validateValidatorBackup(nodePath, out.Fingerprint)
	if (ValidatorHSMEnabled || ValidatorMPCEnabled) && out.Loaded {
		backupPresent = true
		backupAge = 0
	}
	out.BackupPresent = backupPresent
	out.BackupAgeSeconds = backupAge
	return out
}

// canPromptInteractive implements the can prompt interactive helper.
func canPromptInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

// promptValidatorKeySetup implements the prompt validator key setup helper.
func promptValidatorKeySetup(nodeID, keyPath string, isCore bool) bool {
	if !canPromptInteractive() {
		return false
	}

	fmt.Println()
	if isCore {
		fmt.Println("[SAFE MODE] Core validator key is missing.")
	} else {
		fmt.Println("[SAFE MODE] Validator key is missing.")
	}
	fmt.Printf("Node: %s\n", nodeID)
	fmt.Printf("Key path: %s\n", keyPath)
	fmt.Println("Creating a new validator.sec changes validator identity.")
	fmt.Println("1) Set validator password now (create new validator.sec)")
	fmt.Println("2) Continue without key (observer mode)")
	fmt.Print("Choose [1/2] (default 2): ")

	// `reader` stores the value produced by this operation.
	reader := bufio.NewReader(os.Stdin)
	// `line` and `err` store the error produced by this operation.
	line, err := reader.ReadString('\n')
	if err != nil {
		log.Printf("[WARN] failed to read bootstrap confirmation: %v", err)
		return false
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "1", "set", "create", "new", "y", "yes":
		return true
	default:
		return false
	}
}

// finalizeValidatorKeyLoad implements the finalize validator key load helper.
func finalizeValidatorKeyLoad(nodeID, path, keyPath, source string, key ValidatorKey, enc SecureWallet) ValidatorKey {
	if !isValidatorKeyUsable(key) {
		return fallbackValidatorKey(nodeID, "validator key unusable after load")
	}
	// `fp` stores the value produced by this operation.
	fp := validatorKeyFingerprint(key.PublicKey)
	if fp == "" {
		return fallbackValidatorKey(nodeID, "validator key fingerprint compute failed")
	}
	// `expected` stores the value produced by this operation.
	expected := strings.TrimSpace(ValidatorRequiredKeyFingerprint)
	// `match` stores the value produced by this operation.
	match := expected == "" || strings.EqualFold(fp, expected)
	if !match {
		return fallbackValidatorKey(nodeID,
			fmt.Sprintf("validator key fingerprint mismatch: expected=%s got=%s", expected, fp),
		)
	}
	// `err` stores the error produced by this operation.
	if err := enforceValidatorFingerprintLock(path, fp); err != nil {
		return fallbackValidatorKey(nodeID, err.Error())
	}
	// `err` stores the error produced by this operation.
	if err := refreshValidatorKeyArtifacts(nodeID, path, keyPath, key, enc.Crypto); err != nil {
		if ValidatorKeyBackupRequired {
			return fallbackValidatorKey(nodeID, fmt.Sprintf("validator key backup refresh failed: %v", err))
		}
		log.Printf("[KEY-AUDIT] validator=%s backup_present=false integrity=false reason=backup_refresh_failed error=%v", nodeID, err)
	}
	// `backupPresent`, `backupAge`, and `backupErr` store the error produced by this operation.
	backupPresent, backupAge, backupErr := validateValidatorBackup(path, fp)
	if backupErr != nil {
		log.Printf("[KEY-AUDIT] validator=%s backup_present=%t backup_age_s=%d integrity=false error=%v",
			nodeID,
			backupPresent,
			backupAge,
			backupErr,
		)
		if ValidatorKeyBackupRequired {
			return fallbackValidatorKey(nodeID, fmt.Sprintf("validator key backup audit failed: %v", backupErr))
		}
	} else {
		log.Printf("[KEY-AUDIT] validator=%s backup_present=%t backup_age_s=%d integrity=true",
			nodeID,
			backupPresent,
			backupAge,
		)
	}
	recordValidatorKeyLoadMeta(nodeID, validatorKeyLoadMeta{
		Source:      source,
		IntegrityOK: true,
		ErrorReason: "",
	})
	return key
}

// GenerateValidatorKeyOffline implements the generate validator key offline helper.
func GenerateValidatorKeyOffline(nodeID, nodePath string) (string, error) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return "", errors.New("node id is required")
	}
	if !canPromptInteractive() {
		return "", errors.New("offline keygen requires interactive terminal")
	}
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(nodePath); err != nil {
		return "", err
	}
	// `keyPath` stores the key used to access the related value.
	keyPath := filepath.Join(nodePath, "validator.sec")
	// `err` stores the error produced by this operation.
	if _, err := os.Stat(keyPath); err == nil {
		return "", fmt.Errorf("validator key already exists at %s; refusing overwrite", keyPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to access %s: %w", keyPath, err)
	}

	// `passRaw` and `err` store the error produced by this operation.
	passRaw, err := ReadPassword("New validator password: ")
	if err != nil {
		return "", fmt.Errorf("failed to read validator password: %w", err)
	}
	defer ZeroMemory(passRaw)
	// `pass` stores the value produced by this operation.
	pass := strings.TrimSpace(string(passRaw))
	// `err` stores the error produced by this operation.
	if err := validateNewValidatorPassword(pass); err != nil {
		return "", err
	}

	// `confirmRaw` and `err` store the error produced by this operation.
	confirmRaw, err := ReadPassword("Confirm validator password: ")
	if err != nil {
		return "", fmt.Errorf("failed to read validator password confirmation: %w", err)
	}
	defer ZeroMemory(confirmRaw)
	// `confirm` stores the value produced by this operation.
	confirm := strings.TrimSpace(string(confirmRaw))
	if pass != confirm {
		return "", errors.New("password confirmation mismatch")
	}

	// `pub`, `priv`, and `err` store the error produced by this operation.
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate validator key: %w", err)
	}
	// `encKey` and `err` store the error produced by this operation.
	encKey, err := EncryptPrivateKey(priv, pass)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt validator key: %w", err)
	}
	// `enc` stores the value produced by this operation.
	enc := SecureWallet{
		Address:   id,
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encKey,
	}
	// `data` and `err` store the error produced by this operation.
	data, err := json.MarshalIndent(enc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal validator key file: %w", err)
	}
	// `err` stores the error produced by this operation.
	if err := writePrivateFile(keyPath, data); err != nil {
		return "", fmt.Errorf("failed to persist validator key file: %w", err)
	}
	// `fp` stores the value produced by this operation.
	fp := validatorKeyFingerprint(pub)
	if fp == "" {
		return "", errors.New("failed to compute validator key fingerprint")
	}
	// `err` stores the error produced by this operation.
	if err := enforceValidatorFingerprintLock(nodePath, fp); err != nil {
		return "", err
	}
	// `created` stores the value produced by this operation.
	created := ValidatorKey{
		ID:         id,
		PublicKey:  pub,
		PrivateKey: priv,
	}
	// `err` stores the error produced by this operation.
	if err := refreshValidatorKeyArtifacts(id, nodePath, keyPath, created, enc.Crypto); err != nil {
		return "", fmt.Errorf("failed to refresh key artifacts: %w", err)
	}
	recordValidatorKeyLoadMeta(id, validatorKeyLoadMeta{
		Source:      "generated",
		IntegrityOK: true,
		ErrorReason: "",
	})
	return fp, nil
}

// loadOrCreateValidatorKeyInternal implements the load or create validator key internal helper.
func loadOrCreateValidatorKeyInternal(nodeID, path string, allowRestore bool, preferredSource string) ValidatorKey {
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(path); err != nil {
		return fallbackValidatorKey(nodeID, err.Error())
	}
	// `key` and `handled` store the key used to access the related value.
	if key, handled := LoadValidatorHSMKey(nodeID, path); handled {
		return key
	}

	// `keyPath` stores the key used to access the related value.
	keyPath := filepath.Join(path, "validator.sec")
	// `data` and `err` store the error produced by this operation.
	if data, err := os.ReadFile(keyPath); err == nil {
		// `err` stores the error produced by this operation.
		if err := ensurePrivateFilePermissions(keyPath); err != nil {
			log.Printf("[WARN] insecure validator key file permissions: %v", err)
		}

		// Some production validators were bootstrapped with the deterministic
		// auto-key JSON format stored at validator.sec instead of the encrypted
		// SecureWallet format. Detect that legacy format before asking for a
		// password; otherwise non-interactive systemd restarts fail with
		// prompt_required_non_interactive even though the validator key is
		// present and internally self-verifiable.
		if looksLikeAutoValidatorKeyFile(data) {
			key, found, autoErr := parseAutoValidatorKeyFile(nodeID, keyPath, data)
			if autoErr != nil {
				return fallbackValidatorKey(nodeID, autoErr.Error())
			}
			if found {
				return finalizeValidatorKeyLoad(nodeID, path, keyPath, "legacy_auto_validator_sec", key, SecureWallet{})
			}
		}

		// `enc` stores the value used by this operation.
		var enc SecureWallet
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(data, &enc); err != nil {
			return fallbackValidatorKey(nodeID, fmt.Sprintf("corrupted validator key file (%s); restore backup validator.sec", keyPath))
		}

		// `hasEnv` stores the value produced by this operation.
		if _, hasEnv := os.LookupEnv(validatorPasswordEnv); !hasEnv && canPromptInteractive() {
			log.Printf("validator key found for %s; enter password to unlock", nodeID)
		}

		// `pass`, `passFromEnv`, and `passErr` store the error produced by this operation.
		pass, passFromEnv, passErr := getValidatorPasswordWithSource(nodeID, path)
		if passErr != nil {
			return fallbackValidatorKey(nodeID, passErr.Error())
		}

		// `attempts` stores the value produced by this operation.
		attempts := validatorUnlockMaxAttempts
		if passFromEnv {
			// Automation mode should fail fast if env password is wrong.
			attempts = 1
		}

		var (
			// `priv` stores the value used by this operation.
			priv ed25519.PrivateKey
			// `derr` stores the error produced by this operation.
			derr error
		)
		// `attempt` stores the value produced by this operation.
		for attempt := 1; attempt <= attempts; attempt++ {
			priv, derr = DecryptPrivateKey(enc, pass)
			if derr == nil {
				break
			}

			if passFromEnv {
				break
			}

			if attempt < attempts {
				log.Printf(
					"[WARN] validator key decrypt failed for %s (attempt %d/%d); retry password prompt (validator.sec unchanged)",
					nodeID,
					attempt,
					attempts,
				)
				time.Sleep(validatorUnlockRetryDelay)
				pass, passFromEnv, passErr = getValidatorPasswordWithSource(nodeID, path)
				if passErr != nil {
					return fallbackValidatorKey(nodeID, passErr.Error())
				}
			}
		}
		if derr != nil {
			if passFromEnv {
				return fallbackValidatorKey(
					nodeID,
					fmt.Sprintf(
						"validator key decrypt failed (%s): invalid %s or corrupted key file (validator.sec kept unchanged)",
						keyPath,
						validatorPasswordEnv,
					),
				)
			}
			return fallbackValidatorKey(
				nodeID,
				fmt.Sprintf(
					"validator key decrypt failed after %d attempts (%s): wrong password or corrupted file (validator.sec kept unchanged)",
					attempts,
					keyPath,
				),
			)
		}

		// `pub` and `err` store the error produced by this operation.
		pub, err := hex.DecodeString(enc.PublicKey)
		if err != nil {
			return fallbackValidatorKey(nodeID, "invalid validator public key encoding")
		}
		if len(pub) != ed25519.PublicKeySize {
			return fallbackValidatorKey(nodeID, "invalid validator public key length")
		}

		if !isEncryptedKeyV2(enc.Crypto) {
			// `upgraded` and `err` store the error produced by this operation.
			if upgraded, err := EncryptPrivateKey(priv, pass); err == nil {
				enc.Crypto = upgraded
				enc.Address = nodeID
				enc.PublicKey = hex.EncodeToString(pub)
				// `rawEnc` and `err` store the error produced by this operation.
				if rawEnc, err := json.MarshalIndent(enc, "", "  "); err == nil {
					// `err` stores the error produced by this operation.
					if err := writePrivateFile(keyPath, rawEnc); err == nil {
						log.Println("validator key encryption upgraded to argon2id (v2)")
					}
				}
			}
		}

		log.Println("validator key unlocked")
		// `loaded` stores the value produced by this operation.
		loaded := ValidatorKey{
			ID:         nodeID,
			PublicKey:  ed25519.PublicKey(pub),
			PrivateKey: priv,
		}
		// `source` stores the value produced by this operation.
		source := "existing"
		if strings.TrimSpace(preferredSource) != "" {
			source = strings.TrimSpace(preferredSource)
		}
		return finalizeValidatorKeyLoad(nodeID, path, keyPath, source, loaded, enc)
	} else if !os.IsNotExist(err) {
		return fallbackValidatorKey(nodeID, fmt.Sprintf("failed to read validator key file %s: %v", keyPath, err))
	}

	autoKeyPath := filepath.Join(path, autoValidatorKeyFileName)
	if key, found, err := loadAutoValidatorKeyFile(nodeID, autoKeyPath); err != nil {
		return fallbackValidatorKey(nodeID, err.Error())
	} else if found {
		if err := refreshValidatorKeyArtifacts(nodeID, path, autoKeyPath, key, EncryptedKey{}); err != nil {
			return fallbackValidatorKey(nodeID, fmt.Sprintf("auto validator key artifact refresh failed: %v", err))
		}
		recordValidatorKeyLoadMeta(nodeID, validatorKeyLoadMeta{
			Source:      "auto_identity",
			IntegrityOK: true,
			ErrorReason: "",
		})
		return key
	}

	// `autoCreate` stores the value produced by this operation.
	autoCreate := shouldAutoCreateValidatorKey(nodeID)
	// `isCore` stores the current position in the related collection.
	isCore := isCoreValidator(nodeID)
	// `existingState` stores the value produced by this operation.
	existingState := hasExistingNodeState(path)
	if isAutoGeneratedNodeID(nodeID) && !existingState {
		key, err := createAutoValidatorKeyFile(nodeID, autoKeyPath)
		if err != nil {
			return fallbackValidatorKey(nodeID, fmt.Sprintf("auto validator key create failed: %v", err))
		}
		if err := refreshValidatorKeyArtifacts(nodeID, path, autoKeyPath, key, EncryptedKey{}); err != nil {
			return fallbackValidatorKey(nodeID, fmt.Sprintf("auto validator key artifact refresh failed: %v", err))
		}
		recordValidatorKeyLoadMeta(nodeID, validatorKeyLoadMeta{
			Source:      "generated_auto_identity",
			IntegrityOK: true,
			ErrorReason: "",
		})
		log.Printf("[IDENTITY] auto validator key created node_id=%s path=%s", nodeID, autoKeyPath)
		return key
	}
	// `restoreAttempted` stores the result produced by this operation.
	restoreAttempted := false
	if ValidatorKeyRestoreAllowedOnMissing && allowRestore {
		restoreAttempted = true
		// `err` stores the error produced by this operation.
		if err := restoreValidatorKeyFromBackup(nodeID, path, keyPath); err == nil {
			return loadOrCreateValidatorKeyInternal(nodeID, path, false, "restored")
		}
	}
	if !ValidatorAllowIdentityRotationOnExistingChain && existingState {
		if restoreAttempted {
			return fallbackValidatorKey(
				nodeID,
				fmt.Sprintf("missing_key_restore_failed (%s); restore backup and retry", keyPath),
			)
		}
		return fallbackValidatorKey(
			nodeID,
			fmt.Sprintf(
				"missing validator.sec on existing chain state (%s); restore original key or set validators.allow_identity_rotation_on_existing_chain=true",
				keyPath,
			),
		)
	}
	// Mainnet safety: when strict key-availability mode is enabled, do not
	// offer interactive key creation. Missing key must fail fast unless an
	// explicit bootstrap override is configured.
	if !autoCreate && ValidatorFailOnKeyUnavailable {
		if isCore {
			return fallbackValidatorKey(
				nodeID,
				fmt.Sprintf(
					"missing validator.sec for core validator (%s) with validators.fail_on_key_unavailable=true; restore key/backup or set %s=1 for explicit bootstrap",
					keyPath,
					coreKeyCreateOverrideEnv,
				),
			)
		}
		return fallbackValidatorKey(
			nodeID,
			fmt.Sprintf(
				"missing validator.sec for validator (%s) with validators.fail_on_key_unavailable=true; restore key/backup or set %s=1 for explicit bootstrap",
				keyPath,
				validatorKeyCreateOverride,
			),
		)
	}
	if !autoCreate {
		if promptValidatorKeySetup(nodeID, keyPath, isCore) {
			autoCreate = true
			if isCore {
				log.Printf("[WARN] core validator key setup confirmed interactively for %s", nodeID)
			} else {
				log.Printf("[WARN] validator key setup confirmed interactively for %s", nodeID)
			}
		}
	}

	if !autoCreate {
		if isCore {
			return fallbackValidatorKey(
				nodeID,
				fmt.Sprintf(
					"missing validator.sec for core validator (%s); choose setup option on startup or set %s=1 for non-interactive bootstrap",
					keyPath,
					coreKeyCreateOverrideEnv,
				),
			)
		}
		return fallbackValidatorKey(
			nodeID,
			fmt.Sprintf(
				"missing validator.sec for validator (%s); choose setup option on startup or set %s=1 for non-interactive bootstrap",
				keyPath,
				validatorKeyCreateOverride,
			),
		)
	}

	if autoCreate {
		// `overrideEnv` stores the value produced by this operation.
		overrideEnv := ""
		if envBool(coreKeyCreateOverrideEnv) {
			overrideEnv = coreKeyCreateOverrideEnv
		} else if envBool(validatorKeyCreateOverride) {
			overrideEnv = validatorKeyCreateOverride
		}
		if overrideEnv != "" {
			log.Printf("[WARN] validator key auto-create override %s=1 for %s; new key changes peer ID. Update p2p.persistent_peers/peers.json and remove override after bootstrap", overrideEnv, nodeID)
		}
	}

	// `hasEnv` stores the value produced by this operation.
	if _, hasEnv := os.LookupEnv(validatorPasswordEnv); !hasEnv && canPromptInteractive() {
		log.Printf("creating validator key for %s; set a new validator password", nodeID)
	}

	// `pass` and `passErr` store the error produced by this operation.
	pass, _, passErr := getValidatorPasswordWithSource(nodeID, path)
	if passErr != nil {
		return fallbackValidatorKey(nodeID, passErr.Error())
	}
	// `err` stores the error produced by this operation.
	if err := validateNewValidatorPassword(pass); err != nil {
		return fallbackValidatorKey(nodeID, err.Error())
	}

	// `pub`, `priv`, and `err` store the error produced by this operation.
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		return fallbackValidatorKey(nodeID, "failed to generate validator key")
	}

	// `encKey` and `err` store the error produced by this operation.
	encKey, err := EncryptPrivateKey(priv, pass)
	if err != nil {
		return fallbackValidatorKey(nodeID, fmt.Sprintf("failed to encrypt validator key: %v", err))
	}

	// `enc` stores the value produced by this operation.
	enc := SecureWallet{
		Address:   nodeID,
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encKey,
	}

	// `data` and `err` store the error produced by this operation.
	data, err := json.MarshalIndent(enc, "", "  ")
	if err != nil {
		return fallbackValidatorKey(nodeID, fmt.Sprintf("failed to marshal validator key file: %v", err))
	}

	// `err` stores the error produced by this operation.
	if err := writePrivateFile(keyPath, data); err != nil {
		return fallbackValidatorKey(nodeID, fmt.Sprintf("failed to persist validator key file: %v", err))
	}

	log.Println("validator key created & encrypted")
	// `created` stores the value produced by this operation.
	created := ValidatorKey{
		ID:         nodeID,
		PublicKey:  pub,
		PrivateKey: priv,
	}
	if existingState {
		log.Printf("[KEY-ROTATION] validator=%s old_fp=unknown new_fp=%s approved=%t",
			nodeID,
			validatorKeyFingerprint(pub),
			ValidatorAllowIdentityRotationOnExistingChain,
		)
	}
	return finalizeValidatorKeyLoad(nodeID, path, keyPath, "generated", created, enc)
}

// LoadOrCreateValidatorKey loads or create validator key.
func LoadOrCreateValidatorKey(nodeID, path string) ValidatorKey {
	return loadOrCreateValidatorKeyInternal(nodeID, path, true, "")
}

// validateCorePasswordFile validates core password file.
func validateCorePasswordFile(path string) error {
	// `stat` and `err` store the error produced by this operation.
	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat validators.core_password_file: %w", err)
	}
	if stat.IsDir() {
		return errors.New("validators.core_password_file points to a directory")
	}
	if stat.Size() <= 0 {
		return errors.New("validators.core_password_file is empty")
	}
	// On Unix-like systems enforce private file mode (no group/other access).
	// Windows permission bits are not reliable through os.FileMode, so ACL checks
	// are left to OS hardening policy.
	if runtime.GOOS != "windows" && (stat.Mode().Perm()&0o077) != 0 {
		return fmt.Errorf("validators.core_password_file must be private (chmod 600), got mode=%#o", stat.Mode().Perm())
	}
	return nil
}

// defaultCorePasswordFileCandidates returns the default core password file candidates.
func defaultCorePasswordFileCandidates(nodeID, nodePath string) []string {
	// `base` stores the value produced by this operation.
	base := strings.TrimSpace(nodePath)
	// `candidates` stores the value produced by this operation.
	candidates := make([]string, 0, 6)
	if base != "" {
		base = filepath.Clean(base)
		candidates = append(candidates,
			filepath.Join(base, "validator.pass"),
			filepath.Join(filepath.Dir(base), "validator.pass"),
		)
	}
	// `homeDir` and `err` store the error produced by this operation.
	if homeDir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(homeDir) != "" {
		// `id` stores the current position in the related collection.
		id := normalizeValidatorID(nodeID)
		if id != "" {
			candidates = append(candidates, filepath.Join(homeDir, ".msc-secrets", id+".pass"))
			candidates = append(candidates, filepath.Join(homeDir, ".msc-secrets", strings.ToLower(id)+".pass"))
		}
	}
	// `out` stores the result produced by this operation.
	out := make([]string, 0, len(candidates))
	// `seen` stores the value produced by this operation.
	seen := make(map[string]struct{}, len(candidates))
	// `candidate` tracks the current values while iterating.
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		candidate = filepath.Clean(candidate)
		// `ok` stores whether the related condition is satisfied.
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

// normalizeValidatorPasswordMode normalizes validator password mode.
func normalizeValidatorPasswordMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", validatorPasswordModeFileOrPrompt:
		return validatorPasswordModeFileOrPrompt
	case validatorPasswordModePromptOnly:
		return validatorPasswordModePromptOnly
	case validatorPasswordModeEnvOnly:
		return validatorPasswordModeEnvOnly
	default:
		return ""
	}
}

// configuredValidatorPasswordMode implements the configured validator password mode helper.
func configuredValidatorPasswordMode() string {
	// `mode` stores the value produced by this operation.
	mode := normalizeValidatorPasswordMode(ValidatorPasswordMode)
	if mode == "" || mode == validatorPasswordModeEnvOnly {
		return validatorPasswordModeFileOrPrompt
	}
	return mode
}

// resolveEffectiveValidatorPasswordMode implements the resolve effective validator password mode helper.
func resolveEffectiveValidatorPasswordMode(nodeID string, nodePath string) (string, error) {
	// `configMode` stores the configuration used by this operation.
	configMode := configuredValidatorPasswordMode()
	if !isCoreValidatorForSecurityPolicy(nodeID, nodePath) {
		return configMode, nil
	}
	// `raw` and `ok` store whether the related condition is satisfied.
	raw, ok := os.LookupEnv(validatorPasswordModeEnv)
	if !ok || strings.TrimSpace(raw) == "" {
		return configMode, nil
	}
	// `mode` stores the value produced by this operation.
	mode := normalizeValidatorPasswordMode(raw)
	if mode != validatorPasswordModeEnvOnly {
		return "", fmt.Errorf("env_password_mode_invalid: %s must be 'env_only' or empty", validatorPasswordModeEnv)
	}
	return validatorPasswordModeEnvOnly, nil
}

// getValidatorPasswordWithSource implements the get validator password with source helper.
func getValidatorPasswordWithSource(nodeID string, nodePath string) (string, bool, error) {
	// `coreNode` stores the value produced by this operation.
	coreNode := isCoreValidatorForSecurityPolicy(nodeID, nodePath)
	// `effectiveMode` and `modeErr` store the error produced by this operation.
	effectiveMode, modeErr := resolveEffectiveValidatorPasswordMode(nodeID, nodePath)
	if modeErr != nil {
		setValidatorSecretRuntime(nodeID, configuredValidatorPasswordMode(), "blocked")
		return "", false, modeErr
	}
	setValidatorSecretRuntime(nodeID, effectiveMode, "")

	if effectiveMode == validatorPasswordModeEnvOnly {
		// `envPass` and `ok` store whether the related condition is satisfied.
		envPass, ok := os.LookupEnv(validatorPasswordEnv)
		if !ok || strings.TrimSpace(envPass) == "" {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", true, errors.New("env_only_password_missing: set MSC_VALIDATOR_PASSWORD for env_only mode")
		}
		setCoreEnvPasswordBlocked(nodeID, false)
		if coreNode {
			log.Printf("[CORE-SECRET-POLICY] mode=env_only source=env allowed=true node=%s", normalizeValidatorID(nodeID))
		}
		setValidatorSecretRuntime(nodeID, effectiveMode, "env")
		return strings.TrimSpace(envPass), true, nil
	}

	// Allow non-interactive runs.
	if envPass, ok := os.LookupEnv(validatorPasswordEnv); ok {
		if effectiveMode == validatorPasswordModePromptOnly {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			if coreNode {
				setCoreEnvPasswordBlocked(nodeID, true)
				log.Printf("[CORE-SECRET-POLICY] mode=prompt_only source=env blocked=true node=%s", normalizeValidatorID(nodeID))
			}
			return "", true, errors.New("env_blocked_core_prompt_only: env password is blocked when validators.password_mode=prompt_only")
		}
		if coreNode && !ValidatorCoreEnvPasswordAllowed {
			setCoreEnvPasswordBlocked(nodeID, true)
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			log.Printf("[CORE-SECRET-POLICY] mode=%s source=env blocked=true node=%s", effectiveMode, normalizeValidatorID(nodeID))
			return "", true, fmt.Errorf("%s is blocked for core validators (validators.core_env_password_allowed=false)", validatorPasswordEnv)
		}
		if !IsTestnet && !ValidatorAllowEnvPasswordInProduction {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", true, fmt.Errorf("%s is disabled in production (validators.allow_env_password_in_production=false)", validatorPasswordEnv)
		}
		// `pass` stores the value produced by this operation.
		pass := strings.TrimSpace(envPass)
		if pass == "" {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", true, errors.New("validator password env is empty")
		}
		setCoreEnvPasswordBlocked(nodeID, false)
		setValidatorSecretRuntime(nodeID, effectiveMode, "env")
		return pass, true, nil
	}
	if effectiveMode == validatorPasswordModePromptOnly {
		if coreNode {
			if strings.TrimSpace(ValidatorCorePasswordFile) != "" {
				log.Printf("[CORE-SECRET-POLICY] mode=prompt_only source=file ignored=true node=%s", normalizeValidatorID(nodeID))
			} else {
				// `passwordFile` tracks the current values while iterating.
				for _, passwordFile := range defaultCorePasswordFileCandidates(nodeID, nodePath) {
					// `err` stores the error produced by this operation.
					if _, err := os.Stat(passwordFile); err == nil {
						log.Printf("[CORE-SECRET-POLICY] mode=prompt_only source=file ignored=true node=%s", normalizeValidatorID(nodeID))
						break
					}
				}
			}
		}
		setCoreEnvPasswordBlocked(nodeID, false)
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, errors.New("prompt_required_non_interactive")
		}
		// `passBytes` and `err` store the error produced by this operation.
		passBytes, err := ReadPassword("Validator password: ")
		if err != nil {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, fmt.Errorf("failed to read validator password: %w", err)
		}
		defer ZeroMemory(passBytes)
		// `pass` stores the value produced by this operation.
		pass := strings.TrimSpace(string(passBytes))
		if pass == "" {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, errors.New("validator password cannot be empty")
		}
		setValidatorSecretRuntime(nodeID, effectiveMode, "prompt")
		return pass, false, nil
	}

	if coreNode && strings.TrimSpace(ValidatorCorePasswordFile) != "" {
		// `passwordFile` stores the value produced by this operation.
		passwordFile := strings.TrimSpace(ValidatorCorePasswordFile)
		// `err` stores the error produced by this operation.
		if err := validateCorePasswordFile(passwordFile); err != nil {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, err
		}
		// `passBytes` and `err` store the error produced by this operation.
		passBytes, err := os.ReadFile(passwordFile)
		if err != nil {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, fmt.Errorf("failed to read validators.core_password_file: %w", err)
		}
		// `pass` stores the value produced by this operation.
		pass := strings.TrimSpace(string(passBytes))
		if pass == "" {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, errors.New("validators.core_password_file is empty")
		}
		setCoreEnvPasswordBlocked(nodeID, false)
		setValidatorSecretRuntime(nodeID, effectiveMode, "file")
		return pass, false, nil
	}
	if coreNode {
		// `passwordFile` tracks the current values while iterating.
		for _, passwordFile := range defaultCorePasswordFileCandidates(nodeID, nodePath) {
			// `err` stores the error produced by this operation.
			if _, err := os.Stat(passwordFile); err != nil {
				continue
			}
			// `err` stores the error produced by this operation.
			if err := validateCorePasswordFile(passwordFile); err != nil {
				setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
				return "", false, err
			}
			// `passBytes` and `err` store the error produced by this operation.
			passBytes, err := os.ReadFile(passwordFile)
			if err != nil {
				setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
				return "", false, fmt.Errorf("failed to read auto core password file %s: %w", passwordFile, err)
			}
			// `pass` stores the value produced by this operation.
			pass := strings.TrimSpace(string(passBytes))
			if pass == "" {
				setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
				return "", false, fmt.Errorf("auto core password file is empty: %s", passwordFile)
			}
			setCoreEnvPasswordBlocked(nodeID, false)
			setValidatorSecretRuntime(nodeID, effectiveMode, "file")
			return pass, false, nil
		}
	}
	setCoreEnvPasswordBlocked(nodeID, false)

	if !term.IsTerminal(int(os.Stdin.Fd())) {
		setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
		return "", false, errors.New("prompt_required_non_interactive")
	}
	// `passBytes` and `err` store the error produced by this operation.
	passBytes, err := ReadPassword("Validator password: ")
	if err != nil {
		setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
		return "", false, fmt.Errorf("failed to read validator password: %w", err)
	}
	defer ZeroMemory(passBytes)

	// `pass` stores the value produced by this operation.
	pass := strings.TrimSpace(string(passBytes))
	if pass == "" {
		setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
		return "", false, errors.New("validator password cannot be empty")
	}
	setValidatorSecretRuntime(nodeID, effectiveMode, "prompt")
	return pass, false, nil
}

// validateNewValidatorPassword validates new validator password.
func validateNewValidatorPassword(pass string) error {
	// `p` stores the value produced by this operation.
	p := strings.TrimSpace(pass)
	if p == "" {
		return errors.New("validator password cannot be empty")
	}
	// `weak` stores the value produced by this operation.
	weak := map[string]struct{}{
		"m":        {},
		"password": {},
		"123456":   {},
		"admin":    {},
		"test":     {},
	}
	// `bad` stores the value produced by this operation.
	if _, bad := weak[strings.ToLower(p)]; bad {
		return errors.New("weak validator password blocked; choose a strong passphrase")
	}
	if len(p) < 8 {
		return errors.New("validator password too short; minimum length is 8")
	}
	return nil
}

// getValidatorPassword implements the get validator password helper.
func getValidatorPassword() (string, error) {
	// `pass` and `err` store the error produced by this operation.
	pass, _, err := getValidatorPasswordWithSource("", "")
	return pass, err
}

// ReadPassword reads password.
func ReadPassword(prompt string) ([]byte, error) {
	fmt.Print(prompt)
	defer fmt.Println()

	// `pass` and `err` store the error produced by this operation.
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}
	return pass, nil
}

// ParseExecutionResult parses execution result.
func ParseExecutionResult(entry string) (
	signer string,
	resultHash string,
	ok bool,
) {

	if entry == "" {
		return "", "", false
	}

	// Expected format:
	// signer|resultHash
	parts := strings.Split(entry, "|")
	if len(parts) != 2 {
		if DebugConsensus {
			fmt.Printf(
				"⚠️ Invalid execution result format: %s\n",
				entry,
			)
		}
		return "", "", false
	}

	signer = parts[0]
	resultHash = parts[1]

	// Basic sanity checks
	if signer == "" || resultHash == "" {
		return "", "", false
	}

	// Result hash must be hex
	if _, err := hex.DecodeString(resultHash); err != nil {
		if DebugConsensus {
			fmt.Printf(
				"⚠️ Invalid result hash encoding: %v\n",
				err,
			)
		}
		return "", "", false
	}

	return signer, resultHash, true
}

// VerifyBlockConsensus verifies block consensus.
func (n *Node) VerifyBlockConsensus(
	block Block,
	height int,
) bool {

	// =====================================================
	// 🔒 1️⃣ MINIMUM EXECUTOR REQUIREMENT
	// =====================================================
	executors := n.GetActiveExecutors()
	if len(executors) < GlobalConfig.MinExecutors {
		if DebugConsensus {
			fmt.Println("❌ Consensus rejected: insufficient executors")
		}
		return false
	}

	// =====================================================
	// 🧠 2️⃣ COLLECT EXECUTION RESULTS
	// =====================================================
	resultCount := make(map[string]int)

	// `res` tracks the result produced by this operation.
	for _, res := range block.ExecutionResults {

		// Executor must be active
		if !IsExecutorActive(res.Signer, height) {
			if DebugConsensus {
				fmt.Println("❌ Invalid executor:", res.Signer)
			}
			return false
		}

		// Result hash must exist
		if res.ResultHash == "" {
			return false
		}

		// Tie result to this block
		if res.Height != block.ID || res.BlockHash != block.BlockHash {
			return false
		}

		resultCount[res.ResultHash]++
	}

	// =====================================================
	// 🔐 3️⃣ RESULT AGREEMENT CHECK
	// =====================================================
	for hash, count := range resultCount {

		// Strict majority agreement
		if count >= GlobalConfig.MinExecutors {
			if DebugConsensus {
				fmt.Printf(
					"✅ Consensus achieved via result hash %s (%d executors)\n",
					hash[:8],
					count,
				)
			}
			return true
		}
	}

	if DebugConsensus {
		fmt.Println("❌ Consensus failed: no matching execution result")
	}

	return false
}

// GetConnectedValidatorCount returns connected validator count.
func (n *Node) GetConnectedValidatorCount(height int) int {
	// `validators` stores whether the related condition is satisfied.
	validators := n.GetConsensusValidators(height)
	if len(validators) == 0 {
		return 0
	}

	// `now` stores the value produced by this operation.
	now := time.Now()
	// `count` stores the measured quantity used by this operation.
	count := 0

	n.validatorMu.RLock()
	defer n.validatorMu.RUnlock()

	// `v` tracks the current values while iterating.
	for _, v := range validators {
		if v == n.ID {
			count++
			continue
		}
		// `st` and `ok` store whether the related condition is satisfied.
		if st, ok := n.validatorStatus[v]; ok {
			if now.Sub(st.LastSeen) <= 90*time.Second {
				count++
			}
		}
	}

	if count == 0 {
		return len(validators)
	}
	return count
}

// VerifyExecutionQuorum verifies execution quorum.
func (n *Node) VerifyExecutionQuorum(block Block, height int) bool {
	// `received` stores the value produced by this operation.
	received := len(block.ExecutionResults)
	if received == 0 {
		return false
	}

	// `total` stores the measured quantity used by this operation.
	total := n.countLiveValidators()
	if total == 0 {
		total = len(n.GetConsensusValidators(height))
	}
	// `required` stores the request data being processed.
	required := execQuorumRequired(total)
	if required < 1 {
		required = 1
	}
	if DebugConsensus {
		fmt.Printf("🔎 Execution quorum target: required=%d total=%d\n", required, total)
	}

	// `resultCount` stores the measured quantity used by this operation.
	resultCount := make(map[string]int)
	// `res` tracks the result produced by this operation.
	for _, res := range block.ExecutionResults {
		if res.ResultHash == "" || res.Signer == "" {
			continue
		}
		if res.Height != block.ID || res.BlockHash != block.BlockHash {
			continue
		}
		if res.TxMerkle != "" && block.MempoolRoot != res.TxMerkle {
			continue
		}
		resultCount[res.ResultHash]++
	}

	// `hash` and `count` track the digest used to identify or verify the related data.
	for hash, count := range resultCount {
		if count >= required {
			if DebugConsensus {
				fmt.Printf("✅ Execution quorum via hash %s (%d/%d total=%d)\n", hash[:8], count, received, total)
			}
			return true
		}
	}

	if DebugConsensus {
		fmt.Printf("❌ Execution quorum failed (need %d of %d total=%d)\n", required, received, total)
	}
	return false
}

// ExecuteAndAttachResult implements the execute and attach result helper.
func (n *Node) ExecuteAndAttachResult(
	block *Block,
	task Task,
) {

	// =====================================================
	// 1️⃣ EXECUTE TASK LOCALLY (DETERMINISTIC)
	// =====================================================
	result := ExecuteTask(task)

	// =====================================================
	// 2️⃣ HASH RESULT (NOT BLOCK)
	// =====================================================
	resultBytes := []byte(fmt.Sprintf("%d", result))
	// `hash` stores the digest used to identify or verify the related data.
	hash := sha256.Sum256(resultBytes)

	// `resultHash` stores the digest used to identify or verify the related data.
	resultHash := hex.EncodeToString(hash[:])

	// =====================================================
	// 3️⃣ ATTACH EXECUTION RESULT TO BLOCK
	// =====================================================
	exec := ExecutionResult{
		Height:     block.ID,
		BlockHash:  block.BlockHash,
		Signer:     n.ID,       // node A / B / C / normal node
		ResultHash: resultHash, // proof of execution
	}

	block.ExecutionResults = append(
		block.ExecutionResults,
		exec,
	)

	if DebugConsensus {
		fmt.Printf(
			"🧮 Task executed | node=%s result=%d hash=%s\n",
			n.ID,
			result,
			resultHash[:12],
		)
	}
}

// GetActiveExecutors returns active executors.
func (n *Node) GetActiveExecutors() []string {
	// `now` stores the value produced by this operation.
	now := time.Now()

	n.peerMu.RLock()
	defer n.peerMu.RUnlock()

	// `executors` stores the value produced by this operation.
	executors := []string{}

	// 🔹 Include self
	executors = append(executors, n.ID)

	// 🔹 Include all connected peers
	for _, p := range n.PeersLibp2p {
		// `id` stores the current position in the related collection.
		id := p.String()

		// Basic liveness gate
		peerLastSeenMu.Lock()
		// `last` and `ok` store whether the related condition is satisfied.
		last, ok := PeerLastSeen[id]
		peerLastSeenMu.Unlock()
		if !ok {
			continue
		}

		if now.Sub(last) > 90*time.Second {
			continue
		}

		executors = append(executors, id)
	}

	sort.Strings(executors)

	if DebugConsensus {
		fmt.Printf(
			"🧠 ACTIVE EXECUTORS %d → %v\n",
			len(executors),
			executors,
		)
	}

	return executors
}

// CanMintDeterministic reports whether mint deterministic is allowed.
func CanMintDeterministic(
	currentTotal int64,
	amount int64,
	maxSupply int64,
) bool {
	if amount <= 0 {
		return false
	}
	return currentTotal+amount <= maxSupply
}

// ExecuteMintTask implements the execute mint task helper.
func ExecuteMintTask(
	currentTotal int64,
	to string,
	amount int64,
) (MintResult, bool) {

	if !CanMintDeterministic(
		currentTotal,
		amount,
		FixedTotalSupply,
	) {
		return MintResult{}, false
	}

	return MintResult{
		To:             to,
		Amount:         amount,
		NewTotalSupply: currentTotal + amount,
	}, true
}

// ApplyMintResult applies mint result.
func ApplyMintResult(
	ledger *Ledger,
	result MintResult,
) {
	ledger.Balances[result.To] += int(result.Amount)
	storeTotalMintedMSC(result.NewTotalSupply)
}

// RecalculateTotalSupplyStrict implements the recalculate total supply strict helper.
func RecalculateTotalSupplyStrict(
	ledger Ledger,
) (int64, bool) {

	// `total` stores the measured quantity used by this operation.
	var total int64

	// `addr` and `bal` track the address used by this operation.
	for addr, bal := range ledger.Balances {

		// ❌ Negative balance = corrupted state
		if bal < 0 {
			if DebugConsensus {
				fmt.Printf(
					"❌ Invalid negative balance detected | %s = %d\n",
					addr, bal,
				)
			}
			return 0, false
		}

		total += int64(bal)
	}

	return total, true
}

// VerifyTotalSupply verifies total supply.
func VerifyTotalSupply(
	ledger Ledger,
	expectedMax int64,
) bool {

	// `total` and `ok` store whether the related condition is satisfied.
	total, ok := RecalculateTotalSupplyStrict(ledger)
	if !ok {
		return false
	}

	// 🔒 Hard cap enforcement
	if total > expectedMax {
		if DebugConsensus {
			fmt.Printf(
				"❌ Supply overflow | total=%d max=%d\n",
				total, expectedMax,
			)
		}
		return false
	}

	return true
}

// VerifySignature verifies signature.
func VerifySignature(
	pub ed25519.PublicKey,
	payload []byte,
	signature []byte,
) bool {

	// 🔒 Hard guards
	if len(pub) != ed25519.PublicKeySize {
		return false
	}
	if len(signature) != ed25519.SignatureSize {
		return false
	}

	// 🔐 Single canonical hash
	hash := sha256.Sum256(payload)

	// ✅ Cryptographic truth
	return ed25519.Verify(pub, hash[:], signature)
}

// Sign implements the sign helper.
func Sign(priv ed25519.PrivateKey, payload []byte) []byte {
	return ed25519.Sign(priv, payload)
}

// txPayloadWithEnvelope implements the tx payload with envelope helper.
func txPayloadWithEnvelope(tx Transaction, includeExtendedReservedFields bool) []byte {
	// `buf` stores the value produced by this operation.
	buf := bytes.NewBuffer(nil)

	buf.WriteString(tx.From)
	buf.WriteByte(0)

	buf.WriteString(tx.To)
	buf.WriteByte(0)

	buf.WriteString(normalizeCoin(tx.Coin))
	buf.WriteByte(0)

	_ = binary.Write(buf, binary.BigEndian, int64(tx.Amount))
	_ = binary.Write(buf, binary.BigEndian, int64(tx.Fee))
	_ = binary.Write(buf, binary.BigEndian, int64(tx.Nonce))
	_ = binary.Write(buf, binary.BigEndian, int64(tx.Expiry))
	_ = binary.Write(buf, binary.BigEndian, int64(tx.StakeEpochs))
	if tx.Type == TxStake {
		// `normalized` stores the value produced by this operation.
		if normalized := normalizeConsensusPubKeyHex(tx.ValidatorPubKey); normalized != "" {
			buf.WriteString(normalized)
			buf.WriteByte(0)
		}
	}
	// Preserve the historical zero-value payload layout for existing native
	// transaction signatures while making the removed VM slots impossible to
	// populate through Transaction.
	_ = binary.Write(buf, binary.BigEndian, int64(0))
	buf.WriteByte(0)
	buf.WriteByte(0)
	if includeExtendedReservedFields {
		buf.WriteByte(0)
		buf.WriteByte(0)
	}
	if tx.Type == TxDTL {
		buf.WriteString(strings.TrimSpace(tx.DTLTxType))
		buf.WriteByte(0)
		buf.WriteString(strings.TrimSpace(tx.DTLTokenID))
		buf.WriteByte(0)
		buf.WriteString(strings.TrimSpace(tx.DTLPayload))
		buf.WriteByte(0)
		buf.WriteString(strings.TrimSpace(tx.DTLGovernanceCert))
		buf.WriteByte(0)
	}
	if tx.Type == TxValidatorUpdate {
		buf.WriteString(validatorUpdateCertPayloadForTx(tx.ValidatorUpdateCert))
		buf.WriteByte(0)
	}

	buf.WriteString(protocolChainID())
	buf.WriteByte(0)

	buf.WriteByte(byte(tx.Type))

	return buf.Bytes()
}

// TxPayload implements the tx payload helper.
func TxPayload(tx Transaction) []byte {
	return txPayloadWithEnvelope(tx, true)
}

// TxPayloadLegacy preserves the shorter historical reserved-field layout used
// by older wallet UIs.
func TxPayloadLegacy(tx Transaction) []byte {
	return txPayloadWithEnvelope(tx, false)
}

// ComputeTxID returns the deterministic tx_id = SHA256(tx_bytes).
func ComputeTxID(tx Transaction) string {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(TxPayload(tx))
	return hex.EncodeToString(sum[:])
}

// ComputeTxIDLegacy hashes the historical payload layout for compatibility.
func ComputeTxIDLegacy(tx Transaction) string {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(TxPayloadLegacy(tx))
	return hex.EncodeToString(sum[:])
}

// TxOrderKey returns deterministic ordering key = SHA256(epoch || tx_id).
func TxOrderKey(epoch uint64, tx Transaction) string {
	// `txID` stores the transaction data handled by this operation.
	txID := ComputeTxID(tx)
	// `buf` stores the value produced by this operation.
	buf := make([]byte, 8+len(txID))
	binary.BigEndian.PutUint64(buf[:8], epoch)
	copy(buf[8:], []byte(txID))
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// IsExecutorActive reports whether executor active is true.
func IsExecutorActive(executorID string, height int) bool {

	peerLastSeenMu.Lock()
	// `last` and `ok` store whether the related condition is satisfied.
	last, ok := PeerLastSeen[executorID]
	peerLastSeenMu.Unlock()

	if !ok {
		return false
	}

	// Hard liveness window
	if time.Since(last) > 90*time.Second {
		return false
	}

	return true
}

// canonicalMisbehaviorReason returns canonical misbehavior reason.
func canonicalMisbehaviorReason(reason string) string {
	// `key` stores the key used to access the related value.
	key := strings.ToLower(strings.TrimSpace(reason))
	switch key {
	case "exec_mismatch_repeat", "exec_mismatch_late":
		return "exec_mismatch"
	case "exec_equivocation_signed":
		return "exec_equivocation"
	case "double_vote", "double_commit":
		return "double_sign"
	default:
		return key
	}
}

// slashingThresholdForReason implements the slashing threshold for reason helper.
func slashingThresholdForReason(reason string) int {
	switch canonicalMisbehaviorReason(reason) {
	case "double_proposal", "double_sign", "exec_equivocation":
		return 1
	case "invalid_proposer", "exec_mismatch":
		return 2
	default:
		return 3
	}
}

// RecordMisbehavior canonicalizes validator evidence, dedupes it, persists it, and applies slash thresholds.
func (n *Node) RecordMisbehavior(
	validator string,
	reason string,
	height int,
	blockHash string,
) {
	// `reasonKey` stores the key used to access the related value.
	reasonKey := canonicalMisbehaviorReason(reason)
	if reasonKey == "" || height <= 0 {
		return
	}

	validator = normalizeValidatorID(validator)
	if validator == "" {
		return
	}
	// `entry` stores the value produced by this operation.
	entry := SlashEvidence{
		Validator:   validator,
		ValidatorID: normalizeValidatorID(validator),
		Reason:      reasonKey,
		Height:      uint64(height), // ✅ FIX
		BlockHash:   blockHash,
		Timestamp:   time.Now().Unix(),
	}

	n.misbehaviorMu.Lock()
	if n.MisbehaviorLog == nil {
		n.MisbehaviorLog = make(map[string][]SlashEvidence)
	}
	// `existing` stores the value produced by this operation.
	existing := n.MisbehaviorLog[validator]
	// `entry` and `entryKey` store the key used to access the related value.
	entry, entryKey := normalizeSlashEvidenceForStore(entry)
	if entryKey == "" {
		n.misbehaviorMu.Unlock()
		return
	}
	// Production evidence dedupe spans the full retained log, not only the
	// recent tail, so startup replay and delayed gossip cannot cascade slashes.
	for _, ev := range existing {
		// `existingKey` stores the key used to access the related value.
		_, existingKey := normalizeSlashEvidenceForStore(ev)
		if existingKey != "" && existingKey == entryKey {
			n.misbehaviorMu.Unlock()
			return
		}
	}
	// `blockHashKey` stores the block data handled by this operation.
	blockHashKey := strings.ToLower(strings.TrimSpace(blockHash))
	// De-duplicate repeated reports for the same validator/reason/height/hash.
	// This prevents slash cascades from re-gossip/re-verify loops.
	for i := len(existing) - 1; i >= 0 && i >= len(existing)-32; i-- {
		// `ev` stores the value produced by this operation.
		ev := existing[i]
		if ev.Height != uint64(height) {
			continue
		}
		if canonicalMisbehaviorReason(ev.Reason) != reasonKey {
			continue
		}
		if strings.ToLower(strings.TrimSpace(ev.BlockHash)) != blockHashKey {
			continue
		}
		n.misbehaviorMu.Unlock()
		return
	}

	// ✅ append to slice INSIDE map
	n.MisbehaviorLog[validator] = append(existing, entry)
	n.misbehaviorMu.Unlock()
	n.persistMisbehaviorEvidence(entry)
	n.emitConsensusTelemetry(consensusTelemetryEvent{
		Type:      "slash_evidence_recorded",
		Reason:    reasonKey,
		Height:    uint64(height),
		BlockHash: blockHash,
		Fields: map[string]interface{}{
			"validator": validator,
		},
	})

	if DebugConsensus {
		fmt.Printf(
			"🚨 MISBEHAVIOR | validator=%s height=%d reason=%s\n",
			ShortID(validator),
			height,
			reasonKey,
		)
	}

	// escalation hook
	n.CheckSlashingThreshold(validator, reasonKey)
}

// CheckSlashingThreshold implements the check slashing threshold helper.
func (n *Node) CheckSlashingThreshold(validator string, reason string) {
	// `reasonKey` stores the key used to access the related value.
	reasonKey := canonicalMisbehaviorReason(reason)
	if reasonKey == "" {
		return
	}

	// `recentWindow` defines the constant value used by this package.
	const recentWindow = uint64(128)
	// `chainHeight` stores the value used by this operation.
	var chainHeight uint64
	if n != nil && n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}

	n.misbehaviorMu.Lock()
	// `entries` stores the value produced by this operation.
	entries := append([]SlashEvidence(nil), n.MisbehaviorLog[validator]...)
	n.misbehaviorMu.Unlock()
	if len(entries) == 0 {
		return
	}

	// `reasonCount` stores the measured quantity used by this operation.
	reasonCount := 0
	// `latestTrigger` stores the value used by this operation.
	var latestTrigger SlashEvidence
	// `i` stores the current position in the related collection.
	for i := len(entries) - 1; i >= 0; i-- {
		// `ev` stores the value produced by this operation.
		ev := entries[i]
		if canonicalMisbehaviorReason(ev.Reason) != reasonKey {
			continue
		}
		if chainHeight > 0 && ev.Height > 0 && chainHeight > ev.Height && chainHeight-ev.Height > recentWindow {
			continue
		}
		reasonCount++
		if latestTrigger.Height == 0 ||
			ev.Height > latestTrigger.Height ||
			(ev.Height == latestTrigger.Height && ev.Timestamp > latestTrigger.Timestamp) {
			latestTrigger = ev
		}
	}

	// `threshold` stores the value produced by this operation.
	threshold := slashingThresholdForReason(reasonKey)
	if reasonCount < threshold {
		return
	}

	if DebugConsensus {
		fmt.Printf(
			"SLASH validator=%s reason=%s count=%d threshold=%d\n",
			ShortID(validator),
			reasonKey,
			reasonCount,
			threshold,
		)
	}

	// `actionKey` stores the key used to access the related value.
	actionKey := slashActionKey(validator, reasonKey, latestTrigger)
	if actionKey != "" {
		slashActionApplyMu.Lock()
		defer slashActionApplyMu.Unlock()
		if n.slashActionApplied(actionKey) {
			return
		}
	}

	// Slash validator (identity + execution penalty), then persist the action
	// marker so startup replay can recover evidence without burning twice.
	n.slashValidatorForReason(validator, reasonKey, latestTrigger.Height)
	if actionKey != "" {
		n.persistSlashAction(actionKey, validator, reasonKey, latestTrigger)
		n.emitConsensusTelemetry(consensusTelemetryEvent{
			Type:      "slash_action_applied",
			Reason:    reasonKey,
			Height:    latestTrigger.Height,
			BlockHash: latestTrigger.BlockHash,
			Fields: map[string]interface{}{
				"validator": validator,
				"action":    actionKey,
			},
		})
	}
}
