package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"
)

const dtlBytecodeHeaderLen = 16

func normalizeDTLBytecodeFormat(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

func trimHexPrefix(raw string) string {
	v := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(v), "0x") {
		return v[2:]
	}
	return v
}

func decodeDTLBytecodeHex(raw string) ([]byte, error) {
	trimmed := trimHexPrefix(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("dtl: missing bytecode")
	}
	if len(trimmed)%2 != 0 {
		return nil, fmt.Errorf("dtl: bytecode hex length must be even")
	}
	data, err := hex.DecodeString(trimmed)
	if err != nil {
		return nil, fmt.Errorf("dtl: invalid bytecode hex")
	}
	return data, nil
}

func encodeDTLBytecodeHex(data []byte) string {
	return strings.ToLower(hex.EncodeToString(data))
}

func dtlBCInstrToLogicOp(in DTLBCInstr) DTLLogicPackOp {
	return DTLLogicPackOp{
		Op:               in.Op,
		Dest:             in.Dest,
		A:                in.A,
		B:                in.B,
		Src:              in.Src,
		Cond:             in.Cond,
		Key:              in.Key,
		Arg:              in.Arg,
		TokenID:          in.TokenID,
		TokenArg:         in.TokenArg,
		ToArg:            in.ToArg,
		AmountArg:        in.AmountArg,
		FromArg:          in.FromArg,
		SpenderArg:       in.SpenderArg,
		NameArg:          in.NameArg,
		SymbolArg:        in.SymbolArg,
		DecimalsArg:      in.DecimalsArg,
		MaxSupplyArg:     in.MaxSupplyArg,
		InitialSupplyArg: in.InitialSupplyArg,
		From:             in.From,
		Message:          in.Message,
		Target:           in.Target,
		Map:              in.Map,
		MapKeyArg:        in.MapKeyArg,
		Topic0Arg:        in.Topic0Arg,
		Topic1Arg:        in.Topic1Arg,
		Topic2Arg:        in.Topic2Arg,
		Topic3Arg:        in.Topic3Arg,
		DataArg:          in.DataArg,
	}
}

func dtlLogicOpToBCInstr(op DTLLogicPackOp) DTLBCInstr {
	return DTLBCInstr{
		Op:               op.Op,
		Dest:             op.Dest,
		A:                op.A,
		B:                op.B,
		Src:              op.Src,
		Cond:             op.Cond,
		Key:              op.Key,
		Arg:              op.Arg,
		TokenID:          op.TokenID,
		TokenArg:         op.TokenArg,
		ToArg:            op.ToArg,
		AmountArg:        op.AmountArg,
		FromArg:          op.FromArg,
		SpenderArg:       op.SpenderArg,
		NameArg:          op.NameArg,
		SymbolArg:        op.SymbolArg,
		DecimalsArg:      op.DecimalsArg,
		MaxSupplyArg:     op.MaxSupplyArg,
		InitialSupplyArg: op.InitialSupplyArg,
		From:             op.From,
		Message:          op.Message,
		Target:           op.Target,
		Map:              op.Map,
		MapKeyArg:        op.MapKeyArg,
		Topic0Arg:        op.Topic0Arg,
		Topic1Arg:        op.Topic1Arg,
		Topic2Arg:        op.Topic2Arg,
		Topic3Arg:        op.Topic3Arg,
		DataArg:          op.DataArg,
	}
}

func dtlBytecodeProgramToLogicPack(program *DTLBytecodeProgram) *DTLLogicPack {
	if program == nil {
		return nil
	}
	pack := &DTLLogicPack{
		Version: DTLLogicPackVersionV3,
		Name:    strings.TrimSpace(program.Name),
		ABI:     append([]DTLLogicPackABIMethod(nil), program.ABI...),
		Storage: append([]DTLLogicPackStorageField(nil), program.Storage...),
		Limits:  program.Limits,
		Methods: make([]DTLLogicPackMethod, 0, len(program.Methods)),
	}
	for _, method := range program.Methods {
		ops := make([]DTLLogicPackOp, 0, len(method.Code))
		for _, instr := range method.Code {
			ops = append(ops, dtlBCInstrToLogicOp(instr))
		}
		pack.Methods = append(pack.Methods, DTLLogicPackMethod{
			Name:     strings.TrimSpace(method.Name),
			MaxSteps: method.MaxSteps,
			Ops:      ops,
		})
	}
	return pack
}

func dtlBytecodeProgramFromLogicPack(pack *DTLLogicPack) *DTLBytecodeProgram {
	if pack == nil {
		return nil
	}
	out := &DTLBytecodeProgram{
		Version: DTLBytecodeVersionV1,
		Name:    strings.TrimSpace(pack.Name),
		ABI:     append([]DTLLogicPackABIMethod(nil), pack.ABI...),
		Storage: append([]DTLLogicPackStorageField(nil), pack.Storage...),
		Limits:  pack.Limits,
		Methods: make([]DTLBytecodeMethod, 0, len(pack.Methods)),
	}
	for _, method := range pack.Methods {
		code := make([]DTLBCInstr, 0, len(method.Ops))
		for _, op := range method.Ops {
			code = append(code, dtlLogicOpToBCInstr(op))
		}
		out.Methods = append(out.Methods, DTLBytecodeMethod{
			Name:     strings.TrimSpace(method.Name),
			MaxSteps: method.MaxSteps,
			Code:     code,
		})
	}
	return out
}

func validateAndNormalizeDTLBytecodeProgram(state *DTLState, program *DTLBytecodeProgram) (*DTLBytecodeProgram, *DTLLogicPack, error) {
	if state == nil {
		return nil, nil, ErrDTLInvalidState
	}
	state.ensure()
	if program == nil {
		return nil, nil, fmt.Errorf("dtl: missing bytecode program")
	}
	pack := dtlBytecodeProgramToLogicPack(program)
	if pack == nil {
		return nil, nil, fmt.Errorf("dtl: invalid bytecode program")
	}
	normalizedPack, err := validateAndNormalizeDTLLogicPack(state, pack)
	if err != nil {
		return nil, nil, err
	}
	out := dtlBytecodeProgramFromLogicPack(normalizedPack)
	out.Version = DTLBytecodeVersionV1
	if len(out.Methods) == 0 {
		return nil, nil, fmt.Errorf("dtl: bytecode must declare at least one method")
	}
	return out, normalizedPack, nil
}

func EncodeDTLBytecode(program *DTLBytecodeProgram) (string, error) {
	if program == nil {
		return "", fmt.Errorf("dtl: missing bytecode program")
	}
	normalized := *program
	if normalized.Version == 0 {
		normalized.Version = DTLBytecodeVersionV1
	}
	if normalized.Version != DTLBytecodeVersionV1 {
		return "", fmt.Errorf("dtl: unsupported bytecode version")
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("dtl: failed to encode bytecode program")
	}
	checksum := crc32.ChecksumIEEE(payload)
	buf := &bytes.Buffer{}
	buf.WriteString(DTLBytecodeMagic)
	if err := binary.Write(buf, binary.BigEndian, normalized.Version); err != nil {
		return "", fmt.Errorf("dtl: failed to encode bytecode header")
	}
	size := uint32(len(payload))
	if err := binary.Write(buf, binary.BigEndian, size); err != nil {
		return "", fmt.Errorf("dtl: failed to encode bytecode header")
	}
	if err := binary.Write(buf, binary.BigEndian, checksum); err != nil {
		return "", fmt.Errorf("dtl: failed to encode bytecode header")
	}
	if _, err := buf.Write(payload); err != nil {
		return "", fmt.Errorf("dtl: failed to encode bytecode payload")
	}
	return encodeDTLBytecodeHex(buf.Bytes()), nil
}

func DecodeDTLBytecode(rawHex string) (*DTLBytecodeProgram, DTLBCHeader, error) {
	var header DTLBCHeader
	data, err := decodeDTLBytecodeHex(rawHex)
	if err != nil {
		return nil, header, err
	}
	if len(data) < dtlBytecodeHeaderLen {
		return nil, header, fmt.Errorf("dtl: bytecode too short")
	}
	magic := string(data[:6])
	if magic != DTLBytecodeMagic {
		return nil, header, fmt.Errorf("dtl: invalid bytecode magic")
	}
	version := binary.BigEndian.Uint16(data[6:8])
	if version != DTLBytecodeVersionV1 {
		return nil, header, fmt.Errorf("dtl: unsupported bytecode version")
	}
	size := binary.BigEndian.Uint32(data[8:12])
	checksum := binary.BigEndian.Uint32(data[12:16])
	if int(size) != len(data)-dtlBytecodeHeaderLen {
		return nil, header, fmt.Errorf("dtl: invalid bytecode payload size")
	}
	payload := data[dtlBytecodeHeaderLen:]
	if crc32.ChecksumIEEE(payload) != checksum {
		return nil, header, fmt.Errorf("dtl: bytecode checksum mismatch")
	}
	var program DTLBytecodeProgram
	if err := json.Unmarshal(payload, &program); err != nil {
		return nil, header, fmt.Errorf("dtl: invalid bytecode payload")
	}
	program.Version = version
	header = DTLBCHeader{
		Magic:       magic,
		Version:     version,
		PayloadSize: size,
		Checksum:    checksum,
	}
	return &program, header, nil
}

func HashDTLBytecode(rawHex string) (string, error) {
	data, err := decodeDTLBytecodeHex(rawHex)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func decodeNormalizeValidateDTLBytecode(state *DTLState, rawHex string) (*DTLBytecodeProgram, *DTLLogicPack, string, error) {
	program, _, err := DecodeDTLBytecode(rawHex)
	if err != nil {
		return nil, nil, "", err
	}
	normalizedProgram, normalizedPack, err := validateAndNormalizeDTLBytecodeProgram(state, program)
	if err != nil {
		return nil, nil, "", err
	}
	canonicalHex, err := EncodeDTLBytecode(normalizedProgram)
	if err != nil {
		return nil, nil, "", err
	}
	if ConfigDTLBytecodeRequireCanonical {
		incoming, err := decodeDTLBytecodeHex(rawHex)
		if err != nil {
			return nil, nil, "", err
		}
		canonicalBytes, err := decodeDTLBytecodeHex(canonicalHex)
		if err != nil {
			return nil, nil, "", err
		}
		if !bytes.Equal(incoming, canonicalBytes) {
			return nil, nil, "", fmt.Errorf("dtl: bytecode is not canonical")
		}
	}
	hash, err := HashDTLBytecode(canonicalHex)
	if err != nil {
		return nil, nil, "", err
	}
	return normalizedProgram, normalizedPack, hash, nil
}

func methodExistsInDTLBytecodeProgram(program *DTLBytecodeProgram, method string) bool {
	target := normalizeDTLContractMethodName(method)
	if target == "" || program == nil {
		return false
	}
	for _, m := range program.Methods {
		if normalizeDTLContractMethodName(m.Name) == target {
			return true
		}
	}
	return false
}
