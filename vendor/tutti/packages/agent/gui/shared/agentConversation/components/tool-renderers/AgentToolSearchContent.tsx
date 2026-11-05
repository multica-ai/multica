import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import {
  ToolMarkdownBlock,
  ToolSection,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { getToolSearchRenderData } from "./render-data/agentToolRenderData";
import { observer } from "mobx-react-lite";
export const AgentToolSearchContent = observer(AgentToolSearchContentRender);

function AgentToolSearchContentRender({
  call
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const toolSearch = getToolSearchRenderData(call);
  if (!toolSearch.query && toolSearch.matches.length === 0) {
    return null;
  }

  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      {toolSearch.displayQuery ? (
        <ToolSection title={translate("agentHost.agentTool.details.query")}>
          <ToolMarkdownBlock
            content={`${toolSearch.displayQuery}\n\n${toolSearch.mode}`}
          />
        </ToolSection>
      ) : null}
      {toolSearch.matches.length > 0 ? (
        <ToolSection title={translate("agentHost.agentTool.details.results")}>
          <div className="workspace-agents-status-panel__detail-tool-result-list overflow-hidden rounded-[8px] border border-[var(--line-2)] bg-[var(--transparency-block)]">
            {toolSearch.matches.map((match, index) => (
              <div
                key={match}
                className={`px-3 py-2 font-[var(--tsh-font-mono)] text-[11px] text-[var(--text-primary)] ${
                  index > 0 ? "border-t border-[var(--line-2)]" : ""
                }`}
              >
                {match}
              </div>
            ))}
          </div>
        </ToolSection>
      ) : null}
      {toolSearch.matches.length === 0 ? (
        <div className="text-[11px] italic text-[var(--text-tertiary)]">
          {translate("agentHost.agentTool.details.noMatchingTools")}
        </div>
      ) : null}
      {typeof toolSearch.totalDeferredTools === "number" ? (
        <div className="text-[10px] text-[var(--text-tertiary)]">
          {translate("agentHost.agentTool.details.loadedAvailable", {
            loaded: toolSearch.matches.length,
            available: toolSearch.totalDeferredTools
          })}
        </div>
      ) : null}
    </div>
  );
}
