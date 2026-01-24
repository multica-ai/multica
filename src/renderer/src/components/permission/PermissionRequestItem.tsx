/**
 * Permission request item - orchestrator component
 *
 * Routes to appropriate UI based on tool type:
 * - AskUserQuestion: Shows question UI with options
 * - ExitPlanMode: Shows plan approval UI with markdown rendering
 * - Other tools: Shows standard permission UI
 */
import { usePermissionStore } from '../../stores/permissionStore'
import { isQuestionTool, isPlanApprovalTool } from '../../../../shared/tool-names'
import { AskUserQuestionUI, CompletedAnswer } from './AskUserQuestion'
import { PlanApprovalUI } from './PlanApprovalUI'
import { StandardPermissionUI } from './StandardPermissionUI'
import type { PermissionRequestItemProps, AskUserQuestionInput } from './types'

/**
 * Safely extract plan content from rawInput
 * Handles various input formats and edge cases
 */
function extractPlanContent(rawInput: unknown): string | undefined {
  if (!rawInput || typeof rawInput !== 'object') return undefined

  const input = rawInput as Record<string, unknown>

  // Try 'plan' field first (expected format)
  if (typeof input.plan === 'string' && input.plan.trim()) {
    return input.plan
  }

  // Fallback: try 'content' field
  if (typeof input.content === 'string' && input.content.trim()) {
    return input.content
  }

  // Fallback: try 'text' field
  if (typeof input.text === 'string' && input.text.trim()) {
    return input.text
  }

  return undefined
}

export function PermissionRequestItem({ request }: PermissionRequestItemProps): React.JSX.Element {
  const currentRequest = usePermissionStore((s) => s.pendingRequests[0] ?? null)
  const currentQuestionIndex = usePermissionStore((s) => s.currentQuestionIndex)
  const getRespondedRequest = usePermissionStore((s) => s.getRespondedRequest)

  const { toolCall } = request
  const isPending = currentRequest?.requestId === request.requestId

  // Get responded data for completed requests
  const respondedData = getRespondedRequest(request.requestId)

  // Detect AskUserQuestion tool
  const isAskUserQuestion = isQuestionTool(toolCall.title)
  const rawInput = toolCall.rawInput as AskUserQuestionInput | undefined
  const questions = isAskUserQuestion ? rawInput?.questions : undefined

  // Detect ExitPlanMode tool (plan approval)
  const isPlanApproval = isPlanApprovalTool(toolCall.title)
  const planContent = isPlanApproval ? extractPlanContent(toolCall.rawInput) : undefined

  // Render AskUserQuestion UI for pending question
  if (isAskUserQuestion && questions && questions.length > 0 && isPending) {
    return (
      <AskUserQuestionUI
        request={request}
        questions={questions}
        currentQuestionIndex={currentQuestionIndex}
      />
    )
  }

  // Render completed AskUserQuestion with selected answer(s)
  if (isAskUserQuestion && questions && questions.length > 0 && !isPending) {
    return (
      <CompletedAnswer
        answers={respondedData?.response.answers}
        selectedOption={respondedData?.response.selectedOption}
        selectedOptions={respondedData?.response.selectedOptions}
        customText={respondedData?.response.customText}
        firstQuestionHeader={questions[0]?.header}
      />
    )
  }

  // Render PlanApprovalUI for ExitPlanMode with plan content
  if (isPlanApproval && planContent && isPending) {
    return <PlanApprovalUI request={request} planContent={planContent} />
  }

  // Standard permission UI
  return <StandardPermissionUI request={request} isPending={isPending} />
}
