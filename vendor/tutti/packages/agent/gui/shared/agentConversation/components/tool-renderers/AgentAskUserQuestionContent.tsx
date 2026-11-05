import type { JSX } from "react";
import { MessageSquareMoreIcon } from "../../../../app/renderer/components/icons/MessageSquareMoreIcon";
import { translate } from "../../../../i18n/index";
import {
  ToolMarkdownBlock,
  ToolSection,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { observer } from "mobx-react-lite";
export const AgentAskUserQuestionContent = observer(
  AgentAskUserQuestionContentRender
);

function AgentAskUserQuestionContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const questions = call.askUserQuestion?.questions ?? [];
  const outputText = stringValue(call.output?.text);
  if (questions.length === 0) {
    return null;
  }

  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      <ToolSection title={translate("agentHost.agentTool.details.questions")}>
        <div className="workspace-agents-status-panel__detail-tool-stack">
          {questions.map((question) => {
            const body =
              question.question ||
              question.header ||
              translate("agentHost.agentTool.details.questionFallback");
            const answer = formatAnswer(question.answer);
            const questionKey =
              question.id.trim() ||
              `${question.header}:${question.question}:${question.options.map((option) => option.label).join("|")}`;
            return (
              <div
                key={questionKey}
                className="workspace-agents-status-panel__detail-tool-stack-item"
              >
                <div className="workspace-agents-status-panel__detail-tool-question">
                  {body}
                </div>
                {question.options.length > 0 ? (
                  <div className="workspace-agents-status-panel__detail-tool-stack">
                    {question.options.map((option) => (
                      <div
                        key={`${questionKey}-option-${option.label}:${option.description}`}
                        className="workspace-agents-status-panel__detail-tool-stack-item"
                      >
                        <div className="workspace-agents-status-panel__detail-tool-summary">
                          {option.label}
                        </div>
                        {option.description ? (
                          <div className="workspace-agents-status-panel__detail-tool-answer workspace-agents-status-panel__detail-tool-answer--muted">
                            {option.description}
                          </div>
                        ) : null}
                      </div>
                    ))}
                  </div>
                ) : null}
                {answer ? (
                  <div className="workspace-agents-status-panel__detail-tool-answer">
                    {translate("agentHost.agentTool.details.answerPrefix", {
                      answer
                    })}
                  </div>
                ) : (
                  <div className="workspace-agents-status-panel__detail-tool-answer workspace-agents-status-panel__detail-tool-answer--muted inline-flex items-center gap-1.5">
                    <MessageSquareMoreIcon
                      size={14}
                      active
                      aria-hidden="true"
                      className="shrink-0"
                    />
                    {translate("agentHost.agentTool.details.waitingForAnswer")}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      </ToolSection>
      {outputText ? (
        <ToolSection title={translate("agentHost.agentTool.details.output")}>
          <ToolMarkdownBlock
            content={outputText}
            onLinkClick={onLinkClick}
            collapsible
          />
        </ToolSection>
      ) : null}
    </div>
  );
}

function formatAnswer(
  answer: string | string[] | null | undefined
): string | null {
  if (Array.isArray(answer)) {
    return answer.filter((value) => value.trim()).join(", ") || null;
  }
  return typeof answer === "string" && answer.trim() ? answer.trim() : null;
}

function stringValue(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}
