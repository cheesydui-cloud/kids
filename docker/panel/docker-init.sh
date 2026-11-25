#!/bin/bash
# kids-docker-init
# Fix volume ownership, then run the panel as the unprivileged kids user.
# The long-running process is the program exec'd by panel-start.sh, not this file.
set -eu

mkdir -p /var/lib/nft /usr/local/lib/kids
if id kids >/dev/null 2>&1; then
  chown -R kids:kids /var/lib/nft /usr/local/lib/kids
fi
if [ -f /var/lib/nft/bootstrap-admin-password ]; then
  chmod 600 /var/lib/nft/bootstrap-admin-password 2>/dev/null || true
  if id kids >/dev/null 2>&1; then
    chown kids:kids /var/lib/nft/bootstrap-admin-password 2>/dev/null || true
  fi
fi
if [ ! -s /var/lib/nft/nft-server.live ]; then
  printf '%s\n' /usr/local/lib/kids/nft-server > /var/lib/nft/nft-server.live
  chown kids:kids /var/lib/nft/nft-server.live 2>/dev/null || true
fi

addr="${NFT_ADDR:-:7788}"
db="${NFT_DB:-/var/lib/nft/panel.db}"
if id kids >/dev/null 2>&1 && command -v setpriv >/dev/null 2>&1; then
  exec setpriv --reuid=kids --regid=kids --init-groups \
    /usr/local/lib/kids/panel-start.sh --addr "$addr" --db "$db" "$@"
fi
exec /usr/local/lib/kids/panel-start.sh --addr "$addr" --db "$db" "$@"
