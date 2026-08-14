import { useState } from 'react'
import { Modal, Select, useConfirm } from './ui'
import { useToast } from './Layout'

/**
 * Dropdown folder filter for single-level admin folders.
 * filter: '' all | '0' ungrouped | folder id string
 */
export default function FolderBar({
  folders = [],
  ungrouped = 0,
  total = 0,
  filter,
  onFilter,
  onCreate,
  onRename,
  onDelete,
}) {
  const toast = useToast()
  const confirm = useConfirm()
  const [showCreate, setShowCreate] = useState(false)
  const [renaming, setRenaming] = useState(null)
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)

  const options = [
    { value: '', label: `全部 ${total}` },
    { value: '0', label: `未分组 ${ungrouped}` },
    ...folders.map(f => ({ value: String(f.id), label: `${f.name} ${f.count || 0}` })),
  ]
  const current = folders.find(f => String(f.id) === String(filter))

  const create = async () => {
    const n = name.trim()
    if (!n) { toast('请输入分组名称', 'error'); return }
    setBusy(true)
    try {
      await onCreate(n)
      setShowCreate(false)
      setName('')
      toast(`已创建分组「${n}」`)
    } catch (err) {
      toast(err?.message || '创建失败', 'error')
    } finally {
      setBusy(false)
    }
  }

  const rename = async () => {
    const n = name.trim()
    if (!n || !renaming) return
    setBusy(true)
    try {
      await onRename(renaming.id, n)
      setRenaming(null)
      setName('')
      toast('已重命名')
    } catch (err) {
      toast(err?.message || '重命名失败', 'error')
    } finally {
      setBusy(false)
    }
  }

  const remove = async (f) => {
    if (!(await confirm({
      title: '删除分组',
      message: `删除「${f.name}」？其中的内容会回到「未分组」，不会被删除。`,
      confirmText: '删除',
      danger: true,
    }))) return
    try {
      await onDelete(f.id)
      if (String(filter) === String(f.id)) onFilter('')
      toast('分组已删除')
    } catch (err) {
      toast(err?.message || '删除失败', 'error')
    }
  }

  return (
    <>
      <div className="flex items-center gap-2 flex-wrap">
        <Select
          value={filter}
          onChange={onFilter}
          options={options}
          className="w-[220px] max-w-full"
        />
        {current && (
          <span className="inline-flex items-center gap-0.5">
            <button
              type="button"
              title="重命名"
              className="icon-btn !w-8 !h-8"
              onClick={() => { setRenaming(current); setName(current.name) }}
            >
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z"/></svg>
            </button>
            <button
              type="button"
              title="删除"
              className="icon-btn-danger !w-8 !h-8"
              onClick={() => remove(current)}
            >
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4h8v2"/></svg>
            </button>
          </span>
        )}
        <button
          type="button"
          onClick={() => { setShowCreate(true); setName('') }}
          className="btn-secondary !h-[42px] text-[13px]"
        >
          ＋ 新建分组
        </button>
      </div>

      {showCreate && (
        <Modal open onClose={() => setShowCreate(false)} title="新建分组">
          <div className="space-y-4">
            <div>
              <label className="block text-[13px] font-semibold text-ink-soft mb-1.5">名称</label>
              <input className="input-field" value={name} onChange={e => setName(e.target.value)} placeholder="如：VIP / 测试" autoFocus
                onKeyDown={e => e.key === 'Enter' && create()} />
            </div>
            <div className="flex gap-2">
              <button type="button" disabled={busy} onClick={create} className="btn-primary flex-1">{busy ? '创建中…' : '创建'}</button>
              <button type="button" onClick={() => setShowCreate(false)} className="btn-secondary">取消</button>
            </div>
          </div>
        </Modal>
      )}

      {renaming && (
        <Modal open onClose={() => setRenaming(null)} title="重命名分组">
          <div className="space-y-4">
            <div>
              <label className="block text-[13px] font-semibold text-ink-soft mb-1.5">新名称</label>
              <input className="input-field" value={name} onChange={e => setName(e.target.value)} autoFocus
                onKeyDown={e => e.key === 'Enter' && rename()} />
            </div>
            <div className="flex gap-2">
              <button type="button" disabled={busy} onClick={rename} className="btn-primary flex-1">{busy ? '保存中…' : '保存'}</button>
              <button type="button" onClick={() => setRenaming(null)} className="btn-secondary">取消</button>
            </div>
          </div>
        </Modal>
      )}
    </>
  )
}

function FolderIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="opacity-90">
      <path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z" />
    </svg>
  )
}

/** Modal: pick a folder to move selected items into. */
export function MoveToFolderModal({ title, folders = [], onClose, onMove }) {
  const [busy, setBusy] = useState(false)
  const toast = useToast()

  const pick = async (folderId) => {
    setBusy(true)
    try {
      await onMove(folderId)
    } catch (err) {
      toast(err?.message || '操作失败', 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal open onClose={onClose} title={title}>
      <div className="space-y-2 max-h-[50vh] overflow-y-auto">
        <button type="button" disabled={busy} onClick={() => pick(0)}
          className="w-full text-left px-3 py-2.5 rounded-lg border border-line hover:bg-raised text-sm font-semibold flex items-center gap-2">
          <FolderIcon /> 未分组
        </button>
        {folders.map(f => (
          <button key={f.id} type="button" disabled={busy} onClick={() => pick(f.id)}
            className="w-full text-left px-3 py-2.5 rounded-lg border border-line hover:bg-raised text-sm font-semibold flex items-center gap-2">
            <FolderIcon /> {f.name}
            <span className="ml-auto text-xs text-ink-mut font-mono">{f.count || 0}</span>
          </button>
        ))}
        {folders.length === 0 && (
          <p className="text-xs text-ink-mut px-1 py-2">还没有分组，先点上方「新建分组」。</p>
        )}
      </div>
      <div className="flex justify-end mt-4">
        <button type="button" onClick={onClose} className="btn-secondary">取消</button>
      </div>
    </Modal>
  )
}
