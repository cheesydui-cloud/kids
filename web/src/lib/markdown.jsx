/* Lightweight Markdown → React for usage docs.
   Supports: headings, paragraphs, lists, code, blockquote, links, images, hr,
   bold/italic/code, and safe font size/color spans:
     {s=18}文字{/}   {c=#9a4a28}文字{/}   {s=18 c=#9a4a28}文字{/}
   No raw HTML. Unsafe schemes on links/images are blocked. */

import { useState } from 'react'
import { copyToClipboard } from './clipboard'

function CodeBlock({ code, lang }) {
  const [copied, setCopied] = useState(false)
  const copy = () => {
    copyToClipboard(code).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }).catch(() => {})
  }
  return (
    <div className="md-cmd">
      <button type="button" className="md-cmd-copy" onClick={copy}>{copied ? '已复制' : '复制'}</button>
      <pre className="md-pre" data-lang={lang || undefined}><code>{code}</code></pre>
    </div>
  )
}

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

function safeURL(url) {
  const u = String(url || '').trim()
  if (!u) return null
  // Allow same-origin asset paths and relative paths used by our upload API.
  if (u.startsWith('/api/docs/assets/') || u.startsWith('./') || u.startsWith('../') || u.startsWith('/')) {
    // Block protocol-relative and weird path tricks
    if (u.startsWith('//')) return null
    return u
  }
  try {
    const parsed = new URL(u, 'https://example.invalid')
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') return u
  } catch {}
  return null
}

const ALLOWED_SIZES = new Set([12, 13, 14, 15, 16, 18, 20, 22, 24, 28, 32])
const HEX_COLOR = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/

function parseStyleOpen(text, i) {
  if (text[i] !== '{') return null
  const close = text.indexOf('}', i + 1)
  if (close === -1 || close - i > 80) return null
  const inner = text.slice(i + 1, close).trim()
  if (!inner || inner === '/') return null
  const style = {}
  const parts = inner.split(/\s+/)
  let any = false
  for (const p of parts) {
    const eq = p.indexOf('=')
    if (eq <= 0) return null
    const key = p.slice(0, eq)
    const val = p.slice(eq + 1)
    if (key === 's' || key === 'size') {
      const n = Number(val)
      if (!ALLOWED_SIZES.has(n)) return null
      style.fontSize = `${n}px`
      any = true
    } else if (key === 'c' || key === 'color') {
      if (!HEX_COLOR.test(val)) return null
      style.color = val
      any = true
    } else {
      return null
    }
  }
  if (!any) return null
  return { end: close + 1, style }
}

function findStyleClose(text, from) {
  let depth = 1
  let i = from
  while (i < text.length) {
    if (text.startsWith('{/}', i)) {
      depth--
      if (depth === 0) return i
      i += 3
      continue
    }
    const open = parseStyleOpen(text, i)
    if (open) {
      depth++
      i = open.end
      continue
    }
    i++
  }
  return -1
}

function matchAt(text, i, re) {
  re.lastIndex = i
  const m = re.exec(text)
  if (!m || m.index !== i) return null
  return m[0]
}

function renderInline(text, keyPrefix = 'i') {
  const nodes = []
  let i = 0
  let k = 0
  let buf = ''
  const flush = () => {
    if (buf) {
      nodes.push(buf)
      buf = ''
    }
  }
  const codeRe = /`[^`]+`/y
  const imgRe = /!\[[^\]]*\]\([^)]+\)/y
  const linkRe = /\[[^\]]+\]\([^)]+\)/y
  const boldRe = /(\*\*[^*]+\*\*)|(__[^_]+__)/y
  const emRe = /(\*[^*]+\*)|(_[^_]+_)/y

  while (i < text.length) {
    const styleOpen = parseStyleOpen(text, i)
    if (styleOpen) {
      const close = findStyleClose(text, styleOpen.end)
      if (close !== -1) {
        flush()
        const inner = text.slice(styleOpen.end, close)
        nodes.push(
          <span key={`${keyPrefix}-s${k++}`} className="md-fmt" style={styleOpen.style}>
            {renderInline(inner, `${keyPrefix}-s${k}`)}
          </span>
        )
        i = close + 3
        continue
      }
    }

    const code = matchAt(text, i, codeRe)
    if (code) {
      flush()
      nodes.push(<code key={`${keyPrefix}-c${k++}`} className="md-code">{code.slice(1, -1)}</code>)
      i += code.length
      continue
    }

    const img = matchAt(text, i, imgRe)
    if (img) {
      const im = img.match(/^!\[([^\]]*)\]\(([^)]+)\)$/)
      const src = im && safeURL(im[2])
      flush()
      if (src) {
        nodes.push(
          <img key={`${keyPrefix}-img${k++}`} src={src} alt={im[1] || ''} className="md-img" loading="lazy" />
        )
      } else {
        nodes.push(img)
      }
      i += img.length
      continue
    }

    const link = matchAt(text, i, linkRe)
    if (link) {
      const lm = link.match(/^\[([^\]]+)\]\(([^)]+)\)$/)
      const href = lm && safeURL(lm[2])
      flush()
      if (href) {
        const external = /^https?:/i.test(href)
        nodes.push(
          <a key={`${keyPrefix}-a${k++}`} href={href} className="md-link"
            {...(external ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>
            {lm[1]}
          </a>
        )
      } else {
        nodes.push(link)
      }
      i += link.length
      continue
    }

    const bold = matchAt(text, i, boldRe)
    if (bold) {
      flush()
      nodes.push(<strong key={`${keyPrefix}-b${k++}`}>{renderInline(bold.slice(2, -2), `${keyPrefix}-b${k}`)}</strong>)
      i += bold.length
      continue
    }

    const em = matchAt(text, i, emRe)
    if (em) {
      flush()
      nodes.push(<em key={`${keyPrefix}-e${k++}`}>{renderInline(em.slice(1, -1), `${keyPrefix}-e${k}`)}</em>)
      i += em.length
      continue
    }

    buf += text[i]
    i++
  }
  flush()
  return nodes
}

function flushParagraph(buf, out, key) {
  if (!buf.length) return
  const text = buf.join('\n').trim()
  if (text) out.push(<p key={key} className="md-p">{renderInline(text, key)}</p>)
  buf.length = 0
}

/** Render a Markdown string into a React tree. */
export function Markdown({ source, className = '' }) {
  const text = String(source || '').replace(/\r\n/g, '\n')
  const lines = text.split('\n')
  const out = []
  const para = []
  let i = 0
  let key = 0

  while (i < lines.length) {
    const line = lines[i]
    const trimmed = line.trim()

    // fenced code
    if (trimmed.startsWith('```')) {
      flushParagraph(para, out, `p${key++}`)
      const lang = trimmed.slice(3).trim()
      i++
      const codeLines = []
      while (i < lines.length && !lines[i].trim().startsWith('```')) {
        codeLines.push(lines[i])
        i++
      }
      if (i < lines.length) i++ // closing fence
      out.push(<CodeBlock key={`code${key++}`} lang={lang} code={codeLines.join('\n')} />)
      continue
    }

    // blank line ends paragraph
    if (trimmed === '') {
      flushParagraph(para, out, `p${key++}`)
      i++
      continue
    }

    // hr
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(trimmed)) {
      flushParagraph(para, out, `p${key++}`)
      out.push(<hr key={`hr${key++}`} className="md-hr" />)
      i++
      continue
    }

    // heading
    const hm = trimmed.match(/^(#{1,6})\s+(.+)$/)
    if (hm) {
      flushParagraph(para, out, `p${key++}`)
      const level = hm[1].length
      const Tag = `h${level}`
      out.push(
        <Tag key={`h${key++}`} className={`md-h md-h${level}`}>
          {renderInline(hm[2], `h${key}`)}
        </Tag>
      )
      i++
      continue
    }

    // blockquote
    if (trimmed.startsWith('>')) {
      flushParagraph(para, out, `p${key++}`)
      const q = []
      while (i < lines.length && lines[i].trim().startsWith('>')) {
        q.push(lines[i].trim().replace(/^>\s?/, ''))
        i++
      }
      out.push(
        <blockquote key={`q${key++}`} className="md-quote">
          {q.map((ql, qi) => <p key={qi} className="md-p">{renderInline(ql, `q${key}-${qi}`)}</p>)}
        </blockquote>
      )
      continue
    }

    // unordered list
    if (/^[-*+]\s+/.test(trimmed)) {
      flushParagraph(para, out, `p${key++}`)
      const items = []
      while (i < lines.length && /^[-*+]\s+/.test(lines[i].trim())) {
        items.push(lines[i].trim().replace(/^[-*+]\s+/, ''))
        i++
      }
      out.push(
        <ul key={`ul${key++}`} className="md-ul">
          {items.map((it, ii) => <li key={ii}>{renderInline(it, `ul${key}-${ii}`)}</li>)}
        </ul>
      )
      continue
    }

    // ordered list
    if (/^\d+\.\s+/.test(trimmed)) {
      flushParagraph(para, out, `p${key++}`)
      const items = []
      while (i < lines.length && /^\d+\.\s+/.test(lines[i].trim())) {
        items.push(lines[i].trim().replace(/^\d+\.\s+/, ''))
        i++
      }
      out.push(
        <ol key={`ol${key++}`} className="md-ol">
          {items.map((it, ii) => <li key={ii}>{renderInline(it, `ol${key}-${ii}`)}</li>)}
        </ol>
      )
      continue
    }

    // standalone image line → block figure
    const onlyImg = trimmed.match(/^!\[([^\]]*)\]\(([^)]+)\)$/)
    if (onlyImg) {
      flushParagraph(para, out, `p${key++}`)
      const src = safeURL(onlyImg[2])
      if (src) {
        out.push(
          <figure key={`fig${key++}`} className="md-figure">
            <img src={src} alt={onlyImg[1] || ''} className="md-img" loading="lazy" />
            {onlyImg[1] ? <figcaption className="md-cap">{onlyImg[1]}</figcaption> : null}
          </figure>
        )
      } else {
        para.push(line)
      }
      i++
      continue
    }

    para.push(line)
    i++
  }
  flushParagraph(para, out, `p${key++}`)

  if (!out.length) {
    return <div className={`md-body ${className}`}><p className="md-p text-ink-mut">（空文档）</p></div>
  }
  return <div className={`md-body ${className}`}>{out}</div>
}

// Keep escape helper available for future sanitizers.
export { escapeHtml }
