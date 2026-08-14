import { useState, useEffect, useRef } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { api } from '../../lib/api'
import { fmtTime, fmtBytes, nullStr } from '../../lib/fmt'
import { Layout, useToast, useBlur } from '../../components/Layout'
import { Loading, Empty, Badge, ProtoBadge, ModeBadge, SensText, NodeTypeBadge, NodeStackBadge, useConfirm, Select, Modal, CopyText, Spinner } from '../../components/ui'
import { TableBox } from '../../components/page'
import { copyToClipboard } from '../../lib/clipboard'

const card = 'bg-surface border border-line rounded-[14px] shadow-[0_1px_2px_rgba(16,24,40,0.04)]'

function semverLT(a, b) {
  const pa = (a || '').replace(/^v/, '').split('.').map(Number)
  const pb = (b || '').replace(/^v/, '').split('.').map(Number)
  for (let i = 0; i < 3; i++) {
    if ((pa[i] || 0) !== (pb[i] || 0)) return (pa[i] || 0) < (pb[i] || 0)
  }
  return false
}

// The version label is not always reliable (older installers wrote "latest");
// use the binary SHA as the authoritative identity when both sides provide it.
function agentIsOutdated(node, latestVersion, latestSHA) {
  if (node.agent_sha && latestSHA && node.agent_sha === latestSHA) return false
  if (node.agent_version === 'latest') return false
  return node.agent_version && semverLT(node.agent_version, latestVersion)
}

// Resolve a usable version label for display. If the daemon reports "latest"
// but its SHA matches the server's target agent SHA, show the concrete tag.
function displayAgentVersion(node, latestVersion, latestSHA) {
  if (node.agent_version && node.agent_version !== 'latest') return node.agent_version
  if (node.agent_sha && latestSHA && node.agent_sha === latestSHA) return latestVersion
  return node.agent_version || '未知'
}

export default function NodeDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [name, setName] = useState('')
  const [relayHost, setRelayHost] = useState('')
  const [relayHostV6, setRelayHostV6] = useState('')
  const [portRange, setPortRange] = useState('10001-60000')
  const [cmdRelayHost, setCmdRelayHost] = useState('')
  const [cmdRelayHostV6, setCmdRelayHostV6] = useState('')
  // CF DNS for line entry domain (same idea as 落地仓库).
  const [cfSync, setCfSync] = useState(false)
  const [backendIP, setBackendIP] = useState('')
  const [cfRecordName, setCfRecordName] = useState('')
  const [cfZoneID, setCfZoneID] = useState('')
  const [changeIPOpen, setChangeIPOpen] = useState(false)
  const [changeIPVal, setChangeIPVal] = useState('')
  const [cfBusy, setCfBusy] = useState(false)
  // revealedSecret holds the one-time plaintext token returned by a reset; it
  // is never persisted server-side and is cleared when the reveal modal closes.
  const [revealedSecret, setRevealedSecret] = useState('')
  const toast = useToast()
  const blurred = useBlur()
  const confirm = useConfirm()
  const upgradePollRef = useRef(null)

  const applyData = (d) => {
    setData(d)
    setName(d.node?.name || '')
    setRelayHost(d.node?.relay_host || '')
    setRelayHostV6(d.node?.relay_host_v6 || '')
    setPortRange(d.node?.port_range || '10001-60000')
    setCfSync(!!d.node?.cf_sync)
    setBackendIP(d.node?.backend_ip || '')
    setCfRecordName(d.node?.cf_record_name || '')
    setCfZoneID(d.node?.cf_zone_id || '')
  }
  const load = () => {
    setLoading(true)
    api.get(`/nodes/${id}`).then(applyData).catch(console.error).finally(() => setLoading(false))
  }
  // Refresh without the full-page Loading swap: load() unmounts every card,
  // which would wipe in-progress child edits (e.g. binding rows) — the silent
  // path keeps them mounted and just refreshes the data underneath.
  const reloadSilent = () => api.get(`/nodes/${id}`).then(applyData).catch(console.error)
  useEffect(load, [id])

  useEffect(() => () => {
    if (upgradePollRef.current) { clearInterval(upgradePollRef.current); upgradePollRef.current = null }
  }, [id])

  // 离线的实体节点按指数退避静默轮询：首次 3 秒，之后 6s / 12s / 24s / 30s 封顶。
  // agent 一连上面板，offline 状态翻转为 false，effect 清理停止；轮询期间只更新
  // 展示数据不动表单，避免覆盖正在编辑的输入。
  const offline = !!data?.node && data.node.node_type !== 'composite' && data.node.online !== 1
  useEffect(() => {
    if (!offline) return
    let timeout = null
    let delay = 3000
    const tick = () => {
      api.get(`/nodes/${id}`).then(d => {
        if (d?.node?.online === 1) { applyData(d); toast('节点已上线') }
        else setData(d)
      }).catch(() => { /* 网络抖动，等下一轮 */ })
      delay = Math.min(delay * 2, 30000)
      timeout = setTimeout(tick, delay)
    }
    timeout = setTimeout(tick, delay)
    return () => clearTimeout(timeout)
  }, [offline, id])

  if (loading) return <Layout><Loading /></Layout>
  if (!data) return <Layout><Empty title="节点不存在" /></Layout>

  const { node, panel_url, panel_url_configured, latest_agent_version, latest_agent_sha } = data
  const ruleHops = data.rule_hops || []
  const nodeHops = data.node_hops || []
  const grantedUsers = data.granted_users || []
  const isComposite = node.node_type === 'composite'
  const agentOutdated = agentIsOutdated(node, latest_agent_version, latest_agent_sha)
  const up = data.upgrade || { status: 'none' }
  const showUpgrade = up.status !== 'none'

  const saveName = async (e) => {
    e.preventDefault()
    try { await api.post(`/nodes/${id}/rename`, { name }); toast('名称已保存'); load() } catch (err) { toast(err.message, 'error') }
  }
  const saveRelay = async (e) => {
    e.preventDefault()
    try { await api.post(`/nodes/${id}/relay-host`, { relay_host: relayHost }); toast('中继地址已保存'); load() } catch (err) { toast(err.message, 'error') }
  }
  const saveRelayV6 = async (e) => {
    e.preventDefault()
    try { await api.post(`/nodes/${id}/relay-host-v6`, { relay_host_v6: relayHostV6 }); toast('IPv6 中继地址已保存'); load() } catch (err) { toast(err.message, 'error') }
  }
  const notifyCF = (cf) => {
    if (!cf || cf.skipped) return
    if (cf.ok) toast(cf.message || `CF 已同步 ${cf.record || ''} → ${cf.ip || ''}`.trim())
    else toast(`CF 同步失败：${cf.message || '未知错误'}`, 'error')
  }
  const saveCF = async (e) => {
    e.preventDefault()
    setCfBusy(true)
    try {
      const res = await api.post(`/nodes/${id}/cf`, {
        cf_sync: !!cfSync,
        backend_ip: backendIP.trim(),
        cf_record_name: cfRecordName.trim(),
        cf_zone_id: cfZoneID.trim(),
      })
      toast('CF 配置已保存')
      notifyCF(res?.cf_sync)
      applyData({ ...data, node: res?.node || data.node })
      load()
    } catch (err) { toast(err.message, 'error') }
    finally { setCfBusy(false) }
  }
  const resyncCF = async () => {
    setCfBusy(true)
    try {
      const res = await api.post(`/nodes/${id}/cf-resync`, {})
      notifyCF(res?.cf_sync)
      if (res?.node) applyData({ ...data, node: res.node })
      else load()
    } catch (err) { toast(err.message, 'error') }
    finally { setCfBusy(false) }
  }
  const submitChangeIP = async (e) => {
    e.preventDefault()
    const v = changeIPVal.trim()
    if (!/^\d{1,3}(\.\d{1,3}){3}$/.test(v)) { toast('请填写合法 IPv4', 'error'); return }
    setCfBusy(true)
    try {
      const body = { backend_ip: v }
      // Domain relay + first-time change IP → auto enable CF like 落地仓库.
      const rh = (node?.relay_host || '').trim()
      const looksDomain = rh && !/^\d{1,3}(\.\d{1,3}){3}$/.test(rh) && !rh.includes(':')
      if (looksDomain && !node?.cf_sync) body.cf_sync = true
      const res = await api.post(`/nodes/${id}/backend-ip`, body)
      toast('IP 已更新')
      notifyCF(res?.cf_sync)
      setChangeIPOpen(false)
      if (res?.node) applyData({ ...data, node: res.node })
      load()
    } catch (err) { toast(err.message, 'error') }
    finally { setCfBusy(false) }
  }
  const savePortRange = async (e) => {
    e.preventDefault()
    try { await api.post(`/nodes/${id}/port-range`, { port_range: portRange }); toast('端口范围已保存'); load() } catch (err) { toast(err.message, 'error') }
  }
  const resetToken = async () => {
    if (!(await confirm({ title: '重置 Token', message: '重置后旧 Token 立即失效，正在运行的 Agent 会在下次重连时被拒绝，需用新 Token 重新配置并重装。', confirmText: '重置', danger: true }))) return
    try {
      const d = await api.post(`/nodes/${id}/reset-token`)
      setRevealedSecret(d.secret || '')
      // Reload so the Token row and install command pick up the new plaintext.
      reloadSilent()
      toast('Token 已重置')
    } catch (err) { toast(err.message, 'error') }
  }
  const resync = async () => {
    try { await api.post(`/nodes/${id}/resync`); toast('已发起同步') } catch (err) { toast(err.message, 'error') }
  }
  const upgrade = async () => {
    if (!(await confirm({ title: '升级节点', message: '推送 agent 二进制到此节点并重启？', confirmText: '升级' }))) return
    try {
      await api.post(`/nodes/${id}/upgrade`); toast('已发起升级')
      if (upgradePollRef.current) { clearInterval(upgradePollRef.current); upgradePollRef.current = null }
      const poll = setInterval(async () => {
        try {
          const d = await api.get(`/nodes/${id}`)
          const st = d?.upgrade?.status
          if (st === 'ok' || st === 'error') {
            clearInterval(poll)
            upgradePollRef.current = null
            toast(st === 'ok' ? '升级成功' : '升级失败: ' + (d.upgrade.error || ''), st === 'ok' ? undefined : 'error')
            load()
          }
        } catch { clearInterval(poll); upgradePollRef.current = null }
      }, 2000)
      upgradePollRef.current = poll
      setTimeout(() => { clearInterval(poll); upgradePollRef.current = null }, 120000)
    } catch (err) { toast(err.message, 'error') }
  }
  const toggle = async () => {
    try { await api.post(`/nodes/${id}/toggle`); toast(node.disabled ? '已启用' : '已禁用'); load() } catch (err) { toast(err.message, 'error') }
  }

  const remove = async () => {
    if (!(await confirm({ title: '删除节点', message: `删除节点「${node.name}」？经过它的规则会被重新连接或清除，此操作不可撤销。`, confirmText: '删除', danger: true }))) return
    try { await api.del(`/nodes/${id}`); toast('节点已删除'); navigate('/nodes') } catch (err) { toast(err.message, 'error') }
  }

  // Normalize panel_url: ensure protocol prefix, detect insecure scheme for --insecure flag.
  let normalizedPanelUrl = (panel_url || '').trim()
  let needsInsecure = false
  if (!normalizedPanelUrl) {
    // Fallback to current browser origin if panel_url is not configured.
    normalizedPanelUrl = window.location.origin
  }
  if (!normalizedPanelUrl.startsWith('http://') && !normalizedPanelUrl.startsWith('https://')) {
    // User entered bare IP:port or hostname — default to http.
    normalizedPanelUrl = 'http://' + normalizedPanelUrl
  }
  if (normalizedPanelUrl.startsWith('http://') && !normalizedPanelUrl.startsWith('https://')) {
    needsInsecure = true
  }

  const portRangePart = node.port_range && node.port_range !== '10001-60000'
    ? ` \\\n  --port-range ${node.port_range}`
    : ' \\\n  --port-range 10001-60000'
  const relayHostPart = cmdRelayHost.trim() ? ` \\\n  --relay-host ${cmdRelayHost.trim()}` : ''
  const relayHostV6Part = cmdRelayHostV6.trim() ? ` \\\n  --relay-host-v6 ${cmdRelayHostV6.trim()}` : ''
  const insecurePart = needsInsecure ? ' \\\n  --insecure' : ''
  // Tokens are stored in plaintext, so node.secret carries the real value for
  // the command. revealedSecret (a fresh reset) takes priority before the reload
  // lands. Legacy v3.0.0 nodes (secret_legacy) have an unusable hashed value —
  // node.secret is empty there, so prompt a reset instead.
  const tokenForCmd = revealedSecret || node.secret || (node.secret_legacy ? '<请先点“重置 Token”>' : '')
  const installCmd = `curl -fsSL ${normalizedPanelUrl}/v1/install-agent | bash -s -- \\\n  --panel-url ${normalizedPanelUrl} \\\n  --token ${tokenForCmd}${portRangePart}${relayHostPart}${relayHostV6Part}${insecurePart}`

  return (
    <Layout>
      <div className="mx-auto max-w-[1160px] flex flex-col gap-[18px]">

        {/* back link */}
        <Link to="/nodes" className="inline-flex items-center gap-1.5 w-fit text-[13.5px] text-ink-mut hover:text-ink transition-colors">
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M19 12H5M12 19l-7-7 7-7" /></svg>
          返回线路监控
        </Link>

        {/* ===== HEADER ===== */}
        <header className={`${card} px-[26px] py-[22px]`}>
          <div className="flex items-start gap-5">
            <div className="w-[52px] h-[52px] flex-none rounded-[14px] grid place-items-center text-[color:var(--brand-from)] text-[22px] font-bold hidden md:grid"
              style={{ background: 'var(--color-raised)', border: '1.5px solid var(--color-line)' }}>⬡</div>
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2.5 flex-wrap">
                <h1 className="m-0 text-[22px] font-bold tracking-[-0.01em]">{node.name}</h1>
                <NodeTypeBadge type={node.node_type} />
                <NodeStackBadge node={node} />
                {isComposite
                  ? (node.disabled && <Badge color="amber">已禁用</Badge>)
                  : <HeaderStatus node={node} />}
              </div>
              {isComposite ? (
                <div className="mt-2 text-[13px] text-ink-mut">虚拟链路节点，由下列各跳依次转发；自身无 Agent / IP / 同步状态</div>
              ) : (
                <div className="mt-2 flex items-center gap-[18px] flex-wrap text-[13px] text-ink-mut">
                  <span>连接 IP&nbsp;&nbsp;{node.address
                    ? <b className="text-ink-soft font-mono font-semibold"><SensText blurred={blurred}>{node.address}</SensText></b>
                    : <span className="text-ink-mut">未连接</span>}</span>
                  <span className="text-[#cfd6df] hidden sm:inline">|</span>
                  <span className="hidden sm:inline">Agent&nbsp;&nbsp;{node.agent_version
                    ? <><span className="font-mono text-ink-soft">{displayAgentVersion(node, latest_agent_version, latest_agent_sha)}</span>{agentOutdated && <span className="ml-1"><Badge color="amber">非最新</Badge></span>}</>
                    : <span className="text-ink-mut">未知</span>}</span>
                  <span className="text-[#cfd6df] hidden sm:inline">|</span>
                  <span className="hidden sm:inline">最近同步&nbsp;&nbsp;<b className="text-ink-soft font-semibold">{fmtTime(node.last_apply_at?.Valid ? node.last_apply_at.Int64 : null)}</b></span>
                </div>
              )}
            </div>
          </div>

          {/* header actions */}
          <div className="flex flex-wrap gap-2.5 mt-4 md:mt-3">
            <button onClick={toggle} className="btn-secondary text-xs">{node.disabled ? '启用节点' : '禁用节点'}</button>
            {!isComposite && (
              <button onClick={resync} className="btn-secondary text-xs">重新同步</button>
            )}
            <button onClick={remove} className="hidden md:inline-flex btn-danger text-xs">删除节点</button>
            {!isComposite && agentOutdated && (
              <button onClick={upgrade} title={`推送升级到 ${latest_agent_version}`} className="hidden md:inline-flex btn-secondary text-xs max-w-[280px] truncate">⤴ 推送升级到 {latest_agent_version}</button>
            )}
          </div>
        </header>

        {/* ===== 组合节点：仅名称配置（desktop only） ===== */}
        {isComposite ? (
          <section className={`${card} px-[26px] py-[22px] hidden md:flex flex-col gap-[18px]`}>
            <h2 className="m-0 text-[15px] font-bold">节点配置</h2>
            <ConfigField label="节点名称">
              <form onSubmit={saveName} className="flex gap-2 max-w-md">
                <input className="input-field flex-1" value={name} onChange={e => setName(e.target.value)} required />
                <button type="submit" className="btn-primary flex-none px-5">保存</button>
              </form>
            </ConfigField>
          </section>
        ) : (
        /* ===== TWO COLUMN: 基本信息 + 安装命令 | 节点配置 ===== */
        <div className="grid grid-cols-1 lg:grid-cols-[1.55fr_1fr] gap-[18px] items-start">

          <div className="flex flex-col gap-[18px] min-w-0">

          {/* 基本信息 */}
          <section className={`${card} px-[26px] py-[22px]`}>
            <h2 className="m-0 mb-[18px] text-[15px] font-bold">基本信息</h2>
            <div className="grid grid-cols-[auto_1fr]">
              <InfoRow label="连接 IP" mono>
                {node.address ? <SensText blurred={blurred}>{node.address}</SensText> : <span className="text-ink-mut">未连接</span>}
              </InfoRow>
              <InfoRow label="中继地址（数据面）" mono>
                {node.relay_host ? <SensText blurred={blurred}>{node.relay_host}</SensText> : <span className="text-ink-mut">未设置（设置后才能进链路）</span>}
              </InfoRow>
              <InfoRow label="IPv6 中继地址" mono>
                {node.relay_host_v6 ? <SensText blurred={blurred}>{node.relay_host_v6}</SensText> : <span className="text-ink-mut">未设置</span>}
              </InfoRow>
              <InfoRow label="Token" mono valueClass="text-[12.5px] break-all leading-relaxed">
                <span className="inline-flex items-center gap-2 flex-wrap">
                  {node.secret
                    ? <SensText blurred={blurred}><CopyText text={node.secret} /></SensText>
                    : <span className="text-ink-mut">{node.secret_legacy ? '旧版哈希 Token，无法显示，请重置' : '未设置'}</span>}
                  <button onClick={resetToken} className="btn-secondary !h-7 px-2.5 text-xs">重置 Token</button>
                </span>
              </InfoRow>
              <InfoRow label="最近心跳">
                {fmtTime(node.last_seen ?? null)}
                <span className={`ml-1.5 font-semibold ${node.online === 1 ? 'text-[#0a8a4f]' : 'text-[#c2520a]'}`}>{node.online === 1 ? '在线' : '离线'}</span>
              </InfoRow>
              <InfoRow label="Agent 版本" mono valueClass="text-[12.5px]" last={!showUpgrade}>
                {node.agent_version
                  ? <>{displayAgentVersion(node, latest_agent_version, latest_agent_sha)} {agentOutdated ? <Badge color="amber">非最新</Badge> : <Badge color="green">最新</Badge>}</>
                  : <span className="text-ink-mut">未知</span>}
              </InfoRow>
              {showUpgrade && (
                <InfoRow label="升级状态" valueClass="text-[12.5px]" last>
                  {up.status === 'ok' && <><Badge color="green">升级成功</Badge> <span className="ml-1 text-ink-mut">{up.version} · {fmtTime(up.at)}</span></>}
                  {up.status === 'error' && <><Badge color="red">升级失败</Badge> <span className="ml-1 text-ink-mut break-all">{up.error}</span></>}
                  {up.status === 'pending' && <><Badge color="blue">升级中</Badge> <span className="ml-1 text-ink-mut">已推送 {up.version} · {fmtTime(up.at)}</span></>}
                  {up.status === 'stuck' && <><Badge color="amber">可能未生效</Badge> <span className="ml-1 text-ink-mut">已确认接收 {up.version}（{fmtTime(up.at)}），当前仍为 {displayAgentVersion(node, latest_agent_version, latest_agent_sha)}，可能重启失败</span></>}
                </InfoRow>
              )}
            </div>
            {nullStr(node.last_error) && (
              <div className="mt-2 text-[12.5px] text-rose-700 dark:text-rose-300 bg-transparent border-[1.5px] border-rose-500/40 rounded-xl px-3 py-2 break-all">
                {nullStr(node.last_error)}
              </div>
            )}
            {node.last_warning && !nullStr(node.last_error) && (
              <div className="mt-2 text-[12.5px] text-amber-800 dark:text-amber-300 bg-transparent border-[1.5px] border-amber-500/45 rounded-xl px-3 py-2 break-all">
                {node.last_warning}
              </div>
            )}
          </section>

          {/* 安装命令（实体节点专属）— desktop only */}
          {node.node_type !== 'self' && (
          <section className={`${card} px-[26px] py-[22px] hidden md:block`}>
            <div className="flex items-baseline gap-3 mb-3.5 flex-wrap">
              <h2 className="m-0 text-[15px] font-bold">节点安装命令</h2>
            </div>
            {!panel_url_configured && (
              <div className="mb-3 px-3 py-2 bg-transparent border-[1.5px] border-amber-500/45 rounded-xl text-amber-800 dark:text-amber-300 text-[13px]">
                尚未设置面板地址，下面用你当前访问的地址推断。如 agent 走不同地址，请到<Link to="/nodes" className="link-accent">节点页</Link>设置后再复制。
              </div>
            )}
            <div className="mb-3 flex items-center gap-2.5 flex-wrap text-[13px]">
              <span className="text-ink-soft">中继地址声明（双出口机器用，可空）</span>
              <input className="input-field font-mono" style={{ height: 30, maxWidth: 220 }} value={cmdRelayHost}
                onChange={e => setCmdRelayHost(e.target.value)} placeholder="IPv4 / 域名（可选）" />
              <input className="input-field font-mono" style={{ height: 30, maxWidth: 220 }} value={cmdRelayHostV6}
                onChange={e => setCmdRelayHostV6(e.target.value)} placeholder="IPv6（可选）" />
            </div>
            <div className="relative bg-[#1e1e2e] dark:bg-app border border-line rounded-[10px] px-5 py-[18px]">
              <button onClick={() => copyToClipboard(installCmd).then(() => toast('已复制')).catch(() => toast('复制失败', 'error'))}
                className="absolute top-3.5 right-3.5 text-[12.5px] font-semibold text-[#a0a4b0] bg-[#2a2a3c] border border-[#3a3a4c] px-3.5 py-[6px] rounded-[7px] cursor-pointer hover:bg-[#33334a] transition-colors">复制</button>
              <pre className="m-0 font-mono text-[13px] leading-[1.75] text-[#e8ecf3] whitespace-pre-wrap break-all">
                <SensText blurred={blurred}>{installCmd}</SensText>
              </pre>
            </div>
          </section>
          )}

          </div>

          {/* 节点配置 — desktop only */}
          <section className={`${card} px-6 py-[22px] flex-col gap-[18px] hidden md:flex`}>
            <h2 className="m-0 text-[15px] font-bold">节点配置</h2>

            <ConfigField label="节点名称">
              <form onSubmit={saveName} className="flex gap-2">
                <input className="input-field flex-1" value={name} onChange={e => setName(e.target.value)} required />
                <button type="submit" className="btn-primary flex-none px-5">保存</button>
              </form>
            </ConfigField>

            <ConfigField label="中继地址（数据面）">
              <form onSubmit={saveRelay} className="flex gap-2">
                <input className="input-field font-mono flex-1" value={relayHost} onChange={e => setRelayHost(e.target.value)} placeholder="数据面公网 IP 或域名" disabled={node.relay_host_declared} />
                <button type="submit" className="btn-primary flex-none px-5" disabled={node.relay_host_declared}>保存</button>
              </form>
            </ConfigField>

            {!isComposite && (
              <ConfigField label="Cloudflare DNS">
                <form onSubmit={saveCF} className="space-y-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="text-[13px] text-ink-soft">
                      同步 A 记录（DNS only / 灰云，勿开橙云代理）
                    </div>
                    <button type="button" role="switch" aria-checked={cfSync}
                      className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors shrink-0 ${cfSync ? 'bg-[color:var(--brand-from)]' : 'bg-gray-500'}`}
                      onClick={() => setCfSync(!cfSync)}>
                      <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${cfSync ? 'translate-x-6' : 'translate-x-1'}`} />
                    </button>
                  </div>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    <div>
                      <label className="block text-[12px] font-semibold text-ink-soft mb-1">
                        当前 IP {cfSync && <span className="text-red-500">*</span>}
                      </label>
                      <input className="input-field font-mono text-sm w-full" value={backendIP}
                        onChange={e => setBackendIP(e.target.value)} placeholder="要写入 A 记录的 IPv4" />
                    </div>
                    <div>
                      <label className="block text-[12px] font-semibold text-ink-soft mb-1">记录名（可选）</label>
                      <input className="input-field font-mono text-sm w-full" value={cfRecordName}
                        onChange={e => setCfRecordName(e.target.value)} placeholder="默认用中继域名" />
                    </div>
                  </div>
                  <div>
                    <label className="block text-[12px] font-semibold text-ink-soft mb-1">Zone ID（可选）</label>
                    <input className="input-field font-mono text-sm w-full" value={cfZoneID}
                      onChange={e => setCfZoneID(e.target.value)} placeholder="空则用系统设置默认 Zone" />
                  </div>
                  {(node.cf_last_sync_at > 0 || node.cf_last_error) && (
                    <div className={`text-[11px] ${node.cf_last_error ? 'text-red-600' : 'text-ink-mut'}`}>
                      {node.cf_last_sync_at > 0 && (
                        <>上次同步：{new Date(node.cf_last_sync_at * 1000).toLocaleString('zh-CN')}
                          {node.cf_last_ip ? ` · ${node.cf_last_ip}` : ''}
                          {node.cf_last_error ? '' : ' · 成功'}
                        </>
                      )}
                      {node.cf_last_error && <>{node.cf_last_sync_at > 0 ? ' · ' : ''}错误：{node.cf_last_error}</>}
                    </div>
                  )}
                  <div className="flex flex-wrap gap-2">
                    <button type="submit" className="btn-primary px-5" disabled={cfBusy}>{cfBusy ? '保存中…' : '保存 CF 配置'}</button>
                    {node.cf_sync && (
                      <>
                        <button type="button" className="btn-secondary px-4" disabled={cfBusy}
                          onClick={() => { setChangeIPVal(node.backend_ip || backendIP || ''); setChangeIPOpen(true) }}>
                          改 IP
                        </button>
                        <button type="button" className="btn-secondary px-4" disabled={cfBusy} onClick={resyncCF}>
                          重新同步
                        </button>
                      </>
                    )}
                  </div>
                </form>
              </ConfigField>
            )}

            <ConfigField label="IPv6 中继地址">
              <form onSubmit={saveRelayV6} className="flex gap-2">
                <input className="input-field font-mono flex-1" value={relayHostV6} onChange={e => setRelayHostV6(e.target.value)} placeholder="数据面公网 IPv6 地址" disabled={node.relay_host_v6_declared} />
                <button type="submit" className="btn-primary flex-none px-5" disabled={node.relay_host_v6_declared}>保存</button>
              </form>
            </ConfigField>

            <ConfigField label="端口范围">
              <form onSubmit={savePortRange} className="flex gap-2">
                <input className="input-field font-mono flex-1" value={portRange} onChange={e => setPortRange(e.target.value)} placeholder="10001-60000" />
                <button type="submit" className="btn-primary flex-none px-5">保存</button>
              </form>
            </ConfigField>
          </section>
        </div>
        )}

        {/* ===== 组合节点跳序 — desktop only ===== */}
        {isComposite && (
          <div className="hidden md:block">
            <CompositeHopsCard nodeId={id} hops={nodeHops} singleNodes={data.single_nodes || []} onDone={load} />
          </div>
        )}

        {/* ===== 经过该节点的规则 ===== */}
        <section className={`${card} px-[26px] pt-[22px] pb-2`}>
          <div className="flex items-baseline gap-2.5 mb-1.5">
            <h2 className="m-0 text-[15px] font-bold">经过该节点的规则</h2>
            <span className="text-[12.5px] text-ink-mut">{ruleHops.length} 条</span>
          </div>
          {ruleHops.length ? (
            <TableBox>
            <table className="tbl">
              <thead><tr><th>规则</th><th>用户</th><th>类型</th><th>协议</th><th>模式</th><th>监听端口</th><th>目标</th><th className="text-right">流量</th></tr></thead>
              <tbody>
                {ruleHops.map((rh, i) => (
                  <tr key={i}>
                    <td className="font-semibold">
                      {rh.rule_id ? <Link to={`/rules/${rh.rule_id}`} className="link-accent hover:underline">{rh.rule_name || `#${rh.rule_id}`}</Link> : '--'}
                    </td>
                    <td className="text-sm">
                      {rh.owner_id ? <Link to={`/users/${rh.owner_id}`} className="link-accent hover:underline">{rh.owner_name}</Link> : <span className="text-ink-mut">--</span>}
                    </td>
                    <td className="text-xs whitespace-nowrap">
                      {rh.total_hops > 1
                        ? <Link to={`/nodes/${rh.rule_node_id}`} className="text-purple-600 font-semibold hover:underline">组合 {rh.position + 1}/{rh.total_hops}</Link>
                        : <span className="text-ink-mut">单点</span>}
                    </td>
                    <td><ProtoBadge proto={rh.proto} /></td>
                    <td><ModeBadge mode={rh.mode} /></td>
                    <td className="font-mono">{rh.listen_port}</td>
                    <td className="font-mono"><SensText blurred={blurred}>{rh.target_host ? `${rh.target_host}:${rh.target_port}` : '--'}</SensText></td>
                    <td className="text-right font-mono text-xs text-ink-mut">{fmtBytes(rh.total_bytes)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            </TableBox>
          ) : <div className="pb-4"><Empty title="该节点尚无规则经过"><Link to="/rules" className="link-accent text-xs">去添加</Link></Empty></div>}
        </section>

        {/* ===== 已授权用户 ===== */}
        <section className={`${card} px-[26px] pt-[22px] pb-2`}>
          <div className="flex items-baseline gap-2.5 mb-1.5">
            <h2 className="m-0 text-[15px] font-bold">已授权用户</h2>
            <span className="text-[12.5px] text-ink-mut">{grantedUsers.length} 人</span>
          </div>
          <GrantEditor nodeId={node.id} grantedUsers={grantedUsers} onChanged={load} />
          {grantedUsers.length ? (
            <TableBox>
            <table className="tbl">
              <thead><tr><th>用户</th><th>单节点配额</th><th>授权时间</th><th className="text-right">操作</th></tr></thead>
              <tbody>
                {grantedUsers.map(g => (
                  <tr key={g.user_id}>
                    <td className="font-semibold">
                      <Link to={`/users/${g.user_id}`} className="link-accent hover:underline">{g.username}</Link>
                    </td>
                    <td className="font-mono">{g.max_forwards}</td>
                    <td className="text-xs text-ink-mut">{fmtTime(g.granted_at)}</td>
                    <td className="text-right">
                      <button onClick={async () => {
                        if (!await confirm({ title: '取消授权', message: `确定取消 ${g.username} 对该节点的授权？`, confirmText: '取消授权', danger: true })) return
                        try { await api.del(`/users/${g.user_id}/grants/${node.id}`); toast('已取消授权'); load() }
                        catch (e) { toast(e.message, 'error') }
                      }} className="link-danger text-xs cursor-pointer bg-transparent border-0 p-0">取消授权</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            </TableBox>
          ) : <div className="pb-4"><Empty title="尚无用户被授权使用此节点" /></div>}
        </section>

      </div>

      {/* One-time reveal of a freshly reset Token. Closing clears it from memory. */}
      <Modal open={!!revealedSecret} onClose={() => setRevealedSecret('')} title="新的 Token">
        <div className="text-[13px] text-ink-soft mb-3">请立即复制并保存。关闭后无法再次查看，只能重新重置。</div>
        <div className="rounded-[10px] border border-line bg-surface px-3.5 py-3 font-mono text-[13px] break-all">
          <CopyText text={revealedSecret} />
        </div>
        <div className="flex justify-end mt-5">
          <button onClick={() => setRevealedSecret('')} className="btn-primary px-5">我已保存</button>
        </div>
      </Modal>

      <Modal open={changeIPOpen} onClose={() => !cfBusy && setChangeIPOpen(false)} title={`改 IP · ${node.name}`}>
        <form onSubmit={submitChangeIP} className="space-y-4">
          <div className="text-[13px] text-ink-soft">
            中继 <span className="font-mono font-semibold text-ink">{node.relay_host || '（未设置）'}</span>
            <div className="text-[12px] text-ink-mut mt-1">
              只改当前入口 IP 并推送到 CF A 记录；中继域名与用户链接不变。
            </div>
          </div>
          <div>
            <label className="block text-[13px] font-semibold text-ink-soft mb-1.5">当前 IPv4</label>
            <input className="input-field font-mono" value={changeIPVal}
              onChange={e => setChangeIPVal(e.target.value)}
              placeholder="例如 68.252.208.113" autoFocus />
          </div>
          <div className="flex gap-2 pt-1">
            <button type="submit" disabled={cfBusy} className="btn-primary flex-1">
              {cfBusy ? '保存中…' : '保存并同步 CF'}
            </button>
            <button type="button" disabled={cfBusy} onClick={() => setChangeIPOpen(false)} className="btn-secondary flex-1">
              取消
            </button>
          </div>
        </form>
      </Modal>
    </Layout>
  )
}

function InfoRow({ label, mono, valueClass = '', last, children }) {
  const bb = last ? '' : 'border-b border-line-soft'
  return (
    <>
      <div className={`text-[13px] text-ink-mut pr-5 py-[11px] ${bb}`}>{label}</div>
      <div className={`text-[13.5px] text-ink py-[11px] min-w-0 ${mono ? 'font-mono' : ''} ${valueClass} ${bb}`}>{children}</div>
    </>
  )
}

function ConfigField({ label, hint, children }) {
  return (
    <div>
      <label className="block text-[12.5px] text-ink-soft mb-[7px] font-medium">{label}</label>
      {hint && <p className="mt-[-2px] mb-[7px] text-[11.5px] text-ink-mut leading-snug">{hint}</p>}
      {children}
    </div>
  )
}

// Header status pill combining online + sync state, with disabled/error overriding.
function HeaderStatus({ node }) {
  let text, cls, dot
  if (node.disabled) {
    text = '已禁用'; cls = 'text-amber-700 dark:text-amber-300 border-amber-500/55'; dot = '#e0892f'
  } else if (nullStr(node.last_error)) {
    text = '错误'; cls = 'text-rose-700 dark:text-rose-300 border-rose-500/55'; dot = '#e5484d'
  } else if (node.last_warning) {
    text = '警告'; cls = 'text-amber-700 dark:text-amber-300 border-amber-500/55'; dot = '#e0892f'
  } else if (node.online === 1) {
    text = node.last_apply_at?.Valid ? '在线 · 已同步' : '在线 · 待同步'
    cls = 'text-emerald-700 dark:text-emerald-300 border-emerald-500/55'; dot = '#22b46e'
  } else {
    text = '离线'; cls = 'text-ink-soft border-line'; dot = '#9aa4b2'
  }
  return (
    <span className={`inline-flex items-center gap-1.5 text-[12.5px] font-semibold px-2.5 py-[3px] rounded-full bg-transparent border-[1.5px] ${cls}`}>
      <span className="w-1.5 h-1.5 rounded-full" style={{ background: dot }} />
      {text}
    </span>
  )
}

/* 多选下拉直接编辑该节点的授权集合：默认选中已授权用户，保存时按 diff
   新增走 grant（默认转发上限同用户页）、移除走 revoke。移除会让该用户在此
   节点的规则失去授权依据，所以先确认。 */
function GrantEditor({ nodeId, grantedUsers, onChanged }) {
  const [allUsers, setAllUsers] = useState(null)
  const [sel, setSel] = useState(grantedUsers.map(g => String(g.user_id)))
  const [saving, setSaving] = useState(false)
  const toast = useToast()
  const confirm = useConfirm()

  // 只在授权集合真实变化时重置选择，静默轮询刷新不打断编辑中的下拉。
  const grantedKey = grantedUsers.map(g => g.user_id).sort((a, b) => a - b).join(',')
  useEffect(() => { setSel(grantedUsers.map(g => String(g.user_id))) }, [grantedKey])
  useEffect(() => {
    api.get('/users').then(d => setAllUsers((d.users || []).filter(u => u.role === 'user'))).catch(() => setAllUsers([]))
  }, [])

  // 已授权但不在用户列表里的（角色变更等）也要可见，否则无法取消勾选。
  const byId = new Map((allUsers || []).map(u => [String(u.id), u.username]))
  for (const g of grantedUsers) if (!byId.has(String(g.user_id))) byId.set(String(g.user_id), g.username)
  const options = [...byId].map(([value, label]) => ({ value, label }))

  const grantedIds = new Set(grantedUsers.map(g => String(g.user_id)))
  const added = sel.filter(v => !grantedIds.has(v))
  const removed = [...grantedIds].filter(v => !sel.includes(v))
  const dirty = added.length > 0 || removed.length > 0

  const save = async () => {
    if (removed.length && !(await confirm({
      title: '取消授权',
      message: `将取消 ${removed.length} 名用户对该节点的授权，其在此节点的规则会失去授权依据。继续？`,
      confirmText: '继续', danger: true,
    }))) return
    setSaving(true)
    try {
      for (const uid of added) await api.post(`/users/${uid}/grants`, { node_id: Number(nodeId) })
      for (const uid of removed) await api.del(`/users/${uid}/grants/${nodeId}`)
      toast('授权已更新')
    } catch (err) { toast(err.message, 'error') } finally { setSaving(false); onChanged() }
  }

  return (
    <div className="flex items-center gap-2 mb-3 max-w-xl">
      <Select multiple searchable className="flex-1" placeholder="选择要授权的用户…"
        value={sel} onChange={setSel} options={options} />
      <button onClick={save} disabled={saving || !dirty} className="btn-primary flex-none px-5">保存</button>
    </div>
  )
}

function HopLatency({ fromId, toId }) {
  const [state, setState] = useState('idle')
  const [label, setLabel] = useState('')
  const probe = async () => {
    if (!fromId || !toId) return
    setState('loading')
    try {
      const d = await api.get(`/probe-hop?from=${fromId}&to=${toId}`)
      if (d.ok) {
        setState('ok')
        setLabel(`${d.latency_ms}ms`)
      } else {
        setState('fail')
        setLabel(d.error || '不通')
      }
    } catch (err) {
      setState('fail')
      setLabel(err.message || '请求失败')
    }
  }
  const ready = !!fromId && !!toId
  return (
    <div className="flex items-center justify-center gap-2 py-1">
      <span className="text-[11px] text-ink-mut">↓ 到下一跳</span>
      <button type="button" onClick={probe} disabled={!ready || state === 'loading'}
        title={ready ? '测试这一跳到下一跳的延迟' : '请先选好两跳节点'}
        className="text-[11px] px-2 py-0.5 rounded-lg border-[1.5px] border-line bg-surface text-ink-soft hover:border-[color:var(--brand-from)] hover:text-[color:var(--brand-from)] hover:-translate-y-px disabled:opacity-50 disabled:cursor-not-allowed disabled:hover:translate-y-0">
        {state === 'loading' ? <Spinner className="w-3 h-3" /> : '测延迟'}
      </button>
      {state === 'ok' && <span className="text-[11px] text-green-700 font-semibold tabular-nums">{label}</span>}
      {state === 'fail' && <span className="text-[11px] text-red-600 max-w-[220px] truncate" title={label}>{label}</span>}
    </div>
  )
}

function CompositeHopsCard({ nodeId, hops: initHops, singleNodes, onDone }) {
  const [rows, setRows] = useState(initHops.map(h => ({
    node_id: h.hop_node_id, node_name: h.node_name || `#${h.hop_node_id}`,
    mode: h.mode,
  })))
  const [saving, setSaving] = useState(false)
  const [dragIdx, setDragIdx] = useState(null)
  const toast = useToast()

  const nodeById = Object.fromEntries(singleNodes.map(n => [n.id, n]))
  const dirty = rows.length !== initHops.length || rows.some((r, i) => {
    const h = initHops[i]
    return !h || r.node_id !== h.hop_node_id || r.mode !== h.mode
  })

  const setField = (i, k, v) => setRows(rs => rs.map((r, j) => j === i ? { ...r, [k]: v } : r))
  const addHop = () => setRows(rs => [...rs, { node_id: '', node_name: '', mode: 'userspace' }])
  const removeHop = (i) => setRows(rs => rs.filter((_, j) => j !== i))
  const onDrop = (toIdx) => {
    if (dragIdx === null || dragIdx === toIdx) { setDragIdx(null); return }
    setRows(rs => {
      const arr = [...rs]
      const [moved] = arr.splice(dragIdx, 1)
      arr.splice(toIdx, 0, moved)
      return arr
    })
    setDragIdx(null)
  }

  const save = async () => {
    if (rows.length < 2) { toast('至少需要 2 个子节点', 'error'); return }
    if (rows.some(r => !r.node_id)) { toast('请选择所有节点', 'error'); return }
    setSaving(true)
    try {
      await api.post(`/nodes/${nodeId}/hops`, {
        hops: rows.map(r => ({ node_id: Number(r.node_id), mode: r.mode }))
      })
      toast('已保存')
      onDone()
    } catch (err) { toast(err.message, 'error') } finally { setSaving(false) }
  }

  return (
    <section className={`${card} px-[26px] pt-[22px] pb-[18px]`}>
      <div className="flex items-baseline gap-2.5 mb-1.5">
        <h2 className="m-0 text-[15px] font-bold">组合节点跳序</h2>
        <span className="text-[12.5px] text-ink-mut">{rows.length} 跳</span>
      </div>
      <p className="text-[12.5px] text-ink-mut mb-2.5">
        拖拽 ⠿ 调整顺序。模式作用于该跳到下一跳之间的段：线路稳定、低丢包用内核态（性能更好，支持 TCP/UDP/TCP+UDP）；
        跨境或网络不稳定、丢包高用用户态（更抗抖动，仅 TCP）。末跳模式在该组合被用作中间层时生效，被用作规则出口时由规则的出口模式覆盖。修改对此后新建的规则生效。
      </p>
      <div className="space-y-1">
        {rows.map((r, i) => (
          <div key={i}>
            <div
              draggable onDragStart={() => setDragIdx(i)} onDragOver={e => e.preventDefault()} onDrop={() => onDrop(i)}
              className={`flex items-center gap-2 bg-raised rounded-lg px-3 py-2 ${dragIdx === i ? 'opacity-50' : ''}`}>
              <span className="text-ink-mut cursor-move select-none" title="拖拽排序">⠿</span>
              <span className="text-xs text-ink-mut w-5 text-center font-mono">{i + 1}</span>
              <Select className="flex-1" placeholder="-- 选择节点 --" searchable value={r.node_id}
                onChange={v => {
                  const nd = nodeById[v]
                  setField(i, 'node_id', v)
                  if (nd) setField(i, 'node_name', nd.name)
                }}
                options={singleNodes.filter(n => n.id === Number(r.node_id) || !rows.some((rr, j) => j !== i && Number(rr.node_id) === n.id)).map(n => ({ value: n.id, label: n.name }))} />
              {/* 每一跳（含末跳）都可配模式：末跳模式在该组合被用作中间层时生效，
                  被用作规则出口时由规则的出口模式覆盖 */}
              <Select value={r.mode} onChange={v => setField(i, 'mode', v)} style={{ width: 120 }}
                title={i === rows.length - 1 ? '末跳模式：作为中间层时生效；作为规则出口时由规则的出口模式覆盖' : undefined}
                options={[{ value: 'kernel', label: 'kernel' }, { value: 'userspace', label: 'userspace' }]} />
              {i === rows.length - 1 && (
                <span className="text-[11px] text-ink-mut shrink-0 cursor-help" title="末跳模式：作为中间层时生效；作为规则出口时由规则的出口模式覆盖">末</span>
              )}
              {rows.length > 2 && (
                <button type="button" onClick={() => removeHop(i)} className="btn-danger-sm text-xs px-1.5">×</button>
              )}
            </div>
            {i < rows.length - 1 && (
              <HopLatency fromId={r.node_id} toId={rows[i + 1].node_id} />
            )}
          </div>
        ))}
      </div>
      <div className="flex items-center gap-3 pt-3">
        <button type="button" onClick={addHop} className="btn-secondary text-xs">+ 添加一跳</button>
        <button onClick={save} disabled={saving || !dirty} className="btn-primary">保存</button>
      </div>
    </section>
  )
}
