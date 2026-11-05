import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import type { AgentToolRendererProps } from "./agentToolContentShared";
import { ToolMarkdownBlock, ToolSection } from "./agentToolContentShared";
import { getImageGenerationRenderData } from "./render-data/agentToolRenderData";
import { observer } from "mobx-react-lite";
export const AgentImageGenerationContent = observer(
  AgentImageGenerationContentRender
);

function AgentImageGenerationContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const image = getImageGenerationRenderData(call);
  if (!image.prompt) {
    return null;
  }

  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      <ToolSection title={translate("agentHost.agentTool.details.input")}>
        <ToolMarkdownBlock
          content={image.prompt}
          onLinkClick={onLinkClick}
          collapsible
        />
      </ToolSection>
    </div>
  );
}
