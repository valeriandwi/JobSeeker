import { useId } from 'react'
import type { InputHTMLAttributes, ReactNode, SelectHTMLAttributes, TextareaHTMLAttributes } from 'react'

interface FieldProps {
  label: string
  error?: string | undefined
  hint?: string
  children: (id: string, describedBy: string | undefined, invalid: boolean) => ReactNode
}

/** Wraps a labelled control, linking label, hint and error via ARIA. */
export function Field({ label, error, hint, children }: FieldProps) {
  const id = useId()
  const errorId = `${id}-error`
  const hintId = `${id}-hint`
  const describedBy = [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(' ') || undefined
  return (
    <div className="flex flex-col gap-1">
      <label htmlFor={id} className="text-sm font-medium text-slate-700">
        {label}
      </label>
      {children(id, describedBy, Boolean(error))}
      {hint && (
        <p id={hintId} className="text-xs text-slate-500">
          {hint}
        </p>
      )}
      {error && (
        <p id={errorId} className="text-xs font-medium text-red-600" role="alert">
          {error}
        </p>
      )}
    </div>
  )
}

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string
  error?: string
  hint?: string
}

const inputClass = (invalid: boolean, extra = '') =>
  `w-full rounded-md border px-3 py-2 text-sm text-slate-900 shadow-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 ${
    invalid ? 'border-red-400 focus-visible:outline-red-500' : 'border-slate-300 focus-visible:outline-brand-600'
  } ${extra}`

export function Input({ label, error, hint, id, className = '', ...rest }: InputProps) {
  if (!label) {
    return <input id={id} className={inputClass(Boolean(error), className)} aria-invalid={Boolean(error)} {...rest} />
  }
  return (
    <Field label={label} error={error} hint={hint}>
      {(fid, describedBy, invalid) => (
        <input
          id={fid}
          className={inputClass(invalid, className)}
          aria-invalid={invalid}
          aria-describedby={describedBy}
          {...rest}
        />
      )}
    </Field>
  )
}

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string
  error?: string
}

export function Select({ label, error, id, className = '', children, ...rest }: SelectProps) {
  if (!label) {
    return (
      <select id={id} className={inputClass(Boolean(error), className)} aria-invalid={Boolean(error)} {...rest}>
        {children}
      </select>
    )
  }
  return (
    <Field label={label} error={error}>
      {(fid, describedBy, invalid) => (
        <select
          id={fid}
          className={inputClass(invalid, className)}
          aria-invalid={invalid}
          aria-describedby={describedBy}
          {...rest}
        >
          {children}
        </select>
      )}
    </Field>
  )
}

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string
  error?: string
  hint?: string
}

export function Textarea({ label, error, hint, className = '', ...rest }: TextareaProps) {
  if (!label) {
    return <textarea className={inputClass(Boolean(error), className)} aria-invalid={Boolean(error)} {...rest} />
  }
  return (
    <Field label={label} error={error} hint={hint}>
      {(fid, describedBy, invalid) => (
        <textarea
          id={fid}
          className={inputClass(invalid, className)}
          aria-invalid={invalid}
          aria-describedby={describedBy}
          {...rest}
        />
      )}
    </Field>
  )
}
