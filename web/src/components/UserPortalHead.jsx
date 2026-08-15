import { useEffect, useState } from 'react'
import { NavLink } from 'react-router-dom'
import { api } from '../lib/api'
import { fmtDate } from '../lib/fmt'
import { BrandBadge } from './BrandMark'
import { Badge, Modal } from './ui'
import { useUser } from './Layout'
import { clearLoginAnnouncementSession } from './LoginAnnouncementModal'

const annColorMeta = {
  red: { badge: 'red', label: '紧急', bar: 'border-l-rose-500' },
  amber: { badge: 'amber', label: '重要', bar: 'border-l-amber-500' },
  blue: { badge: 'blue', label: '信息', bar: 'border-l-sky-500' },
  green: { badge: 'green', label: '成功', bar: 'border-l-emerald-500' },
}

export function UserPortalHead({ title = '我的订阅' }) {
  const { logoUrl } = useUser()
  const [annOpen, setAnnOpen] = useState(false)
  const [items, setItems] = useState([])
  const [loading, setLoading] = useState(false)
  const readKey = 'announcements.read'
  const [readIds, setReadIds] = useState(() => {
    try { return new Set(JSON.parse(localStorage.getItem(readKey) || '[]')) } catch { return new Set() }
  })

	  useEffect(() => {
	    let cancelled = false
	    api.get('/my/announcements')
	      .then((d) => { if (!cancelled) setItems(d?.announcements || []) })
	      .catch(() => { if (!cancelled) setItems([]) })
	    return () => { cancelled = true }
	  }, [])

	  useEffect(() => {
	    if (!annOpen) return
	    setLoading(true)
	    api.get('/my/announcements')
	      .then((d) => setItems(d?.announcements || []))
	      .catch(() => setItems([]))
	      .finally(() => setLoading(false))
	  }, [annOpen])

	  const unread = items.filter((a) => !readIds.has(a.id)).length

  const markRead = (id) => {
    setReadIds((prev) => {
      if (prev.has(id)) return prev
      const next = new Set(prev)
      next.add(id)
      localStorage.setItem(readKey, JSON.stringify([...next]))
      return next
    })
  }

  const handleLogout = async () => {
    try { await fetch('/api/logout', { method: 'POST' }) } catch {}
    clearLoginAnnouncementSession()
    window.location.href = '/login'
  }

  return (
    <>
      <header className="sub-hero">
        <div className="sub-title-row">
          <BrandBadge src={logoUrl} size={36} markClassName="w-[20px] h-[20px]" />
          <h1 className="sub-title">{title}</h1>
        </div>
        <div className="sub-hero-meta">
	          <button type="button" className="sub-chip" onClick={() => setAnnOpen(true)}>
	            公告
	            {unread > 0 && (
	              <span className="sub-chip-dot" aria-label={`${unread} 条未读`}>{unread > 9 ? '9+' : unread}</span>
	            )}
	          </button>
          <NavLink to="/change-password" className="sub-chip">账户设置</NavLink>
          <button type="button" className="sub-chip" onClick={handleLogout}>退出账号</button>
        </div>
      </header>

      <Modal open={annOpen} onClose={() => setAnnOpen(false)} title="公告" wide>
        {loading ? (
          <div className="text-sm text-ink-mut py-8 text-center">加载中…</div>
        ) : items.length === 0 ? (
          <div className="text-sm text-ink-mut py-8 text-center">暂无公告</div>
        ) : (
          <div className="flex flex-col gap-4 max-h-[60vh] overflow-y-auto">
            {items.map((a) => {
              const isRead = readIds.has(a.id)
              const pinned = a.pinned === 1 || a.pinned === true
              const meta = annColorMeta[a.color]
              const privateMsg = a.target_user_id > 0 || (a.target_user_ids && a.target_user_ids !== '[]')
              return (
                <div
                  key={a.id}
                  className={`border-b border-line-soft pb-3 last:border-0 last:pb-0 cursor-pointer transition-opacity border-l-2 pl-3 ${meta?.bar || 'border-l-transparent'} ${isRead ? 'opacity-60' : ''}`}
                  onClick={() => markRead(a.id)}
                >
                  <div className="flex items-center gap-2 mb-1 flex-wrap">
                    {pinned && <Badge color="amber">置顶</Badge>}
                    {meta && <Badge color={meta.badge}>{meta.label}</Badge>}
                    <span className="font-semibold text-[14px]">{a.title}</span>
                    {!isRead && <Badge color="red">新</Badge>}
                    {privateMsg && <Badge color="blue">私信</Badge>}
                  </div>
                  <div className="text-[13px] text-ink-soft whitespace-pre-wrap">{a.content}</div>
                  <div className="text-[11px] text-ink-mut mt-1">{fmtDate(a.created_at)}</div>
                </div>
              )
            })}
          </div>
        )}
        <div className="flex justify-end mt-5">
          <button type="button" className="btn-secondary" onClick={() => setAnnOpen(false)}>
            关闭
          </button>
        </div>
      </Modal>
    </>
  )
}
