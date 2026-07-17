package main

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
)

type SnapshotChunkDownloader struct {
	// `Node` stores the value associated with this record.
	Node      *Node
	// `Manifest` stores the value associated with this record.
	Manifest  *SnapshotManifest
	// `Primary` stores the value associated with this record.
	Primary   peer.ID
	// `Providers` stores the value associated with this record.
	Providers []peer.ID
	// `MinHeight` stores the value associated with this record.
	MinHeight uint64
}

// NewSnapshotChunkDownloader creates a new snapshot chunk downloader.
func (n *Node) NewSnapshotChunkDownloader(manifest *SnapshotManifest, primary peer.ID, minHeight uint64) *SnapshotChunkDownloader {
	return &SnapshotChunkDownloader{
		Node:      n,
		Manifest:  manifest,
		Primary:   primary,
		MinHeight: minHeight,
	}
}

// Download implements the download helper.
func (d *SnapshotChunkDownloader) Download() (*StateSnapshot, error) {
	if d == nil || d.Node == nil {
		return nil, fmt.Errorf("snapshot chunk downloader unavailable")
	}
	if d.Manifest == nil {
		return nil, fmt.Errorf("snapshot manifest unavailable")
	}
	// `providers` stores the value produced by this operation.
	providers := d.Providers
	if len(providers) == 0 {
		// `peers` stores the value produced by this operation.
		peers := []peer.ID(nil)
		if d.Node.Host != nil {
			peers = d.Node.Host.Network().Peers()
		}
		providers = d.Node.snapshotProvidersForManifest(d.Manifest, d.Primary, peers)
	}
	if len(providers) == 0 {
		return nil, fmt.Errorf("snapshot providers unavailable")
	}
	return d.Node.fetchSnapshotFromManifestChunks(d.Manifest, providers, d.MinHeight)
}
