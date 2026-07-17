package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// `DTLMaxNameLen` defines the measured quantity used by this operation.
	DTLMaxNameLen = 64
	// `DTLMaxSymbolLen` defines the measured quantity used by this operation.
	DTLMaxSymbolLen = 16
	// `DTLMaxDecimals` defines the constant value used by this package.
	DTLMaxDecimals = 18
	// `DTLMaxTaxBPS` defines the constant value used by this package.
	DTLMaxTaxBPS = 10000
	// `DTLMaxPoolFeeBPS` defines the constant value used by this package.
	DTLMaxPoolFeeBPS = 1000
	// `DTLMaxLTVBPS` defines the constant value used by this package.
	DTLMaxLTVBPS = 9500
	// `DTLMaxLiqBonusBPS` defines the constant value used by this package.
	DTLMaxLiqBonusBPS = 2000
	// `DTLMaxTournamentPlayers` defines the constant value used by this package.
	DTLMaxTournamentPlayers = 256
	// `DTLMaxContractMethods` defines the constant value used by this package.
	DTLMaxContractMethods = 64
	// `DTLMaxContractArgs` defines the constant value used by this package.
	DTLMaxContractArgs = 64
	// `DTLMaxContractKeyLen` defines the measured quantity used by this operation.
	DTLMaxContractKeyLen = 64
	// `DTLMaxContractValueLen` defines the measured quantity used by this operation.
	DTLMaxContractValueLen = 512
	// `DTLLogicPackVersionV1` defines the constant value used by this package.
	DTLLogicPackVersionV1 = 1
	// `DTLLogicPackVersionV2` defines the constant value used by this package.
	DTLLogicPackVersionV2 = 2
	// `DTLLogicPackVersionV3` defines the constant value used by this package.
	DTLLogicPackVersionV3 = 3
	// `DTLLogicPackVersion` defines the constant value used by this package.
	DTLLogicPackVersion = DTLLogicPackVersionV1
	// `DTLMaxLogicPackStorage` defines the constant value used by this package.
	DTLMaxLogicPackStorage = 256
	// `DTLMaxLogicPackOps` defines the constant value used by this package.
	DTLMaxLogicPackOps = 256
	// `DTLMaxLogicPackTotalOps` defines the constant value used by this package.
	DTLMaxLogicPackTotalOps = 4096
	// `DTLMaxLogicPackSteps` defines the constant value used by this package.
	DTLMaxLogicPackSteps = 4096
	// `DTLMaxLogicPackReads` defines the constant value used by this package.
	DTLMaxLogicPackReads = 1024
	// `DTLMaxLogicPackWrites` defines the constant value used by this package.
	DTLMaxLogicPackWrites = 1024
	// `DTLMaxLogicPackTransfers` defines the constant value used by this package.
	DTLMaxLogicPackTransfers = 128
	// `DTLMaxLogicPackLogs` defines the constant value used by this package.
	DTLMaxLogicPackLogs = 128
	// `DTLMaxLogicPackMapReads` defines the constant value used by this package.
	DTLMaxLogicPackMapReads = 1024
	// `DTLMaxLogicPackMapWrites` defines the constant value used by this package.
	DTLMaxLogicPackMapWrites = 1024
	// `DTLMaxLogicPackCrossCalls` defines the constant value used by this package.
	DTLMaxLogicPackCrossCalls = 64
	// `DTLMaxLogicPackRoleOps` defines the constant value used by this package.
	DTLMaxLogicPackRoleOps = 64
	// Scaffold treasury sink for static tax in DTL transfer.
	DTLTreasuryAccount = "MSC_DTL_TREASURY"
	// `DTLDefaultPoolFeeBPS` defines the constant value used by this package.
	DTLDefaultPoolFeeBPS = 30
	// `DTLDefaultDuelJoinBlocks` defines the constant value used by this package.
	DTLDefaultDuelJoinBlocks uint64 = 20
	// `DTLDefaultDuelRevealBlocks` defines the constant value used by this package.
	DTLDefaultDuelRevealBlocks uint64 = 20
	// `DTLDefaultLendingLTVBPS` defines the constant value used by this package.
	DTLDefaultLendingLTVBPS = 7500
	// `DTLDefaultLendingLiqBonusBPS` defines the constant value used by this package.
	DTLDefaultLendingLiqBonusBPS = 500
	// `DTLDefaultTournamentJoinBlocks` defines the constant value used by this package.
	DTLDefaultTournamentJoinBlocks uint64 = 30
	// `DTLDefaultTournamentRevealBlocks` defines the constant value used by this package.
	DTLDefaultTournamentRevealBlocks uint64 = 30
	// `DTLDefaultLogicPackReads` defines the constant value used by this package.
	DTLDefaultLogicPackReads = 16
	// `DTLDefaultLogicPackWrites` defines the constant value used by this package.
	DTLDefaultLogicPackWrites = 16
	// `DTLDefaultLogicPackTransfers` defines the constant value used by this package.
	DTLDefaultLogicPackTransfers = 4
	// `DTLDefaultLogicPackLogs` defines the constant value used by this package.
	DTLDefaultLogicPackLogs = 8
	// `DTLDefaultLogicPackMapReads` defines the constant value used by this package.
	DTLDefaultLogicPackMapReads = 16
	// `DTLDefaultLogicPackMapWrites` defines the constant value used by this package.
	DTLDefaultLogicPackMapWrites = 16
	// `DTLDefaultLogicPackCrossCalls` defines the constant value used by this package.
	DTLDefaultLogicPackCrossCalls = 8
	// `DTLDefaultLogicPackRoleOps` defines the constant value used by this package.
	DTLDefaultLogicPackRoleOps = 8
	// `DTLDefaultRouterMaxHops` defines the constant value used by this package.
	DTLDefaultRouterMaxHops = 4
	// `DTLDefaultRouterDeadlineMaxBlocks` defines the constant value used by this package.
	DTLDefaultRouterDeadlineMaxBlocks uint64 = 30
	// `DTLDefaultRouterMaxPriceImpactBPS` defines the constant value used by this package.
	DTLDefaultRouterMaxPriceImpactBPS = 3000
	// `DTLDefaultRouterQuoteMaxPaths` defines the constant value used by this package.
	DTLDefaultRouterQuoteMaxPaths = 16
	// `DTLDefaultCreateBaseFee` defines the constant value used by this package.
	DTLDefaultCreateBaseFee = 1
	// `DTLDefaultTransferBaseFee` defines the constant value used by this package.
	DTLDefaultTransferBaseFee = 1
	// `DTLDefaultMintBaseFee` defines the constant value used by this package.
	DTLDefaultMintBaseFee = 1
	// `DTLDefaultBurnBaseFee` defines the constant value used by this package.
	DTLDefaultBurnBaseFee = 1
	// `DTLDefaultPayloadFeePerKB` defines the constant value used by this package.
	DTLDefaultPayloadFeePerKB = 0
	// `DTLDefaultFeeMaxMultiplier` defines the constant value used by this package.
	DTLDefaultFeeMaxMultiplier = 10
	// `DTLDefaultOracleMinSigners` defines the constant value used by this package.
	DTLDefaultOracleMinSigners uint16 = 3
	// `DTLDefaultOracleMaxStalenessBlocks` defines the constant value used by this package.
	DTLDefaultOracleMaxStalenessBlocks uint64 = 120
	// `DTLDefaultLendingAccrualIntervalBlocks` defines the constant value used by this package.
	DTLDefaultLendingAccrualIntervalBlocks uint64 = 1
	// `DTLDefaultGameBeaconDelayBlocks` defines the constant value used by this package.
	DTLDefaultGameBeaconDelayBlocks uint64 = 8
	// `DTLDefaultGameFiSeasonLengthBlocks` defines the constant value used by this package.
	DTLDefaultGameFiSeasonLengthBlocks uint64 = 43200
	// `DTLDefaultGameFiClaimGraceBlocks` defines the constant value used by this package.
	DTLDefaultGameFiClaimGraceBlocks uint64 = 21600
	// `DTLDefaultGameFiFeeSharePoolBPS` defines the constant value used by this package.
	DTLDefaultGameFiFeeSharePoolBPS = 2500
	// `DTLDefaultGameFiFeeShareLendingBPS` defines the constant value used by this package.
	DTLDefaultGameFiFeeShareLendingBPS = 2000
	// `DTLDefaultGameFiDuelWinPoints` defines the constant value used by this package.
	DTLDefaultGameFiDuelWinPoints = 100
	// `DTLDefaultGameFiTournamentWinPoints` defines the constant value used by this package.
	DTLDefaultGameFiTournamentWinPoints = 600
	// `DTLDefaultGameFiTournamentPartPoints` defines the constant value used by this package.
	DTLDefaultGameFiTournamentPartPoints = 80
	// `DTLDefaultFarmMinStakeBlocks` defines the constant value used by this package.
	DTLDefaultFarmMinStakeBlocks uint64 = 50
	// `DTLDefaultFarmLPPointsPerBlock` defines the synchronization state protecting shared data.
	DTLDefaultFarmLPPointsPerBlock uint64 = 1
	// `DTLDefaultFarmMaxMultiplierBPS` defines the constant value used by this package.
	DTLDefaultFarmMaxMultiplierBPS = 30000
	// `DTLDefaultGameFiMaxRewardPerSeason` defines the constant value used by this package.
	DTLDefaultGameFiMaxRewardPerSeason uint64 = 1000000
	// `DTLDefaultLeaderboardLimit` defines the constant value used by this package.
	DTLDefaultLeaderboardLimit = 100
	// `DTLMaxLeaderboardLimit` defines the constant value used by this package.
	DTLMaxLeaderboardLimit = 1000
	// `DTLGovernanceCertV2ActivationEpoch` requires replay-bound certs after genesis.
	DTLGovernanceCertV2ActivationEpoch uint64 = 1
	// `DTLGovernanceCertMaxNonceLen` caps replay nonce storage and signing bytes.
	DTLGovernanceCertMaxNonceLen = 128
)

const (
	// `DTLContractStandardCustom` defines the constant value used by this package.
	DTLContractStandardCustom = "CUSTOM"
	// `DTLContractStandardMSC20` defines the constant value used by this package.
	DTLContractStandardMSC20 = "MSC20"
	// `DTLContractStandardMSC721` defines the constant value used by this package.
	DTLContractStandardMSC721 = "MSC721"
	// `DTLContractStandardMSC1155` defines the constant value used by this package.
	DTLContractStandardMSC1155 = "MSC1155"
)

type DTLTxType string

const (
	// `DTLTxTokenCreate` defines the constant value used by this package.
	DTLTxTokenCreate DTLTxType = "TOKEN_CREATE"
	// `DTLTxTokenTransfer` defines the constant value used by this package.
	DTLTxTokenTransfer DTLTxType = "TOKEN_TRANSFER"
	// `DTLTxTokenApprove` defines the constant value used by this package.
	DTLTxTokenApprove DTLTxType = "TOKEN_APPROVE"
	// `DTLTxTokenTransferFrom` defines the constant value used by this package.
	DTLTxTokenTransferFrom DTLTxType = "TOKEN_TRANSFER_FROM"
	// `DTLTxTokenMint` defines the constant value used by this package.
	DTLTxTokenMint DTLTxType = "TOKEN_MINT"
	// `DTLTxTokenBurn` defines the constant value used by this package.
	DTLTxTokenBurn DTLTxType = "TOKEN_BURN"
	// `DTLTxNFT721Create` defines the constant value used by this package.
	DTLTxNFT721Create DTLTxType = "NFT721_CREATE"
	// `DTLTxNFT721Mint` defines the constant value used by this package.
	DTLTxNFT721Mint DTLTxType = "NFT721_MINT"
	// `DTLTxNFT721Transfer` defines the constant value used by this package.
	DTLTxNFT721Transfer DTLTxType = "NFT721_TRANSFER"
	// `DTLTxNFT1155Create` defines the constant value used by this package.
	DTLTxNFT1155Create DTLTxType = "NFT1155_CREATE"
	// `DTLTxNFT1155Mint` defines the constant value used by this package.
	DTLTxNFT1155Mint DTLTxType = "NFT1155_MINT"
	// `DTLTxNFT1155Transfer` defines the constant value used by this package.
	DTLTxNFT1155Transfer DTLTxType = "NFT1155_TRANSFER"
	// `DTLTxPoolCreate` defines the constant value used by this package.
	DTLTxPoolCreate DTLTxType = "POOL_CREATE"
	// `DTLTxPoolAdd` defines the constant value used by this package.
	DTLTxPoolAdd DTLTxType = "POOL_ADD_LIQUIDITY"
	// `DTLTxPoolRemove` defines the constant value used by this package.
	DTLTxPoolRemove DTLTxType = "POOL_REMOVE_LIQUIDITY"
	// `DTLTxPoolSwap` defines the constant value used by this package.
	DTLTxPoolSwap DTLTxType = "POOL_SWAP"
	// `DTLTxPoolSwapRoute` defines the constant value used by this package.
	DTLTxPoolSwapRoute DTLTxType = "POOL_SWAP_ROUTE"
	// `DTLTxDuelCreate` defines the constant value used by this package.
	DTLTxDuelCreate DTLTxType = "DUEL_CREATE"
	// `DTLTxDuelJoin` defines the constant value used by this package.
	DTLTxDuelJoin DTLTxType = "DUEL_JOIN"
	// `DTLTxDuelReveal` defines the constant value used by this package.
	DTLTxDuelReveal DTLTxType = "DUEL_REVEAL"
	// `DTLTxDuelFinalize` defines the constant value used by this package.
	DTLTxDuelFinalize DTLTxType = "DUEL_FINALIZE"
	// `DTLTxLendMarketCreate` defines the constant value used by this package.
	DTLTxLendMarketCreate DTLTxType = "LEND_MARKET_CREATE"
	// `DTLTxLendDeposit` defines the constant value used by this package.
	DTLTxLendDeposit DTLTxType = "LEND_DEPOSIT_COLLATERAL"
	// `DTLTxLendBorrow` defines the constant value used by this package.
	DTLTxLendBorrow DTLTxType = "LEND_BORROW"
	// `DTLTxLendRepay` defines the constant value used by this package.
	DTLTxLendRepay DTLTxType = "LEND_REPAY"
	// `DTLTxLendWithdraw` defines the constant value used by this package.
	DTLTxLendWithdraw DTLTxType = "LEND_WITHDRAW_COLLATERAL"
	// `DTLTxLendLiquidate` defines the constant value used by this package.
	DTLTxLendLiquidate DTLTxType = "LEND_LIQUIDATE"
	// `DTLTxTournamentCreate` defines the constant value used by this package.
	DTLTxTournamentCreate DTLTxType = "TOURNAMENT_CREATE"
	// `DTLTxTournamentJoin` defines the constant value used by this package.
	DTLTxTournamentJoin DTLTxType = "TOURNAMENT_JOIN"
	// `DTLTxTournamentReveal` defines the constant value used by this package.
	DTLTxTournamentReveal DTLTxType = "TOURNAMENT_REVEAL"
	// `DTLTxTournamentFinalize` defines the constant value used by this package.
	DTLTxTournamentFinalize DTLTxType = "TOURNAMENT_FINALIZE"
	// `DTLTxFarmCreate` defines the constant value used by this package.
	DTLTxFarmCreate DTLTxType = "FARM_CREATE"
	// `DTLTxFarmStakeLP` defines the constant value used by this package.
	DTLTxFarmStakeLP DTLTxType = "FARM_STAKE_LP"
	// `DTLTxFarmUnstakeLP` defines the constant value used by this package.
	DTLTxFarmUnstakeLP DTLTxType = "FARM_UNSTAKE_LP"
	// `DTLTxFarmClaim` defines the constant value used by this package.
	DTLTxFarmClaim DTLTxType = "FARM_CLAIM"
	// `DTLTxSeasonCreate` defines the constant value used by this package.
	DTLTxSeasonCreate DTLTxType = "SEASON_CREATE"
	// `DTLTxSeasonFinalize` defines the constant value used by this package.
	DTLTxSeasonFinalize DTLTxType = "SEASON_FINALIZE"
	// `DTLTxSeasonClaim` defines the constant value used by this package.
	DTLTxSeasonClaim DTLTxType = "SEASON_CLAIM"
	// `DTLTxOracleFeedCreate` defines the constant value used by this package.
	DTLTxOracleFeedCreate DTLTxType = "ORACLE_FEED_CREATE"
	// `DTLTxOraclePriceSubmit` defines the constant value used by this package.
	DTLTxOraclePriceSubmit DTLTxType = "ORACLE_PRICE_SUBMIT"
	// `DTLTxContractDeploy` defines the constant value used by this package.
	DTLTxContractDeploy DTLTxType = "CONTRACT_DEPLOY"
	// `DTLTxContractCall` defines the constant value used by this package.
	DTLTxContractCall DTLTxType = "CONTRACT_CALL"
)

type DTLContractOp string

const (
	// `DTLContractOpSetStr` defines the constant value used by this package.
	DTLContractOpSetStr DTLContractOp = "SET_STR"
	// `DTLContractOpSetU64` defines the constant value used by this package.
	DTLContractOpSetU64 DTLContractOp = "SET_U64"
	// `DTLContractOpAddU64` defines the constant value used by this package.
	DTLContractOpAddU64 DTLContractOp = "ADD_U64"
	// `DTLContractOpSubU64` defines the constant value used by this package.
	DTLContractOpSubU64 DTLContractOp = "SUB_U64"
	// `DTLContractOpTokenTransfer` defines the constant value used by this package.
	DTLContractOpTokenTransfer DTLContractOp = "TOKEN_TRANSFER"
)

type DTLGovernanceAction string

const (
	// `DTLGovMint` defines the constant value used by this package.
	DTLGovMint DTLGovernanceAction = "MINT"
	// `DTLGovPause` defines the constant value used by this package.
	DTLGovPause DTLGovernanceAction = "PAUSE"
	// `DTLGovUnpause` defines the constant value used by this package.
	DTLGovUnpause DTLGovernanceAction = "UNPAUSE"
	// `DTLGovFreezeAccount` defines the measured quantity used by this operation.
	DTLGovFreezeAccount DTLGovernanceAction = "FREEZE_ACCOUNT"
	// `DTLGovUnfreezeAccount` defines the measured quantity used by this operation.
	DTLGovUnfreezeAccount DTLGovernanceAction = "UNFREEZE_ACCOUNT"
	// `DTLGovRotateAuthority` defines the constant value used by this package.
	DTLGovRotateAuthority DTLGovernanceAction = "ROTATE_AUTHORITY"
)

type DTLTokenState struct {
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Symbol` stores the value associated with this record.
	Symbol string `json:"symbol"`
	// `Decimals` stores the value associated with this record.
	Decimals uint8 `json:"decimals"`
	// `MaxSupply` stores the value associated with this record.
	MaxSupply uint64 `json:"max_supply"`
	// `TotalSupply` stores the measured quantity used by this operation.
	TotalSupply uint64 `json:"total_supply"`
	// `Paused` stores the value associated with this record.
	Paused bool `json:"paused"`
	// `FreezeEnabled` stores whether the related condition is satisfied.
	FreezeEnabled bool `json:"freeze_enabled"`
	// `TaxBPS` stores the value associated with this record.
	TaxBPS uint16 `json:"tax_bps"`
	// `AuthoritySigners` stores the value associated with this record.
	AuthoritySigners []string `json:"authority_signers"`
	// `AuthorityThreshold` stores the value associated with this record.
	AuthorityThreshold uint16 `json:"authority_threshold"`
	// `MetadataURI` stores the current position in the related collection.
	MetadataURI string `json:"metadata_uri,omitempty"`
}

type DTLGovernanceCert struct {
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Epoch` stores the value associated with this record.
	Epoch uint64 `json:"epoch"`
	// `Nonce` binds each governance certificate to a single use.
	Nonce string `json:"nonce,omitempty"`
	// `Sequence` enforces monotonic governance ordering for a token.
	Sequence uint64 `json:"sequence,omitempty"`
	// `Expiry` is the last execution epoch at which this certificate is valid.
	Expiry uint64 `json:"expiry,omitempty"`
	// `Action` stores the value associated with this record.
	Action DTLGovernanceAction `json:"action"`
	// `ActionPayloadHash` stores the digest used to identify or verify the related data.
	ActionPayloadHash string `json:"action_payload_hash"`
	// `Signers` stores the value associated with this record.
	Signers []string `json:"signers"`
	// `SignerPublicKeys` stores the value associated with this record.
	SignerPublicKeys []string `json:"signer_public_keys,omitempty"`
	// `Signatures` stores the result produced by this operation.
	Signatures []string `json:"signatures"`
}

type DTLCreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Symbol` stores the value associated with this record.
	Symbol string `json:"symbol"`
	// `Decimals` stores the value associated with this record.
	Decimals uint8 `json:"decimals"`
	// `MaxSupply` stores the value associated with this record.
	MaxSupply uint64 `json:"max_supply"`
	// `InitialSupply` stores the current position in the related collection.
	InitialSupply uint64 `json:"initial_supply"`
	// `AuthoritySigners` stores the value associated with this record.
	AuthoritySigners []string `json:"authority_signers"`
	// `AuthorityThreshold` stores the value associated with this record.
	AuthorityThreshold uint16 `json:"authority_threshold"`
	// `FreezeEnabled` stores whether the related condition is satisfied.
	FreezeEnabled bool `json:"freeze_enabled"`
	// `TaxBPS` stores the value associated with this record.
	TaxBPS uint16 `json:"tax_bps"`
	// `MetadataURI` stores the current position in the related collection.
	MetadataURI string `json:"metadata_uri,omitempty"`
}

type DTLTransferTx struct {
	// `From` stores the value associated with this record.
	From string `json:"from"`
	// `To` stores the value associated with this record.
	To string `json:"to"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLApproveTx struct {
	// `Owner` stores the value associated with this record.
	Owner string `json:"owner"`
	// `Spender` stores the value associated with this record.
	Spender string `json:"spender"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLTransferFromTx struct {
	// `Spender` stores the value associated with this record.
	Spender string `json:"spender"`
	// `From` stores the value associated with this record.
	From string `json:"from"`
	// `To` stores the value associated with this record.
	To string `json:"to"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLMintTx struct {
	// `Proposer` stores the value associated with this record.
	Proposer string `json:"proposer"`
	// `To` stores the value associated with this record.
	To string `json:"to"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLBurnTx struct {
	// `From` stores the value associated with this record.
	From string `json:"from"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLNFT721CollectionState struct {
	// `CollectionID` stores the value associated with this record.
	CollectionID string `json:"collection_id"`
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Symbol` stores the value associated with this record.
	Symbol string `json:"symbol"`
	// `BaseURI` stores the current position in the related collection.
	BaseURI string `json:"base_uri,omitempty"`
	// `NextTokenID` stores the value associated with this record.
	NextTokenID uint64 `json:"next_token_id"`
	// `TotalMinted` stores the measured quantity used by this operation.
	TotalMinted uint64 `json:"total_minted"`
	// `Paused` stores the value associated with this record.
	Paused bool `json:"paused"`
}

type DTLNFT721CreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Symbol` stores the value associated with this record.
	Symbol string `json:"symbol"`
	// `BaseURI` stores the current position in the related collection.
	BaseURI string `json:"base_uri,omitempty"`
}

type DTLNFT721MintTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `CollectionID` stores the value associated with this record.
	CollectionID string `json:"collection_id"`
	// `To` stores the value associated with this record.
	To string `json:"to"`
	// `TokenURI` stores the current position in the related collection.
	TokenURI string `json:"token_uri,omitempty"`
}

type DTLNFT721TransferTx struct {
	// `From` stores the value associated with this record.
	From string `json:"from"`
	// `To` stores the value associated with this record.
	To string `json:"to"`
	// `CollectionID` stores the value associated with this record.
	CollectionID string `json:"collection_id"`
	// `TokenID` stores the value associated with this record.
	TokenID uint64 `json:"token_id"`
}

type DTLNFT1155CollectionState struct {
	// `CollectionID` stores the value associated with this record.
	CollectionID string `json:"collection_id"`
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Symbol` stores the value associated with this record.
	Symbol string `json:"symbol"`
	// `BaseURI` stores the current position in the related collection.
	BaseURI string `json:"base_uri,omitempty"`
	// `Paused` stores the value associated with this record.
	Paused bool `json:"paused"`
}

type DTLNFT1155CreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Symbol` stores the value associated with this record.
	Symbol string `json:"symbol"`
	// `BaseURI` stores the current position in the related collection.
	BaseURI string `json:"base_uri,omitempty"`
}

type DTLNFT1155MintTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `CollectionID` stores the value associated with this record.
	CollectionID string `json:"collection_id"`
	// `To` stores the value associated with this record.
	To string `json:"to"`
	// `TokenID` stores the value associated with this record.
	TokenID uint64 `json:"token_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLNFT1155TransferTx struct {
	// `From` stores the value associated with this record.
	From string `json:"from"`
	// `To` stores the value associated with this record.
	To string `json:"to"`
	// `CollectionID` stores the value associated with this record.
	CollectionID string `json:"collection_id"`
	// `TokenID` stores the value associated with this record.
	TokenID uint64 `json:"token_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLPoolState struct {
	// `PoolID` stores the value associated with this record.
	PoolID string `json:"pool_id"`
	// `TokenA` stores the value associated with this record.
	TokenA string `json:"token_a"`
	// `TokenB` stores the value associated with this record.
	TokenB string `json:"token_b"`
	// `ReserveA` stores the result produced by this operation.
	ReserveA uint64 `json:"reserve_a"`
	// `ReserveB` stores the result produced by this operation.
	ReserveB uint64 `json:"reserve_b"`
	// `TotalLPShares` stores the measured quantity used by this operation.
	TotalLPShares uint64 `json:"total_lp_shares"`
	// `FeeBPS` stores the value associated with this record.
	FeeBPS uint16 `json:"fee_bps"`
	// `ProtocolFeeBPS` stores the value associated with this record.
	ProtocolFeeBPS uint16 `json:"protocol_fee_bps,omitempty"`
	// `ProtocolFeeAccount` stores the measured quantity used by this operation.
	ProtocolFeeAccount string `json:"protocol_fee_account,omitempty"`
	// `PriceCumulativeA` stores the value associated with this record.
	PriceCumulativeA uint64 `json:"price_cumulative_a,omitempty"`
	// `PriceCumulativeB` stores the value associated with this record.
	PriceCumulativeB uint64 `json:"price_cumulative_b,omitempty"`
	// `LastTwapHeight` stores the value associated with this record.
	LastTwapHeight uint64 `json:"last_twap_height,omitempty"`
}

type DTLPoolCreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `TokenA` stores the value associated with this record.
	TokenA string `json:"token_a"`
	// `TokenB` stores the value associated with this record.
	TokenB string `json:"token_b"`
	// `AmountA` stores the value associated with this record.
	AmountA uint64 `json:"amount_a"`
	// `AmountB` stores the value associated with this record.
	AmountB uint64 `json:"amount_b"`
	// `FeeBPS` stores the value associated with this record.
	FeeBPS uint16 `json:"fee_bps,omitempty"`
}

type DTLPoolAddLiquidityTx struct {
	// `Provider` stores the value associated with this record.
	Provider string `json:"provider"`
	// `PoolID` stores the value associated with this record.
	PoolID string `json:"pool_id"`
	// `AmountA` stores the value associated with this record.
	AmountA uint64 `json:"amount_a"`
	// `AmountB` stores the value associated with this record.
	AmountB uint64 `json:"amount_b"`
	// `MinLPShares` stores the result produced by this operation.
	MinLPShares uint64 `json:"min_lp_shares,omitempty"`
}

type DTLPoolRemoveLiquidityTx struct {
	// `Provider` stores the value associated with this record.
	Provider string `json:"provider"`
	// `PoolID` stores the value associated with this record.
	PoolID string `json:"pool_id"`
	// `LPShares` stores the result produced by this operation.
	LPShares uint64 `json:"lp_shares"`
	// `MinAmountA` stores the value associated with this record.
	MinAmountA uint64 `json:"min_amount_a,omitempty"`
	// `MinAmountB` stores the value associated with this record.
	MinAmountB uint64 `json:"min_amount_b,omitempty"`
}

type DTLPoolSwapTx struct {
	// `Trader` stores the value associated with this record.
	Trader string `json:"trader"`
	// `PoolID` stores the value associated with this record.
	PoolID string `json:"pool_id"`
	// `TokenIn` stores the value associated with this record.
	TokenIn string `json:"token_in"`
	// `AmountIn` stores the value associated with this record.
	AmountIn uint64 `json:"amount_in"`
	// `MinAmountOut` stores the result produced by this operation.
	MinAmountOut uint64 `json:"min_amount_out,omitempty"`
}

type DTLPoolSwapRouteTx struct {
	// `Trader` stores the value associated with this record.
	Trader string `json:"trader"`
	// `TokenIn` stores the value associated with this record.
	TokenIn string `json:"token_in"`
	// `AmountIn` stores the value associated with this record.
	AmountIn uint64 `json:"amount_in"`
	// `MinAmountOut` stores the result produced by this operation.
	MinAmountOut uint64 `json:"min_amount_out,omitempty"`
	// `Path` stores the value associated with this record.
	Path []string `json:"path"`
	// `DeadlineHeight` stores the value associated with this record.
	DeadlineHeight uint64 `json:"deadline_height"`
}

type DTLDuelState struct {
	// `DuelID` stores the value associated with this record.
	DuelID string `json:"duel_id"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Stake` stores the value associated with this record.
	Stake uint64 `json:"stake"`
	// `PlayerA` stores the value associated with this record.
	PlayerA string `json:"player_a"`
	// `PlayerB` stores the value associated with this record.
	PlayerB string `json:"player_b,omitempty"`
	// `CommitA` stores the value associated with this record.
	CommitA string `json:"commit_a"`
	// `CommitB` stores the value associated with this record.
	CommitB string `json:"commit_b,omitempty"`
	// `RevealA` stores the value associated with this record.
	RevealA string `json:"reveal_a,omitempty"`
	// `RevealB` stores the value associated with this record.
	RevealB string `json:"reveal_b,omitempty"`
	// `JoinDeadline` stores the current position in the related collection.
	JoinDeadline uint64 `json:"join_deadline"`
	// `RevealDeadline` stores the value associated with this record.
	RevealDeadline uint64 `json:"reveal_deadline"`
	// `Settled` stores the value associated with this record.
	Settled bool `json:"settled"`
	// `Winner` stores the value associated with this record.
	Winner string `json:"winner,omitempty"`
	// `BeaconHeight` stores the value associated with this record.
	BeaconHeight uint64 `json:"beacon_height,omitempty"`
	// `BeaconHash` stores the digest used to identify or verify the related data.
	BeaconHash string `json:"beacon_hash,omitempty"`
	// `FinalizationSeed` stores the value associated with this record.
	FinalizationSeed string `json:"finalization_seed,omitempty"`
}

type DTLDuelCreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Stake` stores the value associated with this record.
	Stake uint64 `json:"stake"`
	// `CommitHash` stores the digest used to identify or verify the related data.
	CommitHash string `json:"commit_hash"`
	// `JoinExpiryBlocks` stores the current position in the related collection.
	JoinExpiryBlocks uint64 `json:"join_expiry_blocks,omitempty"`
	// `RevealExpiryBlocks` stores the value associated with this record.
	RevealExpiryBlocks uint64 `json:"reveal_expiry_blocks,omitempty"`
}

type DTLDuelJoinTx struct {
	// `Joiner` stores the current position in the related collection.
	Joiner string `json:"joiner"`
	// `DuelID` stores the value associated with this record.
	DuelID string `json:"duel_id"`
	// `CommitHash` stores the digest used to identify or verify the related data.
	CommitHash string `json:"commit_hash"`
}

type DTLDuelRevealTx struct {
	// `Player` stores the value associated with this record.
	Player string `json:"player"`
	// `DuelID` stores the value associated with this record.
	DuelID string `json:"duel_id"`
	// `Secret` stores the value associated with this record.
	Secret string `json:"secret"`
}

type DTLDuelFinalizeTx struct {
	// `Caller` stores the value associated with this record.
	Caller string `json:"caller"`
	// `DuelID` stores the value associated with this record.
	DuelID string `json:"duel_id"`
}

type DTLLendingMarketState struct {
	// `MarketID` stores the value associated with this record.
	MarketID string `json:"market_id"`
	// `CollateralTokenID` stores the value associated with this record.
	CollateralTokenID string `json:"collateral_token_id"`
	// `DebtTokenID` stores the value associated with this record.
	DebtTokenID string `json:"debt_token_id"`
	// `CollateralFactorBPS` stores the value associated with this record.
	CollateralFactorBPS uint16 `json:"collateral_factor_bps"`
	// `LiquidationBonusBPS` stores the value associated with this record.
	LiquidationBonusBPS uint16 `json:"liquidation_bonus_bps"`
	// `TotalCollateral` stores the measured quantity used by this operation.
	TotalCollateral uint64 `json:"total_collateral"`
	// `TotalDebt` stores the measured quantity used by this operation.
	TotalDebt uint64 `json:"total_debt"`
	// `CollateralFeedID` stores the value associated with this record.
	CollateralFeedID string `json:"collateral_feed_id,omitempty"`
	// `DebtFeedID` stores the value associated with this record.
	DebtFeedID string `json:"debt_feed_id,omitempty"`
	// `ReserveFactorBPS` stores the result produced by this operation.
	ReserveFactorBPS uint16 `json:"reserve_factor_bps,omitempty"`
	// `BaseBorrowRateBPS` stores the value associated with this record.
	BaseBorrowRateBPS uint16 `json:"base_borrow_rate_bps,omitempty"`
	// `SlopeBorrowRateBPS` stores the value associated with this record.
	SlopeBorrowRateBPS uint16 `json:"slope_borrow_rate_bps,omitempty"`
	// `CloseFactorBPS` stores the value associated with this record.
	CloseFactorBPS uint16 `json:"close_factor_bps,omitempty"`
	// `BorrowIndex` stores the current position in the related collection.
	BorrowIndex uint64 `json:"borrow_index,omitempty"`
	// `LastAccrualHeight` stores the value associated with this record.
	LastAccrualHeight uint64 `json:"last_accrual_height,omitempty"`
}

type DTLLendingPositionState struct {
	// `MarketID` stores the value associated with this record.
	MarketID string `json:"market_id"`
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `Collateral` stores the value associated with this record.
	Collateral uint64 `json:"collateral"`
	// `Debt` stores the value associated with this record.
	Debt uint64 `json:"debt"`
	// `ScaledDebt` stores the value associated with this record.
	ScaledDebt uint64 `json:"scaled_debt,omitempty"`
}

type DTLLendMarketCreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `CollateralTokenID` stores the value associated with this record.
	CollateralTokenID string `json:"collateral_token_id"`
	// `DebtTokenID` stores the value associated with this record.
	DebtTokenID string `json:"debt_token_id"`
	// `DebtLiquidity` stores the value associated with this record.
	DebtLiquidity uint64 `json:"debt_liquidity"`
	// `CollateralFactorBPS` stores the value associated with this record.
	CollateralFactorBPS uint16 `json:"collateral_factor_bps,omitempty"`
	// `LiquidationBonusBPS` stores the value associated with this record.
	LiquidationBonusBPS uint16 `json:"liquidation_bonus_bps,omitempty"`
	// `CollateralFeedID` stores the value associated with this record.
	CollateralFeedID string `json:"collateral_feed_id,omitempty"`
	// `DebtFeedID` stores the value associated with this record.
	DebtFeedID string `json:"debt_feed_id,omitempty"`
	// `ReserveFactorBPS` stores the result produced by this operation.
	ReserveFactorBPS uint16 `json:"reserve_factor_bps,omitempty"`
	// `BaseBorrowRateBPS` stores the value associated with this record.
	BaseBorrowRateBPS uint16 `json:"base_borrow_rate_bps,omitempty"`
	// `SlopeBorrowRateBPS` stores the value associated with this record.
	SlopeBorrowRateBPS uint16 `json:"slope_borrow_rate_bps,omitempty"`
	// `CloseFactorBPS` stores the value associated with this record.
	CloseFactorBPS uint16 `json:"close_factor_bps,omitempty"`
}

type DTLLendDepositCollateralTx struct {
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `MarketID` stores the value associated with this record.
	MarketID string `json:"market_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLLendBorrowTx struct {
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `MarketID` stores the value associated with this record.
	MarketID string `json:"market_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLLendRepayTx struct {
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `MarketID` stores the value associated with this record.
	MarketID string `json:"market_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLLendWithdrawCollateralTx struct {
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `MarketID` stores the value associated with this record.
	MarketID string `json:"market_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLLendLiquidateTx struct {
	// `Liquidator` stores the value associated with this record.
	Liquidator string `json:"liquidator"`
	// `Borrower` stores the value associated with this record.
	Borrower string `json:"borrower"`
	// `MarketID` stores the value associated with this record.
	MarketID string `json:"market_id"`
	// `RepayAmount` stores the value associated with this record.
	RepayAmount uint64 `json:"repay_amount"`
}

type DTLTournamentState struct {
	// `TournamentID` stores the value associated with this record.
	TournamentID string `json:"tournament_id"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `EntryFee` stores the value associated with this record.
	EntryFee uint64 `json:"entry_fee"`
	// `MaxPlayers` stores the value associated with this record.
	MaxPlayers uint16 `json:"max_players"`
	// `JoinDeadline` stores the current position in the related collection.
	JoinDeadline uint64 `json:"join_deadline"`
	// `RevealDeadline` stores the value associated with this record.
	RevealDeadline uint64 `json:"reveal_deadline"`
	// `Players` stores the value associated with this record.
	Players []string `json:"players"`
	// `Commits` stores the value associated with this record.
	Commits map[string]string `json:"commits"`
	// `Reveals` stores the value associated with this record.
	Reveals map[string]string `json:"reveals"`
	// `Pot` stores the value associated with this record.
	Pot uint64 `json:"pot"`
	// `Settled` stores the value associated with this record.
	Settled bool `json:"settled"`
	// `Winner` stores the value associated with this record.
	Winner string `json:"winner,omitempty"`
	// `BeaconHeight` stores the value associated with this record.
	BeaconHeight uint64 `json:"beacon_height,omitempty"`
	// `BeaconHash` stores the digest used to identify or verify the related data.
	BeaconHash string `json:"beacon_hash,omitempty"`
	// `FinalizationSeed` stores the value associated with this record.
	FinalizationSeed string `json:"finalization_seed,omitempty"`
}

type DTLTournamentCreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id"`
	// `EntryFee` stores the value associated with this record.
	EntryFee uint64 `json:"entry_fee"`
	// `MaxPlayers` stores the value associated with this record.
	MaxPlayers uint16 `json:"max_players"`
	// `JoinExpiryBlocks` stores the current position in the related collection.
	JoinExpiryBlocks uint64 `json:"join_expiry_blocks,omitempty"`
	// `RevealExpiryBlocks` stores the value associated with this record.
	RevealExpiryBlocks uint64 `json:"reveal_expiry_blocks,omitempty"`
}

type DTLTournamentJoinTx struct {
	// `Player` stores the value associated with this record.
	Player string `json:"player"`
	// `TournamentID` stores the value associated with this record.
	TournamentID string `json:"tournament_id"`
	// `CommitHash` stores the digest used to identify or verify the related data.
	CommitHash string `json:"commit_hash"`
}

type DTLTournamentRevealTx struct {
	// `Player` stores the value associated with this record.
	Player string `json:"player"`
	// `TournamentID` stores the value associated with this record.
	TournamentID string `json:"tournament_id"`
	// `Secret` stores the value associated with this record.
	Secret string `json:"secret"`
}

type DTLTournamentFinalizeTx struct {
	// `Caller` stores the value associated with this record.
	Caller string `json:"caller"`
	// `TournamentID` stores the value associated with this record.
	TournamentID string `json:"tournament_id"`
}

type DTLFarmPoolState struct {
	// `FarmID` stores the value associated with this record.
	FarmID string `json:"farm_id"`
	// `PoolID` stores the value associated with this record.
	PoolID string `json:"pool_id"`
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `MultiplierBPS` stores the synchronization state protecting shared data.
	MultiplierBPS uint16 `json:"multiplier_bps"`
	// `CreatedHeight` stores the value associated with this record.
	CreatedHeight uint64 `json:"created_height"`
	// `LastUpdateHeight` stores the value associated with this record.
	LastUpdateHeight uint64 `json:"last_update_height,omitempty"`
	// `Active` stores the value associated with this record.
	Active bool `json:"active"`
}

type DTLFarmPositionState struct {
	// `FarmID` stores the value associated with this record.
	FarmID string `json:"farm_id"`
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `StakedLP` stores the value associated with this record.
	StakedLP uint64 `json:"staked_lp"`
	// `LastStakeHeight` stores the value associated with this record.
	LastStakeHeight uint64 `json:"last_stake_height"`
	// `LastAccrualHeight` stores the value associated with this record.
	LastAccrualHeight uint64 `json:"last_accrual_height"`
	// `AccruedPoints` stores the value associated with this record.
	AccruedPoints uint64 `json:"accrued_points"`
}

type DTLFarmCreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `FarmID` stores the value associated with this record.
	FarmID string `json:"farm_id,omitempty"`
	// `PoolID` stores the value associated with this record.
	PoolID string `json:"pool_id"`
	// `MultiplierBPS` stores the synchronization state protecting shared data.
	MultiplierBPS uint16 `json:"multiplier_bps,omitempty"`
}

type DTLFarmStakeLPTx struct {
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `FarmID` stores the value associated with this record.
	FarmID string `json:"farm_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLFarmUnstakeLPTx struct {
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `FarmID` stores the value associated with this record.
	FarmID string `json:"farm_id"`
	// `Amount` stores the value associated with this record.
	Amount uint64 `json:"amount"`
}

type DTLFarmClaimTx struct {
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `FarmID` stores the value associated with this record.
	FarmID string `json:"farm_id"`
}

type DTLSeasonState struct {
	// `SeasonID` stores the value associated with this record.
	SeasonID string `json:"season_id"`
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `RewardToken` stores the value associated with this record.
	RewardToken string `json:"reward_token"`
	// `StartHeight` stores the value associated with this record.
	StartHeight uint64 `json:"start_height"`
	// `EndHeight` stores the value associated with this record.
	EndHeight uint64 `json:"end_height"`
	// `ClaimGraceEndHeight` stores the value associated with this record.
	ClaimGraceEndHeight uint64 `json:"claim_grace_end_height"`
	// `Finalized` stores the value associated with this record.
	Finalized bool `json:"finalized"`
	// `FinalizedHeight` stores the value associated with this record.
	FinalizedHeight uint64 `json:"finalized_height,omitempty"`
	// `TotalScore` stores the measured quantity used by this operation.
	TotalScore uint64 `json:"total_score"`
	// `TotalClaimed` stores the measured quantity used by this operation.
	TotalClaimed uint64 `json:"total_claimed"`
}

type DTLSeasonCreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `SeasonID` stores the value associated with this record.
	SeasonID string `json:"season_id,omitempty"`
	// `StartHeight` stores the value associated with this record.
	StartHeight uint64 `json:"start_height,omitempty"`
}

type DTLSeasonFinalizeTx struct {
	// `Caller` stores the value associated with this record.
	Caller string `json:"caller"`
	// `SeasonID` stores the value associated with this record.
	SeasonID string `json:"season_id"`
}

type DTLSeasonClaimTx struct {
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
	// `SeasonID` stores the value associated with this record.
	SeasonID string `json:"season_id"`
}

type DTLOracleFeedState struct {
	// `FeedID` stores the value associated with this record.
	FeedID string `json:"feed_id"`
	// `BaseTokenID` stores the value associated with this record.
	BaseTokenID string `json:"base_token_id"`
	// `QuoteTokenID` stores the value associated with this record.
	QuoteTokenID string `json:"quote_token_id"`
	// `Signers` stores the value associated with this record.
	Signers []string `json:"signers"`
	// `Threshold` stores the value associated with this record.
	Threshold uint16 `json:"threshold"`
	// `Decimals` stores the value associated with this record.
	Decimals uint8 `json:"decimals"`
	// `LastMedianPrice` stores the value associated with this record.
	LastMedianPrice uint64 `json:"last_median_price,omitempty"`
	// `LastUpdateHeight` stores the value associated with this record.
	LastUpdateHeight uint64 `json:"last_update_height,omitempty"`
}

type DTLOracleSampleState struct {
	// `FeedID` stores the value associated with this record.
	FeedID string `json:"feed_id"`
	// `Signer` stores the value associated with this record.
	Signer string `json:"signer"`
	// `Price` stores the value associated with this record.
	Price uint64 `json:"price"`
	// `Height` stores the value associated with this record.
	Height uint64 `json:"height"`
}

type DTLOracleFeedCreateTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `FeedID` stores the value associated with this record.
	FeedID string `json:"feed_id,omitempty"`
	// `BaseTokenID` stores the value associated with this record.
	BaseTokenID string `json:"base_token_id"`
	// `QuoteTokenID` stores the value associated with this record.
	QuoteTokenID string `json:"quote_token_id"`
	// `Signers` stores the value associated with this record.
	Signers []string `json:"signers"`
	// `Threshold` stores the value associated with this record.
	Threshold uint16 `json:"threshold"`
	// `Decimals` stores the value associated with this record.
	Decimals uint8 `json:"decimals"`
}

type DTLOraclePriceSubmitTx struct {
	// `Submitter` stores the value associated with this record.
	Submitter string `json:"submitter"`
	// `FeedID` stores the value associated with this record.
	FeedID string `json:"feed_id"`
	// `Price` stores the value associated with this record.
	Price uint64 `json:"price"`
}

type DTLContractMethodState struct {
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Op` stores the value associated with this record.
	Op DTLContractOp `json:"op"`
	// `Key` stores the key used to access the related value.
	Key string `json:"key,omitempty"`
	// `Arg` stores the value associated with this record.
	Arg string `json:"arg,omitempty"`
	// `ToArg` stores the value associated with this record.
	ToArg string `json:"to_arg,omitempty"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id,omitempty"`
	// `From` stores the value associated with this record.
	From string `json:"from,omitempty"` // caller | contract
}

type DTLLogicPackArg struct {
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Type` stores the value associated with this record.
	Type string `json:"type"`
}

type DTLLogicPackABIMethod struct {
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Args` stores the value associated with this record.
	Args []DTLLogicPackArg `json:"args,omitempty"`
	// `Returns` stores the value associated with this record.
	Returns []string `json:"returns,omitempty"`
}

type DTLLogicPackStorageField struct {
	// `Key` stores the key used to access the related value.
	Key string `json:"key"`
	// `Type` stores the value associated with this record.
	Type string `json:"type"`
	// `Init` stores the current position in the related collection.
	Init string `json:"init,omitempty"`
}

type DTLLogicPackLimits struct {
	// `MaxReads` stores the value associated with this record.
	MaxReads uint16 `json:"max_reads"`
	// `MaxWrites` stores the value associated with this record.
	MaxWrites uint16 `json:"max_writes"`
	// `MaxTokenTransfers` stores the value associated with this record.
	MaxTokenTransfers uint16 `json:"max_token_transfers"`
	// `MaxLogs` stores the value associated with this record.
	MaxLogs uint16 `json:"max_logs,omitempty"`
	// `MaxMapReads` stores the value associated with this record.
	MaxMapReads uint16 `json:"max_map_reads,omitempty"`
	// `MaxMapWrites` stores the value associated with this record.
	MaxMapWrites uint16 `json:"max_map_writes,omitempty"`
	// `MaxCrossCalls` stores the value associated with this record.
	MaxCrossCalls uint16 `json:"max_cross_calls,omitempty"`
	// `MaxRoleOps` stores the value associated with this record.
	MaxRoleOps uint16 `json:"max_role_ops,omitempty"`
}

type DTLLogicPackOp struct {
	// `Op` stores the value associated with this record.
	Op string `json:"op"`
	// `Dest` stores the value associated with this record.
	Dest string `json:"dest,omitempty"`
	// `A` stores the value associated with this record.
	A string `json:"a,omitempty"`
	// `B` stores the value associated with this record.
	B string `json:"b,omitempty"`
	// `Src` stores the value associated with this record.
	Src string `json:"src,omitempty"`
	// `Cond` stores the value associated with this record.
	Cond string `json:"cond,omitempty"`
	// `Key` stores the key used to access the related value.
	Key string `json:"key,omitempty"`
	// `Arg` stores the value associated with this record.
	Arg string `json:"arg,omitempty"`
	// `TokenID` stores the value associated with this record.
	TokenID string `json:"token_id,omitempty"`
	// `TokenArg` stores the value associated with this record.
	TokenArg string `json:"token_arg,omitempty"`
	// `ToArg` stores the value associated with this record.
	ToArg string `json:"to_arg,omitempty"`
	// `AmountArg` stores the value associated with this record.
	AmountArg string `json:"amount_arg,omitempty"`
	// `FromArg` stores the value associated with this record.
	FromArg string `json:"from_arg,omitempty"`
	// `SpenderArg` stores the value associated with this record.
	SpenderArg string `json:"spender_arg,omitempty"`
	// `NameArg` stores the value associated with this record.
	NameArg string `json:"name_arg,omitempty"`
	// `SymbolArg` stores the value associated with this record.
	SymbolArg string `json:"symbol_arg,omitempty"`
	// `DecimalsArg` stores the value associated with this record.
	DecimalsArg string `json:"decimals_arg,omitempty"`
	// `MaxSupplyArg` stores the value associated with this record.
	MaxSupplyArg string `json:"max_supply_arg,omitempty"`
	// `InitialSupplyArg` stores the current position in the related collection.
	InitialSupplyArg string `json:"initial_supply_arg,omitempty"`
	// `From` stores the value associated with this record.
	From string `json:"from,omitempty"` // caller | contract
	// `Message` stores the value associated with this record.
	Message string `json:"message,omitempty"`
	// `Target` stores the value associated with this record.
	Target int `json:"target,omitempty"`
	// `Map` stores the value associated with this record.
	Map string `json:"map,omitempty"`
	// `MapKeyArg` stores the value associated with this record.
	MapKeyArg string `json:"map_key_arg,omitempty"`
	// `Topic0Arg` stores the value associated with this record.
	Topic0Arg string `json:"topic0_arg,omitempty"`
	// `Topic1Arg` stores the value associated with this record.
	Topic1Arg string `json:"topic1_arg,omitempty"`
	// `Topic2Arg` stores the value associated with this record.
	Topic2Arg string `json:"topic2_arg,omitempty"`
	// `Topic3Arg` stores the value associated with this record.
	Topic3Arg string `json:"topic3_arg,omitempty"`
	// `DataArg` stores the value associated with this record.
	DataArg string `json:"data_arg,omitempty"`
}

type DTLLogicPackMethod struct {
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `MaxSteps` stores the value associated with this record.
	MaxSteps uint16 `json:"max_steps"`
	// `Ops` stores the value associated with this record.
	Ops []DTLLogicPackOp `json:"ops"`
}

type DTLLogicPack struct {
	// `Version` stores the value associated with this record.
	Version uint16 `json:"version"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `ABI` stores the current position in the related collection.
	ABI []DTLLogicPackABIMethod `json:"abi"`
	// `Storage` stores the value associated with this record.
	Storage []DTLLogicPackStorageField `json:"storage,omitempty"`
	// `Methods` stores the value associated with this record.
	Methods []DTLLogicPackMethod `json:"methods"`
	// `Limits` stores the value associated with this record.
	Limits DTLLogicPackLimits `json:"limits"`
}

type DTLContractState struct {
	// `ContractID` stores the value associated with this record.
	ContractID string `json:"contract_id"`
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Lang` stores the value associated with this record.
	Lang string `json:"lang"`
	// `Version` stores the value associated with this record.
	Version uint16 `json:"version"`
	// `Methods` stores the value associated with this record.
	Methods map[string]*DTLContractMethodState `json:"methods"`
	// `Storage` stores the value associated with this record.
	Storage map[string]string `json:"storage"`
	// `LogicPack` stores the value associated with this record.
	LogicPack *DTLLogicPack `json:"logic_pack,omitempty"`
	// `LogicHash` stores the digest used to identify or verify the related data.
	LogicHash string `json:"logic_hash,omitempty"`
	// `Paused` stores the value associated with this record.
	Paused bool `json:"paused"`
	// `Standard` stores the value associated with this record.
	Standard string `json:"standard,omitempty"`
	// `ABI` stores the current position in the related collection.
	ABI json.RawMessage `json:"abi,omitempty"`
	// `MetadataURI` stores the current position in the related collection.
	MetadataURI string `json:"metadata_uri,omitempty"`
	// `Interfaces` stores the current position in the related collection.
	Interfaces []string `json:"interfaces,omitempty"`
	// `Upgradeable` stores the value associated with this record.
	Upgradeable bool `json:"upgradeable,omitempty"`
	// `ProxyTarget` stores the value associated with this record.
	ProxyTarget string `json:"proxy_target,omitempty"`
	// `Bytecode` stores the value associated with this record.
	Bytecode string `json:"bytecode,omitempty"`
	// `BytecodeFormat` stores the value associated with this record.
	BytecodeFormat string `json:"bytecode_format,omitempty"`
	// `BytecodeHash` stores the digest used to identify or verify the related data.
	BytecodeHash string `json:"bytecode_hash,omitempty"`
	// `Compiler` stores the value associated with this record.
	Compiler string `json:"compiler,omitempty"`
	// `SourceHash` stores the digest used to identify or verify the related data.
	SourceHash string `json:"source_hash,omitempty"`
	// `BytecodeVersion` stores the value associated with this record.
	BytecodeVersion uint16 `json:"bytecode_version,omitempty"`
}

type DTLContractDeployTx struct {
	// `Creator` stores the value associated with this record.
	Creator string `json:"creator"`
	// `Name` stores the value associated with this record.
	Name string `json:"name"`
	// `Lang` stores the value associated with this record.
	Lang string `json:"lang"`
	// `Version` stores the value associated with this record.
	Version uint16 `json:"version"`
	// `Methods` stores the value associated with this record.
	Methods []DTLContractMethodState `json:"methods"`
	// `Init` stores the current position in the related collection.
	Init map[string]string `json:"init,omitempty"`
	// `LogicPack` stores the value associated with this record.
	LogicPack *DTLLogicPack `json:"logic_pack,omitempty"`
	// `Standard` stores the value associated with this record.
	Standard string `json:"standard,omitempty"`
	// `ABI` stores the current position in the related collection.
	ABI json.RawMessage `json:"abi,omitempty"`
	// `MetadataURI` stores the current position in the related collection.
	MetadataURI string `json:"metadata_uri,omitempty"`
	// `Interfaces` stores the current position in the related collection.
	Interfaces []string `json:"interfaces,omitempty"`
	// `Upgradeable` stores the value associated with this record.
	Upgradeable bool `json:"upgradeable,omitempty"`
	// `ProxyTarget` stores the value associated with this record.
	ProxyTarget string `json:"proxy_target,omitempty"`
	// `Bytecode` stores the value associated with this record.
	Bytecode string `json:"bytecode,omitempty"`
	// `BytecodeFormat` stores the value associated with this record.
	BytecodeFormat string `json:"bytecode_format,omitempty"`
	// `Compiler` stores the value associated with this record.
	Compiler string `json:"compiler,omitempty"`
	// `SourceHash` stores the digest used to identify or verify the related data.
	SourceHash string `json:"source_hash,omitempty"`
}

type DTLContractCallTx struct {
	// `Caller` stores the value associated with this record.
	Caller string `json:"caller"`
	// `ContractID` stores the value associated with this record.
	ContractID string `json:"contract_id"`
	// `Method` stores the value associated with this record.
	Method string `json:"method"`
	// `Args` stores the value associated with this record.
	Args map[string]string `json:"args,omitempty"`
}

type DTLFreezeAccountPayload struct {
	// `Account` stores the measured quantity used by this operation.
	Account string `json:"account"`
}

type DTLRotateAuthorityPayload struct {
	// `AuthoritySigners` stores the value associated with this record.
	AuthoritySigners []string `json:"authority_signers"`
	// `AuthorityThreshold` stores the value associated with this record.
	AuthorityThreshold uint16 `json:"authority_threshold"`
}

type DTLState struct {
	// `Tokens` stores the value associated with this record.
	Tokens map[string]*DTLTokenState `json:"tokens"`
	// `SymbolIndex` stores the current position in the related collection.
	SymbolIndex map[string]string `json:"symbol_index"`
	// `Balances` stores the value associated with this record.
	Balances map[string]uint64 `json:"balances"`
	// `Allowances` stores the value associated with this record.
	Allowances map[string]uint64 `json:"allowances"`
	// `NFT721Collections` stores the value associated with this record.
	NFT721Collections map[string]*DTLNFT721CollectionState `json:"nft721_collections"`
	// `NFT721SymbolIndex` stores the current position in the related collection.
	NFT721SymbolIndex map[string]string `json:"nft721_symbol_index"`
	// `NFT721Owners` stores the value associated with this record.
	NFT721Owners map[string]string `json:"nft721_owners"`
	// `NFT721TokenURIs` stores the value associated with this record.
	NFT721TokenURIs map[string]string `json:"nft721_token_uris"`
	// `NFT1155Collections` stores the value associated with this record.
	NFT1155Collections map[string]*DTLNFT1155CollectionState `json:"nft1155_collections"`
	// `NFT1155SymbolIndex` stores the current position in the related collection.
	NFT1155SymbolIndex map[string]string `json:"nft1155_symbol_index"`
	// `NFT1155Balances` stores the value associated with this record.
	NFT1155Balances map[string]uint64 `json:"nft1155_balances"`
	// `NFT1155Supplies` stores the value associated with this record.
	NFT1155Supplies map[string]uint64 `json:"nft1155_supplies"`
	// `Pools` stores the value associated with this record.
	Pools map[string]*DTLPoolState `json:"pools"`
	// `PoolIndex` stores the current position in the related collection.
	PoolIndex map[string]string `json:"pool_index"`
	// `LPBalances` stores the value associated with this record.
	LPBalances map[string]uint64 `json:"lp_balances"`
	// `Duels` stores the value associated with this record.
	Duels map[string]*DTLDuelState `json:"duels"`
	// `LendingMarkets` stores the measured quantity used by this operation.
	LendingMarkets map[string]*DTLLendingMarketState `json:"lending_markets"`
	// `LendingIndex` stores the measured quantity used by this operation.
	LendingIndex map[string]string `json:"lending_index"`
	// `LendingPositions` stores the measured quantity used by this operation.
	LendingPositions map[string]*DTLLendingPositionState `json:"lending_positions"`
	// `Tournaments` stores the value associated with this record.
	Tournaments map[string]*DTLTournamentState `json:"tournaments"`
	// `FarmPools` stores the value associated with this record.
	FarmPools map[string]*DTLFarmPoolState `json:"farm_pools"`
	// `FarmPositions` stores the value associated with this record.
	FarmPositions map[string]*DTLFarmPositionState `json:"farm_positions"`
	// `Seasons` stores the value associated with this record.
	Seasons map[string]*DTLSeasonState `json:"seasons"`
	// `SeasonScores` stores the result produced by this operation.
	SeasonScores map[string]uint64 `json:"season_scores"`
	// `SeasonClaims` stores the value associated with this record.
	SeasonClaims map[string]bool `json:"season_claims"`
	// `SeasonVaults` stores the value associated with this record.
	SeasonVaults map[string]uint64 `json:"season_vaults"`
	// `OracleFeeds` stores the value associated with this record.
	OracleFeeds map[string]*DTLOracleFeedState `json:"oracle_feeds"`
	// `OracleSamples` stores the value associated with this record.
	OracleSamples map[string]map[string]DTLOracleSampleState `json:"oracle_samples"`
	// `Contracts` stores the value associated with this record.
	Contracts map[string]*DTLContractState `json:"contracts"`
	// `FrozenAccounts` stores the value associated with this record.
	FrozenAccounts map[string]map[string]bool `json:"frozen_accounts"`
	// `GovernanceReplay` stores the value associated with this record.
	GovernanceReplay map[string]uint64 `json:"governance_replay"`
	// `Events` stores the value associated with this record.
	Events []string `json:"events,omitempty"`
	// `EventLogs` stores the value associated with this record.
	EventLogs []DTLEventLog `json:"event_logs,omitempty"`
	// `canonical` stores the value associated with this record.
	canonical bool
}

type DTLEventLog struct {
	// `ContractID` stores the value associated with this record.
	ContractID string `json:"contract_id,omitempty"`
	// `Topics` stores the value associated with this record.
	Topics []string `json:"topics,omitempty"`
	// `Data` stores the value associated with this record.
	Data string `json:"data,omitempty"`
	// `BlockHeight` stores the block data handled by this operation.
	BlockHeight uint64 `json:"block_height,omitempty"`
	// `TxID` stores the transaction data handled by this operation.
	TxID string `json:"tx_id,omitempty"`
	// `TxIndex` stores the transaction data handled by this operation.
	TxIndex int `json:"tx_index,omitempty"`
	// `LogIndex` stores the current position in the related collection.
	LogIndex int `json:"log_index,omitempty"`
}

// NewDTLState creates a new dtl state.
func NewDTLState() *DTLState {
	return &DTLState{
		Tokens:             make(map[string]*DTLTokenState),
		SymbolIndex:        make(map[string]string),
		Balances:           make(map[string]uint64),
		Allowances:         make(map[string]uint64),
		NFT721Collections:  make(map[string]*DTLNFT721CollectionState),
		NFT721SymbolIndex:  make(map[string]string),
		NFT721Owners:       make(map[string]string),
		NFT721TokenURIs:    make(map[string]string),
		NFT1155Collections: make(map[string]*DTLNFT1155CollectionState),
		NFT1155SymbolIndex: make(map[string]string),
		NFT1155Balances:    make(map[string]uint64),
		NFT1155Supplies:    make(map[string]uint64),
		Pools:              make(map[string]*DTLPoolState),
		PoolIndex:          make(map[string]string),
		LPBalances:         make(map[string]uint64),
		Duels:              make(map[string]*DTLDuelState),
		LendingMarkets:     make(map[string]*DTLLendingMarketState),
		LendingIndex:       make(map[string]string),
		LendingPositions:   make(map[string]*DTLLendingPositionState),
		Tournaments:        make(map[string]*DTLTournamentState),
		FarmPools:          make(map[string]*DTLFarmPoolState),
		FarmPositions:      make(map[string]*DTLFarmPositionState),
		Seasons:            make(map[string]*DTLSeasonState),
		SeasonScores:       make(map[string]uint64),
		SeasonClaims:       make(map[string]bool),
		SeasonVaults:       make(map[string]uint64),
		OracleFeeds:        make(map[string]*DTLOracleFeedState),
		OracleSamples:      make(map[string]map[string]DTLOracleSampleState),
		Contracts:          make(map[string]*DTLContractState),
		FrozenAccounts:     make(map[string]map[string]bool),
		GovernanceReplay:   make(map[string]uint64),
		EventLogs:          make([]DTLEventLog, 0),
		canonical:          true,
	}
}

// ensure implements the ensure helper.
func (s *DTLState) ensure() {
	if s.Tokens == nil {
		s.Tokens = make(map[string]*DTLTokenState)
	}
	if s.SymbolIndex == nil {
		s.SymbolIndex = make(map[string]string)
	}
	if s.Balances == nil {
		s.Balances = make(map[string]uint64)
	}
	if s.Allowances == nil {
		s.Allowances = make(map[string]uint64)
	}
	if s.NFT721Collections == nil {
		s.NFT721Collections = make(map[string]*DTLNFT721CollectionState)
	}
	if s.NFT721SymbolIndex == nil {
		s.NFT721SymbolIndex = make(map[string]string)
	}
	if s.NFT721Owners == nil {
		s.NFT721Owners = make(map[string]string)
	}
	if s.NFT721TokenURIs == nil {
		s.NFT721TokenURIs = make(map[string]string)
	}
	if s.NFT1155Collections == nil {
		s.NFT1155Collections = make(map[string]*DTLNFT1155CollectionState)
	}
	if s.NFT1155SymbolIndex == nil {
		s.NFT1155SymbolIndex = make(map[string]string)
	}
	if s.NFT1155Balances == nil {
		s.NFT1155Balances = make(map[string]uint64)
	}
	if s.NFT1155Supplies == nil {
		s.NFT1155Supplies = make(map[string]uint64)
	}
	if s.Pools == nil {
		s.Pools = make(map[string]*DTLPoolState)
	}
	if s.PoolIndex == nil {
		s.PoolIndex = make(map[string]string)
	}
	if s.LPBalances == nil {
		s.LPBalances = make(map[string]uint64)
	}
	if s.Duels == nil {
		s.Duels = make(map[string]*DTLDuelState)
	}
	if s.LendingMarkets == nil {
		s.LendingMarkets = make(map[string]*DTLLendingMarketState)
	}
	if s.LendingIndex == nil {
		s.LendingIndex = make(map[string]string)
	}
	if s.LendingPositions == nil {
		s.LendingPositions = make(map[string]*DTLLendingPositionState)
	}
	if s.Tournaments == nil {
		s.Tournaments = make(map[string]*DTLTournamentState)
	}
	if s.FarmPools == nil {
		s.FarmPools = make(map[string]*DTLFarmPoolState)
	}
	if s.FarmPositions == nil {
		s.FarmPositions = make(map[string]*DTLFarmPositionState)
	}
	if s.Seasons == nil {
		s.Seasons = make(map[string]*DTLSeasonState)
	}
	if s.SeasonScores == nil {
		s.SeasonScores = make(map[string]uint64)
	}
	if s.SeasonClaims == nil {
		s.SeasonClaims = make(map[string]bool)
	}
	if s.SeasonVaults == nil {
		s.SeasonVaults = make(map[string]uint64)
	}
	if s.OracleFeeds == nil {
		s.OracleFeeds = make(map[string]*DTLOracleFeedState)
	}
	if s.OracleSamples == nil {
		s.OracleSamples = make(map[string]map[string]DTLOracleSampleState)
	}
	if s.Contracts == nil {
		s.Contracts = make(map[string]*DTLContractState)
	}
	if s.FrozenAccounts == nil {
		s.FrozenAccounts = make(map[string]map[string]bool)
	}
	if s.GovernanceReplay == nil {
		s.GovernanceReplay = make(map[string]uint64)
	}
	if s.EventLogs == nil {
		s.EventLogs = make([]DTLEventLog, 0)
	}
}

// normalizeDTLAccount normalizes dtl account.
func normalizeDTLAccount(account string) string {
	return strings.ToLower(strings.TrimSpace(account))
}

// normalizeDTLTokenID normalizes dtl token id.
func normalizeDTLTokenID(tokenID string) string {
	return strings.ToLower(strings.TrimSpace(tokenID))
}

// normalizeDTLCollectionID normalizes dtl collection id.
func normalizeDTLCollectionID(collectionID string) string {
	return strings.ToLower(strings.TrimSpace(collectionID))
}

// normalizeDTLSymbol normalizes dtl symbol.
func normalizeDTLSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

// resolveDTLTokenRef implements the resolve dtl token ref helper.
func resolveDTLTokenRef(state *DTLState, tokenRef string) (string, bool) {
	if state == nil {
		return "", false
	}
	// `tokenID` stores the value produced by this operation.
	tokenID := normalizeDTLTokenID(tokenRef)
	if tokenID != "" {
		// `tok` stores whether the related condition is satisfied.
		if tok := state.Tokens[tokenID]; tok != nil {
			return tokenID, true
		}
	}
	// `symbol` stores the value produced by this operation.
	symbol := normalizeDTLSymbol(tokenRef)
	if symbol == "" {
		return tokenID, false
	}
	// `mapped` stores the value produced by this operation.
	mapped := normalizeDTLTokenID(state.SymbolIndex[symbol])
	if mapped == "" {
		return tokenID, false
	}
	// `tok` stores whether the related condition is satisfied.
	if tok := state.Tokens[mapped]; tok == nil {
		return tokenID, false
	}
	return mapped, true
}

// normalizeDTLPoolID normalizes dtl pool id.
func normalizeDTLPoolID(poolID string) string {
	return strings.ToLower(strings.TrimSpace(poolID))
}

// normalizeDTLMarketID normalizes dtl market id.
func normalizeDTLMarketID(marketID string) string {
	return strings.ToLower(strings.TrimSpace(marketID))
}

// normalizeDTLTournamentID normalizes dtl tournament id.
func normalizeDTLTournamentID(tournamentID string) string {
	return strings.ToLower(strings.TrimSpace(tournamentID))
}

// normalizeDTLFarmID normalizes dtl farm id.
func normalizeDTLFarmID(farmID string) string {
	return strings.ToLower(strings.TrimSpace(farmID))
}

// normalizeDTLSeasonID normalizes dtl season id.
func normalizeDTLSeasonID(seasonID string) string {
	return strings.ToLower(strings.TrimSpace(seasonID))
}

// normalizeDTLContractID normalizes dtl contract id.
func normalizeDTLContractID(contractID string) string {
	return strings.ToLower(strings.TrimSpace(contractID))
}

// normalizeDTLContractMethodName normalizes dtl contract method name.
func normalizeDTLContractMethodName(method string) string {
	return strings.ToLower(strings.TrimSpace(method))
}

// dtlBalanceKey implements the dtl balance key helper.
func dtlBalanceKey(tokenID, account string) string {
	return normalizeDTLTokenID(tokenID) + "|" + normalizeDTLAccount(account)
}

// dtlAllowanceKey implements the dtl allowance key helper.
func dtlAllowanceKey(tokenID, owner, spender string) string {
	return normalizeDTLTokenID(tokenID) + "|" + normalizeDTLAccount(owner) + "|" + normalizeDTLAccount(spender)
}

// dtlNFT721OwnerKey implements the dtl nft721 owner key helper.
func dtlNFT721OwnerKey(collectionID string, tokenID uint64) string {
	return normalizeDTLCollectionID(collectionID) + "|" + strconv.FormatUint(tokenID, 10)
}

// dtlNFT1155BalanceKey implements the dtl nft1155 balance key helper.
func dtlNFT1155BalanceKey(collectionID string, tokenID uint64, account string) string {
	return normalizeDTLCollectionID(collectionID) + "|" + strconv.FormatUint(tokenID, 10) + "|" + normalizeDTLAccount(account)
}

// dtlNFT1155SupplyKey implements the dtl nft1155 supply key helper.
func dtlNFT1155SupplyKey(collectionID string, tokenID uint64) string {
	return normalizeDTLCollectionID(collectionID) + "|" + strconv.FormatUint(tokenID, 10)
}

// dtlLPBalanceKey implements the dtl lp balance key helper.
func dtlLPBalanceKey(poolID, account string) string {
	return normalizeDTLPoolID(poolID) + "|" + normalizeDTLAccount(account)
}

// dtlPoolPairKey implements the dtl pool pair key helper.
func dtlPoolPairKey(tokenA, tokenB string) string {
	// `a` stores the value produced by this operation.
	a := normalizeDTLTokenID(tokenA)
	// `b` stores the value produced by this operation.
	b := normalizeDTLTokenID(tokenB)
	if a <= b {
		return a + "|" + b
	}
	return b + "|" + a
}

// dtlLendingPairKey implements the dtl lending pair key helper.
func dtlLendingPairKey(collateralTokenID, debtTokenID string) string {
	return normalizeDTLTokenID(collateralTokenID) + "|" + normalizeDTLTokenID(debtTokenID)
}

// dtlLendingPositionKey implements the dtl lending position key helper.
func dtlLendingPositionKey(marketID, account string) string {
	return normalizeDTLMarketID(marketID) + "|" + normalizeDTLAccount(account)
}

// dtlFarmPositionKey implements the dtl farm position key helper.
func dtlFarmPositionKey(farmID, account string) string {
	return normalizeDTLFarmID(farmID) + "|" + normalizeDTLAccount(account)
}

// dtlSeasonAccountKey implements the dtl season account key helper.
func dtlSeasonAccountKey(seasonID, account string) string {
	return normalizeDTLSeasonID(seasonID) + "|" + normalizeDTLAccount(account)
}

// BalanceOf implements the balance of helper.
func (s *DTLState) BalanceOf(tokenID, account string) uint64 {
	s.ensure()
	return s.Balances[dtlBalanceKey(tokenID, account)]
}

// AllowanceOf implements the allowance of helper.
func (s *DTLState) AllowanceOf(tokenID, owner, spender string) uint64 {
	s.ensure()
	return s.Allowances[dtlAllowanceKey(tokenID, owner, spender)]
}

// NFT721OwnerOf implements the nft721 owner of helper.
func (s *DTLState) NFT721OwnerOf(collectionID string, tokenID uint64) string {
	s.ensure()
	return s.NFT721Owners[dtlNFT721OwnerKey(collectionID, tokenID)]
}

// NFT1155BalanceOf implements the nft1155 balance of helper.
func (s *DTLState) NFT1155BalanceOf(collectionID string, tokenID uint64, account string) uint64 {
	s.ensure()
	return s.NFT1155Balances[dtlNFT1155BalanceKey(collectionID, tokenID, account)]
}

// LPBalanceOf implements the lp balance of helper.
func (s *DTLState) LPBalanceOf(poolID, account string) uint64 {
	s.ensure()
	return s.LPBalances[dtlLPBalanceKey(poolID, account)]
}

// IsFrozen reports whether frozen is true.
func (s *DTLState) IsFrozen(tokenID, account string) bool {
	s.ensure()
	// `byToken` stores the value produced by this operation.
	byToken := s.FrozenAccounts[normalizeDTLTokenID(tokenID)]
	if byToken == nil {
		return false
	}
	return byToken[normalizeDTLAccount(account)]
}

// DTLTokenIDFromCreate implements the dtl token id from create helper.
func DTLTokenIDFromCreate(chainID string, tx DTLCreateTx, nonce uint64) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%s",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLSymbol(tx.Symbol),
		nonce,
		strings.TrimSpace(tx.Name),
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// DTLNFT721CollectionIDFromCreate implements the dtlnft721 collection id from create helper.
func DTLNFT721CollectionIDFromCreate(chainID string, tx DTLNFT721CreateTx, nonce uint64) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%s|%s",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLSymbol(tx.Symbol),
		nonce,
		strings.TrimSpace(tx.Name),
		"NFT721",
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// DTLNFT1155CollectionIDFromCreate implements the dtlnft1155 collection id from create helper.
func DTLNFT1155CollectionIDFromCreate(chainID string, tx DTLNFT1155CreateTx, nonce uint64) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%s|%s",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLSymbol(tx.Symbol),
		nonce,
		strings.TrimSpace(tx.Name),
		"NFT1155",
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// DTLPoolIDFromTokens implements the dtl pool id from tokens helper.
func DTLPoolIDFromTokens(chainID, tokenA, tokenB string) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%s",
		strings.TrimSpace(chainID),
		dtlPoolPairKey(tokenA, tokenB),
		"POOL",
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// DTLDuelIDFromCreate implements the dtl duel id from create helper.
func DTLDuelIDFromCreate(chainID string, nonce uint64, tx DTLDuelCreateTx) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%d|%s",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLTokenID(tx.TokenID),
		nonce,
		tx.Stake,
		normalizeDTLHex(tx.CommitHash),
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// DTLDuelCommitHash implements the dtl duel commit hash helper.
func DTLDuelCommitHash(secret string) string {
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

// DTLLendingMarketIDFromTokens implements the dtl lending market id from tokens helper.
func DTLLendingMarketIDFromTokens(chainID, collateralTokenID, debtTokenID string) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%s",
		strings.TrimSpace(chainID),
		dtlLendingPairKey(collateralTokenID, debtTokenID),
		"LEND",
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// DTLTournamentIDFromCreate implements the dtl tournament id from create helper.
func DTLTournamentIDFromCreate(chainID string, nonce uint64, tx DTLTournamentCreateTx) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%d|%d",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLTokenID(tx.TokenID),
		nonce,
		tx.EntryFee,
		tx.MaxPlayers,
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// DTLFarmIDFromCreate implements the dtl farm id from create helper.
func DTLFarmIDFromCreate(chainID string, nonce uint64, tx DTLFarmCreateTx) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%d",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLPoolID(tx.PoolID),
		nonce,
		tx.MultiplierBPS,
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// DTLSeasonIDFromCreate implements the dtl season id from create helper.
func DTLSeasonIDFromCreate(chainID string, nonce uint64, tx DTLSeasonCreateTx) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%d|%d",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		nonce,
		tx.StartHeight,
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// DTLContractIDFromDeploy implements the dtl contract id from deploy helper.
func DTLContractIDFromDeploy(chainID string, nonce uint64, tx DTLContractDeployTx) string {
	// `payload` stores the value produced by this operation.
	payload := fmt.Sprintf(
		"%s|%s|%s|%s|%d|%d",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		strings.TrimSpace(tx.Name),
		strings.ToLower(strings.TrimSpace(tx.Lang)),
		tx.Version,
		nonce,
	)
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// dtlPoolVaultAccount implements the dtl pool vault account helper.
func dtlPoolVaultAccount(poolID string) string {
	// `id` stores the current position in the related collection.
	id := normalizeDTLPoolID(poolID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_POOL_" + id
}

// dtlDuelVaultAccount implements the dtl duel vault account helper.
func dtlDuelVaultAccount(duelID string) string {
	// `id` stores the current position in the related collection.
	id := normalizeDTLTokenID(duelID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_DUEL_" + id
}

// dtlLendingVaultAccount implements the dtl lending vault account helper.
func dtlLendingVaultAccount(marketID string) string {
	// `id` stores the current position in the related collection.
	id := normalizeDTLMarketID(marketID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_LEND_" + id
}

// dtlTournamentVaultAccount implements the dtl tournament vault account helper.
func dtlTournamentVaultAccount(tournamentID string) string {
	// `id` stores the current position in the related collection.
	id := normalizeDTLTournamentID(tournamentID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_TOUR_" + id
}

// dtlFarmVaultAccount implements the dtl farm vault account helper.
func dtlFarmVaultAccount(farmID string) string {
	// `id` stores the current position in the related collection.
	id := normalizeDTLFarmID(farmID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_FARM_" + id
}

// dtlContractVaultAccount implements the dtl contract vault account helper.
func dtlContractVaultAccount(contractID string) string {
	// `id` stores the current position in the related collection.
	id := normalizeDTLContractID(contractID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_CON_" + id
}

// DTLPayloadHash implements the dtl payload hash helper.
func DTLPayloadHash(v any) (string, error) {
	// `b` and `err` store the error produced by this operation.
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	// `sum` stores the value produced by this operation.
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// normalizeDTLHex normalizes dtl hex.
func normalizeDTLHex(raw string) string {
	// `s` stores the value produced by this operation.
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	return strings.ToLower(s)
}

// DTLGovernanceCertSignBytes returns canonical signing bytes for governance cert signatures.
// This binds signatures to chain, token, epoch, action and payload hash.
func DTLGovernanceCertSignBytes(
	tokenID string,
	epoch uint64,
	action DTLGovernanceAction,
	actionPayloadHash string,
) []byte {
	// `b` stores the value used by this operation.
	var b strings.Builder
	b.WriteString("MSC|DTL|GCERT|")
	b.WriteString(protocolChainID())
	b.WriteString("|")
	b.WriteString(normalizeDTLTokenID(tokenID))
	b.WriteString("|")
	b.WriteString(strconv.FormatUint(epoch, 10))
	b.WriteString("|")
	b.WriteString(strings.ToUpper(strings.TrimSpace(string(action))))
	b.WriteString("|")
	b.WriteString(normalizeDTLHex(actionPayloadHash))
	return []byte(b.String())
}

// DTLGovernanceCertSignBytesV2 returns canonical bytes for nonce/sequence-bound certs.
func DTLGovernanceCertSignBytesV2(
	tokenID string,
	epoch uint64,
	nonce string,
	sequence uint64,
	expiry uint64,
	action DTLGovernanceAction,
	actionPayloadHash string,
) []byte {
	// `b` stores the value used by this operation.
	var b strings.Builder
	b.WriteString("MSC|DTL|GCERT|V2|")
	b.WriteString(protocolChainID())
	b.WriteString("|")
	b.WriteString(normalizeDTLTokenID(tokenID))
	b.WriteString("|")
	b.WriteString(strconv.FormatUint(epoch, 10))
	b.WriteString("|")
	b.WriteString(normalizeDTLGovernanceNonce(nonce))
	b.WriteString("|")
	b.WriteString(strconv.FormatUint(sequence, 10))
	b.WriteString("|")
	b.WriteString(strconv.FormatUint(expiry, 10))
	b.WriteString("|")
	b.WriteString(strings.ToUpper(strings.TrimSpace(string(action))))
	b.WriteString("|")
	b.WriteString(normalizeDTLHex(actionPayloadHash))
	return []byte(b.String())
}

// normalizeDTLGovernanceNonce normalizes governance cert nonces.
func normalizeDTLGovernanceNonce(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// dtlGovernanceCertHasV2Fields reports whether a cert uses the v2 replay envelope.
func dtlGovernanceCertHasV2Fields(cert DTLGovernanceCert) bool {
	return normalizeDTLGovernanceNonce(cert.Nonce) != "" || cert.Sequence != 0 || cert.Expiry != 0
}

// dtlGovernanceCertSigningBytes returns the canonical signing bytes for a cert.
func dtlGovernanceCertSigningBytes(cert DTLGovernanceCert) []byte {
	if dtlGovernanceCertHasV2Fields(cert) {
		return DTLGovernanceCertSignBytesV2(
			cert.TokenID,
			cert.Epoch,
			cert.Nonce,
			cert.Sequence,
			cert.Expiry,
			cert.Action,
			cert.ActionPayloadHash,
		)
	}
	return DTLGovernanceCertSignBytes(
		cert.TokenID,
		cert.Epoch,
		cert.Action,
		cert.ActionPayloadHash,
	)
}

// SignDTLGovernanceCert signs dtl governance cert.
func SignDTLGovernanceCert(priv ed25519.PrivateKey, cert DTLGovernanceCert) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", errors.New("dtl: invalid signer private key length")
	}
	// `msg` stores the value produced by this operation.
	msg := dtlGovernanceCertSigningBytes(cert)
	// `sig` stores the value produced by this operation.
	sig := ed25519.Sign(priv, msg)
	return hex.EncodeToString(sig), nil
}

var (
	// `ErrDTLInvalidState` stores the error produced by this operation.
	ErrDTLInvalidState = errors.New("dtl: invalid state")
	// `ErrDTLUnknownToken` stores the error produced by this operation.
	ErrDTLUnknownToken = errors.New("dtl: unknown token")
	// `ErrDTLInsufficientFunds` stores the error produced by this operation.
	ErrDTLInsufficientFunds = errors.New("dtl: insufficient balance")
	// `ErrDTLInsufficientAllowance` stores the error produced by this operation.
	ErrDTLInsufficientAllowance = errors.New("dtl: insufficient allowance")
	// `ErrDTLPaused` stores the error produced by this operation.
	ErrDTLPaused = errors.New("dtl: token paused")
	// `ErrDTLFrozen` stores the error produced by this operation.
	ErrDTLFrozen = errors.New("dtl: account frozen")
	// `ErrDTLReplay` stores the error produced by this operation.
	ErrDTLReplay = errors.New("dtl: governance replay rejected")
	// `ErrDTLUnknownNFTCollection` stores the error produced by this operation.
	ErrDTLUnknownNFTCollection = errors.New("dtl: unknown nft collection")
	// `ErrDTLUnknownNFTToken` stores the error produced by this operation.
	ErrDTLUnknownNFTToken = errors.New("dtl: unknown nft token")
	// `ErrDTLNotNFTTokenOwner` stores the error produced by this operation.
	ErrDTLNotNFTTokenOwner = errors.New("dtl: not nft token owner")
	// `ErrDTLUnknownFarm` stores the error produced by this operation.
	ErrDTLUnknownFarm = errors.New("dtl: unknown farm")
	// `ErrDTLUnknownSeason` stores the error produced by this operation.
	ErrDTLUnknownSeason = errors.New("dtl: unknown season")
)
