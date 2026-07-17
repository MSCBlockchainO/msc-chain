package main

import (
	"fmt"
	"strings"
)

func executeDTLBytecodeCallWithContext(
	state *DTLState,
	contractID string,
	contract *DTLContractState,
	tx DTLContractCallTx,
	ctx dtlLogicExecContext,
	readOnly bool,
) (dtlLogicCallResult, error) {
	result := dtlLogicCallResult{}
	if state == nil {
		return result, ErrDTLInvalidState
	}
	if contract == nil {
		return result, fmt.Errorf("dtl: unknown contract")
	}
	if strings.TrimSpace(contract.Bytecode) == "" {
		return result, fmt.Errorf("dtl: missing contract bytecode")
	}
	_, normalizedPack, _, err := decodeNormalizeValidateDTLBytecode(state, contract.Bytecode)
	if err != nil {
		return result, err
	}
	execContract := *contract
	execContract.LogicPack = cloneDTLLogicPack(normalizedPack)
	return executeDTLLogicPackCallWithContext(state, contractID, &execContract, tx, ctx, readOnly)
}
