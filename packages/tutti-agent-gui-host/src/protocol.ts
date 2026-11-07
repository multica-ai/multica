import type { ConversationModel } from "@multica/conversation-client/models";
import type {
LocalBackgroundTask,
LocalPermissionMode,
LocalProviderUsage,
LocalUserQuestion
} from "@multica/core/local-conversation/protocol";
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
TaskMessagePayload
} from "@multica/core/types";

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
  id: string;
  name: string;
  provider: string;
  description?: string;
  model?: string | null;
  iconUrl: string;
  canInvoke: boolean;
  runtimeBound: boolean;
  runtimeOnline: boolean;
  unavailableReason?: string;
  canFork?: boolean;
}

export function executionTargets(
  state: AgentGuiWorkspaceState
): AgentGuiExecutionTarget[] {
  return (
    state.executionTargets ??
    state.agents.map(({ agent, ...descriptor }) => ({
      ...descriptor,
      id: agent.id,
      name: agent.name,
      description: agent.description ?? undefined,
      model: agent.model,
      canInvoke: descriptor.canInvoke && !agent.archived_at
    }))
  );
}

export interface AgentGuiSession extends ChatSession {
  localDirectory?: { cwd: string; projectPath: string | null };
  localConversation?: {
    sideSourceId?: string;
    /** Latest user turn's send time; null for no turns, absent on older daemons. */
    lastTurnSentAt?: string | null;
  };
}

export interface AgentGuiWorkspaceState {
  /** Local composer storage partition: API origin and main/Side surface. */
  composerDraftScope?: string;
  queueOwner?: "desktop";
  sideWorkspace?: boolean;
  sidePane?: boolean;
  agents: AgentGuiAgentDescriptor[];
  members?: { userId: string; name: string; iconUrl: string }[];
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
  reasoningEffort?:
    | import("@multica/core/local-conversation/protocol").LocalReasoningEffort
    | null;
  permissionMode?: LocalPermissionMode;
  messages: ChatMessage[];
  pending: ChatPendingTask;
  /** Known authoritative run rows; missing historical rows are not fabricated. */
  tasks?: AgentTask[];
  forkedFrom?: {
    sessionId: string;
    turnId: string;
    targetTurnId: string;
    operationId: string;
    createdAt: string;
  };
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

export type AgentGuiContextReference = import("@multica/core/local-conversation/protocol").LocalContextReference;

/** Local CLI status events supplement the cloud transcript contract. */
export type AgentGuiTaskMessagePayload = Omit<TaskMessagePayload, "type"> & {
  type: TaskMessagePayload["type"] | "status";
};

/** IDs belong to the selected transport; local IDs are never sent to cloud APIs. */
export interface AgentGuiOperations {
  "queue.enqueue": {
    params: {
      sessionId: string;
      item: import("@multica/core/local-conversation/protocol").LocalQueuedPrompt;
      paused?: boolean;
      queued: true;
    };
    result: import("@multica/core/local-conversation/protocol").LocalPromptQueue;
  };
  /** What became of each message; empty from a daemon that cannot say. */
  "queue.statuses": {
    params: { sessionId: string; requestIds: string[] };
    result: Record<
      string,
      import("@multica/conversation-client/services").PromptStatus
    >;
  };
  "queue.prepare": {
    params:AgentGuiOperations["queue.enqueue"]["params"];
    result:AgentGuiOperations["queue.enqueue"]["params"];
  };
  "queue.update": {
    params: {
      sessionId: string;
      action: "pause" | "stop" | "resume" | "remove" | "sendNow";
      requestId?: string;
      targetTurnId?: string;
    };
    result: import("@multica/core/local-conversation/protocol").LocalPromptQueue;
  };
  "interaction.respond": {
    params: {
      sessionId: string;
      turnId: string;
      requestId: string;
      answers: Record<string, string | string[]>;
    };
    result: void;
  };
  "workspace.load": {
    params: Record<string, never>;
    result: AgentGuiWorkspaceState;
  };
  "session.load": {
    params: { sessionId: string };
    result: AgentGuiSessionDetail;
  };
  "session.create": {
    params: {
      sessionId?: string;
      agentId: string;
      title?: string;
      projectId?: string | null;
      cwd?: string;
    };
    result: AgentGuiSession;
  };
  "session.send": {
    params: {
      sessionId: string;
      content: string;
      attachmentIds?: string[];
      references?: AgentGuiContextReference[];
      model?: string;
      reasoningEffort?:
        | import("@multica/core/local-conversation/protocol").LocalReasoningEffort
        | null;
      permissionMode?: LocalPermissionMode;
      queued?: true;
      requestId?: string;
    };
    result: SendChatMessageResponse;
  };
  "session.rename": {
    params: { sessionId: string; title: string };
    result: AgentGuiSession;
  };
  "session.fork": {
    params: { sessionId: string; turnId: string; requestId: string };
    result: AgentGuiSession;
  };
  "session.pin": {
    params: { sessionId: string; pinned: boolean };
    result: AgentGuiSession;
  };
  "session.delete": { params: { sessionId: string }; result: void };
  "session.cancel": {
    params: { sessionId: string; taskId: string };
    result: CancelTaskResponse;
  };
  "models.list": {
    params: { agentId: string; force?: boolean };
    result: RuntimeModelsResult;
  };
  "usage.read": {
    params: { agentId: string; sessionId?: string; force?: boolean };
    result: LocalProviderUsage;
  };
  "session.compact": {
    params: { sessionId: string; requestId: string };
    result: { turnId: string };
  };
  "context.tasks": {
    params: { query: string; limit: number };
    result: AgentGuiContextItem[];
  };
  "attachment.upload": {
    /** background answers at once with the final id; "attachment.upload" events report the outcome. */
    params: { file: File; sessionId?: string; background?: boolean };
    result: Attachment & { uploading?: boolean };
  };
  "attachment.cancel": { params: { attachmentId: string }; result: void };
  "attachment.retry": { params: { attachmentId: string }; result: void };
  "attachment.forget": { params: { attachmentId: string }; result: void };
  "attachment.url": {
    params: { attachmentId: string };
    result: { url: string };
  };
  "attachment.metadata": {
    params: { attachmentId: string };
    result: { name: string; mimeType: string; sizeBytes: number; url: string };
  };
  "attachment.read": {
    params: { attachmentId: string };
    result: { data: string; mimeType: string; name: string };
  };
  "image.url": {
    params: { sessionId: string; path: string };
    result: { url: string };
  };
  "image.read": {
    params: { sessionId: string; path: string };
    result: { data: string; mimeType: string; name: string };
  };
  /** Parent prefixes keys by account and workspace before using StorageAdapter. */
  "storage.read": { params: { key: string }; result: string | null };
  "storage.write": { params: { key: string; value: string }; result: void };
  "host.openTask": { params: { issueId: string }; result: void };
  "host.openInternalResource": {
    params: { href: string; background?: boolean };
    result: void;
  };
  "host.openSide": {
    params: {
      sessionId: string;
      turnId?: string;
      quote?: string;
      requestId: string;
      queuedRequestId?: string;
      queued?: true;
    };
    result: ChatSession;
  };
  "host.openResource": {
    params: { sessionId: string; url: string };
    result: void;
  };
  "host.showFileContextMenu": {
    params: { sessionId: string; url: string; hasSelection: boolean };
    result: void;
  };
  "host.locateSource": {
    params: { sessionId: string; turnId?: string; quote?: string };
    result: void;
  };
  "host.selectSession": {
    params: { sessionId: string | null; agentId?: string };
    result: void;
  };
  "host.showMessage": {
    params: {
      message: string;
      tone?: "success" | "info" | "warning" | "error";
    };
    result: void;
  };
  "host.promptApplied": {
    params: { sequence: number; restoreRequestId?: string };
    result: void;
  };
}

export type AgentGuiOperation = keyof AgentGuiOperations;
export interface AgentGuiHostErrorDetails {
  message: string;
  code?: string;
  status?: number;
}

export type AgentGuiHostEvent =
  | { type: "dismissPopovers" }
  | {
      type: "locateSource";
      sessionId: string;
      turnId?: string;
      quote?: string;
      sequence: number;
    }
  | { type: "invalidate"; scope: "workspace"; generation?: number }
  | {
      type: "invalidate";
      scope: "session";
      sessionId: string;
      generation?: number;
    }
  | { type: "init"; snapshot: AgentGuiInitSnapshot }
  | {
      type: "appendPrompt";
      sequence: number;
      prompt: string;
      sessionId?: string;
      attachments?: Attachment[];
      restoreRequestId?: string;
    }
  | { type: "connection"; connected: boolean }
  | { type: "attachment.upload"; attachmentId: string; state: "uploaded" }
  | {
      type: "attachment.upload";
      attachmentId: string;
      state: "failed";
      message: string;
    };

/**
 * Local conversations as the runtime reads them. It owns nothing here: the
 * conversations outlive the GUI, which projects them while it is mounted.
 */
export interface LocalConversationSource {
  completions?: import("@multica/conversation-client/services").CompletionReadService;
  submissionStorage?: import("@multica/core/local-conversation/submission-storage").SubmissionStorage;
  submissionContext?(): Promise<{ generation: string; lifecycle: boolean }>;
  inspectSubmissions?(
    sessionId: string,
    requestIds: string[],
  ): Promise<
    | import("zod").infer<
        typeof import("@multica/core/local-conversation/submission").localSubmissionStatusesSchema
      >
    | null
  >;
  navigation?(
    sessionId: string,
  ): import("@multica/conversation-client/services").ConversationNavigation;
  /** Starts keeping a conversation current; the returned function is leaving it. */
  open(sessionId: string): () => void;
  /** The conversation with at least its first turns. */
  ensure(sessionId: string): Promise<ConversationModel>;
  /** The conversation as the daemon has it now, for a caller that must see what it just caused. */
  refresh(sessionId: string): Promise<ConversationModel>;
  /** Reads the next older page, if any. */
  loadOlder(sessionId: string): Promise<void>;
  /** Watches changes; events is empty for a change that is not new events. */
  observe(
    sessionId: string,
    listener: (events: readonly LocalV2Event[], change?: "navigation") => void
  ): () => void;
  conversation(sessionId: string): ConversationModel | undefined;
  /** What the GUI's session model needs from a loaded conversation. */
  detail(sessionId: string): AgentGuiSessionDetail | null;
}

export type AgentGuiCallOptions = { signal?: AbortSignal };
export type AgentGuiOperationInvoker = <M extends AgentGuiOperation>(
  method: M,
  params: AgentGuiOperations[M]["params"],
  options?: AgentGuiCallOptions
) => Promise<AgentGuiOperations[M]["result"]>;
export interface AgentGuiHostServices {
  queue: {
    prepare?(
      params: AgentGuiOperations["queue.enqueue"]["params"],
    ): Promise<AgentGuiOperations["queue.enqueue"]["params"]>;
    enqueue(
      params: AgentGuiOperations["queue.enqueue"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["queue.enqueue"]["result"]>;
    statuses(
      params: AgentGuiOperations["queue.statuses"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["queue.statuses"]["result"]>;
    update(
      params: AgentGuiOperations["queue.update"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["queue.update"]["result"]>;
  };
  interaction: {
    respond(
      params: AgentGuiOperations["interaction.respond"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["interaction.respond"]["result"]>;
  };
  workspace: {
    load(
      params: AgentGuiOperations["workspace.load"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["workspace.load"]["result"]>;
  };
  session: {
    load(
      params: AgentGuiOperations["session.load"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["session.load"]["result"]>;
    create(
      params: AgentGuiOperations["session.create"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["session.create"]["result"]>;
    send(
      params: AgentGuiOperations["session.send"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["session.send"]["result"]>;
    rename(
      params: AgentGuiOperations["session.rename"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["session.rename"]["result"]>;
    fork(
      params: AgentGuiOperations["session.fork"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["session.fork"]["result"]>;
    pin(
      params: AgentGuiOperations["session.pin"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["session.pin"]["result"]>;
    delete(
      params: AgentGuiOperations["session.delete"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["session.delete"]["result"]>;
    cancel(
      params: AgentGuiOperations["session.cancel"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["session.cancel"]["result"]>;
    compact(
      params: AgentGuiOperations["session.compact"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["session.compact"]["result"]>;
  };
  models: {
    list(
      params: AgentGuiOperations["models.list"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["models.list"]["result"]>;
  };
  usage: {
    read(
      params: AgentGuiOperations["usage.read"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["usage.read"]["result"]>;
  };
  context: {
    tasks(
      params: AgentGuiOperations["context.tasks"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["context.tasks"]["result"]>;
  };
  attachment: {
    metadata(
      params: AgentGuiOperations["attachment.metadata"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["attachment.metadata"]["result"]>;
    upload(
      params: AgentGuiOperations["attachment.upload"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["attachment.upload"]["result"]>;
    cancel(
      params: AgentGuiOperations["attachment.cancel"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["attachment.cancel"]["result"]>;
    retry(
      params: AgentGuiOperations["attachment.retry"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["attachment.retry"]["result"]>;
    forget(
      params: AgentGuiOperations["attachment.forget"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["attachment.forget"]["result"]>;
    url(
      params: AgentGuiOperations["attachment.url"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["attachment.url"]["result"]>;
    read(
      params: AgentGuiOperations["attachment.read"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["attachment.read"]["result"]>;
  };
  image: {
    url(
      params: AgentGuiOperations["image.url"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["image.url"]["result"]>;
    read(
      params: AgentGuiOperations["image.read"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["image.read"]["result"]>;
  };
  storage: {
    subscribe?(params: { key: string }, listener: (value: string | null) => void): () => void;
    read(
      params: AgentGuiOperations["storage.read"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["storage.read"]["result"]>;
    write(
      params: AgentGuiOperations["storage.write"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["storage.write"]["result"]>;
  };
  hostApi: {
    openTask(
      params: AgentGuiOperations["host.openTask"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["host.openTask"]["result"]>;
    openInternalResource(
      params: AgentGuiOperations["host.openInternalResource"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["host.openInternalResource"]["result"]>;
    openSide(
      params: AgentGuiOperations["host.openSide"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["host.openSide"]["result"]>;
    openResource(
      params: AgentGuiOperations["host.openResource"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["host.openResource"]["result"]>;
    showFileContextMenu(
      params: AgentGuiOperations["host.showFileContextMenu"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["host.showFileContextMenu"]["result"]>;
    locateSource(
      params: AgentGuiOperations["host.locateSource"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["host.locateSource"]["result"]>;
    selectSession(
      params: AgentGuiOperations["host.selectSession"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["host.selectSession"]["result"]>;
    showMessage(
      params: AgentGuiOperations["host.showMessage"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["host.showMessage"]["result"]>;
    promptApplied(
      params: AgentGuiOperations["host.promptApplied"]["params"],
      options?: AgentGuiCallOptions
    ): Promise<AgentGuiOperations["host.promptApplied"]["result"]>;
  };
  subscribe(listener: (event: AgentGuiHostEvent) => void): () => void;
}
