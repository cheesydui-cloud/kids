# Changelog

本文件记录每个正式版本的升级详情。发版规则：

- 每个版本必须先在本文件顶部新增一节 `## vX.Y.Z — YYYY-MM-DD`
- 至少包含「新增 / 修复 / 改进 / 升级注意」中适用的小节，写清用户能感知的变化
- GitHub Release 的说明必须与本节同步上传，不能只贴 commit 列表或空 notes
- 一键安装命令固定不变，默认始终安装 latest

---

## v0.1.9 — 2026-08-14

自定义 Logo 铺满圆角方块，不再缩成绿底里的小图标。

### 修复

- 上传的 Logo 在侧栏、登录页、系统设置预览里会铺满整个圆角方块，不再套默认绿底再缩成小图
- 未上传时仍用原来的绿色徽章 + 默认图标
- 更换 Logo 后立刻刷新预览和浏览器标签，不再吃到旧缓存

### 升级注意

- 只改面板前端展示，节点不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.1.8 — 2026-08-15

节点详情更干净，系统设置可换 Logo，安装/升级会清掉临时文件。

### 改进

- 节点详情去掉说明小字，以及倍率、计费方向、直接转发、节点用途；后端默认仍是倍率 1、双向计费、允许直接转发
- 端口范围预填 `10001-60000`；仍用旧默认 `10001-20000` 的节点升级后面板会自动扩池，自定义范围不动
- 系统设置按「面板 / 转发 / Cloudflare」分 TAB；可更换 Logo，侧栏、登录页和浏览器标签同步
- 安装与升级结束会打印清除步骤，并删掉临时脚本、下载缓存和 `*.bak`；不碰 `/usr/local/sbin/nft-upgrade`

### 修复

- 面板安装先清缓存再写升级脚本时，会把脚本写进已删除的临时目录；现在走独立临时目录

### 升级注意

- **先升级面板**，再重新复制节点安装命令（端口默认已是 `10001-60000`）
- 已自定义端口范围的节点不会被改
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.1.7 — 2026-08-15

修好节点安装：面板地址已经写进脚本，却仍报「缺少面板地址」。

### 修复

- 烘焙面板地址时误把「未设置」判断也替换成了真实 URL，导致正确的 `--panel-url` 反而被拒绝
- 占位符只出现一次；有值就用，不再和烘焙地址做相等比较

### 升级注意

- **先升级面板**，再重新执行节点安装命令
- 面板还没升上来时，临时可把地址写成带斜杠的：`--panel-url http://面板IP:7788/`
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.1.6 — 2026-08-15

修复节点从面板安装时报「缺少面板地址」。

### 修复

- `/v1/install-agent` 用节点刚才访问的地址写入脚本（不再依赖系统设置里的 `panel_url`）
- 节点详情里的安装命令补上 `--panel-url`，旧面板也能装

### 升级注意

- **先升级面板**，再在节点上重新执行详情页命令
- 若暂时不能升级面板，在现有命令里加一行 `--panel-url http://面板IP:7788` 即可
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.1.5 — 2026-08-15

落地仓库可以从 Cloudflare 把当前 A 记录拉回「当前 IP」，不必手抄。

### 新增

- 添加 / 编辑落地节点时，Cloudflare 区域提供 **从 CF 拉取**：按目标域名（或自定义记录名）读取现有 A 记录并填入当前 IP
- 接口 `POST /api/node-repo/cf-lookup`：只读 Cloudflare，不改 DNS、不必先保存节点
- 拉取成功后自动打开 CF 同步开关，便于核对后再写回

### 改进

- 系统设置里的 Cloudflare 说明补充「可拉取、可写回」
- CI 在 `go vet` 前先构建前端，避免仓库不提交 `web/dist` 时 embed 失败

### 升级注意

- 需要先在系统设置填好 Cloudflare API Token 和默认 Zone
- 目标地址必须是域名；裸 IP 没有 CF 记录可拉
- 一键安装命令不变。升级面板：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.1.4 — 2026-08-15

节点 agent 改为从面板安装：脚本和二进制都走面板，不再经过 GitHub。落地仓库增加 Mieru。

### 新增

- 面板提供 `/v1/install-agent`：节点一条 `curl | bash` 即可安装
- 面板按架构下发 `/v1/binary?arch=amd64|arm64`，并带 `X-SHA256` 供校验
- 发版时把 amd64 / arm64 的 `nft-agent` 打进 `nft-server`，节点装机不必访问 GitHub
- 落地仓库支持 **Mieru**：表单填 IP、端口、账号、密码，保存时生成官方 `mierus://user:pass@host?profile=&port=&protocol=TCP`
- 解析识别 `mierus://`（端口在查询参数，含 `port=2012-2022` 区间取起始端口）以及 `mieru://user:pass@host:port`

### 改进

- 节点详情里的安装命令改为从当前面板下载，去掉 gh-proxy 选项
- 旧 `install.sh agent --panel-url …` 也会优先从面板拉二进制
- 推送升级按节点上报的 CPU 架构选择对应 agent
- 复制中继 URI 时会改写 `mierus://` 的主机和全部 `port` 查询项

### 升级注意

- **先升级面板**，再在节点上执行详情页里的新命令
- 节点只需能访问面板地址（例如 `http://31.40.214.186:7788`）
- 官方 `mieru://` 是 protobuf 整包配置，请改贴 `mierus://` 或用表单录入
- 面板仍用原来的固定命令升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.1.3 — 2026-08-15

修复节点 agent 一键安装在 sha256 校验阶段必失败的问题。固定安装命令不变。

### 修复

- 安装脚本下载后把二进制存成 `nft-agent` / `nft-server`，校验却按发布名 `nft-*-linux-amd64` 找文件，导致 `sha256sum: No such file or directory`
- 改为按发布名取出期望哈希，再和本地文件内容比对，不再要求磁盘文件名与资产名一致
- 下载结果小于 1MB 时视为失败（常见是代理返回了 HTML），自动尝试下一个候选资产名

### 升级注意

- 一键安装命令不变。重新跑同一条即可，脚本从 `main` 拉取，不必先升级面板
- 已装上面板的机器如需同步脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.1.2 — 2026-08-14

落地仓库支持 SOCKS5 / Naive 用 IP、端口、账号、密码直接填表，不必再手拼分享链接。

### 新增

- 添加 / 编辑落地节点时，协议可选 **SOCKS5** 或 **Naive**，只需填 IP、端口、账号、密码
- 保存时自动生成分享 URI：`socks5://user:pass@ip:port`、`naive+https://user:pass@ip:port`
- 解析识别 `socks5://`、`socks5h://`、`naive+https://`、`naive+http://`、`naive://`
- 表单粘贴常见 Naive 客户端地址 `https://user:pass@ip:port` 会识别为 Naive（仍拒绝把订阅 URL 当节点入库）

### 改进

- 落地仓库协议改为下拉选择，粘贴 URI 仍会自动填充地址、端口、账号密码
- 批量导入占位提示补充 SOCKS5 / Naive 行格式
- 转发规则仍然只用 IP 和端口，账号密码只写进可复制的分享 URI

### 升级注意

- 一键安装命令不变，默认仍拉 latest
- 裸 `http://` / `https://` 订阅地址不会被当成落地节点；Naive 请用表单或 `naive+https://` 前缀
- 已安装机器升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

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
- `.gitignore` 不再误忽略 `cmd/nft-server` 源码

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
