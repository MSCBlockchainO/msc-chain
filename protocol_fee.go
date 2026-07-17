package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// dtlBaseFeeByType returns the immutable protocol fee assigned to a DTL
// operation. It deliberately does not read node-local configuration.
func dtlBaseFeeByType(dtlTxType string) int {
	switch strings.ToUpper(strings.TrimSpace(dtlTxType)) {
	case string(DTLTxTokenCreate),
		string(DTLTxPoolCreate),
		string(DTLTxDuelCreate),
		string(DTLTxLendMarketCreate),
		string(DTLTxTournamentCreate),
		string(DTLTxFarmCreate),
		string(DTLTxSeasonCreate),
		string(DTLTxNFT721Create),
		string(DTLTxNFT1155Create):
		return DTLDefaultCreateBaseFee
	case string(DTLTxTokenMint),
		string(DTLTxNFT721Mint),
		string(DTLTxNFT1155Mint):
		return DTLDefaultMintBaseFee
	case string(DTLTxTokenBurn):
		return DTLDefaultBurnBaseFee
	case string(DTLTxTokenTransfer),
		string(DTLTxNFT721Transfer),
		string(DTLTxNFT1155Transfer),
		string(DTLTxTokenApprove),
		string(DTLTxTokenTransferFrom),
		string(DTLTxPoolAdd),
		string(DTLTxPoolRemove),
		string(DTLTxPoolSwap),
		string(DTLTxPoolSwapRoute),
		string(DTLTxDuelJoin),
		string(DTLTxDuelReveal),
		string(DTLTxDuelFinalize),
		string(DTLTxLendDeposit),
		string(DTLTxLendBorrow),
		string(DTLTxLendRepay),
		string(DTLTxLendWithdraw),
		string(DTLTxLendLiquidate),
		string(DTLTxTournamentJoin),
		string(DTLTxTournamentReveal),
		string(DTLTxTournamentFinalize),
		string(DTLTxFarmStakeLP),
		string(DTLTxFarmUnstakeLP),
		string(DTLTxFarmClaim),
		string(DTLTxSeasonFinalize),
		string(DTLTxSeasonClaim):
		return DTLDefaultTransferBaseFee
	default:
		return DTLDefaultTransferBaseFee
	}
}

func requiredDTLFeeByPayload(dtlTxType string, payloadBytes int) int {
	fee := dtlBaseFeeByType(dtlTxType)
	if fee <= 0 {
		fee = 1
	}
	if payloadBytes < 0 {
		payloadBytes = 0
	}
	if DTLDefaultPayloadFeePerKB > 0 && payloadBytes > 0 {
		fee += ((payloadBytes + 1023) / 1024) * DTLDefaultPayloadFeePerKB
	}
	if fee < 1 {
		return 1
	}
	return fee
}

func requiredDTLFeeForTx(tx Transaction) int {
	payloadBytes := len(strings.TrimSpace(tx.DTLPayload)) + len(strings.TrimSpace(tx.DTLGovernanceCert))
	return requiredDTLFeeByPayload(tx.DTLTxType, payloadBytes)
}

func maxAllowedDTLFee(requiredFee int) int {
	if requiredFee < 1 {
		requiredFee = 1
	}
	multiplier := DTLDefaultFeeMaxMultiplier
	if multiplier < 1 {
		multiplier = 1
	}
	limit := requiredFee * multiplier
	if limit < requiredFee {
		return requiredFee
	}
	return limit
}

func validateDTLFeeBounds(gotFee int, requiredFee int) error {
	if gotFee < requiredFee {
		return fmt.Errorf("invalid fee: got %d minimum %d", gotFee, requiredFee)
	}
	maxFee := maxAllowedDTLFee(requiredFee)
	if gotFee > maxFee {
		return fmt.Errorf("invalid fee: got %d maximum %d", gotFee, maxFee)
	}
	return nil
}

// requiredFeeForTx is a pure protocol rule. Removed VM envelopes are rejected
// before this function is reached, so no VM gas market participates in MSC or
// DTL fee calculation.
func requiredFeeForTx(tx Transaction) int {
	if tx.Type == TxDTL {
		return requiredDTLFeeForTx(tx)
	}
	return ComputeTxFee(tx.Amount)
}

func requiredFeeForTxWithLedger(_ *Ledger, tx Transaction) int {
	return requiredFeeForTx(tx)
}

func stripHexPrefix(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X")) {
		return v[2:]
	}
	return v
}

func decodeHexBytes(v string) ([]byte, error) {
	raw := stripHexPrefix(v)
	if raw == "" {
		return []byte{}, nil
	}
	if len(raw)%2 != 0 {
		return nil, errors.New("hex payload must have even length")
	}
	out, err := hex.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	return out, nil
}
