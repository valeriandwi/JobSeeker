import { describe, expect, it } from 'vitest'
import { filtersToParams, filtersToQuery, parseFilters, FILTER_KEYS } from './filters'
import type { JobFilters } from '../api/types'

describe('filter serialization', () => {
  it('serializes only the documented query params and drops empty values', () => {
    const filters: JobFilters = {
      q: 'engineer',
      country: 'DE',
      city: '',
      arrangement: 'remote',
      employment_type: 'full_time',
      seniority: '',
      source: 'greenhouse',
      min_score: '60',
      status: 'new',
      sort: 'score',
      page: '2',
      per_page: '20',
    }
    const query = filtersToQuery(filters)
    expect(query).toEqual({
      q: 'engineer',
      country: 'DE',
      arrangement: 'remote',
      employment_type: 'full_time',
      source: 'greenhouse',
      min_score: '60',
      status: 'new',
      sort: 'score',
      page: '2',
      per_page: '20',
    })
    expect(Object.keys(query).sort()).toEqual(
      ['q', 'country', 'arrangement', 'employment_type', 'source', 'min_score', 'status', 'sort', 'page', 'per_page'].sort(),
    )
  })

  it('produces a URLSearchParams in documented key order', () => {
    const params = filtersToParams({ q: 'dev', arrangement: 'hybrid' })
    expect(params.toString()).toBe('q=dev&arrangement=hybrid')
    for (const key of params.keys()) {
      expect(FILTER_KEYS).toContain(key)
    }
  })

  it('round-trips through parseFilters', () => {
    const original: JobFilters = { q: 'data', min_score: '70', sort: 'salary', page: '3' }
    const params = filtersToParams(original)
    expect(parseFilters(params)).toEqual(original)
  })

  it('ignores unknown query params when parsing', () => {
    const params = new URLSearchParams('q=test&bogus=1&domain=ignored')
    expect(parseFilters(params)).toEqual({ q: 'test' })
  })

  it('drops empty strings when parsing', () => {
    const params = new URLSearchParams('q=&country=FR')
    expect(parseFilters(params)).toEqual({ country: 'FR' })
  })
})
