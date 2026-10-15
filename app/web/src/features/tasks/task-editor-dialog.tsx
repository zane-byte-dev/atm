import { useCallback, useState } from 'react'
import { Modal } from 'antd'
import { Editor } from './editor'
import type { TodoDetail, OperationWarning } from '../../types'
export function TaskEditorDialog({
  onClose,
  onSaved,
  defaultProject,
  initial,
  canRefine,
  onDirtyChange,
  onBusyChange,
}: {
  onClose: () => void
  onSaved: (id: string, warnings?: OperationWarning[]) => void
  defaultProject: string
  initial?: TodoDetail
  canRefine?: boolean
  onDirtyChange?: (dirty: boolean) => void
  onBusyChange?: (busy: boolean) => void
}) {
  const [busy, setBusy] = useState(false)
  const handleBusyChange = useCallback(
    (value: boolean) => {
      setBusy(value)
      onBusyChange?.(value)
    },
    [onBusyChange],
  )
  return (
    <Modal
      open
      centered
      width="min(780px, calc(100vw - 48px))"
      title={
        <span className="task-create-breadcrumb">
          <span>{defaultProject || '工作台'}</span>
          <span aria-hidden="true">›</span>
          <strong>{initial ? `编辑任务 · ${initial.todo.id}` : '手动创建'}</strong>
        </span>
      }
      className="task-create-modal"
      footer={null}
      closable={!busy}
      mask={{ closable: !busy }}
      keyboard={!busy}
      onCancel={() => {
        if (!busy) onClose()
      }}
      destroyOnHidden
    >
      <Editor
        onClose={onClose}
        onSaved={onSaved}
        initial={initial}
        canRefine={canRefine}
        onDirtyChange={onDirtyChange}
        onBusyChange={handleBusyChange}
        defaultProject={defaultProject}
      />
    </Modal>
  )
}
