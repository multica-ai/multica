import { AlertCircle, ChevronDown, ChevronRight } from "lucide-react";
import { observer } from "mobx-react-lite";
import type { JSX } from "react";
import { FilePenLineIcon } from "../../../app/renderer/components/icons/FilePenLineIcon";
import { FileTextIcon } from "../../../app/renderer/components/icons/FileTextIcon";
import { HammerIcon } from "../../../app/renderer/components/icons/HammerIcon";
import { ToolsLinedIcon } from "../../../app/renderer/components/icons/ToolsLinedIcon";
import { WriteLinedIcon } from "../../../app/renderer/components/icons/WriteLinedIcon";
import { isImageGenerationToolCall } from "../../imageGenerationTool";
import { ToolNameIcon } from "../../toolActivityKindIcons";
import type { AgentToolCallVM } from "../contracts/agentToolCallVM";
import {
  getFileChangeRenderData,
  isFailedToolCall
} from "./tool-renderers/render-data/agentToolRenderData";
const LoadingEllipsis = observer(LoadingEllipsisRender);

const TOOL_ROW_ICON_SIZE = 16;

interface AgentToolCallHeaderProps {
  call: AgentToolCallVM;
  expanded: boolean;
  hasDetail: boolean;
}

export const AgentToolCallHeader = observer(function AgentToolCallHeader({
  call,
  expanded,
  hasDetail
}: AgentToolCallHeaderProps): JSX.Element {
  "use memo";
  const diffStats = diffStatsForCall(call);
  const isActive =
    call.statusKind === "working" || call.statusKind === "waiting";
  const isFailed = isFailedToolCall(call);

  return (
    <div
      data-active={isActive ? "true" : undefined}
      className={[
        "workspace-agents-status-panel__detail-tool-row-header-content",
        isActive ? "tsh-inline-scanlight-group" : ""
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <div className="workspace-agents-status-panel__detail-tool-row-icon tsh-inline-scanlight-icon">
        {isFailed ? (
          <AlertCircle
            size={TOOL_ROW_ICON_SIZE}
            strokeWidth={2}
            aria-hidden="true"
          />
        ) : isActive && isFileReadTool(call.toolName) ? (
          <FileTextIcon
            size={TOOL_ROW_ICON_SIZE}
            active
            aria-hidden="true"
            className="text-current"
            data-testid="agent-tool-read-loading-icon"
          />
        ) : isActive && isFileWriteTool(call.toolName) ? (
          <FilePenLineIcon
            size={TOOL_ROW_ICON_SIZE}
            active
            aria-hidden="true"
            className="text-current"
            data-testid="agent-tool-write-loading-icon"
          />
        ) : isActive && isFileEditTool(call.toolName) ? (
          <FileTextIcon
            size={TOOL_ROW_ICON_SIZE}
            active
            aria-hidden="true"
            className="text-current"
            data-testid="agent-tool-edit-loading-icon"
          />
        ) : isActive && isStdinWriteTool(call.toolName) ? (
          <WriteLinedIcon
            width={TOOL_ROW_ICON_SIZE}
            height={TOOL_ROW_ICON_SIZE}
            aria-hidden="true"
            className="text-current"
            data-testid="agent-tool-stdin-write-icon"
          />
        ) : isImageGenerationToolCall({
            toolName: call.toolName,
            displayName: call.name
          }) ? (
          <ToolNameIcon
            toolName={call.toolName ?? call.name}
            width={TOOL_ROW_ICON_SIZE}
            height={TOOL_ROW_ICON_SIZE}
            aria-hidden="true"
            className="text-current"
            data-testid={
              isActive
                ? "agent-tool-image-generation-loading-icon"
                : "agent-tool-image-generation-icon"
            }
          />
        ) : isWebTool(call.toolName) ? (
          <ToolNameIcon
            toolName={call.toolName}
            width={TOOL_ROW_ICON_SIZE}
            height={TOOL_ROW_ICON_SIZE}
            aria-hidden="true"
            className="text-current"
            data-testid={
              isActive ? "agent-tool-web-loading-icon" : "agent-tool-web-icon"
            }
          />
        ) : isFileReadTool(call.toolName) ? (
          <ToolNameIcon
            toolName={call.toolName}
            width={TOOL_ROW_ICON_SIZE}
            height={TOOL_ROW_ICON_SIZE}
            aria-hidden="true"
            className="text-current"
            data-testid="agent-tool-read-icon"
          />
        ) : call.statusKind === "working" || call.statusKind === "waiting" ? (
          <HammerIcon
            size={TOOL_ROW_ICON_SIZE}
            active
            aria-hidden="true"
            className="text-current"
            data-testid="agent-tool-call-loading-icon"
          />
        ) : (
          <ToolsLinedIcon
            width={TOOL_ROW_ICON_SIZE}
            height={TOOL_ROW_ICON_SIZE}
            aria-hidden="true"
          />
        )}
      </div>
      <div
        className={[
          "workspace-agents-status-panel__detail-tool-row-text",
          isActive ? "tsh-inline-scanlight-line" : ""
        ]
          .filter(Boolean)
          .join(" ")}
      >
        <strong className="workspace-agents-status-panel__detail-tool-row-title">
          {formatInlineTitleLabel(call.name)}
        </strong>
        {call.status ? (
          <span className="workspace-agents-status-panel__detail-tool-status">
            {formatInlineStatusLabel(call.status)}
          </span>
        ) : null}
        {isActive ? <LoadingEllipsis /> : null}
        {diffStats ? (
          <span className="workspace-agents-status-panel__detail-tool-diff-stats">
            {diffStats.added > 0 ? (
              <span className="workspace-agents-status-panel__detail-tool-diff-added">
                +{diffStats.added}
              </span>
            ) : null}
            {diffStats.removed > 0 ? (
              <span className="workspace-agents-status-panel__detail-tool-diff-removed">
                -{diffStats.removed}
              </span>
            ) : null}
          </span>
        ) : null}
      </div>
      {hasDetail ? (
        expanded ? (
          <ChevronDown
            size={12}
            strokeWidth={2.2}
            aria-hidden="true"
            className="workspace-agents-status-panel__detail-tool-row-chevron"
          />
        ) : (
          <ChevronRight
            size={12}
            strokeWidth={2.2}
            aria-hidden="true"
            className="workspace-agents-status-panel__detail-tool-row-chevron"
          />
        )
      ) : null}
    </div>
  );
});

function formatInlineStatusLabel(label: string): string {
  return /^[A-Z][a-z]+$/.test(label) ? label.toLowerCase() : label;
}

function formatInlineTitleLabel(label: string): string {
  const trimmed = label.trim();
  if (!/[a-z]/.test(trimmed) || !/[A-Z]/.test(trimmed.slice(1))) {
    return label;
  }

  const words = trimmed.split(/(\s+)/);
  let changed = false;
  const normalized = words
    .map((word, index) => {
      if (index === 0 || !/^[A-Z][a-z]+$/.test(word)) {
        return word;
      }
      changed = true;
      return word.toLowerCase();
    })
    .join("");
  return changed ? normalized : label;
}

function LoadingEllipsisRender(): JSX.Element {
  "use memo";
  return (
    <span className="tsh-inline-loading-ellipsis" aria-hidden="true">
      <span />
      <span />
      <span />
    </span>
  );
}

function diffStatsForCall(
  call: AgentToolCallVM
): { added: number; removed: number } | null {
  if (call.rendererKind === "approval" || isFailedToolCall(call)) {
    return null;
  }
  const files = getFileChangeRenderData(call);
  if (files.length > 0) {
    const totals = files.reduce(
      (accumulator, file) => ({
        added: accumulator.added + file.added,
        removed: accumulator.removed + file.removed
      }),
      { added: 0, removed: 0 }
    );
    if (totals.added > 0 || totals.removed > 0) {
      return totals;
    }
  }
  return null;
}

function isFileReadTool(toolName: string | null): boolean {
  switch (normalizeToolName(toolName)) {
    case "read":
    case "readfile":
    case "listfiles":
    case "findfiles":
    case "glob":
      return true;
    default:
      return false;
  }
}

function isFileEditTool(toolName: string | null): boolean {
  switch (normalizeToolName(toolName)) {
    case "edit":
    case "editfile":
    case "multiedit":
      return true;
    default:
      return false;
  }
}

function isFileWriteTool(toolName: string | null): boolean {
  switch (normalizeToolName(toolName)) {
    case "write":
    case "writefile":
      return true;
    default:
      return false;
  }
}

function isStdinWriteTool(toolName: string | null): boolean {
  return normalizeToolName(toolName) === "writestdin";
}

function isWebTool(toolName: string | null): boolean {
  switch (normalizeToolName(toolName)) {
    case "websearch":
    case "webfetch":
      return true;
    default:
      return false;
  }
}

function normalizeToolName(value: string | null): string {
  return (value ?? "")
    .trim()
    .replace(/[_\s-]+/g, "")
    .toLowerCase();
}
