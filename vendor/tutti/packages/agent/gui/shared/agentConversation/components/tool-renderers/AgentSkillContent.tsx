import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import {
  ToolSection,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { ToolMarkdownBlock } from "./agentToolContentShared";
import { getSkillRenderData } from "./render-data/agentToolRenderData";
import { observer } from "mobx-react-lite";
export const AgentSkillContent = observer(AgentSkillContentRender);

function AgentSkillContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const skill = getSkillRenderData(call);

  if (!skill.skill && !skill.statusText) {
    return null;
  }

  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      {skill.skill ? (
        <ToolSection title={translate("agentHost.agentTool.details.skill")}>
          <ToolMarkdownBlock
            content={
              skill.args ? `${skill.skill}\n\n${skill.args}` : skill.skill
            }
            onLinkClick={onLinkClick}
          />
        </ToolSection>
      ) : null}
      {skill.statusText ? (
        <div className="text-[10px] text-[var(--text-tertiary)]">
          {skill.statusText}
        </div>
      ) : null}
    </div>
  );
}
