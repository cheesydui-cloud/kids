#!/bin/bash
# Prove the official panel image: unprivileged pid 1, starter is not the
# entrypoint inode, published port is loopback, the bootstrap password is not
# in docker inspect or logs, and a later install switches through nft-server.next.
set -euo pipefail

if [[ "$(id -u)" -ne 0 ]]; then
  exec sudo -n bash "$(readlink -f "$0")" "$@"
fi

root="$(cd "$(dirname "$0")/.." && pwd)"
docker_installer="$root/scripts/install-docker-panel.sh"
container_installer="$root/scripts/install-container-panel.sh"
upgrade="$root/internal/server/container-upgrade.sh"
starter="$root/internal/server/panel-start.sh"
init="$root/docker/panel/docker-init.sh"

name=kids-docker-panel-test
volume=kids-docker-panel-test-data
image=kids-panel:docker-test
port="$((22000 + RANDOM % 20000))"
pw='kids-secret-not-in-inspect'

workdir="$(mktemp -d)"
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker volume rm -f "$volume" >/dev/null 2>&1 || true
  docker rmi -f "$image" >/dev/null 2>&1 || true
  rm -rf "$workdir"
}
trap cleanup EXIT

cat >"$workdir/panel-v1" <<'EOF'
#!/bin/bash
# panel-slot-v1
mkdir -p /var/lib/nft
printf 'v1\n' > /var/lib/nft/ran-version
printf '%s\n' "$(id -u)" > /var/lib/nft/ran-uid
exec sleep 3600
EOF
cat >"$workdir/panel-v2" <<'EOF'
#!/bin/bash
# panel-slot-v2
mkdir -p /var/lib/nft
printf 'v2\n' > /var/lib/nft/ran-version
printf '%s\n' "$(id -u)" > /var/lib/nft/ran-uid
exec sleep 3600
EOF
chmod 755 "$workdir/panel-v1" "$workdir/panel-v2"

echo "install official image"
NFT_INSTALL_LOCAL=1 \
NFT_VERSION=v0.16.0 \
NFT_PUBLISH="127.0.0.1:${port}:7788" \
NFT_VOLUME="$volume" \
NFT_IMAGE="$image" \
NFT_WORK="$workdir/install" \
NFT_SERVER_BIN="$workdir/panel-v1" \
NFT_UPGRADE_FILE="$upgrade" \
NFT_STARTER_FILE="$starter" \
NFT_INIT_FILE="$init" \
NFT_BOOTSTRAP_ADMIN_PASSWORD="$pw" \
bash "$docker_installer" "$name" | tee "$workdir/install.log"

if [[ -e "$workdir/install/bootstrap-admin-password" || -e "$workdir/install/bootstrap.env" ]]; then
  echo "installer left the password file in the work directory" >&2
  exit 1
fi

id_before="$(docker inspect -f '{{.Id}}' "$name")"
entry="$(docker inspect -f '{{index .Config.Entrypoint 0}}' "$name")"
if [[ "$entry" != "/usr/local/sbin/kids-docker-init" ]]; then
  echo "entrypoint = $entry" >&2
  exit 1
fi
docker inspect "$name" | grep -q 'no-new-privileges'
docker inspect -f '{{json .HostConfig.CapDrop}}' "$name" | grep -q ALL
bindings="$(docker inspect -f '{{json .HostConfig.PortBindings}}' "$name")"
printf '%s\n' "$bindings" | grep -q '127.0.0.1'
printf '%s\n' "$bindings" | grep -q "$port"
if docker inspect "$name" | grep -F -q "$pw"; then
  echo "password leaked into docker inspect" >&2
  exit 1
fi
if docker logs "$name" 2>&1 | grep -F -q "$pw"; then
  echo "password leaked into docker logs" >&2
  exit 1
fi

ok=0
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  if docker exec "$name" grep -q v1 /var/lib/nft/ran-version 2>/dev/null; then
    ok=1
    break
  fi
  sleep 1
done
if [[ "$ok" -ne 1 ]]; then
  echo "panel did not start" >&2
  docker logs "$name" --tail 40 >&2 || true
  exit 1
fi
docker exec "$name" grep -qx 65532 /var/lib/nft/ran-uid
docker exec "$name" grep -q '# kids-panel-start' /usr/local/lib/kids/panel-start.sh
docker exec "$name" grep -q '# kids-docker-init' /usr/local/sbin/kids-docker-init
owner="$(docker exec "$name" stat -c '%U' /usr/local/sbin/kids-docker-init)"
if [[ "$owner" != "root" ]]; then
  echo "docker-init owner = $owner" >&2
  exit 1
fi
if docker exec -u kids "$name" test -w /usr/local/sbin/kids-docker-init; then
  echo "kids can overwrite the container entrypoint" >&2
  exit 1
fi
uid="$(docker exec "$name" sed -n 's/^Uid:[[:space:]]*\([0-9]*\).*/\1/p' /proc/1/status)"
if [[ "$uid" != "65532" ]]; then
  echo "pid 1 uid = $uid" >&2
  exit 1
fi
capeff="$(docker exec "$name" sed -n 's/^CapEff:[[:space:]]*//p' /proc/1/status)"
if [[ -z "$capeff" || "$capeff" == *[!0]* ]]; then
  echo "pid 1 CapEff = $capeff" >&2
  exit 1
fi
mode="$(docker exec "$name" stat -c '%a' /var/lib/nft/bootstrap-admin-password)"
if [[ "$mode" != "600" ]]; then
  echo "bootstrap password mode = $mode" >&2
  exit 1
fi
got="$(docker exec "$name" cat /var/lib/nft/bootstrap-admin-password)"
if [[ "$got" != "$pw" ]]; then
  echo "bootstrap password file was not seeded" >&2
  exit 1
fi

echo "second install must refuse the existing name"
set +e
bash "$docker_installer" "$name" >"$workdir/second.out" 2>&1
rc=$?
set -e
if [[ "$rc" -eq 0 ]]; then
  echo "second install replaced an existing container" >&2
  exit 1
fi
grep -q '已存在' "$workdir/second.out"

echo "upgrade through next file"
NFT_INSTALL_LOCAL=1 \
NFT_VERSION=v0.16.0 \
NFT_WORK="$workdir/next" \
NFT_SERVER_BIN="$workdir/panel-v2" \
NFT_UPGRADE_FILE="$upgrade" \
NFT_STARTER_FILE="$starter" \
NFT_EXPECT_FILE=/var/lib/nft/ran-version \
NFT_EXPECT_TEXT=v2 \
bash "$container_installer" "$name" | tee "$workdir/next.log"
grep -q 'KIDS_SWITCH=next' "$workdir/next.log"
if [[ "$(docker inspect -f '{{.Id}}' "$name")" != "$id_before" ]]; then
  echo "next install changed container id" >&2
  exit 1
fi
if [[ "$(docker inspect -f '{{index .Config.Entrypoint 0}}' "$name")" != "$entry" ]]; then
  echo "upgrade replaced docker-init" >&2
  exit 1
fi
docker exec "$name" grep -q '# kids-docker-init' /usr/local/sbin/kids-docker-init
if docker exec -u kids "$name" test -w /usr/local/sbin/kids-docker-init; then
  echo "upgrade left the entrypoint writable by kids" >&2
  exit 1
fi
docker exec "$name" grep -qx 65532 /var/lib/nft/ran-uid
docker exec "$name" sh -c 'grep -q panel-slot-v2 /usr/local/lib/kids/nft-server.a || grep -q panel-slot-v2 /usr/local/lib/kids/nft-server.b'
echo "docker panel ok"
