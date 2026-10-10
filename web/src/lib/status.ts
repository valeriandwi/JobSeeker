import type { RunStatus } from '../api/types'

type Tone = 'neutral' | 'brand' | 'success' | 'warning' | 'danger' | 'info'

/** Distinguishes failed vs partial clearly, per the run status vocabulary. */
export function runStatusTone(status: RunStatus | string): Tone {
  switch (status) {
    case 'succeeded':
      return 'success'
    case 'partial':
      return 'warning'
    case 'failed':
      return 'danger'
    case 'aborted':
      return 'danger'
    case 'running':
      return 'info'
    default:
      return 'neutral'
  }
}

export function runStatusLabel(status: RunStatus | string): string {
  switch (status) {
    case 'succeeded':
      return 'Succeeded'
    case 'partial':
      return 'Partial (some sources failed)'
    case 'failed':
      return 'Failed'
    case 'aborted':
      return 'Aborted'
    case 'running':
      return 'Running'
    default:
      return status
  }
}

export const APPLICATION_STATUSES = [
  'saved',
  'applied',
  'interviewing',
  'rejected',
  'accepted',
  'withdrawn',
] as const

export function applicationStatusTone(status: string): Tone {
  switch (status) {
    case 'accepted':
      return 'success'
    case 'interviewing':
      return 'info'
    case 'rejected':
      return 'danger'
    case 'withdrawn':
      return 'neutral'
    case 'applied':
      return 'brand'
    default:
      return 'neutral'
  }
}

/*
 * Work arrangement and employment type are FACTS about the vacancy, not
 * judgements, so the colour is only a scanning aid — the label always states the
 * value, and colour is never the sole signal. Neutral is the default for
 * anything unrecognised, so a new source value cannot imply a meaning.
 */

export function arrangementTone(arrangement: string): Tone {
  switch ((arrangement ?? '').toLowerCase()) {
    case 'remote':
      return 'info'
    case 'hybrid':
      return 'warning'
    case 'onsite':
      return 'neutral'
    default:
      return 'neutral'
  }
}

export function arrangementLabel(arrangement: string): string {
  switch ((arrangement ?? '').toLowerCase()) {
    case 'remote':
      return 'Remote'
    case 'hybrid':
      return 'Hybrid'
    case 'onsite':
      return 'On-site'
    default:
      return arrangement
  }
}

export function employmentTypeTone(employmentType: string): Tone {
  switch ((employmentType ?? '').toLowerCase()) {
    case 'full_time':
    case 'permanent':
      return 'success'
    case 'part_time':
      return 'info'
    case 'contract':
    case 'temporary':
      return 'warning'
    case 'internship':
      return 'brand'
    default:
      return 'neutral'
  }
}

export function employmentTypeLabel(employmentType: string): string {
  switch ((employmentType ?? '').toLowerCase()) {
    case 'full_time':
      return 'Full time'
    case 'part_time':
      return 'Part time'
    case 'contract':
      return 'Contract'
    case 'temporary':
      return 'Temporary'
    case 'permanent':
      return 'Permanent'
    case 'internship':
      return 'Internship'
    default:
      return employmentType
  }
}
