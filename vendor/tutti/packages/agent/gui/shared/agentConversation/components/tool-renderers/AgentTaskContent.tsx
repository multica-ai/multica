import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import {
  ToolMarkdownBlock,
  ToolSection,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { AgentTaskStepList } from "../AgentTaskStepList";
import { getTaskRenderData } from "./render-data/agentToolRenderData";
import { observer } from "mobx-react-lite";
export const AgentTaskContent = observer(AgentTaskContentRender);

function AgentTaskContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element {
  "use memo";
  const task = getTaskRenderData(call);
  const failureMarkdown =
    task.errorMarkdown ??
    (isFailedTaskStatus(call.statusKind, task.status, call.status)
      ? translate("agentHost.agentTool.details.missingFailureDetails")
      : null);

  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      <div className="workspace-agents-status-panel__detail-tool-answer">
        <strong>{task.title}</strong>
        {task.status ? (
          <span className="workspace-agents-status-panel__detail-tool-answer--muted">
            {" · "}
            {task.status}
          </span>
        ) : null}
        {task.durationText ? (
          <span className="workspace-agents-status-panel__detail-tool-answer--muted">
            {" · "}
            {task.durationText}
          </span>
        ) : null}
      </div>
      {task.status === "running" && task.latestStepSummary ? (
        <ToolSection title={translate("agentHost.agentTool.details.summary")}>
          <div className="workspace-agents-status-panel__detail-tool-answer workspace-agents-status-panel__detail-tool-answer--muted">
            {task.latestStepSummary}
          </div>
        </ToolSection>
      ) : null}
      {task.prompt ? (
        <ToolSection title={translate("agentHost.agentTool.details.prompt")}>
          <ToolMarkdownBlock content={task.prompt} onLinkClick={onLinkClick} />
        </ToolSection>
      ) : null}
      {task.childSessionId ? (
        <ToolSection
          title={translate("agentHost.agentTool.details.delegateSession")}
        >
          <div className="workspace-agents-status-panel__detail-tool-answer">
            {task.childSessionId}
          </div>
        </ToolSection>
      ) : null}
      {task.steps.length > 0 ? (
        <ToolSection title={translate("agentHost.agentTool.details.steps")}>
          <AgentTaskStepList steps={task.steps} onLinkClick={onLinkClick} />
        </ToolSection>
      ) : null}
      {task.resultMarkdown ? (
        <ToolSection title={translate("agentHost.agentTool.details.output")}>
          <ToolMarkdownBlock
            content={task.resultMarkdown}
            onLinkClick={onLinkClick}
            collapsible
          />
        </ToolSection>
      ) : null}
      {failureMarkdown ? (
        <ToolSection title={translate("agentHost.agentTool.details.error")}>
          <ToolMarkdownBlock
            content={failureMarkdown}
            onLinkClick={onLinkClick}
            collapsible
          />
        </ToolSection>
      ) : null}
    </div>
  );
}

function isFailedTaskStatus(
  ...values: Array<string | null | undefined>
): boolean {
  return values.some((value) => {
    const normalized = (value ?? "").trim().toLowerCase();
    return normalized === "failed" || normalized === "error";
  });
}
