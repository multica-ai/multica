import type {
  AgentActivityMessageSemantics,
  AgentActivitySession,
  AgentActivityTurn
} from "@tutti-os/agent-activity-core";
import type { WorkspaceAgentActivityTimelineItem } from "./workspaceAgentTimelineTypes";
import type { WorkspaceAgentActivityCard } from "./workspaceAgentActivityListViewModel";
import type { ToolCallStatusKind } from "./workspaceAgentToolCallDisplay";
import { buildCanonicalWorkspaceAgentDetailView } from "./workspaceAgentTimelineCanonical";

export interface WorkspaceAgentSessionDetailMessage {
  id: string;
  body: string;
  status?: string | null;
  statusKind?: ToolCallStatusKind | null;
  turnId?: string;
  occurredAtUnixMs?: number | null;
  sourceTimelineItems?: WorkspaceAgentActivityTimelineItem[];
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
}

export interface WorkspaceAgentSessionDetailThinking {
  id: string;
  body: string;
  statusKind?: ToolCallStatusKind | null;
  turnId?: string;
  occurredAtUnixMs?: number | null;
  sourceTimelineItems?: WorkspaceAgentActivityTimelineItem[];
}

export interface WorkspaceAgentSessionDetailToolCall {
  id: string;
  name: string;
  toolName: string | null;
  callType: string | null;
  status: string | null;
  statusKind: ToolCallStatusKind | null;
  summary: string;
  payload: Record<string, unknown> | null;
  turnId?: string;
  compactSummary?: string | null;
  occurredAtUnixMs?: number | null;
  sourceTimelineItems?: WorkspaceAgentActivityTimelineItem[];
}

export type WorkspaceAgentSessionDetailToolGroupEntry =
  | {
      kind: "thinking";
      thinking: WorkspaceAgentSessionDetailThinking;
    }
  | {
      kind: "tool-call";
      call: WorkspaceAgentSessionDetailToolCall;
    };

export type WorkspaceAgentSessionDetailAgentItem =
  | {
      kind: "message";
      message: WorkspaceAgentSessionDetailMessage;
    }
  | {
      kind: "thinking";
      thinking: WorkspaceAgentSessionDetailThinking;
    }
  | {
      kind: "tool-calls";
      id: string;
      toolCalls: WorkspaceAgentSessionDetailToolCall[];
      toolCallCount: number;
      hasFailedToolCall: boolean;
      summary?: string | null;
      groupEntries?: WorkspaceAgentSessionDetailToolGroupEntry[];
    };

export interface WorkspaceAgentSessionDetailTurn {
  id: string;
  userMessage: WorkspaceAgentSessionDetailMessage | null;
  userMessages: WorkspaceAgentSessionDetailMessage[];
  agentMessages: WorkspaceAgentSessionDetailMessage[];
  toolCalls: WorkspaceAgentSessionDetailToolCall[];
  toolCallCount: number;
  hasFailedToolCall: boolean;
  rawAgentItems?: WorkspaceAgentSessionDetailAgentItem[];
  agentItems: WorkspaceAgentSessionDetailAgentItem[];
}

export interface WorkspaceAgentSessionDetailViewModel {
  activity: WorkspaceAgentActivityCard;
  session: AgentActivitySession;
  cwd: string;
  workspaceRoot: string | null;
  turns: WorkspaceAgentSessionDetailTurn[];
  sessionTurns?: readonly AgentActivityTurn[];
  showProcessingIndicator?: boolean;
}

export interface BuildWorkspaceAgentSessionDetailInput {
  activity: WorkspaceAgentActivityCard;
  session: AgentActivitySession;
  sessionTurns?: readonly AgentActivityTurn[];
  timelineItems: WorkspaceAgentActivityTimelineItem[];
  workspaceRoot?: string | null;
}

export function buildWorkspaceAgentSessionDetailViewModel(
  input: BuildWorkspaceAgentSessionDetailInput
): WorkspaceAgentSessionDetailViewModel {
  return buildCanonicalWorkspaceAgentDetailView(input);
}
