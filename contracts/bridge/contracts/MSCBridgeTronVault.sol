// SPDX-License-Identifier: MIT
pragma solidity ^0.8.24;

import {MSCBridgeVault} from "./MSCBridgeVault.sol";

/// @notice TRC20 custody vault using the TIP-712 chain domain required by TRON wallets.
/// @dev Compile this contract with the official TRON Solidity compiler before deployment.
contract MSCBridgeTronVault is MSCBridgeVault {
    bytes32 private constant TIP712_DOMAIN_TYPEHASH =
        keccak256("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)");
    bytes32 private constant TIP712_NAME_HASH = keccak256("MSCBridgeVault");
    bytes32 private constant TIP712_VERSION_HASH = keccak256("1");

    constructor(
        uint48 defaultAdminDelay,
        address initialGovernance,
        address initialGuardian,
        bytes32 mscSourceChainId,
        address[] memory initialCommittee,
        uint16 initialThreshold
    )
        MSCBridgeVault(
            defaultAdminDelay,
            initialGovernance,
            initialGuardian,
            mscSourceChainId,
            initialCommittee,
            initialThreshold
        )
    {}

    /// @notice TIP-712 uses the low 32 bits of TVM's genesis-block chain ID.
    function tip712ChainId() public view returns (uint256) {
        return block.chainid & 0xffffffff;
    }

    function tip712DomainSeparator() public view returns (bytes32) {
        return keccak256(
            abi.encode(
                TIP712_DOMAIN_TYPEHASH,
                TIP712_NAME_HASH,
                TIP712_VERSION_HASH,
                tip712ChainId(),
                address(this)
            )
        );
    }

    /// @notice Reports the actual domain used by this vault, including masked chain ID.
    function eip712Domain()
        public
        view
        override
        returns (
            bytes1 fields,
            string memory name,
            string memory version,
            uint256 chainId,
            address verifyingContract,
            bytes32 salt,
            uint256[] memory extensions
        )
    {
        return (
            hex"0f",
            "MSCBridgeVault",
            "1",
            tip712ChainId(),
            address(this),
            bytes32(0),
            new uint256[](0)
        );
    }

    function _bridgeAuthorizationDigest(bytes32 structHash)
        internal
        view
        override
        returns (bytes32)
    {
        return keccak256(abi.encodePacked("\x19\x01", tip712DomainSeparator(), structHash));
    }
}
