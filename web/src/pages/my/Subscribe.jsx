import { useEffect, useMemo, useState } from 'react'
import QRCode from 'qrcode'
import { api } from '../../lib/api'
import { copyToClipboard } from '../../lib/clipboard'
import { fmtDate, fmtTrafficGB, isExpired, nullStr, pct } from '../../lib/fmt'
import { Layout, useToast } from '../../components/Layout'
import { UserPortalHead } from '../../components/UserPortalHead'
import { Badge, Loading } from '../../components/ui'

const LAT_CACHE_KEY = 'nf.sub.latency.v1'
const LAT_TTL_MS = 45_000

export default function MySubscribe() {
  const toast = useToast()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [qr, setQr] = useState('')
  const [qrErr, setQrErr] = useState('')
  const [copied, setCopied] = useState('')
  const [latency, setLatency] = useState({})
  const [latLoading, setLatLoading] = useState(false)

  const load = () => api.get('/my/subscribe')
    .then(setData)
    .catch((e) => { console.error(e); toast(e.message || '加载失败', 'error') })
    .finally(() => setLoading(false))

  useEffect(() => { load() }, [])

  const uriURL = data?.uri_url || ''
  useEffect(() => {
    if (!uriURL) { setQr(''); setQrErr(''); return }
    let cancelled = false
    QRCode.toDataURL(uriURL, {
      width: 280,
      margin: 2,
      errorCorrectionLevel: 'M',
      color: { dark: '#1b1612', light: '#fffdf9' },
    }).then((url) => {
      if (!cancelled) { setQr(url); setQrErr('') }
    }).catch((e) => {
      if (!cancelled) { setQr(''); setQrErr(e?.message || '二维码生成失败') }
    })
    return () => { cancelled = true }
  }, [uriURL])

  const items = data?.items || []
  const skipped = data?.skipped || []
  const account = data?.account || {}
  const v2rayLines = useMemo(
    () => items.map((it) => it.uri).filter(Boolean).join('\n'),
    [items],
  )
  const profileName = account.username || 'kids'

  const applyLatency = (d) => {
    const map = {}
    for (const it of d?.items || []) {
      map[latencyKey(it)] = it
    }
    setLatency(map)
  }

  const fetchLatency = (force) => {
    if (!data) return Promise.resolve()
    const cached = force ? null : readLatCache()
    if (cached) {
      applyLatency(cached)
      return Promise.resolve()
    }
    setLatLoading(true)
    const q = force ? '?refresh=1' : ''
    return api.get(`/my/subscribe/latency${q}`).then((d) => {
      applyLatency(d)
      writeLatCache(d)
    }).catch(() => {
      if (force) setLatency({})
    }).finally(() => setLatLoading(false))
  }

  useEffect(() => {
    if (!data) return
    let cancelled = false
    const cached = readLatCache()
    if (cached) {
      applyLatency(cached)
      return
    }
    setLatLoading(true)
    api.get('/my/subscribe/latency').then((d) => {
      if (cancelled) return
      applyLatency(d)
      writeLatCache(d)
    }).catch(() => {
      if (!cancelled) setLatency({})
    }).finally(() => {
      if (!cancelled) setLatLoading(false)
    })
    return () => { cancelled = true }
  }, [data])

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
    a.download = `${profileName}.yaml`
    document.body.appendChild(a)
    a.click()
    a.remove()
    toast('已开始下载 YAML')
  }

  if (loading) return <Layout><Loading /></Layout>

  const expiresAt = account.expires_at && account.expires_at > 0 ? account.expires_at : null
  const rate = account.billing_rate ?? 1
  const used = Math.round((account.traffic_used_bytes || 0) * (data?.show_rate ? rate : 1))
  const quota = account.traffic_quota_bytes || 0
  const empty = items.length === 0
  const expired = !!(expiresAt && isExpired(expiresAt))
  const quotaOut = quota > 0 && used >= quota
  const importBlocked = !!account.disabled || expired || quotaOut
  const blockReason = account.disabled
    ? (`账号已被禁用：${nullStr(account.disable_reason) || '请联系管理员'}`)
    : expired
      ? '订阅已过期，无法导入客户端'
      : quotaOut
        ? '流量已用完，无法导入客户端'
        : ''
  const imports = importHrefs(uriURL, data?.clash_url, data?.mihomo_url, profileName)

  const openImport = (href, fallback, opened) => {
    if (importBlocked) { toast(blockReason, 'error'); return }
    if (!href && !fallback) { toast('暂无订阅地址', 'error'); return }
    if (href) {
      const a = document.createElement('a')
      a.href = href
      a.rel = 'noreferrer'
      document.body.appendChild(a)
      a.click()
      a.remove()
      toast(opened || '已唤起客户端')
      return
    }
    copy(fallback, 'import', opened || '已复制订阅地址')
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

        <section className="sub-account" aria-label="账户概览">
          <div>
            <span className="sub-account-k">账户</span>
            <strong>{account.username || '—'}</strong>
          </div>
          <div>
            <span className="sub-account-k">{data?.show_rate ? '流量（计费）' : '流量'}</span>
            <strong className="font-mono">
              {fmtTrafficGB(used, quota)}
              {quota > 0 && <span className="text-ink-mut font-sans font-medium"> · {pct(used, quota)}%</span>}
            </strong>
          </div>
          <div>
            <span className="sub-account-k">到期</span>
            <strong>
              {expiresAt ? fmtDate(expiresAt) : '永不过期'}
              {expired && <Badge color="red" className="ml-2">已过期</Badge>}
            </strong>
          </div>
        </section>

        {empty && (
          <div className="sub-empty">
            <h2>还没有可导入的节点</h2>
            {skipped.length > 0 && (
              <ul>
                {skipped.map((sk, i) => (
                  <li key={i}>{skipLabel(sk)}</li>
                ))}
              </ul>
            )}
          </div>
        )}

        <section className="sub-grid">
          <article className="sub-card sub-card-qr">
            <div className="sub-card-head">
              <span className="sub-idx">01</span>
              <h3>小火箭</h3>
            </div>
            <div className="sub-qr-wrap">
              {qr ? (
                <img src={qr} alt="订阅二维码" className="sub-qr" />
              ) : (
                <div className="sub-qr sub-qr-ph">{qrErr || (empty ? '暂无节点' : '生成中…')}</div>
              )}
            </div>
            <div className="sub-card-actions">
              <button type="button" className="btn-secondary h-[34px] px-3 text-[12px]" disabled={!uriURL || importBlocked}
                onClick={() => copy(uriURL, 'qr', '已复制小火箭订阅地址')}>
                {copied === 'qr' ? '已复制' : '复制订阅地址'}
              </button>
            </div>
          </article>

          <article className="sub-card">
            <div className="sub-card-head">
              <span className="sub-idx">02</span>
              <h3>V2rayN</h3>
            </div>
            <FieldRow label="订阅地址" value={uriURL} disabled={importBlocked} onCopy={() => copy(uriURL, 'v2sub', '已复制 V2rayN 订阅')} copied={copied === 'v2sub'} />
            <FieldRow
              label="节点链接"
              value={v2rayLines}
              multiline
              disabled={importBlocked}
              placeholder={empty ? '暂无节点链接' : ''}
              onCopy={() => copy(v2rayLines, 'v2uri', '已复制全部节点链接')}
              copied={copied === 'v2uri'}
            />
          </article>

          <article className="sub-card">
            <div className="sub-card-head">
              <span className="sub-idx">03</span>
              <h3>Clash Verge</h3>
            </div>
            <FieldRow label="订阅地址" value={data?.clash_url} disabled={importBlocked} onCopy={() => copy(data?.clash_url, 'clash', '已复制 Clash 订阅')} copied={copied === 'clash'} />
          </article>

          <article className="sub-card">
            <div className="sub-card-head">
              <span className="sub-idx">04</span>
              <h3>Mihomo</h3>
            </div>
            <FieldRow label="拉取地址" value={data?.mihomo_url} disabled={importBlocked} onCopy={() => copy(data?.mihomo_url, 'mihomo', '已复制 Mihomo 订阅')} copied={copied === 'mihomo'} />
            <div className="flex flex-wrap gap-2 mt-3">
              <button type="button" className="btn-secondary" disabled={empty || importBlocked} onClick={downloadYaml}>下载 YAML</button>
              <button type="button" className="btn-secondary" disabled={!data?.mihomo_url || importBlocked}
                onClick={() => copy(data?.mihomo_url, 'mihomo2', '已复制拉取地址')}>
                {copied === 'mihomo2' ? '已复制' : '复制拉取地址'}
              </button>
            </div>
          </article>
        </section>

        <section className="sub-nodes">
          <div className="sub-nodes-head">
            <h2>节点清单</h2>
            <button
              type="button"
              className="btn-secondary h-[32px] px-3 text-[12px]"
              disabled={latLoading || items.length === 0}
              onClick={() => fetchLatency(true).then(() => toast('已刷新延迟'))}
            >
              {latLoading ? '测速中…' : '刷新延迟'}
            </button>
          </div>
          {items.length > 0 && (
            <div className="sub-node-grid">
              {items.map((it) => (
                <NodeCard
                  key={`${it.kind}-${it.rule_id}-${it.family}-${it.name}`}
                  item={it}
                  probe={latency[latencyKey(it)]}
                />
              ))}
            </div>
          )}

          {skipped.length > 0 && items.length > 0 && (
            <div className="sub-skip">
              <h3>未纳入订阅</h3>
              <ul>
                {skipped.map((sk, i) => <li key={i}>{skipLabel(sk)}</li>)}
              </ul>
            </div>
          )}
        </section>

        <section className="sub-import">
          <h2>一键导入客户端</h2>
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
        </section>
      </div>
    </Layout>
  )
}

function readLatCache() {
  try {
    const raw = sessionStorage.getItem(LAT_CACHE_KEY)
    if (!raw) return null
    const d = JSON.parse(raw)
    if (!d || !d.at || !Array.isArray(d.items)) return null
    if (Date.now() - d.at > LAT_TTL_MS) return null
    return d
  } catch {
    return null
  }
}

function writeLatCache(d) {
  try {
    sessionStorage.setItem(LAT_CACHE_KEY, JSON.stringify({
      at: Date.now(),
      items: d?.items || [],
    }))
  } catch {}
}

function latencyKey(it) {
  return `${it.kind || ''}|${it.rule_id || 0}|${it.family || ''}|${it.name || ''}`
}

function importHrefs(uriURL, clashURL, mihomoURL, name) {
  const label = encodeURIComponent(name || 'kids')
  const enc = (u) => encodeURIComponent(u)
  return {
    // One scheme only: firing two in a row makes Verge import once then the
    // page thinks the app failed and toasts a copy error.
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

function FieldRow({ label, value, onCopy, copied, multiline, placeholder, disabled }) {
  return (
    <div className="sub-field">
      <div className="sub-field-label">{label}</div>
      <div className={`sub-field-box ${multiline ? 'is-multi' : ''}`}>
        <code>{value || placeholder || '—'}</code>
        <button type="button" className="btn-secondary h-[34px] px-3 text-[12px]" disabled={!value || disabled} onClick={onCopy}>
          {copied ? '已复制' : '复制'}
        </button>
      </div>
    </div>
  )
}

function NodeCard({ item, probe }) {
  const st = statusView(item)
  return (
    <article className="sub-node-card">
      <div className="sub-node-card-top">
        <div className={`sub-status is-${st.tone}`}>
          <i />
          {st.label}
        </div>
        <div className="sub-latency">{latencyLabel(probe)}</div>
      </div>
      <h3>{item.name}</h3>
    </article>
  )
}

function latencyLabel(probe) {
  if (!probe) return '测速中…'
  if (probe.ok) return `${probe.latency_ms || 0} ms`
  if (probe.kind === 'direct' || (probe.error || '').includes('直连')) return '—'
  return '超时'
}

function statusView(item) {
  if (item.status === 'online') return { label: '在线', tone: 'on' }
  if (item.status === 'offline') return { label: '离线', tone: 'off' }
  if (item.status === 'direct') return { label: '直连', tone: 'direct' }
  return { label: '未知', tone: 'unk' }
}

function skipLabel(sk) {
  const detail = sk.detail ? `「${sk.detail}」` : '规则'
  if (sk.reason === 'custom') return `${detail} 是自定义出口，没有可导入的代理协议`
  if (sk.reason === 'no_entry') return `${detail} 尚未生成入口，暂时无法改写`
  if (sk.reason === 'disabled') return `${detail} 已停用`
  return `${detail} 未纳入（${sk.reason || '未知'}）`
}
