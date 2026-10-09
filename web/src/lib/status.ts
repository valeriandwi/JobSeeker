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
