import { useState, useEffect, useMemo } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { billedUsedBytes, fmtBytes, fmtTime, fmtDate, nullStr, nullInt, expiryBadge } from '../lib/fmt'
import { Layout } from '../components/Layout'
import { Loading, Badge, ErrorState } from '../components/ui'
import { PageHeader, TableBox } from '../components/page'
import { HourlyTrafficChart } from '../components/HourlyTrafficChart'

const DAY = 86400

export default function Dashboard() {
  const [data, setData] = useState(null)
  const [users, setUsers] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = () => {
    setLoading(true)
    setError('')
    Promise.all([
      api.get('/dashboard'),
      api.get('/users').catch(() => null),
    ]).then(([dash, userList]) => {
      setData(dash)
      setUsers(userList?.users || null)
    }).catch(e => {
      setError(e.message || '加载失败')
      setData(null)
    }).finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const visibleData = useMemo(() => {
    if (!data) return data
    return { ...data, nodes: (data.nodes || []).filter(n => n.node_type !== 'self') }
  }, [data])
  const tenantUsers = useMemo(
    () => (Array.isArray(users) ? users.filter(u => u.username !== 'admin') : users),
    [users],
  )
  const attention = useMemo(() => buildAttention(visibleData, tenantUsers), [visibleData, tenantUsers])
  const expiryCalendar = useMemo(
    () => buildExpiryCalendar(tenantUsers, data?.landing_expiring || []),
    [tenantUsers, data?.landing_expiring],
  )

  if (loading) return <Layout><Loading /></Layout>
  if (error || !data) {
    return (
      <Layout>
        <ErrorState title="无法加载概览" desc={error || '请稍后重试。'} onRetry={load} />
      </Layout>
    )
  }

  const { total_bytes = 0, user_count = 0, today_raw_bytes = 0 } = data
  const visibleUsers = Array.isArray(users) ? users.filter(u => u.username !== 'admin') : null
  const visibleUserCount = visibleUsers ? visibleUsers.length : user_count

  const metrics = [
    { key: 'billed', label: '计费', value: fmtBytes(total_bytes), hint: '累计 × 倍率' },
    { key: 'today', label: '今日', value: fmtBytes(today_raw_bytes || 0), hint: '实际最后一跳' },
    { key: 'users', label: '用户', value: visibleUserCount, hint: '系统用户' },
  ]

  return (
    <Layout>
      <PageHeader title="运营概览" />

      <div className="card mb-5">
        <div className="grid grid-cols-1 sm:grid-cols-3">
          {metrics.map((m, i) => (
            <div
              key={m.key}
              className={`px-5 py-4 ${i < metrics.length - 1 ? 'sm:border-r border-line-soft' : ''} ${i < metrics.length - 1 ? 'border-b sm:border-b-0 border-line-soft' : ''}`}
            >
              <div className="text-[12px] font-medium text-ink-mut">{m.label}</div>
              <div className={`mt-1.5 text-[22px] font-bold leading-none tracking-tight tabular-nums ${m.accent ? 'text-green-600 dark:text-green-400' : 'text-ink'}`}>
                {m.value}
              </div>
              <div className="mt-1.5 text-[12px] text-ink-mut truncate">{m.hint}</div>
            </div>
          ))}
        </div>
      </div>

      <HourlyTrafficChart series={data.hourly_raw || []} />

      {attention.length > 0 && (
        <div className="card mb-5">
          <div className="card-header justify-between">
            <h3 className="text-[15px] font-bold">需关注</h3>
            <span className="text-[12.5px] text-ink-mut">{attention.length} 项</span>
          </div>
          <div className="attention-list">
            {attention.map(item => (
              <Link key={item.key} to={item.to} className="attention-item">
                <span className={`attention-dot ${item.tone}`} />
                <div className="min-w-0 flex-1">
                  <div className="text-[13.5px] font-semibold text-ink truncate">{item.title}</div>
                  <div className="text-[12.5px] text-ink-mut mt-0.5 truncate">{item.desc}</div>
                </div>
                <Badge color={item.tone === 'danger' ? 'red' : item.tone === 'warn' ? 'amber' : 'blue'}>{item.tag}</Badge>
              </Link>
            ))}
          </div>
        </div>
      )}

      {expiryCalendar.length > 0 && (
        <div className="card mb-5">
          <div className="card-header justify-between">
            <h3 className="text-[15px] font-bold">7 天内到期</h3>
            <span className="text-[12.5px] text-ink-mut">{expiryCalendar.length} 项</span>
          </div>
          <TableBox>
            <table className="tbl">
              <thead>
                <tr>
                  <th>类型</th>
                  <th>用户</th>
                  <th>名称</th>
                  <th>到期日</th>
                  <th>剩余</th>
                  <th className="text-right">状态</th>
                </tr>
              </thead>
              <tbody>
                {expiryCalendar.map(row => (
                  <tr key={row.key}>
                    <td>
                      <Badge color={row.kind === 'user' ? 'blue' : 'emerald'}>
                        {row.kind === 'user' ? '账号' : '落地'}
                      </Badge>
                    </td>
                    <td>
                      <Link to={`/users/${row.userId}`} className="font-semibold link-accent hover:underline">
                        {row.username}
                      </Link>
                    </td>
                    <td className="text-ink-soft text-[13px] truncate max-w-[220px]" title={row.name}>
                      {row.name}
                    </td>
                    <td className="font-mono text-xs">{fmtDate(row.expiresAt)}</td>
                    <td className="font-mono text-xs">
                      {row.daysLeft < 0
                        ? <span className="text-red-600">已过期</span>
                        : row.daysLeft === 0
                          ? <span className="text-amber-600">今天</span>
                          : `${row.daysLeft} 天`}
                    </td>
                    <td className="text-right">
                      {row.badge
                        ? <Badge color={row.badge.color}>{row.badge.label}</Badge>
                        : <span className="text-ink-mut text-xs">—</span>}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </TableBox>
        </div>
      )}
    </Layout>
  )
}

function buildAttention(data, users) {
  if (!data) return []
  const items = []
  const now = Math.floor(Date.now() / 1000)
  const nodes = data.nodes || []

  for (const n of nodes) {
    if (n.disabled) {
      items.push({
        key: `node-dis-${n.id}`,
        to: `/nodes/${n.id}`,
        tone: 'warn',
        tag: '禁用',
        title: n.name,
        desc: '节点已禁用',
      })
      continue
    }
    const lastErr = nullStr(n.last_error)
    if (n.node_type !== 'composite' && lastErr) {
      items.push({
        key: `node-err-${n.id}`,
        to: `/nodes/${n.id}`,
        tone: 'danger',
        tag: '错误',
        title: n.name,
        desc: lastErr.slice(0, 80),
      })
      continue
    }
    if (n.online !== 1) {
      items.push({
        key: `node-off-${n.id}`,
        to: `/nodes/${n.id}`,
        tone: 'danger',
        tag: '离线',
        title: n.name,
        desc: n.last_seen ? `最后心跳 ${fmtTime(n.last_seen)}` : '暂无心跳',
      })
    }
  }

  if (Array.isArray(users)) {
    for (const u of users) {
      if (u.role === 'admin') continue
      const exp = nullInt(u.expires_at) || 0
      if (exp > 0) {
        if (exp <= now) {
          items.push({
            key: `user-exp-${u.id}`,
            to: `/users/${u.id}`,
            tone: 'danger',
            tag: '已到期',
            title: u.username,
            desc: '账号已过期，请续期或禁用',
          })
        } else if (exp - now <= 7 * DAY) {
          const days = Math.max(1, Math.ceil((exp - now) / DAY))
          items.push({
            key: `user-soon-${u.id}`,
            to: `/users/${u.id}`,
            tone: 'warn',
            tag: `${days} 天内到期`,
            title: u.username,
            desc: '账号即将到期',
          })
        }
      }
      const quota = u.traffic_quota_bytes || 0
      const used = billedUsedBytes(u.traffic_used_bytes, u.billing_rate)
      if (quota > 0) {
        const ratio = used / quota
        if (ratio >= 1) {
          items.push({
            key: `user-quota-${u.id}`,
            to: `/users/${u.id}`,
            tone: 'danger',
            tag: '流量用尽',
            title: u.username,
            desc: `${fmtBytes(used)} / ${fmtBytes(quota)}`,
          })
        } else if (ratio >= 0.9) {
          items.push({
            key: `user-quota-high-${u.id}`,
            to: `/users/${u.id}`,
            tone: 'warn',
            tag: '流量将尽',
            title: u.username,
            desc: `已用 ${Math.round(ratio * 100)}%`,
          })
        }
      }
    }
  }

  const rank = { danger: 0, warn: 1, info: 2 }
  items.sort((a, b) => (rank[a.tone] ?? 9) - (rank[b.tone] ?? 9))
  return items.slice(0, 8)
}

/** Merge account + landing exits due within 7 days. Hidden when empty. */
function buildExpiryCalendar(users, landingExpiring) {
  const now = Math.floor(Date.now() / 1000)
  const rows = []

  if (Array.isArray(users)) {
    for (const u of users) {
      if (u.role === 'admin' || u.disabled) continue
      const exp = nullInt(u.expires_at) || 0
      if (exp <= 0) continue
      if (exp > now + 7 * DAY) continue
      const daysLeft = Math.ceil((exp - now) / DAY)
      rows.push({
        key: `user-${u.id}`,
        kind: 'user',
        userId: u.id,
        username: u.username,
        name: '账号到期',
        expiresAt: exp,
        daysLeft,
        badge: expiryBadge(exp),
      })
    }
  }

  if (Array.isArray(landingExpiring)) {
    for (const e of landingExpiring) {
      const exp = e.expires_at || 0
      if (exp <= 0) continue
      const daysLeft = Math.ceil((exp - now) / DAY)
      const hostPort = e.port ? `${e.host}:${e.port}` : e.host
      rows.push({
        key: `land-${e.user_id}-${e.host}-${e.port}`,
        kind: 'landing',
        userId: e.user_id,
        username: e.username,
        name: e.name || hostPort || '落地',
        expiresAt: exp,
        daysLeft,
        badge: expiryBadge(exp),
      })
    }
  }

  rows.sort((a, b) => a.expiresAt - b.expiresAt)
  return rows.slice(0, 50)
}
