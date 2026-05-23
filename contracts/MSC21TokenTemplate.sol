// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

// DEX-ready MSC-21 token template (ERC-20 style).
// Features:
// - Standard transfer/approve/transferFrom flow
// - Owner-controlled mint
// - Holder burn
// - Ownership transfer
contract MSC21TokenTemplate {
    string public name;
    string public symbol;
    uint8 public decimals;
    uint256 public totalSupply;

    address public owner;

    mapping(address => uint256) private _balances;
    mapping(address => mapping(address => uint256)) private _allowances;

    event Transfer(address indexed from, address indexed to, uint256 value);
    event Approval(address indexed owner, address indexed spender, uint256 value);
    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);

    modifier onlyOwner() {
        require(msg.sender == owner, "MSC21: only owner");
        _;
    }

    constructor(
        string memory tokenName,
        string memory tokenSymbol,
        uint8 tokenDecimals,
        uint256 initialSupply
    ) {
        require(bytes(tokenName).length > 0, "MSC21: empty name");
        require(bytes(tokenSymbol).length > 0, "MSC21: empty symbol");
        owner = msg.sender;
        name = tokenName;
        symbol = tokenSymbol;
        decimals = tokenDecimals;
        emit OwnershipTransferred(address(0), owner);
        _mint(owner, initialSupply);
    }

    function balanceOf(address account) external view returns (uint256) {
        return _balances[account];
    }

    function allowance(address owner_, address spender) external view returns (uint256) {
        return _allowances[owner_][spender];
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
        uint256 currentAllowance = _allowances[from][msg.sender];
        require(currentAllowance >= amount, "MSC21: insufficient allowance");
        unchecked {
            _allowances[from][msg.sender] = currentAllowance - amount;
        }
        _transfer(from, to, amount);
        emit Approval(from, msg.sender, _allowances[from][msg.sender]);
        return true;
    }

    // Optional helper for DEX UX.
    function increaseAllowance(address spender, uint256 addedValue) external returns (bool) {
        _approve(msg.sender, spender, _allowances[msg.sender][spender] + addedValue);
        return true;
    }

    // Optional helper for DEX UX.
    function decreaseAllowance(address spender, uint256 subtractedValue) external returns (bool) {
        uint256 currentAllowance = _allowances[msg.sender][spender];
        require(currentAllowance >= subtractedValue, "MSC21: allowance below zero");
        unchecked {
            _approve(msg.sender, spender, currentAllowance - subtractedValue);
        }
        return true;
    }

    // Owner can mint for liquidity bootstrap, incentives, etc.
    function mint(address to, uint256 amount) external onlyOwner returns (bool) {
        _mint(to, amount);
        return true;
    }

    // Holder burn.
    function burn(uint256 amount) external returns (bool) {
        _burn(msg.sender, amount);
        return true;
    }

    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "MSC21: new owner is zero");
        address oldOwner = owner;
        owner = newOwner;
        emit OwnershipTransferred(oldOwner, newOwner);
    }

    function _transfer(address from, address to, uint256 amount) internal {
        require(from != address(0), "MSC21: transfer from zero");
        require(to != address(0), "MSC21: transfer to zero");
        uint256 fromBalance = _balances[from];
        require(fromBalance >= amount, "MSC21: insufficient balance");
        unchecked {
            _balances[from] = fromBalance - amount;
        }
        _balances[to] += amount;
        emit Transfer(from, to, amount);
    }

    function _approve(address owner_, address spender, uint256 amount) internal {
        require(owner_ != address(0), "MSC21: approve from zero");
        require(spender != address(0), "MSC21: approve to zero");
        _allowances[owner_][spender] = amount;
        emit Approval(owner_, spender, amount);
    }

    function _mint(address to, uint256 amount) internal {
        require(to != address(0), "MSC21: mint to zero");
        totalSupply += amount;
        _balances[to] += amount;
        emit Transfer(address(0), to, amount);
    }

    function _burn(address from, uint256 amount) internal {
        require(from != address(0), "MSC21: burn from zero");
        uint256 fromBalance = _balances[from];
        require(fromBalance >= amount, "MSC21: burn exceeds balance");
        unchecked {
            _balances[from] = fromBalance - amount;
        }
        totalSupply -= amount;
        emit Transfer(from, address(0), amount);
    }
}

