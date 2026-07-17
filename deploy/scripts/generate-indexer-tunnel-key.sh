#!/usr/bin/env bash
set -euo pipefail

key="${HOME}/.ssh/msc_indexer_to_gateway_ed25519"
if [[ ! -f "${key}" ]]; then
  ssh-keygen -q -t ed25519 -N "" -C "msc-indexer-to-gateway" -f "${key}"
fi

chmod 600 "${key}"
chmod 644 "${key}.pub"
ssh-keygen -lf "${key}.pub"
