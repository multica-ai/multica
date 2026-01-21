/**
 * Spinner - 3x3 grid spinner based on SpinKit Grid
 *
 * Features:
 * - Uses currentColor (inherits text color from parent, typically theme primary)
 * - Uses em sizing (scales with font-size)
 * - 3x3 grid of cubes with staggered scale animation
 * - Pure CSS animation (no JS state)
 *
 * Usage:
 *   <Spinner className="text-primary" />           // Uses primary theme color
 *   <Spinner className="text-muted-foreground" />  // Uses muted color
 *   <Spinner className="text-xs" />                // Controls size via Tailwind font-size
 */
import { cn } from '@/lib/utils'

export interface SpinnerProps {
  /** Additional className for styling (color via text-*, size via Tailwind text-*) */
  className?: string
}

export function Spinner({ className }: SpinnerProps): React.JSX.Element {
  return (
    <span className={cn('spinner', className)} role="status" aria-label="Loading">
      <span className="spinner-cube" />
      <span className="spinner-cube" />
      <span className="spinner-cube" />
      <span className="spinner-cube" />
      <span className="spinner-cube" />
      <span className="spinner-cube" />
      <span className="spinner-cube" />
      <span className="spinner-cube" />
      <span className="spinner-cube" />
    </span>
  )
}
