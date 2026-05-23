package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
)

type dtlTestSigner struct {
	Address string
	PubHex  string
	Priv    ed25519.PrivateKey
}

func newDTLTestSigner(t *testing.T) dtlTestSigner {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test signer: %v", err)
	}
	return dtlTestSigner{
		Address: AddressFromPublicKey(pub),
		PubHex:  hex.EncodeToString(pub),
		Priv:    priv,
	}
}

func newDTLTestSigners(t *testing.T, n int) []dtlTestSigner {
	t.Helper()
	if n <= 0 {
		return nil
	}
	out := make([]dtlTestSigner, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, newDTLTestSigner(t))
	}
	return out
}

func buildDTLCertForSigners(
	t *testing.T,
	tokenID string,
	epoch uint64,
	action DTLGovernanceAction,
	actionPayloadHash string,
	signers []dtlTestSigner,
) DTLGovernanceCert {
	t.Helper()
	cert := DTLGovernanceCert{
		TokenID:           tokenID,
		Epoch:             epoch,
		Action:            action,
		ActionPayloadHash: actionPayloadHash,
		Signers:           make([]string, 0, len(signers)),
		SignerPublicKeys:  make([]string, 0, len(signers)),
		Signatures:        make([]string, 0, len(signers)),
	}
	signBytes := DTLGovernanceCertSignBytes(tokenID, epoch, action, actionPayloadHash)
	for _, signer := range signers {
		cert.Signers = append(cert.Signers, signer.Address)
		cert.SignerPublicKeys = append(cert.SignerPublicKeys, signer.PubHex)
		sig := ed25519.Sign(signer.Priv, signBytes)
		cert.Signatures = append(cert.Signatures, hex.EncodeToString(sig))
	}
	return cert
}

func dtlSignerAddresses(signers []dtlTestSigner) []string {
	out := make([]string, 0, len(signers))
	for _, signer := range signers {
		out = append(out, signer.Address)
	}
	return out
}
