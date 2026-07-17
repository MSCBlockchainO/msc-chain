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
	DTLMaxNameLen             = 64
	DTLMaxSymbolLen           = 16
	DTLMaxDecimals            = 18
	DTLMaxTaxBPS              = 10000
	DTLMaxPoolFeeBPS          = 1000
	DTLMaxLTVBPS              = 9500
	DTLMaxLiqBonusBPS         = 2000
	DTLMaxTournamentPlayers   = 256
	DTLMaxContractMethods     = 64
	DTLMaxContractArgs        = 64
	DTLMaxContractKeyLen      = 64
	DTLMaxContractValueLen    = 512
	DTLLogicPackVersionV1     = 1
	DTLLogicPackVersionV2     = 2
	DTLLogicPackVersionV3     = 3
	DTLLogicPackVersion       = DTLLogicPackVersionV1
	DTLMaxLogicPackStorage    = 256
	DTLMaxLogicPackOps        = 256
	DTLMaxLogicPackTotalOps   = 4096
	DTLMaxLogicPackSteps      = 4096
	DTLMaxLogicPackReads      = 1024
	DTLMaxLogicPackWrites     = 1024
	DTLMaxLogicPackTransfers  = 128
	DTLMaxLogicPackLogs       = 128
	DTLMaxLogicPackMapReads   = 1024
	DTLMaxLogicPackMapWrites  = 1024
	DTLMaxLogicPackCrossCalls = 64
	DTLMaxLogicPackRoleOps    = 64

	// Scaffold treasury sink for static tax in DTL transfer.
	DTLTreasuryAccount                       = "MSC_DTL_TREASURY"
	DTLDefaultPoolFeeBPS                     = 30
	DTLDefaultDuelJoinBlocks          uint64 = 20
	DTLDefaultDuelRevealBlocks        uint64 = 20
	DTLDefaultLendingLTVBPS                  = 7500
	DTLDefaultLendingLiqBonusBPS             = 500
	DTLDefaultTournamentJoinBlocks    uint64 = 30
	DTLDefaultTournamentRevealBlocks  uint64 = 30
	DTLDefaultLogicPackReads                 = 16
	DTLDefaultLogicPackWrites                = 16
	DTLDefaultLogicPackTransfers             = 4
	DTLDefaultLogicPackLogs                  = 8
	DTLDefaultLogicPackMapReads              = 16
	DTLDefaultLogicPackMapWrites             = 16
	DTLDefaultLogicPackCrossCalls            = 8
	DTLDefaultLogicPackRoleOps               = 8
	DTLDefaultBytecodeMaxSize         uint64 = 65536
	DTLDefaultRouterMaxHops                  = 4
	DTLDefaultRouterDeadlineMaxBlocks uint64 = 30
	DTLDefaultRouterMaxPriceImpactBPS        = 3000
	DTLDefaultRouterQuoteMaxPaths            = 16
	DTLDefaultGameFiSeasonLengthBlocks uint64 = 43200
	DTLDefaultGameFiClaimGraceBlocks   uint64 = 21600
	DTLDefaultGameFiFeeSharePoolBPS           = 2500
	DTLDefaultGameFiFeeShareLendingBPS        = 2000
	DTLDefaultGameFiDuelWinPoints             = 100
	DTLDefaultGameFiTournamentWinPoints       = 600
	DTLDefaultGameFiTournamentPartPoints      = 80
	DTLDefaultFarmMinStakeBlocks       uint64 = 50
	DTLDefaultFarmLPPointsPerBlock     uint64 = 1
	DTLDefaultFarmMaxMultiplierBPS             = 30000
	DTLDefaultGameFiMaxRewardPerSeason uint64 = 1000000
	DTLDefaultLeaderboardLimit                 = 100
	DTLMaxLeaderboardLimit                     = 1000
)

const (
	DTLContractStandardCustom  = "CUSTOM"
	DTLContractStandardMSC20   = "MSC20"
	DTLContractStandardMSC721  = "MSC721"
	DTLContractStandardMSC1155 = "MSC1155"
)

const (
	DTLContractRuntimeModeLegacyMethods = "legacy_methods"
	DTLContractRuntimeModeLogicPack     = "logic_pack"
	DTLContractRuntimeModeBytecode      = "bytecode"
)

const (
	DTLContractLangBytecodeV1 = "dtl-bytecode-v1"
	DTLBytecodeFormatV1       = "dtl-bc-v1"
	DTLBytecodeMagic          = "DTLBC1"
	DTLBytecodeVersionV1      = 1
)

type DTLTxType string

const (
	DTLTxTokenCreate        DTLTxType = "TOKEN_CREATE"
	DTLTxTokenTransfer      DTLTxType = "TOKEN_TRANSFER"
	DTLTxTokenApprove       DTLTxType = "TOKEN_APPROVE"
	DTLTxTokenTransferFrom  DTLTxType = "TOKEN_TRANSFER_FROM"
	DTLTxTokenMint          DTLTxType = "TOKEN_MINT"
	DTLTxTokenBurn          DTLTxType = "TOKEN_BURN"
	DTLTxNFT721Create       DTLTxType = "NFT721_CREATE"
	DTLTxNFT721Mint         DTLTxType = "NFT721_MINT"
	DTLTxNFT721Transfer     DTLTxType = "NFT721_TRANSFER"
	DTLTxNFT1155Create      DTLTxType = "NFT1155_CREATE"
	DTLTxNFT1155Mint        DTLTxType = "NFT1155_MINT"
	DTLTxNFT1155Transfer    DTLTxType = "NFT1155_TRANSFER"
	DTLTxPoolCreate         DTLTxType = "POOL_CREATE"
	DTLTxPoolAdd            DTLTxType = "POOL_ADD_LIQUIDITY"
	DTLTxPoolRemove         DTLTxType = "POOL_REMOVE_LIQUIDITY"
	DTLTxPoolSwap           DTLTxType = "POOL_SWAP"
	DTLTxPoolSwapRoute      DTLTxType = "POOL_SWAP_ROUTE"
	DTLTxDuelCreate         DTLTxType = "DUEL_CREATE"
	DTLTxDuelJoin           DTLTxType = "DUEL_JOIN"
	DTLTxDuelReveal         DTLTxType = "DUEL_REVEAL"
	DTLTxDuelFinalize       DTLTxType = "DUEL_FINALIZE"
	DTLTxLendMarketCreate   DTLTxType = "LEND_MARKET_CREATE"
	DTLTxLendDeposit        DTLTxType = "LEND_DEPOSIT_COLLATERAL"
	DTLTxLendBorrow         DTLTxType = "LEND_BORROW"
	DTLTxLendRepay          DTLTxType = "LEND_REPAY"
	DTLTxLendWithdraw       DTLTxType = "LEND_WITHDRAW_COLLATERAL"
	DTLTxLendLiquidate      DTLTxType = "LEND_LIQUIDATE"
	DTLTxTournamentCreate   DTLTxType = "TOURNAMENT_CREATE"
	DTLTxTournamentJoin     DTLTxType = "TOURNAMENT_JOIN"
	DTLTxTournamentReveal   DTLTxType = "TOURNAMENT_REVEAL"
	DTLTxTournamentFinalize DTLTxType = "TOURNAMENT_FINALIZE"
	DTLTxFarmCreate         DTLTxType = "FARM_CREATE"
	DTLTxFarmStakeLP        DTLTxType = "FARM_STAKE_LP"
	DTLTxFarmUnstakeLP      DTLTxType = "FARM_UNSTAKE_LP"
	DTLTxFarmClaim          DTLTxType = "FARM_CLAIM"
	DTLTxSeasonCreate       DTLTxType = "SEASON_CREATE"
	DTLTxSeasonFinalize     DTLTxType = "SEASON_FINALIZE"
	DTLTxSeasonClaim        DTLTxType = "SEASON_CLAIM"
	DTLTxOracleFeedCreate   DTLTxType = "ORACLE_FEED_CREATE"
	DTLTxOraclePriceSubmit  DTLTxType = "ORACLE_PRICE_SUBMIT"
	DTLTxContractDeploy     DTLTxType = "CONTRACT_DEPLOY"
	DTLTxContractCall       DTLTxType = "CONTRACT_CALL"
)

type DTLContractOp string

const (
	DTLContractOpSetStr        DTLContractOp = "SET_STR"
	DTLContractOpSetU64        DTLContractOp = "SET_U64"
	DTLContractOpAddU64        DTLContractOp = "ADD_U64"
	DTLContractOpSubU64        DTLContractOp = "SUB_U64"
	DTLContractOpTokenTransfer DTLContractOp = "TOKEN_TRANSFER"
)

type DTLGovernanceAction string

const (
	DTLGovMint            DTLGovernanceAction = "MINT"
	DTLGovPause           DTLGovernanceAction = "PAUSE"
	DTLGovUnpause         DTLGovernanceAction = "UNPAUSE"
	DTLGovFreezeAccount   DTLGovernanceAction = "FREEZE_ACCOUNT"
	DTLGovUnfreezeAccount DTLGovernanceAction = "UNFREEZE_ACCOUNT"
	DTLGovRotateAuthority DTLGovernanceAction = "ROTATE_AUTHORITY"
)

type DTLTokenState struct {
	TokenID            string   `json:"token_id"`
	Name               string   `json:"name"`
	Symbol             string   `json:"symbol"`
	Decimals           uint8    `json:"decimals"`
	MaxSupply          uint64   `json:"max_supply"`
	TotalSupply        uint64   `json:"total_supply"`
	Paused             bool     `json:"paused"`
	FreezeEnabled      bool     `json:"freeze_enabled"`
	TaxBPS             uint16   `json:"tax_bps"`
	AuthoritySigners   []string `json:"authority_signers"`
	AuthorityThreshold uint16   `json:"authority_threshold"`
	MetadataURI        string   `json:"metadata_uri,omitempty"`
}

type DTLGovernanceCert struct {
	TokenID           string              `json:"token_id"`
	Epoch             uint64              `json:"epoch"`
	Action            DTLGovernanceAction `json:"action"`
	ActionPayloadHash string              `json:"action_payload_hash"`
	Signers           []string            `json:"signers"`
	SignerPublicKeys  []string            `json:"signer_public_keys,omitempty"`
	Signatures        []string            `json:"signatures"`
}

type DTLCreateTx struct {
	Creator            string   `json:"creator"`
	Name               string   `json:"name"`
	Symbol             string   `json:"symbol"`
	Decimals           uint8    `json:"decimals"`
	MaxSupply          uint64   `json:"max_supply"`
	InitialSupply      uint64   `json:"initial_supply"`
	AuthoritySigners   []string `json:"authority_signers"`
	AuthorityThreshold uint16   `json:"authority_threshold"`
	FreezeEnabled      bool     `json:"freeze_enabled"`
	TaxBPS             uint16   `json:"tax_bps"`
	MetadataURI        string   `json:"metadata_uri,omitempty"`
}

type DTLTransferTx struct {
	From    string `json:"from"`
	To      string `json:"to"`
	TokenID string `json:"token_id"`
	Amount  uint64 `json:"amount"`
}

type DTLApproveTx struct {
	Owner   string `json:"owner"`
	Spender string `json:"spender"`
	TokenID string `json:"token_id"`
	Amount  uint64 `json:"amount"`
}

type DTLTransferFromTx struct {
	Spender string `json:"spender"`
	From    string `json:"from"`
	To      string `json:"to"`
	TokenID string `json:"token_id"`
	Amount  uint64 `json:"amount"`
}

type DTLMintTx struct {
	Proposer string `json:"proposer"`
	To       string `json:"to"`
	TokenID  string `json:"token_id"`
	Amount   uint64 `json:"amount"`
}

type DTLBurnTx struct {
	From    string `json:"from"`
	TokenID string `json:"token_id"`
	Amount  uint64 `json:"amount"`
}

type DTLNFT721CollectionState struct {
	CollectionID string `json:"collection_id"`
	Creator      string `json:"creator"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	BaseURI      string `json:"base_uri,omitempty"`
	NextTokenID  uint64 `json:"next_token_id"`
	TotalMinted  uint64 `json:"total_minted"`
	Paused       bool   `json:"paused"`
}

type DTLNFT721CreateTx struct {
	Creator string `json:"creator"`
	Name    string `json:"name"`
	Symbol  string `json:"symbol"`
	BaseURI string `json:"base_uri,omitempty"`
}

type DTLNFT721MintTx struct {
	Creator      string `json:"creator"`
	CollectionID string `json:"collection_id"`
	To           string `json:"to"`
	TokenURI     string `json:"token_uri,omitempty"`
}

type DTLNFT721TransferTx struct {
	From         string `json:"from"`
	To           string `json:"to"`
	CollectionID string `json:"collection_id"`
	TokenID      uint64 `json:"token_id"`
}

type DTLNFT1155CollectionState struct {
	CollectionID string `json:"collection_id"`
	Creator      string `json:"creator"`
	Name         string `json:"name"`
	Symbol       string `json:"symbol"`
	BaseURI      string `json:"base_uri,omitempty"`
	Paused       bool   `json:"paused"`
}

type DTLNFT1155CreateTx struct {
	Creator string `json:"creator"`
	Name    string `json:"name"`
	Symbol  string `json:"symbol"`
	BaseURI string `json:"base_uri,omitempty"`
}

type DTLNFT1155MintTx struct {
	Creator      string `json:"creator"`
	CollectionID string `json:"collection_id"`
	To           string `json:"to"`
	TokenID      uint64 `json:"token_id"`
	Amount       uint64 `json:"amount"`
}

type DTLNFT1155TransferTx struct {
	From         string `json:"from"`
	To           string `json:"to"`
	CollectionID string `json:"collection_id"`
	TokenID      uint64 `json:"token_id"`
	Amount       uint64 `json:"amount"`
}

type DTLPoolState struct {
	PoolID             string `json:"pool_id"`
	TokenA             string `json:"token_a"`
	TokenB             string `json:"token_b"`
	ReserveA           uint64 `json:"reserve_a"`
	ReserveB           uint64 `json:"reserve_b"`
	TotalLPShares      uint64 `json:"total_lp_shares"`
	FeeBPS             uint16 `json:"fee_bps"`
	ProtocolFeeBPS     uint16 `json:"protocol_fee_bps,omitempty"`
	ProtocolFeeAccount string `json:"protocol_fee_account,omitempty"`
	PriceCumulativeA   uint64 `json:"price_cumulative_a,omitempty"`
	PriceCumulativeB   uint64 `json:"price_cumulative_b,omitempty"`
	LastTwapHeight     uint64 `json:"last_twap_height,omitempty"`
}

type DTLPoolCreateTx struct {
	Creator string `json:"creator"`
	TokenA  string `json:"token_a"`
	TokenB  string `json:"token_b"`
	AmountA uint64 `json:"amount_a"`
	AmountB uint64 `json:"amount_b"`
	FeeBPS  uint16 `json:"fee_bps,omitempty"`
}

type DTLPoolAddLiquidityTx struct {
	Provider    string `json:"provider"`
	PoolID      string `json:"pool_id"`
	AmountA     uint64 `json:"amount_a"`
	AmountB     uint64 `json:"amount_b"`
	MinLPShares uint64 `json:"min_lp_shares,omitempty"`
}

type DTLPoolRemoveLiquidityTx struct {
	Provider   string `json:"provider"`
	PoolID     string `json:"pool_id"`
	LPShares   uint64 `json:"lp_shares"`
	MinAmountA uint64 `json:"min_amount_a,omitempty"`
	MinAmountB uint64 `json:"min_amount_b,omitempty"`
}

type DTLPoolSwapTx struct {
	Trader       string `json:"trader"`
	PoolID       string `json:"pool_id"`
	TokenIn      string `json:"token_in"`
	AmountIn     uint64 `json:"amount_in"`
	MinAmountOut uint64 `json:"min_amount_out,omitempty"`
}

type DTLPoolSwapRouteTx struct {
	Trader         string   `json:"trader"`
	TokenIn        string   `json:"token_in"`
	AmountIn       uint64   `json:"amount_in"`
	MinAmountOut   uint64   `json:"min_amount_out,omitempty"`
	Path           []string `json:"path"`
	DeadlineHeight uint64   `json:"deadline_height"`
}

type DTLDuelState struct {
	DuelID           string `json:"duel_id"`
	TokenID          string `json:"token_id"`
	Stake            uint64 `json:"stake"`
	PlayerA          string `json:"player_a"`
	PlayerB          string `json:"player_b,omitempty"`
	CommitA          string `json:"commit_a"`
	CommitB          string `json:"commit_b,omitempty"`
	RevealA          string `json:"reveal_a,omitempty"`
	RevealB          string `json:"reveal_b,omitempty"`
	JoinDeadline     uint64 `json:"join_deadline"`
	RevealDeadline   uint64 `json:"reveal_deadline"`
	Settled          bool   `json:"settled"`
	Winner           string `json:"winner,omitempty"`
	BeaconHeight     uint64 `json:"beacon_height,omitempty"`
	BeaconHash       string `json:"beacon_hash,omitempty"`
	FinalizationSeed string `json:"finalization_seed,omitempty"`
}

type DTLDuelCreateTx struct {
	Creator            string `json:"creator"`
	TokenID            string `json:"token_id"`
	Stake              uint64 `json:"stake"`
	CommitHash         string `json:"commit_hash"`
	JoinExpiryBlocks   uint64 `json:"join_expiry_blocks,omitempty"`
	RevealExpiryBlocks uint64 `json:"reveal_expiry_blocks,omitempty"`
}

type DTLDuelJoinTx struct {
	Joiner     string `json:"joiner"`
	DuelID     string `json:"duel_id"`
	CommitHash string `json:"commit_hash"`
}

type DTLDuelRevealTx struct {
	Player string `json:"player"`
	DuelID string `json:"duel_id"`
	Secret string `json:"secret"`
}

type DTLDuelFinalizeTx struct {
	Caller string `json:"caller"`
	DuelID string `json:"duel_id"`
}

type DTLLendingMarketState struct {
	MarketID            string `json:"market_id"`
	CollateralTokenID   string `json:"collateral_token_id"`
	DebtTokenID         string `json:"debt_token_id"`
	CollateralFactorBPS uint16 `json:"collateral_factor_bps"`
	LiquidationBonusBPS uint16 `json:"liquidation_bonus_bps"`
	TotalCollateral     uint64 `json:"total_collateral"`
	TotalDebt           uint64 `json:"total_debt"`
	CollateralFeedID    string `json:"collateral_feed_id,omitempty"`
	DebtFeedID          string `json:"debt_feed_id,omitempty"`
	ReserveFactorBPS    uint16 `json:"reserve_factor_bps,omitempty"`
	BaseBorrowRateBPS   uint16 `json:"base_borrow_rate_bps,omitempty"`
	SlopeBorrowRateBPS  uint16 `json:"slope_borrow_rate_bps,omitempty"`
	CloseFactorBPS      uint16 `json:"close_factor_bps,omitempty"`
	BorrowIndex         uint64 `json:"borrow_index,omitempty"`
	LastAccrualHeight   uint64 `json:"last_accrual_height,omitempty"`
}

type DTLLendingPositionState struct {
	MarketID   string `json:"market_id"`
	Account    string `json:"account"`
	Collateral uint64 `json:"collateral"`
	Debt       uint64 `json:"debt"`
	ScaledDebt uint64 `json:"scaled_debt,omitempty"`
}

type DTLLendMarketCreateTx struct {
	Creator             string `json:"creator"`
	CollateralTokenID   string `json:"collateral_token_id"`
	DebtTokenID         string `json:"debt_token_id"`
	DebtLiquidity       uint64 `json:"debt_liquidity"`
	CollateralFactorBPS uint16 `json:"collateral_factor_bps,omitempty"`
	LiquidationBonusBPS uint16 `json:"liquidation_bonus_bps,omitempty"`
	CollateralFeedID    string `json:"collateral_feed_id,omitempty"`
	DebtFeedID          string `json:"debt_feed_id,omitempty"`
	ReserveFactorBPS    uint16 `json:"reserve_factor_bps,omitempty"`
	BaseBorrowRateBPS   uint16 `json:"base_borrow_rate_bps,omitempty"`
	SlopeBorrowRateBPS  uint16 `json:"slope_borrow_rate_bps,omitempty"`
	CloseFactorBPS      uint16 `json:"close_factor_bps,omitempty"`
}

type DTLLendDepositCollateralTx struct {
	Account  string `json:"account"`
	MarketID string `json:"market_id"`
	Amount   uint64 `json:"amount"`
}

type DTLLendBorrowTx struct {
	Account  string `json:"account"`
	MarketID string `json:"market_id"`
	Amount   uint64 `json:"amount"`
}

type DTLLendRepayTx struct {
	Account  string `json:"account"`
	MarketID string `json:"market_id"`
	Amount   uint64 `json:"amount"`
}

type DTLLendWithdrawCollateralTx struct {
	Account  string `json:"account"`
	MarketID string `json:"market_id"`
	Amount   uint64 `json:"amount"`
}

type DTLLendLiquidateTx struct {
	Liquidator  string `json:"liquidator"`
	Borrower    string `json:"borrower"`
	MarketID    string `json:"market_id"`
	RepayAmount uint64 `json:"repay_amount"`
}

type DTLTournamentState struct {
	TournamentID     string            `json:"tournament_id"`
	TokenID          string            `json:"token_id"`
	Creator          string            `json:"creator"`
	EntryFee         uint64            `json:"entry_fee"`
	MaxPlayers       uint16            `json:"max_players"`
	JoinDeadline     uint64            `json:"join_deadline"`
	RevealDeadline   uint64            `json:"reveal_deadline"`
	Players          []string          `json:"players"`
	Commits          map[string]string `json:"commits"`
	Reveals          map[string]string `json:"reveals"`
	Pot              uint64            `json:"pot"`
	Settled          bool              `json:"settled"`
	Winner           string            `json:"winner,omitempty"`
	BeaconHeight     uint64            `json:"beacon_height,omitempty"`
	BeaconHash       string            `json:"beacon_hash,omitempty"`
	FinalizationSeed string            `json:"finalization_seed,omitempty"`
}

type DTLTournamentCreateTx struct {
	Creator            string `json:"creator"`
	TokenID            string `json:"token_id"`
	EntryFee           uint64 `json:"entry_fee"`
	MaxPlayers         uint16 `json:"max_players"`
	JoinExpiryBlocks   uint64 `json:"join_expiry_blocks,omitempty"`
	RevealExpiryBlocks uint64 `json:"reveal_expiry_blocks,omitempty"`
}

type DTLTournamentJoinTx struct {
	Player       string `json:"player"`
	TournamentID string `json:"tournament_id"`
	CommitHash   string `json:"commit_hash"`
}

type DTLTournamentRevealTx struct {
	Player       string `json:"player"`
	TournamentID string `json:"tournament_id"`
	Secret       string `json:"secret"`
}

type DTLTournamentFinalizeTx struct {
	Caller       string `json:"caller"`
	TournamentID string `json:"tournament_id"`
}

type DTLFarmPoolState struct {
	FarmID         string `json:"farm_id"`
	PoolID         string `json:"pool_id"`
	Creator        string `json:"creator"`
	MultiplierBPS  uint16 `json:"multiplier_bps"`
	CreatedHeight  uint64 `json:"created_height"`
	LastUpdateHeight uint64 `json:"last_update_height,omitempty"`
	Active         bool   `json:"active"`
}

type DTLFarmPositionState struct {
	FarmID            string `json:"farm_id"`
	Account           string `json:"account"`
	StakedLP          uint64 `json:"staked_lp"`
	LastStakeHeight   uint64 `json:"last_stake_height"`
	LastAccrualHeight uint64 `json:"last_accrual_height"`
	AccruedPoints     uint64 `json:"accrued_points"`
}

type DTLFarmCreateTx struct {
	Creator       string `json:"creator"`
	FarmID        string `json:"farm_id,omitempty"`
	PoolID        string `json:"pool_id"`
	MultiplierBPS uint16 `json:"multiplier_bps,omitempty"`
}

type DTLFarmStakeLPTx struct {
	Account string `json:"account"`
	FarmID  string `json:"farm_id"`
	Amount  uint64 `json:"amount"`
}

type DTLFarmUnstakeLPTx struct {
	Account string `json:"account"`
	FarmID  string `json:"farm_id"`
	Amount  uint64 `json:"amount"`
}

type DTLFarmClaimTx struct {
	Account string `json:"account"`
	FarmID  string `json:"farm_id"`
}

type DTLSeasonState struct {
	SeasonID            string `json:"season_id"`
	Creator             string `json:"creator"`
	RewardToken         string `json:"reward_token"`
	StartHeight         uint64 `json:"start_height"`
	EndHeight           uint64 `json:"end_height"`
	ClaimGraceEndHeight uint64 `json:"claim_grace_end_height"`
	Finalized           bool   `json:"finalized"`
	FinalizedHeight     uint64 `json:"finalized_height,omitempty"`
	TotalScore          uint64 `json:"total_score"`
	TotalClaimed        uint64 `json:"total_claimed"`
}

type DTLSeasonCreateTx struct {
	Creator     string `json:"creator"`
	SeasonID    string `json:"season_id,omitempty"`
	StartHeight uint64 `json:"start_height,omitempty"`
}

type DTLSeasonFinalizeTx struct {
	Caller   string `json:"caller"`
	SeasonID string `json:"season_id"`
}

type DTLSeasonClaimTx struct {
	Account  string `json:"account"`
	SeasonID string `json:"season_id"`
}

type DTLOracleFeedState struct {
	FeedID           string   `json:"feed_id"`
	BaseTokenID      string   `json:"base_token_id"`
	QuoteTokenID     string   `json:"quote_token_id"`
	Signers          []string `json:"signers"`
	Threshold        uint16   `json:"threshold"`
	Decimals         uint8    `json:"decimals"`
	LastMedianPrice  uint64   `json:"last_median_price,omitempty"`
	LastUpdateHeight uint64   `json:"last_update_height,omitempty"`
}

type DTLOracleSampleState struct {
	FeedID string `json:"feed_id"`
	Signer string `json:"signer"`
	Price  uint64 `json:"price"`
	Height uint64 `json:"height"`
}

type DTLOracleFeedCreateTx struct {
	Creator      string   `json:"creator"`
	FeedID       string   `json:"feed_id,omitempty"`
	BaseTokenID  string   `json:"base_token_id"`
	QuoteTokenID string   `json:"quote_token_id"`
	Signers      []string `json:"signers"`
	Threshold    uint16   `json:"threshold"`
	Decimals     uint8    `json:"decimals"`
}

type DTLOraclePriceSubmitTx struct {
	Submitter string `json:"submitter"`
	FeedID    string `json:"feed_id"`
	Price     uint64 `json:"price"`
}

type DTLContractMethodState struct {
	Name    string        `json:"name"`
	Op      DTLContractOp `json:"op"`
	Key     string        `json:"key,omitempty"`
	Arg     string        `json:"arg,omitempty"`
	ToArg   string        `json:"to_arg,omitempty"`
	TokenID string        `json:"token_id,omitempty"`
	From    string        `json:"from,omitempty"` // caller | contract
}

type DTLLogicPackArg struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type DTLLogicPackABIMethod struct {
	Name    string            `json:"name"`
	Args    []DTLLogicPackArg `json:"args,omitempty"`
	Returns []string          `json:"returns,omitempty"`
}

type DTLLogicPackStorageField struct {
	Key  string `json:"key"`
	Type string `json:"type"`
	Init string `json:"init,omitempty"`
}

type DTLLogicPackLimits struct {
	MaxReads          uint16 `json:"max_reads"`
	MaxWrites         uint16 `json:"max_writes"`
	MaxTokenTransfers uint16 `json:"max_token_transfers"`
	MaxLogs           uint16 `json:"max_logs,omitempty"`
	MaxMapReads       uint16 `json:"max_map_reads,omitempty"`
	MaxMapWrites      uint16 `json:"max_map_writes,omitempty"`
	MaxCrossCalls     uint16 `json:"max_cross_calls,omitempty"`
	MaxRoleOps        uint16 `json:"max_role_ops,omitempty"`
}

type DTLLogicPackOp struct {
	Op               string `json:"op"`
	Dest             string `json:"dest,omitempty"`
	A                string `json:"a,omitempty"`
	B                string `json:"b,omitempty"`
	Src              string `json:"src,omitempty"`
	Cond             string `json:"cond,omitempty"`
	Key              string `json:"key,omitempty"`
	Arg              string `json:"arg,omitempty"`
	TokenID          string `json:"token_id,omitempty"`
	TokenArg         string `json:"token_arg,omitempty"`
	ToArg            string `json:"to_arg,omitempty"`
	AmountArg        string `json:"amount_arg,omitempty"`
	FromArg          string `json:"from_arg,omitempty"`
	SpenderArg       string `json:"spender_arg,omitempty"`
	NameArg          string `json:"name_arg,omitempty"`
	SymbolArg        string `json:"symbol_arg,omitempty"`
	DecimalsArg      string `json:"decimals_arg,omitempty"`
	MaxSupplyArg     string `json:"max_supply_arg,omitempty"`
	InitialSupplyArg string `json:"initial_supply_arg,omitempty"`
	From             string `json:"from,omitempty"` // caller | contract
	Message          string `json:"message,omitempty"`
	Target           int    `json:"target,omitempty"`
	Map              string `json:"map,omitempty"`
	MapKeyArg        string `json:"map_key_arg,omitempty"`
	Topic0Arg        string `json:"topic0_arg,omitempty"`
	Topic1Arg        string `json:"topic1_arg,omitempty"`
	Topic2Arg        string `json:"topic2_arg,omitempty"`
	Topic3Arg        string `json:"topic3_arg,omitempty"`
	DataArg          string `json:"data_arg,omitempty"`
}

type DTLLogicPackMethod struct {
	Name     string           `json:"name"`
	MaxSteps uint16           `json:"max_steps"`
	Ops      []DTLLogicPackOp `json:"ops"`
}

type DTLLogicPack struct {
	Version uint16                     `json:"version"`
	Name    string                     `json:"name"`
	ABI     []DTLLogicPackABIMethod    `json:"abi"`
	Storage []DTLLogicPackStorageField `json:"storage,omitempty"`
	Methods []DTLLogicPackMethod       `json:"methods"`
	Limits  DTLLogicPackLimits         `json:"limits"`
}

type DTLBCHeader struct {
	Magic       string `json:"magic"`
	Version     uint16 `json:"version"`
	PayloadSize uint32 `json:"payload_size"`
	Checksum    uint32 `json:"checksum"`
}

type DTLBCInstr struct {
	Op               string `json:"op"`
	Dest             string `json:"dest,omitempty"`
	A                string `json:"a,omitempty"`
	B                string `json:"b,omitempty"`
	Src              string `json:"src,omitempty"`
	Cond             string `json:"cond,omitempty"`
	Key              string `json:"key,omitempty"`
	Arg              string `json:"arg,omitempty"`
	TokenID          string `json:"token_id,omitempty"`
	TokenArg         string `json:"token_arg,omitempty"`
	ToArg            string `json:"to_arg,omitempty"`
	AmountArg        string `json:"amount_arg,omitempty"`
	FromArg          string `json:"from_arg,omitempty"`
	SpenderArg       string `json:"spender_arg,omitempty"`
	NameArg          string `json:"name_arg,omitempty"`
	SymbolArg        string `json:"symbol_arg,omitempty"`
	DecimalsArg      string `json:"decimals_arg,omitempty"`
	MaxSupplyArg     string `json:"max_supply_arg,omitempty"`
	InitialSupplyArg string `json:"initial_supply_arg,omitempty"`
	From             string `json:"from,omitempty"` // caller | contract
	Message          string `json:"message,omitempty"`
	Target           int    `json:"target,omitempty"`
	Map              string `json:"map,omitempty"`
	MapKeyArg        string `json:"map_key_arg,omitempty"`
	Topic0Arg        string `json:"topic0_arg,omitempty"`
	Topic1Arg        string `json:"topic1_arg,omitempty"`
	Topic2Arg        string `json:"topic2_arg,omitempty"`
	Topic3Arg        string `json:"topic3_arg,omitempty"`
	DataArg          string `json:"data_arg,omitempty"`
}

type DTLBytecodeMethod struct {
	Name     string       `json:"name"`
	MaxSteps uint16       `json:"max_steps"`
	Code     []DTLBCInstr `json:"code"`
}

type DTLBytecodeProgram struct {
	Version uint16                     `json:"version"`
	Name    string                     `json:"name,omitempty"`
	ABI     []DTLLogicPackABIMethod    `json:"abi"`
	Storage []DTLLogicPackStorageField `json:"storage,omitempty"`
	Methods []DTLBytecodeMethod        `json:"methods"`
	Limits  DTLLogicPackLimits         `json:"limits"`
}

type DTLContractState struct {
	ContractID      string                             `json:"contract_id"`
	Creator         string                             `json:"creator"`
	Name            string                             `json:"name"`
	Lang            string                             `json:"lang"`
	Version         uint16                             `json:"version"`
	Methods         map[string]*DTLContractMethodState `json:"methods"`
	Storage         map[string]string                  `json:"storage"`
	LogicPack       *DTLLogicPack                      `json:"logic_pack,omitempty"`
	LogicHash       string                             `json:"logic_hash,omitempty"`
	Paused          bool                               `json:"paused"`
	Standard        string                             `json:"standard,omitempty"`
	ABI             json.RawMessage                    `json:"abi,omitempty"`
	MetadataURI     string                             `json:"metadata_uri,omitempty"`
	Interfaces      []string                           `json:"interfaces,omitempty"`
	Upgradeable     bool                               `json:"upgradeable,omitempty"`
	ProxyTarget     string                             `json:"proxy_target,omitempty"`
	Bytecode        string                             `json:"bytecode,omitempty"`
	BytecodeFormat  string                             `json:"bytecode_format,omitempty"`
	BytecodeHash    string                             `json:"bytecode_hash,omitempty"`
	Compiler        string                             `json:"compiler,omitempty"`
	SourceHash      string                             `json:"source_hash,omitempty"`
	BytecodeVersion uint16                             `json:"bytecode_version,omitempty"`
}

type DTLContractDeployTx struct {
	Creator        string                   `json:"creator"`
	Name           string                   `json:"name"`
	Lang           string                   `json:"lang"`
	Version        uint16                   `json:"version"`
	Methods        []DTLContractMethodState `json:"methods"`
	Init           map[string]string        `json:"init,omitempty"`
	LogicPack      *DTLLogicPack            `json:"logic_pack,omitempty"`
	Standard       string                   `json:"standard,omitempty"`
	ABI            json.RawMessage          `json:"abi,omitempty"`
	MetadataURI    string                   `json:"metadata_uri,omitempty"`
	Interfaces     []string                 `json:"interfaces,omitempty"`
	Upgradeable    bool                     `json:"upgradeable,omitempty"`
	ProxyTarget    string                   `json:"proxy_target,omitempty"`
	Bytecode       string                   `json:"bytecode,omitempty"`
	BytecodeFormat string                   `json:"bytecode_format,omitempty"`
	Compiler       string                   `json:"compiler,omitempty"`
	SourceHash     string                   `json:"source_hash,omitempty"`
}

type DTLContractCallTx struct {
	Caller     string            `json:"caller"`
	ContractID string            `json:"contract_id"`
	Method     string            `json:"method"`
	Args       map[string]string `json:"args,omitempty"`
}

type DTLFreezeAccountPayload struct {
	Account string `json:"account"`
}

type DTLRotateAuthorityPayload struct {
	AuthoritySigners   []string `json:"authority_signers"`
	AuthorityThreshold uint16   `json:"authority_threshold"`
}

type DTLState struct {
	Tokens             map[string]*DTLTokenState                  `json:"tokens"`
	SymbolIndex        map[string]string                          `json:"symbol_index"`
	Balances           map[string]uint64                          `json:"balances"`
	Allowances         map[string]uint64                          `json:"allowances"`
	NFT721Collections  map[string]*DTLNFT721CollectionState       `json:"nft721_collections"`
	NFT721SymbolIndex  map[string]string                          `json:"nft721_symbol_index"`
	NFT721Owners       map[string]string                          `json:"nft721_owners"`
	NFT721TokenURIs    map[string]string                          `json:"nft721_token_uris"`
	NFT1155Collections map[string]*DTLNFT1155CollectionState      `json:"nft1155_collections"`
	NFT1155SymbolIndex map[string]string                          `json:"nft1155_symbol_index"`
	NFT1155Balances    map[string]uint64                          `json:"nft1155_balances"`
	NFT1155Supplies    map[string]uint64                          `json:"nft1155_supplies"`
	Pools              map[string]*DTLPoolState                   `json:"pools"`
	PoolIndex          map[string]string                          `json:"pool_index"`
	LPBalances         map[string]uint64                          `json:"lp_balances"`
	Duels              map[string]*DTLDuelState                   `json:"duels"`
	LendingMarkets     map[string]*DTLLendingMarketState          `json:"lending_markets"`
	LendingIndex       map[string]string                          `json:"lending_index"`
	LendingPositions   map[string]*DTLLendingPositionState        `json:"lending_positions"`
	Tournaments        map[string]*DTLTournamentState             `json:"tournaments"`
	FarmPools          map[string]*DTLFarmPoolState               `json:"farm_pools"`
	FarmPositions      map[string]*DTLFarmPositionState           `json:"farm_positions"`
	Seasons            map[string]*DTLSeasonState                 `json:"seasons"`
	SeasonScores       map[string]uint64                          `json:"season_scores"`
	SeasonClaims       map[string]bool                            `json:"season_claims"`
	SeasonVaults       map[string]uint64                          `json:"season_vaults"`
	OracleFeeds        map[string]*DTLOracleFeedState             `json:"oracle_feeds"`
	OracleSamples      map[string]map[string]DTLOracleSampleState `json:"oracle_samples"`
	Contracts          map[string]*DTLContractState               `json:"contracts"`
	FrozenAccounts     map[string]map[string]bool                 `json:"frozen_accounts"`
	GovernanceReplay   map[string]uint64                          `json:"governance_replay"`
	Events             []string                                   `json:"events,omitempty"`
	EventLogs          []DTLEventLog                              `json:"event_logs,omitempty"`
}

type DTLEventLog struct {
	ContractID  string   `json:"contract_id,omitempty"`
	Topics      []string `json:"topics,omitempty"`
	Data        string   `json:"data,omitempty"`
	BlockHeight uint64   `json:"block_height,omitempty"`
	TxID        string   `json:"tx_id,omitempty"`
	TxIndex     int      `json:"tx_index,omitempty"`
	LogIndex    int      `json:"log_index,omitempty"`
}

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
	}
}

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

func normalizeDTLAccount(account string) string {
	return strings.ToLower(strings.TrimSpace(account))
}

func normalizeDTLTokenID(tokenID string) string {
	return strings.ToLower(strings.TrimSpace(tokenID))
}

func normalizeDTLCollectionID(collectionID string) string {
	return strings.ToLower(strings.TrimSpace(collectionID))
}

func normalizeDTLSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func resolveDTLTokenRef(state *DTLState, tokenRef string) (string, bool) {
	if state == nil {
		return "", false
	}
	tokenID := normalizeDTLTokenID(tokenRef)
	if tokenID != "" {
		if tok := state.Tokens[tokenID]; tok != nil {
			return tokenID, true
		}
	}
	symbol := normalizeDTLSymbol(tokenRef)
	if symbol == "" {
		return tokenID, false
	}
	mapped := normalizeDTLTokenID(state.SymbolIndex[symbol])
	if mapped == "" {
		return tokenID, false
	}
	if tok := state.Tokens[mapped]; tok == nil {
		return tokenID, false
	}
	return mapped, true
}

func normalizeDTLPoolID(poolID string) string {
	return strings.ToLower(strings.TrimSpace(poolID))
}

func normalizeDTLMarketID(marketID string) string {
	return strings.ToLower(strings.TrimSpace(marketID))
}

func normalizeDTLTournamentID(tournamentID string) string {
	return strings.ToLower(strings.TrimSpace(tournamentID))
}

func normalizeDTLFarmID(farmID string) string {
	return strings.ToLower(strings.TrimSpace(farmID))
}

func normalizeDTLSeasonID(seasonID string) string {
	return strings.ToLower(strings.TrimSpace(seasonID))
}

func normalizeDTLContractID(contractID string) string {
	return strings.ToLower(strings.TrimSpace(contractID))
}

func normalizeDTLContractMethodName(method string) string {
	return strings.ToLower(strings.TrimSpace(method))
}

func dtlBalanceKey(tokenID, account string) string {
	return normalizeDTLTokenID(tokenID) + "|" + normalizeDTLAccount(account)
}

func dtlAllowanceKey(tokenID, owner, spender string) string {
	return normalizeDTLTokenID(tokenID) + "|" + normalizeDTLAccount(owner) + "|" + normalizeDTLAccount(spender)
}

func dtlNFT721OwnerKey(collectionID string, tokenID uint64) string {
	return normalizeDTLCollectionID(collectionID) + "|" + strconv.FormatUint(tokenID, 10)
}

func dtlNFT1155BalanceKey(collectionID string, tokenID uint64, account string) string {
	return normalizeDTLCollectionID(collectionID) + "|" + strconv.FormatUint(tokenID, 10) + "|" + normalizeDTLAccount(account)
}

func dtlNFT1155SupplyKey(collectionID string, tokenID uint64) string {
	return normalizeDTLCollectionID(collectionID) + "|" + strconv.FormatUint(tokenID, 10)
}

func dtlLPBalanceKey(poolID, account string) string {
	return normalizeDTLPoolID(poolID) + "|" + normalizeDTLAccount(account)
}

func dtlPoolPairKey(tokenA, tokenB string) string {
	a := normalizeDTLTokenID(tokenA)
	b := normalizeDTLTokenID(tokenB)
	if a <= b {
		return a + "|" + b
	}
	return b + "|" + a
}

func dtlLendingPairKey(collateralTokenID, debtTokenID string) string {
	return normalizeDTLTokenID(collateralTokenID) + "|" + normalizeDTLTokenID(debtTokenID)
}

func dtlLendingPositionKey(marketID, account string) string {
	return normalizeDTLMarketID(marketID) + "|" + normalizeDTLAccount(account)
}

func dtlFarmPositionKey(farmID, account string) string {
	return normalizeDTLFarmID(farmID) + "|" + normalizeDTLAccount(account)
}

func dtlSeasonAccountKey(seasonID, account string) string {
	return normalizeDTLSeasonID(seasonID) + "|" + normalizeDTLAccount(account)
}

func (s *DTLState) BalanceOf(tokenID, account string) uint64 {
	s.ensure()
	return s.Balances[dtlBalanceKey(tokenID, account)]
}

func (s *DTLState) AllowanceOf(tokenID, owner, spender string) uint64 {
	s.ensure()
	return s.Allowances[dtlAllowanceKey(tokenID, owner, spender)]
}

func (s *DTLState) NFT721OwnerOf(collectionID string, tokenID uint64) string {
	s.ensure()
	return s.NFT721Owners[dtlNFT721OwnerKey(collectionID, tokenID)]
}

func (s *DTLState) NFT1155BalanceOf(collectionID string, tokenID uint64, account string) uint64 {
	s.ensure()
	return s.NFT1155Balances[dtlNFT1155BalanceKey(collectionID, tokenID, account)]
}

func (s *DTLState) LPBalanceOf(poolID, account string) uint64 {
	s.ensure()
	return s.LPBalances[dtlLPBalanceKey(poolID, account)]
}

func (s *DTLState) IsFrozen(tokenID, account string) bool {
	s.ensure()
	byToken := s.FrozenAccounts[normalizeDTLTokenID(tokenID)]
	if byToken == nil {
		return false
	}
	return byToken[normalizeDTLAccount(account)]
}

func DTLTokenIDFromCreate(chainID string, tx DTLCreateTx, nonce uint64) string {
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%s",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLSymbol(tx.Symbol),
		nonce,
		strings.TrimSpace(tx.Name),
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func DTLNFT721CollectionIDFromCreate(chainID string, tx DTLNFT721CreateTx, nonce uint64) string {
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%s|%s",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLSymbol(tx.Symbol),
		nonce,
		strings.TrimSpace(tx.Name),
		"NFT721",
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func DTLNFT1155CollectionIDFromCreate(chainID string, tx DTLNFT1155CreateTx, nonce uint64) string {
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%s|%s",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLSymbol(tx.Symbol),
		nonce,
		strings.TrimSpace(tx.Name),
		"NFT1155",
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func DTLPoolIDFromTokens(chainID, tokenA, tokenB string) string {
	payload := fmt.Sprintf(
		"%s|%s|%s",
		strings.TrimSpace(chainID),
		dtlPoolPairKey(tokenA, tokenB),
		"POOL",
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func DTLDuelIDFromCreate(chainID string, nonce uint64, tx DTLDuelCreateTx) string {
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%d|%s",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLTokenID(tx.TokenID),
		nonce,
		tx.Stake,
		normalizeDTLHex(tx.CommitHash),
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func DTLDuelCommitHash(secret string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	return hex.EncodeToString(sum[:])
}

func DTLLendingMarketIDFromTokens(chainID, collateralTokenID, debtTokenID string) string {
	payload := fmt.Sprintf(
		"%s|%s|%s",
		strings.TrimSpace(chainID),
		dtlLendingPairKey(collateralTokenID, debtTokenID),
		"LEND",
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func DTLTournamentIDFromCreate(chainID string, nonce uint64, tx DTLTournamentCreateTx) string {
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%d|%d",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLTokenID(tx.TokenID),
		nonce,
		tx.EntryFee,
		tx.MaxPlayers,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func DTLFarmIDFromCreate(chainID string, nonce uint64, tx DTLFarmCreateTx) string {
	payload := fmt.Sprintf(
		"%s|%s|%s|%d|%d",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		normalizeDTLPoolID(tx.PoolID),
		nonce,
		tx.MultiplierBPS,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func DTLSeasonIDFromCreate(chainID string, nonce uint64, tx DTLSeasonCreateTx) string {
	payload := fmt.Sprintf(
		"%s|%s|%d|%d",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		nonce,
		tx.StartHeight,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func DTLContractIDFromDeploy(chainID string, nonce uint64, tx DTLContractDeployTx) string {
	payload := fmt.Sprintf(
		"%s|%s|%s|%s|%d|%d",
		strings.TrimSpace(chainID),
		normalizeDTLAccount(tx.Creator),
		strings.TrimSpace(tx.Name),
		strings.ToLower(strings.TrimSpace(tx.Lang)),
		tx.Version,
		nonce,
	)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func resolveDTLContractRuntimeMode(contract *DTLContractState) string {
	if contract == nil {
		return DTLContractRuntimeModeLegacyMethods
	}
	if strings.TrimSpace(contract.Bytecode) != "" {
		return DTLContractRuntimeModeBytecode
	}
	if contract.LogicPack != nil {
		return DTLContractRuntimeModeLogicPack
	}
	return DTLContractRuntimeModeLegacyMethods
}

func resolveDTLDeployRuntimeMode(tx *DTLContractDeployTx) string {
	if tx == nil {
		return DTLContractRuntimeModeLegacyMethods
	}
	if strings.TrimSpace(tx.Bytecode) != "" {
		return DTLContractRuntimeModeBytecode
	}
	if tx.LogicPack != nil {
		return DTLContractRuntimeModeLogicPack
	}
	return DTLContractRuntimeModeLegacyMethods
}

func dtlPoolVaultAccount(poolID string) string {
	id := normalizeDTLPoolID(poolID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_POOL_" + id
}

func dtlDuelVaultAccount(duelID string) string {
	id := normalizeDTLTokenID(duelID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_DUEL_" + id
}

func dtlLendingVaultAccount(marketID string) string {
	id := normalizeDTLMarketID(marketID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_LEND_" + id
}

func dtlTournamentVaultAccount(tournamentID string) string {
	id := normalizeDTLTournamentID(tournamentID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_TOUR_" + id
}

func dtlFarmVaultAccount(farmID string) string {
	id := normalizeDTLFarmID(farmID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_FARM_" + id
}

func dtlContractVaultAccount(contractID string) string {
	id := normalizeDTLContractID(contractID)
	if len(id) > 24 {
		id = id[:24]
	}
	return "MSC_DTL_CON_" + id
}

func DTLPayloadHash(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeDTLHex(raw string) string {
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
	var b strings.Builder
	b.WriteString("MSC|DTL|GCERT|")
	b.WriteString(strings.TrimSpace(ChainID))
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

func SignDTLGovernanceCert(priv ed25519.PrivateKey, cert DTLGovernanceCert) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", errors.New("dtl: invalid signer private key length")
	}
	msg := DTLGovernanceCertSignBytes(
		cert.TokenID,
		cert.Epoch,
		cert.Action,
		cert.ActionPayloadHash,
	)
	sig := ed25519.Sign(priv, msg)
	return hex.EncodeToString(sig), nil
}

var (
	ErrDTLInvalidState          = errors.New("dtl: invalid state")
	ErrDTLUnknownToken          = errors.New("dtl: unknown token")
	ErrDTLInsufficientFunds     = errors.New("dtl: insufficient balance")
	ErrDTLInsufficientAllowance = errors.New("dtl: insufficient allowance")
	ErrDTLPaused                = errors.New("dtl: token paused")
	ErrDTLFrozen                = errors.New("dtl: account frozen")
	ErrDTLReplay                = errors.New("dtl: governance replay rejected")
	ErrDTLUnknownNFTCollection  = errors.New("dtl: unknown nft collection")
	ErrDTLUnknownNFTToken       = errors.New("dtl: unknown nft token")
	ErrDTLNotNFTTokenOwner      = errors.New("dtl: not nft token owner")
	ErrDTLUnknownFarm           = errors.New("dtl: unknown farm")
	ErrDTLUnknownSeason         = errors.New("dtl: unknown season")
)
