import type {
  AgentActivityComposerOptions,
  AgentActivityCreateSessionInput,
  AgentActivityMessage,
  AgentActivityMessageOrder,
  AgentActivityRailPlacement,
  AgentActivitySendInput,
  AgentActivitySession,
  AgentActivitySessionSettings
} from "@tutti-os/agent-activity-core";
import {
  createContext,
  useContext,
  type JSX,
  type PropsWithChildren
} from "react";
import type {
  AgentGUIComposerOptionsReader,
  AgentGUIComposerSettingsLane
} from "./agent-gui/agentGuiNode/controller/agentGuiController.providerHelpers";
import type { AgentComposerDraft } from "./agent-gui/agentGuiNode/model/agentGuiNodeTypes";
import type { AgentHostAgentSessionComposerSettings } from "./shared/contracts/dto";
import type { ConversationRowsModel } from "./state/ConversationRowsModel";
import type { AgentConversationServices } from "./state/conversation-services";
import { observer } from "mobx-react-lite";
export const ConversationServicesProvider = observer(
  ConversationServicesProviderRender
);

export type AgentActivitySessionMessages = Readonly<
  Record<string, readonly AgentActivityMessage[]>
>;

export interface AgentGUIWorkspaceLoadReader {
  readonly status: "idle" | "loading" | "ready" | "failed";
  readonly errorMessage: string | null;
}

export interface AgentGUITranscriptReader {
  messages(agentSessionId: string): readonly AgentActivityMessage[];
  turnIds(agentSessionId: string): readonly string[];
  turnMessages(
    agentSessionId: string,
    turnId: string
  ): readonly AgentActivityMessage[];
  hasOlderMessages(agentSessionId: string): boolean;
  conversationIds(): readonly string[];
}

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

export interface AgentActivityRuntimeUserProject {
  createdAtUnixMs: number;
  id: string;
  label: string;
  lastUsedAtUnixMs?: number;
  path: string;
  pinnedAtUnixMs: number;
  sectionKey: string;
  updatedAtUnixMs: number;
}

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
 * owned by the conversation services; this boundary contains
 * only the reads, metadata actions, uploads, diagnostics, and subscriptions
 * that remain host-owned.
 */
export interface AgentGUIConversationMutations {
  deleteSessions(input: {
    agentSessionIds: readonly string[];
  }): Promise<
    import("@multica/conversation-client/services").ConversationDeleteResult
  >;
  rename(input: {
    agentSessionId: string;
    title: string;
  }): Promise<AgentActivitySession>;
  setPinned(input: {
    agentSessionId: string;
    pinned: boolean;
  }): Promise<AgentActivitySession>;
  forkThroughTurn(input: {
    sourceAgentSessionId: string;
    turnId: string;
  }): Promise<{ targetAgentSessionId: string }>;
  /** The turns of a conversation being forked right now. */
  pendingForkTurnIds(
    agentSessionId: string | null | undefined
  ): readonly string[];
}

export interface AttachmentService {
  readSessionAttachmentSource?: (input: {
    workspaceId: string;
    attachmentId: string;
  }) => Promise<{ attachmentId: string; url: string }>;
  promptAttachmentsRemoved?: (
    before: AgentComposerDraft,
    after: AgentComposerDraft
  ) => void;
  promptContentUploadSupport?: {
    file?: boolean;
    image?: boolean;
  };
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
}

export interface ConversationDataService {
  refreshWorkspace(workspaceId: string): Promise<void>;
  loadOlder: (input: {
    workspaceId: string;
    agentSessionId: string;
  }) => Promise<void>;
  load(
    workspaceId: string,
    agentSessionId: string
  ): Promise<AgentActivitySession>;
  watch?(input: AgentActivityRuntimeEnsureSessionSynchronizedInput): () => void;
  loadAll(input: {
    workspaceId: string;
    agentSessionId: string;
  }): Promise<readonly AgentActivityMessage[]>;
}

export interface ConversationServices extends AgentConversationServices {
  conversations: ConversationDataService;

  attachments?: AttachmentService;

  /** Renames, pins and forks; each waits for the server. */
  mutations: AgentGUIConversationMutations;

  /** Each target's composer choices and native catalog loader. Also writes a conversation's settings. */
  composer: AgentGUIComposerOptionsReader &
    AgentGUIComposerSettingsLane &
    Pick<
      import("@multica/conversation-client/services").ComposerService<AgentActivityComposerOptions>,
      "load"
    >;

  /** Whether the conversation list has been read. */
  workspaceLoad: AgentGUIWorkspaceLoadReader;

  /** Each conversation's committed messages, for reading. */
  transcript: AgentGUITranscriptReader;

  conversationRows(
    id: string,
    options?: {
      workspaceId: string;
      userId?: string | null;
      workspaceRoot?: string;
      avoidGroupingEdits?: boolean;
    }
  ): ConversationRowsModel;

  /** Stops, answers to interactions and edit retries. */
  prompts: AgentConversationServices["prompts"];

  /** Stable platform plumbing supplied by this instance's provider. */
  environment?: { origin?: string; projectPathIsRemote?: boolean };

  diagnostics?: {
    devDiagnosticConsoleSink?: boolean;
    trackSettingsProjectChange?(
      input: AgentActivityRuntimeTrackSettingsProjectChangeInput
    ): Promise<void>;

    trackDraftComposerSettingsChange?(
      input: AgentActivityRuntimeTrackDraftComposerSettingsChangeInput
    ): Promise<void>;

    reportDiagnostic?(
      input: AgentActivityRuntimeDiagnosticInput
    ): Promise<void> | void;
  };
}

const ConversationServicesContext = createContext<ConversationServices | null>(
  null
);

function createTestConversationServicesHolder(): {
  get: () => ConversationServices | null;
  set: (runtime: ConversationServices | null) => void;
} {
  let runtime: ConversationServices | null = null;
  return {
    get: () => runtime,
    set: (nextRuntime) => {
      runtime = nextRuntime;
    }
  };
}

const testConversationServicesHolder = createTestConversationServicesHolder();

export interface ConversationServicesProviderProps extends PropsWithChildren {
  runtime?: ConversationServices | null;
}

function ConversationServicesProviderRender({
  children,
  runtime
}: ConversationServicesProviderProps): JSX.Element {
  return (
    <ConversationServicesContext.Provider value={runtime ?? null}>
      {children}
    </ConversationServicesContext.Provider>
  );
}

export function useConversationServices(): ConversationServices {
  const runtime =
    useContext(ConversationServicesContext) ?? getTestConversationServices();
  if (!runtime) {
    throw new Error("ConversationServicesProvider is required.");
  }
  return runtime;
}

export function useOptionalConversationServices(): ConversationServices | null {
  return (
    useContext(ConversationServicesContext) ?? getTestConversationServices()
  );
}

export function resetConversationServicesForTests(): void {
  if (process.env.NODE_ENV === "test") {
    testConversationServicesHolder.set(null);
  }
}

export function setConversationServicesForTests(
  runtime: ConversationServices | null
): void {
  if (process.env.NODE_ENV === "test") {
    testConversationServicesHolder.set(runtime);
  }
}

function getTestConversationServices(): ConversationServices | null {
  if (process.env.NODE_ENV !== "test") {
    return null;
  }
  if (typeof window === "undefined") {
    return null;
  }
  const explicitRuntime = getExplicitWindowTestConversationServices();
  if (explicitRuntime) {
    return explicitRuntime;
  }
  const testRuntimeOverride = testConversationServicesHolder.get();
  if (testRuntimeOverride) {
    return testRuntimeOverride;
  }
  const testRuntime = (
    window as unknown as Window & {
      agentGUIRuntime?: ConversationServices;
    }
  ).agentGUIRuntime;
  return testRuntime ?? null;
}

function getExplicitWindowTestConversationServices(): ConversationServices | null {
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
  return (testDescriptor.value as ConversationServices | undefined) ?? null;
}
