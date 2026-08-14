#!/usr/bin/env bash
# kids 节点 agent 安装脚本（由面板 /v1/install-agent 下发）
# 节点只连面板，不访问 GitHub。
# 用法（面板详情页已拼好）:
#   curl -fsSL http://面板:7788/v1/install-agent | bash -s -- --token <hex>
set -euo pipefail

INSTALL_DIR="/usr/local/sbin"
SYSTEMD_DIR="/etc/systemd/system"
ETC_DIR="/etc/nft"
# 面板生成脚本时会替换下一行；也可被 --panel-url / 环境变量覆盖。
KIDS_PANEL_URL_BAKED='__KIDS_PANEL_URL__'

die() { echo "错误: $*" >&2; exit 1; }
note() { printf '\033[36m%s\033[0m\n' "$*"; }
ok()   { printf '\033[32m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m警告: %s\033[0m\n' "$*" >&2; }

require_val() {
  if [[ -z "${2:-}" || "${2:-}" == --* ]]; then
    die "$1 需要值（是否漏写了？）"
  fi
}

[[ ${EUID:-$(id -u)} -eq 0 ]] || die "请以 root 运行（精简系统往往没有 sudo）"

PANEL_URL="${KIDS_PANEL_URL:-}"
TOKEN="${AGENT_TOKEN:-}"
PORT_RANGE=""
RELAY_HOST=""
RELAY_HOST_V6=""
ALLOW_INSECURE="${NFTF_ALLOW_INSECURE:-}"
CURL_TLS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --panel-url) require_val --panel-url "${2:-}"; PANEL_URL="$2"; shift 2 ;;
    --panel-url=*) PANEL_URL="${1#*=}"; shift ;;
    --token) require_val --token "${2:-}"; TOKEN="$2"; shift 2 ;;
    --token=*) TOKEN="${1#*=}"; shift ;;
    --port-range) require_val --port-range "${2:-}"; PORT_RANGE="$2"; shift 2 ;;
    --port-range=*) PORT_RANGE="${1#*=}"; shift ;;
    --relay-host) require_val --relay-host "${2:-}"; RELAY_HOST="$2"; shift 2 ;;
    --relay-host=*) RELAY_HOST="${1#*=}"; shift ;;
    --relay-host-v6) require_val --relay-host-v6 "${2:-}"; RELAY_HOST_V6="$2"; shift 2 ;;
    --relay-host-v6=*) RELAY_HOST_V6="${1#*=}"; shift ;;
    --insecure) ALLOW_INSECURE=1; shift ;;
    -k|--insecure-tls) CURL_TLS=(-k); shift ;;
    -h|--help)
      cat <<'H'
kids 节点安装（只连面板）
  --panel-url URL   面板地址，缺省用脚本内置
  --token TOKEN     节点 token（必填）
  --port-range A-B  中继端口范围
  --relay-host H    数据面 IPv4/域名
  --relay-host-v6 H 数据面 IPv6
  --insecure        允许明文 http 控制信道
  -k                curl 跳过 TLS 校验（自签证书）
H
      exit 0 ;;
    *) die "未知参数: $1" ;;
  esac
done

if [[ -z "$PANEL_URL" || "$PANEL_URL" == "__KIDS_PANEL_URL__" ]]; then
  PANEL_URL="${KIDS_PANEL_URL_BAKED:-}"
fi
[[ -n "$PANEL_URL" && "$PANEL_URL" != "__KIDS_PANEL_URL__" ]] || die "缺少面板地址：加 --panel-url http://IP:7788"
[[ -n "$TOKEN" ]] || die "缺少 --token（在面板节点详情页复制）"

case "$PANEL_URL" in
  https://*|http://*) ;;
  ws://*) PANEL_URL="http://${PANEL_URL#ws://}" ;;
  wss://*) PANEL_URL="https://${PANEL_URL#wss://}" ;;
  *) PANEL_URL="http://$PANEL_URL" ;;
esac
PANEL_URL="${PANEL_URL%/}"

m="$(uname -m)"
case "$m" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "不支持的架构: $m（需要 linux/amd64 或 linux/arm64）" ;;
esac

need=()
command -v curl >/dev/null 2>&1 || need+=(curl)
command -v nft >/dev/null 2>&1 || need+=(nftables)
command -v tc >/dev/null 2>&1 || need+=(iproute2)
command -v systemctl >/dev/null 2>&1 || die "需要 systemd（未找到 systemctl）"
if [[ ${#need[@]} -gt 0 ]]; then
  command -v apt-get >/dev/null 2>&1 || die "缺少依赖: ${need[*]}，请先安装"
  note "安装缺失依赖: ${need[*]}"
  DEBIAN_FRONTEND=noninteractive apt-get update -qq
  DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${need[@]}" \
    || die "自动安装依赖失败: ${need[*]}"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
hdr="$tmp/hdr"
bin="$tmp/nft-agent"
url="$PANEL_URL/v1/binary?arch=$ARCH"

note "[1/3] 从面板下载 nft-agent ($ARCH) ..."
note "      $url"
curl -fL --retry 5 --retry-delay 2 --connect-timeout 20 --progress-bar \
  "${CURL_TLS[@]}" -D "$hdr" "$url" -o "$bin" \
  || die "从面板下载失败。请确认本机能访问 $PANEL_URL ，且面板已升级到含节点安装接口的版本"

size="$(wc -c < "$bin" | tr -d ' ')"
if [[ -z "$size" || "$size" -lt 1048576 ]]; then
  head -c 200 "$bin" >&2 || true
  die "下载内容不是二进制（${size:-0} bytes）。面板可能还没准备好 agent，请先升级面板"
fi

want="$(awk 'BEGIN{IGNORECASE=1} /^X-SHA256:/{print $2}' "$hdr" | tr -d '\r' | tail -1)"
got="$(sha256sum "$bin" | awk '{print $1}')"
if [[ -n "$want" && "$got" != "$want" ]]; then
  die "sha256 校验失败（期望 ${want:0:12}…，实际 ${got:0:12}…）"
fi
ver="$(awk 'BEGIN{IGNORECASE=1} /^X-Agent-Version:/{print $2}' "$hdr" | tr -d '\r' | tail -1)"
ver="${ver:-panel}"

note "[2/3] 安装到 $INSTALL_DIR ..."
mkdir -p "$INSTALL_DIR" "$ETC_DIR"
install -m 0755 "$bin" "$INSTALL_DIR/nft-agent"
printf '%s' "$ver" >"$ETC_DIR/agent.version"
printf '%s' "$got" >"$ETC_DIR/agent.sha"
umask 077
printf '%s' "$TOKEN" >"$ETC_DIR/panel.token"
chmod 600 "$ETC_DIR/panel.token"

ws="$PANEL_URL"
case "$ws" in
  https://*) ws="wss://${ws#https://}" ;;
  http://*)  ws="ws://${ws#http://}" ;;
esac
case "$ws" in
  *"/v1/agents"|*"/v1/agents/") ;;
  */) ws="${ws}v1/agents" ;;
  *)  ws="${ws}/v1/agents" ;;
esac

insecure_arg=""
case "$ws" in
  ws://*)
    if [[ -n "$ALLOW_INSECURE" ]]; then
      warn "使用明文控制信道 $ws（仅限内网/测试）"
      insecure_arg=" --insecure-connect"
    else
      die "面板是 http://，控制信道为明文。确认后请加 --insecure"
    fi
    ;;
esac

extra=" --connect $ws --panel-token-file $ETC_DIR/panel.token"
[[ -n "$PORT_RANGE" ]] && extra+=" --port-range $PORT_RANGE"
[[ -n "$RELAY_HOST" ]] && extra+=" --relay-host $RELAY_HOST"
[[ -n "$RELAY_HOST_V6" ]] && extra+=" --relay-host-v6 $RELAY_HOST_V6"
extra+="$insecure_arg"

cat >"$SYSTEMD_DIR/nft-daemon.service" <<EOF
[Unit]
Description=nft host daemon (nftables controller)
After=network-online.target nftables.service
Wants=network-online.target
StartLimitIntervalSec=120
StartLimitBurst=8

[Service]
Type=simple
ExecStart=$INSTALL_DIR/nft-agent daemon$extra
Restart=always
RestartSec=3
WatchdogSec=60
NotifyAccess=main
RuntimeDirectory=nft
RuntimeDirectoryMode=0750
StateDirectory=nft
StateDirectoryMode=0750

[Install]
WantedBy=multi-user.target
EOF

cat >"$INSTALL_DIR/nft-upgrade" <<UP
#!/usr/bin/env bash
set -euo pipefail
curl -fsSL ${CURL_TLS[*]+"${CURL_TLS[*]} "} $PANEL_URL/v1/install-agent | bash -s -- --token "\$(cat $ETC_DIR/panel.token)" ${PORT_RANGE:+--port-range $PORT_RANGE} ${RELAY_HOST:+--relay-host $RELAY_HOST} ${RELAY_HOST_V6:+--relay-host-v6 $RELAY_HOST_V6} ${ALLOW_INSECURE:+--insecure}
UP
chmod 0755 "$INSTALL_DIR/nft-upgrade"

note "[3/3] 启动 nft-daemon ..."
systemctl daemon-reload
systemctl enable nft-daemon.service
systemctl restart nft-daemon.service
sleep 1
if systemctl is-active --quiet nft-daemon.service; then
  ok "===== Agent 已安装并启动 ====="
else
  warn "服务未处于 active，请看日志: journalctl -u nft-daemon.service -n 80 --no-pager"
fi
echo "面板:     $PANEL_URL"
echo "控制信道: $ws"
echo "架构:     linux-$ARCH"
echo "版本:     $ver"
echo "sha256:   $got"
echo "日志:     journalctl -u nft-daemon.service -f"
