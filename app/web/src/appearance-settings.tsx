import { Check, Moon, Sun } from 'lucide-react'
import { themes } from './theme-preference'
import { useTheme } from './theme'
import './appearance-settings.css'

export function AppearanceSettings() {
  const { theme: selected, persisted, setTheme } = useTheme()
  return (
    <div className="appearance-settings">
      <div className="as-section-title">
        <div>
          <h2>外观</h2>
          <p>选一种喜欢的配色，让工作台更像自己的空间。</p>
        </div>
      </div>
      <fieldset className="theme-options">
        <legend>工作台主题</legend>
        <p className="theme-hint" id="theme-hint">
          切换后立即生效，仅保存在当前浏览器。
        </p>
        <div className="theme-grid">
          {themes.map((theme) => (
            <label className="theme-option" key={theme.id}>
              <input
                type="radio"
                name="workspace-theme"
                value={theme.id}
                checked={theme.id === selected}
                onChange={() => setTheme(theme.id)}
                aria-label={theme.name}
                aria-describedby={`theme-${theme.id}-description theme-hint`}
              />
              <span className="theme-card">
                <span className="theme-preview" data-theme={theme.id} aria-hidden="true">
                  <span className="theme-preview-sidebar">
                    <i className="theme-preview-logo" />
                    <i className="theme-preview-nav active" />
                    <i className="theme-preview-nav" />
                    <i className="theme-preview-nav" />
                  </span>
                  <span className="theme-preview-main">
                    <span className="theme-preview-heading">
                      <i />
                      <b />
                    </span>
                    <span className="theme-preview-content">
                      <span className="theme-preview-row">
                        <i />
                        <b />
                        <em />
                      </span>
                      <span className="theme-preview-row">
                        <i />
                        <b />
                        <em />
                      </span>
                      <span className="theme-preview-row">
                        <i />
                        <b />
                        <em />
                      </span>
                    </span>
                  </span>
                </span>
                <span className="theme-card-caption">
                  <span>
                    {theme.scheme === 'dark' ? <Moon size={15} /> : <Sun size={15} />}
                    <strong>{theme.name}</strong>
                  </span>
                  <span className="theme-check" aria-hidden="true">
                    <Check size={13} />
                  </span>
                </span>
                <span className="theme-description" id={`theme-${theme.id}-description`}>
                  {theme.description}
                </span>
              </span>
            </label>
          ))}
        </div>
      </fieldset>
      <p className="theme-save-status" role="status">
        <Check size={14} />
        {themes.find((theme) => theme.id === selected)!.name} ·{' '}
        {persisted ? '已应用' : '已应用，本次浏览期间有效'}
      </p>
    </div>
  )
}
