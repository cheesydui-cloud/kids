import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import './index.css'
import { applyTheme, applySkin, getCachedSkin, getStoredTheme, initThemeWatcher } from './lib/theme'

applyTheme(getStoredTheme())
applySkin(getCachedSkin())
initThemeWatcher()

createRoot(document.getElementById('root')).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
