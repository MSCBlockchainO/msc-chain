# MSC Snapshot Bootstrap

MSC nodes should not require manual database copies when they join the network.
The production bootstrap path is:

1. A healthy validator/full node exports a verified recovery snapshot.
2. The snapshot is packaged into a static mirror artifact with `latest.json`.
3. New nodes download `latest.json`, verify `latest.json.sig` when a trusted
   public key is configured, verify the artifact SHA-256, verify the native MSC
   backup manifest, import/apply the snapshot, then continue P2P sync.

Identity material is never included in the snapshot artifact. The native backup
format contains `snapshot.json`, `snapshot_manifest.json`,
`backup_manifest.json`, and optional storage/checkpoint metadata. Files such as
`validator.sec`, MPC shares, `p2p_identity.key`, wallet files, fingerprints, and
secure backups are not copied.

## Publish

On a healthy source node:

```bash
bash scripts/publish_msc_snapshot.sh \
  --id A \
  --datadir runtime-data/distributed/A \
  --binary ./msc-node \
  --mirror-dir /var/www/msc-snapshots \
  --signing-key /etc/msc/snapshot_signing_key.pem
```

To install a periodic publisher:

```bash
sudo bash scripts/install_msc_snapshot_publisher.sh \
  --id A \
  --datadir /home/ubuntu/msc-chain/runtime-data/distributed/A \
  --binary /home/ubuntu/msc-chain/msc-node \
  --mirror-dir /var/www/msc-snapshots \
  --signing-key /etc/msc/snapshot_signing_key.pem \
  --interval 15min
```

The mirror directory can be served by nginx or synced to S3/CDN. If publishing to
S3 directly, pass `--s3-uri s3://bucket/prefix`.

If the source validator should push a local mirror to a public web node, install
the web sync timer:

```bash
sudo bash scripts/install_msc_snapshot_web_sync.sh \
  --mirror-dir /home/ubuntu/msc-chain/runtime-data/snapshot-mirror \
  --target ubuntu@web.example.com:/var/www/msc-ui/snapshots \
  --ssh-key /home/ubuntu/.ssh/msc_snapshot_publish_ed25519 \
  --public-key /home/ubuntu/.msc/snapshot_pubkey.pem \
  --interval 2min
```

## Bootstrap

On a new node before first start:

```bash
bash scripts/bootstrap_msc_snapshot.sh \
  --id NODE1 \
  --datadir /home/ubuntu/.msc/nodes/NODE1/data \
  --binary /home/ubuntu/.msc/nodes/NODE1/msc-node \
  --config-dir /home/ubuntu/.msc/nodes/NODE1 \
  --mirror-url https://snapshots.example.com/msc/ \
  --trusted-public-key /usr/local/share/msc/snapshot_pubkey.pem \
  --require-signature
```

The installer can do this automatically:

```bash
curl -fsSL https://mscblockexplorer.in/install.sh | bash -s -- \
  --role full \
  --id NODE1 \
  --auto-start
```

For local builds, the node installer accepts the same bootstrap settings:

```bash
bash scripts/install_msc_node.sh \
  --node-type full \
  --id NODE1 \
  --bootnodes /ip4/BOOTNODE/tcp/7001/p2p/PEERID \
  --snapshot-url https://snapshots.example.com/msc/ \
  --snapshot-public-key /usr/local/share/msc/snapshot_pubkey.pem \
  --require-snapshot-signature \
  --snapshot-required \
  --auto-start
```

In release mode, `scripts/build_mainnet_release.ps1` can publish
`snapshot_url`, `snapshot_public_key_path`, and signature requirements into
`latest.json`. Then `install.sh` passes them automatically to
`install_msc_node.sh`. The generated `start.sh` also runs the bootstrap checker
before every node start; it skips safely when local height is already at or
above the mirror height.

Use `--snapshot-required` for production nodes where falling back to genesis sync
is not acceptable.
