/**
 * Completed answer display for answered questions
 */
import { CheckCircle2 } from 'lucide-react'
import type { CompletedAnswerProps } from '../types'

export function CompletedAnswer({
  answers,
  selectedOption,
  selectedOptions,
  customText,
  firstQuestionHeader
}: CompletedAnswerProps): React.JSX.Element {
  // Multi-question: simply show "Answered"
  if (answers && answers.length > 1) {
    return (
      <div className="flex items-center gap-2 text-sm text-muted-foreground py-1">
        <CheckCircle2 className="h-3.5 w-3.5 text-[var(--tool-success)] flex-shrink-0" />
        <span className="text-secondary-foreground">Answered</span>
      </div>
    )
  }

  // Single question or backward compatibility
  const selectedAnswer =
    answers?.[0]?.answer || selectedOptions?.join(', ') || selectedOption || customText

  return (
    <div className="flex items-center gap-2 text-sm text-muted-foreground py-1">
      <CheckCircle2 className="h-3.5 w-3.5 text-[var(--tool-success)] flex-shrink-0" />
      <span className="text-secondary-foreground">
        {firstQuestionHeader ? `${firstQuestionHeader}: ` : ''}
        {selectedAnswer || 'Answered'}
      </span>
    </div>
  )
}
