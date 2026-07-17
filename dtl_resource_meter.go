package main

import (
	"fmt"
	"math"
	"strings"

	dtlcore "msc-chain/dtl"
)

const (
	protocolDTLMaxReads       uint64 = 64
	protocolDTLMaxWrites      uint64 = 64
	protocolDTLMaxEvents      uint64 = 256
	protocolDTLMaxSteps       uint64 = 1 << 20
	protocolDTLMaxStorageByte uint64 = 1 << 20
	protocolDTLReadFee        uint64 = 1
	protocolDTLWriteFee       uint64 = 10
	protocolDTLEventFee       uint64 = 5
	protocolDTLStepFee        uint64 = 1
	protocolDTLStorageByteFee uint64 = 1
)

func protocolNativeDTLResourceLimits() dtlcore.Limits {
	return dtlcore.Limits{
		MaxReads:       protocolDTLMaxReads,
		MaxWrites:      protocolDTLMaxWrites,
		MaxEvents:      protocolDTLMaxEvents,
		MaxSteps:       protocolDTLMaxSteps,
		MaxStorageByte: protocolDTLMaxStorageByte,
		ReadFee:        protocolDTLReadFee,
		WriteFee:       protocolDTLWriteFee,
		EventFee:       protocolDTLEventFee,
		StepFee:        protocolDTLStepFee,
		StorageByteFee: protocolDTLStorageByteFee,
	}
}

func dtlResourceUsageZero() dtlcore.Usage { return dtlcore.Usage{} }

func checkedResourceSize(total uint64, value string) (uint64, error) {
	delta := uint64(len(value))
	if delta > math.MaxUint64-total {
		return 0, fmt.Errorf("dtl: resource size overflow")
	}
	return total + delta, nil
}

// deterministicDTLResourceUsage derives resource cost exclusively from the
// canonical transaction envelope and produced logs. A transition that exceeds
// limits is rejected before its cloned state is returned to the caller.
func deterministicDTLResourceUsage(tx Transaction, logs []DTLEventLog, stateChanged bool) (dtlcore.Usage, error) {
	steps := uint64(1)
	storageBytes := uint64(0)
	var err error
	for _, value := range []string{strings.TrimSpace(tx.DTLTxType), strings.TrimSpace(tx.DTLTokenID), strings.TrimSpace(tx.DTLPayload), strings.TrimSpace(tx.DTLGovernanceCert)} {
		steps, err = checkedResourceSize(steps, value)
		if err != nil {
			return dtlcore.Usage{}, err
		}
	}
	for _, event := range logs {
		for _, value := range append([]string{event.ContractID, event.Data}, event.Topics...) {
			storageBytes, err = checkedResourceSize(storageBytes, value)
			if err != nil {
				return dtlcore.Usage{}, err
			}
		}
	}
	// Payload bytes represent the upper bound of newly materialized state for
	// the operation; event bytes are charged independently as durable storage.
	storageBytes, err = checkedResourceSize(storageBytes, tx.DTLPayload)
	if err != nil {
		return dtlcore.Usage{}, err
	}
	meter := dtlcore.NewMeter(protocolNativeDTLResourceLimits())
	if err := meter.Read(); err != nil {
		return dtlcore.Usage{}, err
	}
	if err := meter.Step(steps); err != nil {
		return dtlcore.Usage{}, err
	}
	if stateChanged {
		if err := meter.Write(storageBytes); err != nil {
			return dtlcore.Usage{}, err
		}
	}
	for range logs {
		if err := meter.Event(); err != nil {
			return dtlcore.Usage{}, err
		}
	}
	return meter.Usage(), nil
}
