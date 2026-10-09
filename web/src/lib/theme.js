// Color theme resolution. The default follows the OS; an explicit user choice
// persisted in localStorage overrides it. A null stored value means "follow
// system", in which case we also react to live OS changes.
//
// Panel skin (xuan / porcelain / pixel / hud) is a global admin setting. We cache
// the last known value so the first paint after reload matches before /branding.
// 航电 (hud) locks the panel into dark: the light/dark toggle yields, and an OS
// theme change must not flip it back.

const KEY = 'nf-theme'
const SKIN_KEY = 'nf-skin'
const mq = () => window.matchMedia('(prefers-color-scheme: dark)')

export function normalizeSkin(raw) {
  if (raw === 'porcelain') return 'porcelain'
  if (raw === 'pixel') return 'pixel'
  if (raw === 'hud') return 'hud'
  return 'xuan'
}

export function getCachedSkin() {
  try {
    return normalizeSkin(localStorage.getItem(SKIN_KEY))
  } catch {
    return 'xuan'
  }
}

export function applySkin(raw, persist = true) {
  const skin = normalizeSkin(raw)
  document.documentElement.setAttribute('data-skin', skin)
  if (skin === 'hud') document.documentElement.classList.add('dark')
  else applyTheme(getStoredTheme())
  if (persist) {
    try { localStorage.setItem(SKIN_KEY, skin) } catch { /* ignore quota */ }
  }
  try { window.dispatchEvent(new CustomEvent('nf-skin', { detail: skin })) } catch { /* ignore */ }
  return skin
}

export function resolvedDark(stored) {
  return stored === 'dark' || (stored == null && mq().matches)
}

export function getStoredTheme() {
  return localStorage.getItem(KEY) // 'dark' | 'light' | null(follow system)
}

export function applyTheme(stored) {
  if (document.documentElement.getAttribute('data-skin') === 'hud') {
    document.documentElement.classList.add('dark')
    return
  }
  document.documentElement.classList.toggle('dark', resolvedDark(stored))
}

export function setStoredTheme(theme) {
  if (theme == null) localStorage.removeItem(KEY)
  else localStorage.setItem(KEY, theme)
  applyTheme(theme)
}

// Keep following the OS while the user hasn't pinned an explicit choice.
export function initThemeWatcher() {
  mq().addEventListener('change', () => {
    if (document.documentElement.getAttribute('data-skin') === 'hud') {
      document.documentElement.classList.add('dark')
      return
    }
    if (getStoredTheme() == null) applyTheme(null)
  })
}
