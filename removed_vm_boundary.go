package main

import (
	"encoding/json"
	"strings"
)

const (
	// Transaction type 7 stays permanently reserved. It must never be reused
	// after removal of the old VM envelope.
	removedLegacyVMTxType TxType = 7
)

// containsRemovedVMJSONFields detects legacy envelope keys before strict JSON
// decoding. This turns old clients into an explicit permanent rejection instead
// of allowing a removed field to be ignored or normalized away.
func containsRemovedVMJSONFields(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return false
	}
	for key := range object {
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "evm_code", "evm_input", "evm_gas_limit", "evm_raw_tx", "evm_tx_hash":
			return true
		}
	}
	return false
}
