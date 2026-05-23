package main

import (
	"crypto/ed25519"
	"fmt"
	"strings"
)

type SnapshotSigner struct {
	Node *Node
}

func SignSnapshot(root []byte, priv ed25519.PrivateKey) []byte {
	if len(root) == 0 || len(priv) != ed25519.PrivateKeySize {
		return nil
	}
	payload := append([]byte(nil), root...)
	return ed25519.Sign(priv, payload)
}

func (s SnapshotSigner) Publish(reason string, force bool) (*StateSnapshot, error) {
	if s.Node == nil {
		return nil, fmt.Errorf("snapshot signer unavailable")
	}
	if strings.TrimSpace(reason) == "" {
		reason = "snapshot_signer"
	}
	return s.Node.publishRequiredValidatorSnapshot(reason, force)
}
