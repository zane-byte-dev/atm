import { DatePicker } from 'antd'
import dayjs from 'dayjs'
import 'dayjs/locale/zh-cn'

// Keep the API and URL representation as a local calendar date, without UTC conversion.
export function DateInput({
  value,
  onChange,
  min,
  max,
  id,
  disabled,
  'aria-label': label,
}: {
  value: string
  onChange: (value: string) => void
  min?: string
  max?: string
  id?: string
  disabled?: boolean
  'aria-label'?: string
}) {
  return (
    <DatePicker
      id={id}
      aria-label={label}
      className="workspace-date-input"
      value={value ? dayjs(value).locale('zh-cn') : null}
      format="YYYY-MM-DD"
      placeholder="选择日期"
      minDate={min ? dayjs(min) : undefined}
      maxDate={max ? dayjs(max) : undefined}
      disabled={disabled}
      onChange={(date) => onChange(date?.format('YYYY-MM-DD') || '')}
    />
  )
}
