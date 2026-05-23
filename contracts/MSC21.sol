// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

// MSC-21: ERC-20 style token standard for MSC Chain EVM compatibility.
contract MSC21 {
    string public name;
    string public symbol;
    uint8 public decimals;
    uint256 public totalSupply;

    mapping(address => uint256) private _balances;
    mapping(address => mapping(address => uint256)) private _allowances;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);

    constructor(
        string memory tokenName,
        string memory tokenSymbol,
        uint8 tokenDecimals,
        uint256 initialSupply
    ) {
        name = tokenName;
        symbol = tokenSymbol;
        decimals = tokenDecimals;
        _mint(msg.sender, initialSupply);
    }

    function balanceOf(address account) external view returns (uint256) {
        return _balances[account];
    }

    function allowance(address owner, address spender) external view returns (uint256) {
        return _allowances[owner][spender];
    }

    function transfer(address to, uint256 amount) external returns (bool) {
        _transfer(msg.sender, to, amount);
        return true;
    }

    function approve(address spender, uint256 amount) external returns (bool) {
        _approve(msg.sender, spender, amount);
        return true;
    }

    function transferFrom(address from, address to, uint256 amount) external returns (bool) {
        uint256 allowed = _allowances[from][msg.sender];
        require(allowed >= amount, "MSC21: insufficient allowance");
        unchecked {
            _allowances[from][msg.sender] = allowed - amount;
        }
        _transfer(from, to, amount);
        emit Approval(from, msg.sender, _allowances[from][msg.sender]);
        return true;
    }

    function _transfer(address from, address to, uint256 amount) internal {
        require(to != address(0), "MSC21: transfer to zero");
        uint256 fromBal = _balances[from];
        require(fromBal >= amount, "MSC21: insufficient balance");
        unchecked {
            _balances[from] = fromBal - amount;
        }
        _balances[to] += amount;
        emit Transfer(from, to, amount);
    }

    function _approve(address owner, address spender, uint256 amount) internal {
        require(owner != address(0), "MSC21: approve from zero");
        require(spender != address(0), "MSC21: approve to zero");
        _allowances[owner][spender] = amount;
        emit Approval(owner, spender, amount);
    }

    function _mint(address to, uint256 amount) internal {
        require(to != address(0), "MSC21: mint to zero");
        totalSupply += amount;
        _balances[to] += amount;
        emit Transfer(address(0), to, amount);
    }
}

