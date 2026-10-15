import { Alert, Button as AntButton } from 'antd'
import { errorText } from '../api'

export function Notice({ error, retry }: { error: unknown; retry?: () => void }) {
  return (
    <Alert
      className="notice"
      type="error"
      title={errorText(error)}
      action={
        retry ? (
          <AntButton htmlType="button" onClick={retry}>
            重试
          </AntButton>
        ) : undefined
      }
    />
  )
}
