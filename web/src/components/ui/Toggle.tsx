import { useId } from 'react'

interface ToggleProps {
  checked: boolean
  onChange: (checked: boolean) => void
  label: string
  disabled?: boolean
}

/** Accessible checkbox rendered as a switch. */
export function Toggle({ checked, onChange, label, disabled }: ToggleProps) {
  const id = useId()
  return (
    <span className="inline-flex items-center gap-2">
      <input
        id={id}
        type="checkbox"
        role="switch"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
        className="h-4 w-4 rounded border-slate-300 text-brand-600 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-brand-600"
      />
      <label htmlFor={id} className="text-sm text-slate-700">
        {label}
      </label>
    </span>
  )
}
