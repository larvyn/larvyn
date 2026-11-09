import React from 'react'
import ReactDOM from 'react-dom/client'
import '@fontsource-variable/inter'
import '@fontsource-variable/jetbrains-mono'
import './styles/app.css'
import './lib/theme'
import App from './App'
import { SystemService } from '../bindings/larvyn/internal/system'

const report = (level: string, msg: string) => SystemService.Log(level, msg).catch(() => {})
window.addEventListener('error', (e) => report('error', `${e.message} at ${e.filename}:${e.lineno}
${e.error?.stack ?? ''}`))
window.addEventListener('unhandledrejection', (e) => report('rejection', String(e.reason?.stack ?? e.reason?.message ?? e.reason)))

ReactDOM.createRoot(document.getElementById('root') as HTMLElement).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
