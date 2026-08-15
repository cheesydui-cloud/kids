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
  const clashCount = items.filter((it) => it.clash_ok).length
  const v2rayLines = useMemo(
    () => items.map((it) => it.uri).filter(Boolean).join('\n'),
    [items],
  )

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
    a.download = `${account.username || 'nft'}.yaml`
    document.body.appendChild(a)
    a.click()
    a.remove()
    toast('已开始下载 YAML')
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

  return (
    <Layout>
      <div className="sub-page">
        <header className="sub-hero">
          <div className="min-w-0">
            <p className="sub-kicker">Subscription</p>
            <h1 className="sub-title">我的订阅</h1>
            <p className="sub-lead">
              一条地址覆盖全部可用节点。中转规则改写到入口，直连节点保持原始落地。
            </p>
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
            <p>
              订阅只收录两类节点：已生成入口的落地中转规则，以及管理员标记为「直连」的落地。
              自定义出口（纯 host:port）无法生成客户端链接。
            </p>
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
              <div>
                <h3>小火箭</h3>
                <p>Shadowrocket 扫描订阅二维码，一次导入全部节点</p>
              </div>
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
              <div>
                <h3>V2rayN</h3>
                <p>订阅地址拉取全部节点，也可复制单条链接手动添加</p>
              </div>
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
              <div>
                <h3>Clash Verge</h3>
                <p>配置 → 新建订阅，粘贴下方地址即可远程拉取</p>
              </div>
            </div>
            <FieldRow label="订阅地址" value={data?.clash_url} onCopy={() => copy(data?.clash_url, 'clash', '已复制 Clash 订阅')} copied={copied === 'clash'} />
            <p className="sub-note">
              {clashCount} / {items.length} 个节点可写入 YAML
              {clashCount < items.length && items.length > 0 ? '（socks / naive / mieru / snell 仅出现在 URI 订阅）' : ''}
            </p>
          </article>

          <article className="sub-card">
            <div className="sub-card-head">
              <span className="sub-idx">04</span>
              <div>
                <h3>Mihomo</h3>
                <p>下载本地 YAML，或用同一地址作为远程配置拉取</p>
              </div>
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
            <div>
              <h2>节点清单</h2>
              <p>中转使用规则入口；直连使用落地原始地址。同一落地可以同时出现两种。</p>
            </div>
            <button type="button" className="btn-secondary" onClick={rotate}>重置订阅地址</button>
          </div>

          {relays.length > 0 && (
            <NodeGroup title={`中转 · ${relays.length}`} hint="规则落地改写到入口">
              {relays.map((it) => (
                <NodeRow key={`${it.kind}-${it.rule_id}-${it.family}-${it.name}`} item={it}
                  onCopy={() => copy(it.uri, `n-${it.name}`, '已复制节点')}
                  copied={copied === `n-${it.name}`} />
              ))}
            </NodeGroup>
          )}

          {directs.length > 0 && (
            <NodeGroup title={`直连 · ${directs.length}`} hint="不经过中转，直连落地">
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
      </div>
    </Layout>
  )
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

function NodeGroup({ title, hint, children }) {
  return (
    <div className="sub-group">
      <div className="sub-group-head">
        <h3>{title}</h3>
        <span>{hint}</span>
      </div>
      <div className="sub-group-list">{children}</div>
    </div>
  )
}

function NodeRow({ item, onCopy, copied }) {
  return (
    <div className="sub-node">
      <div className="min-w-0">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="font-semibold text-[14px]">{item.name}</span>
          <Badge color={item.kind === 'direct' ? 'amber' : 'teal'}>{item.kind === 'direct' ? '直连' : '中转'}</Badge>
          {item.protocol && <Badge color="gray">{item.protocol}</Badge>}
          {item.family === 'v6' && <Badge color="blue">IPv6</Badge>}
          {!item.clash_ok && <Badge color="gray">仅 URI</Badge>}
        </div>
        <p className="text-[12px] text-ink-mut mt-1 truncate">
          {item.rule_name ? `规则 ${item.rule_name}` : '直连落地'}
          {item.landing ? ` · ${item.landing}` : ''}
        </p>
      </div>
      <button type="button" className="btn-secondary h-[34px] px-3 text-[12px] flex-none" onClick={onCopy}>
        {copied ? '已复制' : '复制链接'}
      </button>
    </div>
  )
}

function skipLabel(sk) {
  const detail = sk.detail ? `「${sk.detail}」` : '规则'
  if (sk.reason === 'custom') return `${detail} 是自定义出口，没有可导入的代理协议`
  if (sk.reason === 'no_entry') return `${detail} 尚未生成入口，暂时无法改写`
  if (sk.reason === 'disabled') return `${detail} 已停用`
  return `${detail} 未纳入（${sk.reason || '未知'}）`
}
