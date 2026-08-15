import { useState, useEffect, useRef } from 'react'
import { api } from '../lib/api'
import { Layout, useToast, useUser } from '../components/Layout'
import { Loading } from '../components/ui'
import { PageHeader } from '../components/page'
import { BrandBadge } from '../components/BrandMark'

const TABS = [
  { id: 'panel', label: '面板' },
  { id: 'forward', label: '转发' },
  { id: 'cloudflare', label: 'Cloudflare' },
]

export default function Settings() {
  const [tab, setTab] = useState('panel')
  const [form, setForm] = useState({
    panel_url: '',
    panel_name: '',
    logo_url: '',
    show_rate_to_user: false,
    pool_size: 4,
    cf_token_configured: false,
    cf_token_prefix: '',
    cf_api_token: '',
    cf_clear_token: false,
    cf_zone_name: '',
    cf_ttl: 1,
  })
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [logoBusy, setLogoBusy] = useState(false)
  const [error, setError] = useState('')
  const fileRef = useRef(null)
  const toast = useToast()
  const { applySession, setLogoUrl } = useUser()

  useEffect(() => {
    api.get('/settings').then(data => {
      setForm(f => ({
        ...f,
        panel_url: data.panel_url || '',
        panel_name: data.panel_name || '',
        logo_url: data.logo_url || '',
        show_rate_to_user: !!data.show_rate_to_user,
        pool_size: data.pool_size ?? 4,
        cf_token_configured: !!data.cf_token_configured,
        cf_token_prefix: data.cf_token_prefix || '',
        cf_api_token: '',
        cf_clear_token: false,
        cf_zone_name: data.cf_zone_name || '',
        cf_ttl: data.cf_ttl ?? 1,
      }))
    }).catch(e => setError(e.message)).finally(() => setLoading(false))
  }, [])

  const set = (k, v) => setForm(f => ({ ...f, [k]: v }))

  const applyLogo = (url) => {
    const next = url ? `${url.split('?')[0]}?t=${Date.now()}` : ''
    set('logo_url', next)
    if (setLogoUrl) setLogoUrl(next)
    applySession({ logo_url: next })
  }

  const submit = async (e) => {
    e.preventDefault()
    setError('')
    const ps = parseInt(form.pool_size, 10)
    if (isNaN(ps) || ps < 0 || ps > 64) {
      setError('TCP 连接池数必须在 0-64 之间')
      return
    }
    const ttl = parseInt(form.cf_ttl, 10)
    if (isNaN(ttl) || ttl < 1) {
      setError('CF TTL 至少为 1（1 = Auto）')
      return
    }
    setSaving(true)
    try {
      const body = {
        panel_url: form.panel_url,
        panel_name: form.panel_name,
        show_rate_to_user: form.show_rate_to_user,
        pool_size: ps,
        cf_zone_name: form.cf_zone_name,
        cf_ttl: ttl,
        cf_clear_token: !!form.cf_clear_token,
      }
      if (!form.cf_clear_token && form.cf_api_token.trim()) {
        body.cf_api_token = form.cf_api_token.trim()
      }
      await api.post('/settings', body)
      applySession({ panel_name: form.panel_name })
      toast('设置已保存')
      const data = await api.get('/settings')
      setForm(f => ({
        ...f,
        logo_url: data.logo_url || '',
        cf_token_configured: !!data.cf_token_configured,
        cf_token_prefix: data.cf_token_prefix || '',
        cf_api_token: '',
        cf_clear_token: false,
        cf_zone_name: data.cf_zone_name || '',
        cf_ttl: data.cf_ttl ?? 1,
      }))
    } catch (err) { setError(err.message) } finally { setSaving(false) }
  }

  const uploadLogo = async (file) => {
    if (!file) return
    setLogoBusy(true)
    setError('')
    try {
      const fd = new FormData()
      fd.append('file', file)
      const res = await fetch('/api/settings/logo', { method: 'POST', body: fd, credentials: 'same-origin' })
      const ct = res.headers.get('content-type') || ''
      let data = null
      if (ct.includes('application/json')) {
        try { data = await res.json() } catch { data = null }
      }
      if (res.status === 401) {
        window.dispatchEvent(new CustomEvent('nf-unauthorized'))
        throw new Error('登录已过期，请重新登录')
      }
      if (!res.ok) throw new Error((data && data.error) || `上传失败（${res.status}）`)
      applyLogo(data.logo_url || '')
      toast('Logo 已更新')
    } catch (err) {
      setError(err.message)
      toast(err.message, 'error')
    } finally {
      setLogoBusy(false)
      if (fileRef.current) fileRef.current.value = ''
    }
  }

  const clearLogo = async () => {
    setLogoBusy(true)
    setError('')
    try {
      await api.del('/settings/logo')
      applyLogo('')
      toast('已恢复默认 Logo')
    } catch (err) {
      setError(err.message)
      toast(err.message, 'error')
    } finally { setLogoBusy(false) }
  }

  if (loading) return <Layout><Loading /></Layout>

  return (
    <Layout>
      <PageHeader title="系统设置" />
      <div className="card" style={{ maxWidth: 980 }}>
        <div className="flex items-center gap-1 px-4 pt-3 border-b border-line-soft">
          {TABS.map(t => (
            <button key={t.id} type="button" onClick={() => setTab(t.id)}
              className={`px-4 py-2.5 text-[13.5px] font-semibold border-b-2 -mb-px transition-colors ${
                tab === t.id ? 'border-[color:var(--brand-from)] text-ink' : 'border-transparent text-ink-mut hover:text-ink-soft'
              }`}>{t.label}</button>
          ))}
        </div>
        <div className="px-6 py-[26px]">
          {error && <div className="mb-4 px-3 py-2 bg-transparent border-[1.5px] border-rose-500/40 rounded-xl text-rose-700 dark:text-rose-300 text-sm">{error}</div>}
          <form onSubmit={submit}>
            {tab === 'panel' && (
              <>
                <div className="flex items-start gap-6 mb-[22px]">
                  <label className="w-[110px] flex-shrink-0 text-[14px] text-ink-soft pt-2">面板 Logo</label>
                  <div className="flex-1">
                    <div className="flex items-center gap-4">
                      <BrandBadge src={form.logo_url} size={52} markClassName="w-[30px] h-[30px]" />
                      <div className="flex flex-wrap gap-2">
                        <button type="button" className="btn-secondary px-4" disabled={logoBusy}
                          onClick={() => fileRef.current?.click()}>
                          {logoBusy ? '处理中…' : '更换 Logo'}
                        </button>
                        {form.logo_url && (
                          <button type="button" className="btn-secondary px-4" disabled={logoBusy} onClick={clearLogo}>
                            恢复默认
                          </button>
                        )}
                      </div>
                    </div>
                    <input ref={fileRef} type="file" accept="image/png,image/jpeg,image/gif,image/webp,image/svg+xml"
                      className="hidden" onChange={e => uploadLogo(e.target.files?.[0])} />
                    <p className="text-[12px] text-ink-mut mt-2 m-0">png / jpg / svg / webp，最大 1MB。侧栏、登录页和浏览器标签会同步。</p>
                  </div>
                </div>
                <div className="flex items-center gap-6 mb-[22px]">
                  <label className="w-[110px] flex-shrink-0 text-[14px] text-ink-soft">面板地址</label>
                  <div className="flex-1 max-w-[560px]">
                    <input className="input-field w-full" type="text" placeholder="http://1.2.3.4:7788 或 https://panel.example.com" value={form.panel_url} onChange={e => set('panel_url', e.target.value)} />
                    <p className="text-[12px] text-ink-mut mt-1.5 m-0">节点升级会从该地址下载 agent。请带协议（http/https）；只写 IP:端口 时保存会自动补 http://。</p>
                  </div>
                </div>
                <div className="flex items-center gap-6">
                  <label className="w-[110px] flex-shrink-0 text-[14px] text-ink-soft">面板名称</label>
                  <input className="input-field max-w-[560px]" type="text" placeholder="nft" value={form.panel_name} onChange={e => set('panel_name', e.target.value)} />
                </div>
              </>
            )}

            {tab === 'forward' && (
              <>
                <div className="flex items-center gap-6 mb-[22px]">
                  <label className="w-[110px] flex-shrink-0 text-[14px] text-ink-soft">显示倍率</label>
                  <button type="button" role="switch" aria-checked={form.show_rate_to_user}
                    className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${form.show_rate_to_user ? 'bg-[color:var(--brand-from)]' : 'bg-gray-600'}`}
                    onClick={() => set('show_rate_to_user', !form.show_rate_to_user)}>
                    <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${form.show_rate_to_user ? 'translate-x-6' : 'translate-x-1'}`} />
                  </button>
                  <span className="text-[13px] text-ink-mut">向普通用户展示节点/链路倍率</span>
                </div>
                <div className="flex items-center gap-6">
                  <label className="w-[110px] flex-shrink-0 text-[14px] text-ink-soft">TCP 连接池</label>
                  <input className="input-field w-[100px]" type="number" min="0" max="64" value={form.pool_size} onChange={e => set('pool_size', e.target.value)} />
                  <span className="text-[13px] text-ink-mut">每端口预建立连接数（0 = 禁用，默认 4）</span>
                </div>
              </>
            )}

            {tab === 'cloudflare' && (
              <>
                <div className="flex items-start gap-6 mb-[22px]">
                  <label className="w-[110px] flex-shrink-0 text-[14px] text-ink-soft pt-2">API Token</label>
                  <div className="flex-1 max-w-[560px]">
                    <input
                      className="input-field w-full font-mono text-sm"
                      type="password"
                      autoComplete="new-password"
                      placeholder={form.cf_token_configured ? `已配置（${form.cf_token_prefix}）· 留空不修改` : '粘贴 Zone.DNS Edit 权限的 Token'}
                      value={form.cf_api_token}
                      onChange={e => set('cf_api_token', e.target.value)}
                      disabled={form.cf_clear_token}
                    />
                    <div className="flex items-center gap-3 mt-2">
                      <label className="inline-flex items-center gap-1.5 text-[12px] text-ink-soft cursor-pointer">
                        <input type="checkbox" checked={form.cf_clear_token}
                          onChange={e => set('cf_clear_token', e.target.checked)} />
                        清除已保存的 Token
                      </label>
                      {form.cf_token_configured && !form.cf_clear_token && (
                        <span className="text-[12px] text-[color:var(--brand-from)] font-semibold">已配置</span>
                      )}
                    </div>
                  </div>
                </div>
                <div className="flex items-center gap-6 mb-[22px]">
                  <label className="w-[110px] flex-shrink-0 text-[14px] text-ink-soft">默认 Zone</label>
                  <div className="flex-1 max-w-[560px]">
                    <input className="input-field w-full font-mono" type="text" placeholder="example.com"
                      value={form.cf_zone_name} onChange={e => set('cf_zone_name', e.target.value)} />
                  </div>
                </div>
                <div className="flex items-center gap-6">
                  <label className="w-[110px] flex-shrink-0 text-[14px] text-ink-soft">TTL</label>
                  <input className="input-field w-[100px]" type="number" min="1" value={form.cf_ttl}
                    onChange={e => set('cf_ttl', e.target.value)} />
                  <span className="text-[13px] text-ink-mut">秒；1 = Cloudflare Auto</span>
                </div>
              </>
            )}

            <div className="flex items-center gap-4 mt-[26px] pt-[18px] border-t border-line-soft">
              <button type="submit" disabled={saving} className="btn-primary">{saving ? '保存中…' : '保存设置'}</button>
            </div>
          </form>
        </div>
      </div>
    </Layout>
  )
}
