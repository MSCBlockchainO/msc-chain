package main

import (
	"bytes"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tyler-smith/go-bip39"
)

const operatorWalletPasswordEnv = "MSC_WALLET_PASSWORD"

type operatorRPCFlags struct {
	rpc       string
	token     string
	basicUser string
	basicPass string
	timeout   time.Duration
}

func runOperatorCLI(args []string) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	if !isOperatorCLICommand(cmd) {
		return false, 0
	}
	if err := operatorRun(cmd, args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return true, 1
	}
	return true, 0
}

func isOperatorCLICommand(cmd string) bool {
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "wallet", "balance", "send", "validator-keygen", "validator-pubkey", "validator", "stake", "unstake", "claim-rewards", "status", "peers", "sync-status", "backup", "snapshot", "help":
		return true
	default:
		return false
	}
}

func operatorRun(cmd string, args []string) error {
	switch cmd {
	case "help":
		operatorPrintHelp()
		return nil
	case "wallet":
		return operatorWalletCommand(args)
	case "balance":
		return operatorBalanceCommand(args)
	case "send":
		return operatorSendCommand(args)
	case "validator-keygen":
		return operatorValidatorKeygenCommand(args)
	case "validator-pubkey":
		return operatorValidatorPubkeyCommand(args)
	case "validator":
		return operatorValidatorCommand(args)
	case "stake":
		return operatorStakeCommand(args)
	case "unstake":
		return operatorUnstakeCommand(args)
	case "claim-rewards":
		return operatorClaimRewardsCommand(args)
	case "status":
		return operatorStatusCommand(args, true)
	case "sync-status":
		return operatorStatusCommand(args, false)
	case "peers":
		return operatorPeersCommand(args)
	case "backup":
		return operatorBackupCommand(args)
	case "snapshot":
		return operatorSnapshotCommand(args)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func operatorPrintHelp() {
	fmt.Println("MSC operator CLI")
	fmt.Println()
	fmt.Println("Wallet:")
	fmt.Println("  wallet new --wallet ~/.msc/secure_wallet.json [--show-mnemonic]")
	fmt.Println("  wallet import --private-key <hex> --wallet ~/.msc/secure_wallet.json")
	fmt.Println("  wallet import --mnemonic \"word ...\" --wallet ~/.msc/secure_wallet.json")
	fmt.Println("  wallet export-public --wallet ~/.msc/secure_wallet.json")
	fmt.Println()
	fmt.Println("Validator:")
	fmt.Println("  validator-keygen --id F --datadir data/F")
	fmt.Println("  validator-pubkey --id F --datadir data/F")
	fmt.Println("  validator create --wallet wallet.json --validator F --validator-pubkey <hex> --amount 100 --rpc http://127.0.0.1:26657")
	fmt.Println("  validator mpc-keygen --validator F --threshold 2 --participants 3 --outdir data/F/mpc")
	fmt.Println("  validator mpc-pubkey --pub data/F/mpc/validator.pub")
	fmt.Println("  validator create-mpc --wallet wallet.json --validator F --mpc-pub data/F/mpc/validator.pub --amount 100 --rpc http://127.0.0.1:26657")
	fmt.Println("  validator mpc-sign --shares data/F/mpc/share1.sec,data/F/mpc/share2.sec  < signer_request.json")
	fmt.Println()
	fmt.Println("Transactions:")
	fmt.Println("  balance --address MSC... --rpc http://127.0.0.1:26657")
	fmt.Println("  send --wallet wallet.json --to MSC... --amount 10 --rpc http://127.0.0.1:26657")
	fmt.Println("  stake --wallet wallet.json --validator F --validator-pubkey <hex> --amount 100 --rpc http://127.0.0.1:26657")
	fmt.Println("  unstake --wallet wallet.json --validator F --amount 100 --rpc http://127.0.0.1:26657")
	fmt.Println("  claim-rewards --wallet wallet.json --rpc http://127.0.0.1:26657")
	fmt.Println()
	fmt.Println("Node:")
	fmt.Println("  status --rpc http://127.0.0.1:26657")
	fmt.Println("  peers --rpc http://127.0.0.1:26657")
	fmt.Println("  sync-status --rpc http://127.0.0.1:26657")
	fmt.Println()
	fmt.Println("Backup / recovery:")
	fmt.Println("  backup export --id A --datadir data/A [--height 0]")
	fmt.Println("  backup verify --path data/A/node_A/backups/backup_...")
	fmt.Println("  backup import --id RESTORE --datadir /tmp/restore --path /tmp/backup --apply")
	fmt.Println("  backup recover --id A --datadir data/A --height 1000")
	fmt.Println("  snapshot export|import|verify ...  (aliases for backup commands)")
}

func operatorWalletCommand(args []string) error {
	if len(args) == 0 {
		operatorPrintHelp()
		return nil
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "new":
		return operatorWalletNew(args[1:])
	case "import":
		return operatorWalletImport(args[1:])
	case "export-public":
		return operatorWalletExportPublic(args[1:])
	default:
		return fmt.Errorf("unknown wallet command %q", sub)
	}
}

func operatorWalletNew(args []string) error {
	fs := flag.NewFlagSet("wallet new", flag.ContinueOnError)
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	showMnemonic := fs.Bool("show-mnemonic", false, "print the mnemonic once")
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	password, err := operatorReadNewPassword("New wallet password: ", "Confirm wallet password: ", *passwordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)

	wallet, mnemonic, err := operatorCreateWallet(password)
	if err != nil {
		return err
	}
	if err := operatorSaveSecureWallet(*walletPath, wallet); err != nil {
		return err
	}
	out := operatorPublicWalletInfo(wallet)
	out["wallet_path"] = operatorCleanPath(*walletPath)
	if *showMnemonic {
		out["mnemonic"] = strings.Join(mnemonic, " ")
		out["mnemonic_warning"] = "secret backup; keep offline"
	} else {
		out["mnemonic"] = "hidden; rerun with --show-mnemonic only on a private terminal if you need seed backup"
	}
	operatorPrintJSON(out)
	return nil
}

func operatorWalletImport(args []string) error {
	fs := flag.NewFlagSet("wallet import", flag.ContinueOnError)
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	privateKeyHex := fs.String("private-key", "", "ed25519 private key hex (32-byte seed or 64-byte private key)")
	privateKeyFile := fs.String("private-key-file", "", "file containing private key hex")
	mnemonic := fs.String("mnemonic", "", "BIP39 mnemonic")
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	password, err := operatorReadNewPassword("Wallet password: ", "Confirm wallet password: ", *passwordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)

	var wallet SecureWallet
	switch {
	case strings.TrimSpace(*mnemonic) != "":
		w, err := RecoverWalletWithPath(strings.TrimSpace(*mnemonic), password, hdDefaultAccount, hdDefaultChange, hdDefaultIndex)
		if err != nil {
			return err
		}
		wallet = w
	case strings.TrimSpace(*privateKeyFile) != "":
		raw, err := os.ReadFile(*privateKeyFile)
		if err != nil {
			return err
		}
		w, err := operatorWalletFromPrivateKeyHex(string(raw), password)
		if err != nil {
			return err
		}
		wallet = w
	case strings.TrimSpace(*privateKeyHex) != "":
		w, err := operatorWalletFromPrivateKeyHex(*privateKeyHex, password)
		if err != nil {
			return err
		}
		wallet = w
	default:
		secret, err := ReadPassword("Private key hex or mnemonic: ")
		if err != nil {
			return err
		}
		defer ZeroMemory(secret)
		raw := strings.TrimSpace(string(secret))
		if strings.Contains(raw, " ") {
			w, err := RecoverWalletWithPath(raw, password, hdDefaultAccount, hdDefaultChange, hdDefaultIndex)
			if err != nil {
				return err
			}
			wallet = w
		} else {
			w, err := operatorWalletFromPrivateKeyHex(raw, password)
			if err != nil {
				return err
			}
			wallet = w
		}
	}

	if err := operatorSaveSecureWallet(*walletPath, wallet); err != nil {
		return err
	}
	out := operatorPublicWalletInfo(wallet)
	out["wallet_path"] = operatorCleanPath(*walletPath)
	operatorPrintJSON(out)
	return nil
}

func operatorWalletExportPublic(args []string) error {
	fs := flag.NewFlagSet("wallet export-public", flag.ContinueOnError)
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	wallet, err := operatorLoadSecureWallet(*walletPath)
	if err != nil {
		return err
	}
	out := operatorPublicWalletInfo(wallet)
	out["wallet_path"] = operatorCleanPath(*walletPath)
	operatorPrintJSON(out)
	return nil
}

func operatorCreateWallet(password string) (SecureWallet, []string, error) {
	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	seed := bip39.NewSeed(mnemonic, password)
	pub, priv, hd, err := deriveHDKeypairFromSeed(seed, hdDefaultAccount, hdDefaultChange, hdDefaultIndex)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	defer ZeroMemory(priv)
	encrypted, err := EncryptPrivateKey(priv, password)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	return SecureWallet{
		Address:   AddressFromPublicKey(pub),
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encrypted,
		HD:        hd,
	}, strings.Split(mnemonic, " "), nil
}

func operatorWalletFromPrivateKeyHex(raw string, password string) (SecureWallet, error) {
	cleaned := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "0x"))
	cleaned = strings.ReplaceAll(cleaned, "\r", "")
	cleaned = strings.ReplaceAll(cleaned, "\n", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	b, err := hex.DecodeString(cleaned)
	if err != nil {
		return SecureWallet{}, fmt.Errorf("invalid private key hex: %w", err)
	}
	var priv ed25519.PrivateKey
	switch len(b) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(b)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(append([]byte(nil), b...))
	default:
		return SecureWallet{}, fmt.Errorf("invalid private key length %d bytes", len(b))
	}
	defer ZeroMemory(priv)
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return SecureWallet{}, errors.New("failed to derive public key")
	}
	encrypted, err := EncryptPrivateKey(priv, password)
	if err != nil {
		return SecureWallet{}, err
	}
	return SecureWallet{
		Address:   AddressFromPublicKey(pub),
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encrypted,
	}, nil
}

func operatorPublicWalletInfo(wallet SecureWallet) map[string]any {
	return map[string]any{
		"address":    displayAddress(wallet.Address),
		"public_key": strings.ToLower(strings.TrimSpace(wallet.PublicKey)),
		"evm_alias":  toEVMHexAddress(wallet.Address),
		"chain_id":   ChainID,
	}
}

func operatorValidatorKeygenCommand(args []string) error {
	fs := flag.NewFlagSet("validator-keygen", flag.ContinueOnError)
	id := fs.String("id", "", "validator id")
	dataDir := fs.String("datadir", "data", "base data directory")
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	nodePath, validatorID, err := operatorValidatorNodePath(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	fp, err := GenerateValidatorKeyOffline(validatorID, nodePath)
	if err != nil {
		return err
	}
	pub, _ := operatorReadValidatorPubkey(nodePath)
	operatorPrintJSON(map[string]any{
		"validator":   normalizeValidatorID(validatorID),
		"fingerprint": fp,
		"public_key":  pub,
		"key_file":    operatorCleanPath(filepath.Join(nodePath, "validator.sec")),
		"public_file": operatorCleanPath(validatorPublicPath(nodePath)),
	})
	return nil
}

func operatorValidatorPubkeyCommand(args []string) error {
	fs := flag.NewFlagSet("validator-pubkey", flag.ContinueOnError)
	id := fs.String("id", "", "validator id")
	dataDir := fs.String("datadir", "data", "base data directory")
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	nodePath, validatorID, err := operatorValidatorNodePath(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	pub, err := operatorReadValidatorPubkey(nodePath)
	if err != nil {
		return err
	}
	operatorPrintJSON(map[string]any{
		"validator":   normalizeValidatorID(validatorID),
		"public_key":  pub,
		"public_file": operatorCleanPath(validatorPublicPath(nodePath)),
	})
	return nil
}

func operatorValidatorCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("validator subcommand required (create, create-mpc, mpc-keygen, mpc-pubkey, mpc-sign)")
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "create":
		return operatorStakeCommand(append([]string{"--create-validator"}, args[1:]...))
	case "create-mpc":
		return operatorValidatorCreateMPCCommand(args[1:])
	case "mpc-keygen":
		return operatorValidatorMPCKeygenCommand(args[1:])
	case "mpc-pubkey":
		return operatorValidatorMPCPubkeyCommand(args[1:])
	case "mpc-sign":
		return operatorValidatorMPCSignCommand(args[1:])
	default:
		return fmt.Errorf("unknown validator command %q", sub)
	}
}

func operatorStakeCommand(args []string) error {
	fs := flag.NewFlagSet("stake", flag.ContinueOnError)
	createValidator := fs.Bool("create-validator", false, "mark this stake as validator creation")
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	validatorID := fs.String("validator", "", "validator id")
	validatorPubkey := fs.String("validator-pubkey", "", "validator consensus pubkey hex")
	dataDir := fs.String("datadir", "data", "base data directory used to auto-read validator pubkey")
	nodePathFlag := fs.String("nodepath", "", "direct node data path used to auto-read validator pubkey")
	amount := fs.Int("amount", int(ValidatorMinStake), "stake amount")
	lockEpochs := fs.Uint64("lock-epochs", DefaultStakeLockEpochs, "stake lock epochs")
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
	rpcFlags := registerOperatorRPCFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*validatorID) == "" {
		return errors.New("--validator required")
	}
	pub := normalizeConsensusPubKeyHex(*validatorPubkey)
	if pub == "" {
		if nodePath, _, err := operatorValidatorNodePath(*validatorID, *dataDir, *nodePathFlag); err == nil {
			if discovered, readErr := operatorReadValidatorPubkey(nodePath); readErr == nil {
				pub = normalizeConsensusPubKeyHex(discovered)
			}
		}
	}
	if pub == "" {
		return errors.New("--validator-pubkey required for first non-core validator stake")
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
	resp["command"] = "stake"
	if *createValidator {
		resp["command"] = "validator create"
	}
	resp["validator"] = normalizeValidatorID(*validatorID)
	resp["validator_pubkey"] = pub
	operatorPrintJSON(resp)
	return nil
}

func operatorUnstakeCommand(args []string) error {
	fs := flag.NewFlagSet("unstake", flag.ContinueOnError)
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	validatorID := fs.String("validator", "", "validator id")
	amount := fs.Int("amount", 0, "unstake amount")
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
	rpcFlags := registerOperatorRPCFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*validatorID) == "" {
		return errors.New("--validator required")
	}
	if *amount <= 0 {
		return errors.New("--amount must be > 0")
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
	tx, err := BuildSignedUnstakeTxSecure(wallet, password, *validatorID, *amount, currentNonce, *coin)
	if err != nil {
		return err
	}
	resp, err := operatorSubmitTx(rpcFlags, tx)
	if err != nil {
		return err
	}
	resp["command"] = "unstake"
	resp["validator"] = normalizeValidatorID(*validatorID)
	operatorPrintJSON(resp)
	return nil
}

func operatorSendCommand(args []string) error {
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	to := fs.String("to", "", "recipient address")
	amount := fs.Int("amount", 0, "amount")
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
	rpcFlags := registerOperatorRPCFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*to) == "" {
		return errors.New("--to required")
	}
	if *amount <= 0 {
		return errors.New("--amount must be > 0")
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
	tx, err := BuildSignedTxSecure(wallet, password, *to, *amount, currentNonce, *coin)
	if err != nil {
		return err
	}
	resp, err := operatorSubmitTx(rpcFlags, tx)
	if err != nil {
		return err
	}
	resp["command"] = "send"
	operatorPrintJSON(resp)
	return nil
}

func operatorClaimRewardsCommand(args []string) error {
	fs := flag.NewFlagSet("claim-rewards", flag.ContinueOnError)
	walletPath := fs.String("wallet", "", "encrypted wallet path used to infer address")
	address := fs.String("address", "", "wallet address")
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	rpcFlags := registerOperatorRPCFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	addr := strings.TrimSpace(*address)
	if addr == "" && strings.TrimSpace(*walletPath) != "" {
		wallet, err := operatorLoadSecureWallet(*walletPath)
		if err != nil {
			return err
		}
		addr = wallet.Address
	}
	if addr == "" {
		return errors.New("--address or --wallet required")
	}
	resp, err := operatorFetchBalance(rpcFlags, addr, *coin)
	if err != nil {
		return err
	}
	resp["command"] = "claim-rewards"
	resp["claim_model"] = "auto_credit"
	resp["note"] = "MSC validator rewards are credited automatically; no separate claim transaction is required."
	operatorPrintJSON(resp)
	return nil
}

func operatorBalanceCommand(args []string) error {
	fs := flag.NewFlagSet("balance", flag.ContinueOnError)
	walletPath := fs.String("wallet", "", "encrypted wallet path used to infer address")
	address := fs.String("address", "", "wallet address")
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	rpcFlags := registerOperatorRPCFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	addr := strings.TrimSpace(*address)
	if addr == "" && strings.TrimSpace(*walletPath) != "" {
		wallet, err := operatorLoadSecureWallet(*walletPath)
		if err != nil {
			return err
		}
		addr = wallet.Address
	}
	if addr == "" {
		return errors.New("--address or --wallet required")
	}
	resp, err := operatorFetchBalance(rpcFlags, addr, *coin)
	if err != nil {
		return err
	}
	operatorPrintJSON(resp)
	return nil
}

func operatorStatusCommand(args []string, fullDefault bool) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	full := fs.Bool("full", fullDefault, "request full status")
	rpcFlags := registerOperatorRPCFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	q := url.Values{}
	if *full {
		q.Set("full", "1")
	}
	resp, err := operatorGET(rpcFlags, "/status", q)
	if err != nil {
		return err
	}
	operatorPrintJSON(resp)
	return nil
}

func operatorPeersCommand(args []string) error {
	fs := flag.NewFlagSet("peers", flag.ContinueOnError)
	rpcFlags := registerOperatorRPCFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	resp, err := operatorGET(rpcFlags, "/v1/peers", nil)
	if err != nil {
		return err
	}
	operatorPrintJSON(resp)
	return nil
}

func operatorBackupCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("backup subcommand required (export/import/verify/recover)")
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "export":
		return operatorBackupExport(args[1:])
	case "import":
		return operatorBackupImport(args[1:])
	case "verify":
		return operatorBackupVerify(args[1:])
	case "recover":
		return operatorBackupRecover(args[1:])
	default:
		return fmt.Errorf("unknown backup command %q", sub)
	}
}

func operatorSnapshotCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("snapshot subcommand required (export/import/verify)")
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "export":
		return operatorBackupExport(args[1:])
	case "import":
		return operatorBackupImport(args[1:])
	case "verify":
		return operatorBackupVerify(args[1:])
	default:
		return fmt.Errorf("unknown snapshot command %q", sub)
	}
}

func operatorBackupExport(args []string) error {
	fs := flag.NewFlagSet("backup export", flag.ContinueOnError)
	id := fs.String("id", "", "node id")
	dataDir := fs.String("datadir", "data", "base data directory")
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	height := fs.Uint64("height", 0, "snapshot height; 0 uses best snapshot")
	reason := fs.String("reason", "operator_backup_export", "backup reason")
	if err := fs.Parse(args); err != nil {
		return err
	}
	node, cleanup, err := operatorOpenRecoveryNode(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	defer cleanup()
	manifest, err := node.ExportSnapshotBackup(*height, *reason)
	if err != nil {
		return err
	}
	operatorPrintJSON(map[string]any{
		"command":       "backup export",
		"height":        manifest.Height,
		"snapshot_hash": manifest.SnapshotHash,
		"state_root":    manifest.StateRoot,
		"backup_dir":    operatorCleanPath(manifest.BackupDir),
		"files":         manifest.Files,
	})
	return nil
}

func operatorBackupImport(args []string) error {
	fs := flag.NewFlagSet("backup import", flag.ContinueOnError)
	id := fs.String("id", "", "node id")
	dataDir := fs.String("datadir", "data", "base data directory")
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	path := fs.String("path", "", "backup directory")
	apply := fs.Bool("apply", true, "apply snapshot after storing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*path) == "" {
		return errors.New("--path required")
	}
	node, cleanup, err := operatorOpenRecoveryNode(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	defer cleanup()
	result, err := node.ImportSnapshotBackup(operatorResolvePath(*path), *apply)
	if err != nil {
		return err
	}
	operatorPrintJSON(map[string]any{
		"command":       "backup import",
		"height":        result.Height,
		"snapshot_hash": result.SnapshotHash,
		"backup_dir":    operatorCleanPath(result.BackupDir),
		"stored":        result.Stored,
		"applied":       result.Applied,
		"node_root":     operatorCleanPath(node.recoveryNodeRoot()),
	})
	return nil
}

func operatorBackupVerify(args []string) error {
	fs := flag.NewFlagSet("backup verify", flag.ContinueOnError)
	path := fs.String("path", "", "backup directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*path) == "" {
		return errors.New("--path required")
	}
	manifest, snapshot, err := verifyRecoveryBackupDir(operatorResolvePath(*path))
	if err != nil {
		return err
	}
	size := uint64(0)
	if manifest.SnapshotManifest != nil {
		size = manifest.SnapshotManifest.SnapshotSizeBytes
	}
	operatorPrintJSON(map[string]any{
		"command":             "backup verify",
		"ok":                  true,
		"height":              manifest.Height,
		"snapshot_hash":       manifest.SnapshotHash,
		"state_root":          manifest.StateRoot,
		"snapshot_size_bytes": size,
		"validated_height":    snapshot.Height,
		"backup_dir":          operatorCleanPath(manifest.BackupDir),
	})
	return nil
}

func operatorBackupRecover(args []string) error {
	fs := flag.NewFlagSet("backup recover", flag.ContinueOnError)
	id := fs.String("id", "", "node id")
	dataDir := fs.String("datadir", "data", "base data directory")
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	height := fs.Uint64("height", 0, "target height")
	allowFinalizedRollback := fs.Bool("allow-finalized-rollback", false, "allow rollback below local finalized height")
	if err := fs.Parse(args); err != nil {
		return err
	}
	node, cleanup, err := operatorOpenRecoveryNode(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	defer cleanup()
	report, err := node.RecoverToPointWithOptions(PointInTimeRecoveryOptions{
		TargetHeight:            *height,
		Apply:                   true,
		AllowFinalizedRollback:  *allowFinalizedRollback,
		VerifyReplayStateRoot:   true,
		RequireContiguousReplay: true,
	})
	if err != nil {
		return err
	}
	operatorPrintJSON(map[string]any{
		"command":         "backup recover",
		"target_height":   report.TargetHeight,
		"base_height":     report.BaseHeight,
		"replayed_blocks": report.ReplayedBlocks,
		"snapshot_hash":   report.SnapshotHash,
		"backup_dir":      operatorCleanPath(report.BackupDir),
		"applied":         report.Applied,
		"node_root":       operatorCleanPath(node.recoveryNodeRoot()),
	})
	return nil
}

func operatorFetchBalance(rpcFlags *operatorRPCFlags, address string, coin string) (map[string]any, error) {
	q := url.Values{}
	q.Set("address", address)
	q.Set("coin", coin)
	return operatorGET(rpcFlags, "/balance", q)
}

func registerOperatorRPCFlags(fs *flag.FlagSet) *operatorRPCFlags {
	flags := &operatorRPCFlags{}
	fs.StringVar(&flags.rpc, "rpc", "http://127.0.0.1:26657", "RPC base URL")
	fs.StringVar(&flags.token, "token", os.Getenv("MSC_RPC_TOKEN"), "RPC bearer token")
	fs.StringVar(&flags.basicUser, "basic-user", os.Getenv("MSC_RPC_BASIC_USER"), "HTTP basic auth user")
	fs.StringVar(&flags.basicPass, "basic-pass", os.Getenv("MSC_RPC_BASIC_PASS"), "HTTP basic auth password")
	fs.DurationVar(&flags.timeout, "timeout", 15*time.Second, "RPC timeout")
	return flags
}

func operatorLoadWalletAndPassword(walletPath, passwordEnv string) (SecureWallet, string, error) {
	wallet, err := operatorLoadSecureWallet(walletPath)
	if err != nil {
		return SecureWallet{}, "", err
	}
	password, err := operatorReadExistingPassword("Wallet password: ", passwordEnv)
	if err != nil {
		return SecureWallet{}, "", err
	}
	return wallet, password, nil
}

func operatorReadExistingPassword(prompt string, envName string) (string, error) {
	if envName != "" {
		if password := os.Getenv(envName); password != "" {
			return password, nil
		}
	}
	raw, err := ReadPassword(prompt)
	if err != nil {
		return "", err
	}
	defer ZeroMemory(raw)
	password := strings.TrimSpace(string(raw))
	if password == "" {
		return "", errors.New("password required")
	}
	return password, nil
}

func operatorReadNewPassword(prompt, confirmPrompt string, envName string) (string, error) {
	if envName != "" {
		if password := os.Getenv(envName); password != "" {
			return password, nil
		}
	}
	password, err := operatorReadExistingPassword(prompt, "")
	if err != nil {
		return "", err
	}
	confirm, err := operatorReadExistingPassword(confirmPrompt, "")
	if err != nil {
		operatorZeroString(&password)
		return "", err
	}
	defer operatorZeroString(&confirm)
	if password != confirm {
		operatorZeroString(&password)
		return "", errors.New("password confirmation mismatch")
	}
	return password, nil
}

func operatorZeroString(v *string) {
	if v != nil {
		*v = ""
	}
}

func operatorLoadSecureWallet(path string) (SecureWallet, error) {
	path = operatorResolvePath(path)
	raw, err := os.ReadFile(path)
	if err != nil {
		return SecureWallet{}, err
	}
	var wallet SecureWallet
	if err := json.Unmarshal(raw, &wallet); err != nil {
		return SecureWallet{}, err
	}
	if strings.TrimSpace(wallet.Address) == "" || strings.TrimSpace(wallet.PublicKey) == "" {
		return SecureWallet{}, errors.New("wallet missing address or public key")
	}
	return wallet, nil
}

func operatorSaveSecureWallet(path string, wallet SecureWallet) error {
	path = operatorResolvePath(path)
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(wallet, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, raw)
}

func operatorValidatorNodePath(id, dataDir, nodePathFlag string) (string, string, error) {
	id = normalizeValidatorID(id)
	if id == "" {
		return "", "", errors.New("--id or --validator required")
	}
	nodePath := strings.TrimSpace(nodePathFlag)
	if nodePath == "" {
		nodePath = nodeDataPath(dataDir, id)
	}
	if abs, err := filepath.Abs(nodePath); err == nil {
		nodePath = abs
	}
	return nodePath, id, nil
}

func operatorRecoveryNodeLocation(id, dataDir, nodePathFlag string) (string, string, string, error) {
	nodePathFlag = strings.TrimSpace(nodePathFlag)
	if nodePathFlag == "" {
		nodePath, nodeID, err := operatorValidatorNodePath(id, dataDir, "")
		if err != nil {
			return "", "", "", err
		}
		return dataDir, nodeID, nodePath, nil
	}
	nodePath := operatorResolvePath(nodePathFlag)
	if abs, err := filepath.Abs(nodePath); err == nil {
		nodePath = abs
	}
	baseName := filepath.Base(nodePath)
	derivedID := strings.TrimPrefix(baseName, "node_")
	nodeID := normalizeValidatorID(id)
	if nodeID == "" {
		nodeID = normalizeValidatorID(derivedID)
	}
	if nodeID == "" || baseName != "node_"+nodeID {
		return "", "", "", fmt.Errorf("--nodepath must point to a node_<id> directory")
	}
	baseDir := filepath.Dir(nodePath)
	return baseDir, nodeID, nodePath, nil
}

func operatorOpenRecoveryNode(id, dataDir, nodePathFlag string) (*Node, func(), error) {
	baseDir, nodeID, nodePath, err := operatorRecoveryNodeLocation(id, dataDir, nodePathFlag)
	if err != nil {
		return nil, nil, err
	}
	db := OpenNodeDB(nodePath)
	node := &Node{
		ID:         nodeID,
		DataDir:    baseDir,
		DB:         db,
		Blockchain: &Blockchain{},
		Ledger:     NewLedger(),
	}
	cleanup := func() {
		if db != nil {
			_ = db.Close()
		}
	}
	return node, cleanup, nil
}

func operatorReadValidatorPubkey(nodePath string) (string, error) {
	pubPath := validatorPublicPath(nodePath)
	if raw, err := os.ReadFile(pubPath); err == nil {
		pub := normalizeConsensusPubKeyHex(string(raw))
		if pub == "" {
			return "", fmt.Errorf("invalid validator public key in %s", pubPath)
		}
		return pub, nil
	}
	keyPath := filepath.Join(nodePath, "validator.sec")
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("validator public key not found; run validator-keygen first")
	}
	var wallet SecureWallet
	if err := json.Unmarshal(raw, &wallet); err != nil {
		return "", err
	}
	pub := normalizeConsensusPubKeyHex(wallet.PublicKey)
	if pub == "" {
		return "", fmt.Errorf("invalid validator public key in %s", keyPath)
	}
	return pub, nil
}

func operatorFetchCurrentNonce(rpcFlags *operatorRPCFlags, address string) (int, error) {
	q := url.Values{}
	q.Set("address", address)
	resp, err := operatorGET(rpcFlags, "/nonce/pending", q)
	if err != nil {
		return 0, err
	}
	next := operatorJSONInt(resp["nonce"])
	if next <= 0 {
		return 0, nil
	}
	return next - 1, nil
}

func operatorSubmitTx(rpcFlags *operatorRPCFlags, tx Transaction) (map[string]any, error) {
	body, err := json.Marshal(tx)
	if err != nil {
		return nil, err
	}
	return operatorJSONRequest(rpcFlags, http.MethodPost, "/v1/submit_tx", nil, body)
}

func operatorGET(rpcFlags *operatorRPCFlags, path string, q url.Values) (map[string]any, error) {
	return operatorJSONRequest(rpcFlags, http.MethodGet, path, q, nil)
}

func operatorJSONRequest(rpcFlags *operatorRPCFlags, method string, path string, q url.Values, body []byte) (map[string]any, error) {
	if rpcFlags == nil {
		rpcFlags = &operatorRPCFlags{}
	}
	target, err := operatorEndpoint(rpcFlags.rpc, path, q)
	if err != nil {
		return nil, err
	}
	timeout := rpcFlags.timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token := strings.TrimSpace(rpcFlags.token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if rpcFlags.basicUser != "" || rpcFlags.basicPass != "" {
		req.SetBasicAuth(rpcFlags.basicUser, rpcFlags.basicPass)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, MaxTxRequestBodyBytes))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s failed: %s", method, target, strings.TrimSpace(string(raw)))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return out, nil
}

func operatorEndpoint(base string, path string, q url.Values) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "http://127.0.0.1:26657"
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(path, "/")
	if q != nil {
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

func operatorJSONInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		i, _ := strconv.Atoi(n.String())
		return i
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

func operatorResolvePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = SecureWalletPath()
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
				return filepath.Join(home, strings.TrimLeft(path[1:], `/\`))
			}
		}
	}
	return filepath.Clean(path)
}

func operatorCleanPath(path string) string {
	if abs, err := filepath.Abs(operatorResolvePath(path)); err == nil {
		return abs
	}
	return operatorResolvePath(path)
}

func operatorPrintJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func generateOperatorTestWallet(password string) (SecureWallet, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	encrypted, err := EncryptPrivateKey(priv, password)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	return SecureWallet{
		Address:   AddressFromPublicKey(pub),
		PublicKey: hex.EncodeToString(pub),
		Crypto:    encrypted,
	}, priv, nil
}
