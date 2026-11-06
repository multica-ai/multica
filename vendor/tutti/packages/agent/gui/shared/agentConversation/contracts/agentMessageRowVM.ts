import type { AgentActivityMessageSemantics } from "@tutti-os/agent-activity-core";
import type { ToolCallStatusKind } from "../../workspaceAgentToolCallDisplay";
import type { WorkspaceAgentActivityTimelineItem } from "../../workspaceAgentTimelineTypes";
import type { AgentTranscriptPresentationKind } from "./agentTranscriptPresentation";
import type { AgentCollaborationVM } from "./agentCollaborationVM";
import type { AgentToolGroupRowVM } from "./agentToolGroupRowVM";

export type AgentMessageContentKind =
  | "text"
  | "image-grid"
  | "file-list"
  | "plan"
  | "collaboration"
  | "tutti-checkpoint-wake"
  | "tutti-plan-issue-link"
  | "selected-text";

export interface AgentMessageContentVM {
  kind: "message-content";
  id: string;
  turnId: string;
  body: string;
  presentationKind: AgentTranscriptPresentationKind;
  copyText?: string | null;
  statusKind?: ToolCallStatusKind | null;
  contentKind?: AgentMessageContentKind;
  isTurnFinalText?: true;
  /** Typed payload for `contentKind: "collaboration"` rows. */
  collaboration?: AgentCollaborationVM | null;
  /**
   * Typed payload for `contentKind: "tutti-checkpoint-wake"` rows: a daemon
   * checkpoint-wake prompt injected into the source agent. `body` carries the
   * full prompt (minus the sentinel line) for the expand-to-full affordance.
   */
  checkpointWake?: AgentTuttiModeCheckpointWakeVM | null;
  /**
   * Typed payload for `contentKind: "tutti-plan-issue-link"` rows: the durable
   * plan→Issue reverse link the daemon writes when the user accepts a Tutti
   * Mode plan (messageId "plan-issue:<issueID>").
   */
  planIssueLink?: AgentTuttiPlanIssueLinkVM | null;
  /** Typed payload for `contentKind: "selected-text"` rows. */
  selectedText?: AgentSelectedTextVM | null;
  images?: AgentMessageImageVM[];
  files?: AgentMessageFileVM[];
  occurredAtUnixMs: number | null;
  visibleError?: {
    code: string | null;
    phase: string | null;
    provider: string | null;
    origin?: string | null;
    detail: string | null;
    detailAvailable?: boolean;
    retryable: boolean | null;
  } | null;
  systemNotice?: {
    noticeKind: string | null;
    semanticKind?: "context-handoff-required" | null;
    severity: string | null;
    source?: string | null;
    command?: AgentActivityMessageSemantics["noticeCommand"] | null;
    commandStatus?: AgentActivityMessageSemantics["noticeCommandStatus"] | null;
    title: string | null;
    detail: string | null;
    retryable: boolean | null;
  } | null;
  sourceTimelineItems?: WorkspaceAgentActivityTimelineItem[];
}

export interface AgentSelectedTextVM {
  count: number;
  texts: readonly string[];
}

export interface AgentTuttiModeCheckpointWakeVM {
  /** Wire kind, e.g. "task_settled" | "task_failed" | "all_tasks_terminal". */
  kind: string;
  issueId: string;
  checkpointId: string;
  graphRevision: number | null;
}

export interface AgentTuttiPlanIssueLinkVM {
  issueId: string;
  /** Issue title with markdown escapes removed; falls back to the issue id. */
  title: string;
  /** The original single-mention markdown, rendered as the issue chip. */
  mentionMarkdown: string;
}

export interface AgentMessageFileVM {
  id: string;
  workspaceId?: string | null;
  agentSessionId: string;
  attachmentId?: string | null;
  name?: string | null;
  url?: string | null;
  metadataUnavailable: boolean;
}

export interface AgentMessageImageVM {
  id: string;
  workspaceId?: string | null;
  agentSessionId: string;
  attachmentId?: string | null;
  mimeType: string;
  name?: string | null;
  data?: string | null;
  url?: string | null;
  path?: string | null;
}

export interface AgentThinkingContentVM {
  kind: "thinking-content";
  id: string;
  turnId: string;
  body: string;
  statusKind?: ToolCallStatusKind | null;
  occurredAtUnixMs: number | null;
  sourceTimelineItems?: WorkspaceAgentActivityTimelineItem[];
}

export interface AgentMessageRowVM {
  kind: "message";
  id: string;
  turnId: string;
  speaker: "user" | "assistant";
  /**
   * Exact first text block from the submitted structured content. Editing must
   * not recover this value from displayPrompt, copy text, or rendered Markdown.
   */
  rawFirstTextBlock?: string | null;
  messages: AgentMessageContentVM[];
  thinking: AgentThinkingContentVM[];
  /**
   * Tool-group rows that happened right before this message and are rendered
   * inside this message's block (under the participant header, above the
   * content) instead of as standalone transcript rows. Populated only for the
   * participant-header presentation (e.g. the Agent board session detail).
   */
  leadingToolRows?: AgentToolGroupRowVM[];
  occurredAtUnixMs: number | null;
}
