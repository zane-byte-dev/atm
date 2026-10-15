import type { ReactNode } from 'react'
import { StyleProvider } from '@ant-design/cssinjs'
import { App as AntApp, ConfigProvider, theme as antdTheme } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { useTheme } from './theme'

// The workspace CSS variables remain the source of truth for all five palettes.
// Ant Design reads the same values so native and library controls stay aligned.
export function DesignSystemProvider({ children }: { children: ReactNode }) {
  const { theme } = useTheme()
  const css = getComputedStyle(document.documentElement)
  const color = (name: string) => css.getPropertyValue(name).trim()
  return (
    // `layer` puts every antd CSS-in-JS rule into `@layer antd`. Unlayered ATM
    // stylesheets then win the cascade unconditionally, which is what the design
    // language requires. Without it antd's rules are `:where(.css-hash).ant-btn`
    // — specificity (0,1,0), the same as a plain legacy class like
    // `.inbox-icon-button` — and because cssinjs appends its <style> at runtime
    // the tie breaks in antd's favour in a production build even though the app
    // stylesheet wins in dev. That would silently restyle every control on
    // release only.
    <StyleProvider layer>
      <ConfigProvider
        locale={zhCN}
        // antd inserts a space between two CJK characters in a single-child
        // button ("取消" renders as "取 消"). That breaks exact-text lookups in
        // the browser tests and reads wrong next to the Latin labels.
        button={{ autoInsertSpace: false }}
        theme={{
          algorithm: theme === 'night' ? antdTheme.darkAlgorithm : antdTheme.defaultAlgorithm,
          token: {
            colorPrimary: color('--accent'),
            colorText: color('--text'),
            colorTextSecondary: color('--muted'),
            colorBgBase: color('--canvas'),
            colorBgContainer: color('--surface'),
            colorBgElevated: color('--surface'),
            colorBorder: color('--line'),
            colorError: color('--danger'),
            colorSuccess: color('--success'),
            colorWarning: color('--warning'),
            borderRadius: 10,
            controlHeight: 34,
            fontSize: 14,
            fontFamily:
              "Inter, -apple-system, BlinkMacSystemFont, 'Segoe UI', 'PingFang SC', sans-serif",
          },
        }}
      >
        <AntApp component={false} notification={{ placement: 'topRight', maxCount: 3 }}>
          {children}
        </AntApp>
      </ConfigProvider>
    </StyleProvider>
  )
}
