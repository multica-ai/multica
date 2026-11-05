import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import {
  dedupeToolSectionContent,
  ToolMarkdownBlock,
  ToolSection,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { getSearchRenderData } from "./render-data/agentToolRenderData";
import { observer } from "mobx-react-lite";
export const AgentSearchContent = observer(AgentSearchContentRender);

function AgentSearchContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element {
  "use memo";
  const search = getSearchRenderData(call);
  const queryText = search.scope
    ? `${search.query ?? ""}\n\n${translate("agentHost.agentTool.details.scope")}: ${search.scope}`.trim()
    : search.query;
  const resultFiles = withStableOccurrenceKeys(search.files, "file");
  const visibleOutput = dedupeToolSectionContent(search.output, queryText);
  const outputLines = withStableOccurrenceKeys(
    visibleOutput.split("\n").filter(Boolean),
    "line"
  );

  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      {queryText ? (
        <ToolSection title={translate("agentHost.agentTool.details.query")}>
          <ToolMarkdownBlock content={queryText} onLinkClick={onLinkClick} />
        </ToolSection>
      ) : null}
      {(search.mode === "files_with_matches" || search.mode === "list_files") &&
      search.files.length > 0 ? (
        <ToolSection title={translate("agentHost.agentTool.details.results")}>
          <div className="workspace-agents-status-panel__detail-tool-result-list overflow-hidden rounded-[8px] border border-[var(--line-2)] bg-[var(--transparency-block)]">
            {resultFiles.map(({ key, value: file, isFirst }) => (
              <div
                key={key}
                className={`px-3 py-2 font-[var(--tsh-font-mono)] text-[11px] text-[var(--text-primary)] ${
                  isFirst ? "" : "border-t border-[var(--line-2)]"
                }`}
              >
                {file}
              </div>
            ))}
          </div>
        </ToolSection>
      ) : null}
      {search.mode === "content" && visibleOutput ? (
        <ToolSection title={translate("agentHost.agentTool.details.output")}>
          <pre className="max-h-[320px] overflow-auto rounded-[8px] border border-[var(--line-2)] bg-[var(--transparency-block)] px-3 py-2 text-[11px] leading-5 text-[var(--text-primary)]">
            {outputLines.map(({ key, value: line }) => (
              <div
                key={key}
                className={
                  line.includes(":")
                    ? "text-[var(--text-primary)]"
                    : "text-[var(--text-tertiary)]"
                }
              >
                {line}
              </div>
            ))}
          </pre>
        </ToolSection>
      ) : null}
      {search.mode === "count" ? (
        <ToolSection title={translate("agentHost.agentTool.details.results")}>
          <div className="inline-flex rounded-full border border-[var(--line-2)] bg-[var(--transparency-block)] px-2.5 py-1 text-[11px] text-[var(--text-tertiary)]">
            {search.output || "0"}
          </div>
        </ToolSection>
      ) : null}
      {(search.mode === "files_with_matches" ||
        search.mode === "list_files" ||
        search.mode === "count") &&
      search.files.length === 0 &&
      !visibleOutput &&
      !search.error ? (
        <ToolSection title={translate("agentHost.agentTool.details.results")}>
          <div className="text-[11px] italic text-[var(--text-tertiary)]">
            {translate("agentHost.agentTool.details.noMatches")}
          </div>
        </ToolSection>
      ) : null}
      {search.mode === "unknown" && visibleOutput ? (
        <ToolSection title={translate("agentHost.agentTool.details.output")}>
          <ToolMarkdownBlock
            content={visibleOutput}
            onLinkClick={onLinkClick}
            collapsible
          />
        </ToolSection>
      ) : null}
      {search.error ? (
        <ToolSection title={translate("agentHost.agentTool.details.error")}>
          <ToolMarkdownBlock
            content={search.error}
            onLinkClick={onLinkClick}
            collapsible
          />
        </ToolSection>
      ) : null}
    </div>
  );
}

function withStableOccurrenceKeys(
  values: readonly string[],
  prefix: string
): Array<{ key: string; value: string; isFirst: boolean }> {
  const seen = new Map<string, number>();
  const keyedValues: Array<{ key: string; value: string; isFirst: boolean }> =
    [];
  let isFirst = true;
  for (const value of values) {
    const occurrence = seen.get(value) ?? 0;
    seen.set(value, occurrence + 1);
    keyedValues.push({
      key: `${prefix}:${value}:${occurrence}`,
      value,
      isFirst
    });
    isFirst = false;
  }
  return keyedValues;
}
