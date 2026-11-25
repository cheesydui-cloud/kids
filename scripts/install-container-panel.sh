#!/bin/bash
# Install the kids panel into a running container without unlinking the
# executed entrypoint. overlayfs and docker cp both return
# "device or resource busy" for that file. This script copies the new program
# to a free path, then writes the starter into the container upper directory
# while the container is stopped. The container id stays the same.
set -euo pipefail

name="${1:-kids}"
ver="${NFT_VERSION:-v0.15.4}"
work="${NFT_WORK:-/tmp/kids-$name-$ver}"
NFT_INSTALL_LOCAL="${NFT_INSTALL_LOCAL:-}"
NFT_FORCE_RECREATE="${NFT_FORCE_RECREATE:-}"
NFT_EXPECT_FILE="${NFT_EXPECT_FILE:-}"
NFT_EXPECT_TEXT="${NFT_EXPECT_TEXT:-}"
NFT_SERVER_BIN="${NFT_SERVER_BIN:-}"
NFT_UPGRADE_FILE="${NFT_UPGRADE_FILE:-}"
NFT_STARTER_FILE="${NFT_STARTER_FILE:-}"

UPPER=""
ENTRY_PATH="/usr/local/sbin/nft-server"
ENTRY_BAK=""
used_recreate=0
recreate_bak=""
recreate_image=""
switch_done=0

fail() {
  echo "错误: $*" >&2
  exit 1
}

cleanup_on_exit() {
  local rc=$?
  if [[ "$rc" -eq 0 || "${switch_done:-0}" -eq 1 ]]; then
    return 0
  fi
  echo "安装未完成，正在恢复容器 $name" >&2
  if [[ "$used_recreate" -eq 0 ]]; then
    restore_upper_entrypoint || true
    if [[ -n "$recreate_bak" ]]; then
      docker rm -f "$name" >/dev/null 2>&1 || true
      if docker inspect "$recreate_bak" >/dev/null 2>&1; then
        docker rename "$recreate_bak" "$name" >/dev/null 2>&1 || true
      fi
    fi
  fi
  docker start "$name" >/dev/null 2>&1 || true
}
trap cleanup_on_exit EXIT

if [[ "$(id -u)" -ne 0 ]]; then
  fail "请用 root 在宿主机执行。不要在面板里点升级"
fi
command -v docker >/dev/null 2>&1 || fail "缺少 docker"

restore_upper_entrypoint() {
  if [[ -z "$UPPER" || -z "$ENTRY_PATH" ]]; then
    return 0
  fi
  local dest="$UPPER$ENTRY_PATH"
  if [[ -n "$ENTRY_BAK" && -f "$ENTRY_BAK" ]]; then
    cp -a "$ENTRY_BAK" "$dest" || true
  else
    rm -f "$dest" || true
  fi
}

write_upper() {
  local src="$1" dest="$2"
  mkdir -p "$(dirname "$dest")"
  if [[ -d "$dest" ]]; then
    echo "上层路径是目录: $dest" >&2
    return 1
  fi
  if [[ -f "$dest" ]]; then
    cat "$src" >"$dest"
  else
    cp "$src" "$dest"
  fi
}

entrypoint_path() {
  local ep
  ep="$(docker inspect -f '{{if .Config.Entrypoint}}{{index .Config.Entrypoint 0}}{{end}}' "$name" 2>/dev/null || true)"
  if [[ -z "$ep" ]]; then
    ep="$(docker inspect -f '{{if .Config.Cmd}}{{index .Config.Cmd 0}}{{end}}' "$name" 2>/dev/null || true)"
  fi
  case "$ep" in
    /bin/sh|/bin/bash|/usr/bin/bash|"") ep="/usr/local/sbin/nft-server" ;;
  esac
  case "$ep" in
    /*) ;;
    *) ep="/usr/local/sbin/nft-server" ;;
  esac
  printf '%s\n' "$ep"
}

ensure_running() {
  local st
  st="$(docker inspect -f '{{.State.Status}}' "$name")"
  if [[ "$st" != "running" ]]; then
    echo "启动 $name 以便写入新文件"
    docker start "$name" >/dev/null
    sleep 1
  fi
}

bash_works() {
  docker exec "$name" sh -c 'if test -x /bin/bash; then /bin/bash -c "exit 0"; elif test -x /usr/bin/bash; then /usr/bin/bash -c "exit 0"; else exit 1; fi'
}

extract_bash_binary() {
  # bash.prerm is itself a bash script. If the binary is already gone, apt-get
  # reinstall cannot run that script. Unpack the .deb and copy the binary.
  docker exec "$name" sh -c 'set -eu
    cd /tmp
    rm -rf bash-root
    rm -f bash_*.deb
    apt-get download bash
    mkdir -p bash-root
    dpkg-deb -x bash_*.deb bash-root
    if [ -f bash-root/usr/bin/bash ]; then
      cp bash-root/usr/bin/bash /usr/bin/bash
      chmod 755 /usr/bin/bash
    fi
    if [ -f bash-root/bin/bash ]; then
      cp bash-root/bin/bash /bin/bash
      chmod 755 /bin/bash
    fi
    if [ ! -x /bin/bash ] && [ -x /usr/bin/bash ]; then
      ln -sf /usr/bin/bash /bin/bash 2>/dev/null || cp /usr/bin/bash /bin/bash
      chmod 755 /bin/bash 2>/dev/null || true
    fi
    test -x /bin/bash || test -x /usr/bin/bash
  '
}

ensure_bash() {
  if bash_works; then
    return 0
  fi
  echo "容器里没有 bash，正在安装"
  docker exec "$name" sh -c 'command -v apt-get >/dev/null' || fail "容器里没有 bash，也没有 apt-get"
  docker exec "$name" sh -c 'DEBIAN_FRONTEND=noninteractive apt-get update -qq' || fail "apt-get update 失败"
  if docker exec "$name" sh -c 'dpkg -s bash >/dev/null 2>&1'; then
    extract_bash_binary || fail "安装 bash 失败"
  else
    if ! docker exec "$name" sh -c 'DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends bash'; then
      extract_bash_binary || fail "安装 bash 失败"
    fi
  fi
  bash_works || fail "安装 bash 后仍然找不到 /bin/bash"
}

copy_free_paths() {
  docker exec "$name" mkdir -p /usr/local/lib/kids /usr/local/sbin /var/lib/nft
  docker cp "$work/nft-server" "$name:/usr/local/lib/kids/nft-server"
  docker cp "$work/panel-start.sh" "$name:/usr/local/lib/kids/panel-start.sh"
  if ! docker cp "$work/nft-upgrade" "$name:/usr/local/sbin/nft-upgrade"; then
    docker cp "$work/nft-upgrade" "$name:/usr/local/lib/kids/nft-upgrade"
  fi
  docker exec "$name" chmod 755 /usr/local/lib/kids/nft-server /usr/local/lib/kids/panel-start.sh /usr/local/sbin/nft-upgrade /usr/local/lib/kids/nft-upgrade 2>/dev/null || \
    docker exec "$name" chmod 755 /usr/local/lib/kids/nft-server /usr/local/lib/kids/panel-start.sh
  printf '%s\n' /usr/local/lib/kids/nft-server | docker exec -i "$name" sh -c 'cat > /var/lib/nft/nft-server.live'
}

find_upper() {
  local upper pid line
  upper="$(docker inspect -f '{{.GraphDriver.Data.UpperDir}}' "$name" 2>/dev/null || true)"
  if [[ -n "$upper" && "$upper" != "<no value>" && -d "$upper" ]]; then
    printf '%s\n' "$upper"
    return 0
  fi
  pid="$(docker inspect -f '{{.State.Pid}}' "$name" 2>/dev/null || true)"
  if [[ -z "$pid" || "$pid" == "0" || ! -r "/proc/$pid/mountinfo" ]]; then
    return 1
  fi
  line="$(awk '$5=="/" {print; exit}' "/proc/$pid/mountinfo" || true)"
  [[ -n "$line" ]] || return 1
  upper="$(printf '%s\n' "$line" | sed -n 's/.*upperdir=\([^, ]*\).*/\1/p')"
  upper="$(printf '%s\n' "$upper" | sed 's/\\040/ /g')"
  [[ -n "$upper" && -d "$upper" ]] || return 1
  printf '%s\n' "$upper"
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
  else
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
    echo "下载 ${ver}"
    (
      cd "$work"
      curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 180 -o SHA256SUMS "$base/SHA256SUMS"
      curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 60 -o nft-upgrade "$base/nft-upgrade"
      curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 60 -o panel-start.sh "$base/panel-start.sh"
    )
    if docker inspect -f '{{.State.Running}}' "$name" 2>/dev/null | grep -q true; then
      machine="$(docker exec "$name" uname -m 2>/dev/null || uname -m)"
    else
      machine="$(uname -m)"
    fi
    case "$machine" in
      aarch64|arm64) arch=arm64 ;;
      x86_64|amd64) arch=amd64 ;;
      *) fail "不支持的架构: $machine" ;;
    esac
    curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --max-time 180 \
      -o "$work/nft-server-linux-$arch" "$base/nft-server-linux-$arch"
    sed -i 's/\r$//' "$work/SHA256SUMS" || true
    grep " nft-server-linux-${arch}$" "$work/SHA256SUMS" | (cd "$work" && sha256sum -c -)
    cp "$work/nft-server-linux-$arch" "$work/nft-server"
    echo "已校验 nft-server-linux-$arch"
  fi
  chmod 755 "$work/nft-server" "$work/nft-upgrade" "$work/panel-start.sh"
  grep -q '# kids-panel-start' "$work/panel-start.sh" || fail "启动器内容未通过校验"
  grep -q '# kids-container-upgrade' "$work/nft-upgrade" || fail "升级脚本内容未通过校验"
  grep -q 'do_update' "$work/nft-upgrade" || fail "升级脚本内容未通过校验"
  grep -q 'nft-server' "$work/nft-upgrade" || fail "升级脚本内容未通过校验"
  sed -i 's/\r$//' "$work/panel-start.sh" "$work/nft-upgrade" 2>/dev/null || true
}

switch_via_upper() {
  echo "上层目录 $UPPER"
  echo "KIDS_SWITCH=upper"
  echo "停止 $name"
  docker stop --time 20 "$name" >/dev/null || true
  mkdir -p "$UPPER/usr/local/lib/kids" "$UPPER/usr/local/sbin" "$UPPER/var/lib/nft" "$(dirname "$UPPER$ENTRY_PATH")"
  write_upper "$work/nft-server" "$UPPER/usr/local/lib/kids/nft-server" || fail "无法写入新程序"
  chmod 755 "$UPPER/usr/local/lib/kids/nft-server"
  printf '%s\n' /usr/local/lib/kids/nft-server >"$UPPER/var/lib/nft/nft-server.live"
  write_upper "$work/nft-upgrade" "$UPPER/usr/local/sbin/nft-upgrade" || fail "无法写入升级脚本"
  chmod 755 "$UPPER/usr/local/sbin/nft-upgrade"
  write_upper "$work/panel-start.sh" "$UPPER/usr/local/lib/kids/panel-start.sh" || fail "无法写入启动器"
  chmod 755 "$UPPER/usr/local/lib/kids/panel-start.sh"

  local dest="$UPPER$ENTRY_PATH"
  if [[ -f "$dest" && ! -f "$ENTRY_BAK" ]]; then
    cp -a "$dest" "$ENTRY_BAK" || fail "无法备份入口"
  fi
  if ! write_upper "$work/panel-start.sh" "$dest"; then
    restore_upper_entrypoint
    docker start "$name" >/dev/null 2>&1 || true
    fail "写入启动器失败，已恢复旧入口"
  fi
  chmod 755 "$dest" || true
  if ! head -n 1 "$dest" | grep -q bash || ! grep -q '# kids-panel-start' "$dest"; then
    restore_upper_entrypoint
    docker start "$name" >/dev/null 2>&1 || true
    fail "启动器没有写到入口，已恢复旧入口"
  fi
  sync || true
  echo "启动 $name"
  if ! docker restart --time 20 "$name" >/dev/null; then
    restore_upper_entrypoint
    docker start "$name" >/dev/null 2>&1 || true
    fail "启动失败，已恢复旧入口"
  fi
}

recreate_container() {
  command -v python3 >/dev/null 2>&1 || fail "重建容器需要 python3"
  local bak img inspect_file
  bak="${name}.bak-before-${ver}"
  if docker inspect "$bak" >/dev/null 2>&1; then
    bak="${bak}-$$"
  fi
  img="kids-panel-switch:${name}-$$"
  recreate_image="$img"
  inspect_file="$work/inspect.json"
  docker inspect "$name" >"$inspect_file"
  echo "KIDS_SWITCH=recreate"
  echo "停止 $name"
  docker stop --time 20 "$name" >/dev/null || true
  docker rename "$name" "$bak"
  recreate_bak="$bak"
  echo "提交文件系统到 $img"
  if ! docker commit "$bak" "$img" >/dev/null; then
    docker rename "$bak" "$name" || true
    docker start "$name" >/dev/null 2>&1 || true
    fail "无法提交容器文件系统，已恢复旧容器"
  fi
  python3 - "$inspect_file" "$name" "$img" >"$work/create-argv" <<'PY'
import json, sys
inspect = json.load(open(sys.argv[1], encoding="utf-8"))[0]
name, image = sys.argv[2], sys.argv[3]
cfg = inspect.get("Config") or {}
hc = inspect.get("HostConfig") or {}
args = ["create", "--name", name, "--entrypoint", "/usr/local/lib/kids/panel-start.sh"]
for k, v in (cfg.get("Labels") or {}).items():
    args.extend(["--label", "%s=%s" % (k, "" if v is None else v)])
for e in cfg.get("Env") or []:
    if e:
        args.extend(["-e", e])
rp = hc.get("RestartPolicy") or {}
rname = rp.get("Name") or ""
if rname and rname != "no":
    n = rp.get("MaximumRetryCount") or 0
    if rname == "on-failure" and n:
        args.extend(["--restart", "on-failure:%s" % n])
    else:
        args.extend(["--restart", rname])
net = hc.get("NetworkMode") or ""
if net:
    args.extend(["--network", net])
mem = hc.get("Memory") or 0
if mem:
    args.extend(["--memory", str(mem)])
swap = hc.get("MemorySwap") or 0
if mem and swap:
    args.extend(["--memory-swap", str(swap)])

def port_spec(container_port, binding):
    hip = (binding or {}).get("HostIp") or ""
    hport = (binding or {}).get("HostPort") or ""
    if hip == "0.0.0.0":
        hip = ""
    dest = container_port[:-4] if container_port.endswith("/tcp") else container_port
    if hip == "::" and hport:
        return "[::]:%s:%s" % (hport, dest)
    if hip and hport:
        return "%s:%s:%s" % (hip, hport, dest)
    if hport:
        return "%s:%s" % (hport, dest)
    return dest

for cport, bindings in (hc.get("PortBindings") or {}).items():
    if not bindings:
        args.extend(["-p", port_spec(cport, {})])
        continue
    for b in bindings:
        args.extend(["-p", port_spec(cport, b)])
if hc.get("PublishAllPorts"):
    args.append("-P")
for m in inspect.get("Mounts") or []:
    dest = m.get("Destination") or ""
    if not dest:
        continue
    ro = ":ro" if m.get("RW") is False else ""
    typ = m.get("Type") or ""
    if typ == "bind":
        src = m.get("Source") or ""
    elif typ == "volume":
        src = m.get("Name") or ""
    else:
        src = ""
    if src:
        args.extend(["-v", "%s:%s%s" % (src, dest, ro)])
for c in hc.get("CapAdd") or []:
    args.extend(["--cap-add", c])
for c in hc.get("CapDrop") or []:
    args.extend(["--cap-drop", c])
for s in hc.get("SecurityOpt") or []:
    if s:
        args.extend(["--security-opt", s])
for h in hc.get("ExtraHosts") or []:
    args.extend(["--add-host", h])
for d in hc.get("Dns") or []:
    args.extend(["--dns", d])
user = cfg.get("User") or ""
if user:
    args.extend(["--user", user])
wd = cfg.get("WorkingDir") or ""
if wd:
    args.extend(["-w", wd])
hn = cfg.get("Hostname") or ""
short = (inspect.get("Id") or "")[:12]
if hn and hn != short:
    args.extend(["--hostname", hn])
args.append(image)
for part in cfg.get("Cmd") or []:
    args.append(part)
sys.stdout.buffer.write(b"\0".join(a.encode("utf-8") for a in args) + b"\0")
PY
  local -a args=()
  local item
  while IFS= read -r -d '' item; do
    [[ -n "$item" ]] || continue
    args+=("$item")
  done <"$work/create-argv"
  if ! docker "${args[@]}"; then
    docker rm -f "$name" >/dev/null 2>&1 || true
    docker rename "$bak" "$name" || true
    docker start "$name" >/dev/null 2>&1 || true
    docker rmi "$img" >/dev/null 2>&1 || true
    fail "重建容器失败，已恢复旧容器"
  fi
  if ! docker start "$name" >/dev/null; then
    docker rm -f "$name" >/dev/null 2>&1 || true
    docker rename "$bak" "$name" || true
    docker start "$name" >/dev/null 2>&1 || true
    docker rmi "$img" >/dev/null 2>&1 || true
    fail "启动重建后的容器失败，已恢复旧容器"
  fi
  used_recreate=1
  echo "旧容器已改名为 $bak。确认新面板正常后可以删除它"
}

starter_in_place() {
  if [[ "$used_recreate" -eq 1 ]]; then
    docker exec "$name" grep -q '# kids-panel-start' /usr/local/lib/kids/panel-start.sh
    return
  fi
  docker exec "$name" sh -c "head -n 1 '$ENTRY_PATH' | grep -q bash && grep -q '# kids-panel-start' '$ENTRY_PATH'"
}

expect_ok() {
  if [[ -n "$NFT_EXPECT_FILE" ]]; then
    [[ -n "$NFT_EXPECT_TEXT" ]] || return 1
    docker exec "$name" grep -q -F -e "$NFT_EXPECT_TEXT" "$NFT_EXPECT_FILE"
    return
  fi
  docker exec "$name" grep -a -q -F -e "$ver" /proc/1/exe
}

verify_after_start() {
  local i ok=0
  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30; do
    if starter_in_place && expect_ok; then
      ok=1
      break
    fi
    sleep 1
  done
  if [[ "$ok" -ne 1 ]]; then
    echo "--- 启动后校验失败 ---" >&2
    docker logs "$name" --tail 40 >&2 || true
    if [[ "$used_recreate" -eq 0 ]]; then
      docker stop --time 20 "$name" >/dev/null 2>&1 || true
      restore_upper_entrypoint
      docker start "$name" >/dev/null 2>&1 || true
      fail "启动后没有看到新面板，已恢复旧入口"
    fi
    if [[ -n "$recreate_bak" ]]; then
      docker rm -f "$name" >/dev/null 2>&1 || true
      docker rename "$recreate_bak" "$name" || true
      docker start "$name" >/dev/null 2>&1 || true
    fi
    if [[ -n "$recreate_image" ]]; then
      docker rmi "$recreate_image" >/dev/null 2>&1 || true
    fi
    fail "启动后没有看到新面板，已恢复旧容器"
  fi
}

if ! docker inspect "$name" >/dev/null 2>&1; then
  fail "找不到容器 $name"
fi

echo "安装 $ver 到容器 $name"
echo "容器 ID $(docker inspect -f '{{.Id}}' "$name")"
prepare_files
ensure_running
ENTRY_PATH="$(entrypoint_path)"
echo "当前入口 $ENTRY_PATH"
case "$ENTRY_PATH" in
  /usr/local/lib/kids/nft-server|/usr/local/lib/kids/panel-start.sh|/usr/local/sbin/nft-upgrade)
    fail "入口路径和要写入的新文件冲突: $ENTRY_PATH"
    ;;
esac
ensure_bash
copy_free_paths

if [[ "$NFT_FORCE_RECREATE" == "1" ]]; then
  recreate_container
else
  UPPER="$(find_upper || true)"
  if [[ -n "$UPPER" ]] && touch "$UPPER/.kids-write-test" 2>/dev/null; then
    rm -f "$UPPER/.kids-write-test"
    ENTRY_BAK="$UPPER${ENTRY_PATH}.bak-before-${ver}"
    switch_via_upper
  else
    echo "上层目录不可写，改为重建容器"
    echo "GraphDriver: $(docker inspect -f '{{json .GraphDriver}}' "$name" 2>/dev/null || true)"
    recreate_container
  fi
fi

verify_after_start
switch_done=1
if [[ -n "$recreate_image" ]]; then
  docker rmi "$recreate_image" >/dev/null 2>&1 || true
fi
echo "--- 启动后 ---"
docker exec "$name" sh -c 'echo cmdline:$(tr "\0" " " < /proc/1/cmdline); echo starter:$(head -n 1 /usr/local/sbin/nft-server 2>/dev/null || head -n 1 /usr/local/lib/kids/panel-start.sh); ls -l /proc/1/exe' || true
if curl -fsS --max-time 8 http://127.0.0.1:7788/healthz >/dev/null 2>&1; then
  echo "healthz 正常"
else
  echo "7788 暂未响应。若页面没有回来，执行: docker start $name"
fi
echo "完成。强制刷新面板，侧边栏应显示 $ver"
echo "容器 ID $(docker inspect -f '{{.Id}}' "$name")"
