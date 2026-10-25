import { fmtDuration } from './fmt'

export const DEFAULT_PANEL_LEASE_HOURS = 24

// panelLeaseSeconds matches the server clamp: missing or invalid → 24h, else 1–168h.
export function panelLeaseSeconds(hours) {
  const n = Number(hours)
  if (!Number.isFinite(n) || n < 1) return DEFAULT_PANEL_LEASE_HOURS * 3600
  return Math.min(168, Math.floor(n)) * 3600
}

// forwardingLeftSec is how long kernel rules keep forwarding after the
// management socket drops. null for rows that are online, disabled, composite,
// self, or never seen. A negative value means the lease has expired.
export function forwardingLeftSec(node, leaseHours, nowSec) {
  if (!node || node.disabled) return null
  if (node.node_type === 'composite' || node.node_type === 'self') return null
  if (node.online === 1) return null
  const seen = Number(node.last_seen) || 0
  if (!seen) return null
  const now = nowSec || Math.floor(Date.now() / 1000)
  return seen + panelLeaseSeconds(leaseHours) - now
}

export function isControlDown(node, leaseHours, nowSec) {
  const left = forwardingLeftSec(node, leaseHours, nowSec)
  return left != null && left > 0
}

export function controlRemainLabel(node, leaseHours, nowSec) {
  const left = forwardingLeftSec(node, leaseHours, nowSec)
  if (left == null || left <= 0) return ''
  return `转发大约还剩 ${fmtDuration(left)}`
}
