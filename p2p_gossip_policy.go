package main

import (
	"context"
	"runtime"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
)

// newMSCGossipSub centralizes the production gossip policy so integration
// tests exercise the same signed, peer-exchange-enabled multi-hop transport.
func newMSCGossipSub(ctx context.Context, h host.Host, heartbeat time.Duration) (*pubsub.PubSub, error) {
	params := pubsub.DefaultGossipSubParams()
	if heartbeat <= 0 {
		heartbeat = 5 * time.Second
	}
	params.HeartbeatInterval = heartbeat

	validateWorkers := runtime.NumCPU()
	if validateWorkers < 2 {
		validateWorkers = 2
	}
	if MaxValidateWorkers > 0 && validateWorkers > MaxValidateWorkers {
		validateWorkers = MaxValidateWorkers
	}
	validateQueue := MaxValidateQueue
	if validateQueue <= 0 {
		validateQueue = 128
	}
	validateThrottle := validateWorkers * 4
	if validateThrottle < validateQueue {
		validateThrottle = validateQueue
	}
	peerOutboundQueue := MaxPeerOutboundQueue
	if peerOutboundQueue <= 0 {
		peerOutboundQueue = 512
	}

	return pubsub.NewGossipSub(
		ctx,
		h,
		pubsub.WithMessageSignaturePolicy(pubsub.StrictSign),
		pubsub.WithPeerExchange(true),
		pubsub.WithMaxMessageSize(10<<20),
		pubsub.WithGossipSubParams(params),
		pubsub.WithValidateWorkers(validateWorkers),
		pubsub.WithValidateQueueSize(validateQueue),
		pubsub.WithValidateThrottle(validateThrottle),
		pubsub.WithPeerOutboundQueueSize(peerOutboundQueue),
	)
}
