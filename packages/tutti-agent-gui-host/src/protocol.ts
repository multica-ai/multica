import type { ConversationModel } from "@multica/conversation-client/models";
import type { LocalV2Event } from "@multica/core/local-conversation/v2";
import type {
  Agent,
  AgentTask,
  Attachment,
  CancelTaskResponse,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  PendingChatTaskItem,
  RuntimeModelsResult,
  SendChatMessageResponse,
  TaskMessagePayload,
} from "@multica/core/types";
import type { LocalBackgroundTask, LocalPermissionMode, LocalProviderUsage, LocalUserQuestion } from "@multica/core/local-conversation/protocol";

export interface AgentGuiAgentDescriptor {
  agent: Agent;
  provider: string;
  iconUrl: string;
  canInvoke: boolean;
  runtimeBound: boolean;
  runtimeOnline: boolean;
  unavailableReason?: string;
}

/** GUI execution targets are independent of the workspace's mentionable agents. */
export interface AgentGuiExecutionTarget {
  id: string; name: string; provider: string; description?: string; model?: string | null;
  iconUrl: string; canInvoke: boolean; runtimeBound: boolean; runtimeOnline: boolean;
  unavailableReason?: string;
  canFork?: boolean;
}

export function executionTargets(state: AgentGuiWorkspaceState): AgentGuiExecutionTarget[] {
  return state.executionTargets ?? state.agents.map(({ agent, ...descriptor }) => ({
    ...descriptor, id: agent.id, name: agent.name, description: agent.description ?? undefined,
    model: agent.model, canInvoke: descriptor.canInvoke && !agent.archived_at,
  }));
}

export interface AgentGuiSession extends ChatSession {
  localDirectory?: { cwd: string; projectPath: string | null };
}

export interface AgentGuiWorkspaceState {
  /** Local composer storage partition: API origin and main/Side surface. */
  composerDraftScope?: string;
  queueOwner?: "desktop";
  sideWorkspace?: boolean;
  sidePane?: boolean;
  agents: AgentGuiAgentDescriptor[];
  executionTargets?: AgentGuiExecutionTarget[];
  local?: boolean;
  sessions: AgentGuiSession[];
  folderProjects?: import("@multica/core/local-conversation/protocol").LocalFolderProject[];
  pendingTasks?: PendingChatTaskItem[];
}

export interface AgentGuiNewConversationRequest {
  sequence: number;
  projectPath: string | null;
}

export interface AgentGuiInitSnapshot extends AgentGuiWorkspaceState {
  appOrigin?: string | null;
  workspaceId: string;
  workspaceSlug: string;
  userId: string;
  locale: "en" | "zh-CN";
  theme: "light" | "dark";
  /** Interaction focus; losing OS focus must not hide an exposed view. */
  hostActive?: boolean;
  /** Whether Multica is showing this view, independent of OS window focus. */
  hostVisible?: boolean;
  activeSessionId: string | null;
  /** Explicit new-conversation intent; ordinary state refreshes never steal focus. */
  composerFocusSequence?: number;
  /** Apply a local project to the home composer without creating a session. */
  newConversationRequest?: AgentGuiNewConversationRequest;
  selectedAgentId?: string | null;
  generation?: number;
}

export interface AgentGuiSessionDetail {
  promptQueue?: import("@multica/core/local-conversation/protocol").LocalPromptQueue;
  backgroundTasks?: { turnId: string; task: LocalBackgroundTask }[];
  questions?: { turnId: string; question: LocalUserQuestion }[];
  session: AgentGuiSession;
  /** Saved native model used when no GUI preference exists (for example, a new fork). */
  model?: string | null;
  reasoningEffort?: import("@multica/core/local-conversation/protocol").LocalReasoningEffort | null;
  permissionMode?: LocalPermissionMode;
  messages: ChatMessage[];
  pending: ChatPendingTask;
  /** Known authoritative run rows; missing historical rows are not fabricated. */
  tasks?: AgentTask[];
  forkedFrom?: { sessionId: string; turnId: string; targetTurnId: string; operationId: string; createdAt: string };
}

export interface AgentGuiContextItem {
  id: string;
  label: string;
  description?: string;
  agentId?: string;
  userId?: string;
  provider?: string;
  iconUrl?: string;
  status?: string;
  statusLabel?: string;
  statusCategory?: string;
}

export interface AgentGuiContextReference {
  kind: "session" | "issue" | "agent";
  id: string;
}

/** Local CLI status events supplement the cloud transcript contract. */
export type AgentGuiTaskMessagePayload = Omit<TaskMessagePayload, "type"> & {
  type: TaskMessagePayload["type"] | "status";
};

/** IDs belong to the selected transport; local IDs are never sent to cloud APIs. */
export interface AgentGuiRpcMethods {
  "queue.enqueue": { params: { sessionId: string; item: import("@multica/core/local-conversation/protocol").LocalQueuedPrompt; paused?: boolean; queued: true }; result: import("@multica/core/local-conversation/protocol").LocalPromptQueue };
  /** What became of each message; empty from a daemon that cannot say. */
  "queue.statuses": { params: { sessionId: string; requestIds: string[] }; result: Record<string, import("@multica/conversation-client/services").PromptStatus> };
  "queue.update": { params: { sessionId: string; action: "pause" | "stop" | "resume" | "remove" | "sendNow"; requestId?: string }; result: import("@multica/core/local-conversation/protocol").LocalPromptQueue };
  "interaction.respond": { params: { sessionId: string; turnId: string; requestId: string; answers: Record<string, string | string[]> }; result: void };
  "workspace.load": { params: Record<string, never>; result: AgentGuiWorkspaceState };
  "session.load": { params: { sessionId: string }; result: AgentGuiSessionDetail };
  "session.create": {
    params: { agentId: string; title?: string; projectId?: string | null; cwd?: string };
    result: AgentGuiSession;
  };
  "session.send": {
    params: { sessionId: string; content: string; attachmentIds?: string[]; references?: AgentGuiContextReference[]; model?: string; reasoningEffort?: import("@multica/core/local-conversation/protocol").LocalReasoningEffort | null; permissionMode?: LocalPermissionMode; queued?: true; requestId?: string };
    result: SendChatMessageResponse;
  };
  "session.rename": { params: { sessionId: string; title: string }; result: AgentGuiSession };
  "session.fork": { params: { sessionId: string; turnId: string; requestId: string }; result: AgentGuiSession };
  "session.pin": { params: { sessionId: string; pinned: boolean }; result: AgentGuiSession };
  "session.delete": { params: { sessionId: string }; result: void };
  "session.cancel": { params: { sessionId: string; taskId: string }; result: CancelTaskResponse };
  "models.list": { params: { agentId: string; force?: boolean }; result: RuntimeModelsResult };
  "usage.read": { params: { agentId: string; sessionId?: string; force?: boolean }; result: LocalProviderUsage };
  "session.compact": { params: { sessionId: string; requestId: string }; result: { turnId: string } };
  "context.tasks": { params: { query: string; limit: number }; result: AgentGuiContextItem[] };
  "attachment.upload": {
    /** background answers at once with the final id; "attachment.upload" events report the outcome. */
    params: { file: File; sessionId?: string; background?: boolean };
    result: Attachment & { uploading?: boolean };
  };
  "attachment.cancel": { params: { attachmentId: string }; result: void };
  "attachment.retry": { params: { attachmentId: string }; result: void };
  "attachment.forget": { params: { attachmentId: string }; result: void };
  "attachment.url": { params: { attachmentId: string }; result: { url: string } };
  "attachment.read": {
    params: { attachmentId: string };
    result: { data: string; mimeType: string; name: string };
  };
  "image.url": { params: { sessionId: string; path: string }; result: { url: string } };
  "image.read": { params: { sessionId: string; path: string }; result: { data: string; mimeType: string; name: string } };
  /** Parent prefixes keys by account and workspace before using StorageAdapter. */
  "storage.read": { params: { key: string }; result: string | null };
  "storage.write": { params: { key: string; value: string }; result: void };
  "host.openTask": { params: { issueId: string }; result: void };
  "host.openInternalResource": { params: { href: string; background?: boolean }; result: void };
  "host.openSide": { params: { sessionId: string; turnId?: string; quote?: string; requestId: string; queuedRequestId?: string; queued?: true }; result: ChatSession };
  "host.openResource": { params: { sessionId: string; url: string }; result: void };
  "host.showFileContextMenu": { params: { sessionId: string; url: string; hasSelection: boolean }; result: void };
  "host.locateSource": { params: { sessionId: string; turnId?: string; quote?: string }; result: void };
  "host.selectSession": { params: { sessionId: string | null; agentId?: string }; result: void };
  "host.showMessage": { params: { message: string; tone?: "success" | "info" | "warning" | "error" }; result: void };
  "host.promptApplied": { params: { sequence: number; restoreRequestId?: string }; result: void };
}

export type AgentGuiRpcMethod = keyof AgentGuiRpcMethods;
export interface AgentGuiRpcError {
  message: string;
  code?: string;
  status?: number;
}

export type AgentGuiHostEvent =
  | { type: "dismissPopovers" }
  | { type: "locateSource"; sessionId: string; turnId?: string; quote?: string; sequence: number }
  | { type: "invalidate"; scope: "workspace"; generation?: number }
  | { type: "invalidate"; scope: "session"; sessionId: string; generation?: number }
  | { type: "init"; snapshot: AgentGuiInitSnapshot }
  | { type: "appendPrompt"; sequence: number; prompt: string; sessionId?: string; attachments?: Attachment[]; restoreRequestId?: string }
  | { type: "connection"; connected: boolean }
  | { type: "attachment.upload"; attachmentId: string; state: "uploaded" }
  | { type: "attachment.upload"; attachmentId: string; state: "failed"; message: string }
  ;

/**
 * Local conversations as the runtime reads them. It owns nothing here: the
 * conversations outlive the GUI, which projects them while it is mounted.
 */
export interface LocalConversationSource {
  /** Starts keeping a conversation current; the returned function is leaving it. */
  open(sessionId: string): () => void;
  /** The conversation with at least its first turns. */
  ensure(sessionId: string): Promise<ConversationModel>;
  /** The conversation as the daemon has it now, for a caller that must see what it just caused. */
  refresh(sessionId: string): Promise<ConversationModel>;
  /** Reads the next older page, if any. */
  loadOlder(sessionId: string): Promise<void>;
  /** Watches changes; events is empty for a change that is not new events. */
  observe(sessionId: string, listener: (events: readonly LocalV2Event[]) => void): () => void;
  conversation(sessionId: string): ConversationModel | undefined;
  /** What the GUI's session model needs from a loaded conversation. */
  detail(sessionId: string): AgentGuiSessionDetail | null;
}

export interface AgentGuiRpcClient {
  request<TMethod extends AgentGuiRpcMethod>(
    method: TMethod,
    params: AgentGuiRpcMethods[TMethod]["params"],
    options?: { signal?: AbortSignal },
  ): Promise<AgentGuiRpcMethods[TMethod]["result"]>;
  subscribe(listener: (event: AgentGuiHostEvent) => void): () => void;
}
