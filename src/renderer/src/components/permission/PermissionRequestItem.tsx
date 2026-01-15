/**
 * Permission request item - orchestrator component
 *
 * Routes to appropriate UI based on tool type:
 * - AskUserQuestion: Shows question UI with options
 * - Other tools: Shows standard permission UI
 */
import { usePermissionStore } from '../../stores/permissionStore'
import { isQuestionTool } from '../../../../shared/tool-names'
import { AskUserQuestionUI, CompletedAnswer } from './AskUserQuestion'
import { StandardPermissionUI } from './StandardPermissionUI'
import type { PermissionRequestItemProps, AskUserQuestionInput } from './types'

export function PermissionRequestItem({ request }: PermissionRequestItemProps) {
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

  // Standard permission UI
  return <StandardPermissionUI request={request} isPending={isPending} />
}
