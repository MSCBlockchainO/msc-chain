// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {AccessControlDefaultAdminRules} from
    "@openzeppelin/contracts/access/extensions/AccessControlDefaultAdminRules.sol";
import {IERC20} from "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {SafeERC20} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import {Pausable} from "@openzeppelin/contracts/utils/Pausable.sol";
import {ReentrancyGuard} from "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import {ECDSA} from "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import {EIP712} from "@openzeppelin/contracts/utils/cryptography/EIP712.sol";

/// @notice Custodies exact-transfer ERC-20 assets for the MSC lock/mint bridge.
/// @dev Deploy behind a governance timelock or multisig. This contract is not upgradeable.
contract MSCBridgeVault is AccessControlDefaultAdminRules, Pausable, ReentrancyGuard, EIP712 {
    using SafeERC20 for IERC20;

    bytes32 public constant GUARDIAN_ROLE = keccak256("GUARDIAN_ROLE");
    bytes32 public constant WITHDRAWAL_ID_DOMAIN = keccak256("MSC_BRIDGE_WITHDRAWAL_V1");
    bytes32 public constant UNLOCK_TYPEHASH = keccak256(
        "UnlockAuthorization(bytes32 sourceChainId,bytes32 sourceTxHash,uint64 sourceLogIndex,address token,address recipient,uint256 amount,uint64 validUntil,uint64 committeeEpoch)"
    );
    uint256 public constant MAX_COMMITTEE_MEMBERS = 32;
    uint256 public constant MAX_AUTHORIZATION_LIFETIME = 7 days;

    bytes32 public immutable MSC_SOURCE_CHAIN_ID;

    struct TokenRoute {
        bool enabled;
        uint256 minAmount;
        uint256 maxAmount;
        uint256 dailyLockLimit;
        uint256 dailyUnlockLimit;
    }

    struct DailyUsage {
        uint64 day;
        uint256 amount;
    }

    struct UnlockAuthorization {
        bytes32 sourceChainId;
        bytes32 sourceTxHash;
        uint64 sourceLogIndex;
        address token;
        address recipient;
        uint256 amount;
        uint64 validUntil;
        uint64 committeeEpoch;
    }

    mapping(address token => TokenRoute route) public tokenRoutes;
    mapping(address token => uint256 amount) public trackedEscrow;
    mapping(bytes32 withdrawalId => bool consumed) public consumedWithdrawals;
    mapping(address member => uint64 epoch) public committeeEpochOf;

    mapping(address token => DailyUsage usage) private lockUsage;
    mapping(address token => DailyUsage usage) private unlockUsage;
    address[] private activeCommittee;

    uint64 public committeeEpoch;
    uint16 public committeeThreshold;

    event Locked(address indexed token, address indexed sender, bytes recipient, uint256 amount);
    event Unlocked(
        bytes32 indexed withdrawalId,
        address indexed token,
        address indexed recipient,
        uint256 amount
    );
    event TokenRouteConfigured(
        address indexed token,
        bool enabled,
        uint256 minAmount,
        uint256 maxAmount,
        uint256 dailyLockLimit,
        uint256 dailyUnlockLimit
    );
    event CommitteeRotated(uint64 indexed epoch, uint16 threshold, address[] members);
    event SurplusRescued(address indexed token, address indexed recipient, uint256 amount);

    error ZeroAddress();
    error InvalidSourceChain();
    error InvalidRecipient();
    error InvalidTokenRoute();
    error RouteDisabled();
    error AmountOutsideRouteLimits();
    error DailyLimitExceeded();
    error NonExactTokenTransfer();
    error InsufficientEscrow();
    error WithdrawalAlreadyConsumed();
    error AuthorizationExpired();
    error AuthorizationLifetimeTooLong();
    error WrongCommitteeEpoch();
    error InvalidCommittee();
    error SignersNotStrictlyIncreasing();
    error UnauthorizedCommitteeSigner();
    error SignatureThresholdNotMet();
    error NoSurplus();
    error UnauthorizedPause();

    constructor(
        uint48 defaultAdminDelay,
        address initialGovernance,
        address initialGuardian,
        bytes32 mscSourceChainId,
        address[] memory initialCommittee,
        uint16 initialThreshold
    )
        AccessControlDefaultAdminRules(defaultAdminDelay, initialGovernance)
        EIP712("MSCBridgeVault", "1")
    {
        if (initialGovernance == address(0) || initialGuardian == address(0)) revert ZeroAddress();
        if (mscSourceChainId == bytes32(0)) revert InvalidSourceChain();
        MSC_SOURCE_CHAIN_ID = mscSourceChainId;
        _grantRole(GUARDIAN_ROLE, initialGuardian);
        _setCommittee(initialCommittee, initialThreshold);
        _pause();
    }

    function setTokenRoute(address token, TokenRoute calldata route)
        external
        onlyRole(DEFAULT_ADMIN_ROLE)
        whenPaused
    {
        if (token == address(0) || token.code.length == 0) revert InvalidTokenRoute();
        if (
            route.enabled
                && (
                    route.minAmount == 0 || route.maxAmount < route.minAmount
                        || route.dailyLockLimit < route.maxAmount || route.dailyUnlockLimit < route.maxAmount
                )
        ) revert InvalidTokenRoute();

        tokenRoutes[token] = route;
        emit TokenRouteConfigured(
            token,
            route.enabled,
            route.minAmount,
            route.maxAmount,
            route.dailyLockLimit,
            route.dailyUnlockLimit
        );
    }

    function rotateCommittee(address[] calldata members, uint16 threshold)
        external
        onlyRole(DEFAULT_ADMIN_ROLE)
        whenPaused
    {
        _setCommittee(members, threshold);
    }

    function pause() external {
        if (msg.sender != defaultAdmin() && !hasRole(GUARDIAN_ROLE, msg.sender)) {
            revert UnauthorizedPause();
        }
        _pause();
    }

    function unpause() external onlyRole(DEFAULT_ADMIN_ROLE) {
        _unpause();
    }

    function lock(address token, uint256 amount, bytes calldata recipient)
        external
        nonReentrant
        whenNotPaused
    {
        TokenRoute memory route = tokenRoutes[token];
        _validateTransfer(route, amount);
        if (!_isCanonicalMSCRecipient(recipient)) revert InvalidRecipient();
        _consumeDaily(lockUsage[token], route.dailyLockLimit, amount);

        IERC20 asset = IERC20(token);
        uint256 balanceBefore = asset.balanceOf(address(this));
        asset.safeTransferFrom(msg.sender, address(this), amount);
        uint256 balanceAfter = asset.balanceOf(address(this));
        if (balanceAfter < balanceBefore || balanceAfter - balanceBefore != amount) {
            revert NonExactTokenTransfer();
        }

        trackedEscrow[token] += amount;
        emit Locked(token, msg.sender, recipient, amount);
    }

    function unlock(UnlockAuthorization calldata authorization, bytes[] calldata signatures)
        external
        nonReentrant
        whenNotPaused
        returns (bytes32 withdrawalId)
    {
        if (authorization.sourceChainId != MSC_SOURCE_CHAIN_ID) revert InvalidSourceChain();
        if (authorization.sourceTxHash == bytes32(0)) revert InvalidSourceChain();
        if (authorization.recipient == address(0) || authorization.recipient == address(this)) {
            revert InvalidRecipient();
        }
        if (authorization.committeeEpoch != committeeEpoch) revert WrongCommitteeEpoch();
        if (authorization.validUntil < block.timestamp) revert AuthorizationExpired();
        if (authorization.validUntil > block.timestamp + MAX_AUTHORIZATION_LIFETIME) {
            revert AuthorizationLifetimeTooLong();
        }

        TokenRoute memory route = tokenRoutes[authorization.token];
        _validateTransfer(route, authorization.amount);
        withdrawalId = computeWithdrawalId(
            authorization.sourceChainId,
            authorization.sourceTxHash,
            authorization.sourceLogIndex
        );
        if (consumedWithdrawals[withdrawalId]) revert WithdrawalAlreadyConsumed();

        _verifySignatures(authorization, signatures);
        _consumeDaily(unlockUsage[authorization.token], route.dailyUnlockLimit, authorization.amount);

        uint256 liability = trackedEscrow[authorization.token];
        if (liability < authorization.amount) revert InsufficientEscrow();
        IERC20 asset = IERC20(authorization.token);
        uint256 vaultBalance = asset.balanceOf(address(this));
        if (vaultBalance < authorization.amount) revert InsufficientEscrow();

        consumedWithdrawals[withdrawalId] = true;
        trackedEscrow[authorization.token] = liability - authorization.amount;

        uint256 recipientBalanceBefore = asset.balanceOf(authorization.recipient);
        asset.safeTransfer(authorization.recipient, authorization.amount);
        uint256 recipientBalanceAfter = asset.balanceOf(authorization.recipient);
        if (
            recipientBalanceAfter < recipientBalanceBefore
                || recipientBalanceAfter - recipientBalanceBefore != authorization.amount
        ) revert NonExactTokenTransfer();

        emit Unlocked(
            withdrawalId,
            authorization.token,
            authorization.recipient,
            authorization.amount
        );
    }

    function rescueSurplus(address token, address recipient, uint256 amount)
        external
        onlyRole(DEFAULT_ADMIN_ROLE)
        whenPaused
        nonReentrant
    {
        if (recipient == address(0) || recipient == address(this)) revert InvalidRecipient();
        IERC20 asset = IERC20(token);
        uint256 balance = asset.balanceOf(address(this));
        uint256 liability = trackedEscrow[token];
        if (balance <= liability || amount == 0 || amount > balance - liability) revert NoSurplus();
        asset.safeTransfer(recipient, amount);
        emit SurplusRescued(token, recipient, amount);
    }

    function computeWithdrawalId(bytes32 sourceChainId, bytes32 sourceTxHash, uint64 sourceLogIndex)
        public
        pure
        returns (bytes32)
    {
        return keccak256(
            abi.encode(WITHDRAWAL_ID_DOMAIN, sourceChainId, sourceTxHash, sourceLogIndex)
        );
    }

    function hashUnlockAuthorization(UnlockAuthorization calldata authorization)
        external
        view
        returns (bytes32)
    {
        return _bridgeAuthorizationDigest(_unlockStructHash(authorization));
    }

    function committeeMembers() external view returns (address[] memory) {
        return activeCommittee;
    }

    function currentLockUsage(address token) external view returns (uint64 day, uint256 amount) {
        DailyUsage memory usage = lockUsage[token];
        if (usage.day != _currentDay()) return (_currentDay(), 0);
        return (usage.day, usage.amount);
    }

    function currentUnlockUsage(address token) external view returns (uint64 day, uint256 amount) {
        DailyUsage memory usage = unlockUsage[token];
        if (usage.day != _currentDay()) return (_currentDay(), 0);
        return (usage.day, usage.amount);
    }

    function _setCommittee(address[] memory members, uint16 threshold) private {
        if (
            members.length == 0 || members.length > MAX_COMMITTEE_MEMBERS || threshold == 0
                || threshold > members.length
        ) revert InvalidCommittee();

        for (uint256 i = 0; i < activeCommittee.length; ++i) {
            committeeEpochOf[activeCommittee[i]] = 0;
        }
        delete activeCommittee;

        uint64 newEpoch = committeeEpoch + 1;
        for (uint256 i = 0; i < members.length; ++i) {
            address member = members[i];
            if (member == address(0) || committeeEpochOf[member] == newEpoch) {
                revert InvalidCommittee();
            }
            committeeEpochOf[member] = newEpoch;
            activeCommittee.push(member);
        }
        committeeEpoch = newEpoch;
        committeeThreshold = threshold;
        emit CommitteeRotated(newEpoch, threshold, members);
    }

    function _verifySignatures(
        UnlockAuthorization calldata authorization,
        bytes[] calldata signatures
    ) private view {
        if (signatures.length < committeeThreshold) revert SignatureThresholdNotMet();
        if (signatures.length > activeCommittee.length) revert InvalidCommittee();

        bytes32 digest = _bridgeAuthorizationDigest(_unlockStructHash(authorization));
        address previousSigner;
        for (uint256 i = 0; i < signatures.length; ++i) {
            address signer = ECDSA.recover(digest, signatures[i]);
            if (uint160(signer) <= uint160(previousSigner)) revert SignersNotStrictlyIncreasing();
            if (committeeEpochOf[signer] != authorization.committeeEpoch) {
                revert UnauthorizedCommitteeSigner();
            }
            previousSigner = signer;
        }
    }

    function _unlockStructHash(UnlockAuthorization calldata authorization)
        private
        pure
        returns (bytes32)
    {
        return keccak256(
            abi.encode(
                UNLOCK_TYPEHASH,
                authorization.sourceChainId,
                authorization.sourceTxHash,
                authorization.sourceLogIndex,
                authorization.token,
                authorization.recipient,
                authorization.amount,
                authorization.validUntil,
                authorization.committeeEpoch
            )
        );
    }

    /// @dev Chain-family vaults override this hook when their typed-data domain
    /// differs from Ethereum EIP-712. The authorization struct itself is frozen.
    function _bridgeAuthorizationDigest(bytes32 structHash)
        internal
        view
        virtual
        returns (bytes32)
    {
        return _hashTypedDataV4(structHash);
    }

    function _validateTransfer(TokenRoute memory route, uint256 amount) private pure {
        if (!route.enabled) revert RouteDisabled();
        if (amount < route.minAmount || amount > route.maxAmount) {
            revert AmountOutsideRouteLimits();
        }
    }

    function _consumeDaily(DailyUsage storage usage, uint256 limit, uint256 amount) private {
        uint64 day = _currentDay();
        if (usage.day != day) {
            usage.day = day;
            usage.amount = 0;
        }
        if (amount > limit || usage.amount > limit - amount) revert DailyLimitExceeded();
        usage.amount += amount;
    }

    function _currentDay() private view returns (uint64) {
        return uint64(block.timestamp / 1 days);
    }

    function _isCanonicalMSCRecipient(bytes calldata recipient) private pure returns (bool) {
        if (recipient.length != 45) return false;
        if (recipient[0] != 0x4d || recipient[1] != 0x53 || recipient[2] != 0x43) return false;
        for (uint256 i = 3; i < recipient.length; ++i) {
            bytes1 value = recipient[i];
            bool decimal = value >= 0x30 && value <= 0x39;
            bool lowerHex = value >= 0x61 && value <= 0x66;
            bool upperHex = value >= 0x41 && value <= 0x46;
            if (!decimal && !lowerHex && !upperHex) return false;
        }
        return true;
    }
}
