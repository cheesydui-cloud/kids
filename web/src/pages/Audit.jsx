import { useEffect, useState } from 'react'
import { api } from '../lib/api'
import { fmtDate, unixFromDateInput } from '../lib/fmt'
import { Layout } from '../components/Layout'
import { Loading, Empty, Badge, DateInput } from '../components/ui'
import { PageHeader, Panel, PanelToolbar, SearchInput, ToolbarButton, ToolbarActions, TableScroll } from '../components/page'
import { useIsMobile } from '../lib/useIsMobile'

const PAGE_SIZE = 50

// Coarse action families get a colored pill; the exact action token stays
// visible next to it so nothing is lost for operators who grep by name.
const ACTION_GROUPS = [
  ['user_folder.', '用户分组', 'violet'],
  ['user.', '用户', 'blue'],
  ['node_repo.', '落地仓库', 'teal'],
  ['node.', '节点', 'green'],
  ['rule.', '规则', 'amber'],
  ['settings.', '设置', 'gray'],
  ['cf.', 'Cloudflare', 'cyan'],
  ['admin.', '管理员', 'red'],
]

function actionMeta(action) {
  if (action === 'login') return { label: '登录', color: 'gray' }
  for (const [prefix, label, color] of ACTION_GROUPS) {
    if (action.startsWith(prefix)) return { label, color }
  }
  return { label: '其它', color: 'gray' }
}

function ActionCell({ action }) {
  const meta = actionMeta(action)
  return (
    <span className="inline-flex items-center gap-2">
      <Badge color={meta.color}>{meta.label}</Badge>
      <span className="font-mono text-[12px] text-ink-soft break-all">{action}</span>
    </span>
  )
}

// Date inputs are wall-clock days; clamp from/to to the enclosing day so a
// single-day range is inclusive.
function dayStartUnix(s) {
  if (!s) return 0
  const m = /^(\d{4})-(\d{1,2})-(\d{1,2})/.exec(s.trim())
  if (!m) return unixFromDateInput(s)
  return Math.floor(new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), 0, 0, 0).getTime() / 1000)
}

function dayEndUnix(s) {
  if (!s) return 0
  const m = /^(\d{4})-(\d{1,2})-(\d{1,2})/.exec(s.trim())
  if (!m) return unixFromDateInput(s)
  return Math.floor(new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]), 23, 59, 59).getTime() / 1000)
}

export default function Audit() {
  const [logs, setLogs] = useState(null)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [q, setQ] = useState('')
  const [from, setFrom] = useState('')
  const [to, setTo] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const isMobile = useIsMobile()

  // `override` lets 重置 clear the filters and fetch in the same tick; reading
  // the state variables directly would still see the pre-reset values.
  const load = (nextPage = page, override = {}) => {
    setLoading(true)
    setError('')
    const qv = override.q !== undefined ? override.q : q
    const fromv = override.from !== undefined ? override.from : from
    const tov = override.to !== undefined ? override.to : to
    const params = new URLSearchParams()
    if (qv.trim()) params.set('q', qv.trim())
    const fromTs = dayStartUnix(fromv)
    const toTs = dayEndUnix(tov)
    if (fromTs) params.set('from', String(fromTs))
    if (toTs) params.set('to', String(toTs))
    params.set('page', String(nextPage))
    params.set('page_size', String(PAGE_SIZE))
    api.get(`/audit-logs?${params.toString()}`)
      .then(d => {
        setLogs(d?.logs || [])
        setTotal(d?.total || 0)
        setPage(nextPage)
      })
      .catch(e => setError(e?.message || '加载失败'))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load(1) }, [])

  const pages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const reset = () => {
    setQ(''); setFrom(''); setTo('')
    load(1, { q: '', from: '', to: '' })
  }

  return (
    <Layout>
      <div className="h-full flex flex-col">
        <PageHeader title="审计日志" count={total} unit="条记录" />

        <Panel fill>
          <PanelToolbar>
            <SearchInput value={q} onChange={setQ} placeholder="搜索动作、对象、详情、用户…" />
            <div className="w-[190px] max-w-full shrink-0">
              <DateInput value={from} onChange={setFrom} placeholder="开始日期" className="w-full" />
            </div>
            <div className="w-[190px] max-w-full shrink-0">
              <DateInput value={to} onChange={setTo} placeholder="结束日期" className="w-full" />
            </div>
            <ToolbarActions>
              <ToolbarButton onClick={() => load(1)}>查询</ToolbarButton>
              <ToolbarButton secondary onClick={reset}>重置</ToolbarButton>
            </ToolbarActions>
          </PanelToolbar>

          {error ? (
            <Empty title="加载失败" desc={error}>
              <button onClick={() => load(page)} className="btn-secondary text-xs mt-3">重试</button>
            </Empty>
          ) : loading && !logs ? (
            <Loading />
          ) : !logs || logs.length === 0 ? (
            <Empty title="暂无审计记录" desc="管理员在面板里的关键操作会记录在这里。" />
          ) : (
            <TableScroll>
              {isMobile ? (
                <div>
                  {logs.map(l => (
                    <div key={l.id} className="mobile-card">
                      <div className="flex items-center justify-between gap-2 mb-1.5">
                        <ActionCell action={l.action} />
                        <span className="text-[11px] text-ink-mut whitespace-nowrap">{fmtDate(l.at)}</span>
                      </div>
                      <div className="text-[13px] text-ink-soft break-all">{l.target || '—'}</div>
                      {l.payload && <div className="text-[12px] text-ink-mut mt-1 break-all">{l.payload}</div>}
                      <div className="text-[11px] text-ink-mut mt-1.5">{l.username || (l.user_id ? `#${l.user_id}` : '系统')}</div>
                    </div>
                  ))}
                </div>
              ) : (
                <table className="tbl">
                  <thead><tr>
                    <th className="w-[150px]">时间</th>
                    <th className="w-[130px]">操作者</th>
                    <th>动作</th>
                    <th>对象</th>
                    <th>详情</th>
                  </tr></thead>
                  <tbody>
                    {logs.map(l => (
                      <tr key={l.id}>
                        <td className="font-mono text-xs text-ink-mut whitespace-nowrap">{fmtDate(l.at)}</td>
                        <td className="text-xs text-ink-soft">{l.username || (l.user_id ? `#${l.user_id}` : '系统')}</td>
                        <td><ActionCell action={l.action} /></td>
                        <td className="font-mono text-xs text-ink-soft max-w-[220px] truncate" title={l.target}>{l.target || '—'}</td>
                        <td className="text-xs text-ink-soft max-w-[360px] truncate" title={l.payload}>{l.payload || '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
            </TableScroll>
          )}

          <div className="flex items-center justify-between gap-3 px-5 py-3 border-t border-line-soft text-[12.5px] text-ink-mut flex-wrap">
            <span>共 {total} 条 · 第 {page}/{pages} 页</span>
            <div className="flex items-center gap-2">
              <button type="button" disabled={page <= 1 || loading} onClick={() => load(page - 1)} className="btn-secondary !h-[32px] text-xs">上一页</button>
              <button type="button" disabled={page >= pages || loading} onClick={() => load(page + 1)} className="btn-secondary !h-[32px] text-xs">下一页</button>
            </div>
          </div>
        </Panel>
      </div>
    </Layout>
  )
}
