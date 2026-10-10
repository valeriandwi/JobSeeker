import { describe, expect, it } from 'vitest'
import {
  arrangementLabel,
  arrangementTone,
  employmentTypeLabel,
  employmentTypeTone,
} from './status'

describe('work arrangement chips', () => {
  it('gives each arrangement a distinct tone', () => {
    expect(arrangementTone('remote')).toBe('info')
    expect(arrangementTone('hybrid')).toBe('warning')
    expect(arrangementTone('onsite')).toBe('neutral')
  })

  it('is case-insensitive, because sources are inconsistent', () => {
    expect(arrangementTone('Remote')).toBe('info')
    expect(arrangementLabel('ONSITE')).toBe('On-site')
  })

  it('falls back to neutral and echoes the value it does not know', () => {
    // A new source value must not imply a meaning it has not earned.
    expect(arrangementTone('field-based')).toBe('neutral')
    expect(arrangementLabel('field-based')).toBe('field-based')
    expect(arrangementTone('')).toBe('neutral')
  })

  it('always states the value in words, never colour alone', () => {
    for (const value of ['remote', 'hybrid', 'onsite']) {
      expect(arrangementLabel(value)).not.toBe('')
      expect(arrangementLabel(value)).toMatch(/^[A-Z]/)
    }
  })
})

describe('employment type chips', () => {
  it('tones the documented employment types', () => {
    expect(employmentTypeTone('full_time')).toBe('success')
    expect(employmentTypeTone('permanent')).toBe('success')
    expect(employmentTypeTone('part_time')).toBe('info')
    expect(employmentTypeTone('contract')).toBe('warning')
    expect(employmentTypeTone('temporary')).toBe('warning')
    expect(employmentTypeTone('internship')).toBe('brand')
  })

  it('prettifies the wire format for display', () => {
    expect(employmentTypeLabel('full_time')).toBe('Full time')
    expect(employmentTypeLabel('part_time')).toBe('Part time')
    expect(employmentTypeLabel('internship')).toBe('Internship')
  })

  it('passes through anything unrecognised, neutral', () => {
    expect(employmentTypeTone('volunteer')).toBe('neutral')
    expect(employmentTypeLabel('volunteer')).toBe('volunteer')
  })
})
