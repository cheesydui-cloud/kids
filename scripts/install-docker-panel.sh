#!/bin/bash
# Create a kids panel container. The running process is not the entrypoint, so
# later upgrades from the panel replace a side file and restart.
#
#   curl -fL -o /tmp/install-docker-panel.sh \
#     https://github.com/cheesydui-cloud/kids/releases/download/v0.16.0/install-docker-panel.sh
#   bash /tmp/install-docker-panel.sh
#
# The container name defaults to kids-panel and the port to 127.0.0.1:7788.
# It does not replace an existing container named kids.
set -euo pipefail

name="${1:-kids-panel}"
ver="${NFT_VERSION:-v0.16.0}"
publish="${NFT_PUBLISH:-127.0.0.1:7788:7788}"
volume="${NFT_VOLUME:-${name}-data}"
image="${NFT_IMAGE:-kids-panel:${ver}}"
work="${NFT_WORK:-/tmp/kids-docker-$name-$ver}"
NFT_INSTALL_LOCAL="${NFT_INSTALL_LOCAL:-}"
NFT_REPLACE="${NFT_REPLACE:-}"
NFT_SERVER_BIN="${NFT_SERVER_BIN:-}"
NFT_UPGRADE_FILE="${NFT_UPGRADE_FILE:-}"
NFT_STARTER_FILE="${NFT_STARTER_FILE:-}"
NFT_INIT_FILE="${NFT_INIT_FILE:-}"

fail() {
  echo "错误: $*" >&2
  exit 1
}

cleanup_secret() {
  rm -f "${work:-}/bootstrap-admin-password" "${work:-}/bootstrap.env"
}
trap cleanup_secret EXIT

if [[ "$(id -u)" -ne 0 ]]; then
  fail "请用 root 在宿主机执行"
fi
command -v docker >/dev/null 2>&1 || fail "缺少 docker"
if docker inspect "$name" >/dev/null 2>&1 && [[ "$NFT_REPLACE" != "1" ]]; then
  fail "容器 $name 已存在。换一个名字，或加 NFT_REPLACE=1 重建容器。数据卷会保留"
fi

write_init() {
  if [[ -n "$NFT_INIT_FILE" && -f "$NFT_INIT_FILE" ]]; then
    cp "$NFT_INIT_FILE" "$work/docker-init.sh"
    return
  fi
  cat >"$work/docker-init.sh" <<'EOF'
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
EOF
}

prepare_files() {
  rm -rf "$work"
  mkdir -p "$work"
  if [[ "$NFT_INSTALL_LOCAL" == "1" ]]; then
    [[ -n "$NFT_SERVER_BIN" && -f "$NFT_SERVER_BIN" ]] || fail "缺少 NFT_SERVER_BIN"
    [[ -n "$NFT_UPGRADE_FILE" && -f "$NFT_UPGRADE_FILE" ]] || fail "缺少 NFT_UPGRADE_FILE"
    [[ -n "$NFT_STARTER_FILE" && -f "$NFT_STARTER_FILE" ]] || fail "缺少 NFT_STARTER_FILE"
    cp "$NFT_SERVER_BIN" "$work/nft-server"
    cp "$NFT_UPGRADE_FILE" "$work/nft-upgrade"
    cp "$NFT_STARTER_FILE" "$work/panel-start.sh"
    return
  fi
  command -v curl >/dev/null 2>&1 || fail "缺少 curl"
  local proxy="" arch machine base
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
  machine="$(uname -m)"
  case "$machine" in
    aarch64|arm64) arch=arm64 ;;
    x86_64|amd64) arch=amd64 ;;
    *) fail "不支持的架构: $machine" ;;
  esac
  echo "下载 ${ver} (${arch})"
  curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 180 -o "$work/SHA256SUMS" "$base/SHA256SUMS"
  curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 60 -o "$work/nft-upgrade" "$base/nft-upgrade"
  curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 60 -o "$work/panel-start.sh" "$base/panel-start.sh"
  curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 180 -o "$work/nft-server-linux-$arch" "$base/nft-server-linux-$arch"
  sed -i 's/\r$//' "$work/SHA256SUMS" || true
  grep " nft-server-linux-${arch}$" "$work/SHA256SUMS" | (cd "$work" && sha256sum -c -)
  cp "$work/nft-server-linux-$arch" "$work/nft-server"
  echo "已校验 nft-server-linux-$arch"
}

echo "安装 $ver 为容器 $name"
prepare_files
write_init
chmod 755 "$work/nft-server" "$work/nft-upgrade" "$work/panel-start.sh" "$work/docker-init.sh"
grep -q '# kids-panel-start' "$work/panel-start.sh" || fail "启动器内容未通过校验"
grep -q '# kids-container-upgrade' "$work/nft-upgrade" || fail "升级脚本内容未通过校验"
grep -q 'setpriv' "$work/docker-init.sh" || fail "容器入口内容未通过校验"
sed -i 's/\r$//' "$work/panel-start.sh" "$work/nft-upgrade" "$work/docker-init.sh" 2>/dev/null || true

cat >"$work/Dockerfile" <<'EOF'
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends bash ca-certificates curl tzdata passwd util-linux \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 65532 --home-dir /var/lib/nft --no-create-home kids \
 && mkdir -p /usr/local/lib/kids /var/lib/nft \
 && chown -R kids:kids /usr/local/lib/kids /var/lib/nft
COPY nft-server /usr/local/lib/kids/nft-server
COPY panel-start.sh /usr/local/lib/kids/panel-start.sh
COPY nft-upgrade /usr/local/sbin/nft-upgrade
COPY docker-init.sh /usr/local/lib/kids/docker-init.sh
RUN sed -i 's/\r$//' /usr/local/lib/kids/panel-start.sh /usr/local/sbin/nft-upgrade /usr/local/lib/kids/docker-init.sh \
 && cp /usr/local/lib/kids/panel-start.sh /usr/local/sbin/nft-server \
 && chmod 755 /usr/local/lib/kids/nft-server /usr/local/lib/kids/panel-start.sh /usr/local/sbin/nft-server /usr/local/sbin/nft-upgrade /usr/local/lib/kids/docker-init.sh \
 && printf '%s\n' /usr/local/lib/kids/nft-server > /var/lib/nft/nft-server.live \
 && chown kids:kids /usr/local/lib/kids/nft-server /usr/local/lib/kids/panel-start.sh /usr/local/sbin/nft-server /usr/local/sbin/nft-upgrade /usr/local/lib/kids/docker-init.sh /var/lib/nft/nft-server.live \
 && chown -R kids:kids /usr/local/lib/kids /var/lib/nft
ENV TZ=Asia/Shanghai NFT_ADDR=:7788 NFT_DB=/var/lib/nft/panel.db
EXPOSE 7788
VOLUME ["/var/lib/nft"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=5 \
  CMD curl -fsS http://127.0.0.1:7788/healthz || exit 1
ENTRYPOINT ["/usr/local/lib/kids/docker-init.sh"]
EOF

echo "构建镜像 $image"
docker build -t "$image" "$work"

if [[ "$NFT_REPLACE" == "1" ]] && docker inspect "$name" >/dev/null 2>&1; then
  docker rm -f "$name" >/dev/null
fi

seed_password() {
  [[ -n "${NFT_BOOTSTRAP_ADMIN_PASSWORD:-}" ]] || return 0
  local pwfile="$work/bootstrap-admin-password"
  (umask 077; printf '%s' "$NFT_BOOTSTRAP_ADMIN_PASSWORD" >"$pwfile")
  docker volume create "$volume" >/dev/null
  docker run --rm --entrypoint /bin/sh \
    -v "${volume}:/var/lib/nft" \
    -v "$pwfile:/run/bootstrap:ro" \
    "$image" \
    -c 'cp /run/bootstrap /var/lib/nft/bootstrap-admin-password && chmod 600 /var/lib/nft/bootstrap-admin-password && chown kids:kids /var/lib/nft/bootstrap-admin-password'
  rm -f "$pwfile"
}

seed_password

run=(docker run -d --name "$name"
  --restart unless-stopped
  -p "$publish"
  -v "${volume}:/var/lib/nft"
  -e TZ=Asia/Shanghai
  -e NFT_ADDR=:7788
  -e NFT_DB=/var/lib/nft/panel.db
  --cap-drop ALL
  --cap-add CHOWN
  --cap-add SETUID
  --cap-add SETGID
  --cap-add DAC_OVERRIDE
  --cap-add FOWNER
  --security-opt no-new-privileges:true
  "$image")

echo "启动 $name  端口 $publish  数据卷 $volume"
"${run[@]}" >/dev/null

echo "容器 ID $(docker inspect -f '{{.Id}}' "$name")"
echo "完成。强制刷新后打开面板。指定的密码在数据卷的 bootstrap-admin-password，面板读完会删除。没指定时，生成的密码在 initial-admin-password。密码不在容器日志里，也不在 docker inspect 里。"
echo "已有容器要升级时，用 install-container-panel.sh，不要再建一个占同一端口的面板。"
