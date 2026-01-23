/**
 * Permission component shared types
 */
import type { PermissionRequest } from '../../../../shared/electron-api'

// AskUserQuestion rawInput structure
export interface QuestionOption {
  label: string
  description?: string
}

export interface Question {
  question: string
  header?: string
  options: QuestionOption[]
  multiSelect?: boolean
}

export interface AskUserQuestionInput {
  questions?: Question[]
}

// Component props
export interface PermissionRequestItemProps {
  request: PermissionRequest
}

export interface QuestionProgressProps {
  currentIndex: number
  totalQuestions: number
}

export interface QuestionOptionsProps {
  options: QuestionOption[]
  isMultiSelect: boolean
  selectedOptions: string[]
  onOptionClick: (label: string) => void
}

export interface CustomInputProps {
  value: string
  onChange: (value: string) => void
  onSubmit: () => void
  /** Hide Send button in multi-select mode (user should use Submit button instead) */
  isMultiSelect?: boolean
}

export interface CompletedAnswerProps {
  answers?: Array<{ question: string; answer: string }>
  selectedOption?: string
  selectedOptions?: string[]
  customText?: string
  firstQuestionHeader?: string
}

export interface AskUserQuestionUIProps {
  request: PermissionRequest
  questions: Question[]
  currentQuestionIndex: number
}

export interface StandardPermissionUIProps {
  request: PermissionRequest
  isPending: boolean
}

// PlanApproval rawInput structure
export interface PlanApprovalInput {
  plan?: string
  allowedPrompts?: Array<{ tool: string; prompt: string }>
  launchSwarm?: boolean
  pushToRemote?: boolean
}

export interface PlanApprovalUIProps {
  request: PermissionRequest
  planContent: string
}
