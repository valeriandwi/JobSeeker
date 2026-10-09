import type { ReactNode } from 'react'

type Tone = 'neutral' | 'brand' | 'success' | 'warning' | 'danger' | 'info'

const tones: Record<Tone, string> = {
  neutral: 'bg-slate-100 text-slate-700 border-slate-200',
  brand: 'bg-brand-50 text-brand-700 border-brand-100',
  success: 'bg-green-50 text-green-700 border-green-200',
  warning: 'bg-amber-50 text-amber-800 border-amber-200',
  danger: 'bg-red-50 text-red-700 border-red-200',
  info: 'bg-blue-50 text-blue-700 border-blue-200',
}

interface BadgeProps {
  tone?: Tone
  children: ReactNode
  title?: string
  className?: string
}

export function Badge({ tone = 'neutral', children, title, className = '' }: BadgeProps) {
  return (
    <span
      title={title}
      className={`inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium ${tones[tone]} ${className}`}
    >
      {children}
    </span>
  )
}
