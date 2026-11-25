#!/bin/sh
# Download the official nft-server release and verify it before it is copied
# into the image. Called only at image build time.
set -eu

version="${VERSION:-latest}"
proxy="${GH_PROXY:-}"
arch="${TARGETARCH:-}"

if [ -z "$arch" ]; then
  case "$(uname -m)" in
    x86_64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *)
      echo "cannot detect architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
fi

case "$proxy" in
  "") ;;
  */) ;;
  *) proxy="${proxy}/" ;;
esac

case "$version" in
  latest)
    base="${proxy}https://github.com/cheesydui-cloud/kids/releases/latest/download"
    ;;
  *)
    base="${proxy}https://github.com/cheesydui-cloud/kids/releases/download/${version}"
    ;;
esac

case "$arch" in
  amd64) candidates="nft-server-linux-amd64 nft-server" ;;
  arm64) candidates="nft-server-linux-arm64" ;;
  *)
    echo "unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

dest=/usr/local/bin/nft-server
tmp=/tmp/nft-server.part
sums=/tmp/SHA256SUMS
local_bin=/tmp/kids/local/nft-server
rm -f "$dest" "$tmp" "$sums"

# Offline build: put the official nft-server binary at docker/panel/local/nft-server.
if [ -s "$local_bin" ]; then
  size=$(wc -c < "$local_bin" | tr -d ' ')
  magic=$(od -An -tx1 -N4 "$local_bin" | tr -d ' \n')
  if [ "$size" -lt 1048576 ] || [ "$magic" != "7f454c46" ]; then
    echo "docker/panel/local/nft-server is not a release binary (${size} bytes, magic ${magic})" >&2
    exit 1
  fi
  cp "$local_bin" "$dest"
  chmod 0755 "$dest"
  echo "installed local nft-server (${size} bytes)"
  exit 0
fi

echo "fetching SHA256SUMS from ${base}"
curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 \
  "${base}/SHA256SUMS" -o "$sums"

ok=0
for name in $candidates; do
  echo "trying ${name}"
  rm -f "$tmp"
  if ! curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 \
      "${base}/${name}" -o "$tmp"; then
    echo "download failed: ${name}" >&2
    continue
  fi
  size=$(wc -c < "$tmp" | tr -d ' ')
  if [ "$size" -lt 1048576 ]; then
    echo "${name} is too small (${size} bytes), not a release binary" >&2
    continue
  fi
  magic=$(od -An -tx1 -N4 "$tmp" | tr -d ' \n')
  if [ "$magic" != "7f454c46" ]; then
    echo "${name} is not an ELF binary (magic ${magic})" >&2
    continue
  fi
  expected=$(awk -v a="$name" '$2 == a { print $1; exit }' "$sums")
  if [ -z "$expected" ]; then
    echo "SHA256SUMS has no entry for ${name}" >&2
    exit 1
  fi
  actual=$(sha256sum "$tmp" | awk '{print $1}')
  if [ "$actual" != "$expected" ]; then
    echo "sha256 mismatch for ${name}" >&2
    echo "expected ${expected}" >&2
    echo "actual   ${actual}" >&2
    exit 1
  fi
  mv "$tmp" "$dest"
  chmod 0755 "$dest"
  echo "installed ${name} ${actual}"
  ok=1
  break
done

rm -f "$tmp" "$sums"
if [ "$ok" -ne 1 ]; then
  echo "failed to install nft-server for ${arch} from ${base}" >&2
  exit 1
fi
