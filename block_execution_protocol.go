package main

import (
	"errors"
	"fmt"
	"strings"

	executioncore "msc-chain/execution"
)

const (
	blockProtocolVersionV1 uint32 = uint32(executioncore.VersionV1)
	blockProtocolVersionV2 uint32 = uint32(executioncore.VersionV2)
	blockFeatureDTLV2      uint64 = uint64(executioncore.FeatureDTLV2)
	blockKnownFeatures     uint64 = uint64(executioncore.FeatureDTLV2 | executioncore.FeatureBridge | executioncore.FeatureNFT | executioncore.FeatureLending)
)

// initializeBlockProtocol stamps the immutable protocol envelope used by new
// blocks. A governance-approved future upgrade changes the committed envelope;
// execution never reads ConfigDTLV2ActivationHeight or another local knob.
func initializeBlockProtocol(block *Block) {
	if block == nil || block.ProtocolVersion != 0 {
		return
	}
	block.ProtocolVersion = blockProtocolVersionV1
	block.FeatureBitmap = 0
}

func scheduledBlockProtocol(height uint64, activationHeight uint64, baseFeatures uint64) (uint32, uint64, error) {
	baseFeatures &^= blockFeatureDTLV2
	if activationHeight == 0 {
		return blockProtocolVersionV1, baseFeatures, nil
	}
	schedule, err := executioncore.NewSchedule(
		executioncore.Activation{
			Height: 0,
			Protocol: executioncore.Protocol{
				Version:  executioncore.VersionV1,
				Features: executioncore.FeatureBitmap(baseFeatures),
			},
		},
		executioncore.Activation{
			Height: activationHeight,
			Protocol: executioncore.Protocol{
				Version:  executioncore.VersionV2,
				Features: executioncore.FeatureBitmap(baseFeatures | blockFeatureDTLV2),
			},
		},
	)
	if err != nil {
		return 0, 0, err
	}
	protocol := schedule.At(height)
	return uint32(protocol.Version), uint64(protocol.Features), nil
}

// initializeBlockProtocolFromParent carries a committed activation schedule
// forward and switches executors at exactly H. Local configuration is not an
// input to this decision.
func initializeBlockProtocolFromParent(block *Block, parent Block) {
	if block == nil || block.ProtocolVersion != 0 {
		return
	}
	activationHeight := parent.DTLV2ActivationHeight
	baseFeatures := parent.FeatureBitmap &^ blockFeatureDTLV2
	if parent.ProtocolVersion == 0 {
		activationHeight = 0
		baseFeatures = 0
	}
	version, features, err := scheduledBlockProtocol(block.ID, activationHeight, baseFeatures)
	if err != nil {
		initializeBlockProtocol(block)
		return
	}
	block.ProtocolVersion = version
	block.FeatureBitmap = features
	block.DTLV2ActivationHeight = activationHeight
}

// scheduleBlockDTLV2Activation commits a future exact activation height into
// a proposal. Once quorum finalizes this block, descendants cannot alter H.
func scheduleBlockDTLV2Activation(block *Block, parent Block, activationHeight uint64) error {
	if block == nil || block.ID == 0 {
		return errors.New("protocol_upgrade_block_required")
	}
	if parent.DTLV2ActivationHeight != 0 && parent.DTLV2ActivationHeight != activationHeight {
		return errors.New("dtl_v2_activation_height_already_committed")
	}
	if activationHeight <= block.ID {
		return errors.New("dtl_v2_activation_must_be_future")
	}
	version, features, err := scheduledBlockProtocol(block.ID, activationHeight, parent.FeatureBitmap)
	if err != nil {
		return err
	}
	block.ProtocolVersion = version
	block.FeatureBitmap = features
	block.DTLV2ActivationHeight = activationHeight
	return nil
}

func initializeBlockCommitteeEnvelope(block *Block) {
	if block == nil || block.ProtocolVersion == 0 {
		return
	}
	if block.ValidatorSetVersion == 0 {
		block.ValidatorSetVersion = block.ID
	}
	if strings.TrimSpace(block.CommitteeHash) == "" {
		block.CommitteeHash = strings.TrimSpace(block.ValidatorSetHash)
	}
}

func blockDTLV2Enabled(protocolVersion uint32, featureBitmap uint64) bool {
	return protocolVersion >= blockProtocolVersionV2 && featureBitmap&blockFeatureDTLV2 != 0
}

func validateBlockProtocolEnvelope(block Block) error {
	if block.ProtocolVersion == 0 {
		if block.FeatureBitmap != 0 {
			return errors.New("feature_bitmap_without_protocol_version")
		}
		return nil // persisted pre-version-envelope block
	}
	if block.ProtocolVersion != blockProtocolVersionV1 && block.ProtocolVersion != blockProtocolVersionV2 {
		return fmt.Errorf("unsupported_protocol_version: %d", block.ProtocolVersion)
	}
	if block.FeatureBitmap&^blockKnownFeatures != 0 {
		return errors.New("unknown_protocol_feature")
	}
	if block.FeatureBitmap&blockFeatureDTLV2 != 0 && block.ProtocolVersion < blockProtocolVersionV2 {
		return errors.New("dtl_v2_feature_requires_protocol_v2")
	}
	if block.DTLV2ActivationHeight > 0 {
		expectedVersion, expectedFeatures, err := scheduledBlockProtocol(
			block.ID,
			block.DTLV2ActivationHeight,
			block.FeatureBitmap&^blockFeatureDTLV2,
		)
		if err != nil {
			return err
		}
		if block.ProtocolVersion != expectedVersion || block.FeatureBitmap != expectedFeatures {
			return errors.New("dtl_v2_activation_envelope_mismatch")
		}
	}
	commitment := executionCommitmentsFromBlock(block)
	for name, value := range map[string]string{
		"state_root":        commitment.StateRoot,
		"receipts_root":     commitment.ReceiptsRoot,
		"event_root":        commitment.EventRoot,
		"execution_hash":    commitment.ExecutionHash,
		"fee_root":          commitment.FeeRoot,
		"dtl_state_root":    commitment.DTLStateRoot,
		"dtl_receipts_root": commitment.DTLReceiptsRoot,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("execution_commitment_missing: %s", name)
		}
	}
	return nil
}

func validateBlockProtocolTransition(parent Block, block Block) error {
	if block.ProtocolVersion == 0 {
		if parent.ProtocolVersion != 0 {
			return errors.New("protocol_version_downgrade")
		}
		return nil
	}
	if parent.ProtocolVersion == 0 {
		if block.ProtocolVersion != blockProtocolVersionV1 || block.FeatureBitmap&blockFeatureDTLV2 != 0 {
			return errors.New("protocol_transition_must_start_v1")
		}
		if block.DTLV2ActivationHeight > 0 && block.DTLV2ActivationHeight <= block.ID {
			return errors.New("dtl_v2_activation_must_be_future")
		}
		return nil
	}
	parentActivation := parent.DTLV2ActivationHeight
	blockActivation := block.DTLV2ActivationHeight
	if parentActivation != 0 && blockActivation != parentActivation {
		return errors.New("dtl_v2_activation_height_changed")
	}
	if parentActivation == 0 && blockActivation != 0 && blockActivation <= block.ID {
		return errors.New("dtl_v2_activation_must_be_future")
	}
	if parentActivation == 0 && blockActivation == 0 {
		if block.ProtocolVersion != parent.ProtocolVersion || block.FeatureBitmap != parent.FeatureBitmap {
			return errors.New("unscheduled_protocol_transition")
		}
		return nil
	}
	if parent.FeatureBitmap&^blockFeatureDTLV2 != block.FeatureBitmap&^blockFeatureDTLV2 {
		return errors.New("unscheduled_feature_transition")
	}
	return validateBlockProtocolEnvelope(block)
}

func applyExecutionCommitmentsToBlock(block *Block, commitments ExecutionCommitments) {
	if block == nil {
		return
	}
	initializeBlockProtocol(block)
	block.StateRoot = strings.TrimSpace(commitments.StateRoot)
	block.ReceiptRoot = strings.TrimSpace(commitments.ReceiptsRoot)
	block.ReceiptsRoot = strings.TrimSpace(commitments.ReceiptsRoot)
	block.EventRoot = strings.TrimSpace(commitments.EventRoot)
	block.ExecutionHash = strings.TrimSpace(commitments.ExecutionHash)
	block.FeeRoot = strings.TrimSpace(commitments.FeeRoot)
	block.DTLStateRoot = strings.TrimSpace(commitments.DTLStateRoot)
	block.DTLReceiptsRoot = strings.TrimSpace(commitments.DTLReceiptsRoot)
}

func executionCommitmentsFromBlock(block Block) ExecutionCommitments {
	receiptsRoot := strings.TrimSpace(block.ReceiptsRoot)
	if receiptsRoot == "" {
		receiptsRoot = strings.TrimSpace(block.ReceiptRoot)
	}
	return ExecutionCommitments{
		StateRoot:       strings.TrimSpace(block.StateRoot),
		ReceiptsRoot:    receiptsRoot,
		EventRoot:       strings.TrimSpace(block.EventRoot),
		ExecutionHash:   strings.TrimSpace(block.ExecutionHash),
		FeeRoot:         strings.TrimSpace(block.FeeRoot),
		DTLStateRoot:    strings.TrimSpace(block.DTLStateRoot),
		DTLReceiptsRoot: strings.TrimSpace(block.DTLReceiptsRoot),
	}
}

func verifyBlockExecutionCommitments(block Block, expected ExecutionCommitments) error {
	if block.ProtocolVersion == 0 {
		return nil
	}
	if block.ReceiptsRoot != "" && block.ReceiptRoot != "" && !strings.EqualFold(strings.TrimSpace(block.ReceiptsRoot), strings.TrimSpace(block.ReceiptRoot)) {
		return errors.New("receipt_root_alias_mismatch")
	}
	got := executionCommitmentsFromBlock(block)
	if !strings.EqualFold(got.StateRoot, expected.StateRoot) ||
		!strings.EqualFold(got.ReceiptsRoot, expected.ReceiptsRoot) ||
		!strings.EqualFold(got.EventRoot, expected.EventRoot) ||
		!strings.EqualFold(got.ExecutionHash, expected.ExecutionHash) ||
		!strings.EqualFold(got.FeeRoot, expected.FeeRoot) ||
		!strings.EqualFold(got.DTLStateRoot, expected.DTLStateRoot) ||
		!strings.EqualFold(got.DTLReceiptsRoot, expected.DTLReceiptsRoot) {
		return errors.New("execution_commitment_mismatch")
	}
	return nil
}

// blockExecutionProtocolHashData binds the version, feature bitmap, registry
// version, committee, and every execution/DTL root into HashBlock. It returns
// empty data for legacy blocks to retain historical hash compatibility.
func blockExecutionProtocolHashData(block Block) string {
	if block.ProtocolVersion == 0 {
		return ""
	}
	commitment := executionCommitmentsFromBlock(block)
	activationData := ""
	if block.DTLV2ActivationHeight > 0 {
		activationData = fmt.Sprintf("|dtl_v2_activation_height=%d", block.DTLV2ActivationHeight)
	}
	return fmt.Sprintf(
		"|protocol=%d|features=%d%s|validator_set_version=%d|committee=%s|execution_commitment=%s",
		block.ProtocolVersion,
		block.FeatureBitmap,
		activationData,
		block.ValidatorSetVersion,
		strings.TrimSpace(block.CommitteeHash),
		commitment.Hash(),
	)
}
