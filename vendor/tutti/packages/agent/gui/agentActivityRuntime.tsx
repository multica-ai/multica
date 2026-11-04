import {
  createContext,
  useContext,
  useMemo,
  useSyncExternalStore,
  type JSX,
  type PropsWithChildren
} from "react";
import type {
  AgentActivityCollaborationRun,
  AgentActivityComposerOptions,
  AgentActivityMessage,
  AgentActivityCreateSessionInput,
  AgentActivityDeleteSessionInput,
  AgentActivityDeleteSessionResult,
  AgentActivityMessageOrder,
  AgentActivityMessagePage,
  AgentActivityRailPlacement,
  AgentActivityRenameSessionInput,
  AgentActivitySendInput,
  AgentActivitySession,
  AgentActivitySessionSettings,
  AgentActivitySetCollaborationAdoptionInput,
  AgentActivitySnapshot,
  AgentActivitySnapshotListener,
  AgentSessionEngine
} from "@tutti-os/agent-activity-core";
import type { AgentComposerDraft } from "./agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type { AgentHostAgentSessionComposerSettings } from "./shared/contracts/dto";
import type {
  AgentConversationRailDeleteSessionsBatchInput,
  AgentConversationRailDeleteSessionsBatchResult,
  AgentConversationRailListPinnedSessionsPageInput,
  AgentConversationRailListSessionSectionPageInput,
  AgentConversationRailListSessionSectionsInput,
  AgentConversationRailListSessionsPageInput,
  AgentConversationRailSessionPage,
  AgentConversationRailSessionSection,
  AgentConversationRailSessionSectionDeletionCandidates,
  AgentConversationRailSessionSectionScopeInput,
  AgentConversationRailSessionSectionsResult,
  AgentConversationRailSessionsPageResult,
  AgentConversationRailUserProject
} from "./agentConversationRailContracts";
import { useEngineSelector } from "./shared/engine/useEngineSelector";

const EMPTY_SESSION_MESSAGES: readonly AgentActivityMessage[] = [];

export type AgentActivitySessionMessages = Readonly<
  Record<string, readonly AgentActivityMessage[]>
>;

export interface AgentActivityRuntimeUpdateSessionSettingsResult {
  agentSessionId: string;
  settings: AgentHostAgentSessionComposerSettings;
  session: AgentActivitySession;
}

export interface AgentActivityRuntimeListSessionMessagesInput {
  afterVersion?: number;
  beforeVersion?: number;
  cache?: boolean;
  agentSessionId: string;
  limit?: number;
  order?: AgentActivityMessageOrder;
  signal?: AbortSignal;
  workspaceId: string;
}

export interface AgentActivityRuntimeListGeneratedFilesInput {
  agentTargetIds?: readonly string[];
  cursor?: string;
  limit?: number;
  query?: string;
  sectionKey: string;
  signal?: AbortSignal;
  workspaceId: string;
}

export type AgentActivityRuntimeListSessionsPageInput =
  AgentConversationRailListSessionsPageInput;
export type AgentActivityRuntimeSessionPageResult =
  AgentConversationRailSessionsPageResult;
export type AgentActivityRuntimeListSessionSectionsInput =
  AgentConversationRailListSessionSectionsInput;
export type AgentActivityRuntimeListSessionSectionPageInput =
  AgentConversationRailListSessionSectionPageInput;
export type AgentActivityRuntimeListPinnedSessionsPageInput =
  AgentConversationRailListPinnedSessionsPageInput;
export type AgentActivityRuntimeUserProject = AgentConversationRailUserProject;
export type AgentActivityRuntimeSessionSection =
  AgentConversationRailSessionSection;
export type AgentActivityRuntimeSessionPage = AgentConversationRailSessionPage;
export type AgentActivityRuntimeSessionSectionsResult =
  AgentConversationRailSessionSectionsResult;

export interface AgentActivityRuntimeGeneratedFile {
  label: string;
  path: string;
}

export interface AgentActivityRuntimeGeneratedFileList {
  entries: AgentActivityRuntimeGeneratedFile[];
  hasMore?: boolean;
  nextCursor?: string;
  workspaceId: string;
}

export interface AgentActivityRuntimeEnsureSessionSynchronizedInput {
  afterVersion?: number;
  agentSessionId: string;
  onError?: (error: unknown) => void;
  workspaceId: string;
}

export interface AgentActivityRuntimeSetSessionPinnedInput {
  agentSessionId: string;
  pinned: boolean;
  workspaceId: string;
}

export interface AgentActivityRuntimeTrackSettingsProjectChangeInput {
  action: "clear" | "create_new" | "import_directory" | "select_existing";
  agentSessionId: string | null;
  provider?: string | null;
  workspaceId: string;
}

export interface AgentActivityRuntimeGetComposerOptionsInput {
  agentSessionId?: string | null;
  agentTargetId: string;
  cwd?: string | null;
  force?: boolean;
  waitForFreshModelCatalog?: boolean;
  provider?: string;
  section?: "full" | "core" | "capabilities" | "connectors";
  settings?: AgentHostAgentSessionComposerSettings | null;
  workspaceId: string;
}

export interface AgentActivityRuntimeUpdateSessionSettingsInput {
  agentSessionId: string;
  signal?: AbortSignal;
  settings: AgentHostAgentSessionComposerSettings;
  workspaceId: string;
}

export interface AgentActivityRuntimeTrackDraftComposerSettingsChangeInput {
  nextSettings: AgentHostAgentSessionComposerSettings;
  previousSettings: AgentHostAgentSessionComposerSettings;
  provider: string;
  workspaceId: string;
}

export interface AgentActivityRuntimeDiagnosticInput {
  details?: Record<string, unknown>;
  event: string;
  level?: "debug" | "info" | "warn" | "error";
  source?: string;
  workspaceId?: string | null;
}

interface AgentActivityRuntimeActivateSessionInputBase {
  activationId: string;
  agentSessionId: string;
  capabilityRefs?: AgentActivityCreateSessionInput["capabilityRefs"];
  cwd?: string;
  initialContent?: AgentActivitySendInput["content"];
  /** 仅展示用首轮文本(bundle 折叠成一个 chip);initialContent 仍带展开后的文件。 */
  initialDisplayPrompt?: string | null;
  isolation?: AgentActivityCreateSessionInput["isolation"];
  modelExplicit?: boolean;
  railPlacement?: AgentActivityRailPlacement;
  reasoningEffortExplicit?: boolean;
  submitDiagnostics?: AgentActivitySendInput["submitDiagnostics"];
  settings?: AgentActivitySessionSettings;
  title?: string;
  visible?: boolean;
  workspaceId: string;
  signal?: AbortSignal;
}

export type AgentActivityRuntimeActivateSessionInput =
  | (AgentActivityRuntimeActivateSessionInputBase & {
      agentTargetId: string;
      clientSubmitId: string;
      initialGoalControl?: AgentActivityCreateSessionInput["initialGoalControl"];
      initialTuttiModeActivation?: AgentActivityCreateSessionInput["initialTuttiModeActivation"];
      mode: "new";
    })
  | (AgentActivityRuntimeActivateSessionInputBase & {
      agentTargetId?: string | null;
      clientSubmitId?: never;
      mode: "existing";
    });

export interface AgentActivityRuntimeUnactivateSessionInput {
  agentSessionId: string;
  workspaceId: string;
}

export interface AgentActivityRuntimeReadSessionAttachmentInput {
  agentSessionId: string;
  attachmentId: string;
  workspaceId: string;
}

export interface AgentActivityRuntimeReadPromptAssetInput {
  agentSessionId?: string | null;
  assetId?: string | null;
  hostPath?: string | null;
  kind?: string | null;
  mimeType: string;
  name?: string | null;
  path?: string | null;
  sha256?: string | null;
  uploadStatus?: string | null;
  uri?: string | null;
  workspaceId: string;
}

export type AgentActivityRuntimePromptContentBlock =
  AgentActivitySendInput["content"][number] & {
    assetId?: string;
    hostPath?: string;
    kind?: string;
    path?: string;
    sizeBytes?: number;
    uploadStatus?: string;
    uri?: string;
  };

export interface AgentActivityRuntimeUploadPromptContentInput {
  content: AgentActivityRuntimePromptContentBlock[];
  workspaceId: string;
}

export interface AgentActivityRuntimeUploadPromptContentResult {
  content: AgentActivityRuntimePromptContentBlock[];
}

/**
 * Dedicated host boundary for turning an in-memory text paste into a prepared
 * prompt asset. The runtime owns persistence and returns one provider-readable
 * locator; AgentGUI must not infer this capability from generic file-upload
 * support.
 */
export interface AgentActivityRuntimeStagePastedTextInput {
  name: string;
  text: string;
  workspaceId: string;
}

/**
 * A prepared long-text asset. Local hosts return a path; remote/shared hosts
 * return the same URL-backed attachment metadata used by ordinary prompt-file
 * upload. Exactly one of `path` and `url` must be present.
 */
export type AgentActivityRuntimeStagePastedTextResult =
  | {
      name: string;
      path: string;
      url?: never;
      assetId?: never;
      mimeType?: string;
      sizeBytes: number;
      uploadStatus?: never;
      uri?: never;
    }
  | {
      name: string;
      path?: never;
      url: string;
      assetId?: string;
      mimeType?: string;
      sizeBytes: number;
      uploadStatus?: string;
      uri?: string;
    };

export type AgentActivityRuntimeSessionSectionScopeInput =
  AgentConversationRailSessionSectionScopeInput;
export type AgentActivityRuntimeSessionSectionDeletionCandidates =
  AgentConversationRailSessionSectionDeletionCandidates;
export type AgentActivityRuntimeDeleteSessionsBatchInput =
  AgentConversationRailDeleteSessionsBatchInput;
export type AgentActivityRuntimeDeleteSessionsBatchResult =
  AgentConversationRailDeleteSessionsBatchResult;

export interface AgentActivityRuntimeSessionAttachment {
  attachmentId: string;
  mimeType: string;
  name?: string;
  data: string;
}

export interface AgentActivityRuntimePromptAsset {
  assetId?: string;
  hostPath?: string;
  kind?: string;
  mimeType: string;
  name?: string;
  path: string;
  uploadStatus?: string;
  uri?: string;
  data: string;
}

/**
 * Host runtime surface consumed by AgentGUI. Session lifecycle writes are
 * owned by the workspace {@link AgentSessionEngine}; this boundary contains
 * only the reads, metadata actions, uploads, diagnostics, and subscriptions
 * that remain host-owned.
 */
export interface AgentGUIRuntime {
  /**
   * Reads the next older page into the session itself, which then updates through
   * its usual snapshot. A runtime with this owns paging; versions need not page.
   */
  loadOlderSessionMessages?: (input: { workspaceId: string; agentSessionId: string }) => Promise<void>;
  /** Reads an attachment as a URL; preferred over readSessionAttachment when present. */
  readSessionAttachmentSource?: (input: { workspaceId: string; attachmentId: string }) => Promise<{ attachmentId: string; url: string }>;
  /** The composer dropped attachments between these two drafts. */
  promptAttachmentsRemoved?: (before: AgentComposerDraft, after: AgentComposerDraft) => void;
  /**
   * Stable identity of this runtime instance (e.g. a local origin vs a
   * shared/room origin). The runtime owns one session engine per workspace and
   * that engine verifies this origin as part of its injected identity. Runtime
   * consumers resolve only through the nearest React provider; module-global
   * runtime lookup and last-mounted fallback are forbidden. An absent origin
   * means the canonical local origin.
   */
  origin?: string;
  /**
   * Enables the Codex-aligned in-memory conversation Activity View. Missing or
   * false fails closed so external hosts opt in explicitly.
   */
  conversationActivityViewEnabled?: boolean;
  /**
   * Host query limits for the Conversation Rail. Omit when the backend accepts
   * AgentGUI's default limits.
   */
  conversationRailQueryLimits?: {
    sectionRefreshLimitMax: number;
  };
  /**
   * The session cwd is not resolvable on the local filesystem (e.g. a
   * shared/cloud sandbox not mounted locally), so AgentGUI must not run its
   * local stat-based "working directory missing" existence check — it would
   * always false-positive. Absent/false (default) => local, legacy behaviour.
   * Only that one guard is gated; project selection/listing is unaffected.
   */
  projectPathIsRemote?: boolean;
  promptContentUploadSupport?: {
    file?: boolean;
    image?: boolean;
  };
  /** Set false to suppress AgentGUI diagnostics in development consoles. */
  devDiagnosticConsoleSink?: boolean;
  deleteSession(
    input: AgentActivityDeleteSessionInput
  ): Promise<AgentActivityDeleteSessionResult>;
  getSession(
    workspaceId: string,
    agentSessionId: string
  ): Promise<AgentActivitySession>;
  getComposerOptions(
    input: AgentActivityRuntimeGetComposerOptionsInput
  ): Promise<AgentActivityComposerOptions>;
  getSnapshot(workspaceId: string): AgentActivitySnapshot;
  getSessionEngine(workspaceId: string): AgentSessionEngine;
  listSessionMessages(
    input: AgentActivityRuntimeListSessionMessagesInput
  ): Promise<AgentActivityMessagePage>;
  listAgentGeneratedFiles?(
    input: AgentActivityRuntimeListGeneratedFilesInput
  ): Promise<AgentActivityRuntimeGeneratedFileList>;
  listSessionsPage?(
    input: AgentActivityRuntimeListSessionsPageInput
  ): Promise<AgentActivityRuntimeSessionPageResult>;
  listSessionSections?(
    input: AgentActivityRuntimeListSessionSectionsInput
  ): Promise<AgentActivityRuntimeSessionSectionsResult>;
  listSessionSectionPage?(
    input: AgentActivityRuntimeListSessionSectionPageInput
  ): Promise<AgentActivityRuntimeSessionSection>;
  listSessionSectionDeletionCandidates?(
    input: AgentActivityRuntimeSessionSectionScopeInput
  ): Promise<AgentActivityRuntimeSessionSectionDeletionCandidates>;
  deleteSessionsBatch?(
    input: AgentActivityRuntimeDeleteSessionsBatchInput
  ): Promise<AgentActivityRuntimeDeleteSessionsBatchResult>;
  listPinnedSessionsPage?(
    input: AgentActivityRuntimeListPinnedSessionsPageInput
  ): Promise<AgentActivityRuntimeSessionPage>;
  load(
    workspaceId: string,
    signal?: AbortSignal
  ): Promise<AgentActivitySnapshot>;
  ensureSessionSynchronized?(
    input: AgentActivityRuntimeEnsureSessionSynchronizedInput
  ): () => void;
  uploadPromptContent?(
    input: AgentActivityRuntimeUploadPromptContentInput
  ): Promise<AgentActivityRuntimeUploadPromptContentResult>;
  stagePastedText?(
    input: AgentActivityRuntimeStagePastedTextInput
  ): Promise<AgentActivityRuntimeStagePastedTextResult>;
  readSessionAttachment?(
    input: AgentActivityRuntimeReadSessionAttachmentInput
  ): Promise<AgentActivityRuntimeSessionAttachment>;
  readPromptAsset?(
    input: AgentActivityRuntimeReadPromptAssetInput
  ): Promise<AgentActivityRuntimePromptAsset>;
  renameSession(
    input: AgentActivityRenameSessionInput
  ): Promise<AgentActivitySession>;
  /**
   * Record whether a collaboration outcome was adopted.
   * Optional; hosts without support omit it and adoption controls stay hidden.
   */
  setCollaborationAdoption?(
    input: AgentActivitySetCollaborationAdoptionInput
  ): Promise<AgentActivityCollaborationRun>;
  setSessionPinned(
    input: AgentActivityRuntimeSetSessionPinnedInput
  ): Promise<AgentActivitySession>;
  trackSettingsProjectChange?(
    input: AgentActivityRuntimeTrackSettingsProjectChangeInput
  ): Promise<void>;
  trackDraftComposerSettingsChange?(
    input: AgentActivityRuntimeTrackDraftComposerSettingsChangeInput
  ): Promise<void>;
  reportDiagnostic?(
    input: AgentActivityRuntimeDiagnosticInput
  ): Promise<void> | void;
  subscribeSessionEvents(
    workspaceId: string,
    listener: (event: unknown) => void
  ): () => void;
  subscribe(
    workspaceId: string,
    listener: AgentActivitySnapshotListener
  ): () => void;
}

const AgentGUIRuntimeContext = createContext<AgentGUIRuntime | null>(null);

function createTestAgentGUIRuntimeHolder(): {
  get: () => AgentGUIRuntime | null;
  set: (runtime: AgentGUIRuntime | null) => void;
} {
  let runtime: AgentGUIRuntime | null = null;
  return {
    get: () => runtime,
    set: (nextRuntime) => {
      runtime = nextRuntime;
    }
  };
}

const testAgentGUIRuntimeHolder = createTestAgentGUIRuntimeHolder();

export interface AgentGUIRuntimeProviderProps extends PropsWithChildren {
  runtime?: AgentGUIRuntime | null;
}

export function AgentGUIRuntimeProvider({
  children,
  runtime
}: AgentGUIRuntimeProviderProps): JSX.Element {
  return (
    <AgentGUIRuntimeContext.Provider value={runtime ?? null}>
      {children}
    </AgentGUIRuntimeContext.Provider>
  );
}

export function useAgentGUIRuntime(): AgentGUIRuntime {
  const runtime =
    useContext(AgentGUIRuntimeContext) ?? getTestAgentGUIRuntime();
  if (!runtime) {
    throw new Error(
      "AgentGUIRuntimeProvider is missing an AgentGUIRuntime instance."
    );
  }
  return runtime;
}

export function useOptionalAgentGUIRuntime(): AgentGUIRuntime | null {
  return useContext(AgentGUIRuntimeContext) ?? getTestAgentGUIRuntime();
}

export function useAgentActivitySnapshot(
  workspaceId: string
): AgentActivitySnapshot {
  const runtime = useAgentGUIRuntime();
  const normalizedWorkspaceId = workspaceId.trim();
  const workspaceStore = useMemo(
    () => ({
      getSnapshot: () => runtime.getSnapshot(normalizedWorkspaceId),
      subscribe: (listener: () => void) =>
        runtime.subscribe(normalizedWorkspaceId, listener)
    }),
    [normalizedWorkspaceId, runtime]
  );
  return useSyncExternalStore(
    workspaceStore.subscribe,
    workspaceStore.getSnapshot,
    workspaceStore.getSnapshot
  );
}

export function useAgentActivitySessionMessages(
  workspaceId: string,
  agentSessionIds: readonly (string | null | undefined)[]
): AgentActivitySessionMessages {
  const runtime = useAgentGUIRuntime();
  const normalizedWorkspaceId = workspaceId.trim();
  const sessionIdsKey = agentSessionIds
    .map((agentSessionId) => agentSessionId?.trim() ?? "")
    .filter(Boolean)
    .join("\u0000");
  const normalizedSessionIds = useMemo(
    () => [...new Set(sessionIdsKey.split("\u0000").filter(Boolean))],
    [sessionIdsKey]
  );
  const workspaceStore = useMemo(
    () => ({
      getSnapshot: () => runtime.getSnapshot(normalizedWorkspaceId),
      subscribe: (listener: () => void) =>
        runtime.subscribe(normalizedWorkspaceId, listener)
    }),
    [normalizedWorkspaceId, runtime]
  );
  const selectMessages = useMemo(
    () => createSessionMessagesSelector(normalizedSessionIds),
    [normalizedSessionIds]
  );
  return useEngineSelector(workspaceStore, selectMessages);
}

export function resetAgentGUIRuntimeForTests(): void {
  if (process.env.NODE_ENV === "test") {
    testAgentGUIRuntimeHolder.set(null);
  }
}

export function setAgentGUIRuntimeForTests(
  runtime: AgentGUIRuntime | null
): void {
  if (process.env.NODE_ENV === "test") {
    testAgentGUIRuntimeHolder.set(runtime);
  }
}

function createSessionMessagesSelector(
  agentSessionIds: readonly string[]
): (snapshot: AgentActivitySnapshot) => AgentActivitySessionMessages {
  let previous: AgentActivitySessionMessages = {};
  return (snapshot) => {
    const next = Object.fromEntries(
      agentSessionIds.map((agentSessionId) => [
        agentSessionId,
        snapshot.sessionMessagesById[agentSessionId] ?? EMPTY_SESSION_MESSAGES
      ])
    );
    if (
      Object.keys(previous).length === agentSessionIds.length &&
      agentSessionIds.every(
        (agentSessionId) => previous[agentSessionId] === next[agentSessionId]
      )
    ) {
      return previous;
    }
    previous = next;
    return previous;
  };
}

function getTestAgentGUIRuntime(): AgentGUIRuntime | null {
  if (process.env.NODE_ENV !== "test") {
    return null;
  }
  if (typeof window === "undefined") {
    return null;
  }
  const explicitRuntime = getExplicitWindowTestAgentGUIRuntime();
  if (explicitRuntime) {
    return explicitRuntime;
  }
  const testRuntimeOverride = testAgentGUIRuntimeHolder.get();
  if (testRuntimeOverride) {
    return testRuntimeOverride;
  }
  const testRuntime = (
    window as unknown as Window & {
      agentGUIRuntime?: AgentGUIRuntime;
    }
  ).agentGUIRuntime;
  return testRuntime ?? null;
}

function getExplicitWindowTestAgentGUIRuntime(): AgentGUIRuntime | null {
  if (process.env.NODE_ENV !== "test" || typeof window === "undefined") {
    return null;
  }
  const testDescriptor = Object.getOwnPropertyDescriptor(
    window,
    "agentGUIRuntime"
  );
  if (!testDescriptor || !("value" in testDescriptor)) {
    return null;
  }
  return (testDescriptor.value as AgentGUIRuntime | undefined) ?? null;
}
