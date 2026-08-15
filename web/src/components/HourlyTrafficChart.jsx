import { useMemo, useState } from 'react'
import { fmtBytes } from '../lib/fmt'

const W = 960
const H = 280
const PAD = { top: 18, right: 16, bottom: 32, left: 52 }

function hourLabel(hour) {
  if (!hour || hour.length < 13) return '--'
  return `${hour.slice(11, 13)}:00`
}

function niceMax(n) {
  if (!n || n <= 0) return 4
  const exp = Math.floor(Math.log10(n))
  const base = 10 ** exp
  const f = n / base
  const nice = f <= 1 ? 1 : f <= 2 ? 2 : f <= 4 ? 4 : f <= 5 ? 5 : 10
  return nice * base
}

function yTicks(max) {
  return [0, max * 0.25, max * 0.5, max * 0.75, max]
}

function catmullRom(points) {
  if (points.length === 0) return ''
  if (points.length === 1) return `M ${points[0].x} ${points[0].y}`
  let d = `M ${points[0].x} ${points[0].y}`
  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[i === 0 ? 0 : i - 1]
    const p1 = points[i]
    const p2 = points[i + 1]
    const p3 = points[i + 2] || p2
    const c1x = p1.x + (p2.x - p0.x) / 6
    const c1y = p1.y + (p2.y - p0.y) / 6
    const c2x = p2.x - (p3.x - p1.x) / 6
    const c2y = p2.y - (p3.y - p1.y) / 6
    d += ` C ${c1x} ${c1y}, ${c2x} ${c2y}, ${p2.x} ${p2.y}`
  }
  return d
}

export function HourlyTrafficChart({ series = [] }) {
  const [hover, setHover] = useState(null)

  const points = useMemo(() => {
    const rows = Array.isArray(series) && series.length ? series : []
    const filled = rows.length === 24
      ? rows
      : Array.from({ length: 24 }, (_, i) => rows[i] || { hour: '', bytes: 0 })
    return filled.map(p => ({ hour: p.hour || '', bytes: Number(p.bytes) || 0 }))
  }, [series])

  const max = useMemo(() => niceMax(Math.max(0, ...points.map(p => p.bytes))), [points])
  const innerW = W - PAD.left - PAD.right
  const innerH = H - PAD.top - PAD.bottom
  const step = points.length > 1 ? innerW / (points.length - 1) : innerW

  const coords = points.map((p, i) => ({
    ...p,
    x: PAD.left + i * step,
    y: PAD.top + innerH - (p.bytes / max) * innerH,
  }))

  const line = catmullRom(coords)
  const area = coords.length
    ? `${line} L ${coords[coords.length - 1].x} ${PAD.top + innerH} L ${coords[0].x} ${PAD.top + innerH} Z`
    : ''

  const onMove = (e) => {
    const svg = e.currentTarget
    const rect = svg.getBoundingClientRect()
    const x = ((e.clientX - rect.left) / rect.width) * W
    let best = 0
    let bestDist = Infinity
    coords.forEach((p, i) => {
      const d = Math.abs(p.x - x)
      if (d < bestDist) {
        bestDist = d
        best = i
      }
    })
    setHover(best)
  }

  const active = hover != null ? coords[hover] : null

  const tipLeft = active ? Math.min(86, Math.max(14, (active.x / W) * 100)) : 50

  return (
    <div className="card mb-5 overflow-hidden">
      <div className="card-header">
        <h3 className="text-[15px] font-bold flex items-center gap-2">
          <span className="w-2 h-2 rounded-full bg-[color:var(--brand-from)]" />
          24小时流量统计
        </h3>
      </div>
      <div className="relative px-3 pb-3 pt-1">
        <svg
          viewBox={`0 0 ${W} ${H}`}
          className="w-full h-[240px] sm:h-[280px] select-none"
          onMouseMove={onMove}
          onMouseLeave={() => setHover(null)}
          role="img"
          aria-label="近 24 小时实际流量"
        >
          {yTicks(max).map((v, i) => {
            const y = PAD.top + innerH - (v / max) * innerH
            return (
              <g key={i}>
                <line x1={PAD.left} x2={W - PAD.right} y1={y} y2={y} stroke="var(--color-line)" strokeDasharray="3 5" strokeWidth="1" />
                <text x={PAD.left - 8} y={y + 4} textAnchor="end" fontSize="11" fill="var(--color-ink-mut)">
                  {v === 0 ? '0' : fmtBytes(v)}
                </text>
              </g>
            )
          })}
          {coords.map((p, i) => (
            i % 2 === 0 ? (
              <text key={p.hour || i} x={p.x} y={H - 10} textAnchor="middle" fontSize="11" fill="var(--color-ink-mut)">
                {hourLabel(p.hour)}
              </text>
            ) : null
          ))}
          <defs>
            <linearGradient id="hourlyFill" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--brand-from)" stopOpacity="0.22" />
              <stop offset="100%" stopColor="var(--brand-from)" stopOpacity="0.02" />
            </linearGradient>
          </defs>
          {area && <path d={area} fill="url(#hourlyFill)" />}
          {line && <path d={line} fill="none" stroke="var(--brand-from)" strokeWidth="2.4" strokeLinejoin="round" strokeLinecap="round" />}
          {active && (
            <>
              <line x1={active.x} x2={active.x} y1={PAD.top} y2={PAD.top + innerH} stroke="var(--color-line)" strokeWidth="1.2" />
              <circle cx={active.x} cy={active.y} r="5" fill="var(--color-surface)" stroke="var(--brand-from)" strokeWidth="2.2" />
            </>
          )}
        </svg>
        {active && (
          <div
            className="pointer-events-none absolute top-[28%] -translate-x-full rounded-xl border-[1.5px] border-line bg-surface px-3 py-2 text-[12.5px] shadow-sm"
            style={{ left: `${tipLeft}%` }}
          >
            <div className="text-ink font-semibold">时间: {hourLabel(active.hour)}</div>
            <div className="mt-0.5" style={{ color: 'var(--brand-from)' }}>流量: {fmtBytes(active.bytes)}</div>
          </div>
        )}
      </div>
    </div>
  )
}
