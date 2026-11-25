#!/bin/sh
# Map container env to nft-server flags, then replace this shell.
# Extra arguments are appended, so a one-shot reset still works:
#   nft-server --reset-admin-password 'new-password'
set -eu

addr="${NFT_ADDR:-:7788}"
db="${NFT_DB:-/var/lib/nft/panel.db}"
mkdir -p "$(dirname "$db")"

if [ -n "${NFT_BOOTSTRAP_ADMIN_PASSWORD:-}" ]; then
  exec /usr/local/bin/nft-server \
    --addr "$addr" \
    --db "$db" \
    --bootstrap-admin-password "$NFT_BOOTSTRAP_ADMIN_PASSWORD" \
    "$@"
fi

exec /usr/local/bin/nft-server --addr "$addr" --db "$db" "$@"
