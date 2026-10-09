import { scoreTier } from '../lib/format'
import { Badge } from './ui/Badge'

const tierTone = {
  high: 'success',
  medium: 'warning',
  low: 'danger',
  none: 'neutral',
} as const

const tierLabel = {
  high: 'High match',
  medium: 'Medium match',
  low: 'Low match',
  none: 'No score',
} as const

/** Score badge with documented thresholds. Renders an explicit marker when score is absent. */
export function ScoreBadge({ score }: { score: number | null | undefined }) {
  const tier = scoreTier(score)
  const display = score == null ? '—' : Math.round(score)
  return (
    <Badge tone={tierTone[tier]} title={tierLabel[tier]}>
      <span aria-hidden="true">●</span>
      <span>{`Match ${display}`}</span>
      <span className="sr-only"> {tierLabel[tier]}</span>
    </Badge>
  )
}
