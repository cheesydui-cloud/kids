// Color theme resolution. The default follows the OS; an explicit user choice
// persisted in localStorage overrides it. A null stored value means "follow
// system", in which case we also react to live OS changes.
//
// Panel skin (xuan / porcelain) is a global admin setting. We cache the last
// known value so the first paint after reload matches before /branding returns.

const KEY = 'nf-theme'
const SKIN_KEY = 'nf-skin'
const mq = () => window.matchMedia('(prefers-color-scheme: dark)')

export function normalizeSkin(raw) {
  return raw === 'porcelain' ? 'porcelain' : 'xuan'
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
  if (persist) {
    try { localStorage.setItem(SKIN_KEY, skin) } catch { /* ignore quota */ }
  }
  return skin
}

export function resolvedDark(stored) {
  return stored === 'dark' || (stored == null && mq().matches)
}

export function getStoredTheme() {
  return localStorage.getItem(KEY) // 'dark' | 'light' | null(follow system)
}

export function applyTheme(stored) {
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
    if (getStoredTheme() == null) applyTheme(null)
  })
}
