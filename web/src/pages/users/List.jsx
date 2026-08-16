import { useState, useEffect } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api } from '../../lib/api'
import { billedUsedBytes, fmtTrafficGB, nullStr, nullInt } from '../../lib/fmt'
import { Layout, useToast } from '../../components/Layout'
import { Loading, Empty, Badge, Modal, Select, DateInput } from '../../components/ui'
import { copyToClipboard } from '../../lib/clipboard'
import { PageHeader, Panel, PanelToolbar, SearchInput, ToolbarButton, ToolbarActions, TableScroll } from '../../components/page'
import FolderBar, { MoveToFolderModal } from '../../components/FolderBar'
import PasteGrantsModal from './PasteGrantsModal'
import { useIsMobile } from '../../lib/useIsMobile'
import { buildUserCardText, CARD_INITIAL_PASSWORD, loadPanelBranding } from '../../lib/userCard'

export default function UserList() {
  const [data, setData] = useState(null)
  const [folders, setFolders] = useState([])
  const [ungrouped, setUngrouped] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [showCreate, setShowCreate] = useState(false)
  const [showPaste, setShowPaste] = useState(false)
  const [showMove, setShowMove] = useState(false)
  const [allNodes, setAllNodes] = useState([])
  const isMobile = useIsMobile()
  const [search, setSearch] = useState('')
  const [folderFilter, setFolderFilter] = useState('') // '' all | '0' ungrouped | folder id
  const [sel, setSel] = useState(new Set())
  const [sortBy, setSortBy] = useState(null) // null | 'expires_asc' | 'expires_desc'
  const [dragIndex, setDragIndex] = useState(null)
  const toast = useToast()
  const navigate = useNavigate()

  const loadFolders = () => api.get('/user-folders').then(d => {
    setFolders(d?.folders || [])
    setUngrouped(d?.ungrouped || 0)
  }).catch(() => {})

  const load = () => {
    setLoading(true)
    setError('')
    Promise.all([
      api.get('/users').then(setData),
      loadFolders(),
    ]).catch(err => setError(err?.message || '加载失败')).finally(() => setLoading(false))
  }
  useEffect(load, [])
  useEffect(() => { api.get('/nodes').then(d => setAllNodes(d?.nodes || [])) }, [])

  if (loading && !data) return <Layout><Loading /></Layout>
  if (!data && error) return <Layout><Empty title="加载失败" desc={error}><button onClick={load} className="btn-secondary text-xs mt-3">重试</button></Empty></Layout>

  const { users: allUsers = [] } = data || {}
  const hiddenAdmin = allUsers.find(u => u.username === 'admin')
  const users = allUsers.filter(u => u.username !== 'admin')
  const visibleUngrouped = hiddenAdmin && !(hiddenAdmin.group_id > 0)
    ? Math.max(0, ungrouped - 1)
    : ungrouped
  const visibleFolders = hiddenAdmin && hiddenAdmin.group_id > 0
    ? folders.map(f => f.id === hiddenAdmin.group_id
      ? { ...f, count: Math.max(0, (f.count || 0) - 1) }
      : f)
    : folders
  const folderName = (id) => folders.find(f => f.id === id)?.name || ''

  const moveToFolder = async (folderId) => {
    if (sel.size === 0) { toast('请先勾选用户', 'error'); return }
    await api.post('/users/batch-group', { ids: [...sel], group_id: folderId })
    const label = folderId ? (folderName(folderId) || '分组') : '未分组'
    toast(folderId ? `已移入「${label}」` : '已移出到未分组')
    setSel(new Set())
    setShowMove(false)
    load()
  }

  const toggleSel = (id, e) => {
    e?.stopPropagation?.()
    setSel(s => {
      const next = new Set(s)
      if (next.has(id)) next.delete(id); else next.add(id)
      return next
    })
  }

  const q = search.trim().toLowerCase()
  let searched = users
  if (folderFilter === '0') searched = searched.filter(u => !(u.group_id > 0))
  else if (folderFilter) searched = searched.filter(u => String(u.group_id) === String(folderFilter))
  if (q) searched = searched.filter(u =>
    [u.username, u.admin_note, u.group_name].some(v => (v || '').toLowerCase().includes(q)))

  const toggleExpirySort = () => setSortBy(s => s === 'expires_asc' ? 'expires_desc' : s === 'expires_desc' ? null : 'expires_asc')
  const filtered = !sortBy ? searched : [...searched].sort((a, b) => {
    const ea = nullInt(a.expires_at) || 0
    const eb = nullInt(b.expires_at) || 0
    if (!ea && !eb) return 0
    if (!ea) return 1  // no expiry → push to end
    if (!eb) return -1
    return sortBy === 'expires_asc' ? ea - eb : eb - ea
  })

  const toggleSelAll = () => setSel(s =>
    s.size === filtered.length && filtered.length > 0 ? new Set() : new Set(filtered.map(u => u.id)))

  // 搜索 / 分组 / 到期排序任一生效时不能拖：saveOrder 以可见行为全量顺序，
  // 子集视图会把隐藏的 admin 和其它分组从顺序里丢掉。
  const draggable = !q && !folderFilter && !sortBy
  const saveOrder = async (visibleList) => {
    const visibleIds = visibleList.map(u => u.id)
    const hiddenIds = []
    if (hiddenAdmin) hiddenIds.push(hiddenAdmin.id)
    const allIds = [...hiddenIds, ...visibleIds]
    const byId = Object.fromEntries(allUsers.map(u => [u.id, u]))
    setData(d => ({ ...d, users: allIds.map(id => byId[id]).filter(Boolean) }))
    try { await api.post('/users/reorder', { ids: allIds }); toast('顺序已保存') } catch (err) { toast(err.message, 'error'); load() }
  }
  const onDrop = async (toIndex) => {
    if (dragIndex === null || dragIndex === toIndex) { setDragIndex(null); return }
    const next = [...filtered]
    const [moved] = next.splice(dragIndex, 1)
    next.splice(toIndex, 0, moved)
    setDragIndex(null)
    saveOrder(next)
  }

  return (
    <Layout>
      <div className="h-full flex flex-col">
      <PageHeader title="用户管理" count={users.length} unit="个用户" />

      <Panel fill>
        <PanelToolbar>
          <FolderBar
            folders={visibleFolders}
            ungrouped={visibleUngrouped}
            total={users.length}
            filter={folderFilter}
            onFilter={f => { setFolderFilter(f); setSel(new Set()) }}
            onCreate={async (name) => { await api.post('/user-folders', { name }); await loadFolders() }}
            onRename={async (id, name) => { await api.patch(`/user-folders/${id}`, { name }); await loadFolders(); load() }}
            onDelete={async (id) => { await api.del(`/user-folders/${id}`); await loadFolders(); load() }}
          />
          <SearchInput value={search} onChange={setSearch} placeholder="搜索用户名…" />
          {sel.size > 0 && (
            <button onClick={() => setShowMove(true)} className="btn-secondary !h-[32px] text-xs">移入分组 ({sel.size})</button>
          )}
          <ToolbarActions className="hidden md:flex">
            <ToolbarButton onClick={() => setShowPaste(true)} secondary>粘贴授权</ToolbarButton>
            <ToolbarButton onClick={() => setShowCreate(true)}>＋ 新建用户</ToolbarButton>
          </ToolbarActions>
        </PanelToolbar>

        {users.length === 0 ? (
          <Empty title="暂无用户" desc="点击右上角「新建用户」创建。" />
        ) : filtered.length === 0 ? (
          <Empty title="无匹配用户" desc="试试别的关键词或分组。" />
        ) : (
          <TableScroll>
          {/* Desktop table */}
          {!isMobile && <table className="tbl">
            <thead><tr>
              <th className="w-8"><input type="checkbox"
                checked={filtered.length > 0 && sel.size === filtered.length} onChange={toggleSelAll} /></th>
              <th className="w-16">ID</th><th>用户名</th><th>分组</th><th>角色</th><th>规则配额</th><th>流量</th><th>状态</th>
              <th className="cursor-pointer select-none whitespace-nowrap" onClick={toggleExpirySort}>到期{sortBy === 'expires_asc' ? ' ↑' : sortBy === 'expires_desc' ? ' ↓' : ''}</th>
              <th>备注</th>
            </tr></thead>
            <tbody>
              {filtered.map((u, i) => (
                  <tr key={u.id}
                    className={`cursor-pointer ${dragIndex === i ? 'opacity-50' : ''}`}
                    onClick={() => navigate(`/users/${u.id}`)}
                    onDragOver={draggable ? e => e.preventDefault() : undefined}
                    onDrop={draggable ? () => onDrop(i) : undefined}>
                    <td onClick={e => e.stopPropagation()}>
                      <input type="checkbox" checked={sel.has(u.id)} onChange={e => toggleSel(u.id, e)} />
                    </td>
                    <td className="font-mono text-xs text-ink-mut whitespace-nowrap">
                      {draggable && <span className="text-ink-mut mr-1 select-none cursor-move" title="拖拽排序"
                        draggable onDragStart={e => { e.stopPropagation(); setDragIndex(i) }}>⠿</span>}{u.id}
                    </td>
                    <td className="font-semibold" style={{ color: 'var(--brand-from)' }}>{u.username}</td>
                    <td className="text-xs">{u.group_name ? <Badge color="blue">{u.group_name}</Badge> : <span className="text-ink-mut">—</span>}</td>
                    <td><span className="inline-flex items-center font-mono text-xs bg-raised text-ink-soft px-1.5 py-0.5 rounded">{u.role}</span></td>
                    <td className="font-mono">{u.role === 'user' ? `${u.rule_count || 0} / ${u.max_forwards}` : '--'}</td>
                    <td className="font-mono">{u.role === 'user' ? fmtTrafficGB(billedUsedBytes(u.traffic_used_bytes, u.billing_rate), u.traffic_quota_bytes) : '--'}</td>
                    <td>
                      {u.disabled ? (
                        <Badge color="amber" title={nullStr(u.disable_reason)}>已禁用</Badge>
                      ) : <Badge color="green">正常</Badge>}
                    </td>
                    <ExpiryCell unix={nullInt(u.expires_at)} />
                    <td className="text-ink-soft text-xs max-w-[200px] truncate" title={u.admin_note}>{u.admin_note}</td>
                  </tr>
              ))}
            </tbody>
          </table>}
          {/* Mobile cards */}
          {isMobile && <div>
            {filtered.map(u => (
              <Link key={u.id} to={`/users/${u.id}`} className="mobile-card block no-underline text-ink">
                <div className="flex items-center justify-between mb-1">
                  <span className="font-semibold" style={{ color: 'var(--brand-from)' }}>{u.username}</span>
                  {u.disabled
                    ? <Badge color="amber">已禁用</Badge>
                    : <Badge color="green">正常</Badge>}
                </div>
                <div className="flex items-center gap-2 text-xs text-ink-soft flex-wrap">
                  {u.group_name && <Badge color="blue">{u.group_name}</Badge>}
                  <span className="font-mono bg-raised px-1.5 py-0.5 rounded">{u.role}</span>
                  {u.role === 'user' && <>
                    <span className="text-ink-mut">·</span>
                    <span className="font-mono">{u.rule_count || 0}/{u.max_forwards} 规则</span>
                    <span className="text-ink-mut">·</span>
                    <span className="font-mono">{fmtTrafficGB(billedUsedBytes(u.traffic_used_bytes, u.billing_rate), u.traffic_quota_bytes)}</span>
                    {nullInt(u.expires_at) ? <>
                      <span className="text-ink-mut">·</span>
                      <ExpiryInline unix={nullInt(u.expires_at)} />
                    </> : null}
                  </>}
                </div>
              </Link>
            ))}
          </div>}
          </TableScroll>
        )}
      </Panel>
      </div>

      <CreateUserModal folders={folders} open={showCreate} onClose={() => setShowCreate(false)} onDone={(userId) => { setShowCreate(false); userId ? navigate(`/users/${userId}`) : load() }} />
      <PasteGrantsModal open={showPaste} onClose={() => setShowPaste(false)} onDone={load}
        allNodes={allNodes.filter(n => n.node_type !== 'self')} allUsers={users} preSelectedUserIds={[]} />

      {showMove && (
        <MoveToFolderModal
          title={`将 ${sel.size} 个用户移入…`}
          folders={folders}
          onClose={() => setShowMove(false)}
          onMove={moveToFolder}
        />
      )}
    </Layout>
  )
}

function fmtExpiry(unix) {
  if (!unix) return '--'
  const d = new Date(unix * 1000)
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function expiryDaysLeft(unix) {
  if (!unix) return null
  return Math.ceil((unix - Date.now() / 1000) / 86400)
}

function ExpiryCell({ unix }) {
  if (!unix) return <td className="text-xs text-ink-mut">--</td>
  const days = expiryDaysLeft(unix)
  const expired = days <= 0
  const soon = !expired && days <= 7
  const cls = expired ? 'text-red-500' : soon ? 'text-amber-500' : 'text-ink-soft'
  return (
    <td className={`text-xs font-mono whitespace-nowrap ${cls}`}>
      {fmtExpiry(unix)}
      {expired ? <span className="ml-1 text-[10px]">已过期</span>
       : soon ? <span className="ml-1 text-[10px]">{days}天</span> : null}
    </td>
  )
}

function ExpiryInline({ unix }) {
  const days = expiryDaysLeft(unix)
  const expired = days <= 0
  const soon = !expired && days <= 7
  const cls = expired ? 'text-red-500' : soon ? 'text-amber-500' : 'text-ink-soft'
  return (
    <span className={`font-mono ${cls}`}>
      {expired ? '已过期' : soon ? `${days}天到期` : fmtExpiry(unix)}
    </span>
  )
}

function todayStr() {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
function toDateStr(d) {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
// Unambiguous alphabet (no O/0/I/l/1) for a copy-pasteable random password.
function genPassword(len = 16) {
  const chars = 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'
  const arr = new Uint32Array(len)
  crypto.getRandomValues(arr)
  return [...arr].map(n => chars[n % chars.length]).join('')
}
const emptyForm = () => ({ username: '', password: CARD_INITIAL_PASSWORD, role: 'user', group_id: '0', max_forwards: '100', traffic_quota_gb: '0', expires_at: todayStr(), landing_sub_url: '', admin_note: '', billing_rate: '1' })

function CreateUserModal({ folders = [], open, onClose, onDone }) {
  const [form, setForm] = useState(emptyForm)
  const [loading, setLoading] = useState(false)
  const [panelURL, setPanelURL] = useState('')
  const toast = useToast()

  // Fetch the configured panel address so "创建并复制信息" can include the login
  // URL; fall back to the current origin when unset.
  useEffect(() => {
    if (!open) return
    setForm(emptyForm())
    api.get('/settings').then(d => setPanelURL((d?.panel_url || '').trim() || window.location.origin)).catch(() => setPanelURL(window.location.origin))
  }, [open])

  const set = (k, v) => setForm(f => ({ ...f, [k]: v }))

  const addToExpiry = (kind) => {
    const base = form.expires_at ? new Date(form.expires_at + 'T00:00:00') : new Date()
    if (kind === '1d') base.setDate(base.getDate() + 1)
    if (kind === '1m') base.setMonth(base.getMonth() + 1)
    if (kind === '3m') base.setMonth(base.getMonth() + 3)
    if (kind === '1y') base.setFullYear(base.getFullYear() + 1)
    set('expires_at', toDateStr(base))
  }

  const submit = async (e) => {
    e.preventDefault()
    setLoading(true)
    const isUser = form.role === 'user'
    // Copy the login info inside the click gesture, before the create request:
    // the execCommand fallback (plain-HTTP panels) only works while the user
    // activation is live, and awaiting the network would spend it. All the info
    // fields are known pre-submit, so copying first is safe; the result is
    // announced only after a successful create, so a failed create never claims
    // the credentials were copied.
    // Card always publishes the fixed initial password for delivery consistency.
    const brand = await loadPanelBranding(api).catch(() => ({ panelURL, panelName: '' }))
    const expUnix = form.expires_at
      ? Math.floor(new Date(form.expires_at + 'T00:00:00').getTime() / 1000)
      : 0
    const draftUser = {
      username: form.username,
      expires_at: expUnix,
    }
    const info = buildUserCardText({
      panelName: brand.panelName,
      panelURL: brand.panelURL || panelURL,
      user: draftUser,
      password: CARD_INITIAL_PASSWORD,
    })
    let copied = true
    try { await copyToClipboard(info) } catch { copied = false }
    try {
      const res = await api.post('/users', {
        username: form.username,
        // Store the same password the card advertises unless admin typed another.
        password: form.password || CARD_INITIAL_PASSWORD,
        role: form.role,
        group_id: Number(form.group_id) || 0,
        ...(isUser ? {
          max_forwards: Number(form.max_forwards),
          traffic_quota_bytes: Math.max(0, Math.round((Number(form.traffic_quota_gb) || 0) * 1073741824)),
          expires_at: form.expires_at || undefined,
          landing_sub_url: form.landing_sub_url.trim() || undefined,
          admin_note: form.admin_note.trim() || undefined,
          billing_rate: Math.max(0, Number(form.billing_rate) || 1),
        } : {}),
      })
      toast(copied ? '用户已创建，名片已复制' : '用户已创建（复制失败，请手动记录）')
      setForm(emptyForm())
      onDone(res?.user?.id)
    } catch (err) { toast(err.message, 'error') } finally { setLoading(false) }
  }

  return (
    <Modal open={open} onClose={onClose} title="新建用户">
      <form onSubmit={submit} className="space-y-4">
        <div className="grid grid-cols-[150px_1fr] gap-4 items-center">
          <label className="fl">用户名</label>
          <input className="input-field" value={form.username} onChange={e => set('username', e.target.value)} required placeholder="登录用户名" />
          <label className="fl">密码</label>
          <div className="flex items-center gap-2">
            <input className="input-field font-mono flex-1" type="text" value={form.password} onChange={e => set('password', e.target.value)} required placeholder="密码" />
            <button type="button" onClick={() => set('password', genPassword())} className="btn-secondary text-xs flex-none">随机生成</button>
          </div>
          <label className="fl">角色</label>
          <Select value={form.role} onChange={v => set('role', v)} options={[{ value: 'user', label: 'user (普通用户)' }, { value: 'admin', label: 'admin (管理员)' }]} style={{ maxWidth: 200 }} />
          <label className="fl">分组</label>
          <select className="input-field" value={form.group_id} onChange={e => set('group_id', e.target.value)}>
            <option value="0">未分组</option>
            {folders.map(f => <option key={f.id} value={String(f.id)}>{f.name}</option>)}
          </select>
          {form.role === 'user' && (
            <>
              <label className="fl">最大转发数</label>
              <input className="input-field font-mono" type="number" min="1" value={form.max_forwards} onChange={e => set('max_forwards', e.target.value)} style={{ maxWidth: 160 }} />
              <label className="fl">流量配额 <span className="text-ink-mut font-normal text-xs">(GB，0 = 不限)</span></label>
              <input className="input-field font-mono" type="number" min="0" step="0.1" value={form.traffic_quota_gb} onChange={e => set('traffic_quota_gb', e.target.value)} style={{ maxWidth: 160 }} />
              <label className="fl">显示倍率 <span className="text-ink-mut font-normal text-xs">(用户已用量按此倍数显示)</span></label>
              <div className="flex items-center gap-1.5">
                <input className="input-field font-mono" type="number" min="0" step="0.1" value={form.billing_rate} onChange={e => set('billing_rate', e.target.value)} style={{ maxWidth: 120 }} title="1.0 = 原值显示，>1 放大显示" />
                <span className="text-xs text-ink-mut">×</span>
              </div>
              <label className="fl">到期时间</label>
              <div className="flex items-center gap-2 flex-wrap">
                <DateInput value={form.expires_at} onChange={v => set('expires_at', v)} style={{ maxWidth: 200, width: 200 }} />
                <button type="button" onClick={() => set('expires_at', todayStr())} title="重置为当天"
                  className="btn-secondary flex-none px-2" style={{ height: 38 }}>
                  <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/></svg>
                </button>
                <div className="inline-flex gap-1.5">
                  {[['1d', '+1天'], ['1m', '+1月'], ['3m', '+3月'], ['1y', '+1年']].map(([k, lbl]) => (
                    <button key={k} type="button" onClick={() => addToExpiry(k)} className="btn-secondary text-xs">{lbl}</button>
                  ))}
                </div>
              </div>
              <label className="fl">订阅地址 <span className="text-ink-mut font-normal text-xs">(可选)</span></label>
              <input className="input-field font-mono" value={form.landing_sub_url} onChange={e => set('landing_sub_url', e.target.value)} placeholder="https://example.com/api/sub/xxxx" />
              <label className="fl">管理备注 <span className="text-ink-mut font-normal text-xs">(可选，仅管理员可见)</span></label>
              <input className="input-field" value={form.admin_note} onChange={e => set('admin_note', e.target.value)} placeholder="仅管理员可见的备注" />
            </>
          )}
        </div>
        <div className="flex items-center justify-end gap-3 pt-4 border-t border-line-soft">
          <button type="button" onClick={onClose} className="btn-secondary">取消</button>
          <button type="submit" disabled={loading} className="btn-primary">创建并复制名片</button>
        </div>
      </form>
    </Modal>
  )
}
