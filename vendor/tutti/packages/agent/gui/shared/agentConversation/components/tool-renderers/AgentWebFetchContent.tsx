import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import {
  dedupeToolSectionContent,
  ToolMarkdownBlock,
  ToolSection,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import {
  getToolFallbackText,
  getWebFetchRenderData
} from "./render-data/agentToolRenderData";
import { observer } from "mobx-react-lite";
export const AgentWebFetchContent = observer(AgentWebFetchContentRender);

function AgentWebFetchContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const web = getWebFetchRenderData(call);
  const urlText =
    web.url && web.domain && web.domain !== web.url
      ? `${web.domain}\n\n${web.url}`
      : web.url;
  const visibleContent = dedupeToolSectionContent(
    web.visibleContent,
    web.url,
    web.domain,
    urlText
  );
  const fallbackText = getToolFallbackText(call);
  const errorText = dedupeToolSectionContent(
    fallbackText.error,
    urlText,
    visibleContent
  );
  if (!web.url && !visibleContent && !errorText) {
    return null;
  }

  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      {web.url ? (
        <ToolSection title={translate("agentHost.agentTool.details.url")}>
          <ToolMarkdownBlock
            content={urlText ?? ""}
            onLinkClick={onLinkClick}
          />
        </ToolSection>
      ) : null}
      {visibleContent ? (
        <ToolSection title={translate("agentHost.agentTool.details.content")}>
          <ToolMarkdownBlock
            content={visibleContent}
            onLinkClick={onLinkClick}
            collapsible
          />
        </ToolSection>
      ) : null}
      {web.isTruncated ? (
        <div className="text-[10px] italic text-[var(--text-tertiary)]">
          {translate("agentHost.agentTool.details.contentTruncated")}
        </div>
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
    </div>
  );
}
