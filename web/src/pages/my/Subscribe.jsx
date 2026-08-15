import { useEffect, useMemo, useState } from 'react'
import QRCode from 'qrcode'
import { api } from '../../lib/api'
import { copyToClipboard } from '../../lib/clipboard'
import { fmtDate, fmtTrafficGB, isExpired, nullStr, pct } from '../../lib/fmt'
import { Layout, useToast } from '../../components/Layout'
import { Badge, Loading, useConfirm } from '../../components/ui'

export default function MySubscribe() {
  const toast = useToast()
  const confirm = useConfirm()
  const [data, setData] = useState(null)
  const [loading, setLoading] = useState(true)
  const [qr, setQr] = useState('')
  const [qrErr, setQrErr] = useState('')
  const [copied, setCopied] = useState('')

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
  const relays = items.filter((it) => it.kind === 'relay')
  const directs = items.filter((it) => it.kind === 'direct')
  const v2rayLines = useMemo(
    () => items.map((it) => it.uri).filter(Boolean).join('\n'),
    [items],
  )
  const profileName = account.username || 'kids'

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

  const openImport = (hrefs, fallback, ok) => {
    const list = (Array.isArray(hrefs) ? hrefs : [hrefs]).filter(Boolean)
    if (list.length === 0) { toast('暂无订阅地址', 'error'); return }
    const started = Date.now()
    list.forEach((href, i) => {
      window.setTimeout(() => {
        const a = document.createElement('a')
        a.href = href
        a.rel = 'noreferrer'
        document.body.appendChild(a)
        a.click()
        a.remove()
      }, i * 80)
    })
    window.setTimeout(() => {
      if (document.hidden) return
      if (Date.now() - started < 400) return
      if (fallback) copy(fallback, 'import', ok)
    }, 1100)
  }

  const rotate = async () => {
    const ok = await confirm({
      title: '重置订阅地址',
      message: '旧的订阅链接会立刻失效，已导入的客户端需要重新添加。确定重置吗？',
      confirmText: '重置',
      danger: true,
    })
    if (!ok) return
    try {
      const next = await api.post('/my/subscribe/rotate')
      setData((d) => ({ ...d, ...next }))
      toast('订阅地址已重置')
    } catch (e) {
      toast(e.message || '重置失败', 'error')
    }
  }

  if (loading) return <Layout><Loading /></Layout>

  const expiresAt = account.expires_at && account.expires_at > 0 ? account.expires_at : null
  const rate = account.billing_rate ?? 1
  const used = Math.round((account.traffic_used_bytes || 0) * (data?.show_rate ? rate : 1))
  const quota = account.traffic_quota_bytes || 0
  const empty = items.length === 0
  const imports = importHrefs(uriURL, data?.clash_url, data?.mihomo_url, profileName)

  return (
    <Layout>
      <div className="sub-page">
        <header className="sub-hero">
          <div className="min-w-0">
            <p className="sub-kicker">Subscription</p>
            <h1 className="sub-title">我的订阅</h1>
          </div>
          <div className="sub-hero-meta">
            <MetaChip label="节点">{items.length}</MetaChip>
            <MetaChip label="中转">{relays.length}</MetaChip>
            <MetaChip label="直连">{directs.length}</MetaChip>
          </div>
        </header>

        {account.disabled && (
          <div className="mb-4 px-4 py-3 bg-transparent border-[1.5px] border-rose-500/40 rounded-xl text-rose-700 dark:text-rose-300 text-sm font-medium">
            账号已被禁用：{nullStr(account.disable_reason) || '请联系管理员'}
          </div>
        )}

        <section className="sub-account">
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
              {expiresAt && isExpired(expiresAt) && <Badge color="red" className="ml-2">已过期</Badge>}
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
            <button type="button" className="btn-primary w-full justify-center" disabled={!uriURL}
              onClick={() => copy(uriURL, 'qr', '已复制小火箭订阅地址')}>
              {copied === 'qr' ? '已复制' : '复制订阅地址'}
            </button>
          </article>

          <article className="sub-card">
            <div className="sub-card-head">
              <span className="sub-idx">02</span>
              <h3>V2rayN</h3>
            </div>
            <FieldRow label="订阅地址" value={uriURL} onCopy={() => copy(uriURL, 'v2sub', '已复制 V2rayN 订阅')} copied={copied === 'v2sub'} />
            <FieldRow
              label="节点链接"
              value={v2rayLines}
              multiline
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
            <FieldRow label="订阅地址" value={data?.clash_url} onCopy={() => copy(data?.clash_url, 'clash', '已复制 Clash 订阅')} copied={copied === 'clash'} />
          </article>

          <article className="sub-card">
            <div className="sub-card-head">
              <span className="sub-idx">04</span>
              <h3>Mihomo</h3>
            </div>
            <FieldRow label="拉取地址" value={data?.mihomo_url} onCopy={() => copy(data?.mihomo_url, 'mihomo', '已复制 Mihomo 订阅')} copied={copied === 'mihomo'} />
            <div className="flex flex-wrap gap-2 mt-3">
              <button type="button" className="btn-secondary" disabled={empty} onClick={downloadYaml}>下载 YAML</button>
              <button type="button" className="btn-secondary" disabled={!data?.mihomo_url}
                onClick={() => copy(data?.mihomo_url, 'mihomo2', '已复制拉取地址')}>
                {copied === 'mihomo2' ? '已复制' : '复制拉取地址'}
              </button>
            </div>
          </article>
        </section>

        <section className="sub-nodes">
          <div className="sub-nodes-head">
            <h2>节点清单</h2>
            <button type="button" className="btn-secondary" onClick={rotate}>重置订阅地址</button>
          </div>

          {relays.length > 0 && (
            <NodeGroup title={`中转 · ${relays.length}`}>
              {relays.map((it) => (
                <NodeRow key={`${it.kind}-${it.rule_id}-${it.family}-${it.name}`} item={it}
                  onCopy={() => copy(it.uri, `n-${it.name}`, '已复制节点')}
                  copied={copied === `n-${it.name}`} />
              ))}
            </NodeGroup>
          )}

          {directs.length > 0 && (
            <NodeGroup title={`直连 · ${directs.length}`}>
              {directs.map((it) => (
                <NodeRow key={`${it.kind}-${it.name}`} item={it}
                  onCopy={() => copy(it.uri, `n-${it.name}`, '已复制节点')}
                  copied={copied === `n-${it.name}`} />
              ))}
            </NodeGroup>
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
          <h2>一键导入</h2>
          <div className="sub-import-grid">
            <button type="button" className="sub-import-btn" disabled={!uriURL}
              onClick={() => openImport(imports.shadowrocket, uriURL, '未打开小火箭，已复制全部节点订阅')}>
              <ImportMark kind="rocket" />
              <span>小火箭</span>
            </button>
            <button type="button" className="sub-import-btn" disabled={!uriURL}
              onClick={() => openImport(imports.v2rayn, uriURL, '未打开 V2rayN，已复制全部节点订阅')}>
              <ImportMark kind="v2" />
              <span>V2rayN</span>
            </button>
            <button type="button" className="sub-import-btn" disabled={!data?.clash_url}
              onClick={() => openImport(imports.clash, data?.clash_url, '未打开 Clash Verge，已复制全部节点订阅')}>
              <ImportMark kind="clash" />
              <span>Clash Verge</span>
            </button>
            <button type="button" className="sub-import-btn" disabled={!data?.mihomo_url}
              onClick={() => openImport(imports.mihomo, data?.mihomo_url, '未打开 Mihomo，已复制全部节点订阅')}>
              <ImportMark kind="mihomo" />
              <span>Mihomo</span>
            </button>
          </div>
        </section>
      </div>
    </Layout>
  )
}

function importHrefs(uriURL, clashURL, mihomoURL, name) {
  const label = encodeURIComponent(name || 'kids')
  const enc = (u) => encodeURIComponent(u)
  return {
    shadowrocket: uriURL ? [
      `shadowrocket://add/sub://${btoaUtf8(uriURL)}?remark=${label}`,
      `sub://${btoaUtf8(uriURL)}`,
    ] : [],
    v2rayn: uriURL ? [
      `v2rayn://install-sub?url=${enc(uriURL)}&name=${label}`,
      `v2rayng://install-sub?url=${enc(uriURL)}&name=${label}`,
    ] : [],
    clash: clashURL ? [
      `clash://install-config?url=${enc(clashURL)}&name=${label}`,
      `clash-verge://install-config?url=${enc(clashURL)}&name=${label}`,
    ] : [],
    mihomo: mihomoURL ? [
      `clash://install-config?url=${enc(mihomoURL)}&name=${label}`,
      `mihomo://install-config?url=${enc(mihomoURL)}&name=${label}`,
    ] : [],
  }
}

function btoaUtf8(text) {
  const bytes = new TextEncoder().encode(text)
  let bin = ''
  bytes.forEach((b) => { bin += String.fromCharCode(b) })
  return btoa(bin)
}

function MetaChip({ label, children }) {
  return (
    <div className="sub-chip">
      <span>{label}</span>
      <strong>{children}</strong>
    </div>
  )
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

function NodeGroup({ title, children }) {
  return (
    <div className="sub-group">
      <div className="sub-group-head">
        <h3>{title}</h3>
      </div>
      <div className="sub-group-list">{children}</div>
    </div>
  )
}

function NodeRow({ item, onCopy, copied }) {
  const st = statusView(item)
  return (
    <div className="sub-node">
      <div className="min-w-0">
        <span className="font-semibold text-[14px]">{item.name}</span>
      </div>
      <div className={`sub-status is-${st.tone}`}>
        <i />
        {st.label}
      </div>
      <button type="button" className="btn-secondary h-[34px] px-3 text-[12px] flex-none" onClick={onCopy}>
        {copied ? '已复制' : '复制链接'}
      </button>
    </div>
  )
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

function ImportMark({ kind }) {
  if (kind === 'rocket') {
    return (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M5 15c-1 3 2 6 5 5l7-7a8 8 0 0 0 2-7 8 8 0 0 0-7 2Z"/>
        <path d="M9 15s.5 2 3 3"/>
        <circle cx="15" cy="9" r="1.2"/>
      </svg>
    )
  }
  if (kind === 'v2') {
    return (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M4 7h16v10H4z"/>
        <path d="M8 11h8M8 14h5"/>
      </svg>
    )
  }
  if (kind === 'clash') {
    return (
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M12 3 4 7v10l8 4 8-4V7Z"/>
        <path d="M12 12 4 8M12 12v10M12 12l8-4"/>
      </svg>
    )
  }
  return (
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="8"/>
      <path d="M8 12h8M12 8v8"/>
    </svg>
  )
}
