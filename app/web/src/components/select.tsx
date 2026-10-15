import { Select as AntSelect } from 'antd'
import {
  Children,
  Fragment,
  isValidElement,
  useEffect,
  useRef,
  useState,
  type ChangeEvent,
  type KeyboardEvent,
  type ReactNode,
  type SelectHTMLAttributes,
} from 'react'
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

// Keep the native select for form submission and the existing change event API.
// Ant Design owns the visible control, search, keyboard behavior and popup.
export function Select({
  children,
  className = '',
  id,
  autoFocus,
  style,
  ...props
}: SelectHTMLAttributes<HTMLSelectElement>) {
  const options = readOptions(children)
  const native = useRef<HTMLSelectElement>(null)
  const root = useRef<HTMLSpanElement>(null)
  const [uncontrolled, setUncontrolled] = useState(
    String(props.defaultValue ?? options[0]?.value ?? ''),
  )
  const [invalid, setInvalid] = useState(false)
  // `open` is controlled only so Home/End can close the dropdown after committing.
  // Without that, rc-select keeps its stale highlighted option and the Enter that
  // normally follows re-selects the old value, silently undoing the jump.
  const [open, setOpen] = useState(false)
  const [caption, setCaption] = useState<string>()
  const current = props.value === undefined ? uncontrolled : String(props.value)

  // Call sites normally write `<label><span>题注</span><Select /></label>` and rely
  // on implicit label association. That does not work for a composite widget: a
  // <label> derives its name from ALL of its text content, so the combobox ends up
  // named "任务范围 全部 down" — caption plus the selected value plus the arrow
  // icon. Derive the caption ourselves by cloning the wrapping label and dropping
  // this bridge, which yields just "任务范围". Only used when the call site did not
  // supply an explicit aria-label / aria-labelledby.
  useEffect(() => {
    if (props['aria-label'] || props['aria-labelledby']) return
    const label = root.current?.closest('label')
    if (!label) return
    const clone = label.cloneNode(true) as HTMLElement
    clone.querySelectorAll('.ds-select-bridge').forEach((node) => node.remove())
    const text = clone.textContent?.replace(/\s+/g, ' ').trim()
    if (text) setCaption(text)
  }, [props['aria-label'], props['aria-labelledby']])

  const choose = (value: string) => {
    if (!native.current) return
    native.current.value = value
    setUncontrolled(value)
    setInvalid(false)
    props.onChange?.({
      target: native.current,
      currentTarget: native.current,
    } as ChangeEvent<HTMLSelectElement>)
  }

  // Forward key events to the caller. rc-select itself handles arrows, Enter,
  // Escape, Tab and typeahead.
  //
  // Home/End are deliberately NOT emulated. The native <select> this bridge
  // replaces supported them, but rc-select does not, and faking them by committing
  // a value is wrong: the WAI-ARIA listbox pattern has Home/End move the
  // *highlighted* option with Enter committing afterwards. rc-select keeps no
  // externally-settable active index and does not resync it to `value` while the
  // dropdown is open, so an immediate commit is then undone by the Enter that
  // follows. Emulating it properly would need a close/reopen flicker, which is
  // worse than the missing shortcut.
  const onKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    props.onKeyDown?.(event as unknown as KeyboardEvent<HTMLSelectElement>)
  }

  return (
    <span ref={root} className={`ds-select-bridge ${className}`} style={style}>
      {/* Order matters: a wrapping <label> binds to its FIRST labelable descendant.
          The hidden native <select> below is labelable even though it is
          `hidden`/`aria-hidden`, so if it came first it would steal the label and
          leave the visible combobox with no accessible name. Rendering the Ant
          Design control first makes the label bind to its `input[role=combobox]`,
          which is what call sites that rely on `<label><Select/></label>` expect. */}
      <AntSelect<string>
        id={id}
        aria-label={props['aria-label'] ?? caption}
        aria-labelledby={props['aria-labelledby']}
        aria-describedby={props['aria-describedby']}
        aria-required={props.required}
        aria-invalid={props['aria-invalid']}
        autoFocus={autoFocus}
        disabled={props.disabled}
        status={invalid || props['aria-invalid'] ? 'error' : undefined}
        className="ds-select-control"
        value={current}
        options={options}
        open={open}
        onOpenChange={setOpen}
        showSearch={options.length > 7}
        optionFilterProp="label"
        onChange={choose}
        onKeyDown={onKeyDown}
        getPopupContainer={() => root.current || document.body}
        notFoundContent="没有匹配选项"
      />
      <select
        {...props}
        ref={native}
        id={id ? `${id}-native` : undefined}
        hidden
        aria-hidden="true"
        tabIndex={-1}
        onInvalid={(event) => {
          event.preventDefault()
          setInvalid(true)
          // Focus alone does not open an Ant Design Select (it opens on click), so a
          // failed required check used to leave the user staring at a closed control.
          // Open it as well as focusing it.
          setOpen(true)
          root.current?.querySelector<HTMLInputElement>('.ant-select input')?.focus()
          props.onInvalid?.(event)
        }}
        onChange={(event) => {
          setUncontrolled(event.target.value)
          props.onChange?.(event)
        }}
      >
        {children}
      </select>
    </span>
  )
}
