import type { JSX } from "react";
import { translate } from "../../../../i18n/index";
import type {
  AgentToolCallVM,
  AgentToolRendererKind
} from "../../contracts/agentToolCallVM";
import { AgentEditContent } from "./AgentEditContent";
import { AgentWebFetchContent } from "./AgentWebFetchContent";
import { AgentWebSearchContent } from "./AgentWebSearchContent";
import {
  arrayValue,
  objectValue,
  stringValue,
  ToolSection,
  ToolMarkdownBlock,
  type AgentToolRendererProps
} from "./agentToolContentShared";
import { formatToolDetails } from "../interactivePromptPresentation";
import { observer } from "mobx-react-lite";
export const AgentApprovalContent = observer(AgentApprovalContentRender);

function AgentApprovalContentRender({
  call,
  onLinkClick
}: AgentToolRendererProps): JSX.Element | null {
  "use memo";
  const previewCall = approvalPreviewCall(call);
  const detailText = approvalDetailText(call.input);
  if (!previewCall && !call.summary.trim() && !detailText) {
    return null;
  }
  if (previewCall) {
    switch (previewCall.rendererKind) {
      case "web-fetch":
        return (
          <AgentWebFetchContent call={previewCall} onLinkClick={onLinkClick} />
        );
      case "web-search":
        return (
          <AgentWebSearchContent call={previewCall} onLinkClick={onLinkClick} />
        );
      default:
        return (
          <AgentEditContent call={previewCall} onLinkClick={onLinkClick} />
        );
    }
  }
  return (
    <div className="workspace-agents-status-panel__detail-tool-body">
      {call.summary.trim() ? (
        <ToolMarkdownBlock content={call.summary} />
      ) : null}
      {detailText ? (
        <ToolSection title={translate("agentHost.agentTool.details.input")}>
          <ToolMarkdownBlock content={detailText} />
        </ToolSection>
      ) : null}
    </div>
  );
}

function approvalDetailText(
  input: Record<string, unknown> | null
): string | null {
  const details = formatToolDetails(input);
  if (details.length === 0) {
    return null;
  }
  return details
    .map((detail) =>
      [detail.label, detail.value, detail.meta]
        .filter((value): value is string => Boolean(value?.trim()))
        .join("\n")
    )
    .join("\n\n");
}

interface ApprovalPreviewKind {
  rendererKind: AgentToolRendererKind;
  toolName: string;
}

// The nested tool-call's `kind` follows the ACP vocabulary (edit/move/fetch/...)
// when the provider sends it (e.g. Codex); Claude Code omits `kind` and only
// sets `title`/`toolName` to the canonical tool name (e.g. "WebFetch"), so
// both vocabularies are matched here.
function approvalPreviewKindFor(
  normalizedKind: string
): ApprovalPreviewKind | null {
  switch (normalizedKind) {
    case "edit":
    case "move":
      return { rendererKind: "edit", toolName: "Edit" };
    case "fetch":
    case "webfetch":
    case "web_fetch":
    case "web-fetch":
      return { rendererKind: "web-fetch", toolName: "WebFetch" };
    case "websearch":
    case "web_search":
    case "web-search":
      return { rendererKind: "web-search", toolName: "WebSearch" };
    default:
      return null;
  }
}

function approvalPreviewCall(call: AgentToolCallVM): AgentToolCallVM | null {
  const toolCall = objectValue(call.input?.toolCall);
  if (!toolCall) {
    return null;
  }
  const normalizedKind = normalizeToolKind(
    stringValue(toolCall.kind) ??
      stringValue(toolCall.title) ??
      stringValue(toolCall.toolName)
  );
  const preview = approvalPreviewKindFor(normalizedKind);
  if (!preview) {
    return null;
  }
  const input = objectValue(toolCall.input) ?? call.input;
  const locations = arrayValue(toolCall.locations);
  return {
    kind: "tool-call",
    id: `${call.id}:approval-preview`,
    turnId: call.turnId,
    name: stringValue(toolCall.title) ?? call.name,
    toolName: preview.toolName,
    callType: "tool",
    status: stringValue(toolCall.status) ?? call.status,
    statusKind: call.statusKind,
    summary: "",
    compactSummary: null,
    payload: {
      input,
      locations
    },
    input,
    output: null,
    error: null,
    metadata: null,
    locations,
    rendererKind: preview.rendererKind,
    approval: null,
    planMode: null,
    askUserQuestion: null,
    task: null,
    occurredAtUnixMs: call.occurredAtUnixMs
  };
}

function normalizeToolKind(value: string | null): string {
  return (value ?? "").trim().toLowerCase();
}
