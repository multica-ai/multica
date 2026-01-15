/**
 * AskUserQuestion UI - main container for question flows
 */
import { useState, useEffect } from 'react'
import { usePermissionStore } from '../../../stores/permissionStore'
import { Button } from '@/components/ui/button'
import { QuestionProgress } from './QuestionProgress'
import { QuestionOptions } from './QuestionOptions'
import { CustomInput } from './CustomInput'
import type { AskUserQuestionUIProps } from '../types'

// Common patterns for "Other" option that requires custom input
const OTHER_PATTERNS = ['other', '其他', '另外']

// Check if an option label is an "Other" type option
const isOtherOption = (label: string): boolean =>
  OTHER_PATTERNS.some((pattern) => label.toLowerCase().includes(pattern))

export function AskUserQuestionUI({
  request,
  questions,
  currentQuestionIndex
}: AskUserQuestionUIProps) {
  const respondToRequest = usePermissionStore((s) => s.respondToRequest)
  const answerCurrentQuestion = usePermissionStore((s) => s.answerCurrentQuestion)
  const [customInput, setCustomInput] = useState('')
  const [selectedOptions, setSelectedOptions] = useState<string[]>([])

  const { options } = request
  const totalQuestions = questions.length
  const isMultiQuestion = totalQuestions > 1
  const currentQuestion = questions[currentQuestionIndex]
  const isMultiSelect = currentQuestion?.multiSelect ?? false

  // Find allow option (used for responding)
  const allowOption =
    options.find((o) => o.kind === 'allow_once') ||
    options.find((o) => o.kind === 'allow') ||
    options[0]

  // Check if "Other" option is selected (requires custom input)
  const hasOtherSelected = selectedOptions.some(isOtherOption)

  // Reset local state when question changes (for multi-question flow)
  useEffect(() => {
    setCustomInput('')
    setSelectedOptions([])
  }, [currentQuestionIndex])

  // Handle custom input submission
  const handleCustomSubmit = () => {
    if (customInput.trim()) {
      if (questions.length > 0) {
        answerCurrentQuestion(customInput.trim(), true)
      } else {
        respondToRequest(allowOption.optionId, { customText: customInput.trim() })
      }
    }
  }

  // Toggle option selection for multi-select mode
  const toggleOption = (label: string) => {
    setSelectedOptions((prev) =>
      prev.includes(label) ? prev.filter((l) => l !== label) : [...prev, label]
    )
  }

  // Submit multi-select selections (including custom input)
  const handleMultiSelectSubmit = () => {
    // Build final options list
    let finalOptions = [...selectedOptions]

    // If "Other" is selected and has custom input, replace "Other" with custom text
    if (hasOtherSelected && customInput.trim()) {
      finalOptions = finalOptions.map((opt) => (isOtherOption(opt) ? customInput.trim() : opt))
    }
    // If custom input is filled but "Other" is not selected, add custom input as an option
    else if (customInput.trim() && !hasOtherSelected) {
      finalOptions.push(customInput.trim())
    }

    // Submit if we have any options (selected or custom)
    if (finalOptions.length > 0) {
      if (questions.length > 0) {
        answerCurrentQuestion(finalOptions.join(', '), false)
      } else {
        respondToRequest(allowOption.optionId, { selectedOptions: finalOptions })
      }
    }
  }

  // Handle single option click
  const handleOptionClick = (optionLabel: string) => {
    if (isMultiSelect) {
      toggleOption(optionLabel)
    } else {
      // Single select - immediately answer
      if (questions.length > 0) {
        answerCurrentQuestion(optionLabel, false)
      } else {
        respondToRequest(allowOption.optionId, { selectedOption: optionLabel })
      }
    }
  }

  if (!currentQuestion) return null

  return (
    <div className="rounded-lg border border-border bg-muted/30 p-3 space-y-3">
      {/* Progress indicator for multi-question */}
      {isMultiQuestion && (
        <QuestionProgress currentIndex={currentQuestionIndex} totalQuestions={totalQuestions} />
      )}

      {/* Question header */}
      {currentQuestion.header && (
        <div className="font-medium text-sm text-foreground">{currentQuestion.header}</div>
      )}

      {/* Question text */}
      <div className="text-sm text-foreground">
        {currentQuestion.question}
        {isMultiSelect && (
          <span className="text-xs text-muted-foreground ml-2">(Select multiple)</span>
        )}
      </div>

      {/* Question options */}
      <QuestionOptions
        options={currentQuestion.options}
        isMultiSelect={isMultiSelect}
        selectedOptions={selectedOptions}
        onOptionClick={handleOptionClick}
      />

      {/* Custom input */}
      <CustomInput
        value={customInput}
        onChange={setCustomInput}
        onSubmit={handleCustomSubmit}
        isMultiSelect={isMultiSelect}
      />

      {/* Submit button for multi-select (after custom input) */}
      {isMultiSelect &&
        (() => {
          const hasCustom = customInput.trim().length > 0
          const totalCount = selectedOptions.length + (hasCustom && !hasOtherSelected ? 1 : 0)
          const canSubmit = totalCount > 0 && !(hasOtherSelected && !hasCustom)

          return (
            <Button
              size="sm"
              onClick={handleMultiSelectSubmit}
              disabled={!canSubmit}
              className="w-full"
            >
              {hasOtherSelected && !hasCustom
                ? 'Please enter custom text above'
                : `Submit (${totalCount} selected)`}
            </Button>
          )
        })()}
    </div>
  )
}
