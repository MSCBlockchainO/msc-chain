package main

import (
	"strings"

	registrycore "msc-chain/registry"
)

func newExecutionRegistrySnapshot(height uint64, records map[string]ValidatorRecord) registrycore.Snapshot {
	out := make(map[string]registrycore.Record, len(records))
	for rawID, record := range records {
		id := normalizeValidatorID(rawID)
		if id == "" {
			id = normalizeValidatorID(record.ID)
		}
		if id == "" {
			continue
		}
		status := registrycore.Pending
		switch record.Status {
		case ValidatorActive:
			status = registrycore.Active
		case ValidatorJailed:
			status = registrycore.Jailed
		case ValidatorExited, ValidatorRemoved:
			status = registrycore.Exited
		}
		stake := uint64(0)
		if record.Stake > 0 {
			stake = uint64(record.Stake)
		}
		out[id] = registrycore.Record{
			ID:               id,
			PublicKey:        []byte(strings.TrimSpace(record.ConsensusPubKey)),
			Stake:            stake,
			Status:           status,
			ActivationHeight: record.JoinHeight,
		}
	}
	return registrycore.NewSnapshot(
		height,
		height,
		strings.TrimSpace(ValidatorRegistrySnapshotHash(copyValidatorRegistrySnapshot(records))),
		out,
	)
}
