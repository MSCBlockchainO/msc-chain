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
	keyEncryptionVersion = 2
	keyEncryptionKDF     = "argon2id"
	keyEncryptionAAD     = "MSC|encrypted-key|v2"

	defaultArgon2Time    uint32 = 3
	defaultArgon2Memory  uint32 = 64 * 1024 // KiB (64 MiB)
	defaultArgon2Threads uint8  = 2

	encryptedKeySaltSize = 16

	validatorPasswordEnv       = "MSC_VALIDATOR_PASSWORD"
	validatorPasswordModeEnv   = "MSC_VALIDATOR_PASSWORD_MODE"
	coreKeyCreateOverrideEnv   = "MSC_ALLOW_CORE_VALIDATOR_KEY_CREATE"
	validatorKeyCreateOverride = "MSC_ALLOW_VALIDATOR_KEY_CREATE"

	validatorUnlockMaxAttempts = 3
	validatorUnlockRetryDelay  = 700 * time.Millisecond

	validatorKeyMetaFileName           = "validator.meta.json"
	validatorKeyBackupManifestFileName = "validator.backup.manifest.json"
	validatorKeyBackupFileName         = "validator.sec.bak"
	validatorPublicFileName            = "validator.pub"
	validatorFingerprintLockFileName   = "fingerprint.lock"
)

const (
	validatorPasswordModeFileOrPrompt = "file_or_prompt"
	validatorPasswordModePromptOnly   = "prompt_only"
	validatorPasswordModeEnvOnly      = "env_only"
)

func IsFinalized(
	expectedStateRoot string,
	computedStateRoot string,
) bool {
	return expectedStateRoot == computedStateRoot
}

func DecodePublicKey(hexKey string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, err
	}
	return ed25519.PublicKey(b), nil
}
func DecodeSignature(hexSig string) ([]byte, error) {
	return hex.DecodeString(hexSig)
}
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
		ChainID:   ChainID,
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
		ChainID:         ChainID,
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

	priv, err := DecryptPrivateKey(w, password)
	if err != nil {
		return Transaction{}, fmt.Errorf("wallet decryption failed")
	}
	defer ZeroMemory(priv)
	maybeUpgradeSecureWalletEncryption(w, priv, password)

	nonce := currentNonce + 1

	tx := Transaction{
		From:      w.Address,
		To:        validatorID,
		Amount:    amount,
		Nonce:     nonce,
		PublicKey: w.PublicKey,
		Fee:       ComputeTxFee(amount),
		Expiry:    time.Now().Add(2 * time.Minute).Unix(),
		ChainID:   ChainID,
		Coin:      normalizeCoin(coin),
		Type:      TxUnstake,
	}

	payload := TxPayload(tx)
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

	priv, err := DecryptPrivateKey(w, password)
	if err != nil {
		return Transaction{}, fmt.Errorf("wallet decryption failed")
	}
	defer ZeroMemory(priv)
	maybeUpgradeSecureWalletEncryption(w, priv, password)

	if action != "add" && action != "remove" {
		return Transaction{}, fmt.Errorf("invalid action")
	}
	if validatorID == "" {
		return Transaction{}, fmt.Errorf("missing validator id")
	}

	nonce := currentNonce + 1
	target := validatorUpdateAddPrefix + validatorID
	if action == "remove" {
		target = validatorUpdateRemovePrefix + validatorID
	}

	tx := Transaction{
		From:      w.Address,
		To:        target,
		Amount:    0,
		Nonce:     nonce,
		PublicKey: w.PublicKey,
		Fee:       0,
		Expiry:    time.Now().Add(2 * time.Minute).Unix(),
		ChainID:   ChainID,
		Coin:      normalizeCoin(coin),
		Type:      TxValidatorUpdate,
	}

	payload := TxPayload(tx)
	sig := Sign(priv, payload)
	tx.Signature = hex.EncodeToString(sig)

	tx.ID = ComputeTxID(tx)

	return tx, nil
}

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
	if action != "add" && action != "remove" {
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

	payload := validatorUpdateCertSigningPayload(
		ChainID,
		action,
		validatorID,
		parentRegistryHash,
		proposalNonce,
		expiryHeight,
	)
	sig := Sign(priv, payload)
	return ValidatorUpdateCertSignature{
		SignerID: signerID,
		SigHex:   strings.ToLower(hex.EncodeToString(sig)),
	}, nil
}

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
	certCopy := *cert
	normalizeValidatorUpdateCert(&certCopy)
	tx.ValidatorUpdateCert = &certCopy
	tx.Signature = strings.ToLower(hex.EncodeToString(Sign(priv, TxPayload(*tx))))
	tx.ID = ComputeTxID(*tx)
	return nil
}

func WalletToRPC(w Wallet) map[string]string {

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
	pub, _, _ := ed25519.GenerateKey(cryptorand.Reader)
	address := AddressFromPublicKey(pub)
	return Wallet{
		PublicKey: pub,
		Address:   address,
	}
}
func walletPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".msc", "wallet.json")
}

func SaveWallet(w Wallet) error {
	os.MkdirAll(filepath.Dir(walletPath()), 0700)
	data, _ := json.MarshalIndent(w, "", "  ")
	return os.WriteFile(walletPath(), data, 0600)
}

func LoadWallet() (Wallet, error) {
	data, err := os.ReadFile(walletPath())
	if err != nil {
		return Wallet{}, err
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return Wallet{}, err
	}
	return Wallet{
		Address: raw["address"],
	}, nil
}

func ensurePrivateDirectory(path string) error {
	if path == "" {
		return errors.New("empty directory path")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func ensurePrivateFilePermissions(path string) error {
	if path == "" {
		return errors.New("empty file path")
	}
	if runtime.GOOS == "windows" {
		// Windows uses ACL semantics; POSIX mode-bit checks are not authoritative.
		return nil
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("permissions too open on %s: %o (expected 600)", path, fi.Mode().Perm())
	}
	return nil
}

func writePrivateFile(path string, data []byte) error {
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func isEncryptedKeyV2(k EncryptedKey) bool {
	return k.Version >= keyEncryptionVersion && strings.EqualFold(strings.TrimSpace(k.KDF), keyEncryptionKDF)
}

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

func decryptPrivateKeyLegacy(password string, salt []byte, nonce []byte, ciphertext []byte) (ed25519.PrivateKey, error) {
	key := sha256.Sum256(append([]byte(password), salt...))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("invalid password or corrupted wallet")
	}
	if len(plain) != ed25519.PrivateKeySize {
		ZeroMemory(plain)
		return nil, errors.New("invalid private key length")
	}
	out := make([]byte, len(plain))
	copy(out, plain)
	ZeroMemory(plain)
	return ed25519.PrivateKey(out), nil
}

func decryptPrivateKeyV2(enc EncryptedKey, password string, salt []byte, nonce []byte, ciphertext []byte) (ed25519.PrivateKey, error) {
	key := deriveKeyArgon2ID(password, salt, enc.Argon2Time, enc.Argon2MemoryKiB, enc.Argon2Threads)
	defer ZeroMemory(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(keyEncryptionAAD))
	if err != nil {
		return nil, errors.New("invalid password or corrupted wallet")
	}
	if len(plain) != ed25519.PrivateKeySize {
		ZeroMemory(plain)
		return nil, errors.New("invalid private key length")
	}
	out := make([]byte, len(plain))
	copy(out, plain)
	ZeroMemory(plain)
	return ed25519.PrivateKey(out), nil
}

func maybeUpgradeSecureWalletEncryption(w SecureWallet, priv ed25519.PrivateKey, password string) {
	if isEncryptedKeyV2(w.Crypto) {
		return
	}
	enc, err := EncryptPrivateKey(priv, password)
	if err != nil {
		return
	}
	w.Crypto = enc
	if err := SaveSecureWallet(w); err == nil && DebugConsensus {
		fmt.Println("🔐 Secure wallet encryption upgraded to argon2id (v2)")
	}
}

func DecryptPrivateKey(w SecureWallet, password string) (ed25519.PrivateKey, error) {
	salt, err := hex.DecodeString(w.Crypto.Salt)
	if err != nil {
		return nil, err
	}

	nonce, err := hex.DecodeString(w.Crypto.Nonce)
	if err != nil {
		return nil, err
	}

	ciphertext, err := hex.DecodeString(w.Crypto.Ciphertext)
	if err != nil {
		return nil, err
	}
	if isEncryptedKeyV2(w.Crypto) {
		return decryptPrivateKeyV2(w.Crypto, password, salt, nonce, ciphertext)
	}
	return decryptPrivateKeyLegacy(password, salt, nonce, ciphertext)
}

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

func SecureWalletPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".msc", "secure_wallet.json")
}
func SaveSecureWallet(w SecureWallet) error {
	if err := ensurePrivateDirectory(filepath.Dir(SecureWalletPath())); err != nil {
		return err
	}
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(SecureWalletPath(), data)
}
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
	salt := make([]byte, encryptedKeySaltSize)
	if _, err := cryptorand.Read(salt); err != nil {
		return EncryptedKey{}, err
	}
	key := deriveKeyArgon2ID(password, salt, defaultArgon2Time, defaultArgon2Memory, defaultArgon2Threads)
	defer ZeroMemory(key)

	block, err := aes.NewCipher(key)
	if err != nil {
		return EncryptedKey{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedKey{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := cryptorand.Read(nonce); err != nil {
		return EncryptedKey{}, err
	}
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

	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return SecureWallet{}, nil, err
	}

	seed := bip39.NewSeed(mnemonic, password)
	pub, priv, hd, err := deriveHDKeypairFromSeed(seed, account, change, index)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	defer ZeroMemory(priv)

	addr := AddressFromPublicKey(pub)
	encrypted, err := EncryptPrivateKey(priv, password)
	if err != nil {
		return SecureWallet{}, nil, fmt.Errorf("failed to encrypt wallet key: %w", err)
	}

	w := SecureWallet{
		Address:   addr,
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encrypted,
		HD:        hd,
	}
	if err := SaveSecureWallet(w); err != nil {
		return SecureWallet{}, nil, fmt.Errorf("failed to save secure wallet: %w", err)
	}
	return w, strings.Split(mnemonic, " "), nil
}

func CreateSecureWallet(password string) (SecureWallet, []string) {
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

	seed := bip39.NewSeed(mnemonic, password)
	pub, priv, hd, err := deriveHDKeypairFromSeed(seed, account, change, index)
	if err != nil {
		return SecureWallet{}, err
	}
	defer ZeroMemory(priv)

	addr := AddressFromPublicKey(pub)
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

func RecoverWallet(mnemonic, password string) SecureWallet {
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

func GenerateValidatorKey(nodeID string) ValidatorKey {
	pub, priv, _ := ed25519.GenerateKey(cryptorand.Reader)

	return ValidatorKey{
		ID:         nodeID,
		PublicKey:  pub,
		PrivateKey: priv,
	}
}

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

func isValidatorKeyUsable(v ValidatorKey) bool {
	return isValidatorSigningKeyUsable(v)
}

func envBool(name string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func shouldAutoCreateValidatorKey(nodeID string) bool {
	// Never silently rotate validator identity. Require explicit operator intent.
	// Core validators keep their dedicated override for bootstrap safety.
	if isCoreValidator(nodeID) {
		return envBool(coreKeyCreateOverrideEnv)
	}
	return envBool(validatorKeyCreateOverride)
}

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
	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		if info.IsDir() {
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

func validatorKeyFingerprint(pub []byte) string {
	if len(pub) != ed25519.PublicKeySize {
		return ""
	}
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:8])
}

type validatorKeyMetaFile struct {
	NodeID        string `json:"node_id"`
	Fingerprint   string `json:"fingerprint"`
	CryptoVersion int    `json:"crypto_version"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
}

type validatorKeyBackupManifest struct {
	BackupPath     string `json:"backup_path"`
	BackupSHA256   string `json:"backup_sha256"`
	Fingerprint    string `json:"fingerprint"`
	UpdatedAt      int64  `json:"updated_at"`
	LastVerifiedAt int64  `json:"last_verified_at"`
}

type validatorKeyLoadMeta struct {
	Source      string
	IntegrityOK bool
	ErrorReason string
}

var validatorKeyLoadState = struct {
	mu sync.RWMutex
	m  map[string]validatorKeyLoadMeta
}{
	m: make(map[string]validatorKeyLoadMeta),
}

func recordValidatorKeyLoadMeta(nodeID string, meta validatorKeyLoadMeta) {
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return
	}
	validatorKeyLoadState.mu.Lock()
	validatorKeyLoadState.m[id] = meta
	validatorKeyLoadState.mu.Unlock()
}

func getValidatorKeyLoadMeta(nodeID string) (validatorKeyLoadMeta, bool) {
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return validatorKeyLoadMeta{}, false
	}
	validatorKeyLoadState.mu.RLock()
	meta, ok := validatorKeyLoadState.m[id]
	validatorKeyLoadState.mu.RUnlock()
	return meta, ok
}

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

func validatorKeyMetaPath(nodePath string) string {
	return filepath.Join(nodePath, validatorKeyMetaFileName)
}

func validatorKeyBackupManifestPath(nodePath string) string {
	return filepath.Join(nodePath, validatorKeyBackupManifestFileName)
}

func validatorPublicPath(nodePath string) string {
	return filepath.Join(nodePath, validatorPublicFileName)
}

func validatorFingerprintLockPath(nodePath string) string {
	return filepath.Join(nodePath, validatorFingerprintLockFileName)
}

func resolveValidatorKeyBackupDir(nodePath string) string {
	dir := strings.TrimSpace(ValidatorKeyBackupDir)
	if dir == "" {
		dir = "secure-backups"
	}
	if filepath.IsAbs(dir) {
		return filepath.Clean(dir)
	}
	return filepath.Join(nodePath, dir)
}

func defaultValidatorKeyBackupPath(nodePath string) string {
	return filepath.Join(resolveValidatorKeyBackupDir(nodePath), validatorKeyBackupFileName)
}

func toManifestPath(basePath, targetPath string) string {
	targetPath = filepath.Clean(targetPath)
	if rel, err := filepath.Rel(basePath, targetPath); err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return targetPath
}

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

func fileSHA256Hex(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func writeValidatorKeyMeta(nodeID, nodePath, fingerprint string, cryptoVersion int) error {
	metaPath := validatorKeyMetaPath(nodePath)
	now := time.Now().Unix()
	meta := validatorKeyMetaFile{
		NodeID:        normalizeValidatorID(nodeID),
		Fingerprint:   strings.TrimSpace(fingerprint),
		CryptoVersion: cryptoVersion,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if raw, err := os.ReadFile(metaPath); err == nil && len(raw) > 0 {
		var prev validatorKeyMetaFile
		if jsonErr := json.Unmarshal(raw, &prev); jsonErr == nil {
			if prev.CreatedAt > 0 {
				meta.CreatedAt = prev.CreatedAt
			}
		}
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(metaPath, raw)
}

func writeValidatorPublicKeyFile(nodePath string, pub []byte) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("invalid validator public key length")
	}
	path := validatorPublicPath(nodePath)
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	payload := []byte(hex.EncodeToString(pub) + "\n")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func enforceValidatorFingerprintLock(nodePath, fingerprint string) error {
	fp := strings.TrimSpace(fingerprint)
	if fp == "" {
		return errors.New("validator key fingerprint is empty")
	}
	lockPath := validatorFingerprintLockPath(nodePath)
	raw, err := os.ReadFile(lockPath)
	if err == nil {
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

func readValidatorKeyBackupManifest(nodePath string) (validatorKeyBackupManifest, error) {
	path := validatorKeyBackupManifestPath(nodePath)
	raw, err := os.ReadFile(path)
	if err != nil {
		return validatorKeyBackupManifest{}, err
	}
	var manifest validatorKeyBackupManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return validatorKeyBackupManifest{}, err
	}
	return manifest, nil
}

func writeValidatorKeyBackupManifest(nodePath string, manifest validatorKeyBackupManifest) error {
	path := validatorKeyBackupManifestPath(nodePath)
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, raw)
}

func copyFilePrivate(dstPath, srcPath string) error {
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	return writePrivateFile(dstPath, raw)
}

type legacyValidatorKeyFile struct {
	NodeID      string `json:"node_id"`
	ValidatorID string `json:"validator_id"`
	PublicKey   string `json:"public_key"`
	PrivateKey  string `json:"private_key"`
}

func loadLegacyValidatorKeyBytes(nodeID string, data []byte) (ValidatorKey, bool, error) {
	var legacy legacyValidatorKeyFile
	if err := json.Unmarshal(data, &legacy); err != nil {
		return ValidatorKey{}, false, nil
	}
	if strings.TrimSpace(legacy.PrivateKey) == "" {
		return ValidatorKey{}, false, nil
	}
	privRaw, err := hex.DecodeString(strings.TrimSpace(legacy.PrivateKey))
	if err != nil {
		return ValidatorKey{}, true, fmt.Errorf("legacy validator key private_key invalid hex: %w", err)
	}
	var priv ed25519.PrivateKey
	switch len(privRaw) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(privRaw)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(append([]byte(nil), privRaw...))
	default:
		return ValidatorKey{}, true, fmt.Errorf("legacy validator key private_key invalid length: %d", len(privRaw))
	}
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return ValidatorKey{}, true, errors.New("legacy validator key public key unavailable")
	}
	if strings.TrimSpace(legacy.PublicKey) != "" {
		wantPub, err := hex.DecodeString(strings.TrimSpace(legacy.PublicKey))
		if err != nil {
			return ValidatorKey{}, true, fmt.Errorf("legacy validator key public_key invalid hex: %w", err)
		}
		if !bytes.Equal(wantPub, pub) {
			return ValidatorKey{}, true, errors.New("legacy validator key public_key does not match private_key")
		}
	}
	return ValidatorKey{
		ID:         nodeID,
		PublicKey:  ed25519.PublicKey(append([]byte(nil), pub...)),
		PrivateKey: priv,
	}, true, nil
}

func refreshValidatorKeyArtifacts(nodeID, nodePath, keyPath string, key ValidatorKey, crypto EncryptedKey) error {
	if !isValidatorKeyUsable(key) {
		return errors.New("validator key unusable")
	}
	fp := validatorKeyFingerprint(key.PublicKey)
	if fp == "" {
		return errors.New("failed to compute validator key fingerprint")
	}
	if err := writeValidatorKeyMeta(nodeID, nodePath, fp, crypto.Version); err != nil {
		return err
	}
	if err := writeValidatorPublicKeyFile(nodePath, key.PublicKey); err != nil {
		return err
	}

	backupPath := defaultValidatorKeyBackupPath(nodePath)
	if err := copyFilePrivate(backupPath, keyPath); err != nil {
		return err
	}
	sum, err := fileSHA256Hex(backupPath)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	manifest := validatorKeyBackupManifest{
		BackupPath:     toManifestPath(nodePath, backupPath),
		BackupSHA256:   sum,
		Fingerprint:    fp,
		UpdatedAt:      now,
		LastVerifiedAt: now,
	}
	if err := writeValidatorKeyBackupManifest(nodePath, manifest); err != nil {
		return err
	}
	return nil
}

func validateValidatorBackup(nodePath string, fingerprint string) (bool, uint64, error) {
	manifest, err := readValidatorKeyBackupManifest(nodePath)
	manifestFound := err == nil
	backupPath := ""
	if manifestFound {
		backupPath = fromManifestPath(nodePath, manifest.BackupPath)
	}
	if strings.TrimSpace(backupPath) == "" {
		backupPath = defaultValidatorKeyBackupPath(nodePath)
	}
	info, err := os.Stat(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, 0, fmt.Errorf("missing backup file %s", backupPath)
		}
		return false, 0, err
	}
	age := time.Since(info.ModTime())
	if age < 0 {
		age = 0
	}
	ageSeconds := uint64(age / time.Second)
	if ValidatorKeyBackupMaxAgeHours > 0 {
		limit := time.Duration(ValidatorKeyBackupMaxAgeHours) * time.Hour
		if age > limit {
			return true, ageSeconds, fmt.Errorf("backup too old: age=%s max=%s", age.Truncate(time.Second), limit)
		}
	}
	if manifestFound {
		if strings.TrimSpace(manifest.BackupSHA256) != "" {
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

func restoreValidatorKeyFromBackup(nodeID, nodePath, keyPath string) error {
	manifest, manifestErr := readValidatorKeyBackupManifest(nodePath)
	candidates := make([]string, 0, 2)
	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		for _, c := range candidates {
			if filepath.Clean(c) == clean {
				return
			}
		}
		candidates = append(candidates, clean)
	}
	if manifestErr == nil {
		if p := fromManifestPath(nodePath, manifest.BackupPath); strings.TrimSpace(p) != "" {
			addCandidate(p)
		}
	}
	defaultPath := defaultValidatorKeyBackupPath(nodePath)
	addCandidate(defaultPath)
	if parent := filepath.Dir(filepath.Clean(nodePath)); parent != "" && parent != "." && parent != filepath.Clean(nodePath) {
		backupDir := strings.TrimSpace(ValidatorKeyBackupDir)
		if backupDir == "" {
			backupDir = "secure-backups"
		}
		if !filepath.IsAbs(backupDir) {
			addCandidate(filepath.Join(parent, backupDir, validatorKeyBackupFileName))
		}
	}
	var lastErr error
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := os.Stat(candidate); err != nil {
			lastErr = err
			continue
		}
		if manifestErr == nil && strings.EqualFold(filepath.Clean(candidate), filepath.Clean(fromManifestPath(nodePath, manifest.BackupPath))) {
			if strings.TrimSpace(manifest.BackupSHA256) != "" {
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

func CollectValidatorKeyHealth(nodeID, nodePath string, key ValidatorKey) ValidatorKeyHealth {
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
	backupPresent, backupAge, _ := validateValidatorBackup(nodePath, out.Fingerprint)
	if (ValidatorHSMEnabled || ValidatorMPCEnabled) && out.Loaded {
		backupPresent = true
		backupAge = 0
	}
	out.BackupPresent = backupPresent
	out.BackupAgeSeconds = backupAge
	return out
}

func canPromptInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

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

	reader := bufio.NewReader(os.Stdin)
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

func finalizeValidatorKeyLoad(nodeID, path, keyPath, source string, key ValidatorKey, enc SecureWallet) ValidatorKey {
	if !isValidatorKeyUsable(key) {
		return fallbackValidatorKey(nodeID, "validator key unusable after load")
	}
	fp := validatorKeyFingerprint(key.PublicKey)
	if fp == "" {
		return fallbackValidatorKey(nodeID, "validator key fingerprint compute failed")
	}
	expected := strings.TrimSpace(ValidatorRequiredKeyFingerprint)
	match := expected == "" || strings.EqualFold(fp, expected)
	if !match {
		return fallbackValidatorKey(nodeID,
			fmt.Sprintf("validator key fingerprint mismatch: expected=%s got=%s", expected, fp),
		)
	}
	if err := enforceValidatorFingerprintLock(path, fp); err != nil {
		return fallbackValidatorKey(nodeID, err.Error())
	}
	if err := refreshValidatorKeyArtifacts(nodeID, path, keyPath, key, enc.Crypto); err != nil {
		if ValidatorKeyBackupRequired {
			return fallbackValidatorKey(nodeID, fmt.Sprintf("validator key backup refresh failed: %v", err))
		}
		log.Printf("[KEY-AUDIT] validator=%s backup_present=false integrity=false reason=backup_refresh_failed error=%v", nodeID, err)
	}
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

func GenerateValidatorKeyOffline(nodeID, nodePath string) (string, error) {
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return "", errors.New("node id is required")
	}
	if !canPromptInteractive() {
		return "", errors.New("offline keygen requires interactive terminal")
	}
	if err := ensurePrivateDirectory(nodePath); err != nil {
		return "", err
	}
	keyPath := filepath.Join(nodePath, "validator.sec")
	if _, err := os.Stat(keyPath); err == nil {
		return "", fmt.Errorf("validator key already exists at %s; refusing overwrite", keyPath)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to access %s: %w", keyPath, err)
	}

	passRaw, err := ReadPassword("New validator password: ")
	if err != nil {
		return "", fmt.Errorf("failed to read validator password: %w", err)
	}
	defer ZeroMemory(passRaw)
	pass := strings.TrimSpace(string(passRaw))
	if err := validateNewValidatorPassword(pass); err != nil {
		return "", err
	}

	confirmRaw, err := ReadPassword("Confirm validator password: ")
	if err != nil {
		return "", fmt.Errorf("failed to read validator password confirmation: %w", err)
	}
	defer ZeroMemory(confirmRaw)
	confirm := strings.TrimSpace(string(confirmRaw))
	if pass != confirm {
		return "", errors.New("password confirmation mismatch")
	}

	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate validator key: %w", err)
	}
	encKey, err := EncryptPrivateKey(priv, pass)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt validator key: %w", err)
	}
	enc := SecureWallet{
		Address:   id,
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encKey,
	}
	data, err := json.MarshalIndent(enc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal validator key file: %w", err)
	}
	if err := writePrivateFile(keyPath, data); err != nil {
		return "", fmt.Errorf("failed to persist validator key file: %w", err)
	}
	fp := validatorKeyFingerprint(pub)
	if fp == "" {
		return "", errors.New("failed to compute validator key fingerprint")
	}
	if err := enforceValidatorFingerprintLock(nodePath, fp); err != nil {
		return "", err
	}
	created := ValidatorKey{
		ID:         id,
		PublicKey:  pub,
		PrivateKey: priv,
	}
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

func loadOrCreateValidatorKeyInternal(nodeID, path string, allowRestore bool, preferredSource string) ValidatorKey {
	if err := ensurePrivateDirectory(path); err != nil {
		return fallbackValidatorKey(nodeID, err.Error())
	}
	if key, handled := LoadValidatorHSMKey(nodeID, path); handled {
		return key
	}

	keyPath := filepath.Join(path, "validator.sec")
	if data, err := os.ReadFile(keyPath); err == nil {
		if err := ensurePrivateFilePermissions(keyPath); err != nil {
			log.Printf("[WARN] insecure validator key file permissions: %v", err)
		}

		if legacyKey, ok, legacyErr := loadLegacyValidatorKeyBytes(nodeID, data); ok {
			if legacyErr != nil {
				return fallbackValidatorKey(nodeID, legacyErr.Error())
			}
			log.Printf("[KEY-LEGACY] validator=%s source=validator.sec result=loaded", nodeID)
			legacyWallet := SecureWallet{
				Address:   nodeID,
				PublicKey: hex.EncodeToString(legacyKey.PublicKey),
			}
			return finalizeValidatorKeyLoad(nodeID, path, keyPath, "legacy", legacyKey, legacyWallet)
		}

		var enc SecureWallet
		if err := json.Unmarshal(data, &enc); err != nil {
			return fallbackValidatorKey(nodeID, fmt.Sprintf("corrupted validator key file (%s); restore backup validator.sec", keyPath))
		}

		if _, hasEnv := os.LookupEnv(validatorPasswordEnv); !hasEnv && canPromptInteractive() {
			log.Printf("validator key found for %s; enter password to unlock", nodeID)
		}

		pass, passFromEnv, passErr := getValidatorPasswordWithSource(nodeID, path)
		if passErr != nil {
			return fallbackValidatorKey(nodeID, passErr.Error())
		}

		attempts := validatorUnlockMaxAttempts
		if passFromEnv {
			// Automation mode should fail fast if env password is wrong.
			attempts = 1
		}

		var (
			priv ed25519.PrivateKey
			derr error
		)
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

		pub, err := hex.DecodeString(enc.PublicKey)
		if err != nil {
			return fallbackValidatorKey(nodeID, "invalid validator public key encoding")
		}
		if len(pub) != ed25519.PublicKeySize {
			return fallbackValidatorKey(nodeID, "invalid validator public key length")
		}

		if !isEncryptedKeyV2(enc.Crypto) {
			if upgraded, err := EncryptPrivateKey(priv, pass); err == nil {
				enc.Crypto = upgraded
				enc.Address = nodeID
				enc.PublicKey = hex.EncodeToString(pub)
				if rawEnc, err := json.MarshalIndent(enc, "", "  "); err == nil {
					if err := writePrivateFile(keyPath, rawEnc); err == nil {
						log.Println("validator key encryption upgraded to argon2id (v2)")
					}
				}
			}
		}

		log.Println("validator key unlocked")
		loaded := ValidatorKey{
			ID:         nodeID,
			PublicKey:  ed25519.PublicKey(pub),
			PrivateKey: priv,
		}
		source := "existing"
		if strings.TrimSpace(preferredSource) != "" {
			source = strings.TrimSpace(preferredSource)
		}
		return finalizeValidatorKeyLoad(nodeID, path, keyPath, source, loaded, enc)
	} else if !os.IsNotExist(err) {
		return fallbackValidatorKey(nodeID, fmt.Sprintf("failed to read validator key file %s: %v", keyPath, err))
	}

	autoCreate := shouldAutoCreateValidatorKey(nodeID)
	isCore := isCoreValidator(nodeID)
	existingState := hasExistingNodeState(path)
	restoreAttempted := false
	if ValidatorKeyRestoreAllowedOnMissing && allowRestore {
		restoreAttempted = true
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

	if _, hasEnv := os.LookupEnv(validatorPasswordEnv); !hasEnv && canPromptInteractive() {
		log.Printf("creating validator key for %s; set a new validator password", nodeID)
	}

	pass, _, passErr := getValidatorPasswordWithSource(nodeID, path)
	if passErr != nil {
		return fallbackValidatorKey(nodeID, passErr.Error())
	}
	if err := validateNewValidatorPassword(pass); err != nil {
		return fallbackValidatorKey(nodeID, err.Error())
	}

	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		return fallbackValidatorKey(nodeID, "failed to generate validator key")
	}

	encKey, err := EncryptPrivateKey(priv, pass)
	if err != nil {
		return fallbackValidatorKey(nodeID, fmt.Sprintf("failed to encrypt validator key: %v", err))
	}

	enc := SecureWallet{
		Address:   nodeID,
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encKey,
	}

	data, err := json.MarshalIndent(enc, "", "  ")
	if err != nil {
		return fallbackValidatorKey(nodeID, fmt.Sprintf("failed to marshal validator key file: %v", err))
	}

	if err := writePrivateFile(keyPath, data); err != nil {
		return fallbackValidatorKey(nodeID, fmt.Sprintf("failed to persist validator key file: %v", err))
	}

	log.Println("validator key created & encrypted")
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

func LoadOrCreateValidatorKey(nodeID, path string) ValidatorKey {
	return loadOrCreateValidatorKeyInternal(nodeID, path, true, "")
}

func validateCorePasswordFile(path string) error {
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

func defaultCorePasswordFileCandidates(nodeID, nodePath string) []string {
	base := strings.TrimSpace(nodePath)
	candidates := make([]string, 0, 6)
	if base != "" {
		base = filepath.Clean(base)
		candidates = append(candidates,
			filepath.Join(base, "validator.pass"),
			filepath.Join(filepath.Dir(base), "validator.pass"),
		)
	}
	if homeDir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(homeDir) != "" {
		id := normalizeValidatorID(nodeID)
		if id != "" {
			candidates = append(candidates, filepath.Join(homeDir, ".msc-secrets", id+".pass"))
			candidates = append(candidates, filepath.Join(homeDir, ".msc-secrets", strings.ToLower(id)+".pass"))
		}
	}
	out := make([]string, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		out = append(out, candidate)
	}
	return out
}

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

func configuredValidatorPasswordMode() string {
	mode := normalizeValidatorPasswordMode(ValidatorPasswordMode)
	if mode == "" || mode == validatorPasswordModeEnvOnly {
		return validatorPasswordModeFileOrPrompt
	}
	return mode
}

func resolveEffectiveValidatorPasswordMode(nodeID string, nodePath string) (string, error) {
	configMode := configuredValidatorPasswordMode()
	if !isCoreValidatorForSecurityPolicy(nodeID, nodePath) {
		return configMode, nil
	}
	raw, ok := os.LookupEnv(validatorPasswordModeEnv)
	if !ok || strings.TrimSpace(raw) == "" {
		return configMode, nil
	}
	mode := normalizeValidatorPasswordMode(raw)
	if mode != validatorPasswordModeEnvOnly {
		return "", fmt.Errorf("env_password_mode_invalid: %s must be 'env_only' or empty", validatorPasswordModeEnv)
	}
	return validatorPasswordModeEnvOnly, nil
}

func getValidatorPasswordWithSource(nodeID string, nodePath string) (string, bool, error) {
	coreNode := isCoreValidatorForSecurityPolicy(nodeID, nodePath)
	effectiveMode, modeErr := resolveEffectiveValidatorPasswordMode(nodeID, nodePath)
	if modeErr != nil {
		setValidatorSecretRuntime(nodeID, configuredValidatorPasswordMode(), "blocked")
		return "", false, modeErr
	}
	setValidatorSecretRuntime(nodeID, effectiveMode, "")

	if effectiveMode == validatorPasswordModeEnvOnly {
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
				for _, passwordFile := range defaultCorePasswordFileCandidates(nodeID, nodePath) {
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
		passBytes, err := ReadPassword("Validator password: ")
		if err != nil {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, fmt.Errorf("failed to read validator password: %w", err)
		}
		defer ZeroMemory(passBytes)
		pass := strings.TrimSpace(string(passBytes))
		if pass == "" {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, errors.New("validator password cannot be empty")
		}
		setValidatorSecretRuntime(nodeID, effectiveMode, "prompt")
		return pass, false, nil
	}

	if coreNode && strings.TrimSpace(ValidatorCorePasswordFile) != "" {
		passwordFile := strings.TrimSpace(ValidatorCorePasswordFile)
		if err := validateCorePasswordFile(passwordFile); err != nil {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, err
		}
		passBytes, err := os.ReadFile(passwordFile)
		if err != nil {
			setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
			return "", false, fmt.Errorf("failed to read validators.core_password_file: %w", err)
		}
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
		for _, passwordFile := range defaultCorePasswordFileCandidates(nodeID, nodePath) {
			if _, err := os.Stat(passwordFile); err != nil {
				continue
			}
			if err := validateCorePasswordFile(passwordFile); err != nil {
				setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
				return "", false, err
			}
			passBytes, err := os.ReadFile(passwordFile)
			if err != nil {
				setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
				return "", false, fmt.Errorf("failed to read auto core password file %s: %w", passwordFile, err)
			}
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
	passBytes, err := ReadPassword("Validator password: ")
	if err != nil {
		setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
		return "", false, fmt.Errorf("failed to read validator password: %w", err)
	}
	defer ZeroMemory(passBytes)

	pass := strings.TrimSpace(string(passBytes))
	if pass == "" {
		setValidatorSecretRuntime(nodeID, effectiveMode, "blocked")
		return "", false, errors.New("validator password cannot be empty")
	}
	setValidatorSecretRuntime(nodeID, effectiveMode, "prompt")
	return pass, false, nil
}

func validateNewValidatorPassword(pass string) error {
	p := strings.TrimSpace(pass)
	if p == "" {
		return errors.New("validator password cannot be empty")
	}
	weak := map[string]struct{}{
		"m":        {},
		"password": {},
		"123456":   {},
		"admin":    {},
		"test":     {},
	}
	if _, bad := weak[strings.ToLower(p)]; bad {
		return errors.New("weak validator password blocked; choose a strong passphrase")
	}
	if len(p) < 8 {
		return errors.New("validator password too short; minimum length is 8")
	}
	return nil
}

func getValidatorPassword() (string, error) {
	pass, _, err := getValidatorPasswordWithSource("", "")
	return pass, err
}

func ReadPassword(prompt string) ([]byte, error) {
	fmt.Print(prompt)
	defer fmt.Println()

	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}
	return pass, nil
}

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

func (n *Node) GetConnectedValidatorCount(height int) int {
	validators := n.GetConsensusValidators(height)
	if len(validators) == 0 {
		return 0
	}

	now := time.Now()
	count := 0

	n.validatorMu.RLock()
	defer n.validatorMu.RUnlock()

	for _, v := range validators {
		if v == n.ID {
			count++
			continue
		}
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

func (n *Node) VerifyExecutionQuorum(block Block, height int) bool {
	received := len(block.ExecutionResults)
	if received == 0 {
		return false
	}

	total := n.countLiveValidators()
	if total == 0 {
		total = len(n.GetConsensusValidators(height))
	}
	required := execQuorumRequired(total)
	if required < 1 {
		required = 1
	}
	if DebugConsensus {
		fmt.Printf("🔎 Execution quorum target: required=%d total=%d\n", required, total)
	}

	resultCount := make(map[string]int)
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
	hash := sha256.Sum256(resultBytes)

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
func (n *Node) GetActiveExecutors() []string {
	now := time.Now()

	n.peerMu.RLock()
	defer n.peerMu.RUnlock()

	executors := []string{}

	// 🔹 Include self
	executors = append(executors, n.ID)

	// 🔹 Include all connected peers
	for _, p := range n.PeersLibp2p {
		id := p.String()

		// Basic liveness gate
		peerLastSeenMu.Lock()
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
func ApplyMintResult(
	ledger *Ledger,
	result MintResult,
) {
	ledger.Balances[result.To] += int(result.Amount)
	TotalMintedMSC = result.NewTotalSupply
}
func RecalculateTotalSupplyStrict(
	ledger Ledger,
) (int64, bool) {

	var total int64

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
func VerifyTotalSupply(
	ledger Ledger,
	expectedMax int64,
) bool {

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
func Sign(priv ed25519.PrivateKey, payload []byte) []byte {
	return ed25519.Sign(priv, payload)
}
func txPayloadWithEnvelope(tx Transaction, includeEVMEnvelope bool) []byte {
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
		if normalized := normalizeConsensusPubKeyHex(tx.ValidatorPubKey); normalized != "" {
			buf.WriteString(normalized)
			buf.WriteByte(0)
		}
	}
	_ = binary.Write(buf, binary.BigEndian, int64(tx.EVMGasLimit))
	buf.WriteString(stripHexPrefix(tx.EVMCode))
	buf.WriteByte(0)
	buf.WriteString(stripHexPrefix(tx.EVMInput))
	buf.WriteByte(0)
	if includeEVMEnvelope {
		buf.WriteString(stripHexPrefix(tx.EVMRawTx))
		buf.WriteByte(0)
		buf.WriteString(stripHexPrefix(tx.EVMTxHash))
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

	buf.WriteString(ChainID)
	buf.WriteByte(0)

	buf.WriteByte(byte(tx.Type))

	return buf.Bytes()
}

func TxPayload(tx Transaction) []byte {
	return txPayloadWithEnvelope(tx, true)
}

// TxPayloadLegacy preserves historical payload layout used by older wallet UIs.
// It excludes evm_raw_tx and evm_tx_hash fields.
func TxPayloadLegacy(tx Transaction) []byte {
	return txPayloadWithEnvelope(tx, false)
}

// ComputeTxID returns the deterministic tx_id = SHA256(tx_bytes).
func ComputeTxID(tx Transaction) string {
	sum := sha256.Sum256(TxPayload(tx))
	return hex.EncodeToString(sum[:])
}

// ComputeTxIDLegacy hashes the historical payload layout for compatibility.
func ComputeTxIDLegacy(tx Transaction) string {
	sum := sha256.Sum256(TxPayloadLegacy(tx))
	return hex.EncodeToString(sum[:])
}

// TxOrderKey returns deterministic ordering key = SHA256(epoch || tx_id).
func TxOrderKey(epoch uint64, tx Transaction) string {
	txID := ComputeTxID(tx)
	buf := make([]byte, 8+len(txID))
	binary.BigEndian.PutUint64(buf[:8], epoch)
	copy(buf[8:], []byte(txID))
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}
func IsExecutorActive(executorID string, height int) bool {

	peerLastSeenMu.Lock()
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

func canonicalMisbehaviorReason(reason string) string {
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

func (n *Node) RecordMisbehavior(
	validator string,
	reason string,
	height int,
	blockHash string,
) {
	reasonKey := canonicalMisbehaviorReason(reason)
	if reasonKey == "" || height <= 0 {
		return
	}

	validator = normalizeValidatorID(validator)
	if validator == "" {
		return
	}
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
	existing := n.MisbehaviorLog[validator]
	entry, entryKey := normalizeSlashEvidenceForStore(entry)
	if entryKey == "" {
		n.misbehaviorMu.Unlock()
		return
	}
	// Production evidence dedupe spans the full retained log, not only the
	// recent tail, so startup replay and delayed gossip cannot cascade slashes.
	for _, ev := range existing {
		_, existingKey := normalizeSlashEvidenceForStore(ev)
		if existingKey != "" && existingKey == entryKey {
			n.misbehaviorMu.Unlock()
			return
		}
	}
	blockHashKey := strings.ToLower(strings.TrimSpace(blockHash))
	// De-duplicate repeated reports for the same validator/reason/height/hash.
	// This prevents slash cascades from re-gossip/re-verify loops.
	for i := len(existing) - 1; i >= 0 && i >= len(existing)-32; i-- {
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

func (n *Node) CheckSlashingThreshold(validator string, reason string) {
	reasonKey := canonicalMisbehaviorReason(reason)
	if reasonKey == "" {
		return
	}
	if reasonKey == "exec_equivocation" {
		if DebugConsensus {
			fmt.Printf(
				"SLASH deferred validator=%s reason=%s source=evidence_only\n",
				ShortID(validator),
				reasonKey,
			)
		}
		return
	}

	const recentWindow = uint64(128)
	var chainHeight uint64
	if n != nil && n.Blockchain != nil {
		chainHeight = n.Blockchain.Height()
	}

	n.misbehaviorMu.Lock()
	entries := append([]SlashEvidence(nil), n.MisbehaviorLog[validator]...)
	n.misbehaviorMu.Unlock()
	if len(entries) == 0 {
		return
	}

	reasonCount := 0
	var latestTrigger SlashEvidence
	for i := len(entries) - 1; i >= 0; i-- {
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
