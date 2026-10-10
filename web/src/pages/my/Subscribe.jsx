import { useEffect, useMemo, useState } from 'react'
import QRCode from 'qrcode'
import { api } from '../../lib/api'
import { copyToClipboard } from '../../lib/clipboard'
import { fmtBytes, fmtDate, fmtTrafficGB, isExpired, nullStr, pct } from '../../lib/fmt'
import { Layout, useToast } from '../../components/Layout'
import { UserPortalHead } from '../../components/UserPortalHead'
import { Badge, Loading, useConfirm } from '../../components/ui'

const LAT_TTL_MS = 45_000

export default function MySubscribe() {
  const toast = useToast()
  const confirm = useConfirm()
  const [data, setData] = useState(null)
  const [loadError, setLoadError] = useState('')
  const [ruleBusy, setRuleBusy] = useState(0)
  const [loading, setLoading] = useState(true)
  const [addrOpen, setAddrOpen] = useState(false)
  const [qr, setQr] = useState('')
  const [qrErr, setQrErr] = useState('')
  const [copied, setCopied] = useState('')
  const [latency, setLatency] = useState({})
  const [latLoading, setLatLoading] = useState(false)
  const [note, setNote] = useState('')
  const [reqBusy, setReqBusy] = useState('')

  const load = () => {
    setLoadError('')
    return api.get('/my/subscribe')
      .then((d) => { setData(d); return d })
      .catch((e) => {
        const msg = e.message || '加载失败'
        setLoadError(msg)
        toast(msg, 'error')
        return null
      })
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const uriURL = data?.uri_url || ''
  useEffect(() => {
    if (!uriURL) { setQr(''); setQrErr(''); return }
    let cancelled = false
    QRCode.toDataURL(uriURL, {
      width: 280,
      margin: 2,
      errorCorrectionLevel: 'M',
      color: { dark: '#1b1612', light: '#ffffff' },
    }).then((url) => {
      if (!cancelled) { setQr(url); setQrErr('') }
    }).catch((e) => {
      if (!cancelled) { setQr(''); setQrErr(e?.message || '二维码生成失败') }
    })
    return () => { cancelled = true }
  }, [uriURL])

  const items = data?.items || []
  const skipped = data?.skipped || []
  const subRules = data?.rules || []
  const account = data?.account || {}
  const profileName = account.username || ''
  const v2rayLines = useMemo(
    () => items.map((it) => it.uri).filter(Boolean).join('\n'),
    [items],
  )

  const applyLatency = (d) => {
    const map = {}
    for (const it of d?.items || []) map[latencyKey(it)] = it
    setLatency(map)
  }

  useEffect(() => {
    if (!data) return
    let cancelled = false
    const cached = readLatCache(profileName)
    if (cached) {
      applyLatency(cached)
      return
    }
    setLatLoading(true)
    api.get('/my/subscribe/latency').then((d) => {
      if (cancelled) return
      applyLatency(d)
      writeLatCache(profileName, d)
    }).catch(() => {
      if (!cancelled) setLatency({})
    }).finally(() => {
      if (!cancelled) setLatLoading(false)
    })
    return () => { cancelled = true }
  }, [data])

  const refreshLatency = async () => {
    clearLatCache(profileName)
    setLatLoading(true)
    try {
      const d = await api.get('/my/subscribe/latency?refresh=1')
      applyLatency(d)
      writeLatCache(profileName, d)
      toast('已刷新延迟')
    } catch (e) {
      setLatency({})
      toast(e.message || '延迟刷新失败', 'error')
    } finally {
      setLatLoading(false)
    }
  }

  const copy = async (text, key, ok = '已复制') => {
    if (!text) { toast('暂无可复制内容', 'error'); return }
    try {
      await copyToClipboard(text)
      setCopied(key)
      toast(ok)
      setTimeout(() => setCopied((c) => (c === key ? '' : c)), 1600)
    } catch {
      toast('复制失败', 'error')
    }
  }

  const downloadYaml = () => {
    if (!data?.mihomo_url) return
    const a = document.createElement('a')
    a.href = data.mihomo_url + (data.mihomo_url.includes('?') ? '&' : '?') + 'download=1'
    a.download = `${profileName || 'kids'}.yaml`
    document.body.appendChild(a)
    a.click()
    a.remove()
    toast('已开始下载 YAML')
  }

  const rotateSub = async () => {
    if (!(await confirm({
      title: '重置订阅链接',
      message: '旧地址立刻失效。请把新地址重新导入小火箭 / Clash / Mihomo。',
      confirmText: '重置',
      danger: true,
    }))) return
    try {
      const d = await api.post('/my/subscribe/rotate')
      clearLatCache(profileName)
      setData((prev) => prev ? { ...prev, ...d } : prev)
      toast('订阅链接已重置，请重新导入客户端')
    } catch (e) {
      toast(e.message || '重置失败', 'error')
    }
  }

  const submitRequest = async (kind) => {
    if (reqBusy) return
    setReqBusy(kind)
    try {
      const d = await api.post('/my/requests', { kind, note: note.trim() })
      toast(d?.already ? '已经提交过，等管理员处理即可' : (kind === 'renew' ? '已提交续期申请' : '已提交加量申请'))
      setNote('')
      await load()
    } catch (e) {
      toast(e.message || '提交失败', 'error')
    } finally {
      setReqBusy('')
    }
  }

  const toggleRule = async (rule) => {
    if (ruleBusy) return
    const nextOff = !rule.disabled
    if (nextOff && !(await confirm({
      title: '停用规则',
      message: `停用「${rule.name}」后入口不再转发，配置还在。需要时再启用即可。`,
      confirmText: '停用',
    }))) return
    setRuleBusy(rule.id)
    try {
      await api.post(`/my/rules/${rule.id}/toggle`)
      clearLatCache(profileName)
      toast(nextOff ? '已停用' : '已启用')
      await load()
    } catch (e) {
      toast(e.message || '操作失败', 'error')
    } finally {
      setRuleBusy(0)
    }
  }

  if (loading && !data) return <Layout><Loading /></Layout>

  if (!data) {
    return (
      <Layout>
        <div className="sub-page">
          <UserPortalHead title="我的订阅" />
          <div className="sub-empty">
            <h2>订阅没有加载出来</h2>
            <p>{loadError || '请再试一次'}</p>
            <button type="button" className="btn-secondary mt-3" onClick={() => { setLoading(true); load() }}>重试</button>
          </div>
        </div>
      </Layout>
    )
  }

  const expiresAt = account.expires_at && account.expires_at > 0 ? account.expires_at : null
  const rate = Number(account.billing_rate)
  const used = Math.round((account.traffic_used_bytes || 0) * (rate > 0 ? rate : 1))
  const quota = account.traffic_quota_bytes || 0
  const empty = items.length === 0
  const expired = !!(expiresAt && isExpired(expiresAt))
  const quotaOut = quota > 0 && used >= quota
  const quotaPct = quota > 0 ? Math.round((used / quota) * 100) : 0
  const quotaBanner = quotaOut
    ? { tone: 'danger', text: '流量已用完，入口已停止转发。请联系管理员加量或重置。' }
    : (quota > 0 && quotaPct >= 80
      ? { tone: 'warn', text: `流量已使用 ${quotaPct}%，还剩 ${fmtBytes(Math.max(quota - used, 0))}。` }
      : null)
  const nowSec = Math.floor(Date.now() / 1000)
  const expiringSoon = !!(expiresAt && !expired && expiresAt - nowSec <= 7 * 86400)
  const showRenew = expired || expiringSoon
  const showQuota = quotaOut || (quota > 0 && quotaPct >= 80)
  const requests = Array.isArray(data.requests) ? data.requests : []
  const openRequests = requests.filter((r) => r.status === 'open')
  const doneRequests = requests.filter((r) => r.status !== 'open').slice(0, 3)
  const openRenew = openRequests.find((r) => r.kind === 'renew')
  const openQuota = openRequests.find((r) => r.kind === 'quota')
  const displayRate = rate > 0 ? rate : 1
  const importBlocked = !!account.disabled || expired || quotaOut
  const blockReason = account.disabled
    ? (`账号已被禁用：${nullStr(account.disable_reason) || '请联系管理员'}`)
    : expired
      ? '订阅已过期，无法导入客户端'
      : quotaOut
        ? '流量已用完，无法导入客户端'
        : ''
  const imports = importHrefs(uriURL, data?.clash_url, data?.mihomo_url, profileName || 'kids')
  const seenRules = new Set()
  const nodeRows = items.map((it) => {
    const rule = subRules.find((r) => r.id === it.rule_id)
    const showToggle = !!(rule && !seenRules.has(rule.id))
    if (rule) seenRules.add(rule.id)
    return { item: it, rule: showToggle ? rule : null }
  })
  const hiddenRules = subRules.filter((r) => !seenRules.has(r.id))
  const skipShown = skipped.filter((sk) => sk.reason !== 'disabled')
  const showRequest = showRenew || showQuota || openRequests.length > 0 || doneRequests.length > 0
  const canSubmit = (showRenew && !openRenew) || (showQuota && !openQuota)

  const openImport = (href, fallback, opened) => {
    if (importBlocked) { toast(blockReason, 'error'); return }
    if (!href) {
      copy(fallback, 'import', opened || '已复制订阅地址')
      return
    }
    let hidden = false
    const mark = () => { if (document.hidden) hidden = true }
    document.addEventListener('visibilitychange', mark)
    window.addEventListener('pagehide', mark)
    const a = document.createElement('a')
    a.href = href
    a.rel = 'noreferrer'
    document.body.appendChild(a)
    a.click()
    a.remove()
    window.setTimeout(() => {
      document.removeEventListener('visibilitychange', mark)
      window.removeEventListener('pagehide', mark)
      if (hidden || document.hidden) {
        toast(opened || '已唤起客户端')
        return
      }
      copy(fallback, 'import', '没有打开客户端，已复制订阅地址')
    }, 1200)
  }

  return (
    <Layout>
      <div className="sub-page">
        <UserPortalHead title="我的订阅" />

        {account.disabled && (
          <div className="mb-4 px-4 py-3 bg-transparent border-[1.5px] border-rose-500/40 rounded-xl text-rose-700 dark:text-rose-300 text-sm font-medium">
            账号已被禁用：{nullStr(account.disable_reason) || '请联系管理员'}
          </div>
        )}
        {quotaBanner && (
          <div className={`mb-4 px-4 py-3 rounded-xl text-sm font-medium border-[1.5px] ${
            quotaBanner.tone === 'danger'
              ? 'bg-transparent border-rose-500/40 text-rose-700 dark:text-rose-300'
              : 'bg-transparent border-amber-500/45 text-amber-800 dark:text-amber-300'
          }`}>
            {quotaBanner.text}
          </div>
        )}

        <section className="sub-primary" aria-label="导入订阅">
          {importBlocked && <p className="sub-import-block">{blockReason}</p>}
          <div className="sub-import-grid">
            <button type="button" className="sub-import-btn" disabled={!uriURL || importBlocked}
              onClick={() => openImport(imports.shadowrocket, uriURL, '已唤起小火箭')}>
              <img src="/clients/shadowrocket.png" alt="" />
              <span>小火箭</span>
            </button>
            <button type="button" className="sub-import-btn" disabled={!uriURL || importBlocked}
              onClick={() => openImport('', uriURL, '已复制订阅地址，请在 V2rayN 订阅里添加')}>
              <img src="/clients/v2rayn.png" alt="" />
              <span>V2rayN</span>
            </button>
            <button type="button" className="sub-import-btn" disabled={!data?.clash_url || importBlocked}
              onClick={() => openImport(imports.clash, data?.clash_url, '已唤起 Clash Verge')}>
              <img src="/clients/clash-verge.png" alt="" />
              <span>Clash Verge</span>
            </button>
            <button type="button" className="sub-import-btn" disabled={!data?.mihomo_url || importBlocked}
              onClick={() => openImport(imports.mihomo, data?.mihomo_url, '已唤起 Mihomo')}>
              <img src="/clients/mihomo.png" alt="" />
              <span>Mihomo</span>
            </button>
          </div>

          <section className="sub-account" aria-label="账户概览">
            <div>
              <span className="sub-account-k">账户</span>
              <strong>{account.username || '—'}</strong>
            </div>
            <div>
              <span className="sub-account-k">流量</span>
              <strong className="font-mono">
                {fmtTrafficGB(used, quota)}
                {quota > 0 && <span className="text-ink-mut font-sans font-medium"> · {pct(used, quota)}%</span>}
                {displayRate !== 1 && <span className="sub-rate-note">已按倍率 ×{rateLabel(displayRate)}</span>}
              </strong>
            </div>
            <div>
              <span className="sub-account-k">到期</span>
              <strong>
                {expiryText(expiresAt, expired, nowSec)}
                {expired && <Badge color="red" className="ml-2">已过期</Badge>}
              </strong>
            </div>
          </section>

          <WeekTraffic daily={data?.daily} rate={displayRate} />
        </section>

        {showRequest && (
          <section className="sub-request" aria-label="续期与加量">
            <div className="sub-nodes-head">
              <h2>联系管理员</h2>
            </div>
            {openRequests.length > 0 && (
              <ul className="sub-request-list">
                {openRequests.map((r) => (
                  <li key={r.id}>
                    <Badge color="amber">{r.kind === 'quota' ? '加量' : '续期'}</Badge>
                    <span>已提交，等待处理{r.note ? ` · ${r.note}` : ''}</span>
                  </li>
                ))}
              </ul>
            )}
            {doneRequests.length > 0 && (
              <ul className="sub-request-list">
                {doneRequests.map((r) => (
                  <li key={r.id}>
                    <Badge color="green">已处理</Badge>
                    <span>{r.kind === 'quota' ? '加量' : '续期'}{r.note ? ` · ${r.note}` : ''}</span>
                  </li>
                ))}
              </ul>
            )}
            {canSubmit && (
              <>
                <textarea
                  className="input-field w-full min-h-[72px] text-[13px]"
                  maxLength={200}
                  placeholder="补充说明，选填，最多 200 字"
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                />
                <div className="sub-note-count">{[...note].length}/200</div>
                <div className="flex flex-wrap gap-2 mt-3">
                  {showRenew && !openRenew && (
                    <button type="button" className="btn-secondary" disabled={!!reqBusy} onClick={() => submitRequest('renew')}>
                      {reqBusy === 'renew' ? '提交中…' : '申请续期'}
                    </button>
                  )}
                  {showQuota && !openQuota && (
                    <button type="button" className="btn-secondary" disabled={!!reqBusy} onClick={() => submitRequest('quota')}>
                      {reqBusy === 'quota' ? '提交中…' : '申请加量'}
                    </button>
                  )}
                </div>
              </>
            )}
          </section>
        )}

        <section className="sub-address">
          <button
            type="button"
            className="sub-address-toggle"
            aria-expanded={addrOpen}
            onClick={() => setAddrOpen((v) => !v)}
          >
            <span>复制地址</span>
            <span className="sub-address-hint">{addrOpen ? '收起' : '二维码和订阅链接'}</span>
          </button>
          {addrOpen && (
            <div className="sub-address-body">
              <div className="sub-qr-wrap">
                {qr ? (
                  <img src={qr} alt="订阅二维码" className="sub-qr" />
                ) : (
                  <div className="sub-qr sub-qr-ph">{qrErr || (empty ? '暂无节点' : '生成中…')}</div>
                )}
              </div>
              <FieldRow label="订阅地址" value={uriURL} onCopy={() => copy(uriURL, 'uri', '已复制订阅地址')} copied={copied === 'uri'} />
              <FieldRow label="Clash Verge" value={data?.clash_url} onCopy={() => copy(data?.clash_url, 'clash', '已复制 Clash 订阅')} copied={copied === 'clash'} />
              <FieldRow label="Mihomo" value={data?.mihomo_url} onCopy={() => copy(data?.mihomo_url, 'mihomo', '已复制 Mihomo 订阅')} copied={copied === 'mihomo'} />
              <FieldRow
                label="节点链接"
                value={v2rayLines}
                multiline
                placeholder={empty ? '暂无节点链接' : ''}
                onCopy={() => copy(v2rayLines, 'v2uri', '已复制全部节点链接')}
                copied={copied === 'v2uri'}
              />
              <div className="sub-address-actions">
                <button type="button" className="btn-secondary" disabled={empty || importBlocked || !data?.mihomo_url} onClick={downloadYaml}>下载 YAML</button>
                <button type="button" className="sub-reset-link" onClick={rotateSub} title="链接泄漏或换设备时用。旧地址立刻失效。">重置订阅链接</button>
              </div>
            </div>
          )}
        </section>

        <section className="sub-nodes">
          <div className="sub-nodes-head">
            <h2>节点</h2>
            <button
              type="button"
              className="btn-secondary h-[32px] px-3 text-[12px]"
              disabled={latLoading || items.length === 0}
              onClick={refreshLatency}
            >
              {latLoading ? '测速中…' : '刷新延迟'}
            </button>
          </div>
          {empty && (
            <div className="sub-empty">
              <h2>还没有可导入的节点</h2>
              {skipShown.length > 0 && (
                <ul>
                  {skipShown.map((sk, i) => <li key={i}>{skipLabel(sk)}</li>)}
                </ul>
              )}
            </div>
          )}
          {(nodeRows.length > 0 || hiddenRules.length > 0) && (
            <div className="sub-node-list">
              {nodeRows.map(({ item, rule }) => (
                <NodeRow
                  key={`${item.kind}-${item.rule_id}-${item.family}-${item.name}`}
                  item={item}
                  probe={latency[latencyKey(item)]}
                  latLoading={latLoading}
                  copied={copied === `node-${latencyKey(item)}`}
                  onCopy={() => copy(item.uri, `node-${latencyKey(item)}`, '已复制节点链接')}
                  rule={rule}
                  ruleBusy={ruleBusy}
                  onToggle={toggleRule}
                />
              ))}
              {hiddenRules.map((rule) => (
                <RuleOnlyRow key={rule.id} rule={rule} ruleBusy={ruleBusy} onToggle={toggleRule} />
              ))}
            </div>
          )}
          {skipShown.length > 0 && items.length > 0 && (
            <div className="sub-skip">
              <h3>未纳入订阅</h3>
              <ul>
                {skipShown.map((sk, i) => <li key={i}>{skipLabel(sk)}</li>)}
              </ul>
            </div>
          )}
        </section>
      </div>
    </Layout>
  )
}

function WeekTraffic({ daily, rate }) {
  const rows = Array.isArray(daily) ? daily : []
  if (rows.length === 0) return null
  const shown = rows.map((d) => Math.round((d.raw_bytes || 0) * (rate > 0 ? rate : 1)))
  const max = Math.max(1, ...shown)
  const total = shown.reduce((sum, n) => sum + n, 0)
  const today = shanghaiDay()
  const rateNote = rate > 0 && rate !== 1
  return (
    <section className="sub-week" aria-label="近 7 天用量">
      <div className="sub-nodes-head">
        <h2>近 7 天</h2>
        <span className="text-[12px] text-ink-mut">
          合计 {fmtBytes(total)}
          {rateNote ? ` · 已按倍率 ×${rateLabel(rate)}` : ''}
        </span>
      </div>
      <div className="sub-week-bars">
        {rows.map((d, i) => {
          const isToday = d.day === today
          return (
            <div key={d.day || i} className={`sub-week-col${isToday ? ' is-today' : ''}`} title={`${d.day || ''} ${fmtBytes(shown[i])}`}>
              <div className="sub-week-track">
                <div className="sub-week-fill" style={{ height: shown[i] > 0 ? `${Math.max(8, (shown[i] / max) * 100)}%` : '0%' }} />
              </div>
              <span className="sub-week-day">{isToday ? '今天' : String(d.day || '').slice(5)}</span>
              <span className="sub-week-val">{fmtBytes(shown[i])}</span>
            </div>
          )
        })}
      </div>
    </section>
  )
}

function shanghaiDay(date = new Date()) {
  try {
    return new Intl.DateTimeFormat('en-CA', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit' }).format(date)
  } catch {
    const d = date
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
  }
}

function rateLabel(rate) {
  return Number.isInteger(rate) ? String(rate) : String(rate)
}

function expiryText(expiresAt, expired, nowSec) {
  if (!expiresAt) return '永不过期'
  const date = fmtDate(expiresAt)
  if (expired) return date
  const days = Math.ceil((expiresAt - nowSec) / 86400)
  if (days <= 1) return `${date} · 今天到期`
  return `${date} · 还剩 ${days} 天`
}

function latKey(name) {
  return `nf.sub.latency.v2:${name || '_'}`
}

function readLatCache(name) {
  try {
    const raw = sessionStorage.getItem(latKey(name))
    if (!raw) return null
    const d = JSON.parse(raw)
    if (!d || !d.at || !Array.isArray(d.items)) return null
    if (Date.now() - d.at > LAT_TTL_MS) return null
    return d
  } catch {
    return null
  }
}

function writeLatCache(name, d) {
  try {
    sessionStorage.setItem(latKey(name), JSON.stringify({
      at: Date.now(),
      items: d?.items || [],
    }))
  } catch { /* ignore quota */ }
}

function clearLatCache(name) {
  try { sessionStorage.removeItem(latKey(name)) } catch { /* ignore */ }
}

function latencyKey(it) {
  return `${it.kind || ''}|${it.rule_id || 0}|${it.family || ''}|${it.name || ''}`
}

function importHrefs(uriURL, clashURL, mihomoURL, name) {
  const label = encodeURIComponent(name || 'kids')
  const enc = (u) => encodeURIComponent(u)
  return {
    shadowrocket: uriURL ? `shadowrocket://add/sub://${btoaUtf8(uriURL)}?remark=${label}` : '',
    clash: clashURL ? `clash://install-config?name=${label}&url=${enc(clashURL)}` : '',
    mihomo: mihomoURL ? `clash://install-config?name=${label}&url=${enc(mihomoURL)}` : '',
  }
}

function btoaUtf8(text) {
  const bytes = new TextEncoder().encode(text)
  let bin = ''
  bytes.forEach((b) => { bin += String.fromCharCode(b) })
  return btoa(bin)
}

function FieldRow({ label, value, onCopy, copied, multiline, placeholder }) {
  return (
    <div className="sub-field">
      <div className="sub-field-label">{label}</div>
      <div className={`sub-field-box ${multiline ? 'is-multi' : ''}`}>
        <code>{value || placeholder || '—'}</code>
        <button type="button" className="btn-secondary h-[34px] px-3 text-[12px]" disabled={!value} onClick={onCopy}>
          {copied ? '已复制' : '复制'}
        </button>
      </div>
    </div>
  )
}

function nodeMeta(item) {
  const bits = []
  if (item.protocol) bits.push(item.protocol)
  if (item.family === 'v6') bits.push('IPv6')
  else if (item.family === 'v4') bits.push('IPv4')
  if (item.rule_name && item.rule_name !== item.name) bits.push(item.rule_name)
  return bits.join(' · ')
}

function NodeRow({ item, probe, latLoading, copied, onCopy, rule, ruleBusy, onToggle }) {
  const st = statusView(item)
  return (
    <div className="sub-node-row">
      <div className="sub-node-main">
        <span className={`sub-status is-${st.tone}`}><i />{st.label}</span>
        <div className="min-w-0">
          <div className="sub-node-name">{item.name}</div>
          {nodeMeta(item) && <div className="sub-node-meta">{nodeMeta(item)}</div>}
          {item.block_text && <div className="sub-node-meta">{item.block_text}</div>}
        </div>
      </div>
      <div className="sub-node-side">
        <span className="sub-latency">{item.block_reason ? '' : latencyLabel(probe, latLoading)}</span>
        <button type="button" className="btn-secondary h-[32px] px-3 text-[12px]" disabled={!item.uri} onClick={onCopy}>
          {copied ? '已复制' : '复制'}
        </button>
        {rule && (
          <button type="button" className="btn-secondary h-[32px] px-3 text-[12px]" disabled={ruleBusy === rule.id} onClick={() => onToggle(rule)}>
            {ruleBusy === rule.id ? '…' : (rule.disabled ? '启用' : '停用')}
          </button>
        )}
      </div>
    </div>
  )
}

function RuleOnlyRow({ rule, ruleBusy, onToggle }) {
  const off = !!rule.disabled
  return (
    <div className="sub-node-row">
      <div className="sub-node-main">
        <span className={`sub-status ${off ? 'is-off' : 'is-unk'}`}><i />{off ? '已停用' : '未纳入'}</span>
        <div className="min-w-0">
          <div className="sub-node-name">{rule.name}</div>
          <div className="sub-node-meta">{rule.block_text || (off ? '已停用，入口不再转发。启用即可恢复。' : '这条规则没有可导入的节点')}</div>
        </div>
      </div>
      <div className="sub-node-side">
        <button type="button" className="btn-secondary h-[32px] px-3 text-[12px]" disabled={ruleBusy === rule.id} onClick={() => onToggle(rule)}>
          {ruleBusy === rule.id ? '…' : (off ? '启用' : '停用')}
        </button>
      </div>
    </div>
  )
}

function latencyLabel(probe, loading) {
  if (!probe) return loading ? '测速中…' : '—'
  if (probe.ok) return `${probe.latency_ms || 0} ms`
  if (probe.kind === 'direct' || (probe.error || '').includes('直连')) return '—'
  return '超时'
}

function statusView(item) {
  if (item.block_reason === 'quota' || item.block_reason === 'landing_quota' || item.block_reason === 'node_quota') {
    return { label: '流量用尽', tone: 'off' }
  }
  if (item.block_reason === 'account_expired' || item.block_reason === 'landing_expired') {
    return { label: '已到期', tone: 'off' }
  }
  if (item.block_reason === 'account_disabled') return { label: '已禁用', tone: 'off' }
  if (item.block_reason === 'node_offline' || item.status === 'offline') return { label: '离线', tone: 'off' }
  if (item.status === 'online') return { label: '在线', tone: 'on' }
  if (item.status === 'direct') return { label: item.block_reason ? '不可用' : '直连', tone: item.block_reason ? 'off' : 'direct' }
  return { label: '未知', tone: 'unk' }
}

function skipLabel(sk) {
  const detail = sk.detail ? `「${sk.detail}」` : '规则'
  if (sk.reason === 'custom') return `${detail} 是自定义出口，没有可导入的代理协议`
  if (sk.reason === 'no_entry') return `${detail} 尚未生成入口，暂时无法改写`
  if (sk.reason === 'disabled') return `${detail} 已停用`
  return `${detail} 未纳入（${sk.reason || '未知'}）`
}
