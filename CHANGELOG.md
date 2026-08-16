# Changelog

本文件记录每个正式版本的升级详情。发版规则：

- 每个版本必须先在本文件顶部新增一节 `## vX.Y.Z — YYYY-MM-DD`
- 至少包含「新增 / 修复 / 改进 / 升级注意」中适用的小节，写清用户能感知的变化
- GitHub Release 的说明必须与本节同步上传，不能只贴 commit 列表或空 notes
- 一键安装命令固定不变，默认始终安装 latest

---

## v0.3.12 — 2026-08-17

入口已经是域名时，复制规则不再多贴一条裸 IPv6。

### 修复

- 点「复制」或扫二维码，域名入口只出一条链接，不再带 `-v6` 那条
- 订阅同样只收录域名这一条，避免客户端导入两个重复节点
- 入口仍是纯 IPv4 + IPv6 时继续出两条

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端；用户端重新拉一次订阅

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.11 — 2026-08-16

运营概览的实时网速不再把整页顶跳。

### 修复

- 实时上下行改成固定两行，不再并排挤在一格里换行
- 每行按最长常见写法锁死宽度，`B/s` 跳到 `KB/s` / `MB/s` 时格子宽度不变
- 下面的小时流量图不再跟着数字长短上下窜

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.10 — 2026-08-16

运营概览增加全站实时上下行。

### 新增

- 顶部指标增加「实时」：全部规则入口的上行 / 下行，每秒刷新
- 口径与转发规则表相同：每条规则只计入口一跳，多跳不重复累计

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.9 — 2026-08-16

落地仓库把同一当前 IP 的不同协议收成一台机器，改 IP 会一起改。

### 新增

- 同一后端 IPv4 的 ss / vless 等在列表里并成一组，组头显示机器名、IP、CF 状态
- 组内仍是独立协议行：端口、使用人数、复制 / 编辑 / 删除不变
- 改 IP 会同步该 IP 下所有协议，弹窗列出将一起改的条目

### 改进

- 多协议时拖整台机器；组内协议固定 ss 在上、vless 在下
- 没有后端 IP 的条目仍单独一行

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端；分组（干杯云）不用改，按 IP 自动成组

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.8 — 2026-08-16

落地仓库和用户管理支持拖动排列，顺序会记住。

### 新增

- 落地仓库、用户管理桌面列表可拖 `⠿` 调整顺序，刷新后保持
- 新建用户 / 落地节点排在列表末尾
- 搜索、分组过滤或按到期排序时关闭拖动，避免只提交当前可见行

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端；已有顺序不变（用户仍按原 ID，落地仓库仍按原来的新的在前），之后拖过才改

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.7 — 2026-08-16

新建用户时可直接选分组，不用建完再移。

### 新增

- 新建用户弹窗增加「分组」，默认未分组，可选已有分组
- 创建接口接受 `group_id`，管理员也可分组

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.6 — 2026-08-16

用户名片改成更短的交付文案，方便直接发微信 / 飞书。

### 改进

- 去掉规则数、流量配额和空行
- 「面板地址」改为「网址」
- 使用说明改为「看网站公告」

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端再复制名片

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.5 — 2026-08-16

弹窗底部按钮改小、靠右，不再拉满整行。

### 改进

- 新建 / 重命名分组：创建、保存按文字宽度，和取消一起靠右
- 落地仓库保存 / 导入 / 改 IP、公告、分配落地、节点改 IP、新建用户、添加节点、组合节点、创建规则、粘贴授权：统一取消在左、主操作在右

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.4 — 2026-08-16

落地仓库复制链接会换成同步后的域名和当前名称；系统设置里可以直接增删 Cloudflare A 记录。

### 新增

- 系统设置 → Cloudflare：列出默认 Zone 的灰云 A 记录，可添加 / 更新 / 删除，不用再去 CF 网站

### 修复

- 落地仓库导入后即使已同步记录名，复制仍是导入时的 IP 和旧名称
- 保存、列表、复制都会把分享 URI 改成当前名称 + 记录名（目标地址是 IP 时）；规则目标地址仍走原来的 IP

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端；旧节点不用重存，复制即用新域名
- 管理 A 记录前，系统设置里要先保存 Token 和默认 Zone

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.3 — 2026-08-16

使用文档编辑器可改字号、颜色、加粗；点标题即可查看，标题也能单独改色。

### 新增

- 编辑工具栏：加粗（Ctrl/⌘+B）、字号、颜色（预设 + 取色器），选中文字后套用
- 文档标题可单独选颜色，列表、查看弹窗、预览同步显示
- 点列表标题和点「查看」一样打开阅读

### 升级注意

- 这是**面板**改动，升面板即可，节点不用动
- 升完后刷新管理端再编辑文档

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.2 — 2026-08-16

修复「从 CF 同步」报 Invalid request headers，查不到 A 记录。

### 修复

- 调 Cloudflare 的 GET 不再带 `Content-Type`（官方会直接拒）
- 粘贴 Token 时去掉空格、换行、引号、多余的 Bearer 前缀
- 面板升级重启时反代 502 不再钉在「更新」页当错误

### 升级注意

- 这是**面板**修复，升面板即可，节点不用动
- 系统设置里要填好 API Token 和默认 Zone（例如 `halo.ix`）
- 升完后再点「从 CF 同步」

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.1 — 2026-08-16

面板离线租约默认 24 小时，可在系统设置里改。

### 改进

- 默认租约从 2 分钟改为 24 小时：面板关机、升级、过夜重启，转发不会马上断
- 系统设置 → 转发：可填 1–168 小时，保存后推给在线节点
- 重装面板 / token 失效，hello 被拒仍立刻拆入口，不等租约

### 升级注意

- 必须把**转发节点**升到本版，才会认面板下发的时长
- 节点上的 `NFT_PANEL_LEASE` 仍可单独覆盖（例如 `2h`）
- 管理端删转发：在线节点还是立刻停

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.3.0 — 2026-08-16

面板挂了或转发删了，用户端入口会跟着停：节点自己拆监听，不用等面板再推一次空规则。

### 新增

- Agent 对面板连接有 2 分钟租约：断线超时自动清掉 panel 转发，入口端口关掉
- 重装面板 / token 失效导致 hello 被拒时，立刻拆掉残留转发，不等租约

### 改进

- 管理端删转发：在线节点仍立刻推空；离线节点重连后对上当前规则，连不上则租约到期自己停
- 本机 TUI / 单机规则不受影响，只拆面板下发的那段

### 升级注意

- 必须把**转发节点**升到本版，旧 agent 仍会在面板死后继续转
- 面板短暂重启一般 2 分钟内连回，用户无感；需要更长窗口可设 `NFT_PANEL_LEASE`（例如 `5m`）
- 客户端缓存的订阅链接还在，但入口端口关了就会连不上

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.32 — 2026-08-16

系统设置里可以检查更新、一键升级面板；节点「全部升级」不再卡住等每一台下完包。

### 新增

- 系统设置增加「更新」页：看当前版 / GitHub 最新版 / 发布说明
- 点「立即升级」在面板里跑官方 `install.sh update`，不用再 SSH

### 改进

- 节点一键全部升级：已是最新的跳过，离线立刻失败，在线只推指令就返回
- 各节点自己从面板下载二进制，按钮马上出「已推送 / 跳过 / 失败」

### 升级注意

- 面板升级不改数据库、不改节点、不会给本机 daemon 加上 `--connect`
- 这一版装上之后，以后可以一直在设置页升级
- 第一次仍需 SSH 升到本版：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.31 — 2026-08-16

管理面板去掉顶栏那条空横条：页面标题抬到顶栏左边，内容和开关同一行。

### 改进

- 「运营概览」等标题进顶栏，不再在下面再画一遍
- 内容区贴顶栏，中间不再空出一条带底线的空白
- 系统设置、账户设置、节点/规则详情同样把标题放到顶栏

### 升级注意

- 只改面板，节点 agent 不用动
- 用户端页面不受影响
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.30 — 2026-08-15

落地「已用」按账户倍率显示，超额停转也按这个数字，跟顶栏流量同一套算法。

### 改进

- 用户详情落地表格的已用改为实际 × 账户倍率
- 落地限额用完按倍率后的已用判断，显示满了才停这条落地的转发
- 落地仓库用户列表的流量数字同样按倍率显示

### 升级注意

- 只改面板，节点 agent 不用动
- 账本仍记原始流量，限额本身不乘倍率
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.29 — 2026-08-15

运营概览补上「本月」：当月最后一跳实际流量合计，不乘倍率，北京时间每月 1 号 0 点自然切月。

### 新增

- 概览 KPI 增加「本月」，与「今日」同一本用户最后一跳账
- 本月不乘计费倍率，只看实际用量
- 按北京时间自然月统计，1 号 0 点起只算当月，不用另清零

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.28 — 2026-08-15

运营概览的「计费」和「今日」改成跟用户详情同一本账，不再把入口和落地加两遍，也不再把今日乘倍率。

### 修复

- 「今日」改为各用户最后一跳实际流量合计，不乘倍率
- 「计费」改为各用户累计实际流量 × 各自倍率
- 不再用节点日账做概览今日，避免一条路径上的多个节点重复加总

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.27 — 2026-08-15

24 小时流量图拉满卡片宽度，整点刻度一次标齐，悬停十字线按真实坐标即时跳。

### 修复

- 宽屏两侧留白不再把鼠标坐标算偏，竖线不会再慢半拍停在左边

### 改进

- X 轴拉满卡片，24 个整点都标出来
- 蓝轴和橙线加粗一倍
- 图表加高，刻度不再挤在一起

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.26 — 2026-08-15

24 小时流量图的悬停十字线贴整点即时跳，坐标轴和曲线换成更清楚的配色。

### 改进

- 悬停竖线卡在整点上，鼠标到哪一格立刻跳过去，没有过渡
- X 轴改成蓝色
- 流量曲线改成阿里橙

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.25 — 2026-08-15

24 小时流量图的坐标轴和悬停十字线单独画，不再跟曲线缠在一起。

### 改进

- X 轴单独拉长画，两端比曲线多出一点
- 流量曲线比坐标轴细，平滑时不会穿到 0 轴下面
- 悬停竖线跟着鼠标左右走，圆点和提示仍对准最近整点

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.24 — 2026-08-15

运营概览收成指标条 + 24 小时流量曲线，空日历、节点表和代理 URI 编辑器不再占一整屏。

### 新增

- 概览增加近 24 小时实际流量曲线（北京时间整点，不含倍率）
- 悬停显示该小时时间和流量

### 改进

- 概览只保留计费、今日流量、用户三项
- 去掉活跃转发、在线节点、到期空日历、节点状态表和「我的代理 URI」
- 「需关注」和 7 天内到期只在有事项时出现

### 升级注意

- 只改面板，节点 agent 不用动
- 升级前没有按小时记账，曲线会从升级后开始鼓起来
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.23 — 2026-08-15

定版前修掉创建用户丢倍率、到期日按 UTC 切、落地源清空残留、订阅覆盖仓库节点，以及本机节点仍能被接口改动的问题。

### 修复

- 创建用户会保存计费倍率，不再只在编辑资料时生效
- 账号到期按北京时间当天 23:59:59 计算，不再写成 UTC 零点
- 清空落地订阅 / 手工 URI 后，自动落地行会一起撤下
- 订阅同步撞上仓库导入的同一 host:port 时，不再改写仓库节点
- 周期重置流量时，规则跳点累计也会清零，和下次手动重置一致
- 单向节点最后一跳的下行仍计入用户流量，测试断言跟生产对齐

### 改进

- `/api/dashboard`、`/api/sub-fetch`、`/api/node-roles` 改为仅管理员
- 本机 `self` 节点不能删除、停用、重置凭证、远程升级或授权给用户，也不能塞进组合节点
- 用户列表和运营概览「需关注」按计费后流量判断额度
- 规则页筛选 / 创建不再出现本机节点和内置 admin
- 用户名片说明改成「我的订阅」

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.22 — 2026-08-15

线路监控和用户管理不再展示面板自带的本机节点和 admin 账号。

### 改进

- 线路监控隐藏本机 `self` 节点，单点计数和运营概览节点列表一并去掉
- 用户管理隐藏内置 `admin` 账号，分组「全部 / 未分组」计数不再把它算进去
- 授权节点、粘贴授权、公告选用户时也不再出现本机节点和 admin
- 本机节点和 admin 账号仍保留在系统里，拖拽排序不会把 `self` 弄丢

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.21 — 2026-08-15

用户详情日用量不再标倍率，落地仓库复制按钮去掉多余图标。

### 改进

- 用户详情「今日 / 昨日」卡片去掉 ×倍率标签，数字仍按计费后流量显示
- 概览里的倍率和单独的计费倍率卡保持不动
- 落地仓库操作列「复制」不再带旁边的小方块图标

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.20 — 2026-08-15

订阅页流量始终按计费倍率显示，和客户端、后台对齐。

### 修复

- 用户订阅页不再看「显示倍率」开关，已用流量固定为原始用量 × 该用户计费倍率
- 挡导入的流量判断也用同一套计费数字，和客户端 `Subscription-Userinfo`、后台配额一致

### 升级注意

- 只改面板，节点 agent 不用动
- 系统设置里的「显示倍率」仍只控制节点/链路倍率是否展示给用户
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.19 — 2026-08-15

登录页「记住账号密码」靠左，和用户名、密码对齐。

### 改进

- 记住账号密码不再居中，和上方输入框左对齐

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.18 — 2026-08-15

登录按钮缩小并和输入框区分开。

### 改进

- 登录不再拉满整卡，按钮居中放在「记住账号密码」上面
- 登录按钮用浅陶土描边和字色，和灰色输入框分开，仍不是实心主色块

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.17 — 2026-08-15

手机订阅页把账户、流量、到期收成一张卡。

### 改进

- 账户概览不再叠三张空盒子；手机上一行标签、一行数值，桌面仍是三列合在一块

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.16 — 2026-08-15

客户端订阅信息里的已用流量按计费倍率显示。

### 修复

- Clash / Mihomo / URI 订阅头 `Subscription-Userinfo` 的 `download` 改为「已用 × 计费倍率」，和面板配额、用户详情同一套数字
- 倍率未设或 ≤ 0 时仍按 1 倍计算，不会把用量写成 0

### 升级注意

- 只改面板，节点 agent 不用动
- 客户端刷新订阅后，用量会按倍率放大；流量上限 `total` 仍是账号配额本身
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.15 — 2026-08-15

订阅页手机适配，节点卡只留状态和延迟，导入受到期/流量限制。

### 改进

- 用户端适配手机：顶栏竖排、节点单列、导入两列、安全区留白，复制按钮不再拉满整卡
- 节点卡去掉「复制链接」，只保留名称、在线状态和到谷歌延迟
- 节点显示名改为「用户名-规则名-到期时间」，订阅 URI 和管理端复制同步
- 公告芯片显示未读红点；延迟 45 秒缓存，清单旁可「刷新延迟」
- 账号过期、流量用完或被禁用时，一键导入和复制订阅不可用，并说明原因
- 账户设置去掉外层大卡片，和订阅页同一套页面结构

### 升级注意

- 只改面板，节点 agent 不用动
- 已导入的客户端节点名会在下次更新订阅后变成「用户名-规则名-到期」
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.14 — 2026-08-15

一键导入能真正唤起客户端，Clash / Mihomo 会打包全部节点。

### 修复

- Clash YAML 补上 socks5 入口，不再只留下一个能转换的节点
- Clash Verge 只唤起一次 `clash://install-config`，`name` 放在 `url` 前面，避免订阅地址被截断、页面误报复制失败
- V2rayN 没有系统级导入协议，改为复制全部节点订阅地址，到客户端订阅里添加

### 改进

- 账户设置去掉会话说明，增加返回主页
- 模块标题改为「一键导入客户端」，四个按钮换成官方图标

### 升级注意

- 只改面板，节点 agent 不用动
- 已导入过的 Clash 配置请重新点一次一键导入，或手动更新订阅
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.13 — 2026-08-14

用户订阅页再收一层：去掉大表头和重置入口，节点改成卡片并显示到谷歌的延迟。

### 改进

- 页面贴顶，logo 放在「我的订阅」前面，不再留 SUBSCRIPTION 大标题和独立顶栏
- 三个芯片改为公告、账户设置、退出账号；公告对接管理员面板发给当前用户的通知
- 去掉「重置订阅地址」和「中转 · N / 直连 · N」分组标题
- 节点清单改成和一键导入一样的卡片网格，每张卡显示在线状态，以及出口节点 TCP 探测 `www.google.com:443` 的延迟

### 升级注意

- 只改面板，节点 agent 不用动
- 延迟由末跳 agent 实测，直连落地没有 agent，卡片上显示「—」
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.12 — 2026-08-14

修正一键升级把旧版本校验和拿来验新二进制的问题。

### 修复

- `update` 先解析出具体 tag，再从该 tag 下载二进制和 SHA256SUMS，不再一边显示 v0.2.10、一边从 latest 拉新包
- 解析 latest 时先问官方 GitHub API，避免镜像把旧的 latest 跳转缓存下来
- 二进制和校验和必须来自同一版本，交叉缓存时会直接失败而不是装错包

### 升级注意

- 这次改的是安装脚本本身，请用仓库 `main/install.sh` 再跑一次 update
- 节点 agent 不用单独重装；面板机跑 update 即可
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.11 — 2026-08-14

用户端去掉侧栏，订阅页只留必要信息，节点清单下方可一键导入全部节点。

### 改进

- 普通用户不再显示左侧栏，顶栏只留品牌、账户设置和退出
- 订阅页去掉说明小字，节点清单不再堆协议标签，改为显示在线 / 离线 / 直连状态
- 节点清单下方单独一块「一键导入」，点小火箭 / V2rayN / Clash Verge / Mihomo 导入全部节点
- 打不开对应客户端时自动复制订阅地址，方便手动添加

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.10 — 2026-08-14

Clash Verge / Mihomo 订阅会打包用户全部落地规则，不再只出第一个节点。

### 修复

- 落地索引不再扫到第一条就返回，多条落地规则都会进订阅
- Clash YAML 与 URI 订阅同步收录全部中转节点，直连落地仍单独打包
- 自定义 / 未生成入口 / 已停用的规则仍跳过，不误伤其它线路

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级后在客户端重新拉取一次订阅即可看到全部节点
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.9 — 2026-08-15

用户端整页改成订阅中心：小火箭扫码、V2rayN 复制链接、Clash Verge / Mihomo 拉 YAML。

### 新增

- 普通用户登录后只剩「我的订阅」：小火箭二维码、V2rayN 订阅/节点链接、Clash Verge 订阅地址、Mihomo YAML 下载与拉取地址
- 订阅收录已启用且能改写到入口的落地中转规则（多条规则各一条；有 IPv6 入口再多一条），以及管理员标记为「直连」的落地原始 URI
- 自定义 host:port、尚未生成入口、已停用的规则不进订阅，页面会说明原因
- 每名用户有独立明文订阅口令，可重置；旧地址立刻失效

### 改进

- 旧的用户概览 / 规则 / 落地 / 文档入口全部跳回订阅页

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.8 — 2026-08-15

落地仓库和线路监控可按 IP 从 Cloudflare 同步域名到记录名；用户详情规则表拉开列距。

### 新增

- 落地仓库「从 CF 同步」按当前 IP 反查 A 记录，自动填入记录名
- 线路监控 Cloudflare 配置增加「从 CF 同步」，按中继 / 当前 IP 填入记录名

### 改进

- 用户详情「规则实时网速」列宽和单元格留白加大，名称、节点、实时、用量不再挤在一起

### 升级注意

- 只改面板，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.7 — 2026-08-15

修正侧栏悬停、分组删除位置，以及用户规则用量只留一列。

### 修复

- 侧栏导航恢复原来的无描边样式，鼠标悬停会变色
- 侧栏字号按比例略加大，组间距和行高收紧，不再挤成一排描边胶囊
- 「删除」按钮改到分组下拉后面，不再跟在「新建分组」后面
- 用户详情「规则实时网速」去掉「真实用量」，只留按倍率显示的用量

### 升级注意

- 只改面板前端，节点 agent 不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.6 — 2026-08-15

主按钮不再单独上色；分组可删；搜索框不再压字；用户列表去掉行内操作；删除一律红色并二次确认。

### 改进

- 主按钮颜色与其它按钮一致，都是奶油描边，悬停上浮变赤陶
- 节点详情启用/禁用/同步/升级、概览在线徽标、登录与设置错误条改成描边，不再铺色块
- 落地仓库和用户管理的分组下拉旁始终有「删除分组」；没选中具体分组时先弹出选择
- 搜索框缩小，放大镜单独占位，不再和占位文字重叠
- 用户列表桌面端去掉名片/禁用/重置密码/删除那一排图标，点整行进详情
- 面板所有删除文字和按钮改为红色；点删除先出确认卡片，确认后才执行
- 左侧栏导航字号略加大

### 升级注意

- 只改面板前端，节点 agent 不用动
- 用户的禁用、重置密码、删除改到用户详情页操作
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.5 — 2026-08-15

面板改成奶油纸描边风；使用文档可直接查看复制；用户端不再显示文档；分组改下拉；登录可记住账号。

### 改进

- 按钮去掉薄荷绿填充，改成加粗描边；主按钮用赤陶描边，悬停上浮变色
- 页面底色、侧栏、表格和输入框统一成奶油纸 + 赤陶字色
- 使用文档列表「下架」改成「查看」，弹窗里可直接复制命令
- 用户端侧栏去掉「使用文档」，旧地址会跳回「我的概览」
- 落地仓库和用户管理的分组从一排芯片改成下拉筛选
- 登录页增加「记住账号密码」，卡片做成悬浮阴影
- 不再请求 Google Fonts，离线面板用系统中文黑体

### 升级注意

- 只改面板前端，节点 agent 不用动
- 「记住账号密码」存在本机浏览器，换设备或清站点数据后要重新勾选
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.4 — 2026-08-14

去掉面板全部限速；使用文档里的命令可一键复制；用户态中继关闭 Nagle。

### 改进

- 新建用户、用户配置、授权节点、粘贴授权、用户名片和「我的」面板都不再出现限速
- 规则下发不再按授权/全局 Mbps 整形，也不会因为残留限速把 TCP 逼到用户态
- 限速写入接口已下线；批量授权即使带上旧字段也不会再写入 Mbps
- 使用文档里三个反引号包起来的命令，预览和用户端都是黑底框，可一键复制
- 用户态中继套接字开启 TCP_NODELAY，小包不再被 Nagle 攒一轮再发

### 升级注意

- 库里旧的限速数字会留着，但不会再生效
- 限速下线和文档命令框只改面板
- 用户态中继的 TCP_NODELAY 需要节点 agent 也升到本版；内核态转发不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.3 — 2026-08-14

用户详情去掉重复的落地来源/订阅栏，规则用量只留一栏；组合跳延迟按真实中继端口做 TCP 探测。

### 修复

- 用户落地页去掉「落地节点来源」标题和订阅地址栏；到期日期选好即保存，不用再点「设」
- 从落地仓库导入后会标成落地，创建规则时立刻能选到，不用整页刷新
- 规则表用量只留一栏，按计费倍率显示（默认 1 倍）
- 线路监控去掉工具栏 agent 版本和角色筛选
- 组合节点相邻两跳改为测已下发规则的真实监听端口，完整 TCP 握手才算通（拒连也算不通）

### 升级注意

- 跳间测延迟仍走 TCP，需要这两跳之间已经有规则；没有规则会提示无法按真实中继端口测试
- 只改面板，节点不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.2 — 2026-08-14

落地仓库目标填 IP 时，也能用「记录名」从 Cloudflare 拉取和同步域名。

### 修复

- 目标地址是 IP 时，「记录名」不再灰掉；把要同步的域名填进去即可拉取 / 保存
- 开启 CF 同步不再要求目标本身必须是域名；域名写在记录名即可
- 从 CF 拉取、改 IP、重新同步都按记录名写 A 记录，不会把 IP 当成域名去查

### 升级注意

- 系统设置里仍要先填 Cloudflare Token 和默认 Zone
- 目标是 IP 时必须填写记录名（例如 `node.example.com`），再点「从 CF 拉取」或打开同步开关保存
- 只改面板，节点不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.2.1 — 2026-08-15

用户详情顶栏吸顶不再露底，组合节点可测相邻两跳延迟。

### 修复

- 用户详情身份条贴到顶栏下方，滚动时用面板底色盖住后面的卡片，不再透出页面

### 新增

- 线路监控组合节点的相邻两跳之间可测延迟：第一跳 agent 测到第二跳中继地址的 RTT（端口未开也算通）

### 升级注意

- 跳间测延迟需要节点 agent 也升到本版；只升面板、旧 agent 仍会走原来的连通性探测
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

---

## v0.1.10 — 2026-08-14

用户详情去掉重复信息，流量只按你设的倍率显示。

### 改进

- 概览去掉「基本信息」，以及和顶部标签重复的剩余额度、规则使用、到期时间
- 流量不再同时显示真实和用户视角，只留一栏；默认 1 倍，改过倍率后按倍率显示
- 今日 / 昨日同样只显示一栏，倍率不是 1 时才标 ×N

### 升级注意

- 只改面板前端，节点不用动
- 面板升级：

```bash
curl -fsSL https://raw.githubusercontent.com/cheesydui-cloud/kids/main/install.sh | bash -s update
```

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
