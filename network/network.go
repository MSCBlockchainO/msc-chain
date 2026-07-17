// Package network exposes transport capabilities without leaking concrete
// libp2p or Node runtime types into consensus or execution.
package network

import "context"

type Message struct {
	Topic   string
	Payload []byte
}

type Broadcaster interface {
	Broadcast(context.Context, Message) error
}

type PeerSource interface {
	PeerIDs() []string
}
