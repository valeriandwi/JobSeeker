import { useState } from 'react'
import type { KeyboardEvent } from 'react'

interface ChipInputProps {
  values: string[]
  onChange: (values: string[]) => void
  label: string
  placeholder?: string
}

/** Free-text chip editor: type and press Enter (or comma) to add, click ✕ to remove. */
export function ChipInput({ values, onChange, label, placeholder }: ChipInputProps) {
  const [draft, setDraft] = useState('')

  const add = (raw: string) => {
    const parts = raw
      .split(',')
      .map((v) => v.trim())
      .filter(Boolean)
    if (parts.length === 0) return
    const next = [...values]
    for (const p of parts) if (!next.includes(p)) next.push(p)
    onChange(next)
    setDraft('')
  }

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      add(draft)
    } else if (e.key === 'Backspace' && draft === '' && values.length > 0) {
      onChange(values.slice(0, -1))
    }
  }

  return (
    <div className="flex flex-col gap-1">
      <span className="text-sm font-medium text-slate-700">{label}</span>
      <div className="flex flex-wrap items-center gap-1 rounded-md border border-slate-300 p-2 focus-within:outline focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-brand-600">
        {values.map((v) => (
          <span
            key={v}
            className="inline-flex items-center gap-1 rounded-full bg-brand-50 px-2 py-0.5 text-xs text-brand-700"
          >
            {v}
            <button
              type="button"
              aria-label={`Remove ${v}`}
              onClick={() => onChange(values.filter((x) => x !== v))}
              className="text-brand-700 hover:text-brand-900"
            >
              ✕
            </button>
          </span>
        ))}
        <input
          type="text"
          value={draft}
          placeholder={placeholder ?? 'Add and press Enter'}
          aria-label={`${label} input`}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={() => add(draft)}
          className="min-w-[8rem] flex-1 border-0 text-sm focus:outline-none"
        />
      </div>
    </div>
  )
}
