package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// `operatorMPCSharePasswordEnv` defines the constant value used by this package.
	operatorMPCSharePasswordEnv = "MSC_MPC_SHARE_PASSWORD"
	// `validatorMPCShareScheme` defines whether the related condition is satisfied.
	validatorMPCShareScheme     = "msc-shamir-ed25519-seed-v1"
	// `validatorMPCShareAAD` defines whether the related condition is satisfied.
	validatorMPCShareAAD        = keyEncryptionAAD + ":mpc-share-v1"
)

type ValidatorMPCShareFile struct {
	// `Version` stores the value associated with this record.
	Version      int          `json:"version"`
	// `Scheme` stores the value associated with this record.
	Scheme       string       `json:"scheme"`
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID  string       `json:"validator_id"`
	// `Participant` stores the value associated with this record.
	Participant  int          `json:"participant"`
	// `Threshold` stores the value associated with this record.
	Threshold    int          `json:"threshold"`
	// `Participants` stores the value associated with this record.
	Participants int          `json:"participants"`
	// `PublicKeyHex` stores the value associated with this record.
	PublicKeyHex string       `json:"public_key_hex"`
	// `Fingerprint` stores the value associated with this record.
	Fingerprint  string       `json:"fingerprint"`
	// `Share` stores the value associated with this record.
	Share        EncryptedKey `json:"share"`
	// `CreatedAt` stores the value associated with this record.
	CreatedAt    int64        `json:"created_at"`
	// `Warning` stores the value associated with this record.
	Warning      string       `json:"warning,omitempty"`
}

type validatorMPCPlainShare struct {
	// `Participant` stores the value associated with this record.
	Participant int
	// `Share` stores the value associated with this record.
	Share       []byte
}

type validatorMPCKeygenResult struct {
	// `ValidatorID` stores whether the related condition is satisfied.
	ValidatorID   string   `json:"validator_id"`
	// `PublicKeyHex` stores the value associated with this record.
	PublicKeyHex  string   `json:"public_key_hex"`
	// `Fingerprint` stores the value associated with this record.
	Fingerprint   string   `json:"fingerprint"`
	// `Threshold` stores the value associated with this record.
	Threshold     int      `json:"threshold"`
	// `Participants` stores the value associated with this record.
	Participants  int      `json:"participants"`
	// `PublicFile` stores the value associated with this record.
	PublicFile    string   `json:"public_file"`
	// `ShareFiles` stores the value associated with this record.
	ShareFiles    []string `json:"share_files"`
	// `SignerCommand` stores the value associated with this record.
	SignerCommand string   `json:"signer_command"`
	// `ConfigTOML` stores the configuration used by this operation.
	ConfigTOML    string   `json:"config_toml"`
	// `Warning` stores the value associated with this record.
	Warning       string   `json:"warning"`
}

// generateValidatorMPCShares implements the generate validator mpc shares helper.
func generateValidatorMPCShares(validatorID string, threshold, participants int) (ed25519.PublicKey, []validatorMPCPlainShare, error) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(validatorID)
	if id == "" {
		return nil, nil, errors.New("validator id is required")
	}
	if threshold < 2 {
		return nil, nil, errors.New("mpc threshold must be at least 2")
	}
	if participants < threshold {
		return nil, nil, errors.New("mpc participants must be >= threshold")
	}
	if participants > 255 {
		return nil, nil, errors.New("mpc participants must be <= 255")
	}
	// `seed` stores the value produced by this operation.
	seed := make([]byte, ed25519.SeedSize)
	// `err` stores the error produced by this operation.
	if _, err := cryptorand.Read(seed); err != nil {
		return nil, nil, err
	}
	defer ZeroMemory(seed)
	// `priv` stores the value produced by this operation.
	priv := ed25519.NewKeyFromSeed(seed)
	defer ZeroMemory(priv)
	// `pub` stores the value produced by this operation.
	pub := append(ed25519.PublicKey(nil), priv.Public().(ed25519.PublicKey)...)
	// `shares` and `err` store the error produced by this operation.
	shares, err := splitValidatorMPCSeed(seed, threshold, participants)
	if err != nil {
		return nil, nil, err
	}
	return pub, shares, nil
}

// writeValidatorMPCShares implements the write validator mpc shares helper.
func writeValidatorMPCShares(validatorID string, outDir string, threshold, participants int, password string, force bool) (validatorMPCKeygenResult, error) {
	// `pub`, `shares`, and `err` store the error produced by this operation.
	pub, shares, err := generateValidatorMPCShares(validatorID, threshold, participants)
	if err != nil {
		return validatorMPCKeygenResult{}, err
	}
	return writeValidatorMPCShareFiles(validatorID, outDir, threshold, participants, password, force, pub, shares, "Built-in share files are for dev/test or controlled operator rehearsal. For production large validators, use an audited external MPC/DKG signer and the same mpc_external_signer_command contract.")
}

// writeValidatorMPCSharesFromSeed implements the write validator mpc shares from seed helper.
func writeValidatorMPCSharesFromSeed(validatorID string, outDir string, threshold, participants int, password string, force bool, seed []byte) (validatorMPCKeygenResult, error) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(validatorID)
	if id == "" {
		return validatorMPCKeygenResult{}, errors.New("validator id is required")
	}
	if len(seed) != ed25519.SeedSize {
		return validatorMPCKeygenResult{}, errors.New("invalid validator seed length")
	}
	// `priv` stores the value produced by this operation.
	priv := ed25519.NewKeyFromSeed(seed)
	defer ZeroMemory(priv)
	// `pub` stores the value produced by this operation.
	pub := append(ed25519.PublicKey(nil), priv.Public().(ed25519.PublicKey)...)
	// `shares` and `err` store the error produced by this operation.
	shares, err := splitValidatorMPCSeed(seed, threshold, participants)
	if err != nil {
		return validatorMPCKeygenResult{}, err
	}
	return writeValidatorMPCShareFiles(id, outDir, threshold, participants, password, force, pub, shares, "Existing validator.sec was migrated into MPC shares for the same consensus public key. Keep validator.sec only as an offline break-glass backup.")
}

// writeValidatorMPCShareFiles implements the write validator mpc share files helper.
func writeValidatorMPCShareFiles(validatorID string, outDir string, threshold, participants int, password string, force bool, pub ed25519.PublicKey, shares []validatorMPCPlainShare, warning string) (validatorMPCKeygenResult, error) {
	// `id` stores the current position in the related collection.
	id := normalizeValidatorID(validatorID)
	if id == "" {
		return validatorMPCKeygenResult{}, errors.New("validator id is required")
	}
	if len(pub) != ed25519.PublicKeySize {
		return validatorMPCKeygenResult{}, errors.New("invalid validator public key")
	}
	if strings.TrimSpace(password) == "" {
		return validatorMPCKeygenResult{}, errors.New("mpc share password required")
	}
	outDir = operatorResolvePath(outDir)
	if outDir == "" {
		outDir = filepath.Join("data", id, "mpc")
	}
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(outDir); err != nil {
		return validatorMPCKeygenResult{}, err
	}
	// `pubHex` stores the value produced by this operation.
	pubHex := hex.EncodeToString(pub)
	// `fp` stores the value produced by this operation.
	fp := validatorKeyFingerprint(pub)
	if fp == "" {
		return validatorMPCKeygenResult{}, errors.New("failed to compute validator public key fingerprint")
	}
	// `pubFile` stores the value produced by this operation.
	pubFile := filepath.Join(outDir, "validator.pub")
	if !force {
		// `err` stores the error produced by this operation.
		if _, err := os.Stat(pubFile); err == nil {
			return validatorMPCKeygenResult{}, fmt.Errorf("%s already exists; use --force to overwrite", pubFile)
		} else if !os.IsNotExist(err) {
			return validatorMPCKeygenResult{}, err
		}
	}
	// `err` stores the error produced by this operation.
	if err := writePrivateFile(pubFile, []byte(pubHex+"\n")); err != nil {
		return validatorMPCKeygenResult{}, err
	}
	// `now` stores the value produced by this operation.
	now := time.Now().Unix()
	// `shareFiles` stores the value produced by this operation.
	shareFiles := make([]string, 0, len(shares))
	// `plain` tracks the current values while iterating.
	for _, plain := range shares {
		// `sharePath` stores the value produced by this operation.
		sharePath := filepath.Join(outDir, fmt.Sprintf("share%d.sec", plain.Participant))
		if !force {
			// `err` stores the error produced by this operation.
			if _, err := os.Stat(sharePath); err == nil {
				return validatorMPCKeygenResult{}, fmt.Errorf("%s already exists; use --force to overwrite", sharePath)
			} else if !os.IsNotExist(err) {
				return validatorMPCKeygenResult{}, err
			}
		}
		// `enc` and `err` store the error produced by this operation.
		enc, err := encryptValidatorMPCSecret(plain.Share, password)
		if err != nil {
			return validatorMPCKeygenResult{}, err
		}
		// `file` stores the value produced by this operation.
		file := ValidatorMPCShareFile{
			Version:      1,
			Scheme:       validatorMPCShareScheme,
			ValidatorID:  id,
			Participant:  plain.Participant,
			Threshold:    threshold,
			Participants: participants,
			PublicKeyHex: pubHex,
			Fingerprint:  fp,
			Share:        enc,
			CreatedAt:    now,
			Warning:      "MPC share only; no full validator private key is stored in this file.",
		}
		// `raw` and `err` store the error produced by this operation.
		raw, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			return validatorMPCKeygenResult{}, err
		}
		// `err` stores the error produced by this operation.
		if err := writePrivateFile(sharePath, raw); err != nil {
			return validatorMPCKeygenResult{}, err
		}
		ZeroMemory(plain.Share)
		shareFiles = append(shareFiles, operatorCleanPath(sharePath))
	}
	// `cleanPubFile` stores the value produced by this operation.
	cleanPubFile := operatorCleanPath(pubFile)
	// `commandShares` stores the result produced by this operation.
	commandShares := make([]string, 0, threshold)
	// `i` stores the current position in the related collection.
	for i := 0; i < threshold && i < len(shareFiles); i++ {
		commandShares = append(commandShares, shareFiles[i])
	}
	// `signerCommand` stores the value produced by this operation.
	signerCommand := fmt.Sprintf("./msc-node validator mpc-sign --shares %s --password-env %s", strings.Join(commandShares, ","), operatorMPCSharePasswordEnv)
	// `config` stores the configuration used by this operation.
	config := fmt.Sprintf("[validators]\nmpc_enabled = true\nmpc_provider = \"threshold_ed25519\"\nmpc_key_id = \"msc-validator-%s-cluster\"\nmpc_public_key = \"%s\"\nmpc_external_signer_command = \"%s\"\nmpc_timeout_ms = 3000\nmpc_threshold = %d\nmpc_participants = %d\n", id, pubHex, signerCommand, threshold, participants)
	return validatorMPCKeygenResult{
		ValidatorID:   id,
		PublicKeyHex:  pubHex,
		Fingerprint:   fp,
		Threshold:     threshold,
		Participants:  participants,
		PublicFile:    cleanPubFile,
		ShareFiles:    shareFiles,
		SignerCommand: signerCommand,
		ConfigTOML:    config,
		Warning:       warning,
	}, nil
}

// encryptValidatorMPCSecret implements the encrypt validator mpc secret helper.
func encryptValidatorMPCSecret(secret []byte, password string) (EncryptedKey, error) {
	if len(secret) == 0 {
		return EncryptedKey{}, errors.New("empty mpc secret")
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
	ciphertext := gcm.Seal(nil, nonce, secret, []byte(validatorMPCShareAAD))
	return EncryptedKey{
		Version:         keyEncryptionVersion,
		KDF:             keyEncryptionKDF,
		Argon2Time:      defaultArgon2Time,
		Argon2MemoryKiB: defaultArgon2Memory,
		Argon2Threads:   defaultArgon2Threads,
		Salt:            hex.EncodeToString(salt),
		Nonce:           hex.EncodeToString(nonce),
		Ciphertext:      hex.EncodeToString(ciphertext),
	}, nil
}

// decryptValidatorMPCSecret implements the decrypt validator mpc secret helper.
func decryptValidatorMPCSecret(enc EncryptedKey, password string) ([]byte, error) {
	if !isEncryptedKeyV2(enc) {
		return nil, errors.New("mpc shares require v2 argon2id encryption")
	}
	// `salt` and `err` store the error produced by this operation.
	salt, err := hex.DecodeString(enc.Salt)
	if err != nil {
		return nil, err
	}
	// `nonce` and `err` store the error produced by this operation.
	nonce, err := hex.DecodeString(enc.Nonce)
	if err != nil {
		return nil, err
	}
	// `ciphertext` and `err` store the error produced by this operation.
	ciphertext, err := hex.DecodeString(enc.Ciphertext)
	if err != nil {
		return nil, err
	}
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
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(validatorMPCShareAAD))
	if err != nil {
		return nil, errors.New("invalid password or corrupted mpc share")
	}
	return plain, nil
}

// readValidatorMPCShare implements the read validator mpc share helper.
func readValidatorMPCShare(path string, password string) (ValidatorMPCShareFile, []byte, error) {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(operatorResolvePath(path))
	if err != nil {
		return ValidatorMPCShareFile{}, nil, err
	}
	// `file` stores the value used by this operation.
	var file ValidatorMPCShareFile
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &file); err != nil {
		return ValidatorMPCShareFile{}, nil, err
	}
	if file.Version != 1 || file.Scheme != validatorMPCShareScheme {
		return ValidatorMPCShareFile{}, nil, errors.New("unsupported mpc share file")
	}
	if file.Participant <= 0 || file.Participant > 255 {
		return ValidatorMPCShareFile{}, nil, errors.New("invalid mpc participant index")
	}
	// `share` and `err` store the error produced by this operation.
	share, err := decryptValidatorMPCSecret(file.Share, password)
	if err != nil {
		return ValidatorMPCShareFile{}, nil, err
	}
	if len(share) != ed25519.SeedSize {
		ZeroMemory(share)
		return ValidatorMPCShareFile{}, nil, fmt.Errorf("invalid mpc share length %d", len(share))
	}
	return file, share, nil
}

// reconstructValidatorMPCSeedFromFiles implements the reconstruct validator mpc seed from files helper.
func reconstructValidatorMPCSeedFromFiles(paths []string, password string) (ed25519.PublicKey, []byte, ValidatorMPCShareFile, error) {
	if len(paths) == 0 {
		return nil, nil, ValidatorMPCShareFile{}, errors.New("at least one mpc share path required")
	}
	// `files` stores the value produced by this operation.
	files := make([]ValidatorMPCShareFile, 0, len(paths))
	// `shares` stores the result produced by this operation.
	shares := make([]validatorMPCPlainShare, 0, len(paths))
	// `seen` stores the value produced by this operation.
	seen := map[int]bool{}
	// `path` tracks the current values while iterating.
	for _, path := range paths {
		// `file`, `share`, and `err` store the error produced by this operation.
		file, share, err := readValidatorMPCShare(path, password)
		if err != nil {
			// `s` tracks the current values while iterating.
			for _, s := range shares {
				ZeroMemory(s.Share)
			}
			return nil, nil, ValidatorMPCShareFile{}, err
		}
		if seen[file.Participant] {
			ZeroMemory(share)
			// `s` tracks the current values while iterating.
			for _, s := range shares {
				ZeroMemory(s.Share)
			}
			return nil, nil, ValidatorMPCShareFile{}, fmt.Errorf("duplicate mpc participant %d", file.Participant)
		}
		seen[file.Participant] = true
		files = append(files, file)
		shares = append(shares, validatorMPCPlainShare{Participant: file.Participant, Share: share})
	}
	sort.Slice(shares, func(i, j int) bool { return shares[i].Participant < shares[j].Participant })
	// `ref` stores the value produced by this operation.
	ref := files[0]
	// `file` tracks the current values while iterating.
	for _, file := range files[1:] {
		if file.ValidatorID != ref.ValidatorID || file.PublicKeyHex != ref.PublicKeyHex || file.Threshold != ref.Threshold || file.Participants != ref.Participants || file.Fingerprint != ref.Fingerprint {
			// `s` tracks the current values while iterating.
			for _, s := range shares {
				ZeroMemory(s.Share)
			}
			return nil, nil, ValidatorMPCShareFile{}, errors.New("mpc shares belong to different validator ceremonies")
		}
	}
	if len(shares) < ref.Threshold {
		// `s` tracks the current values while iterating.
		for _, s := range shares {
			ZeroMemory(s.Share)
		}
		return nil, nil, ValidatorMPCShareFile{}, fmt.Errorf("not enough mpc shares: got %d need %d", len(shares), ref.Threshold)
	}
	// `seed` and `err` store the error produced by this operation.
	seed, err := reconstructValidatorMPCSeed(shares[:ref.Threshold])
	// `s` tracks the current values while iterating.
	for _, s := range shares {
		ZeroMemory(s.Share)
	}
	if err != nil {
		return nil, nil, ValidatorMPCShareFile{}, err
	}
	// `priv` stores the value produced by this operation.
	priv := ed25519.NewKeyFromSeed(seed)
	defer ZeroMemory(priv)
	// `pub` stores the value produced by this operation.
	pub := append(ed25519.PublicKey(nil), priv.Public().(ed25519.PublicKey)...)
	if hex.EncodeToString(pub) != normalizeConsensusPubKeyHex(ref.PublicKeyHex) {
		ZeroMemory(seed)
		return nil, nil, ValidatorMPCShareFile{}, errors.New("reconstructed mpc seed does not match public key")
	}
	return pub, seed, ref, nil
}

// splitValidatorMPCSeed implements the split validator mpc seed helper.
func splitValidatorMPCSeed(seed []byte, threshold, participants int) ([]validatorMPCPlainShare, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("invalid seed length")
	}
	// `out` stores the result produced by this operation.
	out := make([]validatorMPCPlainShare, participants)
	// `i` tracks the current position in the related collection.
	for i := range out {
		out[i] = validatorMPCPlainShare{Participant: i + 1, Share: make([]byte, len(seed))}
	}
	// `byteIdx` and `secretByte` track the current position in the related collection.
	for byteIdx, secretByte := range seed {
		// `coeff` stores the value produced by this operation.
		coeff := make([]byte, threshold)
		coeff[0] = secretByte
		if threshold > 1 {
			// `err` stores the error produced by this operation.
			if _, err := cryptorand.Read(coeff[1:]); err != nil {
				return nil, err
			}
		}
		// `i` tracks the current position in the related collection.
		for i := range out {
			out[i].Share[byteIdx] = gf256PolyEval(coeff, byte(i+1))
		}
		ZeroMemory(coeff)
	}
	return out, nil
}

// reconstructValidatorMPCSeed implements the reconstruct validator mpc seed helper.
func reconstructValidatorMPCSeed(shares []validatorMPCPlainShare) ([]byte, error) {
	if len(shares) == 0 {
		return nil, errors.New("no mpc shares")
	}
	// `shareLen` stores the measured quantity used by this operation.
	shareLen := len(shares[0].Share)
	if shareLen != ed25519.SeedSize {
		return nil, errors.New("invalid mpc share length")
	}
	// `xs` stores the value produced by this operation.
	xs := make([]byte, len(shares))
	// `i` and `share` track the current position in the related collection.
	for i, share := range shares {
		if share.Participant <= 0 || share.Participant > 255 {
			return nil, errors.New("invalid mpc participant")
		}
		if len(share.Share) != shareLen {
			return nil, errors.New("inconsistent mpc share lengths")
		}
		xs[i] = byte(share.Participant)
		// `j` stores the current position in the related collection.
		for j := 0; j < i; j++ {
			if xs[j] == xs[i] {
				return nil, errors.New("duplicate mpc participant")
			}
		}
	}
	// `seed` stores the value produced by this operation.
	seed := make([]byte, shareLen)
	// `byteIdx` stores the current position in the related collection.
	for byteIdx := 0; byteIdx < shareLen; byteIdx++ {
		// `secret` stores the value used by this operation.
		var secret byte
		// `i` and `share` track the current position in the related collection.
		for i, share := range shares {
			// `li` stores the current position in the related collection.
			li := byte(1)
			// `j` tracks the current position in the related collection.
			for j := range shares {
				if i == j {
					continue
				}
				li = gf256Mul(li, gf256Div(xs[j], xs[j]^xs[i]))
			}
			secret ^= gf256Mul(share.Share[byteIdx], li)
		}
		seed[byteIdx] = secret
	}
	return seed, nil
}

// gf256PolyEval implements the gf256 poly eval helper.
func gf256PolyEval(coeff []byte, x byte) byte {
	// `y` stores the value used by this operation.
	var y byte
	// `i` stores the current position in the related collection.
	for i := len(coeff) - 1; i >= 0; i-- {
		y = gf256Mul(y, x) ^ coeff[i]
	}
	return y
}

// gf256Mul implements the gf256 mul helper.
func gf256Mul(a, b byte) byte {
	// `p` stores the value used by this operation.
	var p byte
	for b != 0 {
		if b&1 != 0 {
			p ^= a
		}
		// `hi` stores the current position in the related collection.
		hi := a & 0x80
		a <<= 1
		if hi != 0 {
			a ^= 0x1b
		}
		b >>= 1
	}
	return p
}

// gf256Pow implements the gf256 pow helper.
func gf256Pow(a byte, n int) byte {
	// `result` stores the result produced by this operation.
	result := byte(1)
	for n > 0 {
		if n&1 == 1 {
			result = gf256Mul(result, a)
		}
		a = gf256Mul(a, a)
		n >>= 1
	}
	return result
}

// gf256Inv implements the gf256 inv helper.
func gf256Inv(a byte) byte {
	if a == 0 {
		return 0
	}
	return gf256Pow(a, 254)
}

// gf256Div implements the gf256 div helper.
func gf256Div(a, b byte) byte {
	if b == 0 {
		return 0
	}
	return gf256Mul(a, gf256Inv(b))
}

// validatorMPCSignRequestFromReader implements the validator mpc sign request from reader helper.
func validatorMPCSignRequestFromReader(r io.Reader) (validatorHSMRequest, error) {
	// `raw` and `err` store the error produced by this operation.
	raw, err := io.ReadAll(r)
	if err != nil {
		return validatorHSMRequest{}, err
	}
	raw = bytes.TrimSpace(raw)
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return validatorHSMRequest{}, errors.New("missing signer request JSON on stdin")
	}
	// `req` stores the request data being processed.
	var req validatorHSMRequest
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &req); err != nil {
		return validatorHSMRequest{}, err
	}
	if normalizeConsensusPubKeyHex(req.PublicKeyHex) == "" {
		return validatorHSMRequest{}, errors.New("signer request missing public_key_hex")
	}
	if strings.TrimSpace(req.PayloadHex) == "" {
		return validatorHSMRequest{}, errors.New("signer request missing payload_hex")
	}
	return req, nil
}
