// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

interface IMSC21RewardToken {
    function transfer(address to, uint256 amount) external returns (bool);
    function transferFrom(address from, address to, uint256 amount) external returns (bool);
}

interface IMSC721Gate {
    function ownerOf(uint256 tokenId) external view returns (address);
}

/// @title MSC GameFi Quest
/// @notice Starter GameFi contract: XP system + reward-claim quests.
contract MSCGameFiQuest {
    struct Quest {
        uint256 rewardAmount;
        uint256 xpRequired;
        bool active;
    }

    address public owner;
    address public immutable rewardToken;
    address public gateNFT;

    mapping(uint256 => Quest) public quests;
    mapping(address => uint256) public playerXP;
    mapping(address => mapping(uint256 => bool)) public questCompleted;

    event OwnershipTransferred(address indexed previousOwner, address indexed newOwner);
    event GateNFTUpdated(address indexed oldGateNFT, address indexed newGateNFT);
    event QuestConfigured(uint256 indexed questId, uint256 rewardAmount, uint256 xpRequired, bool active);
    event XPGranted(address indexed player, uint256 amount, uint256 newXP);
    event QuestCompleted(address indexed player, uint256 indexed questId, uint256 rewardAmount, uint256 gateTokenId);
    event RewardsDeposited(address indexed from, uint256 amount);

    modifier onlyOwner() {
        require(msg.sender == owner, "MSCGameFiQuest: not owner");
        _;
    }

    constructor(address _rewardToken) {
        require(_rewardToken != address(0), "MSCGameFiQuest: zero reward token");
        rewardToken = _rewardToken;
        owner = msg.sender;
        emit OwnershipTransferred(address(0), msg.sender);
    }

    function transferOwnership(address newOwner) external onlyOwner {
        require(newOwner != address(0), "MSCGameFiQuest: zero owner");
        emit OwnershipTransferred(owner, newOwner);
        owner = newOwner;
    }

    function setGateNFT(address newGateNFT) external onlyOwner {
        emit GateNFTUpdated(gateNFT, newGateNFT);
        gateNFT = newGateNFT;
    }

    function configureQuest(
        uint256 questId,
        uint256 rewardAmount,
        uint256 xpRequired,
        bool active
    ) external onlyOwner {
        quests[questId] = Quest({
            rewardAmount: rewardAmount,
            xpRequired: xpRequired,
            active: active
        });
        emit QuestConfigured(questId, rewardAmount, xpRequired, active);
    }

    function depositRewards(uint256 amount) external onlyOwner {
        require(amount > 0, "MSCGameFiQuest: zero deposit");
        require(
            IMSC21RewardToken(rewardToken).transferFrom(msg.sender, address(this), amount),
            "MSCGameFiQuest: transferFrom failed"
        );
        emit RewardsDeposited(msg.sender, amount);
    }

    function grantXP(address player, uint256 amount) external onlyOwner {
        require(player != address(0), "MSCGameFiQuest: zero player");
        require(amount > 0, "MSCGameFiQuest: zero xp");
        uint256 updated = playerXP[player] + amount;
        playerXP[player] = updated;
        emit XPGranted(player, amount, updated);
    }

    /// @notice Complete a quest and claim reward.
    /// @param questId Quest ID.
    /// @param gateTokenId NFT token id used only when gateNFT is set.
    function completeQuest(uint256 questId, uint256 gateTokenId) external returns (uint256 reward) {
        Quest memory q = quests[questId];
        require(q.active, "MSCGameFiQuest: quest inactive");
        require(!questCompleted[msg.sender][questId], "MSCGameFiQuest: already completed");
        require(playerXP[msg.sender] >= q.xpRequired, "MSCGameFiQuest: insufficient xp");

        if (gateNFT != address(0)) {
            require(IMSC721Gate(gateNFT).ownerOf(gateTokenId) == msg.sender, "MSCGameFiQuest: gate NFT required");
        }

        questCompleted[msg.sender][questId] = true;
        reward = q.rewardAmount;
        require(IMSC21RewardToken(rewardToken).transfer(msg.sender, reward), "MSCGameFiQuest: reward transfer failed");

        emit QuestCompleted(msg.sender, questId, reward, gateTokenId);
    }

    function playerState(address player, uint256 questId) external view returns (
        uint256 xp,
        bool completed,
        uint256 rewardAmount,
        uint256 xpRequired,
        bool active
    ) {
        Quest memory q = quests[questId];
        return (
            playerXP[player],
            questCompleted[player][questId],
            q.rewardAmount,
            q.xpRequired,
            q.active
        );
    }
}
