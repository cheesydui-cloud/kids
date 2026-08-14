# Changelog

本文件记录每个正式版本的升级详情。发版规则：

- 每个版本必须先在本文件顶部新增一节 `## vX.Y.Z — YYYY-MM-DD`
- 至少包含「新增 / 修复 / 改进 / 升级注意」中适用的小节，写清用户能感知的变化
- GitHub Release 的说明必须与本节同步上传，不能只贴 commit 列表或空 notes
- 一键安装命令固定不变，默认始终安装 latest

---

## v0.1.1 — 2026-08-14

安装体验与发布产物加固。固定一键安装命令不变，默认仍拉 latest。

### 新增

- 官方预编译同时提供 **linux/amd64** 与 **linux/arm64**
- `install.sh` 按本机架构自动选择对应二进制
- 安装前自动检测并安装缺失依赖（`curl` / `nftables` / `iproute2`，Debian/Ubuntu）

### 修复

- 精简 VPS 没有 `sudo` 时，安装命令不再依赖 `sudo`（用 root 直接跑 `bash`）
- 面板二进制默认监听地址与安装脚本对齐为 `:7788`，避免手跑 `nft-server` 和一键安装端口不一致

### 改进

- 下载 GitHub 产物增加重试与连接超时，弱网更稳
- 面板从 GitHub 拉取 agent 升级包时限制体积，避免异常响应撑爆内存
- 增加 GitHub Actions CI（`go vet` + `go test`），发版必须带 CHANGELOG 详情

### 升级注意

- 已安装机器直接跑固定命令升级即可：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

- 新安装仍用：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh)
```

- 需要 systemd；非 Debian 系统请先自行安装 nftables / iproute2 / curl

---

## v0.1.0 — 2026-08-14

首发版本。把 nft-server / nft-agent 源码、固定一键安装脚本和 linux/amd64 预编译产物发布到 [cheesydui-cloud/kids](https://github.com/cheesydui-cloud/kids)。

### 新增

- 双二进制架构：`nft-server`（Web 面板 + API + agent hub + SQLite）与 `nft-agent`（daemon + TUI）
- 节点 agent 反向 WebSocket 连入面板，节点无需开放管理端口
- 数据面支持内核态 nftables DNAT 与用户态 split-TCP，可逐跳混用
- 用户态内置 TCP 预连接池，降低多跳握手延迟
- 多租户：用户/节点授权、规则配额、全局与 per-node 流量配额、到期与周期重置
- 组合节点自动编排多跳链路，端口自动分配并对齐
- 落地节点：订阅地址 + `vless://` / `trojan://` 等手动 URI
- Web 面板：节点 / 用户 / 规则管理、连通性探测、节点推送升级、安装命令生成
- 单机 TUI：本机终端管理转发规则，无需浏览器
- 固定一键安装入口 `install.sh`：默认拉 latest，支持 server / agent / tui / update / uninstall / reset-password

### 改进

- 安装与升级走 GitHub Releases + `SHA256SUMS` 强校验，失败自动回滚
- 安装时持久化 gh-proxy，后续 `nft-upgrade` 自动沿用镜像
- 面板安装命令、agent 升级下载地址统一指向本仓库 `cheesydui-cloud/kids`

### 升级注意

- 官方预编译二进制仅 linux/amd64，需要 root + nftables
- 面板不内置 TLS，公网部署请用 Caddy / Nginx 做 HTTPS / WSS 终结
- 固定安装命令（以后任意版本都用这一条）：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh)
```
