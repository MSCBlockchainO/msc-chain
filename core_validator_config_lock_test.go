package main

import (
	"testing"

	"github.com/BurntSushi/toml"
)

type coreValidatorConfigCheck struct {
	Consensus struct {
		CorePendingExcludedFromProposer     bool   `toml:"core_pending_excluded_from_proposer"`
		CoreActivationEffectiveHeightBuffer uint64 `toml:"core_activation_effective_height_buffer"`
	} `toml:"consensus"`

	Validators struct {
		RequiredKeyFingerprint          string `toml:"required_key_fingerprint"`
		PasswordMode                    string `toml:"password_mode"`
		FailOnKeyUnavailable            bool   `toml:"fail_on_key_unavailable"`
		AllowIdentityRotationOnExisting bool   `toml:"allow_identity_rotation_on_existing_chain"`
		KeyBackupRequired               bool   `toml:"key_backup_required"`
		KeyRestoreAllowedOnMissing      bool   `toml:"key_restore_allowed_on_missing"`
		AllowEnvPasswordInProduction    bool   `toml:"allow_env_password_in_production"`
		CoreEnvPasswordAllowed          bool   `toml:"core_env_password_allowed"`
	} `toml:"validators"`

	Core struct {
		RegistryPath        string `toml:"registry_path"`
		RegistryEnforcement string `toml:"registry_enforcement"`
		RegistryMinSigs     int    `toml:"registry_min_signatures"`
		RegistryReloadSec   uint64 `toml:"registry_reload_seconds"`
	} `toml:"core"`
}

func TestCoreValidatorConfigFingerprintLocks(t *testing.T) {
	file := "config.toml"
	var cfg coreValidatorConfigCheck
	if _, err := toml.DecodeFile(file, &cfg); err != nil {
		t.Fatalf("decode %s: %v", file, err)
	}

	if !cfg.Validators.FailOnKeyUnavailable {
		t.Fatalf("%s fail_on_key_unavailable must be true", file)
	}
	if cfg.Validators.AllowIdentityRotationOnExisting {
		t.Fatalf("%s allow_identity_rotation_on_existing_chain must be false", file)
	}
	if !cfg.Validators.KeyBackupRequired {
		t.Fatalf("%s key_backup_required must be true", file)
	}
	if !cfg.Validators.KeyRestoreAllowedOnMissing {
		t.Fatalf("%s key_restore_allowed_on_missing must be true", file)
	}
	if cfg.Validators.PasswordMode == "" {
		t.Fatalf("%s validators.password_mode must be set", file)
	}
	if !cfg.Consensus.CorePendingExcludedFromProposer {
		t.Fatalf("%s consensus.core_pending_excluded_from_proposer must be true", file)
	}
	if cfg.Consensus.CoreActivationEffectiveHeightBuffer == 0 {
		t.Fatalf("%s consensus.core_activation_effective_height_buffer must be > 0", file)
	}
	if cfg.Core.RegistryPath == "" {
		t.Fatalf("%s core.registry_path must be set", file)
	}
	if cfg.Core.RegistryEnforcement != "warn" && cfg.Core.RegistryEnforcement != "enforce" {
		t.Fatalf("%s core.registry_enforcement must be warn|enforce, got=%q", file, cfg.Core.RegistryEnforcement)
	}
	if cfg.Core.RegistryMinSigs < 0 {
		t.Fatalf("%s core.registry_min_signatures must be >= 0", file)
	}
	if cfg.Core.RegistryReloadSec == 0 {
		t.Fatalf("%s core.registry_reload_seconds must be > 0", file)
	}
}
