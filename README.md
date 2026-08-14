# kids

基于 nftables 的轻量多节点端口转发平台。**两个二进制（`nft-server` + `nft-agent`），零外部依赖**——面板管理多节点并推送规则，节点 agent 反向连入面板，节点零端口暴露。支持内核态 DNAT 与用户态 split-TCP 逐跳混用、多租户配额、组合节点自动编排多跳链路。

当前版本：**v0.1.3**（下面这条安装命令固定不变，永远装最新版）  
仓库：https://github.com/cheesydui-cloud/kids

> 衍生自 [xjetry/nft-forward](https://github.com/xjetry/nft-forward)，以 MIT 许可证发布（见 [LICENSE](./LICENSE)）。

---

## 一键安装（固定命令）

支持 **linux/amd64** 与 **linux/arm64**，需要 **root + systemd**。脚本会自动安装缺失的 `nftables` / `iproute2` / `curl`（Debian/Ubuntu）。

这条 URL 钉在 `main/install.sh`，**以后发 v0.2.0、v1.0.0 都不用改命令**。脚本会自动拉 GitHub Releases 的 **latest** 二进制。

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh)
```

> 必须用 **root**。很多 Debian/VPS 精简镜像没有 `sudo`，已经是 `root@...#` 时不要加 `sudo`，否则会报 `sudo: command not found`，后面的 `curl: (23)` 只是连带失败。

等价写法（同样永远指向最新 release）：

```bash
curl -fsSL https://github.com/cheesydui-cloud/kids/releases/latest/download/install.sh | bash
```

指定角色（管道模式，同样永远装最新版）：

```bash
# 控制面板（默认监听 :7788）
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s server

# 自定义面板地址
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s server --addr 0.0.0.0:7788

# 远程节点（token 在面板「节点详情」里生成）
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s agent \
  --panel-url https://panel.example.com \
  --token <hex>

# 单机 TUI（不装 Web 面板）
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s tui

# 已安装机器升级到最新版
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

国内访问 GitHub 较慢时，加镜像前缀（会持久化，后续升级自动沿用）：

```bash
curl -fsSL https://gh-proxy.com/https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s server \
  --gh-proxy https://gh-proxy.com/
```

只有需要钉死旧版本时才加 `--release`（一般不用）：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s server --release v0.1.0
```

---

## 功能详情

### 架构

```
┌──────────────┐        ┌─────────────────┐
│  浏览器       │ HTTPS  │  反向代理 (可选)  │
│  admin/user  │ ─────► │  Caddy / Nginx   │
└──────────────┘        └────────┬────────┘
                                 │ HTTP
                        ┌────────▼────────┐
                        │   nft-server    │
                        │  Web 面板 + API  │
                        │  Agent Hub      │
                        │  SQLite WAL     │
                        └────────┬────────┘
                                 │ Unix socket / WSS
                 ┌───────────────┼───────────────┐
            ┌────▼────┐    ┌─────▼─────┐    ┌────▼─────┐
            │nft-agent│    │ nft-agent │    │nft-agent │
            │ (本机)   │    │ (节点 A)  │    │(节点 B)  │
            │ daemon  │    │ --connect │    │--connect │
            │nftables │    │ wss://…   │    │wss://…   │
            └─────────┘    └───────────┘    └──────────┘
```

| 组件 | 作用 |
|------|------|
| **nft-server** | Web UI（React SPA，embed 进二进制）、JSON API、agent hub（WebSocket）、SQLite |
| **nft-agent** | nftables 内核操作、tc 限速、userspace relay + 连接池、DNS。无面板/sqlite/web 依赖 |

agent 反向 WebSocket 连入面板，节点无需对公网开放管理端口。

### 数据面

**内核态（默认）**：nftables DNAT，零拷贝，吞吐接近线速。支持 TCP / UDP / TCP+UDP。

**用户态（split-TCP）**：每一跳独立建 TCP 连接，避免 TCP-over-TCP 拥塞折叠，多跳链路推荐。内置 TCP 预连接池，新请求跳过握手延迟。

| 环境变量 | 说明 | 默认 |
|---------|------|------|
| `NFT_FORWARD_POOL_SIZE` | 每端口预连接数（0 = 关闭） | `4` |
| `NFT_FORWARD_DNS_INTERVAL` | DDNS 重解析周期 | `60s` |

### 多租户

管理员创建用户并授权节点，用户在授权范围内自助建转发规则。

- **规则配额**：最大转发规则数
- **流量配额**：全局流量上限（GB），可周期自动重置
- **到期时间**：支持 +1 天 / +7 天 / +30 天 / +1 年
- **per-node 流量配额**：单节点独立限额
- **节点授权**：按节点授权，可附带 per-node 规则上限，支持批量授权/撤销
- **管理备注**：仅管理员可见

### 组合节点与多跳

管理员把多个单点编排成组合节点（如 `上海 → 日本`），指定跳序、各段模式（内核态/用户态）和流量倍率。用户在组合节点上建规则时，系统自动展开多跳、分配并对齐端口。

- 拖拽调整跳序，端口保持稳定
- 节点间各段可独立选内核态或用户态；出口段模式由建规则的用户选择
- 出口支持域名（daemon 侧 DNS 定期刷新）
- 连通性探测：每跳独立测延迟

### 落地节点 / 代理订阅

- 订阅地址（如 Remnawave 等面板订阅链接）
- 手动 URI（`vless://`、`trojan://`、`socks5://`、`naive+https://` 等）
- SOCKS5 / Naive 也可只填 IP、端口、账号、密码，面板自动生成分享链接
- 订阅与手动 URI 可组合
- 节点可标记为「落地」「直连」或「未配置」
- 用户建规则时可选落地节点作为出口

### Web 面板

**管理员**

- 节点：创建 / 编辑 / 禁用 / 删除 / 隐藏，设中继地址（IPv4 / IPv6）、端口范围、倍率
- 组合节点：多跳序列、拖拽排序、各段模式与倍率
- 用户：配额、到期、备注，统一表单
- 规则：查看 / 编辑 / 删除全部规则
- 节点推送升级：一键把 agent 二进制推到远程节点并重启
- 连通性探测、安装命令生成（含 token，支持 gh-proxy）

**用户**

- 自助创建 / 编辑 / 删除转发规则
- 查看流量、配额、到期
- 连通性探测
- 复制入口地址（URI / YAML）
- 代理节点管理（本地浏览器存储）

前端为 React + Tailwind，embed 进 `nft-server`。响应式，支持深色/浅色主题和敏感信息脱敏。

### 单机 TUI

`nft-agent` 不带参数进入终端 TUI，可在本机管理规则，无需浏览器。需本机 daemon 已运行。

---

## 使用说明

### install.sh 模式

| 模式 | 说明 |
|------|------|
| `tui` | 单机 TUI（装 nft-agent，自动启动 daemon） |
| `server` | 控制面板（装 nft-server + nft-agent，含本机 daemon） |
| `agent` | 远程节点（daemon 反向 dial 面板 WebSocket） |
| `update` | 拉 latest 二进制原子替换 + restart，失败自动回滚 |
| `update-script` | 只更新本地 install.sh |
| `uninstall <角色>` | 卸载 `server` / `agent` / `daemon` / `all` |
| `reset-password` | 重置面板 admin 密码 |

常用选项：

| 选项 | 说明 |
|------|------|
| `--addr ADDR` | 面板监听地址，默认 `:7788` |
| `--panel-url URL` | agent 连接的面板地址 |
| `--token TOKEN` | agent bearer token |
| `--port-range START-END` | 中继端口范围，默认 `10001-20000` |
| `--relay-host HOST` | 声明数据面 IPv4 |
| `--relay-host-v6 HOST` | 声明数据面 IPv6 |
| `--release VER` | 指定 GitHub release tag，默认 latest |
| `--gh-proxy PFX` | GitHub 镜像前缀 |
| `--purge` | uninstall 时按角色清残留数据 |
| `--insecure` | 允许明文 http/ws（仅本地测试） |

### 命令

```
nft-agent                      TUI 交互（需 daemon 运行中）
nft-agent daemon               前台启动 daemon
nft-agent daemon --connect wss://panel/v1/agents \
    --panel-token-file /etc/nft/panel.token
                               agent 模式：反向连入面板

nft-server [--addr :7788] [--db PATH]
                               Web 面板
nft-server --reset-admin-password <newpw>
                               重置 admin 密码
```

首次启动 server 会创建 admin 账号并打印随机密码。请立刻保存；之后可用：

```bash
bash /usr/local/sbin/nft-upgrade   # 等价于 install.sh update
# 或
bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh) reset-password
```

### 典型部署流程

1. **面板机**执行 `server` 一键安装，浏览器打开 `http://<IP>:7788`，用打印出的 admin 密码登录。
2. 在面板创建节点，复制「一键安装命令」。
3. **节点机**粘贴执行 `agent` 安装；节点会反向连上面板，状态变为在线。
4. 管理员给用户授权节点；用户在授权范围内创建转发规则。
5. 需要 HTTPS / WSS 时，前面加 Caddy / Nginx 做 TLS 终结（面板本身不内置 TLS）。

### 文件路径

| 路径 | 用途 |
|------|------|
| `/usr/local/sbin/nft-server` | 面板二进制 |
| `/usr/local/sbin/nft-agent` | 节点二进制（daemon + TUI） |
| `/var/run/nft.sock` | daemon Unix socket |
| `/var/lib/nft/state.json` | daemon 规则持久化 |
| `/var/lib/nft/panel.db` | 面板 SQLite |
| `/etc/nft/panel.token` | agent token |
| `/usr/local/sbin/nft-upgrade` | 安装时生成的升级脚本 |

### 升级 / 卸载

```bash
nft-upgrade
# 或指定版本
bash install.sh update --release v0.1.0

bash install.sh uninstall server
bash install.sh uninstall daemon --purge
bash install.sh uninstall all
```

升级流程：拉 release → sha256 校验 → 备份 → 原子替换 → 重启 → health-check → 失败自动回滚。

---

## 与同类方案对比

| 维度 | realm | gost | 哆啦A梦面板 | **kids** |
|------|-------|------|-----------|----------|
| **数据面** | 用户态 relay | 用户态 relay + 协议栈 | 调用 iptables/realm | nftables 内核态 DNAT **或** 用户态 split-TCP，逐跳可选 |
| **多节点管理** | 无 | 无 | 有，节点需开端口 | agent 反向 WebSocket，节点零端口暴露 |
| **多跳链路** | 手动逐节点配置 | 转发链配置复杂 | 需手动对齐端口 | 面板自动编排：选跳 + 填出口，端口自动分配对齐 |
| **多租户** | 无 | 无 | 有，粗粒度 | 节点授权 + 规则/流量配额 + 到期 + 周期重置 |
| **预连接** | 无 | 无 | 无 | userspace 模式内置 TCP 预连接池 |
| **TUI** | 无 | 无 | 无 | 内置终端 TUI，单机免浏览器 |

---

## 开发

```bash
git clone https://github.com/cheesydui-cloud/kids.git && cd kids

go test ./...
go vet ./...

cd web && npm ci && npm run build && cd ..
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X nft/internal/server.buildVersion=v0.1.0" \
    -o build/nft-server ./cmd/nft-server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X nft/internal/server.buildVersion=v0.1.0" -o build/nft-agent ./cmd/nft-agent
```

依赖：Go ≥ 1.24，Node.js（前端构建），无 CGO。运行时需 nftables + iproute2（仅 Linux）。

推送 `v*` 标签会触发 GitHub Actions：从 [CHANGELOG.md](./CHANGELOG.md) 抽出该版本的升级详情作为 Release 正文，编译 linux/amd64 二进制，并把 `nft-server` / `nft-agent` / `SHA256SUMS` / `install.sh` / `CHANGELOG.md` 一起上传。

**每个版本必须先写 CHANGELOG 再打 tag。** 缺章节或内容过短时发版会失败。模板见 `CHANGELOG.md` 文件头，完整流程见 `.claude/commands/release.md`。

---

## 已知限制

- 限速仅 egress 方向
- 面板单实例（SQLite）
- WSS 不内置 TLS，需反代做 TLS 终结
- 官方预编译二进制目前仅 linux/amd64 与 linux/arm64
