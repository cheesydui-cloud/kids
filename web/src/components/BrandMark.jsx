// Triangle of three bidirectional arrows — panel brand mark (scheme A).
// Sized to the golden ratio relative to the 42px badge:
//   badge : mark ≈ φ (1.618) → mark ≈ 26px in a 42px box (viewBox scaled).
// Arrow stroke and radius keep white glyphs large enough to read at 42px.
export function BrandMark({ className = 'w-[26px] h-[26px]' }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.15" strokeLinecap="round" strokeLinejoin="round">
      <g transform="translate(12 12)" vectorEffect="non-scaling-stroke">
        <BidirectionalArrow />
        <g transform="rotate(120)"><BidirectionalArrow /></g>
        <g transform="rotate(240)"><BidirectionalArrow /></g>
      </g>
    </svg>
  )
}

const badgeBg = { background: 'linear-gradient(145deg, #10b981 0%, #14b8a6 52%, #0d9488 100%)' }

// Custom logos fill the whole rounded badge. The default mark stays inset on
// the green tile so a 26px glyph does not look like a cropped photo.
export function BrandBadge({ src, size = 42, className = '', title, markClassName = 'w-[26px] h-[26px]' }) {
  const box = `flex-none overflow-hidden rounded-[14px] ring-1 ring-white/25 ${className}`
  const dim = { width: size, height: size }
  if (src) {
    return (
      <div className={`${box} bg-surface shadow-[0_10px_24px_-8px_rgba(16,185,129,0.35)]`} style={dim} title={title}>
        <img src={src} alt="" className="w-full h-full object-cover" />
      </div>
    )
  }
  return (
    <div className={`${box} grid place-items-center text-white shadow-[0_10px_24px_-8px_rgba(16,185,129,0.65)]`}
      style={{ ...dim, ...badgeBg }} title={title}>
      <BrandMark className={markClassName} />
    </div>
  )
}

// One side of the equilateral layout: horizontal double arrow, scaled and
// offset so the three copies form a clear triangle without shrinking into
// a tiny white knot in the badge center.
function BidirectionalArrow() {
  // radius ≈ 5.5 keeps tips near the badge edge; scale 0.72 enlarges glyphs.
  return (
    <g transform="translate(0 5.5) scale(0.72)">
      <g transform="translate(-12 -12)">
        <path d="M17 7 21 11 17 15" />
        <path d="M21 11H7" />
        <path d="M7 17 3 13 7 9" />
        <path d="M3 13H17" />
      </g>
    </g>
  )
}
