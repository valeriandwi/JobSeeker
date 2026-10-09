import { UNKNOWN_LABEL, presentOrNull } from '../lib/format'
import { Badge } from './ui/Badge'

/**
 * Renders the explicit "Unknown" chip when an attribute is absent, otherwise the
 * value. Never renders an empty cell and never implies a value the data lacks.
 */
export function UnknownChip({ value, fallback = UNKNOWN_LABEL }: { value: unknown; fallback?: string }) {
  const present = presentOrNull(value == null ? null : String(value))
  if (present === null) {
    return (
      <Badge tone="neutral" title="This attribute is not available in the source data.">
        {fallback}
      </Badge>
    )
  }
  return <span>{present}</span>
}
