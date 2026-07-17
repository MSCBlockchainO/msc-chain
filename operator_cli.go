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
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/tyler-smith/go-bip39"
)

// `operatorWalletPasswordEnv` defines the constant value used by this package.
const operatorWalletPasswordEnv = "MSC_WALLET_PASSWORD"

type operatorRPCFlags struct {
	// `rpc` stores the value associated with this record.
	rpc string
	// `token` stores the value associated with this record.
	token string
	// `basicUser` stores the value associated with this record.
	basicUser string
	// `basicPass` stores the value associated with this record.
	basicPass string
	// `timeout` stores the result produced by this operation.
	timeout time.Duration
}

// runOperatorCLI implements the run operator cli helper.
func runOperatorCLI(args []string) (bool, int) {
	if len(args) == 0 {
		return false, 0
	}
	// `cmd` stores the value produced by this operation.
	cmd := strings.ToLower(strings.TrimSpace(args[0]))
	if !isOperatorCLICommand(cmd) {
		return false, 0
	}
	// `err` stores the error produced by this operation.
	if err := operatorRun(cmd, args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return true, 1
	}
	return true, 0
}

// isOperatorCLICommand implements the is operator cli command helper.
func isOperatorCLICommand(cmd string) bool {
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "wallet", "balance", "send", "validator-keygen", "validator-pubkey", "validator", "stake", "unstake", "claim-rewards", "status", "peers", "sync-status", "setup", "install", "doctor", "service", "start", "stop", "repair", "update", "restore", "uninstall", "backup", "snapshot", "storage", "indexer", "version", "help":
		return true
	default:
		return false
	}
}

// operatorRun implements the operator run helper.
func operatorRun(cmd string, args []string) error {
	switch cmd {
	case "help":
		operatorPrintHelp()
		return nil
	case "version":
		return operatorVersionCommand(args)
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
		return operatorStatusAliasCommand(args)
	case "sync-status":
		return operatorStatusCommand(args, false)
	case "peers":
		return operatorPeersCommand(args)
	case "setup":
		return operatorSetupCommand(args)
	case "install":
		return operatorSetupCommand(args)
	case "doctor":
		return operatorDoctorCommand(args)
	case "service":
		return operatorServiceCommand(args)
	case "start":
		return operatorStartCommand(args)
	case "stop":
		return operatorServiceCommand(append([]string{cmd}, args...))
	case "repair":
		return operatorRepairCommand(args)
	case "update":
		return operatorUpdateCommand(args)
	case "restore":
		return operatorRestoreCommand(args)
	case "uninstall":
		return operatorUninstallCommand(args)
	case "backup":
		return operatorBackupCommand(args)
	case "snapshot":
		return operatorSnapshotCommand(args)
	case "storage":
		return operatorStorageCommand(args)
	case "indexer":
		return operatorIndexerCommand(args)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// operatorPrintHelp implements the operator print help helper.
func operatorPrintHelp() {
	fmt.Println("MSC operator CLI")
	fmt.Println()
	fmt.Println("Version:")
	fmt.Println("  version [--json]")
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
	fmt.Println("  validator mpc-import-key --id A --datadir runtime-data/distributed/A --threshold 2 --participants 3 --outdir runtime-data/distributed/A/mpc")
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
	fmt.Println("  start [--mode=full] [--datadir data/node1] [--id my-validator] [--port 7002] [--rpcaddr 127.0.0.1:0]")
	fmt.Println("  install candidate --auto-start")
	fmt.Println("  install validator --id HOME1 --low-ram --auto-start")
	fmt.Println("  setup candidate --auto-start")
	fmt.Println("  doctor --id HOME1 --datadir data [--json]")
	fmt.Println("  stop|status --id HOME1 --install-dir ~/.msc-node")
	fmt.Println("  repair --id HOME1 --install-dir ~/.msc-node")
	fmt.Println("  update --id HOME1 --source auto --install-dir ~/.msc-node")
	fmt.Println("  restore --id HOME1 --backup /path/to/backup --install-dir ~/.msc-node")
	fmt.Println("  uninstall --id HOME1 --install-dir ~/.msc-node")
	fmt.Println("  service start|stop|status --install-dir ~/.msc-node")
	fmt.Println("  status --rpc http://127.0.0.1:26657  (explicit RPC status)")
	fmt.Println("  peers --rpc http://127.0.0.1:26657")
	fmt.Println("  sync-status --rpc http://127.0.0.1:26657")
	fmt.Println()
	fmt.Println("Backup / recovery:")
	fmt.Println("  backup --id A --datadir data --out /offline/msc-backups")
	fmt.Println("  backup export --id A --datadir data/A [--height 0]")
	fmt.Println("  backup verify --path data/A/node_A/backups/backup_...")
	fmt.Println("  backup import --id RESTORE --datadir /tmp/restore --path /tmp/backup --apply")
	fmt.Println("  backup recover --id A --datadir data/A --height 1000")
	fmt.Println("  backup wizard --id A --datadir data")
	fmt.Println("  snapshot export|import|verify ...  (aliases for backup commands)")
	fmt.Println("  storage metrics --path data/A/node_A/meta.db")
	fmt.Println("  storage compact --path data/A/node_A/meta.db --confirm-offline")
	fmt.Println()
	fmt.Println("Archive / indexer:")
	fmt.Println("  indexer run --source http://127.0.0.1:26667 --listen 127.0.0.1:26780 --datadir runtime-data/indexer/INDEXER1")
}

// operatorVersionCommand implements the version command helper.
func operatorVersionCommand(args []string) error {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	info := map[string]any{
		"version":        Version,
		"version_tag":    strings.TrimSpace(BuildVersionTag),
		"git_commit":     strings.TrimSpace(BuildGitCommit),
		"build_date":     strings.TrimSpace(BuildDate),
		"chain_id":       protocolChainID(),
		"network":        protocolNetworkName,
		"genesis_sha256": GenesisHashExpected,
		"goos":           runtime.GOOS,
		"goarch":         runtime.GOARCH,
	}
	if *jsonOut {
		operatorPrintJSON(info)
		return nil
	}
	fmt.Printf("MSC version %s\n", Version)
	if tag := strings.TrimSpace(BuildVersionTag); tag != "" {
		fmt.Printf("release: %s\n", tag)
	}
	if commit := strings.TrimSpace(BuildGitCommit); commit != "" {
		fmt.Printf("commit: %s\n", commit)
	}
	if date := strings.TrimSpace(BuildDate); date != "" {
		fmt.Printf("build_date: %s\n", date)
	}
	fmt.Printf("chain_id: %s\n", protocolChainID())
	fmt.Printf("network: %s\n", protocolNetworkName)
	fmt.Printf("genesis_sha256: %s\n", GenesisHashExpected)
	fmt.Printf("platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}

// operatorWalletCommand implements the operator wallet command helper.
func operatorWalletCommand(args []string) error {
	if len(args) == 0 {
		operatorPrintHelp()
		return nil
	}
	// `sub` stores the value produced by this operation.
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

// operatorWalletNew implements the operator wallet new helper.
func operatorWalletNew(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("wallet new", flag.ContinueOnError)
	// `walletPath` stores the value produced by this operation.
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	// `showMnemonic` stores the value produced by this operation.
	showMnemonic := fs.Bool("show-mnemonic", false, "print the mnemonic once")
	// `passwordEnv` stores the value produced by this operation.
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `password` and `err` store the error produced by this operation.
	password, err := operatorReadNewPassword("New wallet password: ", "Confirm wallet password: ", *passwordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)

	// `wallet`, `mnemonic`, and `err` store the error produced by this operation.
	wallet, mnemonic, err := operatorCreateWallet(password)
	if err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := operatorSaveSecureWallet(*walletPath, wallet); err != nil {
		return err
	}
	// `out` stores the result produced by this operation.
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

// operatorWalletImport implements the operator wallet import helper.
func operatorWalletImport(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("wallet import", flag.ContinueOnError)
	// `walletPath` stores the value produced by this operation.
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	// `privateKeyHex` stores the value produced by this operation.
	privateKeyHex := fs.String("private-key", "", "ed25519 private key hex (32-byte seed or 64-byte private key)")
	// `privateKeyFile` stores the value produced by this operation.
	privateKeyFile := fs.String("private-key-file", "", "file containing private key hex")
	// `mnemonic` stores the value produced by this operation.
	mnemonic := fs.String("mnemonic", "", "BIP39 mnemonic")
	// `passwordEnv` stores the value produced by this operation.
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `password` and `err` store the error produced by this operation.
	password, err := operatorReadNewPassword("Wallet password: ", "Confirm wallet password: ", *passwordEnv)
	if err != nil {
		return err
	}
	defer operatorZeroString(&password)

	// `wallet` stores the value used by this operation.
	var wallet SecureWallet
	switch {
	case strings.TrimSpace(*mnemonic) != "":
		// `w` and `err` store the error produced by this operation.
		w, err := RecoverWalletWithPath(strings.TrimSpace(*mnemonic), password, hdDefaultAccount, hdDefaultChange, hdDefaultIndex)
		if err != nil {
			return err
		}
		wallet = w
	case strings.TrimSpace(*privateKeyFile) != "":
		// `raw` and `err` store the error produced by this operation.
		raw, err := os.ReadFile(*privateKeyFile)
		if err != nil {
			return err
		}
		// `w` and `err` store the error produced by this operation.
		w, err := operatorWalletFromPrivateKeyHex(string(raw), password)
		if err != nil {
			return err
		}
		wallet = w
	case strings.TrimSpace(*privateKeyHex) != "":
		// `w` and `err` store the error produced by this operation.
		w, err := operatorWalletFromPrivateKeyHex(*privateKeyHex, password)
		if err != nil {
			return err
		}
		wallet = w
	default:
		// `secret` and `err` store the error produced by this operation.
		secret, err := ReadPassword("Private key hex or mnemonic: ")
		if err != nil {
			return err
		}
		defer ZeroMemory(secret)
		// `raw` stores the value produced by this operation.
		raw := strings.TrimSpace(string(secret))
		if strings.Contains(raw, " ") {
			// `w` and `err` store the error produced by this operation.
			w, err := RecoverWalletWithPath(raw, password, hdDefaultAccount, hdDefaultChange, hdDefaultIndex)
			if err != nil {
				return err
			}
			wallet = w
		} else {
			// `w` and `err` store the error produced by this operation.
			w, err := operatorWalletFromPrivateKeyHex(raw, password)
			if err != nil {
				return err
			}
			wallet = w
		}
	}

	// `err` stores the error produced by this operation.
	if err := operatorSaveSecureWallet(*walletPath, wallet); err != nil {
		return err
	}
	// `out` stores the result produced by this operation.
	out := operatorPublicWalletInfo(wallet)
	out["wallet_path"] = operatorCleanPath(*walletPath)
	operatorPrintJSON(out)
	return nil
}

// operatorWalletExportPublic implements the operator wallet export public helper.
func operatorWalletExportPublic(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("wallet export-public", flag.ContinueOnError)
	// `walletPath` stores the value produced by this operation.
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `wallet` and `err` store the error produced by this operation.
	wallet, err := operatorLoadSecureWallet(*walletPath)
	if err != nil {
		return err
	}
	// `out` stores the result produced by this operation.
	out := operatorPublicWalletInfo(wallet)
	out["wallet_path"] = operatorCleanPath(*walletPath)
	operatorPrintJSON(out)
	return nil
}

// operatorCreateWallet implements the operator create wallet helper.
func operatorCreateWallet(password string) (SecureWallet, []string, error) {
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
	pub, priv, hd, err := deriveHDKeypairFromSeed(seed, hdDefaultAccount, hdDefaultChange, hdDefaultIndex)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	defer ZeroMemory(priv)
	// `encrypted` and `err` store the error produced by this operation.
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

// operatorWalletFromPrivateKeyHex implements the operator wallet from private key hex helper.
func operatorWalletFromPrivateKeyHex(raw string, password string) (SecureWallet, error) {
	// `cleaned` stores the value produced by this operation.
	cleaned := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "0x"))
	cleaned = strings.ReplaceAll(cleaned, "\r", "")
	cleaned = strings.ReplaceAll(cleaned, "\n", "")
	cleaned = strings.ReplaceAll(cleaned, " ", "")
	// `b` and `err` store the error produced by this operation.
	b, err := hex.DecodeString(cleaned)
	if err != nil {
		return SecureWallet{}, fmt.Errorf("invalid private key hex: %w", err)
	}
	// `priv` stores the value used by this operation.
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
	// `pub` and `ok` store whether the related condition is satisfied.
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(pub) != ed25519.PublicKeySize {
		return SecureWallet{}, errors.New("failed to derive public key")
	}
	// `encrypted` and `err` store the error produced by this operation.
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

// operatorPublicWalletInfo implements the operator public wallet info helper.
func operatorPublicWalletInfo(wallet SecureWallet) map[string]any {
	return map[string]any{
		"address":    displayAddress(wallet.Address),
		"public_key": strings.ToLower(strings.TrimSpace(wallet.PublicKey)),
		"chain_id":   protocolChainID(),
	}
}

// operatorValidatorKeygenCommand implements the operator validator keygen command helper.
func operatorValidatorKeygenCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("validator-keygen", flag.ContinueOnError)
	// `id` stores the current position in the related collection.
	id := fs.String("id", "", "validator id")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory")
	// `nodePathFlag` stores the value produced by this operation.
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `nodePath`, `validatorID`, and `err` store the error produced by this operation.
	nodePath, validatorID, err := operatorValidatorNodePath(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	// `fp` and `err` store the error produced by this operation.
	fp, err := GenerateValidatorKeyOffline(validatorID, nodePath)
	if err != nil {
		return err
	}
	// `pub` stores the value produced by this operation.
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

// operatorValidatorPubkeyCommand implements the operator validator pubkey command helper.
func operatorValidatorPubkeyCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("validator-pubkey", flag.ContinueOnError)
	// `id` stores the current position in the related collection.
	id := fs.String("id", "", "validator id")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory")
	// `nodePathFlag` stores the value produced by this operation.
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `nodePath`, `validatorID`, and `err` store the error produced by this operation.
	nodePath, validatorID, err := operatorValidatorNodePath(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	// `pub` and `err` store the error produced by this operation.
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

// operatorValidatorCommand implements the operator validator command helper.
func operatorValidatorCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("validator subcommand required (create, create-mpc, mpc-keygen, mpc-import-key, mpc-pubkey, mpc-sign)")
	}
	// `sub` stores the value produced by this operation.
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "create":
		return operatorStakeCommand(append([]string{"--create-validator"}, args[1:]...))
	case "create-mpc":
		return operatorValidatorCreateMPCCommand(args[1:])
	case "mpc-keygen":
		return operatorValidatorMPCKeygenCommand(args[1:])
	case "mpc-import-key":
		return operatorValidatorMPCImportKeyCommand(args[1:])
	case "mpc-pubkey":
		return operatorValidatorMPCPubkeyCommand(args[1:])
	case "mpc-sign":
		return operatorValidatorMPCSignCommand(args[1:])
	default:
		return fmt.Errorf("unknown validator command %q", sub)
	}
}

// operatorStakeCommand implements the operator stake command helper.
func operatorStakeCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("stake", flag.ContinueOnError)
	// `createValidator` stores the value produced by this operation.
	createValidator := fs.Bool("create-validator", false, "mark this stake as validator creation")
	// `walletPath` stores the value produced by this operation.
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	// `validatorID` stores whether the related condition is satisfied.
	validatorID := fs.String("validator", "", "validator id")
	// `validatorPubkey` stores whether the related condition is satisfied.
	validatorPubkey := fs.String("validator-pubkey", "", "validator consensus pubkey hex")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory used to auto-read validator pubkey")
	// `nodePathFlag` stores the value produced by this operation.
	nodePathFlag := fs.String("nodepath", "", "direct node data path used to auto-read validator pubkey")
	// `amount` stores the value produced by this operation.
	amount := fs.Int("amount", int(ValidatorMinStake), "stake amount")
	// `lockEpochs` stores the synchronization state protecting shared data.
	lockEpochs := fs.Uint64("lock-epochs", DefaultStakeLockEpochs, "stake lock epochs")
	// `coin` stores the value produced by this operation.
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	// `passwordEnv` stores the value produced by this operation.
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
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
		// `nodePath` and `err` store the error produced by this operation.
		if nodePath, _, err := operatorValidatorNodePath(*validatorID, *dataDir, *nodePathFlag); err == nil {
			// `discovered` and `readErr` store the error produced by this operation.
			if discovered, readErr := operatorReadValidatorPubkey(nodePath); readErr == nil {
				pub = normalizeConsensusPubKeyHex(discovered)
			}
		}
	}
	if pub == "" {
		return errors.New("--validator-pubkey required for first non-core validator stake")
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
	resp["command"] = "stake"
	if *createValidator {
		resp["command"] = "validator create"
	}
	resp["validator"] = normalizeValidatorID(*validatorID)
	resp["validator_pubkey"] = pub
	operatorPrintJSON(resp)
	return nil
}

// operatorUnstakeCommand implements the operator unstake command helper.
func operatorUnstakeCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("unstake", flag.ContinueOnError)
	// `walletPath` stores the value produced by this operation.
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	// `validatorID` stores whether the related condition is satisfied.
	validatorID := fs.String("validator", "", "validator id")
	// `amount` stores the value produced by this operation.
	amount := fs.Int("amount", 0, "unstake amount")
	// `coin` stores the value produced by this operation.
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	// `passwordEnv` stores the value produced by this operation.
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
	// `rpcFlags` stores the value produced by this operation.
	rpcFlags := registerOperatorRPCFlags(fs)
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*validatorID) == "" {
		return errors.New("--validator required")
	}
	if *amount <= 0 {
		return errors.New("--amount must be > 0")
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
	tx, err := BuildSignedUnstakeTxSecure(wallet, password, *validatorID, *amount, currentNonce, *coin)
	if err != nil {
		return err
	}
	// `resp` and `err` store the error produced by this operation.
	resp, err := operatorSubmitTx(rpcFlags, tx)
	if err != nil {
		return err
	}
	resp["command"] = "unstake"
	resp["validator"] = normalizeValidatorID(*validatorID)
	operatorPrintJSON(resp)
	return nil
}

// operatorSendCommand implements the operator send command helper.
func operatorSendCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("send", flag.ContinueOnError)
	// `walletPath` stores the value produced by this operation.
	walletPath := fs.String("wallet", SecureWalletPath(), "encrypted wallet path")
	// `to` stores the value produced by this operation.
	to := fs.String("to", "", "recipient address")
	// `amount` stores the value produced by this operation.
	amount := fs.Int("amount", 0, "amount")
	// `coin` stores the value produced by this operation.
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	// `passwordEnv` stores the value produced by this operation.
	passwordEnv := fs.String("password-env", operatorWalletPasswordEnv, "password environment variable")
	// `rpcFlags` stores the value produced by this operation.
	rpcFlags := registerOperatorRPCFlags(fs)
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*to) == "" {
		return errors.New("--to required")
	}
	if *amount <= 0 {
		return errors.New("--amount must be > 0")
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
	tx, err := BuildSignedTxSecure(wallet, password, *to, *amount, currentNonce, *coin)
	if err != nil {
		return err
	}
	// `resp` and `err` store the error produced by this operation.
	resp, err := operatorSubmitTx(rpcFlags, tx)
	if err != nil {
		return err
	}
	resp["command"] = "send"
	operatorPrintJSON(resp)
	return nil
}

// operatorClaimRewardsCommand implements the operator claim rewards command helper.
func operatorClaimRewardsCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("claim-rewards", flag.ContinueOnError)
	// `walletPath` stores the value produced by this operation.
	walletPath := fs.String("wallet", "", "encrypted wallet path used to infer address")
	// `address` stores the address used by this operation.
	address := fs.String("address", "", "wallet address")
	// `coin` stores the value produced by this operation.
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	// `rpcFlags` stores the value produced by this operation.
	rpcFlags := registerOperatorRPCFlags(fs)
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `addr` stores the address used by this operation.
	addr := strings.TrimSpace(*address)
	if addr == "" && strings.TrimSpace(*walletPath) != "" {
		// `wallet` and `err` store the error produced by this operation.
		wallet, err := operatorLoadSecureWallet(*walletPath)
		if err != nil {
			return err
		}
		addr = wallet.Address
	}
	if addr == "" {
		return errors.New("--address or --wallet required")
	}
	// `resp` and `err` store the error produced by this operation.
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

// operatorBalanceCommand implements the operator balance command helper.
func operatorBalanceCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("balance", flag.ContinueOnError)
	// `walletPath` stores the value produced by this operation.
	walletPath := fs.String("wallet", "", "encrypted wallet path used to infer address")
	// `address` stores the address used by this operation.
	address := fs.String("address", "", "wallet address")
	// `coin` stores the value produced by this operation.
	coin := fs.String("coin", CoinSymbol, "coin symbol")
	// `rpcFlags` stores the value produced by this operation.
	rpcFlags := registerOperatorRPCFlags(fs)
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `addr` stores the address used by this operation.
	addr := strings.TrimSpace(*address)
	if addr == "" && strings.TrimSpace(*walletPath) != "" {
		// `wallet` and `err` store the error produced by this operation.
		wallet, err := operatorLoadSecureWallet(*walletPath)
		if err != nil {
			return err
		}
		addr = wallet.Address
	}
	if addr == "" {
		return errors.New("--address or --wallet required")
	}
	// `resp` and `err` store the error produced by this operation.
	resp, err := operatorFetchBalance(rpcFlags, addr, *coin)
	if err != nil {
		return err
	}
	operatorPrintJSON(resp)
	return nil
}

// operatorStatusCommand implements the operator status command helper.
func operatorStatusCommand(args []string, fullDefault bool) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	// `full` stores the value produced by this operation.
	full := fs.Bool("full", fullDefault, "request full status")
	// `rpcFlags` stores the value produced by this operation.
	rpcFlags := registerOperatorRPCFlags(fs)
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `q` stores the value produced by this operation.
	q := url.Values{}
	if *full {
		q.Set("full", "1")
	}
	// `resp` and `err` store the error produced by this operation.
	resp, err := operatorGET(rpcFlags, "/status", q)
	if err != nil {
		return err
	}
	operatorPrintJSON(resp)
	return nil
}

// operatorPeersCommand implements the operator peers command helper.
func operatorPeersCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("peers", flag.ContinueOnError)
	// `rpcFlags` stores the value produced by this operation.
	rpcFlags := registerOperatorRPCFlags(fs)
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `resp` and `err` store the error produced by this operation.
	resp, err := operatorGET(rpcFlags, "/v1/peers", nil)
	if err != nil {
		return err
	}
	operatorPrintJSON(resp)
	return nil
}

// operatorBackupCommand implements the operator backup command helper.
func operatorBackupCommand(args []string) error {
	if len(args) == 0 {
		return operatorBackupBundle(args)
	}
	if strings.HasPrefix(strings.TrimSpace(args[0]), "-") {
		return operatorBackupBundle(args)
	}
	// `sub` stores the value produced by this operation.
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "wizard":
		return operatorBackupWizard(args[1:])
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

// operatorSnapshotCommand implements the operator snapshot command helper.
func operatorSnapshotCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("snapshot subcommand required (export/import/verify)")
	}
	// `sub` stores the value produced by this operation.
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

// operatorBackupExport implements the operator backup export helper.
func operatorBackupExport(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("backup export", flag.ContinueOnError)
	// `id` stores the current position in the related collection.
	id := fs.String("id", "", "node id")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory")
	// `nodePathFlag` stores the value produced by this operation.
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	// `height` stores the value produced by this operation.
	height := fs.Uint64("height", 0, "snapshot height; 0 uses best snapshot")
	// `reason` stores the value produced by this operation.
	reason := fs.String("reason", "operator_backup_export", "backup reason")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `node`, `cleanup`, and `err` store the error produced by this operation.
	node, cleanup, err := operatorOpenRecoveryNode(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	defer cleanup()
	// `manifest` and `err` store the error produced by this operation.
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

// operatorBackupImport implements the operator backup import helper.
func operatorBackupImport(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("backup import", flag.ContinueOnError)
	// `id` stores the current position in the related collection.
	id := fs.String("id", "", "node id")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory")
	// `nodePathFlag` stores the value produced by this operation.
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	// `path` stores the value produced by this operation.
	path := fs.String("path", "", "backup directory")
	// `apply` stores the value produced by this operation.
	apply := fs.Bool("apply", true, "apply snapshot after storing")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*path) == "" {
		return errors.New("--path required")
	}
	// `node`, `cleanup`, and `err` store the error produced by this operation.
	node, cleanup, err := operatorOpenRecoveryNode(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	defer cleanup()
	// `result` and `err` store the error produced by this operation.
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

// operatorBackupVerify implements the operator backup verify helper.
func operatorBackupVerify(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("backup verify", flag.ContinueOnError)
	// `path` stores the value produced by this operation.
	path := fs.String("path", "", "backup directory")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*path) == "" {
		return errors.New("--path required")
	}
	// `manifest`, `snapshot`, and `err` store the error produced by this operation.
	manifest, snapshot, err := verifyRecoveryBackupDir(operatorResolvePath(*path))
	if err != nil {
		return err
	}
	// `size` stores the measured quantity used by this operation.
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

// operatorBackupRecover implements the operator backup recover helper.
func operatorBackupRecover(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("backup recover", flag.ContinueOnError)
	// `id` stores the current position in the related collection.
	id := fs.String("id", "", "node id")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory")
	// `nodePathFlag` stores the value produced by this operation.
	nodePathFlag := fs.String("nodepath", "", "direct node data path override")
	// `height` stores the value produced by this operation.
	height := fs.Uint64("height", 0, "target height")
	// `allowFinalizedRollback` stores the value produced by this operation.
	allowFinalizedRollback := fs.Bool("allow-finalized-rollback", false, "allow rollback below local finalized height")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `node`, `cleanup`, and `err` store the error produced by this operation.
	node, cleanup, err := operatorOpenRecoveryNode(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	defer cleanup()
	// `report` and `err` store the error produced by this operation.
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

// operatorFetchBalance implements the operator fetch balance helper.
func operatorFetchBalance(rpcFlags *operatorRPCFlags, address string, coin string) (map[string]any, error) {
	// `q` stores the value produced by this operation.
	q := url.Values{}
	q.Set("address", address)
	q.Set("coin", coin)
	return operatorGET(rpcFlags, "/balance", q)
}

// registerOperatorRPCFlags implements the register operator rpc flags helper.
func registerOperatorRPCFlags(fs *flag.FlagSet) *operatorRPCFlags {
	// `flags` stores the value produced by this operation.
	flags := &operatorRPCFlags{}
	fs.StringVar(&flags.rpc, "rpc", "http://127.0.0.1:26657", "RPC base URL")
	fs.StringVar(&flags.token, "token", os.Getenv("MSC_RPC_TOKEN"), "RPC bearer token")
	fs.StringVar(&flags.basicUser, "basic-user", os.Getenv("MSC_RPC_BASIC_USER"), "HTTP basic auth user")
	fs.StringVar(&flags.basicPass, "basic-pass", os.Getenv("MSC_RPC_BASIC_PASS"), "HTTP basic auth password")
	fs.DurationVar(&flags.timeout, "timeout", 15*time.Second, "RPC timeout")
	return flags
}

// operatorLoadWalletAndPassword implements the operator load wallet and password helper.
func operatorLoadWalletAndPassword(walletPath, passwordEnv string) (SecureWallet, string, error) {
	// `wallet` and `err` store the error produced by this operation.
	wallet, err := operatorLoadSecureWallet(walletPath)
	if err != nil {
		return SecureWallet{}, "", err
	}
	// `password` and `err` store the error produced by this operation.
	password, err := operatorReadExistingPassword("Wallet password: ", passwordEnv)
	if err != nil {
		return SecureWallet{}, "", err
	}
	return wallet, password, nil
}

// operatorReadExistingPassword implements the operator read existing password helper.
func operatorReadExistingPassword(prompt string, envName string) (string, error) {
	if envName != "" {
		// `password` stores the value produced by this operation.
		if password := os.Getenv(envName); password != "" {
			return password, nil
		}
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := ReadPassword(prompt)
	if err != nil {
		return "", err
	}
	defer ZeroMemory(raw)
	// `password` stores the value produced by this operation.
	password := strings.TrimSpace(string(raw))
	if password == "" {
		return "", errors.New("password required")
	}
	return password, nil
}

// operatorReadNewPassword implements the operator read new password helper.
func operatorReadNewPassword(prompt, confirmPrompt string, envName string) (string, error) {
	if envName != "" {
		// `password` stores the value produced by this operation.
		if password := os.Getenv(envName); password != "" {
			return password, nil
		}
	}
	// `password` and `err` store the error produced by this operation.
	password, err := operatorReadExistingPassword(prompt, "")
	if err != nil {
		return "", err
	}
	// `confirm` and `err` store the error produced by this operation.
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

// operatorZeroString implements the operator zero string helper.
func operatorZeroString(v *string) {
	if v != nil {
		*v = ""
	}
}

// operatorLoadSecureWallet implements the operator load secure wallet helper.
func operatorLoadSecureWallet(path string) (SecureWallet, error) {
	path = operatorResolvePath(path)
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		return SecureWallet{}, err
	}
	// `wallet` stores the value used by this operation.
	var wallet SecureWallet
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &wallet); err != nil {
		return SecureWallet{}, err
	}
	if strings.TrimSpace(wallet.Address) == "" || strings.TrimSpace(wallet.PublicKey) == "" {
		return SecureWallet{}, errors.New("wallet missing address or public key")
	}
	return wallet, nil
}

// operatorSaveSecureWallet implements the operator save secure wallet helper.
func operatorSaveSecureWallet(path string, wallet SecureWallet) error {
	path = operatorResolvePath(path)
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(wallet, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(path, raw)
}

// operatorValidatorNodePath implements the operator validator node path helper.
func operatorValidatorNodePath(id, dataDir, nodePathFlag string) (string, string, error) {
	id = normalizeValidatorID(id)
	if id == "" {
		return "", "", errors.New("--id or --validator required")
	}
	// `nodePath` stores the value produced by this operation.
	nodePath := strings.TrimSpace(nodePathFlag)
	if nodePath == "" {
		nodePath = nodeDataPath(dataDir, id)
	}
	// `abs` and `err` store the error produced by this operation.
	if abs, err := filepath.Abs(nodePath); err == nil {
		nodePath = abs
	}
	return nodePath, id, nil
}

// operatorRecoveryNodeLocation implements the operator recovery node location helper.
func operatorRecoveryNodeLocation(id, dataDir, nodePathFlag string) (string, string, string, error) {
	nodePathFlag = strings.TrimSpace(nodePathFlag)
	if nodePathFlag == "" {
		// `nodePath`, `nodeID`, and `err` store the error produced by this operation.
		nodePath, nodeID, err := operatorValidatorNodePath(id, dataDir, "")
		if err != nil {
			return "", "", "", err
		}
		return dataDir, nodeID, nodePath, nil
	}
	// `nodePath` stores the value produced by this operation.
	nodePath := operatorResolvePath(nodePathFlag)
	// `abs` and `err` store the error produced by this operation.
	if abs, err := filepath.Abs(nodePath); err == nil {
		nodePath = abs
	}
	// `baseName` stores the value produced by this operation.
	baseName := filepath.Base(nodePath)
	// `derivedID` stores the value produced by this operation.
	derivedID := strings.TrimPrefix(baseName, "node_")
	// `nodeID` stores the value produced by this operation.
	nodeID := normalizeValidatorID(id)
	if nodeID == "" {
		nodeID = normalizeValidatorID(derivedID)
	}
	if nodeID == "" || baseName != "node_"+nodeID {
		return "", "", "", fmt.Errorf("--nodepath must point to a node_<id> directory")
	}
	// `baseDir` stores the value produced by this operation.
	baseDir := filepath.Dir(nodePath)
	return baseDir, nodeID, nodePath, nil
}

// operatorOpenRecoveryNode implements the operator open recovery node helper.
func operatorOpenRecoveryNode(id, dataDir, nodePathFlag string) (*Node, func(), error) {
	// `baseDir`, `nodeID`, `nodePath`, and `err` store the error produced by this operation.
	baseDir, nodeID, nodePath, err := operatorRecoveryNodeLocation(id, dataDir, nodePathFlag)
	if err != nil {
		return nil, nil, err
	}
	// `db` stores the value produced by this operation.
	db := OpenNodeDB(nodePath)
	// `node` stores the value produced by this operation.
	node := &Node{
		ID:         nodeID,
		DataDir:    baseDir,
		DB:         db,
		Blockchain: &Blockchain{},
		Ledger:     NewLedger(),
	}
	// `cleanup` stores the value produced by this operation.
	cleanup := func() {
		if db != nil {
			_ = db.Close()
		}
	}
	return node, cleanup, nil
}

// operatorReadValidatorPubkey implements the operator read validator pubkey helper.
func operatorReadValidatorPubkey(nodePath string) (string, error) {
	// `pubPath` stores the value produced by this operation.
	pubPath := validatorPublicPath(nodePath)
	// `raw` and `err` store the error produced by this operation.
	if raw, err := os.ReadFile(pubPath); err == nil {
		// `pub` stores the value produced by this operation.
		pub := normalizeConsensusPubKeyHex(string(raw))
		if pub == "" {
			return "", fmt.Errorf("invalid validator public key in %s", pubPath)
		}
		return pub, nil
	}
	// `keyPath` stores the key used to access the related value.
	keyPath := filepath.Join(nodePath, "validator.sec")
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return "", fmt.Errorf("validator public key not found; run validator-keygen first")
	}
	// `wallet` stores the value used by this operation.
	var wallet SecureWallet
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &wallet); err != nil {
		return "", err
	}
	// `pub` stores the value produced by this operation.
	pub := normalizeConsensusPubKeyHex(wallet.PublicKey)
	if pub == "" {
		return "", fmt.Errorf("invalid validator public key in %s", keyPath)
	}
	return pub, nil
}

// operatorFetchCurrentNonce implements the operator fetch current nonce helper.
func operatorFetchCurrentNonce(rpcFlags *operatorRPCFlags, address string) (int, error) {
	// `q` stores the value produced by this operation.
	q := url.Values{}
	q.Set("address", address)
	// `resp` and `err` store the error produced by this operation.
	resp, err := operatorGET(rpcFlags, "/nonce/pending", q)
	if err != nil {
		return 0, err
	}
	// `next` stores the value produced by this operation.
	next := operatorJSONInt(resp["nonce"])
	if next <= 0 {
		return 0, nil
	}
	return next - 1, nil
}

// operatorSubmitTx implements the operator submit tx helper.
func operatorSubmitTx(rpcFlags *operatorRPCFlags, tx Transaction) (map[string]any, error) {
	// `body` and `err` store the error produced by this operation.
	body, err := json.Marshal(tx)
	if err != nil {
		return nil, err
	}
	return operatorJSONRequest(rpcFlags, http.MethodPost, "/v1/submit_tx", nil, body)
}

// operatorGET implements the operator get helper.
func operatorGET(rpcFlags *operatorRPCFlags, path string, q url.Values) (map[string]any, error) {
	return operatorJSONRequest(rpcFlags, http.MethodGet, path, q, nil)
}

// operatorJSONRequest implements the operator json request helper.
func operatorJSONRequest(rpcFlags *operatorRPCFlags, method string, path string, q url.Values, body []byte) (map[string]any, error) {
	if rpcFlags == nil {
		rpcFlags = &operatorRPCFlags{}
	}
	// `target` and `err` store the error produced by this operation.
	target, err := operatorEndpoint(rpcFlags.rpc, path, q)
	if err != nil {
		return nil, err
	}
	// `timeout` stores the result produced by this operation.
	timeout := rpcFlags.timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	// `client` stores the value produced by this operation.
	client := &http.Client{Timeout: timeout}
	// `reader` stores the value used by this operation.
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	// `req` and `err` store the error produced by this operation.
	req, err := http.NewRequest(method, target, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// `token` stores the value produced by this operation.
	if token := strings.TrimSpace(rpcFlags.token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if rpcFlags.basicUser != "" || rpcFlags.basicPass != "" {
		req.SetBasicAuth(rpcFlags.basicUser, rpcFlags.basicPass)
	}
	// `res` and `err` store the error produced by this operation.
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	// `raw` stores the value produced by this operation.
	raw, _ := io.ReadAll(io.LimitReader(res.Body, MaxTxRequestBodyBytes))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s failed: %s", method, target, strings.TrimSpace(string(raw)))
	}
	// `out` stores the result produced by this operation.
	var out map[string]any
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return out, nil
}

// operatorEndpoint implements the operator endpoint helper.
func operatorEndpoint(base string, path string, q url.Values) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "http://127.0.0.1:26657"
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	// `u` and `err` store the error produced by this operation.
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

// operatorJSONInt implements the operator json int helper.
func operatorJSONInt(v any) int {
	// `n` stores the value produced by this operation.
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case json.Number:
		// `i` stores the current position in the related collection.
		i, _ := strconv.Atoi(n.String())
		return i
	case string:
		// `i` stores the current position in the related collection.
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

// operatorResolvePath implements the operator resolve path helper.
func operatorResolvePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = SecureWalletPath()
	}
	if strings.HasPrefix(path, "~") {
		// `home` and `err` store the error produced by this operation.
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

// operatorCleanPath implements the operator clean path helper.
func operatorCleanPath(path string) string {
	// `abs` and `err` store the error produced by this operation.
	if abs, err := filepath.Abs(operatorResolvePath(path)); err == nil {
		return abs
	}
	return operatorResolvePath(path)
}

// operatorPrintJSON implements the operator print json helper.
func operatorPrintJSON(v any) {
	// `enc` stores the value produced by this operation.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// generateOperatorTestWallet implements the generate operator test wallet helper.
func generateOperatorTestWallet(password string) (SecureWallet, ed25519.PrivateKey, error) {
	// `pub`, `priv`, and `err` store the error produced by this operation.
	pub, priv, err := ed25519.GenerateKey(cryptorand.Reader)
	if err != nil {
		return SecureWallet{}, nil, err
	}
	// `encrypted` and `err` store the error produced by this operation.
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
