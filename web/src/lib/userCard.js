/* Admin-facing user delivery card (微信/飞书可直接粘贴).
   Password is always the panel's published initial password — not a live hash. */

import { fmtDate, nullInt } from './fmt'

/** Published initial password shown on every user card. */
export const CARD_INITIAL_PASSWORD = '123456'

/**
 * Build plain-text user card for clipboard delivery.
 * @param {object} opts
 * @param {string} [opts.panelName]
 * @param {string} opts.panelURL
 * @param {object} opts.user - list or detail user object
 * @param {string} [opts.password] - defaults to CARD_INITIAL_PASSWORD
 */
export function buildUserCardText({
  panelName = '',
  panelURL = '',
  user,
  password = CARD_INITIAL_PASSWORD,
} = {}) {
  if (!user) return ''
  const brand = (panelName || '').trim() || 'nft'
  const url = (panelURL || '').trim() || (typeof window !== 'undefined' ? window.location.origin : '')
  const username = user.username || ''
  const exp = nullInt(user.expires_at) || (user.expires_at > 0 ? user.expires_at : 0)
  const expiresLine = exp > 0 ? fmtDate(exp) : '永不过期'

  const lines = [
    `【${brand}】账号信息`,
    `网址：${url}`,
    `用户名：${username}`,
    `初始密码：${password}`,
    `到期时间：${expiresLine}`,
    '使用说明：看网站公告',
  ]

  if (user.disabled) {
    lines.splice(5, 0, '状态：已禁用（请联系管理员）')
  }

  return lines.join('\n')
}

/** Resolve panel URL/name for cards (settings first, then branding/origin). */
export async function loadPanelBranding(api) {
  let panelURL = ''
  let panelName = ''
  try {
    const s = await api.get('/settings')
    panelURL = (s?.panel_url || '').trim()
    panelName = (s?.panel_name || '').trim()
  } catch { /* ignore */ }
  if (!panelURL && typeof window !== 'undefined') panelURL = window.location.origin
  if (!panelName) {
    try {
      const b = await api.get('/branding')
      panelName = (b?.panel_name || '').trim()
    } catch { /* ignore */ }
  }
  return { panelURL, panelName }
}
