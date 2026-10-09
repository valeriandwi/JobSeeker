import type { ReactNode } from 'react'

interface TableProps {
  caption?: string
  head: ReactNode[]
  children: ReactNode
}

/** Semantic, scrollable data table. */
export function Table({ caption, head, children }: TableProps) {
  return (
    <div className="w-full overflow-x-auto">
      <table className="w-full min-w-[640px] border-collapse text-left text-sm">
        {caption && <caption className="sr-only">{caption}</caption>}
        <thead>
          <tr className="border-b border-slate-200 text-xs uppercase tracking-wide text-slate-500">
            {head.map((cell, i) => (
              <th key={i} scope="col" className="px-3 py-2 font-medium">
                {cell}
              </th>
            ))}
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">{children}</tbody>
      </table>
    </div>
  )
}
