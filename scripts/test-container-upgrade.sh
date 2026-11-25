#!/bin/bash
# Prove the container installer on overlayfs: do not docker-cp onto the running
# entrypoint, write the starter through the upper directory, then switch again
# by restarting onto /var/lib/nft/nft-server.next. A second container checks the
# recreate fallback keeps the label, bind mount, and published port.
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$(readlink -f "$0")" "$@"
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
installer="$root/scripts/install-container-panel.sh"
upgrade="$root/internal/server/container-upgrade.sh"
starter="$root/internal/server/panel-start.sh"
image=kids-container-upgrade-fixture
name=kids-upg-test
recreate_name=kids-upg-recreate

if grep -F ':/usr/local/sbin/nft-server' "$installer"; then
  echo "installer still targets the running entrypoint" >&2
  exit 1
fi

workdir="$(mktemp -d)"
cleanup() {
  docker rm -f "$name" "$recreate_name" >/dev/null 2>&1 || true
  docker ps -aq --filter "name=${recreate_name}.bak-before" | xargs -r docker rm -f >/dev/null 2>&1 || true
  docker images -q 'kids-panel-switch' | xargs -r docker rmi -f >/dev/null 2>&1 || true
  docker rmi -f "$image" >/dev/null 2>&1 || true
  rm -rf "$workdir"
}
trap cleanup EXIT

cat >"$workdir/Dockerfile" <<'EOF'
FROM debian:bookworm-slim
RUN cp /bin/sleep /usr/local/sbin/nft-server \
 && chmod 755 /usr/local/sbin/nft-server \
 && rm -f /bin/bash /usr/bin/bash \
 && test ! -x /bin/bash \
 && test ! -x /usr/bin/bash
ENTRYPOINT ["/usr/local/sbin/nft-server"]
CMD ["3600"]
EOF

echo "build fixture image"
docker build -t "$image" "$workdir"

cat >"$workdir/panel-v1" <<'EOF'
#!/bin/bash
# panel-slot-v1
mkdir -p /var/lib/nft
printf 'v1\n' > /var/lib/nft/ran-version
exec sleep 3600
EOF
cat >"$workdir/panel-v2" <<'EOF'
#!/bin/bash
# panel-slot-v2
mkdir -p /var/lib/nft
printf 'v2\n' > /var/lib/nft/ran-version
exec sleep 3600
EOF
chmod 755 "$workdir/panel-v1" "$workdir/panel-v2"

docker rm -f "$name" >/dev/null 2>&1 || true
docker run -d --name "$name" "$image" >/dev/null
sleep 1

set +e
docker cp "$workdir/panel-v1" "$name:/usr/local/sbin/nft-server" >"$workdir/cp.out" 2>&1
cp_rc=$?
set -e
echo "docker cp onto running entrypoint rc=$cp_rc"
cat "$workdir/cp.out" || true
if [[ "$cp_rc" -eq 0 ]]; then
  echo "docker cp replaced the entrypoint; recreating the fixture so the inode stays busy"
  docker rm -f "$name" >/dev/null
  docker run -d --name "$name" "$image" >/dev/null
  sleep 1
fi

id_before="$(docker inspect -f '{{.Id}}' "$name")"
echo "install via upper directory"
NFT_INSTALL_LOCAL=1 \
NFT_VERSION=v0.15.4 \
NFT_WORK="$workdir/install-upper" \
NFT_SERVER_BIN="$workdir/panel-v1" \
NFT_UPGRADE_FILE="$upgrade" \
NFT_STARTER_FILE="$starter" \
NFT_EXPECT_FILE=/var/lib/nft/ran-version \
NFT_EXPECT_TEXT=v1 \
bash "$installer" "$name" | tee "$workdir/upper.log"

grep -q '容器里没有 bash' "$workdir/upper.log"
grep -q 'KIDS_SWITCH=upper' "$workdir/upper.log"
id_after="$(docker inspect -f '{{.Id}}' "$name")"
if [[ "$id_before" != "$id_after" ]]; then
  echo "upper install changed container id" >&2
  exit 1
fi

docker exec "$name" sh -c 'head -n 1 /usr/local/sbin/nft-server' | grep -q bash
docker exec "$name" grep -q '# kids-panel-start' /usr/local/sbin/nft-server
docker exec "$name" grep -q v1 /var/lib/nft/ran-version
docker exec "$name" sh -c '
  f=/usr/local/sbin/nft-upgrade
  test -f "$f"
  test "$(wc -c < "$f")" -ge 80
  head -n 1 "$f" | grep -q "^#!"
  grep -q do_update "$f"
  grep -q nft-server "$f"
'

echo "second upgrade through installer next path"
NFT_INSTALL_LOCAL=1 \
NFT_VERSION=v0.16.0 \
NFT_WORK="$workdir/install-next" \
NFT_SERVER_BIN="$workdir/panel-v2" \
NFT_UPGRADE_FILE="$upgrade" \
NFT_STARTER_FILE="$starter" \
NFT_EXPECT_FILE=/var/lib/nft/ran-version \
NFT_EXPECT_TEXT=v2 \
bash "$installer" "$name" | tee "$workdir/next.log"
grep -q 'KIDS_SWITCH=next' "$workdir/next.log"
if [[ "$(docker inspect -f '{{.Id}}' "$name")" != "$id_after" ]]; then
  echo "next install changed container id" >&2
  exit 1
fi
ok=0
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  if docker exec "$name" grep -q v2 /var/lib/nft/ran-version 2>/dev/null; then
    ok=1
    break
  fi
  sleep 1
done
if [[ "$ok" -ne 1 ]]; then
  echo "restart did not switch to v2" >&2
  docker logs "$name" --tail 40 >&2 || true
  exit 1
fi
if [[ "$(docker inspect -f '{{.Id}}' "$name")" != "$id_after" ]]; then
  echo "restart changed container id" >&2
  exit 1
fi
docker exec "$name" grep -q '# kids-panel-start' /usr/local/sbin/nft-server
docker exec "$name" grep -q panel-slot-v1 /usr/local/lib/kids/nft-server
docker exec "$name" sh -c 'grep -q panel-slot-v2 /usr/local/lib/kids/nft-server.a || grep -q panel-slot-v2 /usr/local/lib/kids/nft-server.b'

echo "recreate fallback"
mnt="$workdir/mnt"
mkdir -p "$mnt"
port="$((20000 + RANDOM % 20000))"
docker rm -f "$recreate_name" >/dev/null 2>&1 || true
docker run -d --name "$recreate_name" \
  --label kids.test=keep \
  -v "$mnt:/data" \
  -p "127.0.0.1:${port}:7788" \
  "$image" >/dev/null
sleep 1
id_old="$(docker inspect -f '{{.Id}}' "$recreate_name")"
NFT_INSTALL_LOCAL=1 \
NFT_FORCE_RECREATE=1 \
NFT_VERSION=v0.15.4 \
NFT_WORK="$workdir/install-recreate" \
NFT_SERVER_BIN="$workdir/panel-v1" \
NFT_UPGRADE_FILE="$upgrade" \
NFT_STARTER_FILE="$starter" \
NFT_EXPECT_FILE=/var/lib/nft/ran-version \
NFT_EXPECT_TEXT=v1 \
bash "$installer" "$recreate_name" | tee "$workdir/recreate.log"

grep -q 'KIDS_SWITCH=recreate' "$workdir/recreate.log"
id_new="$(docker inspect -f '{{.Id}}' "$recreate_name")"
if [[ "$id_old" == "$id_new" ]]; then
  echo "recreate kept the same container id" >&2
  exit 1
fi
docker inspect -f '{{index .Config.Labels "kids.test"}}' "$recreate_name" | grep -qx keep
docker inspect -f '{{range .Mounts}}{{.Destination}}{{"\n"}}{{end}}' "$recreate_name" | grep -qx /data
docker port "$recreate_name" 7788/tcp | grep -q "$port"
docker inspect -f '{{index .Config.Entrypoint 0}}' "$recreate_name" | grep -qx /usr/local/lib/kids/panel-start.sh
docker exec "$recreate_name" grep -q v1 /var/lib/nft/ran-version
docker ps -a --format '{{.Names}}' | grep -q "${recreate_name}.bak-before"

echo "container upgrade test ok"
