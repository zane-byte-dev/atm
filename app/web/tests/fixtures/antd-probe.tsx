import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { Button as AntButton, Checkbox, Switch, Radio, Input, Empty, Spin, Alert } from 'antd'
import { DesignSystemProvider } from '../../src/design-system'
import { Notice } from '../../src/components/markdown'
import { QueryLoading } from '../../src/components/query-loading'
import { EmptyActivity } from '../../src/features/agents/activity-shared'
import '../../src/app.css'

// Probe / regression fixture for the antd compat layer in design-system.css.
// Two things are asserted elsewhere in the browser tests:
//   1. legacy ATM classes stay visually authoritative over antd's injected styles
//   2. a <label> wrapping an antd Switch (which renders a <button>) toggles exactly
//      once per click, on both the text and the control
function Harness() {
  const [labelClicks, setLabelClicks] = useState(0)
  const [switchOn, setSwitchOn] = useState(false)
  const [checkOn, setCheckOn] = useState(false)
  return (
    <DesignSystemProvider>
      <div className="workspace">
        <div className="main" style={{ padding: 16, display: 'grid', gap: 12 }}>
          <button className="button" data-probe="raw-button">
            原生 button
          </button>
          <AntButton htmlType="button" className="button" data-probe="antd-button">
            antd button
          </AntButton>
          <button className="button primary" data-probe="raw-primary">
            原生 primary
          </button>
          <AntButton htmlType="button" className="button primary" data-probe="antd-primary">
            antd primary
          </AntButton>
          <button className="text-button" data-probe="raw-text">
            原生 text
          </button>
          <AntButton htmlType="button" className="text-button" data-probe="antd-text">
            antd text
          </AntButton>
          <button className="icon-button" data-probe="raw-icon" aria-label="原生图标">
            ×
          </button>
          <AntButton
            htmlType="button"
            className="icon-button"
            data-probe="antd-icon"
            aria-label="antd图标"
          >
            ×
          </AntButton>

          {/* The settings-toggle pattern used by business-settings and source-editor. */}
          <label className="workspace-check" data-probe="switch-label">
            <span>
              开关标题<small>开关说明</small>
            </span>
            <Switch
              data-probe="antd-switch-in-label"
              aria-label="开关标题"
              checked={switchOn}
              onChange={() => {
                setSwitchOn((v) => !v)
                setLabelClicks((n) => n + 1)
              }}
            />
          </label>
          <output aria-label="开关切换次数">{labelClicks}</output>
          <output aria-label="开关状态">{switchOn ? 'on' : 'off'}</output>

          <label className="workspace-check" data-probe="checkbox-label">
            <span>勾选项</span>
            <Checkbox
              aria-label="勾选项"
              checked={checkOn}
              onChange={(event) => setCheckOn(event.target.checked)}
            />
          </label>
          <output aria-label="勾选状态">{checkOn ? 'on' : 'off'}</output>

          <Switch data-probe="antd-switch" aria-label="独立开关" />
          <Radio data-probe="antd-radio" aria-label="antd单选">
            antd 单选
          </Radio>
          <Input data-probe="antd-input" aria-label="antd输入" placeholder="输入" />
          <Empty data-probe="antd-empty" description="空状态" />
          <Spin data-probe="antd-spin" />

          {/* The shared Notice component (src/markdown.tsx) is used at 45 call sites
              plus 10 hand-written .notice divs. Validate the Alert mapping here
              before applying it: antd v6 renamed `message` to `title`, hardcodes
              role="alert" (overridable because it precedes ...restProps), and puts
              the retry control in `action`. */}
          <div className="notice" data-probe="raw-notice" role="alert">
            <span>原生 notice：读取失败</span>
            <button type="button">重试</button>
          </div>
          <Alert
            data-probe="antd-notice"
            className="notice"
            type="error"
            title={<span>antd notice：读取失败</span>}
            action={
              <AntButton htmlType="button" className="text-button">
                重试
              </AntButton>
            }
          />
          <Alert
            data-probe="antd-status-notice"
            className="notice"
            type="info"
            role="status"
            title={<span>antd status notice</span>}
          />

          {/* The real shared components, so the assertions below cover what the app
              actually renders rather than a hand-written stand-in. */}
          <div data-probe="real-notice-wrap">
            <Notice error={new Error('读取失败：连接中断')} retry={() => {}} />
          </div>
          <QueryLoading className="probe-loading" text="正在读取任务…" />
          <EmptyActivity title="暂无会话">等待新的会话记录。</EmptyActivity>
          <Alert className="as-inline-error" type="error" title="设置保存失败" />
        </div>
      </div>
    </DesignSystemProvider>
  )
}

createRoot(document.getElementById('root')!).render(<Harness />)
