package main

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
)

type SnapshotChunkDownloader struct {
	Node      *Node
	Manifest  *SnapshotManifest
	Primary   peer.ID
	Providers []peer.ID
	MinHeight uint64
}

func (n *Node) NewSnapshotChunkDownloader(manifest *SnapshotManifest, primary peer.ID, minHeight uint64) *SnapshotChunkDownloader {
	return &SnapshotChunkDownloader{
		Node:      n,
		Manifest:  manifest,
		Primary:   primary,
		MinHeight: minHeight,
	}
}

func (d *SnapshotChunkDownloader) Download() (*StateSnapshot, error) {
	if d == nil || d.Node == nil {
		return nil, fmt.Errorf("snapshot chunk downloader unavailable")
	}
	if d.Manifest == nil {
		return nil, fmt.Errorf("snapshot manifest unavailable")
	}
	providers := d.Providers
	if len(providers) == 0 {
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
