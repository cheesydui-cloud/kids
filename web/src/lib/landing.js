/* Client-side proxy-URI parsing and endpoint rewriting — the JS counterpart of
   the server's internal/landing package. It lives in the browser because the
   user's own proxy URIs are kept in localStorage and must never reach the
   server (privacy). The server still resolves admin-assigned landing nodes;
   this handles the user's own ones and merges them in, with the user's URIs
   winning on a host:port collision. */

const LS_PREFIX = 'nf-landing-uris:'
const LS_SUB_URL_PREFIX = 'nf-sub-urls:'
const LS_SUB_CACHE_PREFIX = 'nf-sub-cache:'

export function loadLocalURIs(username) {
  if (!username) return ''
  try { return localStorage.getItem(LS_PREFIX + username) || '' } catch { return '' }
}

export function saveLocalURIs(username, text) {
  if (!username) return
  try {
    if (text.trim()) localStorage.setItem(LS_PREFIX + username, text)
    else localStorage.removeItem(LS_PREFIX + username)
  } catch { /* storage unavailable — non-fatal */ }
  // Same-tab listeners (the nav) don't get the native 'storage' event, so emit
  // our own so the landing-nodes entry can appear/disappear immediately.
  try { window.dispatchEvent(new Event('nf-landing-changed')) } catch { /* SSR/no window */ }
}

export function hasLocalURIs(username) {
  return loadLocalURIs(username).trim() !== ''
}

export function hasLocalProxies(username) {
  return hasLocalURIs(username) || loadSubURLs(username).trim() !== ''
}

export function loadSubURLs(username) {
  if (!username) return ''
  try { return localStorage.getItem(LS_SUB_URL_PREFIX + username) || '' } catch { return '' }
}

export function saveSubURLs(username, text) {
  if (!username) return
  try {
    if (text.trim()) localStorage.setItem(LS_SUB_URL_PREFIX + username, text)
    else localStorage.removeItem(LS_SUB_URL_PREFIX + username)
  } catch {}
  try { window.dispatchEvent(new Event('nf-landing-changed')) } catch {}
}

export function loadSubCache(username) {
  if (!username) return []
  try {
    const raw = localStorage.getItem(LS_SUB_CACHE_PREFIX + username)
    return raw ? JSON.parse(raw) : []
  } catch { return [] }
}

export function saveSubCache(username, nodes) {
  if (!username) return
  try {
    if (nodes.length) localStorage.setItem(LS_SUB_CACHE_PREFIX + username, JSON.stringify(nodes))
    else localStorage.removeItem(LS_SUB_CACHE_PREFIX + username)
  } catch {}
  try { window.dispatchEvent(new Event('nf-landing-changed')) } catch {}
}

export function nodeRoleKey(n) {
  return n.protocol && n.host && n.port ? `${n.protocol}:${n.host}:${n.port}` : null
}

/* Node role is a bitmask so a node can be both a rule exit ("落地") and
   appear in the user's own proxy list ("直连") at the same time. */
export const ROLE_LANDING = 1
export const ROLE_DIRECT = 2
const ROLE_MASK = ROLE_LANDING | ROLE_DIRECT

// Pre-bitmask data (server settings row, browser localStorage) stored the
// role as the string 'landing'/'direct' — accept both on read so existing
// assignments survive the format change.
function roleBits(v) {
  if (typeof v === 'number') return v & ROLE_MASK
  if (v === 'landing') return ROLE_LANDING
  if (v === 'direct') return ROLE_DIRECT
  return 0
}

export function nodeHasRole(roles, n, bit) {
  const key = nodeRoleKey(n)
  return !!(key && (roleBits(roles[key]) & bit))
}

export async function fetchNodeRoles() {
  try {
    const res = await fetch('/api/node-roles', { credentials: 'same-origin' })
    if (!res.ok) return {}
    const d = await res.json()
    const out = {}
    for (const [k, v] of Object.entries(d?.roles || {})) {
      const bits = roleBits(v)
      if (bits) out[k] = bits
    }
    return out
  } catch { return {} }
}

export async function saveNodeRoles(roles) {
  const clean = {}
  for (const [k, v] of Object.entries(roles)) {
    const bits = roleBits(v)
    if (bits) clean[k] = bits
  }
  const res = await fetch('/api/node-roles', {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ roles: clean }),
  })
  if (!res.ok) {
    let msg = '保存用途失败'
    try {
      const d = await res.json()
      if (d?.error) msg = d.error
    } catch {}
    throw new Error(msg)
  }
  return clean
}

const LS_LOCAL_ROLES_PREFIX = 'nf-local-roles:'

export function loadLocalRoles(username) {
  if (!username) return {}
  try {
    const raw = localStorage.getItem(LS_LOCAL_ROLES_PREFIX + username)
    const parsed = raw ? JSON.parse(raw) : {}
    const out = {}
    for (const [k, v] of Object.entries(parsed)) {
      const bits = roleBits(v)
      if (bits) out[k] = bits
    }
    return out
  } catch { return {} }
}

export function saveLocalRoles(username, roles) {
  if (!username) return
  const clean = {}
  for (const [k, v] of Object.entries(roles)) {
    const bits = roleBits(v)
    if (bits) clean[k] = bits
  }
  try {
    if (Object.keys(clean).length) localStorage.setItem(LS_LOCAL_ROLES_PREFIX + username, JSON.stringify(clean))
    else localStorage.removeItem(LS_LOCAL_ROLES_PREFIX + username)
  } catch {}
}

// Toggle a single role bit for one node, leaving its other bit untouched.
export function applyNodeRole(roles, n, bit) {
  const next = { ...roles }
  const key = nodeRoleKey(n)
  if (!key) return next
  const nv = roleBits(next[key]) ^ bit
  if (nv) next[key] = nv; else delete next[key]
  return next
}

// Set (on=true) or clear (on=false) a role bit across many nodes at once.
export function applyNodeRoleBatch(roles, nodes, bit, on) {
  const next = { ...roles }
  for (const n of nodes) {
    const key = nodeRoleKey(n)
    if (!key) continue
    const cur = roleBits(next[key])
    const nv = on ? (cur | bit) : (cur & ~bit)
    if (nv) next[key] = nv; else delete next[key]
  }
  return next
}

export function nodeKey(n) {
  return n.host && n.port ? joinHostPort(n.host, n.port) : null
}

/* Parse a multiline blob of proxy URIs into landing nodes, skipping blank
   lines, comments (#...) and anything that doesn't resolve to host:port. */
export function parseURIs(text) {
  const out = []
  for (let line of (text || '').split('\n')) {
    line = line.trim()
    if (!line || line.startsWith('#')) continue
    const n = parseOne(line)
    if (n) out.push(n)
  }
  return out
}

/* Map "host:port" -> node; first wins, so callers should put higher-priority
   nodes (the user's own) first. */
export function landingIndex(nodes) {
  const m = new Map()
  for (const n of nodes) {
    const key = joinHostPort(n.host, n.port)
    if (!m.has(key)) m.set(key, n)
  }
  return m
}

/* If a rule's exit matches a landing node, rewrite the rule's entry endpoint
   into the proxy URI so the UI can offer a copyable relay URI. Returns the
   original rule when there is no match. */
export function enrichRuleWithLanding(rule, landingIdx) {
  const key = rule.exit_host && rule.exit_port ? `${rule.exit_host}:${rule.exit_port}` : null
  if (!key || !landingIdx.has(key) || !rule.entry) return rule
  const node = landingIdx.get(key)
  const ep = splitEndpoint(rule.entry)
  const relay = ep && rewriteEndpoint(node.uri, ep.host, ep.port)
  if (!relay) return rule
  const out = {
    ...rule,
    exit_kind: 'landing',
    landing_name: node.name,
    landing_protocol: node.protocol,
    relay_uri: relay,
  }
  // Domain entry is already the client-facing name — skip the raw IPv6 twin.
  if (rule.entry_v6 && ep && looksBareIP(ep.host)) {
    const ep6 = splitEndpoint(rule.entry_v6)
    const relay6 = ep6 && rewriteEndpoint(node.uri, ep6.host, ep6.port)
    if (relay6) out.relay_uri_v6 = relay6
  }
  return out
}

/* Parse a "host:port" string (e.g. a rule's entry endpoint) into {host, port},
   or null if malformed. Handles bracketed IPv6. */
export function splitEndpoint(s) {
  return splitHostPort(s)
}

/* Merge landing-node lists, de-duplicated by host:port with earlier lists
   winning — pass the user's own nodes first so they override admin ones. */
export function mergeLanding(...lists) {
  const seen = new Set()
  const out = []
  for (const list of lists) {
    for (const n of list || []) {
      const key = joinHostPort(n.host, n.port)
      if (seen.has(key)) continue
      seen.add(key)
      out.push(n)
    }
  }
  return out
}

/* Replace a proxy URI's connection host:port, preserving everything else. */
function looksBareIP(s) {
  const v = String(s || '').trim()
  if (!v) return false
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(v)) return true
  return v.includes(':') && !/[a-zA-Z]/.test(v)
}

/* Hostname written into a copied/stored share URI.
   Prefer the CF record name when the forwarding target is still an IP. */
export function repoShareHost(host, recordName) {
  const rec = String(recordName || '').trim()
  const h = String(host || '').trim()
  if (rec && looksBareIP(h)) return rec
  if (h && !looksBareIP(h)) return h
  return rec || h
}

/* Rewrite a repo share URI onto the current name + domain (or IP if no domain). */
export function rewriteRepoShareURI(uri, { name, host, recordName, port } = {}) {
  let out = String(uri || '').trim()
  if (!out) return out
  const shareHost = repoShareHost(host, recordName)
  const portNum = Number(port)
  if (shareHost && portNum > 0) {
    const next = rewriteEndpoint(out, shareHost, portNum)
    if (next) out = next
  }
  const label = String(name || '').trim()
  if (label) {
    const next = setURIName(out, label)
    if (next) out = next
  }
  return out
}

export function rewriteEndpoint(uri, host, port) {
  const i = uri.indexOf('://')
  if (i <= 0) return rewriteSnell(uri, host, port)
  const scheme = uri.slice(0, i).toLowerCase()
  if (scheme === 'vmess') return rewriteVMess(uri, host, port)
  if (scheme === 'ss') return rewriteSS(uri, host, port)
  if (scheme === 'mierus') return rewriteMierus(uri, host, port)
  return rewriteAuthority(uri, host, port)
}

export function tryParseURI(uri) {
  return parseOne((uri || '').trim())
}

/* Display name for a copied relay URI:
   `{username}-{ruleName}-{8月5日}` (omit empty parts).
   List UI keeps the landing node name; only copy/export uses this. */
export function fmtProxyExpiryLabel(unix) {
  if (!unix || unix <= 0) return ''
  const d = new Date(unix * 1000)
  if (Number.isNaN(d.getTime())) return ''
  return `${d.getMonth() + 1}月${d.getDate()}日`
}

export function buildRelayDisplayName({ username, ruleName, expiresAt } = {}) {
  const user = String(username || '').trim()
  const rule = String(ruleName || '').trim()
  const day = fmtProxyExpiryLabel(expiresAt)
  return [user, rule, day].filter(Boolean).join('-')
}

/* Replace only the human-visible name of a proxy URI (fragment / vmess.ps /
   snell left-hand name). Connection params stay untouched. */
export function setURIName(uri, name) {
  if (!uri || !name) return uri
  const n = String(name).trim()
  if (!n) return uri
  const i = uri.indexOf('://')
  if (i <= 0) return setSnellName(uri, n)
  const scheme = uri.slice(0, i).toLowerCase()
  if (scheme === 'vmess') return setVMessName(uri, n)
  const hash = uri.indexOf('#')
  const base = hash >= 0 ? uri.slice(0, hash) : uri
  return `${base}#${encodeURIComponent(n)}`
}

/* Rename a rewritten relay URI for clipboard export. Returns original uri
   when name cannot be built. */
export function renameRelayURI(uri, opts = {}) {
  if (!uri) return uri
  const name = buildRelayDisplayName(opts)
  if (!name) return uri
  return setURIName(uri, name) || uri
}

/* Allocate unique display names across a batch (Clash keys by name).
   Returns a Map from item key → final name. items: [{ key, username, ruleName, expiresAt }] */
export function allocateRelayDisplayNames(items) {
  const used = new Map()
  const out = new Map()
  for (const it of items || []) {
    const base = buildRelayDisplayName(it) || 'proxy'
    let name = base
    let n = 2
    while (used.has(name)) {
      name = `${base}-${n++}`
    }
    used.set(name, true)
    out.set(it.key, name)
  }
  return out
}

/* ---------- internals ---------- */

function parseOne(uri) {
  const n = parseRaw(uri)
  if (n) n.name = stripDedupSuffix(n.name)
  return n
}

function parseRaw(uri) {
  const i = uri.indexOf('://')
  if (i <= 0) return parseSnell(uri)
  const scheme = uri.slice(0, i).toLowerCase()
  if (scheme === 'vmess') return parseVMess(uri)
  if (scheme === 'ss') return parseSS(uri)
  if (scheme === 'http' || scheme === 'https') return null
  if (scheme === 'mierus') return parseMierus(uri)
  if (scheme === 'mieru') return parseMieru(uri)
  return parseAuthority(uri, normProto(scheme))
}

function normProto(scheme) {
  if (scheme === 'hy2') return 'hysteria2'
  if (scheme === 'naive+https' || scheme === 'naive+http' || scheme === 'naive') return 'naive'
  if (scheme === 'socks5h' || scheme === 'socks') return 'socks5'
  if (scheme === 'mieru' || scheme === 'mierus') return 'mieru'
  return scheme
}

/* Pull userinfo from scheme://user:pass@host:port... (socks5 / naive / trojan). */
export function extractUserPass(uri) {
  const raw = String(uri || '').trim()
  const i = raw.indexOf('://')
  if (i <= 0) return { username: '', password: '' }
  let rest = raw.slice(i + 3)
  const hash = rest.indexOf('#')
  if (hash >= 0) rest = rest.slice(0, hash)
  let end = rest.length
  for (let j = 0; j < rest.length; j++) {
    const c = rest[j]
    if (c === '/' || c === '?') { end = j; break }
  }
  const authority = rest.slice(0, end)
  const at = authority.lastIndexOf('@')
  if (at < 0) return { username: '', password: '' }
  const userinfo = authority.slice(0, at)
  const colon = userinfo.indexOf(':')
  if (colon < 0) return { username: safeDecode(userinfo), password: '' }
  return {
    username: safeDecode(userinfo.slice(0, colon)),
    password: safeDecode(userinfo.slice(colon + 1)),
  }
}

/* Form-only: accept a bare https://user:pass@host:port as Naive so operators
   can paste the common client URL. Bulk import / server still reject http(s)
   so subscription links are not stored as nodes. */
export function tryParseNaiveHTTPS(uri) {
  const raw = String(uri || '').trim()
  if (!/^https?:\/\//i.test(raw)) return null
  try {
    const u = new URL(raw)
    const port = Number(u.port)
    if (!u.hostname || !Number.isInteger(port) || port < 1 || port > 65535) return null
    if (!u.username && !u.password) return null
    return {
      name: u.hash ? safeDecode(u.hash.slice(1)) : '',
      protocol: 'naive',
      host: u.hostname,
      port,
      uri: raw,
      username: safeDecode(u.username || ''),
      password: safeDecode(u.password || ''),
    }
  } catch {
    return null
  }
}

export function isAuthFormProtocol(protocol) {
  const p = String(protocol || '').toLowerCase()
  return p === 'socks5' || p === 'socks5h' || p === 'naive' || p === 'mieru' || p === 'mierus'
}

/* SOCKS5 / Naive / Mieru share-link from IP + port + user + pass.
   Naive is stored as naive+https:// so the parser never confuses it with a
   subscription https:// URL. Mieru uses official mierus:// (port in query).
   Forwarding still uses host+port only. */
export function buildSimpleAuthURI({ protocol, host, port, username, password, name } = {}) {
  const proto = String(protocol || '').toLowerCase()
  const h = String(host || '').trim()
  const portNum = Number(port)
  const u = String(username || '')
  const p = String(password || '')
  const label = name && String(name).trim() ? String(name).trim() : ''
  if (proto === 'mieru' || proto === 'mierus') {
    if (!h || !portNum) return ''
    const q = new URLSearchParams()
    q.set('profile', label || 'default')
    q.set('port', String(portNum))
    q.set('protocol', 'TCP')
    const auth = (u || p) ? `${encodeURIComponent(u)}:${encodeURIComponent(p)}@` : ''
    const hostPart = h.includes(':') ? `[${h}]` : h
    const frag = label ? `#${encodeURIComponent(label)}` : ''
    return `mierus://${auth}${hostPart}?${q.toString()}${frag}`
  }
  const scheme = proto === 'naive' ? 'naive+https' : (proto === 'socks5' || proto === 'socks5h' ? 'socks5' : '')
  if (!scheme || !h || !portNum) return ''
  const hp = joinHostPort(h, portNum)
  const auth = (u || p) ? `${encodeURIComponent(u)}:${encodeURIComponent(p)}@` : ''
  const frag = label ? `#${encodeURIComponent(label)}` : ''
  return `${scheme}://${auth}${hp}${frag}`
}

/* Some panels (e.g. Remnawave) append "^~2~^"-style counters to same-named
   nodes in a subscription — typically the same node exported once per
   protocol. Nodes are identified by host:port here, so the counter is display
   noise; keep the name as-is when the counter is all there is. */
function stripDedupSuffix(name) {
  const out = (name || '').replace(/(\s*\^~\d+~\^)+$/, '').trim()
  return out || name
}

function parseAuthority(uri, proto) {
  const i = uri.indexOf('://')
  let rest = uri.slice(i + 3)
  let name = ''
  const h = rest.indexOf('#')
  if (h >= 0) { name = safeDecode(rest.slice(h + 1)); rest = rest.slice(0, h) }
  let end = rest.length
  for (let j = 0; j < rest.length; j++) {
    const c = rest[j]
    if (c === '/' || c === '?') { end = j; break }
  }
  let authority = rest.slice(0, end)
  const at = authority.lastIndexOf('@')
  if (at >= 0) authority = authority.slice(at + 1)
  const hp = splitHostPort(authority)
  if (!hp) return null
  return { name, protocol: proto, host: hp.host, port: hp.port, uri }
}

function parseVMess(uri) {
  const dec = b64decode(uri.slice('vmess://'.length))
  if (!dec) return null
  let m
  try { m = JSON.parse(dec) } catch { return null }
  const host = m.add
  const port = Number(m.port)
  if (!host || !(port >= 1 && port <= 65535)) return null
  return { name: m.ps || '', protocol: 'vmess', host, port, uri }
}

function firstMieruPort(vals) {
  for (const raw0 of vals || []) {
    let raw = String(raw0 || '').trim()
    if (!raw) continue
    const dash = raw.indexOf('-')
    if (dash > 0) raw = raw.slice(0, dash).trim()
    const n = Number(raw)
    if (Number.isInteger(n) && n >= 1 && n <= 65535) return n
  }
  return 0
}

function parseMierus(uri) {
  const i = uri.indexOf('://')
  let rest = uri.slice(i + 3)
  let name = ''
  const h = rest.indexOf('#')
  if (h >= 0) { name = safeDecode(rest.slice(h + 1)); rest = rest.slice(0, h) }
  const q = rest.indexOf('?')
  let authority = q >= 0 ? rest.slice(0, q) : rest
  const query = q >= 0 ? rest.slice(q + 1) : ''
  const at = authority.lastIndexOf('@')
  if (at >= 0) authority = authority.slice(at + 1)
  let host = authority
  let authPort = 0
  const hp = splitHostPort(authority)
  if (hp) { host = hp.host; authPort = hp.port }
  const params = new URLSearchParams(query)
  const port = firstMieruPort(params.getAll('port')) || authPort
  if (!host || !port) return null
  if (!name) name = params.get('profile') || ''
  return { name, protocol: 'mieru', host, port, uri }
}

function parseMieru(uri) {
  const auth = parseAuthority(uri, 'mieru')
  if (auth) return auth
  let rest = uri.slice('mieru://'.length)
  let name = ''
  const h = rest.indexOf('#')
  if (h >= 0) { name = safeDecode(rest.slice(h + 1)); rest = rest.slice(0, h) }
  const dec = b64decode(rest)
  if (!dec) return null
  let root
  try { root = JSON.parse(dec) } catch { return null }
  const profiles = Array.isArray(root.profiles) ? root.profiles : []
  const prof = profiles[0] || root
  if (!prof || typeof prof !== 'object') return null
  const servers = Array.isArray(prof.servers) ? prof.servers : []
  const srv = servers[0]
  if (!srv) return null
  const host = srv.ipAddress || srv.domainName
  let port = 0
  for (const b of srv.portBindings || []) {
    const p = Number(b?.port)
    if (Number.isInteger(p) && p >= 1 && p <= 65535) { port = p; break }
    const fromRange = firstMieruPort([b?.portRange])
    if (fromRange) { port = fromRange; break }
  }
  if (!host || !port) return null
  return { name: name || prof.profileName || root.activeProfile || '', protocol: 'mieru', host, port, uri }
}

function rewriteMierus(uri, newHost, newPort) {
  const i = uri.indexOf('://')
  if (i <= 0) return null
  const prefix = uri.slice(0, i + 3)
  const rest = uri.slice(i + 3)
  let frag = ''
  let body = rest
  const hash = body.indexOf('#')
  if (hash >= 0) { frag = body.slice(hash); body = body.slice(0, hash) }
  const q = body.indexOf('?')
  const authority = q >= 0 ? body.slice(0, q) : body
  const query = q >= 0 ? body.slice(q + 1) : ''
  let userinfo = ''
  const at = authority.lastIndexOf('@')
  if (at >= 0) userinfo = authority.slice(0, at + 1)
  const params = new URLSearchParams(query)
  const ports = params.getAll('port')
  params.delete('port')
  if (ports.length === 0) {
    params.append('port', String(newPort))
    if (!params.get('protocol')) params.append('protocol', 'TCP')
  } else {
    for (let n = 0; n < ports.length; n++) params.append('port', String(newPort))
  }
  if (!params.get('profile')) params.set('profile', 'default')
  const hostPart = String(newHost).includes(':') ? `[${newHost}]` : newHost
  return `${prefix}${userinfo}${hostPart}?${params.toString()}${frag}`
}

function parseSS(uri) {
  let rest = uri.slice('ss://'.length)
  let name = ''
  const h = rest.indexOf('#')
  if (h >= 0) { name = safeDecode(rest.slice(h + 1)); rest = rest.slice(0, h) }
  const q = rest.indexOf('?')
  if (q >= 0) rest = rest.slice(0, q)
  let hostport
  const at = rest.lastIndexOf('@')
  if (at >= 0) {
    hostport = rest.slice(at + 1)
  } else {
    const dec = b64decode(rest)
    if (!dec) return null
    const at2 = dec.lastIndexOf('@')
    if (at2 < 0) return null
    hostport = dec.slice(at2 + 1)
  }
  const hp = splitHostPort(hostport)
  if (!hp) return null
  return { name, protocol: 'ss', host: hp.host, port: hp.port, uri }
}

function rewriteAuthority(uri, newHost, newPort) {
  const i = uri.indexOf('://')
  const prefix = uri.slice(0, i + 3)
  const rest = uri.slice(i + 3)
  let end = rest.length
  for (let j = 0; j < rest.length; j++) {
    const c = rest[j]
    if (c === '/' || c === '?' || c === '#') { end = j; break }
  }
  const authority = rest.slice(0, end)
  const tail = rest.slice(end)
  let userinfo = ''
  const at = authority.lastIndexOf('@')
  if (at >= 0) userinfo = authority.slice(0, at + 1)
  return prefix + userinfo + joinHostPort(newHost, newPort) + tail
}

function rewriteVMess(uri, newHost, newPort) {
  const dec = b64decode(uri.slice('vmess://'.length))
  if (!dec) return null
  let m
  try { m = JSON.parse(dec) } catch { return null }
  m.add = newHost
  m.port = String(newPort)
  return 'vmess://' + b64encode(JSON.stringify(m))
}

function rewriteSS(uri, newHost, newPort) {
  let rest = uri.slice('ss://'.length)
  let frag = ''
  const h = rest.indexOf('#')
  if (h >= 0) { frag = rest.slice(h); rest = rest.slice(0, h) }
  if (rest.lastIndexOf('@') >= 0) return rewriteAuthority(uri, newHost, newPort)
  let query = ''
  const q = rest.indexOf('?')
  if (q >= 0) { query = rest.slice(q); rest = rest.slice(0, q) }
  const dec = b64decode(rest)
  if (!dec) return null
  const at = dec.lastIndexOf('@')
  if (at < 0) return null
  const payload = dec.slice(0, at + 1) + joinHostPort(newHost, newPort)
  return 'ss://' + b64encode(payload) + query + frag
}

function parseSnell(line) {
  const eq = line.indexOf('=')
  if (eq < 0) return null
  const name = line.slice(0, eq).trim()
  const rest = line.slice(eq + 1).trim()
  const parts = rest.split(',').map(s => s.trim())
  if (parts.length < 3 || parts[0].toLowerCase() !== 'snell') return null
  const host = parts[1]
  const port = Number(parts[2])
  if (!host || !Number.isInteger(port) || port < 1 || port > 65535) return null
  return { name, protocol: 'snell', host, port, uri: line }
}

function rewriteSnell(line, newHost, newPort) {
  const eq = line.indexOf('=')
  if (eq < 0) return null
  const rest = line.slice(eq + 1).trim()
  const parts = rest.split(',')
  if (parts.length < 3 || parts[0].trim().toLowerCase() !== 'snell') return null
  parts[1] = ' ' + newHost
  parts[2] = ' ' + String(newPort)
  return line.slice(0, eq + 1) + ' ' + parts.join(',')
}

function setVMessName(uri, name) {
  const dec = b64decode(uri.slice('vmess://'.length))
  if (!dec) return uri
  let m
  try { m = JSON.parse(dec) } catch { return uri }
  m.ps = name
  return 'vmess://' + b64encode(JSON.stringify(m))
}

function setSnellName(line, name) {
  const eq = line.indexOf('=')
  if (eq < 0) return line
  return `${name} =${line.slice(eq + 1)}`
}

function splitHostPort(authority) {
  if (!authority) return null
  let host, portStr
  if (authority.startsWith('[')) {
    const close = authority.indexOf(']')
    if (close < 0) return null
    host = authority.slice(1, close)
    const rem = authority.slice(close + 1)
    if (!rem.startsWith(':')) return null
    portStr = rem.slice(1)
  } else {
    const c = authority.lastIndexOf(':')
    if (c < 0) return null
    host = authority.slice(0, c)
    portStr = authority.slice(c + 1)
  }
  const port = Number(portStr)
  if (!host || !Number.isInteger(port) || port < 1 || port > 65535) return null
  return { host, port }
}

function joinHostPort(host, port) {
  return host.includes(':') ? `[${host}]:${port}` : `${host}:${port}`
}

function b64decode(s) {
  const candidates = [s, s.replace(/-/g, '+').replace(/_/g, '/')]
  for (const v of candidates) {
    const pad = v.length % 4 ? '='.repeat(4 - (v.length % 4)) : ''
    try {
      const bin = atob(v + pad)
      try { return decodeURIComponent(escape(bin)) } catch { return bin }
    } catch { /* try next */ }
  }
  return null
}

function b64encode(s) {
  try { return btoa(unescape(encodeURIComponent(s))) } catch { return btoa(s) }
}

function safeDecode(s) {
  try { return decodeURIComponent(s) } catch { return s }
}
