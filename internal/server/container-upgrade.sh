#!/bin/bash
# kids-container-upgrade
# Container-safe nft-upgrade. The panel accepts this file only when it mentions
# do_update and nft-server. It downloads a release, replaces the running
# nft-server, then restarts. systemd hosts restart the unit. Containers signal
# pid 1 so the runtime starts the new file.
set -u

REPO="cheesydui-cloud/kids"
GH_PROXY_FILE="/etc/nft/gh-proxy"
LOG="/var/lib/nft/panel-update.log"
GH_PROXY=""

log() {
  mkdir -p /var/lib/nft 2>/dev/null || true
  printf '%s\n' "$*" >&2
}

die() {
  log "错误: $*"
  exit 1
}

read_proxy() {
  GH_PROXY=""
  [[ -f "$GH_PROXY_FILE" ]] || return 0
  local raw
  raw="$(tr -d ' \t\r\n' <"$GH_PROXY_FILE" 2>/dev/null || true)"
  case "$raw" in
    http://*|https://*)
      case "$raw" in
        */) GH_PROXY="$raw" ;;
        *) GH_PROXY="${raw}/" ;;
      esac
      ;;
  esac
}

ensure_curl() {
  if command -v curl >/dev/null 2>&1 && [[ -f /etc/ssl/certs/ca-certificates.crt ]]; then
    return 0
  fi
  if ! command -v apt-get >/dev/null 2>&1; then
    die "缺少 curl 或 CA 证书，且没有 apt-get"
  fi
  log "安装 curl 和 ca-certificates"
  DEBIAN_FRONTEND=noninteractive apt-get update -qq || die "apt-get update 失败"
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends curl ca-certificates || die "安装 curl 失败"
}

sum_for() {
  local name="$1" file="$2" hash fname
  while read -r hash fname || [[ -n "${hash:-}" ]]; do
    fname="${fname#\*}"
    fname="${fname%%$'\r'}"
    if [[ "$fname" == "$name" ]]; then
      printf '%s\n' "$hash"
      return 0
    fi
  done <"$file"
  return 1
}

in_container() {
  [[ -f /.dockerenv || -f /run/.containerenv ]] && return 0
  if [[ -r /proc/1/cgroup ]] && grep -qE 'docker|containerd|kubepods|libpod' /proc/1/cgroup; then
    return 0
  fi
  return 1
}

find_panel() {
  local pid link
  PANEL_PID=""
  PANEL_PATH=""
  if [[ -d /proc/1 ]]; then
    link="$(readlink /proc/1/exe 2>/dev/null || true)"
    link="${link%% (*}"
    case "$link" in
      *nft-server*)
        PANEL_PID=1
        PANEL_PATH="$link"
        return 0
        ;;
    esac
  fi
  for pid in /proc/[0-9]*; do
    [[ -d "$pid" ]] || continue
    link="$(readlink "$pid/exe" 2>/dev/null || true)"
    link="${link%% (*}"
    case "$link" in
      *nft-server*)
        PANEL_PID="${pid#/proc/}"
        PANEL_PATH="$link"
        return 0
        ;;
    esac
  done
  if [[ -e /usr/local/sbin/nft-server || -L /usr/local/sbin/nft-server ]]; then
    PANEL_PATH="/usr/local/sbin/nft-server"
    return 0
  fi
  return 1
}

replace_busy_by_dir_swap() {
  local dest="$1" src="$2"
  local parent base grand newdir olddir name f
  parent="$(dirname "$dest")"
  base="$(basename "$dest")"
  grand="$(dirname "$parent")"
  [[ -n "$parent" && "$parent" != "/" && -d "$parent" ]] || return 1
  [[ -n "$grand" && "$grand" != "$parent" && -d "$grand" && -w "$grand" ]] || return 1
  newdir="$grand/.nft-sbin-new.$$"
  olddir="$grand/.nft-sbin-old.$$"
  rm -rf "$newdir"
  mkdir -p "$newdir" || return 1
  while IFS= read -r f; do
    name="$(basename "$f")"
    [[ "$name" == "$base" ]] && continue
    case "$name" in
      .nft-server-new-*) continue ;;
    esac
    cp -a "$f" "$newdir/" || { rm -rf "$newdir"; return 1; }
  done < <(find "$parent" -mindepth 1 -maxdepth 1 -print)
  cp "$src" "$newdir/$base" || { rm -rf "$newdir"; return 1; }
  chmod 755 "$newdir/$base" || true
  if ! mv "$parent" "$olddir"; then
    rm -rf "$newdir"
    return 1
  fi
  if ! mv "$newdir" "$parent"; then
    mv "$olddir" "$parent" 2>/dev/null || true
    rm -rf "$newdir"
    return 1
  fi
  rm -rf "$olddir" 2>/dev/null || true
  return 0
}

replace_binary() {
  local dest="$1" src="$2" dir tmp
  dir="$(dirname "$dest")"
  mkdir -p "$dir" || die "无法写入 $dir"
  tmp="$(mktemp "$dir/.nft-server-new.XXXXXX")" || die "无法写入新程序"
  cp "$src" "$tmp" || die "无法写入新程序"
  chmod 755 "$tmp" || die "无法写入新程序"
  if rm -f "$dest" 2>/dev/null; then
    if mv "$tmp" "$dest"; then
      chmod 755 "$dest" || true
      return 0
    fi
  fi
  # overlayfs refuses to unlink a running binary: Device or resource busy.
  log "正在运行的程序无法直接删除，改为替换所在目录"
  if replace_busy_by_dir_swap "$dest" "$tmp"; then
    rm -f "$tmp" 2>/dev/null || true
    log "已通过目录替换放入新程序"
    return 0
  fi
  if mount --bind "$tmp" "$dest" 2>/dev/null; then
    log "已用 bind mount 盖住旧程序"
    return 0
  fi
  die "无法替换正在运行的程序: $dest"
}

save_cmdline() {
  local pid="$1" item
  [[ -n "$pid" && -r "/proc/$pid/cmdline" ]] || return 0
  {
    printf '%s\n' '#!/bin/bash'
    printf 'cd / || true\n'
    printf 'exec'
    while IFS= read -r -d '' item; do
      printf ' %q' "$item"
    done <"/proc/$pid/cmdline"
    printf '\n'
  } > /var/lib/nft/panel-cmdline.sh
  chmod 755 /var/lib/nft/panel-cmdline.sh
}

schedule_restart() {
  local pid="$1"
  mkdir -p /var/lib/nft
  save_cmdline "$pid"
  cat > /var/lib/nft/panel-restart.sh <<EOF
#!/bin/bash
sleep 3
if command -v systemctl >/dev/null 2>&1 && systemctl cat nft-server.service >/dev/null 2>&1; then
  systemctl restart nft-server.service
  exit 0
fi
in_ctr=0
if [[ -f /.dockerenv || -f /run/.containerenv ]]; then
  in_ctr=1
elif [[ -r /proc/1/cgroup ]] && grep -qE 'docker|containerd|kubepods|libpod' /proc/1/cgroup; then
  in_ctr=1
fi
if [[ "$pid" == "1" || "\$in_ctr" -eq 1 ]]; then
  if [[ -n "$pid" && "$pid" != "1" ]]; then
    kill -TERM "$pid" 2>/dev/null || true
    sleep 1
  fi
  kill -TERM 1 2>/dev/null || true
  echo "若页面没有回来，在宿主机执行 docker start 对应容器"
  exit 0
fi
if [[ -n "$pid" ]]; then
  kill -TERM "$pid" 2>/dev/null || true
  sleep 1
fi
if [[ -x /var/lib/nft/panel-cmdline.sh ]]; then
  nohup /bin/bash /var/lib/nft/panel-cmdline.sh >>/var/lib/nft/panel-update-restart.log 2>&1 &
fi
EOF
  chmod 755 /var/lib/nft/panel-restart.sh
  nohup /bin/bash /var/lib/nft/panel-restart.sh >>/var/lib/nft/panel-update-restart.log 2>&1 &
  disown 2>/dev/null || true
}

download_one() {
  local base="$1" name="$2" tmp="$3" expected size magic actual
  expected="$(sum_for "$name" "$tmp/SHA256SUMS" || true)"
  if [[ -z "$expected" ]]; then
    log "SHA256SUMS 中没有 $name"
    return 1
  fi
  if ! curl -fsSL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 180 \
    -o "$tmp/nft-server" "$base/$name"; then
    log "下载 $name 失败"
    return 1
  fi
  size="$(wc -c <"$tmp/nft-server" | tr -d ' ')"
  if [[ "${size:-0}" -lt 1048576 ]]; then
    die "下载 $name 失败: 不是面板程序"
  fi
  magic="$(od -An -t x1 -N 4 "$tmp/nft-server" | tr -d ' \n')"
  if [[ "$magic" != "7f454c46" ]]; then
    die "下载 $name 失败: 不是面板程序"
  fi
  actual="$(sha256sum "$tmp/nft-server" | cut -d' ' -f1)"
  if [[ "$actual" != "$expected" ]]; then
    die "下载 $name 失败: sha256 校验失败"
  fi
  log "已校验 $name"
  return 0
}

do_update() {
  local tag="$1" arch base tmp name dest ok
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "版本号无效: $tag"
  ensure_curl
  read_proxy
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) die "不支持的架构: $(uname -m)" ;;
  esac
  base="${GH_PROXY}https://github.com/${REPO}/releases/download/${tag}"
  tmp="$(mktemp -d)" || die "无法创建临时目录"
  trap 'rm -rf "$tmp"' EXIT
  log "下载 ${tag} (${arch})"
  curl -fsSL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 60 \
    "$base/SHA256SUMS" -o "$tmp/SHA256SUMS" || die "下载 SHA256SUMS 失败"
  ok=0
  for name in "nft-server-linux-${arch}" "nft-server"; do
    if [[ "$name" == "nft-server" && "$arch" != "amd64" ]]; then
      continue
    fi
    if download_one "$base" "$name" "$tmp"; then
      ok=1
      break
    fi
  done
  [[ "$ok" -eq 1 ]] || die "未找到 nft-server 安装包"
  find_panel || die "找不到正在运行的 nft-server"
  dest="$PANEL_PATH"
  [[ -n "$dest" ]] || dest="/usr/local/sbin/nft-server"
  log "替换 $dest"
  replace_binary "$dest" "$tmp/nft-server"
  schedule_restart "$PANEL_PID"
  log "新程序已放好，即将重启。若页面没有回来，在宿主机执行 docker start 对应容器"
  exit 0
}

if [[ -z "${NFT_UPGRADE_REEXEC:-}" && -n "${0:-}" && -f "$0" ]]; then
  if cp "$0" /tmp/nft-upgrade-run 2>/dev/null; then
    chmod 755 /tmp/nft-upgrade-run
    export NFT_UPGRADE_REEXEC=1
    exec /bin/bash /tmp/nft-upgrade-run "$@"
  fi
fi

mode=""
release=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    update)
      mode="update"
      shift
      ;;
    --release)
      release="${2:-}"
      shift 2
      ;;
    --release=*)
      release="${1#*=}"
      shift
      ;;
    *)
      shift
      ;;
  esac
done

if [[ "$mode" != "update" ]]; then
  echo "用法: nft-upgrade update --release vX.Y.Z" >&2
  exit 2
fi
if [[ -z "$release" ]]; then
  die "缺少 --release"
fi
do_update "$release"
