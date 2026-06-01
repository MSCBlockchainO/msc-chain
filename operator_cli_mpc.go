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

func (f *operatorStringListFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *operatorStringListFlag) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*f = append(*f, part)
		}
	}
	return nil
}

func operatorValidatorMPCKeygenCommand(args []string) error {
	fs := flag.NewFlagSet("validator mpc-keygen", flag.ContinueOnError)
	validatorID := fs.String("validator", "", "validator id")
	idAlias := fs.String("id", "", "validator id alias")
	outDir := fs.String("outdir", "", "output directory for validator.pub and share*.sec")
	threshold := fs.Int("threshold", 2, "threshold shares required to sign")
	participants := fs.Int("participants", 3, "total share count")
	passwordEnv := fs.String("password-env", operatorMPCSharePasswordEnv, "MPC share password environment variable")
	force := fs.Bool("force", false, "overwrite existing MPC share files")
	if err := fs.Parse(args); err != nil {
		return err
	}
	id := strings.TrimSpace(*validatorID)
	if id == "" {
		id = strings.TrimSpace(*idAlias)
	}
	if id == "" {
		return errors.New("--validator required")
	}
	dest := strings.TrimSpace(*outDir)
	if dest == "" {
		dest = filepath.Join("data", normalizeValidatorID(id), "mpc")
	}
	password, err := operatorReadNewPassword("New MPC share password: ", "Confirm MPC share password: ", *passwordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)
	result, err := writeValidatorMPCShares(id, dest, *threshold, *participants, password, *force)
	if err != nil {
		return err
	}
	operatorPrintJSON(result)
	return nil
}

func operatorValidatorMPCPubkeyCommand(args []string) error {
	fs := flag.NewFlagSet("validator mpc-pubkey", flag.ContinueOnError)
	pubFile := fs.String("pub", "", "validator.pub file from mpc-keygen")
	shareFile := fs.String("share", "", "MPC share file")
	if err := fs.Parse(args); err != nil {
		return err
	}
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

func operatorValidatorCreateMPCCommand(args []string) error {
	fs := flag.NewFlagSet("validator create-mpc", flag.ContinueOnError)
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	validatorID := fs.String("validator", "", "validator id")
	validatorPubkey := fs.String("validator-pubkey", "", "validator consensus pubkey hex")
	pubFile := fs.String("mpc-pub", "", "validator.pub file from mpc-keygen")
	shareFile := fs.String("share", "", "MPC share file used only to read public key")
	amount := fs.Int("amount", int(ValidatorMinStake), "stake amount")
	lockEpochs := fs.Uint64("lock-epochs", DefaultStakeLockEpochs, "stake lock epochs")
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "wallet password environment variable")
	rpcFlags := registerOperatorRPCFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*validatorID) == "" {
		return errors.New("--validator required")
	}
	pub := normalizeConsensusPubKeyHex(*validatorPubkey)
	if pub == "" {
		discovered, _, err := operatorReadMPCPublicKey(*pubFile, *shareFile)
		if err != nil {
			return err
		}
		pub = normalizeConsensusPubKeyHex(discovered)
	}
	if pub == "" {
		return errors.New("--validator-pubkey or --mpc-pub required")
	}
	wallet, password, err := operatorLoadWalletAndPassword(*walletPath, *passwordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)
	currentNonce, err := operatorFetchCurrentNonce(rpcFlags, wallet.Address)
	if err != nil {
		return err
	}
	tx, err := BuildSignedStakeTxSecure(wallet, password, *validatorID, pub, *amount, currentNonce, *coin, *lockEpochs)
	if err != nil {
		return err
	}
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

func operatorValidatorMPCSignCommand(args []string) error {
	fs := flag.NewFlagSet("validator mpc-sign", flag.ContinueOnError)
	var shares operatorStringListFlag
	fs.Var(&shares, "share", "MPC share file; repeat or comma-separate")
	fs.Var(&shares, "shares", "comma-separated MPC share files")
	passwordEnv := fs.String("password-env", operatorMPCSharePasswordEnv, "MPC share password environment variable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(shares) == 0 {
		return errors.New("--share or --shares required")
	}
	password, err := operatorReadExistingPassword("MPC share password: ", *passwordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)
	req, err := validatorMPCSignRequestFromReader(os.Stdin)
	if err != nil {
		return err
	}
	pub, seed, ref, err := reconstructValidatorMPCSeedFromFiles(shares, password)
	if err != nil {
		return err
	}
	defer ZeroMemory(seed)
	if normalizeConsensusPubKeyHex(req.PublicKeyHex) != hex.EncodeToString(pub) {
		return errors.New("mpc share public key does not match signer request")
	}
	payload, err := hex.DecodeString(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(req.PayloadHex), "0x"), "0X"))
	if err != nil {
		return fmt.Errorf("invalid payload_hex: %w", err)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	defer ZeroMemory(priv)
	sig := ed25519.Sign(priv, payload)
	operatorPrintJSON(map[string]any{
		"signature_hex": hex.EncodeToString(sig),
		"validator_id":  ref.ValidatorID,
		"mpc_threshold": ref.Threshold,
	})
	return nil
}

func operatorReadMPCPublicKey(pubFile, shareFile string) (string, string, error) {
	if strings.TrimSpace(pubFile) != "" {
		path := operatorResolvePath(pubFile)
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", "", err
		}
		pub := normalizeConsensusPubKeyHex(string(raw))
		if pub == "" {
			return "", "", errors.New("invalid mpc public key file")
		}
		return pub, path, nil
	}
	if strings.TrimSpace(shareFile) != "" {
		path := operatorResolvePath(shareFile)
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", "", err
		}
		var share ValidatorMPCShareFile
		if err := json.Unmarshal(raw, &share); err != nil {
			return "", "", err
		}
		pub := normalizeConsensusPubKeyHex(share.PublicKeyHex)
		if pub == "" {
			return "", "", errors.New("invalid mpc share public key")
		}
		return pub, path, nil
	}
	return "", "", errors.New("--mpc-pub/--pub or --share required")
}

func mustDecodeOperatorPubkey(pub string) []byte {
	raw, err := hex.DecodeString(normalizeConsensusPubKeyHex(pub))
	if err != nil {
		return nil
	}
	return raw
}
