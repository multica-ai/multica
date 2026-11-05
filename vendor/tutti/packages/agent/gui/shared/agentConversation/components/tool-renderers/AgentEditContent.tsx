import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import { AgentCodeBlock } from "./code/AgentCodeBlock";
import { AgentMonacoDiffViewer } from "./file-diff/AgentMonacoDiffViewer";
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
export const AgentEditContent = observer(AgentEditContentRender);

function AgentEditContentRender({
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
  const patchFiles = files.filter(
    (candidate) => candidate.unifiedDiff !== null
  );
  const diffFiles = files.filter(
    (candidate) =>
      candidate.unifiedDiff === null &&
      candidate.oldString !== null &&
      candidate.newString !== null
  );
  const contentFiles = files.filter(
    (candidate) =>
      candidate.unifiedDiff === null &&
      !(candidate.oldString !== null && candidate.newString !== null) &&
      candidate.content !== null
  );
  const failureText = getToolCallFailureText(call);
  const hasRenderableContent =
    Boolean(failureText) ||
    Boolean(path && files.length === 0) ||
    patchFiles.length > 0 ||
    diffFiles.length > 0 ||
    contentFiles.length > 0;

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
          compact
          flat
        />
      ))}
      {diffFiles.map((candidate) => (
        <AgentMonacoDiffViewer
          key={`diff:${candidate.path}`}
          path={candidate.path}
          oldValue={candidate.oldString!}
          newValue={candidate.newString!}
          flat
        />
      ))}
      {contentFiles.map((candidate) => (
        <AgentCodeBlock
          key={`content:${candidate.path}`}
          path={candidate.path}
          content={candidate.content!}
          language={candidate.language}
          showHeader={false}
          flat
        />
      ))}
    </div>
  );
}
