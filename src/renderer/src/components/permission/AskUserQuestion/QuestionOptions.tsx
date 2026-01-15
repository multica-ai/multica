/**
 * Question options with single/multi-select support
 */
import { Check } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { QuestionOptionsProps } from '../types'

export function QuestionOptions({
  options,
  isMultiSelect,
  selectedOptions,
  onOptionClick
}: QuestionOptionsProps) {
  return (
    <div className="flex flex-col gap-2">
      {options.map((opt, idx) => {
        const isSelected = selectedOptions.includes(opt.label)

        return (
          <Button
            key={idx}
            variant="outline"
            className={cn(
              'justify-start h-auto py-2 px-3 text-left',
              isMultiSelect && isSelected && 'border-primary bg-primary/10'
            )}
            onClick={() => onOptionClick(opt.label)}
          >
            <div className="flex items-start gap-2 w-full">
              {/* Checkbox indicator for multi-select */}
              {isMultiSelect && (
                <div
                  className={cn(
                    'h-4 w-4 rounded border flex-shrink-0 mt-0.5 flex items-center justify-center',
                    isSelected ? 'bg-primary border-primary' : 'border-muted-foreground/40'
                  )}
                >
                  {isSelected && <Check className="h-3 w-3 text-primary-foreground" />}
                </div>
              )}
              <div className="flex-1">
                <div className="font-medium text-sm">{opt.label}</div>
                {opt.description && (
                  <div className="text-xs text-muted-foreground mt-0.5">{opt.description}</div>
                )}
              </div>
            </div>
          </Button>
        )
      })}
    </div>
  )
}
