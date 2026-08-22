import { useState, useEffect } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '../lib/api'
import { useUser } from '../components/Layout'
import { BrandBadge } from '../components/BrandMark'
import { clearLoginAnnouncementSession } from '../components/LoginAnnouncementModal'

const REMEMBER_KEY = 'nf-remember-login'

function loadRemembered() {
  try {
    const raw = localStorage.getItem(REMEMBER_KEY)
    if (!raw) return { username: '', password: '', remember: false }
    const d = JSON.parse(raw)
    return {
      username: typeof d.username === 'string' ? d.username : '',
      password: typeof d.password === 'string' ? d.password : '',
      remember: true,
    }
  } catch {
    return { username: '', password: '', remember: false }
  }
}

export default function Login() {
  const remembered = loadRemembered()
  const [username, setUsername] = useState(remembered.username)
  const [password, setPassword] = useState(remembered.password)
  const [remember, setRemember] = useState(remembered.remember)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [panelName, setPanelName] = useState('')
  const [logoUrl, setLogoUrl] = useState('')
  const navigate = useNavigate()
  const { applySession, refreshUser } = useUser()

  useEffect(() => {
    api.get('/branding').then(d => {
      const name = (d?.panel_name || '').trim()
      setPanelName(name)
      if (d?.logo_url) setLogoUrl(d.logo_url)
      if (d?.panel_skin) applySession({ panel_skin: d.panel_skin })
      if (name) document.title = name
    }).catch(() => {})
  }, [applySession])

  const submit = async (e) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const data = await api.post('/login', { username, password })
      clearLoginAnnouncementSession()
      if (data?.user) applySession(data)
      try { await refreshUser() } catch { /* session already applied */ }
      try {
        if (remember) {
          localStorage.setItem(REMEMBER_KEY, JSON.stringify({ username, password }))
        } else {
          localStorage.removeItem(REMEMBER_KEY)
        }
      } catch {}
      navigate('/', { replace: true })
    } catch (err) {
      const msg = err?.message || '登录失败'
      if (/登录已过期|请重新登录/.test(msg)) {
        setError('用户名或密码错误，请重新输入')
      } else {
        setError(msg)
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-shell">
      <div className="login-card">
        <div className="flex items-center gap-3.5 mb-8">
          <BrandBadge src={logoUrl} size={46} markClassName="w-[28px] h-[28px]" />
          <div className="text-[17px] font-bold tracking-tight text-ink">{panelName || 'nft'}</div>
        </div>

        {error && (
          <div className="mb-4 px-3.5 py-2.5 bg-transparent border-[1.5px] border-rose-500/40 rounded-xl text-rose-700 dark:text-rose-300 text-[13px]">{error}</div>
        )}

        <form onSubmit={submit} className="flex flex-col gap-4">
          <div>
            <label className="block text-[13px] font-semibold text-ink-soft mb-1.5">用户名</label>
            <input className="input-field" value={username} onChange={e => setUsername(e.target.value)} required autoFocus autoComplete="username" />
          </div>
          <div>
            <label className="block text-[13px] font-semibold text-ink-soft mb-1.5">密码</label>
            <input className="input-field" type="password" value={password} onChange={e => setPassword(e.target.value)} required autoComplete="current-password" />
          </div>
          <button type="submit" disabled={loading}
            className="login-submit mt-1 self-center min-w-[88px] h-[42px] px-5 justify-center text-[13.5px] disabled:opacity-60">
            {loading ? <div className="w-4 h-4 border-2 border-current border-t-transparent rounded-full animate-spin" /> : '登录'}
          </button>
          <label className="inline-flex items-center self-start gap-2 text-[13px] font-semibold text-ink-soft cursor-pointer select-none">
            <input type="checkbox" checked={remember} onChange={e => setRemember(e.target.checked)} />
            记住账号密码
          </label>
        </form>
      </div>
    </div>
  )
}
