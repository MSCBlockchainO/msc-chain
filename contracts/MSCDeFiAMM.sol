// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/// @notice Minimal ERC-20 interface used by AMM.
interface IMSC21Token {
    function transfer(address to, uint256 amount) external returns (bool);
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
    function balanceOf(address account) external view returns (uint256);
}

/// @title MSC DeFi AMM (constant product x*y=k)
/// @notice Starter contract for DeFi pools on MSC EVM.
contract MSCDeFiAMM {
    address public immutable tokenA;
    address public immutable tokenB;
    address public owner;

    // 30 bps = 0.30%
    uint16 public swapFeeBps = 30;

    uint112 private reserveA;
    uint112 private reserveB;

    uint256 public totalShares;
    mapping(address => uint256) public shares;

    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
    event SwapFeeUpdated(uint16 oldFeeBps, uint16 newFeeBps);
    event LiquidityAdded(address indexed provider, uint256 amountA, uint256 amountB, uint256 mintedShares);
    event LiquidityRemoved(address indexed provider, uint256 amountA, uint256 amountB, uint256 burnedShares);
    event Swapped(address indexed trader, address indexed tokenIn, uint256 amountIn, address indexed tokenOut, uint256 amountOut, address to);
    event Sync(uint112 reserveA, uint112 reserveB);

    modifier onlyOwner() {
        require(msg.sender == owner, "MSCDeFiAMM: not owner");
        _;
    }

    constructor(address _tokenA, address _tokenB) {
        require(_tokenA != address(0) && _tokenB != address(0), "MSCDeFiAMM: zero token");
        require(_tokenA != _tokenB, "MSCDeFiAMM: identical token");
        tokenA = _tokenA;
        tokenB = _tokenB;
        owner = msg.sender;
        emit OwnershipTransferred(address(0), msg.sender);
    }

    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "MSCDeFiAMM: zero owner");
        emit OwnershipTransferred(owner, newOwner);
        owner = newOwner;
    }

    function setSwapFeeBps(uint16 newFeeBps) external onlyOwner {
        // Keep fee bounded for safety.
        require(newFeeBps <= 300, "MSCDeFiAMM: fee too high");
        uint16 oldFeeBps = swapFeeBps;
        swapFeeBps = newFeeBps;
        emit SwapFeeUpdated(oldFeeBps, newFeeBps);
    }

    function getReserves() external view returns (uint112 _reserveA, uint112 _reserveB) {
        return (reserveA, reserveB);
    }

    function quoteOut(
        uint256 amountIn,
        uint256 reserveIn,
        uint256 reserveOut
    ) public view returns (uint256 amountOut) {
        require(amountIn > 0, "MSCDeFiAMM: zero input");
        require(reserveIn > 0 && reserveOut > 0, "MSCDeFiAMM: empty reserves");
        uint256 amountInWithFee = amountIn * (10_000 - swapFeeBps);
        uint256 numerator = amountInWithFee * reserveOut;
        uint256 denominator = reserveIn * 10_000 + amountInWithFee;
        amountOut = numerator / denominator;
    }

    function addLiquidity(
        uint256 amountA,
        uint256 amountB,
        uint256 minShares
    ) external returns (uint256 mintedShares) {
        require(amountA > 0 && amountB > 0, "MSCDeFiAMM: zero amount");

        require(IMSC21Token(tokenA).transferFrom(msg.sender, address(this), amountA), "MSCDeFiAMM: transferFrom A failed");
        require(IMSC21Token(tokenB).transferFrom(msg.sender, address(this), amountB), "MSCDeFiAMM: transferFrom B failed");

        uint256 _totalShares = totalShares;
        if (_totalShares == 0) {
            mintedShares = _sqrt(amountA * amountB);
        } else {
            mintedShares = _min(
                (amountA * _totalShares) / reserveA,
                (amountB * _totalShares) / reserveB
            );
        }

        require(mintedShares > 0, "MSCDeFiAMM: zero shares");
        require(mintedShares >= minShares, "MSCDeFiAMM: slippage shares");

        shares[msg.sender] += mintedShares;
        totalShares = _totalShares + mintedShares;

        _sync();
        emit LiquidityAdded(msg.sender, amountA, amountB, mintedShares);
    }

    function removeLiquidity(
        uint256 burnShares,
        uint256 minAmountA,
        uint256 minAmountB
    ) external returns (uint256 amountA, uint256 amountB) {
        require(burnShares > 0, "MSCDeFiAMM: zero burn");
        uint256 userShares = shares[msg.sender];
        require(userShares >= burnShares, "MSCDeFiAMM: insufficient shares");

        amountA = (uint256(reserveA) * burnShares) / totalShares;
        amountB = (uint256(reserveB) * burnShares) / totalShares;
        require(amountA >= minAmountA && amountB >= minAmountB, "MSCDeFiAMM: slippage remove");
        require(amountA > 0 && amountB > 0, "MSCDeFiAMM: zero output");

        shares[msg.sender] = userShares - burnShares;
        totalShares -= burnShares;

        require(IMSC21Token(tokenA).transfer(msg.sender, amountA), "MSCDeFiAMM: transfer A failed");
        require(IMSC21Token(tokenB).transfer(msg.sender, amountB), "MSCDeFiAMM: transfer B failed");

        _sync();
        emit LiquidityRemoved(msg.sender, amountA, amountB, burnShares);
    }

    function swapExactAForB(
        uint256 amountIn,
        uint256 minAmountOut,
        address to
    ) external returns (uint256 amountOut) {
        require(to != address(0), "MSCDeFiAMM: zero to");
        amountOut = quoteOut(amountIn, reserveA, reserveB);
        require(amountOut >= minAmountOut, "MSCDeFiAMM: slippage A->B");

        require(IMSC21Token(tokenA).transferFrom(msg.sender, address(this), amountIn), "MSCDeFiAMM: transferFrom A failed");
        require(IMSC21Token(tokenB).transfer(to, amountOut), "MSCDeFiAMM: transfer B failed");

        _sync();
        emit Swapped(msg.sender, tokenA, amountIn, tokenB, amountOut, to);
    }

    function swapExactBForA(
        uint256 amountIn,
        uint256 minAmountOut,
        address to
    ) external returns (uint256 amountOut) {
        require(to != address(0), "MSCDeFiAMM: zero to");
        amountOut = quoteOut(amountIn, reserveB, reserveA);
        require(amountOut >= minAmountOut, "MSCDeFiAMM: slippage B->A");

        require(IMSC21Token(tokenB).transferFrom(msg.sender, address(this), amountIn), "MSCDeFiAMM: transferFrom B failed");
        require(IMSC21Token(tokenA).transfer(to, amountOut), "MSCDeFiAMM: transfer A failed");

        _sync();
        emit Swapped(msg.sender, tokenB, amountIn, tokenA, amountOut, to);
    }

    function _sync() internal {
        uint256 balA = IMSC21Token(tokenA).balanceOf(address(this));
        uint256 balB = IMSC21Token(tokenB).balanceOf(address(this));
        require(balA <= type(uint112).max && balB <= type(uint112).max, "MSCDeFiAMM: reserve overflow");
        reserveA = uint112(balA);
        reserveB = uint112(balB);
        emit Sync(reserveA, reserveB);
    }

    function _min(uint256 x, uint256 y) internal pure returns (uint256) {
        return x <= y ? x : y;
    }

    function _sqrt(uint256 y) internal pure returns (uint256 z) {
        if (y == 0) return 0;
        if (y <= 3) return 1;
        z = y;
        uint256 x = y / 2 + 1;
        while (x < z) {
            z = x;
            x = (y / x + x) / 2;
        }
    }
}
