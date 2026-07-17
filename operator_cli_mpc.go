package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type operatorStringListFlag []string

// String implements the string helper.
func (f *operatorStringListFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

// Set implements the set helper.
func (f *operatorStringListFlag) Set(v string) error {
	// `part` tracks the current values while iterating.
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*f = append(*f, part)
		}
	}
	return nil
}

// operatorValidatorMPCKeygenCommand implements the operator validator mpc keygen command helper.
func operatorValidatorMPCKeygenCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("validator mpc-keygen", flag.ContinueOnError)
	// `validatorID` stores whether the related condition is satisfied.
	validatorID := fs.String("validator", "", "validator id")
	// `idAlias` stores the current position in the related collection.
	idAlias := fs.String("id", "", "validator id alias")
	// `outDir` stores the result produced by this operation.
	outDir := fs.String("outdir", "", "output directory for validator.pub and share*.sec")
	// `threshold` stores the value produced by this operation.
	threshold := fs.Int("threshold", 2, "threshold shares required to sign")
	// `participants` stores the value produced by this operation.
	participants := fs.Int("participants", 3, "total share count")
	// `passwordEnv` stores the value produced by this operation.
	passwordEnv := fs.String("password-env", operatorMPCSharePasswordEnv, "MPC share password environment variable")
	// `force` stores the value produced by this operation.
	force := fs.Bool("force", false, "overwrite existing MPC share files")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `id` stores the current position in the related collection.
	id := strings.TrimSpace(*validatorID)
	if id == "" {
		id = strings.TrimSpace(*idAlias)
	}
	if id == "" {
		return errors.New("--validator required")
	}
	// `dest` stores the value produced by this operation.
	dest := strings.TrimSpace(*outDir)
	if dest == "" {
		dest = filepath.Join("data", normalizeValidatorID(id), "mpc")
	}
	// `password` and `err` store the error produced by this operation.
	password, err := operatorReadNewPassword("New MPC share password: ", "Confirm MPC share password: ", *passwordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)
	// `result` and `err` store the error produced by this operation.
	result, err := writeValidatorMPCShares(id, dest, *threshold, *participants, password, *force)
	if err != nil {
		return err
	}
	operatorPrintJSON(result)
	return nil
}

// operatorValidatorMPCImportKeyCommand implements the operator validator mpc import key command helper.
func operatorValidatorMPCImportKeyCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("validator mpc-import-key", flag.ContinueOnError)
	// `validatorID` stores whether the related condition is satisfied.
	validatorID := fs.String("validator", "", "validator id")
	// `idAlias` stores the current position in the related collection.
	idAlias := fs.String("id", "", "validator id alias")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory")
	// `nodePathFlag` stores the value produced by this operation.
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	// `outDir` stores the result produced by this operation.
	outDir := fs.String("outdir", "", "output directory for validator.pub and share*.sec")
	// `threshold` stores the value produced by this operation.
	threshold := fs.Int("threshold", 2, "threshold shares required to sign")
	// `participants` stores the value produced by this operation.
	participants := fs.Int("participants", 3, "total share count")
	// `validatorPasswordEnvFlag` stores whether the related condition is satisfied.
	validatorPasswordEnvFlag := fs.String("validator-password-env", validatorPasswordEnv, "validator.sec password environment variable")
	// `sharePasswordEnv` stores the value produced by this operation.
	sharePasswordEnv := fs.String("password-env", operatorMPCSharePasswordEnv, "MPC share password environment variable")
	// `force` stores the value produced by this operation.
	force := fs.Bool("force", false, "overwrite existing MPC share files")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `id` stores the current position in the related collection.
	id := strings.TrimSpace(*validatorID)
	if id == "" {
		id = strings.TrimSpace(*idAlias)
	}
	// `nodePath`, `normalizedID`, and `err` store the error produced by this operation.
	nodePath, normalizedID, err := operatorValidatorNodePath(id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	// `dest` stores the value produced by this operation.
	dest := strings.TrimSpace(*outDir)
	if dest == "" {
		dest = filepath.Join(filepath.Dir(nodePath), "mpc")
	}
	// `keyPath` stores the key used to access the related value.
	keyPath := filepath.Join(nodePath, "validator.sec")
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	// `enc` stores the value used by this operation.
	var enc SecureWallet
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &enc); err != nil {
		return fmt.Errorf("decode validator.sec: %w", err)
	}
	// `validatorPassword` and `err` store the error produced by this operation.
	validatorPassword, err := operatorReadExistingPassword("Validator key password: ", *validatorPasswordEnvFlag)
	if err != nil {
		return err
	}
	defer operatorZeroString(&validatorPassword)
	// `priv` and `err` store the error produced by this operation.
	priv, err := DecryptPrivateKey(enc, validatorPassword)
	if err != nil {
		return err
	}
	defer ZeroMemory(priv)
	if len(priv) != ed25519.PrivateKeySize {
		return errors.New("invalid validator private key length")
	}
	// `sharePassword` and `err` store the error produced by this operation.
	sharePassword, err := operatorReadNewPassword("New MPC share password: ", "Confirm MPC share password: ", *sharePasswordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&sharePassword)
	// `seed` stores the value produced by this operation.
	seed := priv.Seed()
	defer ZeroMemory(seed)
	// `result` and `err` store the error produced by this operation.
	result, err := writeValidatorMPCSharesFromSeed(normalizedID, dest, *threshold, *participants, sharePassword, *force, seed)
	if err != nil {
		return err
	}
	result.Warning = "Existing validator.sec migrated to MPC shares for the same public key. Keep validator.sec offline as break-glass backup; do not run MPC and software signing at the same time."
	operatorPrintJSON(result)
	return nil
}

// operatorValidatorMPCPubkeyCommand implements the operator validator mpc pubkey command helper.
func operatorValidatorMPCPubkeyCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("validator mpc-pubkey", flag.ContinueOnError)
	// `pubFile` stores the value produced by this operation.
	pubFile := fs.String("pub", "", "validator.pub file from mpc-keygen")
	// `shareFile` stores the value produced by this operation.
	shareFile := fs.String("share", "", "MPC share file")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `pub`, `source`, and `err` store the error produced by this operation.
	pub, source, err := operatorReadMPCPublicKey(*pubFile, *shareFile)
	if err != nil {
		return err
	}
	operatorPrintJSON(map[string]any{
		"public_key":  pub,
		"fingerprint": validatorKeyFingerprint(mustDecodeOperatorPubkey(pub)),
		"source":      operatorCleanPath(source),
	})
	return nil
}

// operatorValidatorCreateMPCCommand implements the operator validator create mpc command helper.
func operatorValidatorCreateMPCCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("validator create-mpc", flag.ContinueOnError)
	// `walletPath` stores the value produced by this operation.
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	// `validatorID` stores whether the related condition is satisfied.
	validatorID := fs.String("validator", "", "validator id")
	// `validatorPubkey` stores whether the related condition is satisfied.
	validatorPubkey := fs.String("validator-pubkey", "", "validator consensus pubkey hex")
	// `pubFile` stores the value produced by this operation.
	pubFile := fs.String("mpc-pub", "", "validator.pub file from mpc-keygen")
	// `shareFile` stores the value produced by this operation.
	shareFile := fs.String("share", "", "MPC share file used only to read public key")
	// `amount` stores the value produced by this operation.
	amount := fs.Int("amount", int(ValidatorMinStake), "stake amount")
	// `lockEpochs` stores the synchronization state protecting shared data.
	lockEpochs := fs.Uint64("lock-epochs", DefaultStakeLockEpochs, "stake lock epochs")
	// `coin` stores the value produced by this operation.
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	// `passwordEnv` stores the value produced by this operation.
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "wallet password environment variable")
	// `rpcFlags` stores the value produced by this operation.
	rpcFlags := registerOperatorRPCFlags(fs)
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*validatorID) == "" {
		return errors.New("--validator required")
	}
	// `pub` stores the value produced by this operation.
	pub := normalizeConsensusPubKeyHex(*validatorPubkey)
	if pub == "" {
		// `discovered` and `err` store the error produced by this operation.
		discovered, _, err := operatorReadMPCPublicKey(*pubFile, *shareFile)
		if err != nil {
			return err
		}
		pub = normalizeConsensusPubKeyHex(discovered)
	}
	if pub == "" {
		return errors.New("--validator-pubkey or --mpc-pub required")
	}
	// `wallet`, `password`, and `err` store the error produced by this operation.
	wallet, password, err := operatorLoadWalletAndPassword(*walletPath, *passwordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)
	// `currentNonce` and `err` store the error produced by this operation.
	currentNonce, err := operatorFetchCurrentNonce(rpcFlags, wallet.Address)
	if err != nil {
		return err
	}
	// `tx` and `err` store the error produced by this operation.
	tx, err := BuildSignedStakeTxSecure(wallet, password, *validatorID, pub, *amount, currentNonce, *coin, *lockEpochs)
	if err != nil {
		return err
	}
	// `resp` and `err` store the error produced by this operation.
	resp, err := operatorSubmitTx(rpcFlags, tx)
	if err != nil {
		return err
	}
	resp["command"] = "validator create-mpc"
	resp["validator"] = normalizeValidatorID(*validatorID)
	resp["validator_pubkey"] = pub
	resp["mpc"] = true
	operatorPrintJSON(resp)
	return nil
}

// operatorValidatorMPCSignCommand implements the operator validator mpc sign command helper.
func operatorValidatorMPCSignCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("validator mpc-sign", flag.ContinueOnError)
	// `shares` stores the result produced by this operation.
	var shares operatorStringListFlag
	fs.Var(&shares, "share", "MPC share file; repeat or comma-separate")
	fs.Var(&shares, "shares", "comma-separated MPC share files")
	// `passwordEnv` stores the value produced by this operation.
	passwordEnv := fs.String("password-env", operatorMPCSharePasswordEnv, "MPC share password environment variable")
	// `passwordFile` stores the value produced by this operation.
	passwordFile := fs.String("password-file", "", "file containing MPC share password")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(shares) == 0 {
		return errors.New("--share or --shares required")
	}
	// `password` and `err` store the error produced by this operation.
	password, err := operatorReadMPCSharePassword(*passwordEnv, *passwordFile)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)
	// `req` and `err` store the error produced by this operation.
	req, err := validatorMPCSignRequestFromReader(os.Stdin)
	if err != nil {
		return err
	}
	// `pub`, `seed`, `ref`, and `err` store the error produced by this operation.
	pub, seed, ref, err := reconstructValidatorMPCSeedFromFiles(shares, password)
	if err != nil {
		return err
	}
	defer ZeroMemory(seed)
	if normalizeConsensusPubKeyHex(req.PublicKeyHex) != hex.EncodeToString(pub) {
		return errors.New("mpc share public key does not match signer request")
	}
	// `payload` and `err` store the error produced by this operation.
	payload, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(req.PayloadHex), "0x"), "0X"))
	if err != nil {
		return fmt.Errorf("invalid payload_hex: %w", err)
	}
	// `priv` stores the value produced by this operation.
	priv := ed25519.NewKeyFromSeed(seed)
	defer ZeroMemory(priv)
	// `sig` stores the value produced by this operation.
	sig := ed25519.Sign(priv, payload)
	operatorPrintJSON(map[string]any{
		"signature_hex": hex.EncodeToString(sig),
		"validator_id":  ref.ValidatorID,
		"mpc_threshold": ref.Threshold,
	})
	return nil
}

// operatorReadMPCSharePassword implements the operator read mpc share password helper.
func operatorReadMPCSharePassword(envName, passwordFile string) (string, error) {
	if strings.TrimSpace(passwordFile) != "" {
		// `raw` and `err` store the error produced by this operation.
		raw, err := os.ReadFile(operatorResolvePath(passwordFile))
		if err != nil {
			return "", err
		}
		// `password` stores the value produced by this operation.
		password := strings.TrimSpace(string(raw))
		if password == "" {
			return "", errors.New("mpc share password file is empty")
		}
		return password, nil
	}
	return operatorReadExistingPassword("MPC share password: ", envName)
}

// operatorReadMPCPublicKey implements the operator read mpc public key helper.
func operatorReadMPCPublicKey(pubFile, shareFile string) (string, string, error) {
	if strings.TrimSpace(pubFile) != "" {
		// `path` stores the value produced by this operation.
		path := operatorResolvePath(pubFile)
		// `raw` and `err` store the error produced by this operation.
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", "", err
		}
		// `pub` stores the value produced by this operation.
		pub := normalizeConsensusPubKeyHex(string(raw))
		if pub == "" {
			return "", "", errors.New("invalid mpc public key file")
		}
		return pub, path, nil
	}
	if strings.TrimSpace(shareFile) != "" {
		// `path` stores the value produced by this operation.
		path := operatorResolvePath(shareFile)
		// `raw` and `err` store the error produced by this operation.
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", "", err
		}
		// `share` stores the value used by this operation.
		var share ValidatorMPCShareFile
		// `err` stores the error produced by this operation.
		if err := json.Unmarshal(raw, &share); err != nil {
			return "", "", err
		}
		// `pub` stores the value produced by this operation.
		pub := normalizeConsensusPubKeyHex(share.PublicKeyHex)
		if pub == "" {
			return "", "", errors.New("invalid mpc share public key")
		}
		return pub, path, nil
	}
	return "", "", errors.New("--mpc-pub/--pub or --share required")
}

// mustDecodeOperatorPubkey implements the must decode operator pubkey helper.
func mustDecodeOperatorPubkey(pub string) []byte {
	// `raw` and `err` store the error produced by this operation.
	raw, err := hex.DecodeString(normalizeConsensusPubKeyHex(pub))
	if err != nil {
		return nil
	}
	return raw
}
