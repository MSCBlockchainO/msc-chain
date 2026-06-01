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
	operatorMPCSharePasswordEnv = "MSC_MPC_SHARE_PASSWORD"
	validatorMPCShareScheme     = "msc-shamir-ed25519-seed-v1"
	validatorMPCShareAAD        = keyEncryptionAAD + ":mpc-share-v1"
)

type ValidatorMPCShareFile struct {
	Version      int          `json:"version"`
	Scheme       string       `json:"scheme"`
	ValidatorID  string       `json:"validator_id"`
	Participant  int          `json:"participant"`
	Threshold    int          `json:"threshold"`
	Participants int          `json:"participants"`
	PublicKeyHex string       `json:"public_key_hex"`
	Fingerprint  string       `json:"fingerprint"`
	Share        EncryptedKey `json:"share"`
	CreatedAt    int64        `json:"created_at"`
	Warning      string       `json:"warning,omitempty"`
}

type validatorMPCPlainShare struct {
	Participant int
	Share       []byte
}

type validatorMPCKeygenResult struct {
	ValidatorID   string   `json:"validator_id"`
	PublicKeyHex  string   `json:"public_key_hex"`
	Fingerprint   string   `json:"fingerprint"`
	Threshold     int      `json:"threshold"`
	Participants  int      `json:"participants"`
	PublicFile    string   `json:"public_file"`
	ShareFiles    []string `json:"share_files"`
	SignerCommand string   `json:"signer_command"`
	ConfigTOML    string   `json:"config_toml"`
	Warning       string   `json:"warning"`
}

func generateValidatorMPCShares(validatorID string, threshold, participants int) (ed25519.PublicKey, []validatorMPCPlainShare, error) {
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
	seed := make([]byte, ed25519.SeedSize)
	if _, err := cryptorand.Read(seed); err != nil {
		return nil, nil, err
	}
	defer ZeroMemory(seed)
	priv := ed25519.NewKeyFromSeed(seed)
	defer ZeroMemory(priv)
	pub := append(ed25519.PublicKey(nil), priv.Public().(ed25519.PublicKey)...)
	shares, err := splitValidatorMPCSeed(seed, threshold, participants)
	if err != nil {
		return nil, nil, err
	}
	return pub, shares, nil
}

func writeValidatorMPCShares(validatorID string, outDir string, threshold, participants int, password string, force bool) (validatorMPCKeygenResult, error) {
	id := normalizeValidatorID(validatorID)
	if id == "" {
		return validatorMPCKeygenResult{}, errors.New("validator id is required")
	}
	if strings.TrimSpace(password) == "" {
		return validatorMPCKeygenResult{}, errors.New("mpc share password required")
	}
	outDir = operatorResolvePath(outDir)
	if outDir == "" {
		outDir = filepath.Join("data", id, "mpc")
	}
	if err := ensurePrivateDirectory(outDir); err != nil {
		return validatorMPCKeygenResult{}, err
	}
	pub, shares, err := generateValidatorMPCShares(id, threshold, participants)
	if err != nil {
		return validatorMPCKeygenResult{}, err
	}
	pubHex := hex.EncodeToString(pub)
	fp := validatorKeyFingerprint(pub)
	if fp == "" {
		return validatorMPCKeygenResult{}, errors.New("failed to compute validator public key fingerprint")
	}
	pubFile := filepath.Join(outDir, "validator.pub")
	if !force {
		if _, err := os.Stat(pubFile); err == nil {
			return validatorMPCKeygenResult{}, fmt.Errorf("%s already exists; use --force to overwrite", pubFile)
		} else if !os.IsNotExist(err) {
			return validatorMPCKeygenResult{}, err
		}
	}
	if err := writePrivateFile(pubFile, []byte(pubHex+"\n")); err != nil {
		return validatorMPCKeygenResult{}, err
	}
	now := time.Now().Unix()
	shareFiles := make([]string, 0, len(shares))
	for _, plain := range shares {
		sharePath := filepath.Join(outDir, fmt.Sprintf("share%d.sec", plain.Participant))
		if !force {
			if _, err := os.Stat(sharePath); err == nil {
				return validatorMPCKeygenResult{}, fmt.Errorf("%s already exists; use --force to overwrite", sharePath)
			} else if !os.IsNotExist(err) {
				return validatorMPCKeygenResult{}, err
			}
		}
		enc, err := encryptValidatorMPCSecret(plain.Share, password)
		if err != nil {
			return validatorMPCKeygenResult{}, err
		}
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
		raw, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			return validatorMPCKeygenResult{}, err
		}
		if err := writePrivateFile(sharePath, raw); err != nil {
			return validatorMPCKeygenResult{}, err
		}
		ZeroMemory(plain.Share)
		shareFiles = append(shareFiles, operatorCleanPath(sharePath))
	}
	cleanPubFile := operatorCleanPath(pubFile)
	commandShares := make([]string, 0, threshold)
	for i := 0; i < threshold && i < len(shareFiles); i++ {
		commandShares = append(commandShares, shareFiles[i])
	}
	signerCommand := fmt.Sprintf("./msc-node validator mpc-sign --shares %s --password-env %s", strings.Join(commandShares, ","), operatorMPCSharePasswordEnv)
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
		Warning:       "Built-in share files are for dev/test or controlled operator rehearsal. For production large validators, use an audited external MPC/DKG signer and the same mpc_external_signer_command contract.",
	}, nil
}

func encryptValidatorMPCSecret(secret []byte, password string) (EncryptedKey, error) {
	if len(secret) == 0 {
		return EncryptedKey{}, errors.New("empty mpc secret")
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

func decryptValidatorMPCSecret(enc EncryptedKey, password string) ([]byte, error) {
	if !isEncryptedKeyV2(enc) {
		return nil, errors.New("mpc shares require v2 argon2id encryption")
	}
	salt, err := hex.DecodeString(enc.Salt)
	if err != nil {
		return nil, err
	}
	nonce, err := hex.DecodeString(enc.Nonce)
	if err != nil {
		return nil, err
	}
	ciphertext, err := hex.DecodeString(enc.Ciphertext)
	if err != nil {
		return nil, err
	}
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
	plain, err := gcm.Open(nil, nonce, ciphertext, []byte(validatorMPCShareAAD))
	if err != nil {
		return nil, errors.New("invalid password or corrupted mpc share")
	}
	return plain, nil
}

func readValidatorMPCShare(path string, password string) (ValidatorMPCShareFile, []byte, error) {
	raw, err := os.ReadFile(operatorResolvePath(path))
	if err != nil {
		return ValidatorMPCShareFile{}, nil, err
	}
	var file ValidatorMPCShareFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return ValidatorMPCShareFile{}, nil, err
	}
	if file.Version != 1 || file.Scheme != validatorMPCShareScheme {
		return ValidatorMPCShareFile{}, nil, errors.New("unsupported mpc share file")
	}
	if file.Participant <= 0 || file.Participant > 255 {
		return ValidatorMPCShareFile{}, nil, errors.New("invalid mpc participant index")
	}
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

func reconstructValidatorMPCSeedFromFiles(paths []string, password string) (ed25519.PublicKey, []byte, ValidatorMPCShareFile, error) {
	if len(paths) == 0 {
		return nil, nil, ValidatorMPCShareFile{}, errors.New("at least one mpc share path required")
	}
	files := make([]ValidatorMPCShareFile, 0, len(paths))
	shares := make([]validatorMPCPlainShare, 0, len(paths))
	seen := map[int]bool{}
	for _, path := range paths {
		file, share, err := readValidatorMPCShare(path, password)
		if err != nil {
			for _, s := range shares {
				ZeroMemory(s.Share)
			}
			return nil, nil, ValidatorMPCShareFile{}, err
		}
		if seen[file.Participant] {
			ZeroMemory(share)
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
	ref := files[0]
	for _, file := range files[1:] {
		if file.ValidatorID != ref.ValidatorID || file.PublicKeyHex != ref.PublicKeyHex || file.Threshold != ref.Threshold || file.Participants != ref.Participants || file.Fingerprint != ref.Fingerprint {
			for _, s := range shares {
				ZeroMemory(s.Share)
			}
			return nil, nil, ValidatorMPCShareFile{}, errors.New("mpc shares belong to different validator ceremonies")
		}
	}
	if len(shares) < ref.Threshold {
		for _, s := range shares {
			ZeroMemory(s.Share)
		}
		return nil, nil, ValidatorMPCShareFile{}, fmt.Errorf("not enough mpc shares: got %d need %d", len(shares), ref.Threshold)
	}
	seed, err := reconstructValidatorMPCSeed(shares[:ref.Threshold])
	for _, s := range shares {
		ZeroMemory(s.Share)
	}
	if err != nil {
		return nil, nil, ValidatorMPCShareFile{}, err
	}
	priv := ed25519.NewKeyFromSeed(seed)
	defer ZeroMemory(priv)
	pub := append(ed25519.PublicKey(nil), priv.Public().(ed25519.PublicKey)...)
	if hex.EncodeToString(pub) != normalizeConsensusPubKeyHex(ref.PublicKeyHex) {
		ZeroMemory(seed)
		return nil, nil, ValidatorMPCShareFile{}, errors.New("reconstructed mpc seed does not match public key")
	}
	return pub, seed, ref, nil
}

func splitValidatorMPCSeed(seed []byte, threshold, participants int) ([]validatorMPCPlainShare, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, errors.New("invalid seed length")
	}
	out := make([]validatorMPCPlainShare, participants)
	for i := range out {
		out[i] = validatorMPCPlainShare{Participant: i + 1, Share: make([]byte, len(seed))}
	}
	for byteIdx, secretByte := range seed {
		coeff := make([]byte, threshold)
		coeff[0] = secretByte
		if threshold > 1 {
			if _, err := cryptorand.Read(coeff[1:]); err != nil {
				return nil, err
			}
		}
		for i := range out {
			out[i].Share[byteIdx] = gf256PolyEval(coeff, byte(i+1))
		}
		ZeroMemory(coeff)
	}
	return out, nil
}

func reconstructValidatorMPCSeed(shares []validatorMPCPlainShare) ([]byte, error) {
	if len(shares) == 0 {
		return nil, errors.New("no mpc shares")
	}
	shareLen := len(shares[0].Share)
	if shareLen != ed25519.SeedSize {
		return nil, errors.New("invalid mpc share length")
	}
	xs := make([]byte, len(shares))
	for i, share := range shares {
		if share.Participant <= 0 || share.Participant > 255 {
			return nil, errors.New("invalid mpc participant")
		}
		if len(share.Share) != shareLen {
			return nil, errors.New("inconsistent mpc share lengths")
		}
		xs[i] = byte(share.Participant)
		for j := 0; j < i; j++ {
			if xs[j] == xs[i] {
				return nil, errors.New("duplicate mpc participant")
			}
		}
	}
	seed := make([]byte, shareLen)
	for byteIdx := 0; byteIdx < shareLen; byteIdx++ {
		var secret byte
		for i, share := range shares {
			li := byte(1)
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

func gf256PolyEval(coeff []byte, x byte) byte {
	var y byte
	for i := len(coeff) - 1; i >= 0; i-- {
		y = gf256Mul(y, x) ^ coeff[i]
	}
	return y
}

func gf256Mul(a, b byte) byte {
	var p byte
	for b != 0 {
		if b&1 != 0 {
			p ^= a
		}
		hi := a & 0x80
		a <<= 1
		if hi != 0 {
			a ^= 0x1b
		}
		b >>= 1
	}
	return p
}

func gf256Pow(a byte, n int) byte {
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

func gf256Inv(a byte) byte {
	if a == 0 {
		return 0
	}
	return gf256Pow(a, 254)
}

func gf256Div(a, b byte) byte {
	if b == 0 {
		return 0
	}
	return gf256Mul(a, gf256Inv(b))
}

func validatorMPCSignRequestFromReader(r io.Reader) (validatorHSMRequest, error) {
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
	var req validatorHSMRequest
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
