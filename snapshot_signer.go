package main

import (
	"crypto/ed25519"
	"fmt"
	"strings"
)

type SnapshotSigner struct {
	// `Node` stores the value associated with this record.
	Node *Node
}

// SignSnapshot signs snapshot.
func SignSnapshot(root []byte, priv ed25519.PrivateKey) []byte {
	if len(root) == 0 || len(priv) != ed25519.PrivateKeySize {
		return nil
	}
	// `payload` stores the value produced by this operation.
	payload := append([]byte(nil), root...)
	return ed25519.Sign(priv, payload)
}

// Publish implements the publish helper.
func (s SnapshotSigner) Publish(reason string, force bool) (*StateSnapshot, error) {
	if s.Node == nil {
		return nil, fmt.Errorf("snapshot signer unavailable")
	}
	if strings.TrimSpace(reason) == "" {
		reason = "snapshot_signer"
	}
	return s.Node.publishRequiredValidatorSnapshot(reason, force)
}
