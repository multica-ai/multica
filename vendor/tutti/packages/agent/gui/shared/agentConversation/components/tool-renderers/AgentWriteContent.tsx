import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import { AgentCodeBlock } from "./code/AgentCodeBlock";
import { AgentUnifiedPatchViewer } from "./file-diff/AgentUnifiedPatchViewer";
import {
  stringValue,
  ToolMarkdownBlock,
  ToolSection,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import {
  getFileChangeRenderData,
  getToolCallFailureText
} from "./render-data/agentToolRenderData";
import { observer } from "mobx-react-lite";
export const AgentWriteContent = observer(AgentWriteContentRender);

function AgentWriteContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const files = getFileChangeRenderData(call);
  const [file] = files;
  const path =
    file?.path ??
    stringValue(call.input?.path) ??
    stringValue(call.input?.file_path) ??
    stringValue(call.input?.filePath) ??
    null;
  const contentFiles = files.filter((candidate) => candidate.content !== null);
  const contentPaths = new Set(contentFiles.map((candidate) => candidate.path));
  const patchFiles = files.filter(
    (candidate) =>
      candidate.unifiedDiff !== null && !contentPaths.has(candidate.path)
  );
  const fallbackContent =
    files.length === 0 ? call.summary.trim() || null : null;
  const failureText = getToolCallFailureText(call);
  const showFileHeaders = files.length > 1;
  const hasRenderableContent =
    Boolean(failureText) ||
    Boolean(path && files.length === 0) ||
    patchFiles.length > 0 ||
    contentFiles.length > 0 ||
    Boolean(fallbackContent);

  if (!hasRenderableContent) {
    return null;
  }

  return (
    <div className="workspace-agents-status-panel__detail-tool-body workspace-agents-status-panel__detail-tool-body--flat">
      {failureText ? (
        <ToolSection title={translate("agentHost.agentTool.details.error")}>
          <ToolMarkdownBlock
            content={failureText}
            onLinkClick={onLinkClick}
            collapsible
          />
        </ToolSection>
      ) : null}
      {path && files.length === 0 ? (
        <ToolMarkdownBlock content={path} onLinkClick={onLinkClick} />
      ) : null}
      {patchFiles.map((candidate) => (
        <AgentUnifiedPatchViewer
          key={`patch:${candidate.path}`}
          path={candidate.path}
          diffText={candidate.unifiedDiff!}
          showHeader={showFileHeaders}
          compact
          flat
        />
      ))}
      {contentFiles.map((candidate) => (
        <AgentCodeBlock
          key={`content:${candidate.path}`}
          path={candidate.path}
          content={candidate.content!}
          language={candidate.language}
          showHeader={showFileHeaders}
          flat
        />
      ))}
      {fallbackContent ? (
        <AgentCodeBlock
          path={path}
          content={fallbackContent}
          language={file?.language ?? null}
          showHeader={false}
          flat
        />
      ) : null}
    </div>
  );
}
