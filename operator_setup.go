package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// `operatorReleaseManifestURLEnv` defines the constant value used by this package.
	operatorReleaseManifestURLEnv = "MSC_RELEASE_MANIFEST_URL"
	// `operatorReleasePublicKeyEnv` defines the constant value used by this package.
	operatorReleasePublicKeyEnv = "MSC_RELEASE_PUBLIC_KEY"
	// `operatorInstallManifestFile` defines the constant value used by this package.
	operatorInstallManifestFile = "install_manifest.json"
)

type operatorSetupOptions struct {
	// `Role` stores the value associated with this record.
	Role string
	// `NodeID` stores the value associated with this record.
	NodeID string
	// `RPC` stores the value associated with this record.
	RPC string
	// `Peers` stores the value associated with this record.
	Peers string
	// `InstallDir` stores the current position in the related collection.
	InstallDir string
	// `LowRAM` stores the value associated with this record.
	LowRAM bool
	// `AutoStart` stores the value associated with this record.
	AutoStart bool
	// `PublicGateway` stores the value associated with this record.
	PublicGateway bool
	// `Allow4GBValidator` stores the value associated with this record.
	Allow4GBValidator bool
	// `Source` stores the value associated with this record.
	Source string
	// `ReleaseManifestURL` stores the value associated with this record.
	ReleaseManifestURL string
	// `ReleasePublicKey` stores the key used to access the related value.
	ReleasePublicKey string
	// `DryRun` stores the value associated with this record.
	DryRun bool
	// `Fresh` stores the value associated with this record.
	Fresh bool
	// `ConfirmDeleteNode` stores the value associated with this record.
	ConfirmDeleteNode string
	// `JSON` stores the current position in the related collection.
	JSON bool
}

type operatorInstallManifest struct {
	// `SchemaVersion` stores the value associated with this record.
	SchemaVersion int `json:"schema_version"`
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id"`
	// `NetworkNodeID` stores the network identity advertised over P2P.
	NetworkNodeID string `json:"network_node_id,omitempty"`
	// `ValidatorID` stores the consensus identity derived from the validator public key.
	ValidatorID string `json:"validator_id,omitempty"`
	// `WalletAddress` stores the account identity derived from the node wallet key.
	WalletAddress string `json:"wallet_address,omitempty"`
	// `WalletPublicKey` stores the public key for the node wallet identity.
	WalletPublicKey string `json:"wallet_public_key,omitempty"`
	// `Role` stores the value associated with this record.
	Role string `json:"role"`
	// `InstallDir` stores the current position in the related collection.
	InstallDir string `json:"install_dir"`
	// `DataDir` stores the value associated with this record.
	DataDir string `json:"data_dir"`
	// `NodePath` stores the value associated with this record.
	NodePath string `json:"node_path"`
	// `ConfigPath` stores the configuration used by this operation.
	ConfigPath string `json:"config_path"`
	// `BinaryPath` stores the value associated with this record.
	BinaryPath string `json:"binary_path"`
	// `AliasPath` stores the value associated with this record.
	AliasPath string `json:"alias_path"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash"`
	// `ValidatorPubkey` stores whether the related condition is satisfied.
	ValidatorPubkey string `json:"validator_pubkey,omitempty"`
	// `ValidatorFingerprint` stores whether the related condition is satisfied.
	ValidatorFingerprint string `json:"validator_fingerprint,omitempty"`
	// `ServiceName` stores the value associated with this record.
	ServiceName string `json:"service_name"`
	// `OS` stores the value associated with this record.
	OS string `json:"os"`
	// `Arch` stores the value associated with this record.
	Arch string `json:"arch"`
	// `Source` stores the value associated with this record.
	Source string `json:"source"`
	// `VersionTag` stores the value associated with this record.
	VersionTag string `json:"version_tag,omitempty"`
	// `UpdatedAt` stores the value associated with this record.
	UpdatedAt string `json:"updated_at"`
}

type operatorHardwareReport struct {
	// `OS` stores the value associated with this record.
	OS string `json:"os"`
	// `Arch` stores the value associated with this record.
	Arch string `json:"arch"`
	// `CPUs` stores the value associated with this record.
	CPUs int `json:"cpus"`
	// `MemoryBytes` stores the value associated with this record.
	MemoryBytes uint64 `json:"memory_bytes,omitempty"`
	// `DiskBytes` stores the value associated with this record.
	DiskBytes uint64 `json:"disk_free_bytes,omitempty"`
}

// operatorSetupCommand implements the operator setup command helper.
func operatorSetupCommand(args []string) error {
	// `roleFromArg` stores the value produced by this operation.
	roleFromArg := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		roleFromArg = strings.TrimSpace(args[0])
		args = args[1:]
	}
	// `opts` stores the value produced by this operation.
	opts := operatorSetupOptions{}
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.StringVar(&opts.Role, "role", roleFromArg, "full, candidate, or validator")
	fs.StringVar(&opts.NodeID, "id", "", "node id")
	fs.StringVar(&opts.RPC, "rpc", "127.0.0.1:26657", "local RPC listen address")
	fs.StringVar(&opts.Peers, "peers", "", "comma-separated persistent peers")
	fs.StringVar(&opts.InstallDir, "install-dir", operatorDefaultInstallDir(), "install directory")
	fs.BoolVar(&opts.LowRAM, "low-ram", false, "enable home low-RAM profile")
	fs.BoolVar(&opts.AutoStart, "auto-start", false, "start after setup")
	fs.BoolVar(&opts.PublicGateway, "public-gateway", false, "configure public gateway mode")
	fs.BoolVar(&opts.Allow4GBValidator, "allow-4gb-validator", false, "explicitly allow 4GB validator mode")
	fs.StringVar(&opts.Source, "source", "auto", "auto, local, or release")
	fs.StringVar(&opts.ReleaseManifestURL, "release-manifest-url", os.Getenv(operatorReleaseManifestURLEnv), "release manifest URL")
	fs.StringVar(&opts.ReleasePublicKey, "release-public-key", os.Getenv(operatorReleasePublicKeyEnv), "ed25519 public key for signed release artifacts")
	fs.BoolVar(&opts.DryRun, "dry-run", false, "print setup plan without running installer")
	fs.BoolVar(&opts.Fresh, "fresh", false, "request a fresh install; existing data still requires explicit uninstall --purge-data")
	fs.StringVar(&opts.ConfirmDeleteNode, "confirm-delete-node-id", "", "required confirmation for destructive fresh reinstall")
	fs.BoolVar(&opts.JSON, "json", true, "print JSON summary")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts.Role = operatorNormalizeSetupRole(opts.Role)
	if opts.Role == "" {
		return errors.New("setup role required: full, candidate, or validator")
	}
	opts.Source = operatorNormalizeSetupSource(opts.Source, opts.ReleaseManifestURL, opts.ReleasePublicKey)
	if opts.Source == "" {
		return errors.New("--source must be auto, local, or release")
	}
	if runtime.GOOS == "darwin" {
		return errors.New("macOS setup is planned but not supported in this release; use Windows or Ubuntu/Linux")
	}
	// `existingManifest` and `manifestFound` store whether the related condition is satisfied.
	existingManifest, manifestFound := operatorReadInstallManifest(opts.InstallDir)
	opts.NodeID = strings.TrimSpace(opts.NodeID)
	if opts.NodeID != "" {
		opts.NodeID = normalizeNodeIdentityID(opts.NodeID)
	} else if manifestFound && strings.TrimSpace(existingManifest.NodeID) != "" {
		opts.NodeID = strings.TrimSpace(existingManifest.NodeID)
	} else {
		if opts.DryRun {
			nodeID, err := generateRandomNodeID()
			if err != nil {
				return fmt.Errorf("node identity: %w", err)
			}
			opts.NodeID = nodeID
		} else {
			identity, err := resolveStartupNodeIdentity("", filepath.Join(operatorResolvePath(opts.InstallDir), "data"), false, true)
			if err != nil {
				return fmt.Errorf("node identity: %w", err)
			}
			opts.NodeID = identity.NodeID
		}
	}
	if manifestFound {
		if existingManifest.NodeID != "" && !strings.EqualFold(existingManifest.NodeID, opts.NodeID) {
			return fmt.Errorf("install dir already belongs to node %s; choose another --install-dir or restore that node", existingManifest.NodeID)
		}
		if existingManifest.Role != "" && opts.Role == "full" && existingManifest.Role == "validator" {
			opts.Role = existingManifest.Role
		}
	}
	// `installHasData` stores the current position in the related collection.
	installHasData := operatorInstallHasProtectedData(opts.InstallDir)
	if opts.Fresh {
		if !strings.EqualFold(strings.TrimSpace(opts.ConfirmDeleteNode), opts.NodeID) {
			return fmt.Errorf("--fresh requires --confirm-delete-node-id %s", opts.NodeID)
		}
		if installHasData {
			return errors.New("fresh reinstall refused while protected data exists; run backup, then uninstall --purge-data --confirm-delete-node-id <id> first")
		}
	}
	// `scriptPath` and `err` store the error produced by this operation.
	scriptPath, err := operatorInstallerScriptPath(runtime.GOOS)
	if err != nil {
		return err
	}
	// `hw` stores the value produced by this operation.
	hw := operatorDetectHardware(opts.InstallDir)
	// `prebuilt` stores the value produced by this operation.
	prebuilt := ""
	// `cleanup` stores the value produced by this operation.
	cleanup := func() {}
	if opts.Source == "release" && !opts.DryRun {
		// `path`, `releaseCleanup`, and `err` store the error produced by this operation.
		path, releaseCleanup, err := operatorDownloadReleaseBinary(opts.ReleaseManifestURL, opts.ReleasePublicKey, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return err
		}
		prebuilt = path
		cleanup = releaseCleanup
	}
	defer cleanup()

	// `cmdName` and `cmdArgs` store the value produced by this operation.
	cmdName, cmdArgs := operatorBuildSetupInstallerCommand(scriptPath, opts, prebuilt)
	// `action` stores the value produced by this operation.
	action := "install"
	if manifestFound || installHasData {
		action = "repair"
	}
	// `plan` stores the value produced by this operation.
	plan := map[string]any{
		"command":      "setup",
		"action":       action,
		"role":         opts.Role,
		"node_id":      opts.NodeID,
		"source":       opts.Source,
		"script":       operatorCleanPath(scriptPath),
		"dry_run":      opts.DryRun,
		"hardware":     hw,
		"exec":         append([]string{cmdName}, cmdArgs...),
		"rpc_private":  !opts.PublicGateway,
		"dashboard":    "http://" + strings.TrimPrefix(opts.RPC, "http://"),
		"install_dir":  operatorCleanPath(opts.InstallDir),
		"release_mode": opts.Source == "release",
		"preserved":    operatorInstallPreservationReport(opts.InstallDir),
	}
	if opts.DryRun {
		operatorPrintJSON(plan)
		return nil
	}
	// `cmd` stores the value produced by this operation.
	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	// `err` stores the error produced by this operation.
	if err := cmd.Run(); err != nil {
		return err
	}
	// `manifest` and `err` store the error produced by this operation.
	manifest, err := operatorBuildInstallManifest(opts)
	if err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := operatorWriteInstallManifest(opts.InstallDir, manifest); err != nil {
		return err
	}
	plan["install_manifest"] = operatorCleanPath(operatorInstallManifestPath(opts.InstallDir))
	plan["validator_pubkey"] = manifest.ValidatorPubkey
	plan["service_enabled"] = opts.AutoStart
	operatorPrintJSON(plan)
	return nil
}

// operatorStatusAliasCommand implements the operator status alias command helper.
func operatorStatusAliasCommand(args []string) error {
	if operatorStatusArgsAreRPC(args) {
		return operatorStatusCommand(args, true)
	}
	return operatorServiceCommand(append([]string{"status"}, args...))
}

// operatorStartCommand starts a node directly with the auto-identity startup flow.
func operatorStartCommand(args []string) error {
	dryRun := false
	nodeArgs := make([]string, 0, len(args)+1)
	hasMode := false
	hasRPCAddr := false
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		if name == "dry-run" {
			dryRun = true
			continue
		}
		if strings.HasPrefix(name, "mode") {
			hasMode = true
		}
		if strings.HasPrefix(name, "rpcaddr") {
			hasRPCAddr = true
		}
		nodeArgs = append(nodeArgs, args[i])
	}
	if !hasMode {
		nodeArgs = append([]string{"--mode=full"}, nodeArgs...)
	}
	if !hasRPCAddr {
		nodeArgs = append(nodeArgs, "--rpcaddr=127.0.0.1:0")
	}
	exe, err := os.Executable()
	if err != nil || strings.TrimSpace(exe) == "" {
		exe = os.Args[0]
	}
	out := map[string]any{
		"command": "start",
		"mode":    "direct-node",
		"exec":    append([]string{exe}, nodeArgs...),
		"dry_run": dryRun,
	}
	if dryRun {
		operatorPrintJSON(out)
		return nil
	}
	cmd := exec.Command(exe, nodeArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// operatorStatusArgsAreRPC implements the operator status args are rpc helper.
func operatorStatusArgsAreRPC(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// `arg` tracks the current values while iterating.
	for _, arg := range args {
		// `name` stores the value produced by this operation.
		name := strings.TrimLeft(strings.TrimSpace(arg), "-")
		switch name {
		case "rpc", "token", "basic-user", "basic-pass", "timeout", "full":
			return true
		}
	}
	return false
}

// operatorRepairCommand implements the operator repair command helper.
func operatorRepairCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("repair", flag.ContinueOnError)
	// `nodeID` stores the value produced by this operation.
	nodeID := fs.String("id", "", "node id")
	// `installDir` stores the current position in the related collection.
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	// `source` stores the value produced by this operation.
	source := fs.String("source", "local", "auto, local, or release")
	// `autoStart` stores the value produced by this operation.
	autoStart := fs.Bool("auto-start", false, "start after repair")
	// `jsonOut` stores the current position in the related collection.
	jsonOut := fs.Bool("json", true, "print JSON")
	// `dryRun` stores the value produced by this operation.
	dryRun := fs.Bool("dry-run", false, "print repair plan")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `manifest` and `found` store whether the related condition is satisfied.
	manifest, found := operatorReadInstallManifest(*installDir)
	// `id` stores the current position in the related collection.
	id := normalizeNodeIdentityID(*nodeID)
	if id == "" {
		id = manifest.NodeID
	}
	if id == "" {
		return errors.New("--id required when install manifest is missing")
	}
	// `role` stores the value produced by this operation.
	role := manifest.Role
	if role == "" {
		role = "candidate"
	}
	// `setupArgs` stores the value produced by this operation.
	setupArgs := []string{role, "--id", id, "--install-dir", *installDir, "--source", *source}
	if *autoStart {
		setupArgs = append(setupArgs, "--auto-start")
	}
	if *dryRun {
		setupArgs = append(setupArgs, "--dry-run")
	}
	if *jsonOut {
		setupArgs = append(setupArgs, "--json")
	}
	if !found {
		setupArgs = append(setupArgs, "--role", role)
	}
	return operatorSetupCommand(setupArgs)
}

// operatorUpdateCommand implements the operator update command helper.
func operatorUpdateCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	// `nodeID` stores the value produced by this operation.
	nodeID := fs.String("id", "", "node id")
	// `installDir` stores the current position in the related collection.
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	// `source` stores the value produced by this operation.
	source := fs.String("source", "auto", "auto, local, or release")
	// `manifestURL` stores the value produced by this operation.
	manifestURL := fs.String("release-manifest-url", os.Getenv(operatorReleaseManifestURLEnv), "release manifest URL")
	// `publicKey` stores the key used to access the related value.
	publicKey := fs.String("release-public-key", os.Getenv(operatorReleasePublicKeyEnv), "release public key")
	// `dryRun` stores the value produced by this operation.
	dryRun := fs.Bool("dry-run", false, "print update plan")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `manifest` and `found` store whether the related condition is satisfied.
	manifest, found := operatorReadInstallManifest(*installDir)
	// `id` stores the current position in the related collection.
	id := normalizeNodeIdentityID(*nodeID)
	if id == "" {
		id = manifest.NodeID
	}
	if id == "" {
		return errors.New("--id required when install manifest is missing")
	}
	// `role` stores the value produced by this operation.
	role := manifest.Role
	if role == "" {
		role = "candidate"
	}
	// `setupArgs` stores the value produced by this operation.
	setupArgs := []string{role, "--id", id, "--install-dir", *installDir, "--source", *source, "--release-manifest-url", *manifestURL, "--release-public-key", *publicKey}
	if *dryRun {
		setupArgs = append(setupArgs, "--dry-run")
	}
	if !found {
		setupArgs = append(setupArgs, "--role", role)
	}
	return operatorSetupCommand(setupArgs)
}

// operatorUninstallCommand implements the operator uninstall command helper.
func operatorUninstallCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	// `nodeID` stores the value produced by this operation.
	nodeID := fs.String("id", "", "node id")
	// `installDir` stores the current position in the related collection.
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	// `purgeData` stores the value produced by this operation.
	purgeData := fs.Bool("purge-data", false, "delete install directory after stopping service")
	// `confirm` stores the value produced by this operation.
	confirm := fs.String("confirm-delete-node-id", "", "required with --purge-data")
	// `dryRun` stores the value produced by this operation.
	dryRun := fs.Bool("dry-run", false, "print uninstall plan")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `manifest` stores the value produced by this operation.
	manifest, _ := operatorReadInstallManifest(*installDir)
	// `id` stores the current position in the related collection.
	id := normalizeNodeIdentityID(*nodeID)
	if id == "" {
		id = manifest.NodeID
	}
	if id == "" {
		return errors.New("--id required")
	}
	// `out` stores the result produced by this operation.
	out := map[string]any{
		"command":                "uninstall",
		"action":                 "disable_service",
		"node_id":                id,
		"install_dir":            operatorCleanPath(*installDir),
		"purge_data":             *purgeData,
		"preserved_keys":         true,
		"preserved_database":     true,
		"requires_confirmation":  *purgeData,
		"install_manifest":       operatorCleanPath(operatorInstallManifestPath(*installDir)),
		"danger_confirmation_ok": strings.EqualFold(*confirm, id),
	}
	if *purgeData && !strings.EqualFold(*confirm, id) {
		return fmt.Errorf("--purge-data requires --confirm-delete-node-id %s", id)
	}
	if *dryRun {
		operatorPrintJSON(out)
		return nil
	}
	_ = operatorServiceCommand([]string{"stop", "--id", id, "--install-dir", *installDir})
	if *purgeData {
		// `quarantine` stores the value produced by this operation.
		quarantine := strings.TrimRight(*installDir, string(os.PathSeparator)) + ".deleted-" + time.Now().UTC().Format("20060102T150405Z")
		// `err` stores the error produced by this operation.
		if err := os.Rename(operatorResolvePath(*installDir), quarantine); err != nil {
			return err
		}
		out["action"] = "quarantined_install_dir"
		out["quarantine_dir"] = operatorCleanPath(quarantine)
		out["preserved_keys"] = false
		out["preserved_database"] = false
	}
	operatorPrintJSON(out)
	return nil
}

// operatorRestoreCommand implements the operator restore command helper.
func operatorRestoreCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	// `nodeID` stores the value produced by this operation.
	nodeID := fs.String("id", "", "node id")
	// `installDir` stores the current position in the related collection.
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	// `backupPath` stores the value produced by this operation.
	backupPath := fs.String("backup", "", "backup bundle directory")
	// `replaceKey` stores the key used to access the related value.
	replaceKey := fs.Bool("replace-key", false, "replace an existing validator key")
	// `confirmPubkey` stores the key used to access the related value.
	confirmPubkey := fs.String("confirm-validator-pubkey", "", "required when replacing key")
	// `dryRun` stores the value produced by this operation.
	dryRun := fs.Bool("dry-run", false, "print restore plan")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*backupPath) == "" {
		return errors.New("--backup required")
	}
	// `bundle` and `err` store the error produced by this operation.
	bundle, err := operatorReadBackupBundleManifest(*backupPath)
	if err != nil {
		return err
	}
	// `id` stores the current position in the related collection.
	id := normalizeNodeIdentityID(*nodeID)
	if id == "" {
		id = bundle.NodeID
	}
	if id == "" {
		return errors.New("--id required")
	}
	if bundle.NodeID != "" && !strings.EqualFold(bundle.NodeID, id) {
		return fmt.Errorf("backup belongs to node %s, not %s", bundle.NodeID, id)
	}
	// `nodePath` stores the value produced by this operation.
	nodePath := nodeDataPath(filepath.Join(operatorResolvePath(*installDir), "data"), id)
	// `keyDst` stores the key used to access the related value.
	keyDst := filepath.Join(nodePath, "validator.sec")
	// `existingPub` stores the value produced by this operation.
	existingPub, _ := operatorReadValidatorPubkey(nodePath)
	// `backupPub` stores the value produced by this operation.
	backupPub := normalizeConsensusPubKeyHex(bundle.ValidatorPubkey)
	if existingPub != "" && backupPub != "" && !strings.EqualFold(existingPub, backupPub) {
		if !*replaceKey || !strings.EqualFold(normalizeConsensusPubKeyHex(*confirmPubkey), backupPub) {
			return fmt.Errorf("existing validator key differs; use --replace-key --confirm-validator-pubkey %s to replace", backupPub)
		}
	}
	// `out` stores the result produced by this operation.
	out := map[string]any{
		"command":            "restore",
		"node_id":            id,
		"backup":             operatorCleanPath(*backupPath),
		"install_dir":        operatorCleanPath(*installDir),
		"node_path":          operatorCleanPath(nodePath),
		"validator_pubkey":   backupPub,
		"replace_key":        *replaceKey,
		"preserved_database": true,
		"dry_run":            *dryRun,
	}
	if *dryRun {
		operatorPrintJSON(out)
		return nil
	}
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(nodePath); err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := operatorCopyBackupFile(*backupPath, "validator.sec", keyDst, true); err != nil {
		return err
	}
	_ = operatorCopyBackupFile(*backupPath, "validator.pub", validatorPublicPath(nodePath), false)
	_ = operatorCopyBackupFile(*backupPath, validatorFingerprintLockFileName, validatorFingerprintLockPath(nodePath), true)
	_ = operatorCopyBackupFile(*backupPath, validatorKeyMetaFileName, validatorKeyMetaPath(nodePath), true)
	_ = operatorCopyBackupFile(*backupPath, validatorKeyBackupManifestFileName, validatorKeyBackupManifestPath(nodePath), true)
	_ = operatorCopyBackupFile(*backupPath, "config.toml", filepath.Join(operatorResolvePath(*installDir), "config.toml"), false)
	// `manifest` stores the value produced by this operation.
	manifest := operatorInstallManifest{
		SchemaVersion:        1,
		NodeID:               id,
		NetworkNodeID:        normalizeNodeIdentityID(id),
		Role:                 bundle.Role,
		InstallDir:           operatorCleanPath(*installDir),
		DataDir:              operatorCleanPath(filepath.Join(operatorResolvePath(*installDir), "data")),
		NodePath:             operatorCleanPath(nodePath),
		ConfigPath:           operatorCleanPath(filepath.Join(operatorResolvePath(*installDir), "config.toml")),
		BinaryPath:           operatorCleanPath(filepath.Join(operatorResolvePath(*installDir), operatorBinaryName())),
		AliasPath:            operatorCleanPath(filepath.Join(operatorResolvePath(*installDir), operatorAliasBinaryName())),
		GenesisHash:          bundle.GenesisHash,
		ValidatorPubkey:      backupPub,
		ValidatorFingerprint: bundle.ValidatorFingerprint,
		ServiceName:          operatorServiceName(id),
		OS:                   runtime.GOOS,
		Arch:                 runtime.GOARCH,
		Source:               "restore",
		UpdatedAt:            time.Now().UTC().Format(time.RFC3339),
	}
	// `err` stores the error produced by this operation.
	if err := operatorWriteInstallManifest(*installDir, manifest); err != nil {
		return err
	}
	out["install_manifest"] = operatorCleanPath(operatorInstallManifestPath(*installDir))
	operatorPrintJSON(out)
	return nil
}

type operatorBackupBundleManifest struct {
	// `SchemaVersion` stores the value associated with this record.
	SchemaVersion int `json:"schema_version"`
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id"`
	// `Role` stores the value associated with this record.
	Role string `json:"role,omitempty"`
	// `GenesisHash` stores the digest used to identify or verify the related data.
	GenesisHash string `json:"genesis_hash"`
	// `ValidatorPubkey` stores whether the related condition is satisfied.
	ValidatorPubkey string `json:"validator_pubkey,omitempty"`
	// `ValidatorFingerprint` stores whether the related condition is satisfied.
	ValidatorFingerprint string `json:"validator_fingerprint,omitempty"`
	// `CreatedAt` stores the value associated with this record.
	CreatedAt string `json:"created_at"`
	// `Files` stores the value associated with this record.
	Files []string `json:"files"`
	// `Warning` stores the value associated with this record.
	Warning string `json:"warning"`
}

// operatorBackupBundle implements the operator backup bundle helper.
func operatorBackupBundle(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	// `id` stores the current position in the related collection.
	id := fs.String("id", "", "node id")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory")
	// `nodePathFlag` stores the value produced by this operation.
	nodePathFlag := fs.String("nodepath", "", "direct node data path")
	// `outDir` stores the result produced by this operation.
	outDir := fs.String("out", "", "backup output directory")
	// `jsonOut` stores the current position in the related collection.
	jsonOut := fs.Bool("json", true, "print JSON")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*outDir) == "" {
		return operatorBackupWizard(append(args, "--json"))
	}
	// `nodePath`, `nodeID`, and `err` store the error produced by this operation.
	nodePath, nodeID, err := operatorValidatorNodePath(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	// `targetDir` stores the value produced by this operation.
	targetDir := filepath.Join(operatorResolvePath(*outDir), nodeID+"-"+time.Now().UTC().Format("20060102T150405Z"))
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(targetDir); err != nil {
		return err
	}
	// `files` stores the value produced by this operation.
	files := make([]string, 0, 8)
	// `copyMaybe` stores the value produced by this operation.
	copyMaybe := func(src, name string, private bool) {
		if src == "" {
			return
		}
		// `err` stores the error produced by this operation.
		if _, err := os.Stat(src); err != nil {
			return
		}
		// `dst` stores the value produced by this operation.
		dst := filepath.Join(targetDir, name)
		// `err` stores the error produced by this operation.
		var err error
		if private {
			err = operatorCopyFilePrivate(dst, src)
		} else {
			err = operatorCopyFile(dst, src, 0o644)
		}
		if err == nil {
			files = append(files, name)
		}
	}
	copyMaybe(filepath.Join(nodePath, "validator.sec"), "validator.sec", true)
	copyMaybe(validatorPublicPath(nodePath), "validator.pub", false)
	copyMaybe(validatorFingerprintLockPath(nodePath), validatorFingerprintLockFileName, true)
	copyMaybe(validatorKeyMetaPath(nodePath), validatorKeyMetaFileName, true)
	copyMaybe(validatorKeyBackupManifestPath(nodePath), validatorKeyBackupManifestFileName, true)
	copyMaybe(filepath.Join(filepath.Dir(filepath.Dir(nodePath)), "config.toml"), "config.toml", false)
	// `installDir` stores the current position in the related collection.
	installDir := filepath.Dir(filepath.Dir(nodePath))
	copyMaybe(operatorInstallManifestPath(installDir), operatorInstallManifestFile, true)
	// `pub` stores the value produced by this operation.
	pub, _ := operatorReadValidatorPubkey(nodePath)
	// `fp` stores the value produced by this operation.
	fp := operatorReadTextFileTrimmed(validatorFingerprintLockPath(nodePath))
	// `manifest` stores the value produced by this operation.
	manifest := operatorBackupBundleManifest{
		SchemaVersion:        1,
		NodeID:               nodeID,
		Role:                 "validator",
		GenesisHash:          operatorGenesisHashFile("genesis.json"),
		ValidatorPubkey:      pub,
		ValidatorFingerprint: fp,
		CreatedAt:            time.Now().UTC().Format(time.RFC3339),
		Files:                files,
		Warning:              "Keep this backup offline. Without validator.sec or MPC shares, the old validator identity cannot be recovered.",
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := writePrivateFile(filepath.Join(targetDir, "backup_bundle_manifest.json"), raw); err != nil {
		return err
	}
	// `out` stores the result produced by this operation.
	out := map[string]any{
		"command":             "backup",
		"action":              "bundle_created",
		"node_id":             nodeID,
		"backup_dir":          operatorCleanPath(targetDir),
		"files":               files,
		"validator_pubkey":    pub,
		"backup_required":     true,
		"preserved_keys":      true,
		"preserved_database":  true,
		"offline_copy_needed": true,
	}
	if *jsonOut {
		operatorPrintJSON(out)
		return nil
	}
	fmt.Println("MSC backup created:", out["backup_dir"])
	fmt.Println("Copy this directory to offline encrypted storage.")
	return nil
}

// operatorReadBackupBundleManifest implements the operator read backup bundle manifest helper.
func operatorReadBackupBundleManifest(path string) (operatorBackupBundleManifest, error) {
	path = operatorResolvePath(path)
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(filepath.Join(path, "backup_bundle_manifest.json"))
	if err != nil {
		return operatorBackupBundleManifest{}, err
	}
	// `manifest` stores the value used by this operation.
	var manifest operatorBackupBundleManifest
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return operatorBackupBundleManifest{}, err
	}
	if strings.TrimSpace(manifest.GenesisHash) != "" && !strings.EqualFold(strings.TrimSpace(manifest.GenesisHash), operatorGenesisHashFile("genesis.json")) {
		return operatorBackupBundleManifest{}, fmt.Errorf("backup genesis hash %s does not match local genesis", manifest.GenesisHash)
	}
	return manifest, nil
}

// operatorCopyBackupFile implements the operator copy backup file helper.
func operatorCopyBackupFile(backupDir, name, dst string, private bool) error {
	// `src` stores the value produced by this operation.
	src := filepath.Join(operatorResolvePath(backupDir), name)
	// `err` stores the error produced by this operation.
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if private {
		return operatorCopyFilePrivate(dst, src)
	}
	return operatorCopyFile(dst, src, 0o644)
}

// operatorCopyFilePrivate implements the operator copy file private helper.
func operatorCopyFilePrivate(dst, src string) error {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writePrivateFile(dst, raw)
}

// operatorCopyFile implements the operator copy file helper.
func operatorCopyFile(dst, src string, mode os.FileMode) error {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	// `err` stores the error produced by this operation.
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, raw, mode)
}

// operatorReadInstallManifest implements the operator read install manifest helper.
func operatorReadInstallManifest(installDir string) (operatorInstallManifest, bool) {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(operatorInstallManifestPath(installDir))
	if err != nil {
		return operatorInstallManifest{}, false
	}
	// `manifest` stores the value used by this operation.
	var manifest operatorInstallManifest
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return operatorInstallManifest{}, false
	}
	return manifest, true
}

// operatorWriteInstallManifest implements the operator write install manifest helper.
func operatorWriteInstallManifest(installDir string, manifest operatorInstallManifest) error {
	// `err` stores the error produced by this operation.
	if err := ensurePrivateDirectory(operatorResolvePath(installDir)); err != nil {
		return err
	}
	// `raw` and `err` store the error produced by this operation.
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(operatorInstallManifestPath(installDir), raw)
}

// operatorInstallManifestPath implements the operator install manifest path helper.
func operatorInstallManifestPath(installDir string) string {
	return filepath.Join(operatorResolvePath(installDir), operatorInstallManifestFile)
}

func operatorValidatorKeyPresent(nodePath string) bool {
	return operatorFileExists(filepath.Join(nodePath, "validator.sec")) ||
		operatorFileExists(filepath.Join(nodePath, autoValidatorKeyFileName))
}

// operatorBuildInstallManifest implements the operator build install manifest helper.
func operatorBuildInstallManifest(opts operatorSetupOptions) (operatorInstallManifest, error) {
	// `installDir` stores the current position in the related collection.
	installDir := operatorResolvePath(opts.InstallDir)
	// `dataDir` stores the value produced by this operation.
	dataDir := filepath.Join(installDir, "data")
	// `nodePath` stores the value produced by this operation.
	nodePath := nodeDataPath(dataDir, opts.NodeID)
	// `networkNodeID` stores the network identity advertised by this node.
	networkNodeID := normalizeNodeIdentityID(opts.NodeID)
	for _, identityPath := range []string{
		filepath.Join(nodePath, nodeIdentityFileName),
		filepath.Join(dataDir, nodeIdentityFileName),
	} {
		if identity, found, err := loadNodeIdentity(identityPath); err == nil && found {
			networkNodeID = normalizeNodeIdentityID(identity.NodeID)
			break
		}
	}
	// `pub` stores the value produced by this operation.
	pub, _ := operatorReadValidatorPubkey(nodePath)
	// `fp` stores the value produced by this operation.
	fp := operatorReadTextFileTrimmed(validatorFingerprintLockPath(nodePath))
	// `validatorID` stores the consensus identity derived from the validator public key.
	validatorID := ""
	if identity, found, err := loadValidatorIdentityFile(filepath.Join(nodePath, validatorIdentityFileName)); err == nil && found {
		validatorID = strings.TrimSpace(identity.ValidatorID)
	}
	if validatorID == "" && strings.TrimSpace(pub) != "" {
		if pubBytes, err := hex.DecodeString(strings.TrimSpace(pub)); err == nil {
			validatorID = validatorIDFromPublicKey(pubBytes)
		}
	}
	// `walletAddress` and `walletPublicKey` store the node account identity.
	walletAddress, walletPublicKey := "", ""
	if walletFile, found, err := loadNodeWalletFile(filepath.Join(nodePath, nodeWalletFileName)); err == nil && found {
		walletAddress = strings.TrimSpace(walletFile.Address)
		walletPublicKey = strings.TrimSpace(walletFile.PublicKey)
	}
	return operatorInstallManifest{
		SchemaVersion:        1,
		NodeID:               opts.NodeID,
		NetworkNodeID:        networkNodeID,
		ValidatorID:          validatorID,
		WalletAddress:        displayAddress(walletAddress),
		WalletPublicKey:      walletPublicKey,
		Role:                 opts.Role,
		InstallDir:           operatorCleanPath(installDir),
		DataDir:              operatorCleanPath(dataDir),
		NodePath:             operatorCleanPath(nodePath),
		ConfigPath:           operatorCleanPath(filepath.Join(installDir, "config.toml")),
		BinaryPath:           operatorCleanPath(filepath.Join(installDir, operatorBinaryName())),
		AliasPath:            operatorCleanPath(filepath.Join(installDir, operatorAliasBinaryName())),
		GenesisHash:          operatorGenesisHashFile("genesis.json"),
		ValidatorPubkey:      pub,
		ValidatorFingerprint: fp,
		ServiceName:          operatorServiceName(opts.NodeID),
		OS:                   runtime.GOOS,
		Arch:                 runtime.GOARCH,
		Source:               opts.Source,
		UpdatedAt:            time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// operatorInstallHasProtectedData implements the operator install has protected data helper.
func operatorInstallHasProtectedData(installDir string) bool {
	installDir = operatorResolvePath(installDir)
	// `rel` tracks the current values while iterating.
	for _, rel := range []string{"data", "config.toml", "node.env", "node.env.ps1", "start.sh", "start.ps1", operatorInstallManifestFile} {
		if operatorFileExists(filepath.Join(installDir, rel)) {
			return true
		}
	}
	return false
}

// operatorInstallPreservationReport implements the operator install preservation report helper.
func operatorInstallPreservationReport(installDir string) map[string]any {
	installDir = operatorResolvePath(installDir)
	// `dataDir` stores the value produced by this operation.
	dataDir := filepath.Join(installDir, "data")
	return map[string]any{
		"keys": operatorFileExists(filepath.Join(dataDir, "validator.sec")) ||
			operatorFileExists(filepath.Join(dataDir, autoValidatorKeyFileName)) ||
			operatorGlobAny(dataDir, "node_*", "validator.sec") ||
			operatorGlobAny(dataDir, "node_*", autoValidatorKeyFileName) ||
			operatorGlobAny(dataDir, "*", "validator.sec") ||
			operatorGlobAny(dataDir, "*", autoValidatorKeyFileName),
		"database": operatorGlobAny(dataDir, "*.db") ||
			operatorFileExists(filepath.Join(dataDir, "chain.db")) ||
			operatorGlobAny(dataDir, "node_*", "*.db") ||
			operatorGlobAny(dataDir, "*", "*.db") ||
			operatorGlobAny(dataDir, "*", "chain.db"),
		"config":   operatorFileExists(filepath.Join(installDir, "config.toml")),
		"manifest": operatorFileExists(operatorInstallManifestPath(installDir)),
		"data_dir": operatorFileExists(dataDir),
		"wallets": operatorGlobAny(installDir, "*.json") ||
			operatorFileExists(filepath.Join(dataDir, nodeWalletFileName)) ||
			operatorGlobAny(dataDir, "node_*", nodeWalletFileName) ||
			operatorGlobAny(dataDir, "*", nodeWalletFileName),
		"backups": operatorFileExists(filepath.Join(dataDir, "backups")) ||
			operatorGlobAny(dataDir, "node_*", "backups") ||
			operatorGlobAny(dataDir, "*", "backups"),
		"protected": operatorInstallHasProtectedData(installDir),
	}
}

// operatorGlobAny implements the operator glob any helper.
func operatorGlobAny(base string, parts ...string) bool {
	// `pattern` stores the value produced by this operation.
	pattern := filepath.Join(append([]string{base}, parts...)...)
	// `matches` and `err` store the error produced by this operation.
	matches, err := filepath.Glob(pattern)
	return err == nil && len(matches) > 0
}

// operatorGenesisHashFile implements the operator genesis hash file helper.
func operatorGenesisHashFile(path string) string {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(operatorResolvePath(path))
	if err != nil {
		return GenesisHashExpected
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// operatorReadTextFileTrimmed implements the operator read text file trimmed helper.
func operatorReadTextFileTrimmed(path string) string {
	// `raw` and `err` store the error produced by this operation.
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// operatorServiceName implements the operator service name helper.
func operatorServiceName(nodeID string) string {
	// `id` stores the current position in the related collection.
	id := normalizeNodeIdentityID(nodeID)
	if id == "" {
		return "msc-node"
	}
	if runtime.GOOS == "windows" {
		return "MSC-Node-" + id
	}
	return "msc-" + id
}

// operatorNormalizeSetupRole implements the operator normalize setup role helper.
func operatorNormalizeSetupRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "full", "full-node":
		return "full"
	case "candidate":
		return "candidate"
	case "validator":
		return "validator"
	default:
		return ""
	}
}

// operatorNormalizeSetupSource implements the operator normalize setup source helper.
func operatorNormalizeSetupSource(source, manifestURL, publicKey string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	switch source {
	case "", "auto":
		if strings.TrimSpace(manifestURL) != "" && strings.TrimSpace(publicKey) != "" {
			return "release"
		}
		return "local"
	case "local", "release":
		return source
	default:
		return ""
	}
}

// operatorDefaultInstallDir implements the operator default install dir helper.
func operatorDefaultInstallDir() string {
	if runtime.GOOS == "windows" {
		// `base` stores the value produced by this operation.
		if base := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); base != "" {
			return filepath.Join(base, "MSCNode")
		}
		return "msc-node-install"
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return "/opt/msc-node"
	}
	// `home` and `err` store the error produced by this operation.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".msc-node")
	}
	return "msc-node-install"
}

// operatorInstallerScriptPath implements the operator installer script path helper.
func operatorInstallerScriptPath(goos string) (string, error) {
	// `names` stores the value used by this operation.
	var names []string
	switch goos {
	case "windows":
		names = []string{filepath.Join("scripts", "install_msc_node.ps1"), "install_msc_node.ps1"}
	case "linux":
		names = []string{filepath.Join("scripts", "install_msc_node.sh"), "install_msc_node.sh"}
	default:
		return "", fmt.Errorf("unsupported setup OS %q", goos)
	}
	// `candidates` stores the value produced by this operation.
	candidates := append([]string{}, names...)
	// `exe` and `err` store the error produced by this operation.
	if exe, err := os.Executable(); err == nil {
		// `base` stores the value produced by this operation.
		base := filepath.Dir(exe)
		// `name` tracks the current values while iterating.
		for _, name := range names {
			candidates = append(candidates, filepath.Join(base, name))
		}
	}
	// `candidate` tracks the current values while iterating.
	for _, candidate := range candidates {
		// `st` and `err` store the error produced by this operation.
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("installer script not found; run from repo root or packaged installer")
}

// operatorBuildSetupInstallerCommand implements the operator build setup installer command helper.
func operatorBuildSetupInstallerCommand(scriptPath string, opts operatorSetupOptions, prebuilt string) (string, []string) {
	if runtime.GOOS == "windows" {
		// `args` stores the value produced by this operation.
		args := []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", scriptPath, "-Role", opts.Role, "-NodeId", opts.NodeID, "-Rpc", opts.RPC, "-InstallDir", opts.InstallDir}
		if opts.Peers != "" {
			args = append(args, "-Peers", opts.Peers)
		}
		if opts.LowRAM {
			args = append(args, "-LowRam")
		}
		if opts.AutoStart {
			args = append(args, "-AutoStart")
		}
		if opts.PublicGateway {
			args = append(args, "-PublicGateway")
		}
		if opts.Allow4GBValidator {
			args = append(args, "-Allow4GBValidator")
		}
		if prebuilt != "" {
			args = append(args, "-PrebuiltBinary", prebuilt)
		}
		return "powershell", args
	}
	// `args` stores the value produced by this operation.
	args := []string{scriptPath, "--role", opts.Role, "--id", opts.NodeID, "--rpc", opts.RPC, "--install-dir", opts.InstallDir}
	if opts.Peers != "" {
		args = append(args, "--peers", opts.Peers)
	}
	if opts.LowRAM {
		args = append(args, "--low-ram")
	}
	if opts.AutoStart {
		args = append(args, "--auto-start")
	}
	if opts.PublicGateway {
		args = append(args, "--public-gateway")
	}
	if opts.Allow4GBValidator {
		args = append(args, "--allow-4gb-validator")
	}
	if prebuilt != "" {
		args = append(args, "--prebuilt-binary", prebuilt)
	}
	return "bash", args
}

// operatorDetectHardware implements the operator detect hardware helper.
func operatorDetectHardware(path string) operatorHardwareReport {
	return operatorHardwareReport{
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		CPUs:        runtime.NumCPU(),
		MemoryBytes: operatorTotalMemoryBytes(),
		DiskBytes:   operatorDiskFreeBytes(path),
	}
}

type operatorReleaseManifest struct {
	// `VersionTag` stores the value associated with this record.
	VersionTag string `json:"version_tag"`
	// `Artifacts` stores the value associated with this record.
	Artifacts []operatorReleaseArtifact `json:"artifacts"`
}

type operatorReleaseArtifact struct {
	// `OS` stores the value associated with this record.
	OS string `json:"os"`
	// `Arch` stores the value associated with this record.
	Arch string `json:"arch"`
	// `File` stores the value associated with this record.
	File string `json:"file"`
	// `URL` stores the value associated with this record.
	URL string `json:"url"`
	// `SHA256` stores the value associated with this record.
	SHA256 string `json:"sha256"`
	// `Signature` stores the value associated with this record.
	Signature string `json:"signature"`
}

// operatorDownloadReleaseBinary implements the operator download release binary helper.
func operatorDownloadReleaseBinary(manifestURL, publicKey, goos, goarch string) (string, func(), error) {
	manifestURL = strings.TrimSpace(manifestURL)
	if manifestURL == "" {
		return "", nil, errors.New("--release-manifest-url required for release source")
	}
	// `client` stores the value produced by this operation.
	client := &http.Client{Timeout: 30 * time.Second}
	// `manifestRaw` and `err` store the error produced by this operation.
	manifestRaw, err := operatorFetchURLBytes(client, manifestURL, 4<<20)
	if err != nil {
		return "", nil, err
	}
	// `artifact`, `artifactURL`, and `err` store the error produced by this operation.
	artifact, artifactURL, err := operatorSelectReleaseArtifact(manifestRaw, manifestURL, goos, goarch)
	if err != nil {
		return "", nil, err
	}
	// `binaryRaw` and `err` store the error produced by this operation.
	binaryRaw, err := operatorFetchURLBytes(client, artifactURL, 256<<20)
	if err != nil {
		return "", nil, err
	}
	// `err` stores the error produced by this operation.
	if _, err := operatorVerifyReleaseArtifactBytes(artifact, binaryRaw, publicKey); err != nil {
		return "", nil, err
	}
	// `ext` stores the value produced by this operation.
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	// `tmp` and `err` store the error produced by this operation.
	tmp, err := os.CreateTemp("", "msc-release-*"+ext)
	if err != nil {
		return "", nil, err
	}
	// `path` stores the value produced by this operation.
	path := tmp.Name()
	// `err` stores the error produced by this operation.
	if _, err := tmp.Write(binaryRaw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", nil, err
	}
	// `err` stores the error produced by this operation.
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, err
	}
	if goos != "windows" {
		_ = os.Chmod(path, 0o755)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

// operatorFetchURLBytes implements the operator fetch url bytes helper.
func operatorFetchURLBytes(client *http.Client, rawURL string, max int64) ([]byte, error) {
	// `req` and `err` store the error produced by this operation.
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	// `res` and `err` store the error produced by this operation.
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("download %s failed: %s", rawURL, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, max))
}

// operatorSelectReleaseArtifact implements the operator select release artifact helper.
func operatorSelectReleaseArtifact(manifestRaw []byte, manifestURL, goos, goarch string) (operatorReleaseArtifact, string, error) {
	// `manifest` stores the value used by this operation.
	var manifest operatorReleaseManifest
	// `err` stores the error produced by this operation.
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return operatorReleaseArtifact{}, "", err
	}
	// `artifact` tracks the current values while iterating.
	for _, artifact := range manifest.Artifacts {
		if strings.EqualFold(artifact.OS, goos) && strings.EqualFold(artifact.Arch, goarch) {
			// `artifactURL` stores the value produced by this operation.
			artifactURL := strings.TrimSpace(artifact.URL)
			if artifactURL == "" {
				if strings.TrimSpace(artifact.File) == "" {
					return operatorReleaseArtifact{}, "", errors.New("release artifact missing file/url")
				}
				// `base` and `err` store the error produced by this operation.
				base, err := url.Parse(manifestURL)
				if err != nil {
					return operatorReleaseArtifact{}, "", err
				}
				base.Path = strings.TrimRight(base.Path, "/")
				// `idx` stores the current position in the related collection.
				if idx := strings.LastIndex(base.Path, "/"); idx >= 0 {
					base.Path = base.Path[:idx+1]
				}
				// `ref` stores the value produced by this operation.
				ref, _ := url.Parse(artifact.File)
				artifactURL = base.ResolveReference(ref).String()
			}
			return artifact, artifactURL, nil
		}
	}
	return operatorReleaseArtifact{}, "", fmt.Errorf("no release artifact for %s/%s", goos, goarch)
}

// operatorVerifyReleaseArtifactBytes implements the operator verify release artifact bytes helper.
func operatorVerifyReleaseArtifactBytes(artifact operatorReleaseArtifact, data []byte, publicKey string) (bool, error) {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(data)
	// `got` stores the value produced by this operation.
	got := hex.EncodeToString(sum[:])
	// `want` stores the value produced by this operation.
	want := strings.ToLower(strings.TrimSpace(artifact.SHA256))
	if want == "" {
		return false, errors.New("release artifact missing sha256")
	}
	if got != want {
		return false, fmt.Errorf("release checksum mismatch: got %s want %s", got, want)
	}
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return false, nil
	}
	if strings.TrimSpace(artifact.Signature) == "" {
		return false, errors.New("release public key provided but artifact signature missing")
	}
	// `pub` and `err` store the error produced by this operation.
	pub, err := operatorDecodeHexOrBase64(publicKey)
	if err != nil {
		return false, fmt.Errorf("invalid release public key: %w", err)
	}
	// `sig` and `err` store the error produced by this operation.
	sig, err := operatorDecodeHexOrBase64(artifact.Signature)
	if err != nil {
		return false, fmt.Errorf("invalid artifact signature: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return false, errors.New("invalid release signature material length")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(got), sig) {
		return false, errors.New("release artifact signature verification failed")
	}
	return true, nil
}

// operatorDecodeHexOrBase64 implements the operator decode hex or base64 helper.
func operatorDecodeHexOrBase64(raw string) ([]byte, error) {
	// `cleaned` stores the value produced by this operation.
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "0x")
	// `b` and `err` store the error produced by this operation.
	if b, err := hex.DecodeString(cleaned); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
}

type operatorDoctorReport struct {
	// `Command` stores the value associated with this record.
	Command string `json:"command"`
	// `OK` stores whether the related condition is satisfied.
	OK bool `json:"ok"`
	// `Result` stores the result produced by this operation.
	Result string `json:"result"`
	// `NodeID` stores the value associated with this record.
	NodeID string `json:"node_id,omitempty"`
	// `Role` stores the value associated with this record.
	Role string `json:"role,omitempty"`
	// `Checks` stores the value associated with this record.
	Checks []operatorDoctorCheck `json:"checks"`
	// `Details` stores the value associated with this record.
	Details map[string]any `json:"details,omitempty"`
}

type operatorDoctorCheck struct {
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Status` stores the value associated with this record.
	Status string `json:"status"`
	// `Detail` stores the value associated with this record.
	Detail string `json:"detail"`
}

// operatorDoctorCommand implements the operator doctor command helper.
func operatorDoctorCommand(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	// `nodeID` stores the value produced by this operation.
	nodeID := fs.String("id", "", "node id")
	// `role` stores the value produced by this operation.
	role := fs.String("role", "auto", "node role")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory")
	// `nodePath` stores the value produced by this operation.
	nodePath := fs.String("nodepath", "", "direct node data path")
	// `configPath` stores the configuration used by this operation.
	configPath := fs.String("config", "config.toml", "config path")
	// `genesisPath` stores the value produced by this operation.
	genesisPath := fs.String("genesis", "genesis.json", "genesis path")
	// `jsonOut` stores the current position in the related collection.
	jsonOut := fs.Bool("json", false, "print JSON")
	// `rpcFlags` stores the value produced by this operation.
	rpcFlags := registerOperatorRPCFlags(fs)
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `report` stores the value produced by this operation.
	report := operatorCollectDoctorReport(*nodeID, *role, *dataDir, *nodePath, *configPath, *genesisPath, rpcFlags)
	if *jsonOut {
		operatorPrintJSON(report)
		return nil
	}
	operatorPrintDoctorReport(report)
	return nil
}

// operatorCollectDoctorReport implements the operator collect doctor report helper.
func operatorCollectDoctorReport(nodeID, role, dataDir, nodePath, configPath, genesisPath string, rpcFlags *operatorRPCFlags) operatorDoctorReport {
	// `report` stores the value produced by this operation.
	report := operatorDoctorReport{
		Command: "doctor",
		NodeID:  normalizeNodeIdentityID(nodeID),
		Role:    strings.ToLower(strings.TrimSpace(role)),
		OK:      true,
		Result:  "ok",
		Details: map[string]any{},
	}
	// `add` stores the value produced by this operation.
	add := func(name, status, detail string) {
		status = strings.ToLower(status)
		report.Checks = append(report.Checks, operatorDoctorCheck{Name: name, Status: status, Detail: detail})
		if status == "error" {
			report.OK = false
			report.Result = "error"
		} else if status == "warn" && report.Result == "ok" {
			report.Result = "warn"
		}
	}
	// `hw` stores the value produced by this operation.
	hw := operatorDetectHardware(filepath.Dir(operatorResolvePath(configPath)))
	report.Details["hardware"] = hw
	add("os", "ok", fmt.Sprintf("%s/%s cpu=%d", hw.OS, hw.Arch, hw.CPUs))
	if hw.MemoryBytes == 0 {
		add("ram", "unknown", "total RAM unavailable")
	} else if report.Role == "validator" && hw.MemoryBytes < 8*1024*1024*1024 {
		add("ram", "warn", fmt.Sprintf("%d GiB detected; validator-ready mode recommends 8 GiB", hw.MemoryBytes/(1024*1024*1024)))
	} else {
		add("ram", "ok", fmt.Sprintf("%d GiB", hw.MemoryBytes/(1024*1024*1024)))
	}
	if hw.DiskBytes == 0 {
		add("disk", "unknown", "free disk unavailable")
	} else if hw.DiskBytes < 50*1024*1024*1024 {
		add("disk", "warn", fmt.Sprintf("%d GiB free; mainnet nodes should have much more headroom", hw.DiskBytes/(1024*1024*1024)))
	} else {
		add("disk", "ok", fmt.Sprintf("%d GiB free", hw.DiskBytes/(1024*1024*1024)))
	}
	// `configRaw` and `configErr` store the error produced by this operation.
	configRaw, configErr := os.ReadFile(operatorResolvePath(configPath))
	if configErr != nil {
		add("config", "error", configErr.Error())
	} else {
		add("config", "ok", operatorCleanPath(configPath))
	}
	// `genesisHash` stores the digest used to identify or verify the related data.
	genesisHash := ""
	// `raw` and `err` store the error produced by this operation.
	if raw, err := os.ReadFile(operatorResolvePath(genesisPath)); err != nil {
		add("genesis", "error", err.Error())
	} else {
		// `sum` stores the value produced by this operation.
		sum := sha256.Sum256(raw)
		genesisHash = hex.EncodeToString(sum[:])
		// `expected` stores the value produced by this operation.
		expected := GenesisHashExpected
		if len(configRaw) > 0 {
			// `configured` stores the configuration used by this operation.
			if configured := operatorTOMLStringValue(string(configRaw), "chain", "genesis_hash"); configured != "" {
				expected = strings.ToLower(configured)
			}
		}
		if genesisHash != strings.ToLower(expected) {
			add("genesis", "error", fmt.Sprintf("hash %s does not match expected %s", genesisHash, expected))
		} else {
			add("genesis", "ok", genesisHash)
		}
	}
	if len(configRaw) > 0 {
		// `rpcBind` stores the value produced by this operation.
		rpcBind := operatorTOMLStringValue(string(configRaw), "rpc", "laddr")
		switch {
		case rpcBind == "":
			add("rpc_bind", "unknown", "rpc.laddr not configured")
		case strings.HasPrefix(rpcBind, "127.0.0.1:") || strings.HasPrefix(rpcBind, "localhost:"):
			add("rpc_bind", "ok", rpcBind)
		case report.Role == "validator" || report.Role == "auto":
			add("rpc_bind", "warn", rpcBind+"; validator RPC should stay localhost/private")
		default:
			add("rpc_bind", "ok", rpcBind)
		}
		// `p2pBind` stores the value produced by this operation.
		p2pBind := operatorTOMLStringValue(string(configRaw), "p2p", "laddr")
		if p2pBind == "" {
			add("p2p", "unknown", "p2p.laddr empty; command-line/default port may be used")
		} else {
			add("p2p", "ok", p2pBind)
		}
	}
	if rpcFlags != nil {
		// `status` and `err` store the error produced by this operation.
		if status, err := operatorGET(rpcFlags, "/status", url.Values{"full": []string{"1"}}); err != nil {
			add("sync", "warn", "RPC status unavailable: "+err.Error())
		} else {
			// `height` stores the value produced by this operation.
			height := operatorJSONInt(status["height"])
			// `finalized` stores the value produced by this operation.
			finalized := operatorJSONInt(status["finalized_height"])
			add("sync", "ok", fmt.Sprintf("height=%d finalized=%d", height, finalized))
			report.Details["status"] = status
		}
		// `peers` and `err` store the error produced by this operation.
		if peers, err := operatorGET(rpcFlags, "/v1/peers", nil); err != nil {
			add("peers", "warn", "peer endpoint unavailable: "+err.Error())
		} else {
			// `count` stores the measured quantity used by this operation.
			count := operatorJSONInt(peers["count"])
			add("peers", "ok", fmt.Sprintf("count=%d", count))
			report.Details["peers"] = peers
		}
	}
	// `nodePathResolved` stores the value produced by this operation.
	nodePathResolved := ""
	if report.NodeID != "" || strings.TrimSpace(nodePath) != "" {
		// `path` and `err` store the error produced by this operation.
		if path, _, err := operatorValidatorNodePath(report.NodeID, dataDir, nodePath); err == nil {
			nodePathResolved = path
			report.Details["node_path"] = operatorCleanPath(path)
			// `err` stores the error produced by this operation.
			if operatorValidatorKeyPresent(path) {
				add("validator_key", "ok", path)
			} else {
				add("validator_key", "warn", "validator key not found; run validator-keygen, MPC setup, or auto-start a fresh node")
			}
		}
	}
	if report.Role == "validator" {
		if hw.MemoryBytes >= 8*1024*1024*1024 && nodePathResolved != "" {
			add("validator_eligible", "ok", "hardware and key path checks passed")
		} else {
			add("validator_eligible", "warn", "validator mode needs 8 GiB RAM recommended and validator key/MPC signer")
		}
	}
	if nodePathResolved != "" {
		// `latest`, `age`, and `ok` store whether the related condition is satisfied.
		if latest, age, ok := operatorLatestBackup(nodePathResolved); ok {
			// `status` stores the value produced by this operation.
			status := "ok"
			if age > 24*time.Hour {
				status = "warn"
			}
			add("backup", status, fmt.Sprintf("latest=%s age=%s", latest, age.Round(time.Second)))
		} else {
			add("backup", "warn", "no node backup found")
		}
	}
	return report
}

// operatorPrintDoctorReport implements the operator print doctor report helper.
func operatorPrintDoctorReport(report operatorDoctorReport) {
	fmt.Println("MSC Doctor")
	fmt.Println("Result:", strings.ToUpper(report.Result))
	if report.NodeID != "" {
		fmt.Println("Node:", report.NodeID)
	}
	// `check` tracks the current values while iterating.
	for _, check := range report.Checks {
		fmt.Printf("[%s] %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Detail)
	}
}

// operatorServiceCommand implements the operator service command helper.
func operatorServiceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("service subcommand required (start/stop/status)")
	}
	// `sub` stores the value produced by this operation.
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("service "+sub, flag.ContinueOnError)
	// `installDir` stores the current position in the related collection.
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	// `nodeID` stores the value produced by this operation.
	nodeID := fs.String("id", "", "node id for systemd service names")
	// `dryRun` stores the value produced by this operation.
	dryRun := fs.Bool("dry-run", false, "print service command without running")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	// `cmdName`, `cmdArgs`, and `err` store the error produced by this operation.
	cmdName, cmdArgs, err := operatorBuildServiceCommand(sub, *installDir, normalizeNodeIdentityID(*nodeID))
	if err != nil {
		return err
	}
	// `out` stores the result produced by this operation.
	out := map[string]any{"command": "service " + sub, "install_dir": operatorCleanPath(*installDir), "exec": append([]string{cmdName}, cmdArgs...), "dry_run": *dryRun}
	if *dryRun {
		out["binary_exists"] = operatorFileExists(filepath.Join(*installDir, operatorBinaryName()))
		out["alias_exists"] = operatorFileExists(filepath.Join(*installDir, operatorAliasBinaryName()))
		operatorPrintJSON(out)
		return nil
	}
	if sub == "status" {
		// `ctx` and `cancel` store the context controlling this operation.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// `cmd` stores the value produced by this operation.
		cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
		// `raw` and `err` store the error produced by this operation.
		raw, err := cmd.CombinedOutput()
		out["output"] = strings.TrimSpace(string(raw))
		out["running"] = err == nil
		if err != nil {
			out["status_error"] = err.Error()
		}
		out["binary_exists"] = operatorFileExists(filepath.Join(*installDir, operatorBinaryName()))
		out["alias_exists"] = operatorFileExists(filepath.Join(*installDir, operatorAliasBinaryName()))
		operatorPrintJSON(out)
		return nil
	}
	// `cmd` stores the value produced by this operation.
	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	// `err` stores the error produced by this operation.
	if err := cmd.Run(); err != nil {
		return err
	}
	operatorPrintJSON(out)
	return nil
}

// operatorBuildServiceCommand implements the operator build service command helper.
func operatorBuildServiceCommand(sub, installDir, nodeID string) (string, []string, error) {
	switch sub {
	case "start":
		if runtime.GOOS == "windows" {
			return "powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(installDir, "start.ps1")}, nil
		}
		return "bash", []string{filepath.Join(installDir, "start.sh")}, nil
	case "stop":
		if runtime.GOOS == "windows" {
			// `ps` stores the value produced by this operation.
			ps := fmt.Sprintf(`Get-Process | Where-Object { $_.Path -like '%s*' -and ($_.ProcessName -eq 'msc-node' -or $_.ProcessName -eq 'msc') } | Stop-Process`, strings.ReplaceAll(operatorCleanPath(installDir), "'", "''"))
			return "powershell", []string{"-NoProfile", "-Command", ps}, nil
		}
		if nodeID != "" && operatorCommandExists("systemctl") {
			return "sudo", []string{"systemctl", "stop", operatorServiceName(nodeID)}, nil
		}
		return "pkill", []string{"-f", filepath.Join(operatorCleanPath(installDir), "msc-node")}, nil
	case "status":
		if runtime.GOOS == "windows" {
			return "powershell", []string{"-NoProfile", "-Command", "Get-Process msc-node,msc -ErrorAction SilentlyContinue"}, nil
		}
		if nodeID != "" && operatorCommandExists("systemctl") {
			return "systemctl", []string{"status", operatorServiceName(nodeID)}, nil
		}
		return "pgrep", []string{"-af", "msc-node|msc"}, nil
	default:
		return "", nil, fmt.Errorf("unknown service command %q", sub)
	}
}

// operatorBackupWizard implements the operator backup wizard helper.
func operatorBackupWizard(args []string) error {
	// `fs` stores the value produced by this operation.
	fs := flag.NewFlagSet("backup wizard", flag.ContinueOnError)
	// `id` stores the current position in the related collection.
	id := fs.String("id", "", "node id")
	// `dataDir` stores the value produced by this operation.
	dataDir := fs.String("datadir", "data", "base data directory")
	// `nodePathFlag` stores the value produced by this operation.
	nodePathFlag := fs.String("nodepath", "", "direct node data path")
	// `jsonOut` stores the current position in the related collection.
	jsonOut := fs.Bool("json", false, "print JSON")
	// `err` stores the error produced by this operation.
	if err := fs.Parse(args); err != nil {
		return err
	}
	// `nodePath`, `nodeID`, and `err` store the error produced by this operation.
	nodePath, nodeID, err := operatorValidatorNodePath(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	// `commands` stores the value produced by this operation.
	commands := []string{
		fmt.Sprintf("msc backup export --id %s --datadir %s", nodeID, *dataDir),
		"msc backup verify --path <backup_dir>",
		"Copy validator.sec or validator.key / secure-backups to offline encrypted storage",
	}
	// `out` stores the result produced by this operation.
	out := map[string]any{
		"command":             "backup wizard",
		"validator":           nodeID,
		"node_path":           operatorCleanPath(nodePath),
		"validator_key":       operatorValidatorKeyPresent(nodePath),
		"validator_pubkey":    operatorFileExists(validatorPublicPath(nodePath)),
		"secure_backups_dir":  operatorCleanPath(filepath.Join(nodePath, ValidatorKeyBackupDir)),
		"secure_backups_seen": operatorFileExists(filepath.Join(nodePath, ValidatorKeyBackupDir)),
		"next_steps":          commands,
		"warning":             "Keep validator keys, wallet passwords, and backup media offline/private. Never commit them.",
	}
	// `latest`, `age`, and `ok` store whether the related condition is satisfied.
	if latest, age, ok := operatorLatestBackup(nodePath); ok {
		out["latest_backup"] = latest
		out["latest_backup_age_seconds"] = int64(age.Seconds())
	}
	if *jsonOut {
		operatorPrintJSON(out)
		return nil
	}
	fmt.Println("MSC Backup Wizard")
	fmt.Println("Validator:", nodeID)
	fmt.Println("Node path:", out["node_path"])
	fmt.Println("Validator key present:", out["validator_key"])
	fmt.Println("Secure backups:", out["secure_backups_dir"])
	fmt.Println("Next steps:")
	// `step` tracks the current values while iterating.
	for _, step := range commands {
		fmt.Println(" -", step)
	}
	fmt.Println("Warning:", out["warning"])
	return nil
}

// operatorTotalMemoryBytes implements the operator total memory bytes helper.
func operatorTotalMemoryBytes() uint64 {
	if runtime.GOOS == "linux" {
		// `raw` and `err` store the error produced by this operation.
		raw, err := os.ReadFile("/proc/meminfo")
		if err == nil {
			// `line` tracks the current values while iterating.
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "MemTotal:") {
					// `fields` stores the value produced by this operation.
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						// `kb` stores the value produced by this operation.
						kb, _ := strconv.ParseUint(fields[1], 10, 64)
						return kb * 1024
					}
				}
			}
		}
	}
	if runtime.GOOS == "windows" {
		// `ctx` and `cancel` store the context controlling this operation.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// `out` and `err` store the error produced by this operation.
		out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory").Output()
		if err == nil {
			// `v` stores the value produced by this operation.
			v, _ := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
			return v
		}
	}
	return 0
}

// operatorDiskFreeBytes implements the operator disk free bytes helper.
func operatorDiskFreeBytes(path string) uint64 {
	path = operatorResolvePath(path)
	if runtime.GOOS == "windows" {
		// `volume` stores the value produced by this operation.
		volume := filepath.VolumeName(path)
		if volume == "" {
			volume = "C:"
		}
		// `drive` stores the value produced by this operation.
		drive := strings.TrimSuffix(volume, ":")
		// `ctx` and `cancel` store the context controlling this operation.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// `cmd` stores the value produced by this operation.
		cmd := fmt.Sprintf("(Get-PSDrive -Name %s).Free", strconv.Quote(drive))
		// `out` and `err` store the error produced by this operation.
		out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", cmd).Output()
		if err == nil {
			// `v` stores the value produced by this operation.
			v, _ := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
			return v
		}
		return 0
	}
	// `ctx` and `cancel` store the context controlling this operation.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// `out` and `err` store the error produced by this operation.
	out, err := exec.CommandContext(ctx, "df", "-Pk", path).Output()
	if err != nil {
		return 0
	}
	// `lines` stores the value produced by this operation.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0
	}
	// `fields` stores the value produced by this operation.
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0
	}
	// `kb` stores the value produced by this operation.
	kb, _ := strconv.ParseUint(fields[3], 10, 64)
	return kb * 1024
}

// operatorTOMLStringValue implements the operator toml string value helper.
func operatorTOMLStringValue(raw, section, key string) string {
	// `current` stores the value produced by this operation.
	current := ""
	// `line` tracks the current values while iterating.
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(strings.Split(line, "#")[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			current = strings.TrimSpace(strings.Trim(line, "[]"))
			continue
		}
		if current != section || !strings.HasPrefix(line, key) {
			continue
		}
		// `parts` stores the value produced by this operation.
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(parts[1]), `"`)
	}
	return ""
}

// operatorLatestBackup implements the operator latest backup helper.
func operatorLatestBackup(nodePath string) (string, time.Duration, bool) {
	// `backupsDir` stores the value produced by this operation.
	backupsDir := filepath.Join(nodePath, "backups")
	// `entries` and `err` store the error produced by this operation.
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return "", 0, false
	}
	// `newest` stores the value used by this operation.
	var newest os.FileInfo
	// `newestName` stores the value used by this operation.
	var newestName string
	// `entry` tracks the current values while iterating.
	for _, entry := range entries {
		// `info` and `err` store the error produced by this operation.
		info, err := entry.Info()
		if err != nil || !info.IsDir() {
			continue
		}
		if newest == nil || info.ModTime().After(newest.ModTime()) {
			newest = info
			newestName = entry.Name()
		}
	}
	if newest == nil {
		return "", 0, false
	}
	return filepath.Join(backupsDir, newestName), time.Since(newest.ModTime()), true
}

// operatorFileExists implements the operator file exists helper.
func operatorFileExists(path string) bool {
	// `err` stores the error produced by this operation.
	_, err := os.Stat(operatorResolvePath(path))
	return err == nil
}

// operatorBinaryName implements the operator binary name helper.
func operatorBinaryName() string {
	if runtime.GOOS == "windows" {
		return "msc-node.exe"
	}
	return "msc-node"
}

// operatorAliasBinaryName implements the operator alias binary name helper.
func operatorAliasBinaryName() string {
	if runtime.GOOS == "windows" {
		return "msc.exe"
	}
	return "msc"
}

// operatorCommandExists implements the operator command exists helper.
func operatorCommandExists(name string) bool {
	// `err` stores the error produced by this operation.
	_, err := exec.LookPath(name)
	return err == nil
}

// operatorSortedKeys implements the operator sorted keys helper.
func operatorSortedKeys(m map[string]any) []string {
	// `keys` stores the key used to access the related value.
	keys := make([]string, 0, len(m))
	// `k` tracks the current values while iterating.
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
