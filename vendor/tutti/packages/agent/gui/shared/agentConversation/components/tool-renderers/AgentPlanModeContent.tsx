import type { JSX } from "react";
import {
  ToolMarkdownBlock,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { getPlanModeRenderData } from "./render-data/agentToolRenderData";
import { AgentPlanCard } from "../AgentPlanCard";
import { observer } from "mobx-react-lite";
export const AgentPlanModeContent = observer(AgentPlanModeContentRender);

function AgentPlanModeContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const planMode = getPlanModeRenderData(call);
  if (call.rendererKind === "plan-enter") {
    if (!planMode.enterText) {
      return null;
    }
    return (
      <div className="rounded-[8px] bg-[var(--transparency-block)] px-3 py-2 text-[11px] text-[var(--text-secondary)]">
        <ToolMarkdownBlock
          content={planMode.enterText}
          onLinkClick={onLinkClick}
        />
      </div>
    );
  }

  if (!planMode.plan) {
    return null;
  }
  return (
    <AgentPlanCard
      title={planMode.fileName ?? undefined}
      copyText={planMode.plan}
    >
      <ToolMarkdownBlock content={planMode.plan} onLinkClick={onLinkClick} />
    </AgentPlanCard>
  );
}
