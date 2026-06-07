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
	operatorReleaseManifestURLEnv = "MSC_RELEASE_MANIFEST_URL"
	operatorReleasePublicKeyEnv   = "MSC_RELEASE_PUBLIC_KEY"
	operatorInstallManifestFile   = "install_manifest.json"
)

type operatorSetupOptions struct {
	Role               string
	NodeID             string
	RPC                string
	Peers              string
	InstallDir         string
	LowRAM             bool
	AutoStart          bool
	PublicGateway      bool
	Allow4GBValidator  bool
	Source             string
	ReleaseManifestURL string
	ReleasePublicKey   string
	DryRun             bool
	Fresh              bool
	ConfirmDeleteNode  string
	JSON               bool
}

type operatorInstallManifest struct {
	SchemaVersion        int    `json:"schema_version"`
	NodeID               string `json:"node_id"`
	Role                 string `json:"role"`
	InstallDir           string `json:"install_dir"`
	DataDir              string `json:"data_dir"`
	NodePath             string `json:"node_path"`
	ConfigPath           string `json:"config_path"`
	BinaryPath           string `json:"binary_path"`
	AliasPath            string `json:"alias_path"`
	GenesisHash          string `json:"genesis_hash"`
	ValidatorPubkey      string `json:"validator_pubkey,omitempty"`
	ValidatorFingerprint string `json:"validator_fingerprint,omitempty"`
	ServiceName          string `json:"service_name"`
	OS                   string `json:"os"`
	Arch                 string `json:"arch"`
	Source               string `json:"source"`
	VersionTag           string `json:"version_tag,omitempty"`
	UpdatedAt            string `json:"updated_at"`
}

type operatorHardwareReport struct {
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	CPUs        int    `json:"cpus"`
	MemoryBytes uint64 `json:"memory_bytes,omitempty"`
	DiskBytes   uint64 `json:"disk_free_bytes,omitempty"`
}

func operatorSetupCommand(args []string) error {
	roleFromArg := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		roleFromArg = strings.TrimSpace(args[0])
		args = args[1:]
	}
	opts := operatorSetupOptions{}
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts.Role = operatorNormalizeSetupRole(opts.Role)
	if opts.Role == "" {
		return errors.New("setup role required: full, candidate, or validator")
	}
	opts.NodeID = normalizeValidatorID(opts.NodeID)
	if opts.NodeID == "" {
		return errors.New("--id required")
	}
	opts.Source = operatorNormalizeSetupSource(opts.Source, opts.ReleaseManifestURL, opts.ReleasePublicKey)
	if opts.Source == "" {
		return errors.New("--source must be auto, local, or release")
	}
	if runtime.GOOS == "darwin" {
		return errors.New("macOS setup is planned but not supported in this release; use Windows or Ubuntu/Linux")
	}
	existingManifest, manifestFound := operatorReadInstallManifest(opts.InstallDir)
	if manifestFound {
		if existingManifest.NodeID != "" && !strings.EqualFold(existingManifest.NodeID, opts.NodeID) {
			return fmt.Errorf("install dir already belongs to node %s; choose another --install-dir or restore that node", existingManifest.NodeID)
		}
		if existingManifest.Role != "" && opts.Role == "full" && existingManifest.Role == "validator" {
			opts.Role = existingManifest.Role
		}
	}
	installHasData := operatorInstallHasProtectedData(opts.InstallDir)
	if opts.Fresh {
		if !strings.EqualFold(strings.TrimSpace(opts.ConfirmDeleteNode), opts.NodeID) {
			return fmt.Errorf("--fresh requires --confirm-delete-node-id %s", opts.NodeID)
		}
		if installHasData {
			return errors.New("fresh reinstall refused while protected data exists; run backup, then uninstall --purge-data --confirm-delete-node-id <id> first")
		}
	}
	scriptPath, err := operatorInstallerScriptPath(runtime.GOOS)
	if err != nil {
		return err
	}
	hw := operatorDetectHardware(opts.InstallDir)
	prebuilt := ""
	cleanup := func() {}
	if opts.Source == "release" && !opts.DryRun {
		path, releaseCleanup, err := operatorDownloadReleaseBinary(opts.ReleaseManifestURL, opts.ReleasePublicKey, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return err
		}
		prebuilt = path
		cleanup = releaseCleanup
	}
	defer cleanup()

	cmdName, cmdArgs := operatorBuildSetupInstallerCommand(scriptPath, opts, prebuilt)
	action := "install"
	if manifestFound || installHasData {
		action = "repair"
	}
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
	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return err
	}
	manifest, err := operatorBuildInstallManifest(opts)
	if err != nil {
		return err
	}
	if err := operatorWriteInstallManifest(opts.InstallDir, manifest); err != nil {
		return err
	}
	plan["install_manifest"] = operatorCleanPath(operatorInstallManifestPath(opts.InstallDir))
	plan["validator_pubkey"] = manifest.ValidatorPubkey
	plan["service_enabled"] = opts.AutoStart
	operatorPrintJSON(plan)
	return nil
}

func operatorStatusAliasCommand(args []string) error {
	if operatorStatusArgsAreRPC(args) {
		return operatorStatusCommand(args, true)
	}
	return operatorServiceCommand(append([]string{"status"}, args...))
}

func operatorStatusArgsAreRPC(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, arg := range args {
		name := strings.TrimLeft(strings.TrimSpace(arg), "-")
		switch name {
		case "rpc", "token", "basic-user", "basic-pass", "timeout", "full":
			return true
		}
	}
	return false
}

func operatorRepairCommand(args []string) error {
	fs := flag.NewFlagSet("repair", flag.ContinueOnError)
	nodeID := fs.String("id", "", "node id")
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	source := fs.String("source", "local", "auto, local, or release")
	autoStart := fs.Bool("auto-start", false, "start after repair")
	jsonOut := fs.Bool("json", true, "print JSON")
	dryRun := fs.Bool("dry-run", false, "print repair plan")
	if err := fs.Parse(args); err != nil {
		return err
	}
	manifest, found := operatorReadInstallManifest(*installDir)
	id := normalizeValidatorID(*nodeID)
	if id == "" {
		id = manifest.NodeID
	}
	if id == "" {
		return errors.New("--id required when install manifest is missing")
	}
	role := manifest.Role
	if role == "" {
		role = "candidate"
	}
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

func operatorUpdateCommand(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	nodeID := fs.String("id", "", "node id")
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	source := fs.String("source", "auto", "auto, local, or release")
	manifestURL := fs.String("release-manifest-url", os.Getenv(operatorReleaseManifestURLEnv), "release manifest URL")
	publicKey := fs.String("release-public-key", os.Getenv(operatorReleasePublicKeyEnv), "release public key")
	dryRun := fs.Bool("dry-run", false, "print update plan")
	if err := fs.Parse(args); err != nil {
		return err
	}
	manifest, found := operatorReadInstallManifest(*installDir)
	id := normalizeValidatorID(*nodeID)
	if id == "" {
		id = manifest.NodeID
	}
	if id == "" {
		return errors.New("--id required when install manifest is missing")
	}
	role := manifest.Role
	if role == "" {
		role = "candidate"
	}
	setupArgs := []string{role, "--id", id, "--install-dir", *installDir, "--source", *source, "--release-manifest-url", *manifestURL, "--release-public-key", *publicKey}
	if *dryRun {
		setupArgs = append(setupArgs, "--dry-run")
	}
	if !found {
		setupArgs = append(setupArgs, "--role", role)
	}
	return operatorSetupCommand(setupArgs)
}

func operatorUninstallCommand(args []string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	nodeID := fs.String("id", "", "node id")
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	purgeData := fs.Bool("purge-data", false, "delete install directory after stopping service")
	confirm := fs.String("confirm-delete-node-id", "", "required with --purge-data")
	dryRun := fs.Bool("dry-run", false, "print uninstall plan")
	if err := fs.Parse(args); err != nil {
		return err
	}
	manifest, _ := operatorReadInstallManifest(*installDir)
	id := normalizeValidatorID(*nodeID)
	if id == "" {
		id = manifest.NodeID
	}
	if id == "" {
		return errors.New("--id required")
	}
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
		quarantine := strings.TrimRight(*installDir, string(os.PathSeparator)) + ".deleted-" + time.Now().UTC().Format("20060102T150405Z")
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

func operatorRestoreCommand(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	nodeID := fs.String("id", "", "node id")
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	backupPath := fs.String("backup", "", "backup bundle directory")
	replaceKey := fs.Bool("replace-key", false, "replace an existing validator key")
	confirmPubkey := fs.String("confirm-validator-pubkey", "", "required when replacing key")
	dryRun := fs.Bool("dry-run", false, "print restore plan")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*backupPath) == "" {
		return errors.New("--backup required")
	}
	bundle, err := operatorReadBackupBundleManifest(*backupPath)
	if err != nil {
		return err
	}
	id := normalizeValidatorID(*nodeID)
	if id == "" {
		id = bundle.NodeID
	}
	if id == "" {
		return errors.New("--id required")
	}
	if bundle.NodeID != "" && !strings.EqualFold(bundle.NodeID, id) {
		return fmt.Errorf("backup belongs to node %s, not %s", bundle.NodeID, id)
	}
	nodePath := nodeDataPath(filepath.Join(operatorResolvePath(*installDir), "data"), id)
	keyDst := filepath.Join(nodePath, "validator.sec")
	existingPub, _ := operatorReadValidatorPubkey(nodePath)
	backupPub := normalizeConsensusPubKeyHex(bundle.ValidatorPubkey)
	if existingPub != "" && backupPub != "" && !strings.EqualFold(existingPub, backupPub) {
		if !*replaceKey || !strings.EqualFold(normalizeConsensusPubKeyHex(*confirmPubkey), backupPub) {
			return fmt.Errorf("existing validator key differs; use --replace-key --confirm-validator-pubkey %s to replace", backupPub)
		}
	}
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
	if err := ensurePrivateDirectory(nodePath); err != nil {
		return err
	}
	if err := operatorCopyBackupFile(*backupPath, "validator.sec", keyDst, true); err != nil {
		return err
	}
	_ = operatorCopyBackupFile(*backupPath, "validator.pub", validatorPublicPath(nodePath), false)
	_ = operatorCopyBackupFile(*backupPath, validatorFingerprintLockFileName, validatorFingerprintLockPath(nodePath), true)
	_ = operatorCopyBackupFile(*backupPath, validatorKeyMetaFileName, validatorKeyMetaPath(nodePath), true)
	_ = operatorCopyBackupFile(*backupPath, validatorKeyBackupManifestFileName, validatorKeyBackupManifestPath(nodePath), true)
	_ = operatorCopyBackupFile(*backupPath, "config.toml", filepath.Join(operatorResolvePath(*installDir), "config.toml"), false)
	manifest := operatorInstallManifest{
		SchemaVersion:        1,
		NodeID:               id,
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
	if err := operatorWriteInstallManifest(*installDir, manifest); err != nil {
		return err
	}
	out["install_manifest"] = operatorCleanPath(operatorInstallManifestPath(*installDir))
	operatorPrintJSON(out)
	return nil
}

type operatorBackupBundleManifest struct {
	SchemaVersion        int      `json:"schema_version"`
	NodeID               string   `json:"node_id"`
	Role                 string   `json:"role,omitempty"`
	GenesisHash          string   `json:"genesis_hash"`
	ValidatorPubkey      string   `json:"validator_pubkey,omitempty"`
	ValidatorFingerprint string   `json:"validator_fingerprint,omitempty"`
	CreatedAt            string   `json:"created_at"`
	Files                []string `json:"files"`
	Warning              string   `json:"warning"`
}

func operatorBackupBundle(args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	id := fs.String("id", "", "node id")
	dataDir := fs.String("datadir", "data", "base data directory")
	nodePathFlag := fs.String("nodepath", "", "direct node data path")
	outDir := fs.String("out", "", "backup output directory")
	jsonOut := fs.Bool("json", true, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*outDir) == "" {
		return operatorBackupWizard(append(args, "--json"))
	}
	nodePath, nodeID, err := operatorValidatorNodePath(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	targetDir := filepath.Join(operatorResolvePath(*outDir), nodeID+"-"+time.Now().UTC().Format("20060102T150405Z"))
	if err := ensurePrivateDirectory(targetDir); err != nil {
		return err
	}
	files := make([]string, 0, 8)
	copyMaybe := func(src, name string, private bool) {
		if src == "" {
			return
		}
		if _, err := os.Stat(src); err != nil {
			return
		}
		dst := filepath.Join(targetDir, name)
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
	installDir := filepath.Dir(filepath.Dir(nodePath))
	copyMaybe(operatorInstallManifestPath(installDir), operatorInstallManifestFile, true)
	pub, _ := operatorReadValidatorPubkey(nodePath)
	fp := operatorReadTextFileTrimmed(validatorFingerprintLockPath(nodePath))
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
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := writePrivateFile(filepath.Join(targetDir, "backup_bundle_manifest.json"), raw); err != nil {
		return err
	}
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

func operatorReadBackupBundleManifest(path string) (operatorBackupBundleManifest, error) {
	path = operatorResolvePath(path)
	raw, err := os.ReadFile(filepath.Join(path, "backup_bundle_manifest.json"))
	if err != nil {
		return operatorBackupBundleManifest{}, err
	}
	var manifest operatorBackupBundleManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return operatorBackupBundleManifest{}, err
	}
	if strings.TrimSpace(manifest.GenesisHash) != "" && !strings.EqualFold(strings.TrimSpace(manifest.GenesisHash), operatorGenesisHashFile("genesis.json")) {
		return operatorBackupBundleManifest{}, fmt.Errorf("backup genesis hash %s does not match local genesis", manifest.GenesisHash)
	}
	return manifest, nil
}

func operatorCopyBackupFile(backupDir, name, dst string, private bool) error {
	src := filepath.Join(operatorResolvePath(backupDir), name)
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if private {
		return operatorCopyFilePrivate(dst, src)
	}
	return operatorCopyFile(dst, src, 0o644)
}

func operatorCopyFilePrivate(dst, src string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writePrivateFile(dst, raw)
}

func operatorCopyFile(dst, src string, mode os.FileMode) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, raw, mode)
}

func operatorReadInstallManifest(installDir string) (operatorInstallManifest, bool) {
	raw, err := os.ReadFile(operatorInstallManifestPath(installDir))
	if err != nil {
		return operatorInstallManifest{}, false
	}
	var manifest operatorInstallManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return operatorInstallManifest{}, false
	}
	return manifest, true
}

func operatorWriteInstallManifest(installDir string, manifest operatorInstallManifest) error {
	if err := ensurePrivateDirectory(operatorResolvePath(installDir)); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateFile(operatorInstallManifestPath(installDir), raw)
}

func operatorInstallManifestPath(installDir string) string {
	return filepath.Join(operatorResolvePath(installDir), operatorInstallManifestFile)
}

func operatorBuildInstallManifest(opts operatorSetupOptions) (operatorInstallManifest, error) {
	installDir := operatorResolvePath(opts.InstallDir)
	dataDir := filepath.Join(installDir, "data")
	nodePath := nodeDataPath(dataDir, opts.NodeID)
	pub, _ := operatorReadValidatorPubkey(nodePath)
	fp := operatorReadTextFileTrimmed(validatorFingerprintLockPath(nodePath))
	return operatorInstallManifest{
		SchemaVersion:        1,
		NodeID:               opts.NodeID,
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

func operatorInstallHasProtectedData(installDir string) bool {
	installDir = operatorResolvePath(installDir)
	for _, rel := range []string{"data", "config.toml", "node.env", "node.env.ps1", "start.sh", "start.ps1", operatorInstallManifestFile} {
		if operatorFileExists(filepath.Join(installDir, rel)) {
			return true
		}
	}
	return false
}

func operatorInstallPreservationReport(installDir string) map[string]any {
	installDir = operatorResolvePath(installDir)
	dataDir := filepath.Join(installDir, "data")
	return map[string]any{
		"keys":      operatorGlobAny(dataDir, "node_*", "validator.sec"),
		"database":  operatorGlobAny(dataDir, "node_*", "*.db"),
		"config":    operatorFileExists(filepath.Join(installDir, "config.toml")),
		"manifest":  operatorFileExists(operatorInstallManifestPath(installDir)),
		"data_dir":  operatorFileExists(dataDir),
		"wallets":   operatorGlobAny(installDir, "*.json"),
		"backups":   operatorGlobAny(dataDir, "node_*", "backups"),
		"protected": operatorInstallHasProtectedData(installDir),
	}
}

func operatorGlobAny(base string, parts ...string) bool {
	pattern := filepath.Join(append([]string{base}, parts...)...)
	matches, err := filepath.Glob(pattern)
	return err == nil && len(matches) > 0
}

func operatorGenesisHashFile(path string) string {
	raw, err := os.ReadFile(operatorResolvePath(path))
	if err != nil {
		return GenesisHashExpected
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func operatorReadTextFileTrimmed(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func operatorServiceName(nodeID string) string {
	id := normalizeValidatorID(nodeID)
	if id == "" {
		return "msc-node"
	}
	if runtime.GOOS == "windows" {
		return "MSC-Node-" + id
	}
	return "msc-" + id
}

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

func operatorDefaultInstallDir() string {
	if runtime.GOOS == "windows" {
		if base := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); base != "" {
			return filepath.Join(base, "MSCNode")
		}
		return "msc-node-install"
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		return "/opt/msc-node"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".msc-node")
	}
	return "msc-node-install"
}

func operatorInstallerScriptPath(goos string) (string, error) {
	var names []string
	switch goos {
	case "windows":
		names = []string{filepath.Join("scripts", "install_msc_node.ps1"), "install_msc_node.ps1"}
	case "linux":
		names = []string{filepath.Join("scripts", "install_msc_node.sh"), "install_msc_node.sh"}
	default:
		return "", fmt.Errorf("unsupported setup OS %q", goos)
	}
	candidates := append([]string{}, names...)
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		for _, name := range names {
			candidates = append(candidates, filepath.Join(base, name))
		}
	}
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("installer script not found; run from repo root or packaged installer")
}

func operatorBuildSetupInstallerCommand(scriptPath string, opts operatorSetupOptions, prebuilt string) (string, []string) {
	if runtime.GOOS == "windows" {
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
	VersionTag string                    `json:"version_tag"`
	Artifacts  []operatorReleaseArtifact `json:"artifacts"`
}

type operatorReleaseArtifact struct {
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	File      string `json:"file"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

func operatorDownloadReleaseBinary(manifestURL, publicKey, goos, goarch string) (string, func(), error) {
	manifestURL = strings.TrimSpace(manifestURL)
	if manifestURL == "" {
		return "", nil, errors.New("--release-manifest-url required for release source")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	manifestRaw, err := operatorFetchURLBytes(client, manifestURL, 4<<20)
	if err != nil {
		return "", nil, err
	}
	artifact, artifactURL, err := operatorSelectReleaseArtifact(manifestRaw, manifestURL, goos, goarch)
	if err != nil {
		return "", nil, err
	}
	binaryRaw, err := operatorFetchURLBytes(client, artifactURL, 256<<20)
	if err != nil {
		return "", nil, err
	}
	if _, err := operatorVerifyReleaseArtifactBytes(artifact, binaryRaw, publicKey); err != nil {
		return "", nil, err
	}
	ext := ""
	if goos == "windows" {
		ext = ".exe"
	}
	tmp, err := os.CreateTemp("", "msc-release-*"+ext)
	if err != nil {
		return "", nil, err
	}
	path := tmp.Name()
	if _, err := tmp.Write(binaryRaw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(path)
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, err
	}
	if goos != "windows" {
		_ = os.Chmod(path, 0o755)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func operatorFetchURLBytes(client *http.Client, rawURL string, max int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
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

func operatorSelectReleaseArtifact(manifestRaw []byte, manifestURL, goos, goarch string) (operatorReleaseArtifact, string, error) {
	var manifest operatorReleaseManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return operatorReleaseArtifact{}, "", err
	}
	for _, artifact := range manifest.Artifacts {
		if strings.EqualFold(artifact.OS, goos) && strings.EqualFold(artifact.Arch, goarch) {
			artifactURL := strings.TrimSpace(artifact.URL)
			if artifactURL == "" {
				if strings.TrimSpace(artifact.File) == "" {
					return operatorReleaseArtifact{}, "", errors.New("release artifact missing file/url")
				}
				base, err := url.Parse(manifestURL)
				if err != nil {
					return operatorReleaseArtifact{}, "", err
				}
				base.Path = strings.TrimRight(base.Path, "/")
				if idx := strings.LastIndex(base.Path, "/"); idx >= 0 {
					base.Path = base.Path[:idx+1]
				}
				ref, _ := url.Parse(artifact.File)
				artifactURL = base.ResolveReference(ref).String()
			}
			return artifact, artifactURL, nil
		}
	}
	return operatorReleaseArtifact{}, "", fmt.Errorf("no release artifact for %s/%s", goos, goarch)
}

func operatorVerifyReleaseArtifactBytes(artifact operatorReleaseArtifact, data []byte, publicKey string) (bool, error) {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])
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
	pub, err := operatorDecodeHexOrBase64(publicKey)
	if err != nil {
		return false, fmt.Errorf("invalid release public key: %w", err)
	}
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

func operatorDecodeHexOrBase64(raw string) ([]byte, error) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "0x")
	if b, err := hex.DecodeString(cleaned); err == nil {
		return b, nil
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(raw))
}

type operatorDoctorReport struct {
	Command string                `json:"command"`
	OK      bool                  `json:"ok"`
	Result  string                `json:"result"`
	NodeID  string                `json:"node_id,omitempty"`
	Role    string                `json:"role,omitempty"`
	Checks  []operatorDoctorCheck `json:"checks"`
	Details map[string]any        `json:"details,omitempty"`
}

type operatorDoctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func operatorDoctorCommand(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	nodeID := fs.String("id", "", "node id")
	role := fs.String("role", "auto", "node role")
	dataDir := fs.String("datadir", "data", "base data directory")
	nodePath := fs.String("nodepath", "", "direct node data path")
	configPath := fs.String("config", "config.toml", "config path")
	genesisPath := fs.String("genesis", "genesis.json", "genesis path")
	jsonOut := fs.Bool("json", false, "print JSON")
	rpcFlags := registerOperatorRPCFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	report := operatorCollectDoctorReport(*nodeID, *role, *dataDir, *nodePath, *configPath, *genesisPath, rpcFlags)
	if *jsonOut {
		operatorPrintJSON(report)
		return nil
	}
	operatorPrintDoctorReport(report)
	return nil
}

func operatorCollectDoctorReport(nodeID, role, dataDir, nodePath, configPath, genesisPath string, rpcFlags *operatorRPCFlags) operatorDoctorReport {
	report := operatorDoctorReport{
		Command: "doctor",
		NodeID:  normalizeValidatorID(nodeID),
		Role:    strings.ToLower(strings.TrimSpace(role)),
		OK:      true,
		Result:  "ok",
		Details: map[string]any{},
	}
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
	configRaw, configErr := os.ReadFile(operatorResolvePath(configPath))
	if configErr != nil {
		add("config", "error", configErr.Error())
	} else {
		add("config", "ok", operatorCleanPath(configPath))
	}
	genesisHash := ""
	if raw, err := os.ReadFile(operatorResolvePath(genesisPath)); err != nil {
		add("genesis", "error", err.Error())
	} else {
		sum := sha256.Sum256(raw)
		genesisHash = hex.EncodeToString(sum[:])
		expected := GenesisHashExpected
		if len(configRaw) > 0 {
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
		p2pBind := operatorTOMLStringValue(string(configRaw), "p2p", "laddr")
		if p2pBind == "" {
			add("p2p", "unknown", "p2p.laddr empty; command-line/default port may be used")
		} else {
			add("p2p", "ok", p2pBind)
		}
	}
	if rpcFlags != nil {
		if status, err := operatorGET(rpcFlags, "/status", url.Values{"full": []string{"1"}}); err != nil {
			add("sync", "warn", "RPC status unavailable: "+err.Error())
		} else {
			height := operatorJSONInt(status["height"])
			finalized := operatorJSONInt(status["finalized_height"])
			add("sync", "ok", fmt.Sprintf("height=%d finalized=%d", height, finalized))
			report.Details["status"] = status
		}
		if peers, err := operatorGET(rpcFlags, "/v1/peers", nil); err != nil {
			add("peers", "warn", "peer endpoint unavailable: "+err.Error())
		} else {
			count := operatorJSONInt(peers["count"])
			add("peers", "ok", fmt.Sprintf("count=%d", count))
			report.Details["peers"] = peers
		}
	}
	nodePathResolved := ""
	if report.NodeID != "" || strings.TrimSpace(nodePath) != "" {
		if path, _, err := operatorValidatorNodePath(report.NodeID, dataDir, nodePath); err == nil {
			nodePathResolved = path
			report.Details["node_path"] = operatorCleanPath(path)
			if _, err := os.Stat(filepath.Join(path, "validator.sec")); err == nil {
				add("validator_key", "ok", filepath.Join(path, "validator.sec"))
			} else {
				add("validator_key", "warn", "validator.sec not found; run validator-keygen or MPC setup")
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
		if latest, age, ok := operatorLatestBackup(nodePathResolved); ok {
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

func operatorPrintDoctorReport(report operatorDoctorReport) {
	fmt.Println("MSC Doctor")
	fmt.Println("Result:", strings.ToUpper(report.Result))
	if report.NodeID != "" {
		fmt.Println("Node:", report.NodeID)
	}
	for _, check := range report.Checks {
		fmt.Printf("[%s] %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Detail)
	}
}

func operatorServiceCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("service subcommand required (start/stop/status)")
	}
	sub := strings.ToLower(strings.TrimSpace(args[0]))
	fs := flag.NewFlagSet("service "+sub, flag.ContinueOnError)
	installDir := fs.String("install-dir", operatorDefaultInstallDir(), "install directory")
	nodeID := fs.String("id", "", "node id for systemd service names")
	dryRun := fs.Bool("dry-run", false, "print service command without running")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cmdName, cmdArgs, err := operatorBuildServiceCommand(sub, *installDir, normalizeValidatorID(*nodeID))
	if err != nil {
		return err
	}
	out := map[string]any{"command": "service " + sub, "install_dir": operatorCleanPath(*installDir), "exec": append([]string{cmdName}, cmdArgs...), "dry_run": *dryRun}
	if *dryRun {
		out["binary_exists"] = operatorFileExists(filepath.Join(*installDir, operatorBinaryName()))
		out["alias_exists"] = operatorFileExists(filepath.Join(*installDir, operatorAliasBinaryName()))
		operatorPrintJSON(out)
		return nil
	}
	if sub == "status" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, cmdName, cmdArgs...)
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
	cmd := exec.Command(cmdName, cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return err
	}
	operatorPrintJSON(out)
	return nil
}

func operatorBuildServiceCommand(sub, installDir, nodeID string) (string, []string, error) {
	switch sub {
	case "start":
		if runtime.GOOS == "windows" {
			return "powershell", []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(installDir, "start.ps1")}, nil
		}
		return "bash", []string{filepath.Join(installDir, "start.sh")}, nil
	case "stop":
		if runtime.GOOS == "windows" {
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

func operatorBackupWizard(args []string) error {
	fs := flag.NewFlagSet("backup wizard", flag.ContinueOnError)
	id := fs.String("id", "", "node id")
	dataDir := fs.String("datadir", "data", "base data directory")
	nodePathFlag := fs.String("nodepath", "", "direct node data path")
	jsonOut := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	nodePath, nodeID, err := operatorValidatorNodePath(*id, *dataDir, *nodePathFlag)
	if err != nil {
		return err
	}
	commands := []string{
		fmt.Sprintf("msc backup export --id %s --datadir %s", nodeID, *dataDir),
		"msc backup verify --path <backup_dir>",
		"Copy validator.sec / secure-backups to offline encrypted storage",
	}
	out := map[string]any{
		"command":             "backup wizard",
		"validator":           nodeID,
		"node_path":           operatorCleanPath(nodePath),
		"validator_key":       operatorFileExists(filepath.Join(nodePath, "validator.sec")),
		"validator_pubkey":    operatorFileExists(validatorPublicPath(nodePath)),
		"secure_backups_dir":  operatorCleanPath(filepath.Join(nodePath, ValidatorKeyBackupDir)),
		"secure_backups_seen": operatorFileExists(filepath.Join(nodePath, ValidatorKeyBackupDir)),
		"next_steps":          commands,
		"warning":             "Keep validator keys, wallet passwords, and backup media offline/private. Never commit them.",
	}
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
	for _, step := range commands {
		fmt.Println(" -", step)
	}
	fmt.Println("Warning:", out["warning"])
	return nil
}

func operatorTotalMemoryBytes() uint64 {
	if runtime.GOOS == "linux" {
		raw, err := os.ReadFile("/proc/meminfo")
		if err == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				if strings.HasPrefix(line, "MemTotal:") {
					fields := strings.Fields(line)
					if len(fields) >= 2 {
						kb, _ := strconv.ParseUint(fields[1], 10, 64)
						return kb * 1024
					}
				}
			}
		}
	}
	if runtime.GOOS == "windows" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory").Output()
		if err == nil {
			v, _ := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
			return v
		}
	}
	return 0
}

func operatorDiskFreeBytes(path string) uint64 {
	path = operatorResolvePath(path)
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(path)
		if volume == "" {
			volume = "C:"
		}
		drive := strings.TrimSuffix(volume, ":")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := fmt.Sprintf("(Get-PSDrive -Name %s).Free", strconv.Quote(drive))
		out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-Command", cmd).Output()
		if err == nil {
			v, _ := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
			return v
		}
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "df", "-Pk", path).Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return 0
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0
	}
	kb, _ := strconv.ParseUint(fields[3], 10, 64)
	return kb * 1024
}

func operatorTOMLStringValue(raw, section, key string) string {
	current := ""
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
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) != key {
			continue
		}
		return strings.Trim(strings.TrimSpace(parts[1]), `"`)
	}
	return ""
}

func operatorLatestBackup(nodePath string) (string, time.Duration, bool) {
	backupsDir := filepath.Join(nodePath, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return "", 0, false
	}
	var newest os.FileInfo
	var newestName string
	for _, entry := range entries {
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

func operatorFileExists(path string) bool {
	_, err := os.Stat(operatorResolvePath(path))
	return err == nil
}

func operatorBinaryName() string {
	if runtime.GOOS == "windows" {
		return "msc-node.exe"
	}
	return "msc-node"
}

func operatorAliasBinaryName() string {
	if runtime.GOOS == "windows" {
		return "msc.exe"
	}
	return "msc"
}

func operatorCommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func operatorSortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
