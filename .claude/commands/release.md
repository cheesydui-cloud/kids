# 发版部署

对 kids 执行完整的发版 → GitHub Release → 远程部署流程。

**硬性要求：每个版本必须先写详细升级内容，再和版本一起上传。** 没有 `CHANGELOG.md` 对应章节，禁止打 tag / 禁止发 Release。

## 前置检查

1. 确认当前在 `main` 分支且工作区干净
2. 运行 `go vet ./...` 和 `go test ./...`，全部通过才继续

## 确定版本号

1. 查看最近 tag：`git tag --sort=-v:refname | head -5`
2. 查看自上次 tag 以来的 commits：`git log <last-tag>..HEAD --oneline`
3. 根据变更类型决定版本号（**严格三段 vX.Y.Z，禁止四段及以上**）：
   - 新功能：minor bump（如 v0.9.0 → v0.10.0）
   - 修复/改进/小修补：patch bump（如 v0.10.0 → v0.10.1）

## 先写升级详情

在 `CHANGELOG.md` **最顶部**（文件头说明之后）新增一节，模板：

```markdown
## vX.Y.Z — YYYY-MM-DD

一句话说明这次升级解决什么问题 / 带来什么能力。

### 新增
- ...

### 修复
- ...

### 改进
- ...

### 升级注意
- 兼容性、需要重启的服务、配置迁移、回滚方式
```

要求：

- 标题必须精确匹配即将打的 tag（如 `## v0.2.0 — 2026-08-20`）
- 用用户能看懂的中文，写清行为变化，不要只贴 commit hash
- 至少 3 条 `- ` 列表项；不适用的小节可以省略
- 提交 CHANGELOG **之后**再打 tag

校验（发版前必跑，失败就停）：

```bash
scripts/release-notes.sh vX.Y.Z
```

## 打 Tag 并推送

**必须先打 tag 再编译 nft-server**，否则 Go 的 VCS stamping 拿不到正确版本号。

```bash
git add CHANGELOG.md
git commit -m "Release vX.Y.Z changelog"
git tag -a vX.Y.Z -m "vX.Y.Z — 英文单行摘要"
git push origin main --tags
```

Tag message 用英文单行。推送 `v*` tag 后，GitHub Actions 会：

1. 从 `CHANGELOG.md` 抽出该版本全文作为 Release notes（缺章节或过短则构建失败）
2. 编译 linux/amd64 二进制
3. 把带架构后缀的二进制、兼容旧脚本的无后缀 amd64 副本、`SHA256SUMS`、`install.sh`、`CHANGELOG.md` 一并上传

## 本地补发（无 Actions 时）

```bash
cd web && npm run build && cd ..
rm -f build/nft-server build/nft-agent
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -X nft/internal/server.buildVersion=vX.Y.Z" \
  -o build/nft-server ./cmd/nft-server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false \
  -ldflags="-s -w -X nft/internal/server.buildVersion=vX.Y.Z" \
  -o build/nft-agent ./cmd/nft-agent
(cd build && shasum -a 256 nft-server nft-agent > SHA256SUMS)

scripts/release-notes.sh vX.Y.Z > /tmp/kids-notes.md
gh release create vX.Y.Z \
  --title "vX.Y.Z" \
  --notes-file /tmp/kids-notes.md \
  --latest \
  build/nft-server build/nft-agent build/SHA256SUMS install.sh CHANGELOG.md
```

禁止用空 notes 或 `generate_release_notes` 代替 CHANGELOG。

## 部署到服务器

```bash
/usr/bin/ssh hosthatch-jp "nft-upgrade"
```

upgrade 脚本会自动：下载 latest → sha256 校验 → 备份 → 原子替换 → 重启 daemon + server → health-check。失败自动回滚。

## 验证

- Release 页面正文与 `CHANGELOG.md` 该版本章节一致
- assets 含 `nft-server-linux-{amd64,arm64}`、`nft-agent-linux-{amd64,arm64}`、无后缀 amd64 副本、`SHA256SUMS`、`install.sh`、`CHANGELOG.md`
- 部署输出含 `sha256: OK`、`Update 完成`

## 约束

- 没有对应 CHANGELOG 章节，不得发版
- nft-server 版本号经 buildinfo / `-X nft/internal/server.buildVersion`
- nft-agent 用 `-buildvcs=false` 可复现构建
- tag 必须用 annotated（`-a`）
- release 必须标为 Latest，install.sh 默认拉 latest
- 不要在 commit message 或 release notes 中包含过程信息（任务编号、方案代号、审阅轮次）
