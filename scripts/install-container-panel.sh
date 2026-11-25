#!/bin/bash
# Install kids panel v0.15.3 into a running container without unlinking the
# executed nft-server. overlayfs and docker cp both return
# "device or resource busy" for that file. This script writes the new ELF to a
# free path, then replaces the entrypoint in the container upper directory
# while the container is stopped.
set -eu
name="${1:-kids}"
ver=v0.15.3
work="/tmp/kids-$name-$ver"
proxy=""
if [[ -f /etc/nft/gh-proxy ]]; then
  proxy="$(tr -d ' \t\r\n' </etc/nft/gh-proxy || true)"
fi
case "$proxy" in
  http://*|https://*)
    case "$proxy" in
      */) ;;
      *) proxy="${proxy}/" ;;
    esac
    ;;
  *) proxy="" ;;
esac
base="${proxy}https://github.com/cheesydui-cloud/kids/releases/download/${ver}"
rm -rf "$work"
mkdir -p "$work"
cd "$work"
echo "下载 ${ver}"
curl -fL --retry 3 --retry-delay 2 -o SHA256SUMS "$base/SHA256SUMS"
curl -fL --retry 3 --retry-delay 2 -o nft-upgrade "$base/nft-upgrade"
curl -fL --retry 3 --retry-delay 2 -o panel-start.sh "$base/panel-start.sh"
arch=amd64
if docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null | grep -q true; then
  machine="$(docker exec "$name" uname -m 2>/dev/null || uname -m)"
else
  machine="$(uname -m)"
fi
case "$machine" in
  aarch64|arm64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *) echo "不支持的架构: $machine"; exit 1 ;;
esac
curl -fL --retry 3 --retry-delay 2 -o "nft-server-linux-$arch" "$base/nft-server-linux-$arch"
grep " nft-server-linux-$arch$" SHA256SUMS | sha256sum -c -
chmod 755 "nft-server-linux-$arch" nft-upgrade panel-start.sh
grep -q '# kids-panel-start' panel-start.sh
echo "已校验 nft-server-linux-$arch"

if ! docker inspect "$name" >/dev/null 2>&1; then
  echo "找不到容器 $name"
  exit 1
fi
upper="$(docker inspect -f '{{.GraphDriver.Data.UpperDir}}' "$name")"
if [[ -z "$upper" || ! -d "$upper" ]]; then
  echo "找不到容器上层目录。GraphDriver:"
  docker inspect -f '{{json .GraphDriver}}' "$name" || true
  exit 1
fi
echo "上层目录 $upper"

running=0
if docker inspect -f '{{.State.Running}}' "$name" | grep -q true; then
  running=1
  docker exec "$name" mkdir -p /usr/local/lib/kids /var/lib/nft
  docker cp "$work/nft-server-linux-$arch" "$name:/usr/local/lib/kids/nft-server"
  docker exec "$name" chmod 755 /usr/local/lib/kids/nft-server
  printf '%s\n' /usr/local/lib/kids/nft-server | docker exec -i "$name" sh -c 'cat > /var/lib/nft/nft-server.live'
fi

echo "停止 $name"
docker stop "$name" >/dev/null
started=0
cleanup() {
  if [[ "$started" -eq 0 ]]; then
    docker start "$name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

write_upper() {
  local src="$1" dest="$2"
  mkdir -p "$(dirname "$dest")"
  if [[ -d "$dest" ]]; then
    echo "上层路径是目录: $dest"
    exit 1
  fi
  if [[ -e "$dest" && ! -f "$dest" ]]; then
    rm -f "$dest" || true
  fi
  if [[ -f "$dest" ]]; then
    if ! cat "$src" >"$dest"; then
      rm -f "$dest" || true
      cp "$src" "$dest"
    fi
  else
    cp "$src" "$dest"
  fi
}
mkdir -p "$upper/usr/local/lib/kids" "$upper/usr/local/sbin" "$upper/var/lib/nft"
write_upper "$work/nft-server-linux-$arch" "$upper/usr/local/lib/kids/nft-server"
chmod 755 "$upper/usr/local/lib/kids/nft-server"
printf '%s\n' /usr/local/lib/kids/nft-server >"$upper/var/lib/nft/nft-server.live"
# docker cp RemoveAll hits EBUSY on the executed entrypoint. The upper
# directory is a normal host directory, so overwrite it here.
bak="$upper/usr/local/sbin/nft-server.bak-before-v0153"
if [[ -f "$upper/usr/local/sbin/nft-server" && ! -f "$bak" ]]; then
  cp -a "$upper/usr/local/sbin/nft-server" "$bak" || true
fi
write_upper "$work/panel-start.sh" "$upper/usr/local/sbin/nft-server"
chmod 755 "$upper/usr/local/sbin/nft-server"
write_upper "$work/nft-upgrade" "$upper/usr/local/sbin/nft-upgrade"
chmod 755 "$upper/usr/local/sbin/nft-upgrade"
head -1 "$upper/usr/local/sbin/nft-server" | grep -q bash
sync || true

echo "启动 $name"
docker start "$name" >/dev/null
started=1
trap - EXIT
sleep 3
echo "--- 启动后 ---"
docker exec "$name" sh -c 'echo cmdline:$(tr "\0" " " < /proc/1/cmdline); echo; echo starter:$(head -1 /usr/local/sbin/nft-server); ls -l /proc/1/exe; grep -a -o v0.15.3 /proc/1/exe | head -1'
curl -fsS --max-time 8 http://127.0.0.1:7788/healthz && echo || echo "7788 暂未响应。执行: docker logs $name --tail 40"
echo "完成。强制刷新面板，侧边栏应显示 $ver"
