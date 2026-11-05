import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import {
  dedupeToolSectionContent,
  RawPayloadSection,
  ToolMarkdownBlock,
  ToolSection,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { renderRegisteredMcp } from "./mcp-renderers/agentMcpRendererRegistry";
import { normalizeMcpPayload } from "./mcp-renderers/agentMcpShared";
import { observer } from "mobx-react-lite";
export const AgentMcpToolContent = observer(AgentMcpToolContentRender);

function AgentMcpToolContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element {
  "use memo";
  const payload = normalizeMcpPayload(call);
  const specialized = renderRegisteredMcp(payload);
  const visibleText = dedupeToolSectionContent(
    payload.text,
    payload.inputSummary
  );
  const errorText = dedupeToolSectionContent(payload.errorText);

  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      {payload.server || payload.tool ? (
        <ToolSection title={translate("agentHost.agentTool.details.mcp")}>
          <ToolMarkdownBlock
            content={[
              payload.server
                ? `${translate("agentHost.agentTool.details.mcpServer")}: ${payload.server}`
                : null,
              payload.tool
                ? `${translate("agentHost.agentTool.details.mcpTool")}: ${payload.tool}`
                : null
            ]
              .filter((value): value is string => Boolean(value))
              .join("\n\n")}
            onLinkClick={onLinkClick}
          />
        </ToolSection>
      ) : null}
      {payload.inputSummary ? (
        <ToolSection title={translate("agentHost.agentTool.details.input")}>
          <ToolMarkdownBlock
            content={payload.inputSummary}
            onLinkClick={onLinkClick}
          />
        </ToolSection>
      ) : null}
      {specialized ? (
        <ToolSection title={translate("agentHost.agentTool.details.output")}>
          {specialized}
        </ToolSection>
      ) : visibleText ? (
        <ToolSection title={translate("agentHost.agentTool.details.output")}>
          <ToolMarkdownBlock
            content={visibleText}
            onLinkClick={onLinkClick}
            collapsible
          />
        </ToolSection>
      ) : null}
      {errorText ? (
        <ToolSection title={translate("agentHost.agentTool.details.error")}>
          <ToolMarkdownBlock
            content={errorText}
            onLinkClick={onLinkClick}
            collapsible
          />
        </ToolSection>
      ) : null}
      <RawPayloadSection payload={call.payload ?? payload.structured} />
    </div>
  );
}
