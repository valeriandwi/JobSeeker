import { useEffect, useRef } from 'react'
import type { ReactNode } from 'react'
import { Button } from './Button'

interface ModalProps {
  open: boolean
  title: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
}

/** Accessible modal built on the native <dialog> element. */
export function Modal({ open, title, onClose, children, footer }: ModalProps) {
  const ref = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const dialog = ref.current
    if (!dialog) return
    if (open && !dialog.open) {
      if (typeof dialog.showModal === 'function') dialog.showModal()
      else dialog.setAttribute('open', '')
    } else if (!open && dialog.open) {
      dialog.close()
    }
  }, [open])

  return (
    <dialog
      ref={ref}
      aria-label={title}
      onClose={onClose}
      onCancel={(e) => {
        e.preventDefault()
        onClose()
      }}
      className="w-full max-w-lg rounded-lg p-0 shadow-xl backdrop:bg-slate-900/40 open:block"
    >
      <div className="flex items-center justify-between border-b border-slate-200 px-4 py-3">
        <h2 className="text-base font-semibold text-slate-900">{title}</h2>
        <Button variant="ghost" aria-label="Close dialog" onClick={onClose} className="px-2 py-1">
          ✕
        </Button>
      </div>
      <div className="max-h-[70vh] overflow-y-auto px-4 py-4">{children}</div>
      {footer && <div className="flex justify-end gap-2 border-t border-slate-200 px-4 py-3">{footer}</div>}
    </dialog>
  )
}
