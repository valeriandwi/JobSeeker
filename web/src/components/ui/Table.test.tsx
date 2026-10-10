import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Table } from './Table'

describe('Table', () => {
  it('scrolls horizontally instead of squeezing wide tables', () => {
    const { container } = render(
      <Table caption="Jobs" head={['Score', 'Title', 'Description']} minWidthClassName="min-w-[1120px]">
        <tr>
          <td>88</td>
          <td>Engineer</td>
          <td>Build things.</td>
        </tr>
      </Table>,
    )

    // The scroll container is the table's parent, so the columns keep their
    // width and the overflow is scrollable rather than compressed.
    const scroll = container.firstElementChild as HTMLElement
    expect(scroll.className).toContain('overflow-x-auto')

    const table = screen.getByRole('table')
    expect(table.className).toContain('min-w-[1120px]')
    // Headers must not wrap once the table is at its minimum width.
    for (const th of screen.getAllByRole('columnheader')) {
      expect(th.className).toContain('whitespace-nowrap')
    }
  })

  it('keeps a default minimum width for narrow tables', () => {
    render(
      <Table head={['A']}>
        <tr>
          <td>1</td>
        </tr>
      </Table>,
    )
    expect(screen.getByRole('table').className).toContain('min-w-[640px]')
  })

  it('exposes the caption to assistive technology', () => {
    render(
      <Table caption="Job search results" head={['A']}>
        <tr>
          <td>1</td>
        </tr>
      </Table>,
    )
    expect(screen.getByRole('table', { name: 'Job search results' })).toBeInTheDocument()
  })
})
