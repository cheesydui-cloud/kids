const MB = 1048576
const GB = 1073741824

/** Format a byte count with an adaptive unit (B/KB/MB/GB/TB), one decimal. */
export function fmtBytes(n) {
  if (n == null || n === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return (i === 0 ? v : v.toFixed(1)) + ' ' + units[i]
}

function fmtAdaptive(bytes) {
  if (bytes < GB) return (bytes / MB).toFixed(1) + ' MB'
  return (bytes / GB).toFixed(1) + ' GB'
}

/** Format used/quota traffic; sub-1 GB values shown as MB. */
/** Account-billed bytes: raw used × billing_rate (rate≤0 treated as 1). */
export function billedUsedBytes(used, rate) {
  const n = Number(rate)
  return Math.round((used || 0) * (n > 0 ? n : 1))
}

export function fmtTrafficGB(used, quota) {
  const u = fmtAdaptive(used || 0)
  if (!quota || quota === 0) return `${u} / ∞`
  return `${u} / ${fmtAdaptive(quota)}`
}

export function fmtTime(unix) {
  if (!unix) return '--'
  const diff = Math.floor(Date.now() / 1000) - unix
  if (diff < 0) return '刚刚'
  if (diff < 60) return `${diff} 秒前`
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return `${Math.floor(diff / 86400)} 天前`
}

export function fmtDate(unix) {
  if (!unix) return '--'
  const d = new Date(unix * 1000)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  if (hh === '23' && mm === '59') return `${y}-${m}-${day}`
  return `${y}-${m}-${day} ${hh}:${mm}`
}

export function fmtDateInput(unix) {
  if (!unix) return ''
  const d = new Date(unix * 1000)
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  const hh = String(d.getHours()).padStart(2, '0')
  const mm = String(d.getMinutes()).padStart(2, '0')
  return `${y}-${m}-${day} ${hh}:${mm}`
}

export function parseLocalDateTime(s) {
  if (!s || typeof s !== 'string') return null
  const t = s.trim().replace('T', ' ').replace(/\//g, '-').replace(/\./g, '-')
  let m = /^(\d{4})-(\d{1,2})-(\d{1,2})(?:[ T](\d{1,2}):(\d{1,2})(?::(\d{1,2}))?)?$/.exec(t)
  if (!m) return null
  const y = Number(m[1]), mo = Number(m[2]), d = Number(m[3])
  const hh = m[4] != null ? Number(m[4]) : 23
  const mm = m[5] != null ? Number(m[5]) : 59
  const ss = m[6] != null ? Number(m[6]) : (m[4] != null ? 0 : 59)
  if (mo < 1 || mo > 12 || d < 1 || d > 31 || hh > 23 || mm > 59 || ss > 59) return null
  const dt = new Date(y, mo - 1, d, hh, mm, ss)
  if (dt.getFullYear() !== y || dt.getMonth() !== mo - 1 || dt.getDate() !== d) return null
  return dt
}

export function toLocalDateTimeValue(dt) {
  if (!dt) return ''
  const y = dt.getFullYear()
  const m = String(dt.getMonth() + 1).padStart(2, '0')
  const d = String(dt.getDate()).padStart(2, '0')
  const hh = String(dt.getHours()).padStart(2, '0')
  const mm = String(dt.getMinutes()).padStart(2, '0')
  return `${y}-${m}-${d} ${hh}:${mm}`
}

export function unixFromDateInput(s) {
  const dt = parseLocalDateTime(s)
  if (!dt) return 0
  return Math.floor(dt.getTime() / 1000)
}

export function pct(used, total) {
  if (!total || total === 0) return '0'
  return ((used / total) * 100).toFixed(1)
}

/** Extract Int64 from Go sql.NullInt64 JSON shape */
export function nullInt(v) {
  if (v == null) return null
  if (typeof v === 'object' && 'Valid' in v) return v.Valid ? v.Int64 : null
  return v
}

/** Extract String from Go sql.NullString JSON shape */
export function nullStr(v) {
  if (v == null) return null
  if (typeof v === 'object' && 'Valid' in v) return v.Valid ? v.String : null
  return v
}

export function isExpired(unix) {
  if (!unix) return false
  return unix < Math.floor(Date.now() / 1000)
}

/** Returns expiry status badge info:
 *  - >3 days: { color: 'green', label: '正常' }
 *  - ≤3 days & not expired: { color: 'red', label: '即将到期' }
 *  - expired: { color: 'gray', label: '已过期' }
 *  - no expiry: null */
export function expiryBadge(unix) {
  if (!unix || unix <= 0) return null
  const now = Math.floor(Date.now() / 1000)
  if (unix <= now) return { color: 'gray', label: '已过期' }
  const daysLeft = (unix - now) / 86400
  return daysLeft > 3 ? { color: 'green', label: '正常' } : { color: 'red', label: '即将到期' }
}
