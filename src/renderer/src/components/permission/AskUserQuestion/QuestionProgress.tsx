/**
 * Question progress indicator for multi-question flows
 */
import { cn } from '@/lib/utils'
import type { QuestionProgressProps } from '../types'

export function QuestionProgress({
  currentIndex,
  totalQuestions
}: QuestionProgressProps): React.JSX.Element {
  return (
    <div className="flex items-center gap-2 text-xs text-muted-foreground">
      <span>
        Question {currentIndex + 1} of {totalQuestions}
      </span>
      <div className="flex gap-1">
        {Array.from({ length: totalQuestions }).map((_, idx) => (
          <span
            key={idx}
            className={cn(
              'h-1.5 w-1.5 rounded-full',
              idx < currentIndex
                ? 'bg-[var(--tool-success)]' // Answered
                : idx === currentIndex
                  ? 'bg-primary' // Current
                  : 'bg-muted-foreground/30' // Pending
            )}
          />
        ))}
      </div>
    </div>
  )
}
