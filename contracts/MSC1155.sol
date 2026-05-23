// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

// MSC-1155: ERC-1155 style multi-token template for MSC Chain.
contract MSC1155 {
    mapping(uint256 => mapping(address => uint256)) private _balances;
    mapping(address => mapping(address => bool)) private _operatorApprovals;
    mapping(uint256 => string) private _tokenURIs;

    event TransferSingle(address indexed operator, address indexed from, address indexed to, uint256 id, uint256 value);
    event TransferBatch(address indexed operator, address indexed from, address indexed to, uint256[] ids, uint256[] values);
    event ApprovalForAll(address indexed owner, address indexed operator, bool approved);
    event URI(string value, uint256 indexed id);

    function supportsInterface(bytes4 interfaceId) external pure returns (bool) {
        return
            interfaceId == 0x01ffc9a7 || // ERC165
            interfaceId == 0xd9b67a26 || // ERC1155
            interfaceId == 0x0e89341c;   // ERC1155MetadataURI
    }

    function uri(uint256 id) external view returns (string memory) {
        return _tokenURIs[id];
    }

    function balanceOf(address account, uint256 id) public view returns (uint256) {
        require(account != address(0), "MSC1155: zero account");
        return _balances[id][account];
    }

    function balanceOfBatch(address[] calldata accounts, uint256[] calldata ids) external view returns (uint256[] memory) {
        require(accounts.length == ids.length, "MSC1155: length mismatch");
        uint256[] memory out = new uint256[](accounts.length);
        for (uint256 i = 0; i < accounts.length; i++) {
            out[i] = balanceOf(accounts[i], ids[i]);
        }
        return out;
    }

    function setApprovalForAll(address operator, bool approved) external {
        require(operator != msg.sender, "MSC1155: self approve");
        _operatorApprovals[msg.sender][operator] = approved;
        emit ApprovalForAll(msg.sender, operator, approved);
    }

    function isApprovedForAll(address account, address operator) external view returns (bool) {
        return _operatorApprovals[account][operator];
    }

    function safeTransferFrom(address from, address to, uint256 id, uint256 amount, bytes calldata) external {
        require(to != address(0), "MSC1155: transfer to zero");
        require(from == msg.sender || _operatorApprovals[from][msg.sender], "MSC1155: not allowed");

        uint256 fromBal = _balances[id][from];
        require(fromBal >= amount, "MSC1155: insufficient balance");
        unchecked {
            _balances[id][from] = fromBal - amount;
        }
        _balances[id][to] += amount;
        emit TransferSingle(msg.sender, from, to, id, amount);
    }

    function safeBatchTransferFrom(
        address from,
        address to,
        uint256[] calldata ids,
        uint256[] calldata amounts,
        bytes calldata
    ) external {
        require(ids.length == amounts.length, "MSC1155: length mismatch");
        require(to != address(0), "MSC1155: transfer to zero");
        require(from == msg.sender || _operatorApprovals[from][msg.sender], "MSC1155: not allowed");

        for (uint256 i = 0; i < ids.length; i++) {
            uint256 fromBal = _balances[ids[i]][from];
            uint256 amount = amounts[i];
            require(fromBal >= amount, "MSC1155: insufficient balance");
            unchecked {
                _balances[ids[i]][from] = fromBal - amount;
            }
            _balances[ids[i]][to] += amount;
        }
        emit TransferBatch(msg.sender, from, to, ids, amounts);
    }

    function mint(address to, uint256 id, uint256 amount, string calldata tokenUriValue) external {
        require(to != address(0), "MSC1155: mint to zero");
        _balances[id][to] += amount;
        if (bytes(tokenUriValue).length != 0) {
            _tokenURIs[id] = tokenUriValue;
            emit URI(tokenUriValue, id);
        }
        emit TransferSingle(msg.sender, address(0), to, id, amount);
    }

    function mintBatch(address to, uint256[] calldata ids, uint256[] calldata amounts) external {
        require(ids.length == amounts.length, "MSC1155: length mismatch");
        require(to != address(0), "MSC1155: mint to zero");

        for (uint256 i = 0; i < ids.length; i++) {
            _balances[ids[i]][to] += amounts[i];
        }
        emit TransferBatch(msg.sender, address(0), to, ids, amounts);
    }
}

