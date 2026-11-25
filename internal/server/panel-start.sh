#!/bin/bash
# kids-panel-start
# Container entrypoint. overlayfs returns "device or resource busy" when the
# running nft-server is unlinked or its directory is renamed. Upgrades therefore
# leave the new ELF at /var/lib/nft/nft-server.next. This script copies that
# file onto a slot that is not the running inode, then execs the slot.
set -u
state_dir=/var/lib/nft
slot_dir=/usr/local/lib/kids
next="$state_dir/nft-server.next"
live="$state_dir/nft-server.live"
fallback="$slot_dir/nft-server"

read_live() {
  local p
  p="$(tr -d '\r\n' <"$live" 2>/dev/null || true)"
  if [[ -n "$p" && -x "$p" ]]; then
    printf '%s\n' "$p"
    return 0
  fi
  if [[ -x "$fallback" ]]; then
    printf '%s\n' "$fallback"
    return 0
  fi
  return 1
}

current="$(read_live || true)"
if [[ -f "$next" ]]; then
  mkdir -p "$slot_dir" "$state_dir"
  target="$slot_dir/nft-server.a"
  if [[ "$current" == "$target" ]]; then
    target="$slot_dir/nft-server.b"
  fi
  tmp="${target}.tmp.$$"
  if cp "$next" "$tmp" && chmod 755 "$tmp" && mv -f "$tmp" "$target"; then
    printf '%s\n' "$target" >"$live"
    rm -f "$next" || true
    current="$target"
  else
    rm -f "$tmp" || true
    if cp "$next" "$target" && chmod 755 "$target"; then
      printf '%s\n' "$target" >"$live"
      rm -f "$next" || true
      current="$target"
    elif [[ -x "$next" ]]; then
      current="$next"
    fi
  fi
fi
if [[ -z "${current:-}" || ! -x "$current" ]]; then
  echo "kids: 找不到面板程序" >&2
  exit 1
fi
exec "$current" "$@"
