package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/klauspost/compress/zstd"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	p2pMessageProtoMagic     = "MSCP2P1\n"
	snapshotBinaryMagic      = "MSCSNAPBIN1\n"
	blockFileProtoMagic      = "MSCBLOCKPB1\n"
	wireCodecSnapshotVersion = 1
)

func appendProtoString(b []byte, field protowire.Number, value string) []byte {
	if value == "" {
		return b
	}
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendString(b, value)
}

func appendProtoBytes(b []byte, field protowire.Number, value []byte) []byte {
	if len(value) == 0 {
		return b
	}
	b = protowire.AppendTag(b, field, protowire.BytesType)
	return protowire.AppendBytes(b, value)
}

func appendProtoVarint(b []byte, field protowire.Number, value uint64) []byte {
	if value == 0 {
		return b
	}
	b = protowire.AppendTag(b, field, protowire.VarintType)
	return protowire.AppendVarint(b, value)
}

func consumeProtoBytes(buf []byte) ([]byte, int, error) {
	value, n := protowire.ConsumeBytes(buf)
	if n < 0 {
		return nil, 0, protowire.ParseError(n)
	}
	return value, n, nil
}

func consumeProtoString(buf []byte) (string, int, error) {
	value, n := protowire.ConsumeString(buf)
	if n < 0 {
		return "", 0, protowire.ParseError(n)
	}
	return value, n, nil
}

func consumeProtoVarint(buf []byte) (uint64, int, error) {
	value, n := protowire.ConsumeVarint(buf)
	if n < 0 {
		return 0, 0, protowire.ParseError(n)
	}
	return value, n, nil
}

func skipProtoField(num protowire.Number, typ protowire.Type, buf []byte) (int, error) {
	n := protowire.ConsumeFieldValue(num, typ, buf)
	if n < 0 {
		return 0, protowire.ParseError(n)
	}
	return n, nil
}

func MarshalP2PMessage(msg Message) ([]byte, error) {
	msg.Type = strings.TrimSpace(msg.Type)
	if msg.Type == "" {
		return nil, fmt.Errorf("p2p message type required")
	}
	var body []byte
	body = appendProtoString(body, 1, msg.Type)
	body = appendProtoBytes(body, 2, msg.Data)
	return append([]byte(p2pMessageProtoMagic), body...), nil
}

func UnmarshalP2PMessage(data []byte, msg *Message) error {
	if msg == nil {
		return fmt.Errorf("nil p2p message")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return fmt.Errorf("empty p2p message")
	}
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return json.Unmarshal(data, msg)
	}
	if bytes.HasPrefix(data, []byte(p2pMessageProtoMagic)) {
		data = data[len(p2pMessageProtoMagic):]
	}
	var out Message
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		switch num {
		case 1:
			if typ != protowire.BytesType {
				return fmt.Errorf("p2p message type field has wire type %v", typ)
			}
			value, used, err := consumeProtoString(data)
			if err != nil {
				return err
			}
			out.Type = value
			data = data[used:]
		case 2:
			if typ != protowire.BytesType {
				return fmt.Errorf("p2p message data field has wire type %v", typ)
			}
			value, used, err := consumeProtoBytes(data)
			if err != nil {
				return err
			}
			out.Data = append(out.Data[:0], value...)
			data = data[used:]
		default:
			used, err := skipProtoField(num, typ, data)
			if err != nil {
				return err
			}
			data = data[used:]
		}
	}
	if strings.TrimSpace(out.Type) == "" {
		return fmt.Errorf("p2p message type missing")
	}
	*msg = out
	return nil
}

func MustWireMessage(msg Message) []byte {
	data, err := MarshalP2PMessage(msg)
	if err != nil {
		return MustJSON(msg)
	}
	return data
}

func MarshalTransactionProtobuf(tx Transaction) ([]byte, error) {
	var b []byte
	b = appendProtoString(b, 1, tx.ID)
	b = appendProtoString(b, 2, tx.From)
	b = appendProtoString(b, 3, tx.To)
	b = appendProtoVarint(b, 4, uint64(tx.Amount))
	b = appendProtoVarint(b, 5, uint64(tx.Nonce))
	b = appendProtoString(b, 6, tx.PublicKey)
	b = appendProtoString(b, 7, tx.Signature)
	b = appendProtoVarint(b, 8, uint64(tx.Fee))
	b = appendProtoVarint(b, 9, uint64(tx.Expiry))
	b = appendProtoVarint(b, 10, tx.GasLimit)
	b = appendProtoVarint(b, 11, tx.StakeEpochs)
	b = appendProtoString(b, 12, tx.ValidatorPubKey)
	b = appendProtoString(b, 13, tx.EVMCode)
	b = appendProtoString(b, 14, tx.EVMInput)
	b = appendProtoVarint(b, 15, tx.EVMGasLimit)
	b = appendProtoString(b, 16, tx.EVMRawTx)
	b = appendProtoString(b, 17, tx.EVMTxHash)
	b = appendProtoString(b, 18, tx.DTLTxType)
	b = appendProtoString(b, 19, tx.DTLTokenID)
	b = appendProtoString(b, 20, tx.DTLPayload)
	b = appendProtoString(b, 21, tx.DTLGovernanceCert)
	if tx.ValidatorUpdateCert != nil {
		raw, err := json.Marshal(tx.ValidatorUpdateCert)
		if err != nil {
			return nil, err
		}
		b = appendProtoBytes(b, 22, raw)
	}
	b = appendProtoString(b, 23, tx.TaskID)
	b = appendProtoVarint(b, 24, uint64(tx.Input))
	b = appendProtoVarint(b, 25, uint64(tx.Type))
	b = appendProtoString(b, 26, tx.ChainID)
	b = appendProtoString(b, 27, tx.Coin)
	return b, nil
}

func UnmarshalTransactionProtobuf(data []byte, tx *Transaction) error {
	if tx == nil {
		return fmt.Errorf("nil transaction")
	}
	var out Transaction
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		readString := func() (string, error) {
			if typ != protowire.BytesType {
				return "", fmt.Errorf("transaction field %d has wire type %v", num, typ)
			}
			value, used, err := consumeProtoString(data)
			if err != nil {
				return "", err
			}
			data = data[used:]
			return value, nil
		}
		readBytes := func() ([]byte, error) {
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("transaction field %d has wire type %v", num, typ)
			}
			value, used, err := consumeProtoBytes(data)
			if err != nil {
				return nil, err
			}
			data = data[used:]
			return value, nil
		}
		readVarint := func() (uint64, error) {
			if typ != protowire.VarintType {
				return 0, fmt.Errorf("transaction field %d has wire type %v", num, typ)
			}
			value, used, err := consumeProtoVarint(data)
			if err != nil {
				return 0, err
			}
			data = data[used:]
			return value, nil
		}
		switch num {
		case 1:
			out.ID, _ = readString()
		case 2:
			out.From, _ = readString()
		case 3:
			out.To, _ = readString()
		case 4:
			v, err := readVarint()
			if err != nil {
				return err
			}
			out.Amount = int(v)
		case 5:
			v, err := readVarint()
			if err != nil {
				return err
			}
			out.Nonce = int(v)
		case 6:
			out.PublicKey, _ = readString()
		case 7:
			out.Signature, _ = readString()
		case 8:
			v, err := readVarint()
			if err != nil {
				return err
			}
			out.Fee = int(v)
		case 9:
			v, err := readVarint()
			if err != nil {
				return err
			}
			out.Expiry = int64(v)
		case 10:
			v, err := readVarint()
			if err != nil {
				return err
			}
			out.GasLimit = v
		case 11:
			v, err := readVarint()
			if err != nil {
				return err
			}
			out.StakeEpochs = v
		case 12:
			out.ValidatorPubKey, _ = readString()
		case 13:
			out.EVMCode, _ = readString()
		case 14:
			out.EVMInput, _ = readString()
		case 15:
			v, err := readVarint()
			if err != nil {
				return err
			}
			out.EVMGasLimit = v
		case 16:
			out.EVMRawTx, _ = readString()
		case 17:
			out.EVMTxHash, _ = readString()
		case 18:
			out.DTLTxType, _ = readString()
		case 19:
			out.DTLTokenID, _ = readString()
		case 20:
			out.DTLPayload, _ = readString()
		case 21:
			out.DTLGovernanceCert, _ = readString()
		case 22:
			raw, err := readBytes()
			if err != nil {
				return err
			}
			if len(raw) > 0 {
				var cert ValidatorUpdateCertificate
				if err := json.Unmarshal(raw, &cert); err != nil {
					return err
				}
				out.ValidatorUpdateCert = &cert
			}
		case 23:
			out.TaskID, _ = readString()
		case 24:
			v, err := readVarint()
			if err != nil {
				return err
			}
			out.Input = int(v)
		case 25:
			v, err := readVarint()
			if err != nil {
				return err
			}
			out.Type = TxType(v)
		case 26:
			out.ChainID, _ = readString()
		case 27:
			out.Coin, _ = readString()
		default:
			used, err := skipProtoField(num, typ, data)
			if err != nil {
				return err
			}
			data = data[used:]
		}
	}
	*tx = out
	return nil
}

func UnmarshalTransactionWire(data []byte, tx *Transaction) error {
	if len(bytes.TrimSpace(data)) > 0 && bytes.TrimSpace(data)[0] == '{' {
		return json.Unmarshal(data, tx)
	}
	return UnmarshalTransactionProtobuf(data, tx)
}

func MarshalBlockProtobuf(block Block) ([]byte, error) {
	legacy, err := json.Marshal(block)
	if err != nil {
		return nil, err
	}
	var b []byte
	b = appendProtoVarint(b, 1, block.ID)
	b = appendProtoVarint(b, 2, block.Height)
	b = appendProtoVarint(b, 3, uint64(block.Round))
	b = appendProtoString(b, 4, block.BlockHash)
	b = appendProtoString(b, 5, block.PrevHash)
	b = appendProtoString(b, 6, block.Proposer)
	b = appendProtoVarint(b, 7, uint64(block.Timestamp))
	b = appendProtoString(b, 8, block.StateRoot)
	b = appendProtoString(b, 9, block.MempoolRoot)
	b = appendProtoString(b, 10, block.ReceiptRoot)
	b = appendProtoString(b, 11, block.ValidatorSetHash)
	b = appendProtoString(b, 12, block.NextValidatorSetHash)
	b = appendProtoString(b, 13, block.ConsensusMode)
	b = appendProtoVarint(b, 14, block.FinalizedEpoch)
	b = appendProtoVarint(b, 15, block.FinalizedHeight)
	b = appendProtoString(b, 16, block.FinalityRoot)
	for _, tx := range block.Transactions {
		raw, err := MarshalTransactionProtobuf(tx)
		if err != nil {
			return nil, err
		}
		b = appendProtoBytes(b, 20, raw)
	}
	for _, sig := range block.Signatures {
		b = appendProtoString(b, 21, sig)
	}
	b = appendProtoBytes(b, 100, legacy)
	return b, nil
}

func UnmarshalBlockProtobuf(data []byte, block *Block) error {
	if block == nil {
		return fmt.Errorf("nil block")
	}
	var out Block
	var txs []Transaction
	var signatures []string
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		switch num {
		case 1, 2, 3, 7, 14, 15:
			if typ != protowire.VarintType {
				return fmt.Errorf("block field %d has wire type %v", num, typ)
			}
			v, used, err := consumeProtoVarint(data)
			if err != nil {
				return err
			}
			switch num {
			case 1:
				out.ID = v
			case 2:
				out.Height = v
			case 3:
				out.Round = uint32(v)
			case 7:
				out.Timestamp = int64(v)
			case 14:
				out.FinalizedEpoch = v
			case 15:
				out.FinalizedHeight = v
			}
			data = data[used:]
		case 4, 5, 6, 8, 9, 10, 11, 12, 13, 16, 21:
			if typ != protowire.BytesType {
				return fmt.Errorf("block field %d has wire type %v", num, typ)
			}
			v, used, err := consumeProtoString(data)
			if err != nil {
				return err
			}
			switch num {
			case 4:
				out.BlockHash = v
			case 5:
				out.PrevHash = v
			case 6:
				out.Proposer = v
			case 8:
				out.StateRoot = v
			case 9:
				out.MempoolRoot = v
			case 10:
				out.ReceiptRoot = v
			case 11:
				out.ValidatorSetHash = v
			case 12:
				out.NextValidatorSetHash = v
			case 13:
				out.ConsensusMode = v
			case 16:
				out.FinalityRoot = v
			case 21:
				signatures = append(signatures, v)
			}
			data = data[used:]
		case 20:
			raw, used, err := consumeProtoBytes(data)
			if err != nil {
				return err
			}
			var tx Transaction
			if err := UnmarshalTransactionProtobuf(raw, &tx); err != nil {
				return err
			}
			txs = append(txs, tx)
			data = data[used:]
		case 100:
			raw, used, err := consumeProtoBytes(data)
			if err != nil {
				return err
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &out); err != nil {
					return err
				}
			}
			data = data[used:]
		default:
			used, err := skipProtoField(num, typ, data)
			if err != nil {
				return err
			}
			data = data[used:]
		}
	}
	if len(txs) > 0 {
		out.Transactions = txs
	}
	if len(signatures) > 0 {
		out.Signatures = signatures
	}
	*block = out
	return nil
}

func UnmarshalBlockWire(data []byte, block *Block) error {
	if len(bytes.TrimSpace(data)) > 0 && bytes.TrimSpace(data)[0] == '{' {
		return json.Unmarshal(data, block)
	}
	return UnmarshalBlockProtobuf(data, block)
}

func MarshalBlockFileRecordProtobuf(record BlockFileRecord) ([]byte, error) {
	blockRaw, err := MarshalBlockProtobuf(record.Block)
	if err != nil {
		return nil, err
	}
	var b []byte
	b = appendProtoVarint(b, 1, record.Height)
	b = appendProtoString(b, 2, record.BlockHash)
	b = appendProtoString(b, 3, record.StateRoot)
	b = appendProtoBytes(b, 4, blockRaw)
	for _, validator := range record.ValidatorSet {
		b = appendProtoString(b, 5, validator)
	}
	return append([]byte(blockFileProtoMagic), b...), nil
}

func UnmarshalBlockFileRecordProtobuf(data []byte, record *BlockFileRecord) error {
	if record == nil {
		return fmt.Errorf("nil block file record")
	}
	if !bytes.HasPrefix(data, []byte(blockFileProtoMagic)) {
		return fmt.Errorf("block file protobuf magic mismatch")
	}
	data = data[len(blockFileProtoMagic):]
	var out BlockFileRecord
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		switch num {
		case 1:
			if typ != protowire.VarintType {
				return fmt.Errorf("block record height wire type %v", typ)
			}
			v, used, err := consumeProtoVarint(data)
			if err != nil {
				return err
			}
			out.Height = v
			data = data[used:]
		case 2:
			v, used, err := consumeProtoString(data)
			if err != nil {
				return err
			}
			out.BlockHash = v
			data = data[used:]
		case 3:
			v, used, err := consumeProtoString(data)
			if err != nil {
				return err
			}
			out.StateRoot = v
			data = data[used:]
		case 4:
			raw, used, err := consumeProtoBytes(data)
			if err != nil {
				return err
			}
			if err := UnmarshalBlockProtobuf(raw, &out.Block); err != nil {
				return err
			}
			data = data[used:]
		case 5:
			v, used, err := consumeProtoString(data)
			if err != nil {
				return err
			}
			out.ValidatorSet = append(out.ValidatorSet, v)
			data = data[used:]
		default:
			used, err := skipProtoField(num, typ, data)
			if err != nil {
				return err
			}
			data = data[used:]
		}
	}
	out.BlockHeader = buildBlockFileHeader(out.Block, out.Height)
	out.Transactions = append([]Transaction{}, out.Block.Transactions...)
	*record = out
	return nil
}

func MarshalSnapshotBinary(snapshot *StateSnapshot) ([]byte, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot unavailable")
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}
	defer enc.Close()
	compressed := enc.EncodeAll(raw, nil)
	var body []byte
	body = appendProtoVarint(body, 1, wireCodecSnapshotVersion)
	body = appendProtoBytes(body, 2, compressed)
	return append([]byte(snapshotBinaryMagic), body...), nil
}

func UnmarshalSnapshotBinary(payload []byte, snapshot *StateSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("nil snapshot")
	}
	if !bytes.HasPrefix(payload, []byte(snapshotBinaryMagic)) {
		return json.Unmarshal(payload, snapshot)
	}
	data := payload[len(snapshotBinaryMagic):]
	var compressed []byte
	for len(data) > 0 {
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 {
			return protowire.ParseError(n)
		}
		data = data[n:]
		switch num {
		case 1:
			if typ != protowire.VarintType {
				return fmt.Errorf("snapshot version wire type %v", typ)
			}
			_, used, err := consumeProtoVarint(data)
			if err != nil {
				return err
			}
			data = data[used:]
		case 2:
			if typ != protowire.BytesType {
				return fmt.Errorf("snapshot payload wire type %v", typ)
			}
			value, used, err := consumeProtoBytes(data)
			if err != nil {
				return err
			}
			compressed = append(compressed[:0], value...)
			data = data[used:]
		default:
			used, err := skipProtoField(num, typ, data)
			if err != nil {
				return err
			}
			data = data[used:]
		}
	}
	if len(compressed) == 0 {
		return fmt.Errorf("snapshot binary payload missing")
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		return err
	}
	defer dec.Close()
	raw, err := dec.DecodeAll(compressed, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, snapshot)
}
