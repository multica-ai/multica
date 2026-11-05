import type { JSX } from "react";
import { CheckCircle2, Circle } from "lucide-react";
import { AtomIcon } from "../../../../app/renderer/components/icons/AtomIcon";
import { translate } from "../../../../i18n/index";
import {
  ToolSection,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { getTodoRenderData } from "./render-data/agentToolRenderData";
import { observer } from "mobx-react-lite";
export const AgentTodoWriteContent = observer(AgentTodoWriteContentRender);

function AgentTodoWriteContentRender({
  call
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const todos = getTodoRenderData(call);
  if (todos.length === 0) {
    return null;
  }
  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      <ToolSection title={translate("agentHost.agentTool.details.todos")}>
        <div className="space-y-1">
          {todos.map((todo) => (
            <div
              key={`${todo.content}:${todo.status ?? "pending"}`}
              className="flex items-start gap-2 rounded-[8px] px-1 py-0.5 text-[11px]"
            >
              <div className="mt-[1px] shrink-0">
                {iconForStatus(todo.status)}
              </div>
              <span className={contentClassName(todo.status)}>
                {todo.content}
              </span>
            </div>
          ))}
        </div>
      </ToolSection>
    </div>
  );
}

function iconForStatus(status: string | null): JSX.Element {
  switch (status) {
    case "completed":
      return (
        <CheckCircle2
          size={14}
          className="text-[var(--state-success)]"
          aria-hidden="true"
        />
      );
    case "in_progress":
      return (
        <AtomIcon
          size={14}
          active
          className="text-[var(--tutti-purple)]"
          aria-hidden="true"
        />
      );
    default:
      return (
        <Circle
          size={14}
          className="text-[var(--text-tertiary)]"
          aria-hidden="true"
        />
      );
  }
}

function contentClassName(status: string | null): string {
  switch (status) {
    case "completed":
      return "text-[var(--text-tertiary)] line-through";
    case "in_progress":
      return "text-[var(--text-primary)]";
    default:
      return "text-[var(--text-secondary)]";
  }
}
