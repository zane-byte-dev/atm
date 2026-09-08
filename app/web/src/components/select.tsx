import {
  Children,
  Fragment,
  isValidElement,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
  type SelectHTMLAttributes,
} from 'react'
import { Check, ChevronDown, Search } from 'lucide-react'
import './select.css'
type Option = { value: string; label: string; disabled: boolean }
function optionText(node: ReactNode): string {
  return Children.toArray(node)
    .map((child) =>
      isValidElement<{ children?: ReactNode }>(child)
        ? optionText(child.props.children)
        : String(child),
    )
    .join('')
}
function readOptions(children: ReactNode, disabled = false): Option[] {
  return Children.toArray(children).flatMap((child) => {
    if (
      !isValidElement<{ value?: string | number; children?: ReactNode; disabled?: boolean }>(child)
    )
      return []
    if (child.type === 'option')
      return [
        {
          value: String(child.props.value ?? optionText(child.props.children)),
          label: optionText(child.props.children),
          disabled: disabled || !!child.props.disabled,
        },
      ]
    if (child.type === Fragment || child.type === 'optgroup')
      return readOptions(child.props.children, disabled || !!child.props.disabled)
    return []
  })
}
// The hidden select preserves form serialization and existing typed change handlers.
// All visible interaction is an accessible custom combobox with a top-layer listbox.
export function Select({
  children,
  className = '',
  id,
  autoFocus,
  ...props
}: SelectHTMLAttributes<HTMLSelectElement>) {
  const uid = useId()
  const options = readOptions(children)
  const root = useRef<HTMLSpanElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const native = useRef<HTMLSelectElement>(null)
  const popup = useRef<HTMLDivElement>(null)
  const search = useRef<HTMLInputElement>(null)
  const [uncontrolled, setUncontrolled] = useState(
    String(props.defaultValue ?? options[0]?.value ?? ''),
  )
  const current = props.value === undefined ? uncontrolled : String(props.value)
  const chosen = options.find((option) => option.value === current)
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [active, setActive] = useState(0)
  const [label, setLabel] = useState('')
  const filtered = options.filter((option) =>
    option.label.toLocaleLowerCase().includes(query.toLocaleLowerCase()),
  )
  const searchable = options.length > 7
  const close = (focus = false) => {
    setOpen(false)
    if (focus) trigger.current?.focus()
  }
  useLayoutEffect(() => {
    const parent = root.current?.closest('label')
    const external = id
      ? document.querySelector<HTMLLabelElement>(`label[for="${CSS.escape(id)}"]`)
      : null
    const source = parent || external
    if (source)
      setLabel(
        [...source.childNodes]
          .filter((node) => node !== root.current)
          .map((node) => node.textContent || '')
          .join('')
          .trim(),
      )
    if (autoFocus) trigger.current?.focus()
  }, [id, autoFocus])
  useLayoutEffect(() => {
    if (!open || !popup.current || !trigger.current) {
      popup.current?.hidePopover()
      return
    }
    const panel = popup.current
    const position = () => {
      const rect = trigger.current!.getBoundingClientRect()
      const width = Math.min(Math.max(rect.width, 220), window.innerWidth - 16)
      const below = window.innerHeight - rect.bottom - 12
      const above = rect.top - 12
      const placeBelow = below >= Math.min(240, above)
      const height = Math.max(60, Math.min(320, placeBelow ? below : above))
      Object.assign(panel.style, {
        width: `${width}px`,
        maxHeight: `${height}px`,
        left: `${Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))}px`,
        top: placeBelow ? `${rect.bottom + 5}px` : 'auto',
        bottom: placeBelow ? 'auto' : `${window.innerHeight - rect.top + 5}px`,
      })
    }
    position()
    panel.showPopover()
    if (searchable) search.current?.focus()
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) close()
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopPropagation()
        close(true)
      }
    }
    document.addEventListener('pointerdown', outside)
    panel.addEventListener('keydown', escape)
    window.addEventListener('resize', position)
    const scroll = (event: Event) => {
      if (!panel.contains(event.target as Node)) position()
    }
    window.addEventListener('scroll', scroll, true)
    return () => {
      panel.hidePopover()
      document.removeEventListener('pointerdown', outside)
      panel.removeEventListener('keydown', escape)
      window.removeEventListener('resize', position)
      window.removeEventListener('scroll', scroll, true)
    }
  }, [open, searchable])
  useEffect(() => {
    if (props.disabled) close()
  }, [props.disabled])
  useEffect(() => {
    if (open)
      popup.current?.querySelector(`[data-active="true"]`)?.scrollIntoView({ block: 'nearest' })
  }, [active, open])
  const expand = () => {
    if (props.disabled) return
    setQuery('')
    setActive(
      Math.max(
        0,
        options.findIndex((option) => option.value === current && !option.disabled),
      ),
    )
    setOpen(true)
  }
  const choose = (option: Option) => {
    if (option.disabled || props.disabled || !native.current) return
    native.current.value = option.value
    native.current.dispatchEvent(new Event('change', { bubbles: true }))
    close(true)
  }
  const keyboard = (event: React.KeyboardEvent) => {
    if (event.nativeEvent.isComposing || event.keyCode === 229) return
    if (event.key === 'Escape') {
      if (open) {
        event.preventDefault()
        event.stopPropagation()
        close(true)
      }
      return
    }
    if (event.key === 'Tab') {
      close()
      return
    }
    if (['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) {
      event.preventDefault()
      if (!open) {
        expand()
        return
      }
      const enabled = filtered
        .map((option, index) => (option.disabled ? -1 : index))
        .filter((index) => index >= 0)
      if (!enabled.length) return
      const index = enabled.indexOf(active)
      setActive(
        event.key === 'Home'
          ? enabled[0]
          : event.key === 'End'
            ? enabled[enabled.length - 1]
            : enabled[
                index < 0
                  ? event.key === 'ArrowDown'
                    ? 0
                    : enabled.length - 1
                  : (index + (event.key === 'ArrowDown' ? 1 : -1) + enabled.length) % enabled.length
              ],
      )
    } else if (event.key === 'Enter' || (event.key === ' ' && event.target === trigger.current)) {
      event.preventDefault()
      if (open && filtered[active]) choose(filtered[active])
      else expand()
    } else if (!searchable && event.key.length === 1 && !event.ctrlKey && !event.metaKey) {
      const index = options.findIndex(
        (option) =>
          !option.disabled &&
          option.label.toLocaleLowerCase().startsWith(event.key.toLocaleLowerCase()),
      )
      if (index >= 0) {
        event.preventDefault()
        if (!open) setOpen(true)
        setActive(index)
      }
    }
  }
  return (
    <span
      ref={root}
      className={`ui-select ${className}`}
      style={{ padding: 0, border: 0, background: 'transparent' }}
    >
      <select
        {...props}
        ref={native}
        id={id ? `${id}-native` : undefined}
        autoFocus={false}
        hidden
        aria-hidden="true"
        tabIndex={-1}
        onInvalid={(event) => {
          event.preventDefault()
          trigger.current?.focus()
          expand()
          props.onInvalid?.(event)
        }}
        onChange={(event) => {
          setUncontrolled(event.target.value)
          props.onChange?.(event)
        }}
      >
        {children}
      </select>
      <button
        ref={trigger}
        type="button"
        id={id}
        role="combobox"
        className="ui-select-trigger"
        aria-label={props['aria-label'] || label || undefined}
        aria-labelledby={props['aria-labelledby']}
        aria-describedby={props['aria-describedby']}
        aria-invalid={props['aria-invalid']}
        aria-required={props.required}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={`${uid}-listbox`}
        aria-activedescendant={
          open && !searchable && filtered[active] ? `${uid}-option-${active}` : undefined
        }
        disabled={props.disabled}
        onClick={() => (open ? close() : expand())}
        onKeyDown={keyboard}
      >
        <span>{chosen?.label || current || '请选择'}</span>
        <ChevronDown size={14} aria-hidden="true" />
      </button>
      <div ref={popup} popover="manual" className="ui-select-popup" onKeyDown={keyboard}>
        {searchable && (
          <div className="ui-select-search">
            <Search size={14} />
            <input
              ref={search}
              role="combobox"
              aria-label="搜索选项"
              aria-expanded={open}
              aria-controls={`${uid}-listbox`}
              aria-activedescendant={filtered[active] ? `${uid}-option-${active}` : undefined}
              value={query}
              placeholder="搜索选项…"
              onChange={(event) => {
                setQuery(event.target.value)
                setActive(0)
              }}
            />
          </div>
        )}
        <div
          role="listbox"
          id={`${uid}-listbox`}
          aria-label={props['aria-label'] || label || '选项'}
          className="ui-select-options"
        >
          {filtered.map((option, index) => (
            <div
              key={`${option.value}:${index}`}
              role="option"
              id={`${uid}-option-${index}`}
              aria-selected={option.value === current}
              aria-disabled={option.disabled}
              data-active={active === index}
              onPointerMove={() => {
                if (!option.disabled) setActive(index)
              }}
              onPointerDown={(event) => event.preventDefault()}
              onClick={() => choose(option)}
            >
              <span>{option.label}</span>
              {option.value === current && <Check size={14} />}
            </div>
          ))}
          {!filtered.length && <p className="ui-select-empty">没有匹配选项</p>}
        </div>
      </div>
    </span>
  )
}
