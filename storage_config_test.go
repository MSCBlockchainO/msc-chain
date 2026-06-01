package main

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestStorageConfigAppliesMainnetRetentionPolicy(t *testing.T) {
	oldEpochLength := StorageEpochLengthBlocks
	oldHistoryProfile := StorageHistoryProfile
	oldPruningEnabled := StorageStatePruningEnabled
	oldRetainedEpochs := StorageValidatorRetainedEpochs
	oldRollbackWindow := StorageValidatorRollbackWindowBlocks
	oldSnapshotKeepLast := StorageValidatorSnapshotKeepLast
	oldRecentBlockWindow := StorageValidatorRecentBlockWindow
	oldFullNodeHistory := StorageFullNodeHistoryBlocks
	oldHourlyRetain := StorageHourlySnapshotRetain
	oldDailyRetain := StorageDailySnapshotRetain
	oldWeeklyRetain := StorageWeeklySnapshotRetain
	oldMonthlyRetain := StorageMonthlySnapshotRetain
	oldHourlyInterval := StorageHourlySnapshotIntervalBlocks
	oldColdEnabled := StorageColdExportEnabled
	oldColdCompression := StorageColdExportCompression
	oldGCWorkers := StorageParallelGCWorkers
	oldStateRentEnabled := StorageStateRentEnabled
	oldStateRentEpochs := StorageStateRentArchiveInactiveAfterEpochs
	oldStateLayout := StorageStateLayoutMode
	t.Cleanup(func() {
		StorageEpochLengthBlocks = oldEpochLength
		StorageHistoryProfile = oldHistoryProfile
		StorageStatePruningEnabled = oldPruningEnabled
		StorageValidatorRetainedEpochs = oldRetainedEpochs
		StorageValidatorRollbackWindowBlocks = oldRollbackWindow
		StorageValidatorSnapshotKeepLast = oldSnapshotKeepLast
		StorageValidatorRecentBlockWindow = oldRecentBlockWindow
		StorageFullNodeHistoryBlocks = oldFullNodeHistory
		StorageHourlySnapshotRetain = oldHourlyRetain
		StorageDailySnapshotRetain = oldDailyRetain
		StorageWeeklySnapshotRetain = oldWeeklyRetain
		StorageMonthlySnapshotRetain = oldMonthlyRetain
		StorageHourlySnapshotIntervalBlocks = oldHourlyInterval
		StorageColdExportEnabled = oldColdEnabled
		StorageColdExportCompression = oldColdCompression
		StorageParallelGCWorkers = oldGCWorkers
		StorageStateRentEnabled = oldStateRentEnabled
		StorageStateRentArchiveInactiveAfterEpochs = oldStateRentEpochs
		StorageStateLayoutMode = oldStateLayout
	})

	coldEnabled := true
	pruningEnabled := true
	stateRentEnabled := true
	changed := applyStorageConfig(StorageConfig{
		HistoryProfile:                      "FULL",
		StatePruningEnabled:                 &pruningEnabled,
		EpochLengthBlocks:                   uint64Ptr(50),
		ValidatorRetainedEpochs:             uint64Ptr(12),
		ValidatorRollbackWindowBlocks:       uint64Ptr(512),
		ValidatorSnapshotKeepLast:           uint64Ptr(4),
		ValidatorRecentBlockWindow:          uint64Ptr(4096),
		FullNodeHistoryBlocks:               uint64Ptr(1000000),
		HourlySnapshotRetain:                uint64Ptr(6),
		DailySnapshotRetain:                 uint64Ptr(7),
		WeeklySnapshotRetain:                uint64Ptr(8),
		MonthlySnapshotRetain:               uint64Ptr(9),
		HourlySnapshotIntervalBlocks:        uint64Ptr(25),
		ColdExportEnabled:                   &coldEnabled,
		ColdExportCompression:               "ZSTD",
		ParallelGCWorkers:                   uint64Ptr(6),
		StateRentEnabled:                    &stateRentEnabled,
		StateRentArchiveInactiveAfterEpochs: uint64Ptr(99),
		StateLayoutMode:                     "MERKLE",
	})
	if !changed {
		t.Fatal("expected storage config to report changed")
	}

	if StorageEpochLengthBlocks != 50 ||
		StorageHistoryProfile != storageProfileFull ||
		!StorageStatePruningEnabled ||
		StorageValidatorRetainedEpochs != 12 ||
		StorageValidatorRollbackWindowBlocks != 512 ||
		StorageValidatorSnapshotKeepLast != 4 ||
		StorageValidatorRecentBlockWindow != 4096 ||
		StorageFullNodeHistoryBlocks != 1000000 ||
		StorageHourlySnapshotRetain != 6 ||
		StorageDailySnapshotRetain != 7 ||
		StorageWeeklySnapshotRetain != 8 ||
		StorageMonthlySnapshotRetain != 9 ||
		StorageHourlySnapshotIntervalBlocks != 25 ||
		!StorageColdExportEnabled ||
		StorageColdExportCompression != "zstd" ||
		StorageParallelGCWorkers != 6 ||
		!StorageStateRentEnabled ||
		StorageStateRentArchiveInactiveAfterEpochs != 99 ||
		StorageStateLayoutMode != "merkle" {
		t.Fatalf("storage globals not updated correctly")
	}
}

func TestConfigTomlCarriesMainnetStoragePolicy(t *testing.T) {
	var cfg ConfigFile
	if _, err := toml.DecodeFile("config.toml", &cfg); err != nil {
		t.Fatalf("decode config.toml: %v", err)
	}
	if cfg.Storage.ValidatorRetainedEpochs == nil || *cfg.Storage.ValidatorRetainedEpochs != 10 {
		t.Fatalf("validator retained epochs not pinned in config")
	}
	if normalizeStorageHistoryProfile(cfg.Storage.HistoryProfile) != storageProfileAuto {
		t.Fatalf("storage history profile must default to auto")
	}
	if cfg.Storage.StatePruningEnabled == nil || !*cfg.Storage.StatePruningEnabled {
		t.Fatalf("state pruning must be explicitly enabled for mainnet")
	}
	if cfg.Storage.ValidatorSnapshotKeepLast == nil || *cfg.Storage.ValidatorSnapshotKeepLast != 3 {
		t.Fatalf("validator snapshot keep-last not pinned in config")
	}
	if cfg.Storage.ColdExportEnabled == nil || !*cfg.Storage.ColdExportEnabled {
		t.Fatalf("cold export must be enabled for validator pruning safety")
	}
	if cfg.Storage.FullNodeHistoryBlocks == nil || *cfg.Storage.FullNodeHistoryBlocks == 0 {
		t.Fatalf("full node history window must be configured separately from validator pruning")
	}
	if cfg.Storage.ParallelGCWorkers == nil || *cfg.Storage.ParallelGCWorkers < 1 {
		t.Fatalf("parallel GC workers must be configured")
	}
}

func uint64Ptr(v uint64) *uint64 {
	return &v
}
