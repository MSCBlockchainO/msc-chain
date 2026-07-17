package main

import "strings"

// protocolChainIDValue is part of the wire and execution protocol. Local
// configuration may describe a chain, but it must never alter replay,
// signature, address, committee, or peer-handshake authority.
const protocolChainIDValue = "91938"

// protocolChainID returns the immutable chain identity used by consensus and
// DTL. Keep presentation/configuration reads of ChainID out of this helper.
func protocolChainID() string {
	return protocolChainIDValue
}

func isProtocolChainID(chainID string) bool {
	return strings.TrimSpace(chainID) == protocolChainID()
}
