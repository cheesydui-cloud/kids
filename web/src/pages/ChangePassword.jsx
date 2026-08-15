import { useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../lib/api'
import { Layout, useToast, useUser } from '../components/Layout'
import { UserPortalHead } from '../components/UserPortalHead'
import { PageHeader } from '../components/page'

export default function ChangePassword() {
  const [form, setForm] = useState({ old_password: '', new_password: '', confirm: '' })
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const toast = useToast()
  const { user } = useUser()
  const isAdmin = user?.role === 'admin'

  const set = (k, v) => setForm(f => ({ ...f, [k]: v }))

  const submitPassword = async (e) => {
    e.preventDefault()
    setError('')
    if (form.new_password !== form.confirm) {
      setError('两次输入的密码不一致')
      return
    }
    if (form.new_password.length < 6) {
      setError('新密码至少 6 位')
      return
    }
    setLoading(true)
    try {
      await api.post('/change-password', { old_password: form.old_password, new_password: form.new_password })
      toast('密码已更新')
      setForm({ old_password: '', new_password: '', confirm: '' })
    } catch (err) { setError(err.message) } finally { setLoading(false) }
  }

  const fields = (
    <>
      <div className="sub-account">
        <div>
          <span className="sub-account-k">用户名</span>
          <strong>{user?.username || '—'}</strong>
        </div>
      </div>

      <section className="sub-settings">
        <h2>修改密码</h2>
        {error && <div className="mb-4 px-3 py-2 bg-transparent border-[1.5px] border-rose-500/40 rounded-xl text-rose-700 dark:text-rose-300 text-sm">{error}</div>}
        <form onSubmit={submitPassword} className="sub-settings-form">
          <label>
            <span>原密码</span>
            <input className="input-field" type="password" value={form.old_password} onChange={e => set('old_password', e.target.value)} required autoFocus />
          </label>
          <label>
            <span>新密码</span>
            <input className="input-field" type="password" minLength="6" value={form.new_password} onChange={e => set('new_password', e.target.value)} required />
          </label>
          <label>
            <span>再次输入</span>
            <input className="input-field" type="password" minLength="6" value={form.confirm} onChange={e => set('confirm', e.target.value)} required />
          </label>
          <div className="flex flex-wrap items-center gap-3 mt-1">
            <button type="submit" disabled={loading} className="btn-primary">更新密码</button>
            <Link to={isAdmin ? '/' : '/my'} className="btn-secondary">返回主页</Link>
          </div>
        </form>
      </section>
    </>
  )

  if (!isAdmin) {
    return (
      <Layout>
        <div className="sub-page">
          <UserPortalHead title="账户设置" />
          {fields}
        </div>
      </Layout>
    )
  }

  return (
    <Layout>
      <PageHeader title="账户设置" />
      <div className="card" style={{ maxWidth: 980 }}>
        <div className="card-header"><h3 className="text-[16px] font-bold">账户设置</h3></div>
        <div className="px-6 py-[26px]">
          {fields}
        </div>
      </div>
    </Layout>
  )
}
