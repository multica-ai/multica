import { z } from "zod";
import type { RuntimeModel } from "../types";

export const LOCAL_REASONING_EFFORTS = [
  "none",
  "minimal",
  "low",
  "medium",
  "high",
  "xhigh",
  "max",
  "ultra",
] as const;
export type LocalReasoningEffort = (typeof LOCAL_REASONING_EFFORTS)[number];
export function isLocalReasoningEffort(
  value: unknown,
): value is LocalReasoningEffort {
  return LOCAL_REASONING_EFFORTS.some((effort) => effort === value);
}
export function localReasoningLevels(
  models: RuntimeModel[],
  model: string | null | undefined,
) {
  return (
    models.find((entry) =>
      model ? entry.id === model : entry.default || entry.isDefault,
    )?.thinking?.supported_levels ?? []
  );
}
export type LocalProviderId = "codex" | "claude";
export const LOCAL_PERMISSION_MODES = [
  "default",
  "auto",
  "full-access",
] as const;
export type LocalPermissionMode = (typeof LOCAL_PERMISSION_MODES)[number];
export function isLocalPermissionMode(
  value: unknown,
): value is LocalPermissionMode {
  return LOCAL_PERMISSION_MODES.some((mode) => mode === value);
}
export const CREATE_TASK_MENTION_ID = "multica:create-task";
export function isTaskCreationReference(value: {
  kind?: unknown;
  id?: unknown;
}): boolean {
  return value.kind === "issue" && value.id === CREATE_TASK_MENTION_ID;
}
export interface LocalConversationScope {
  userId: string;
  workspaceId: string;
}
export interface LocalFolderProject {
  id: string;
  title: string;
  path: string;
  createdAt: string;
}
export interface LocalConversationEvent {
  seq: number;
  type: "text" | "thinking" | "tool_use" | "tool_result" | "status" | "error";
  content?: string;
  tool?: string;
  input?: Record<string, unknown>;
  output?: string;
  call_id?: string;
  created_at: string;
}
export interface LocalBackgroundTask {
  id: string;
  description: string;
  status: "running" | "completed" | "failed" | "cancelled";
  ambient?: boolean;
  summary?: string;
  createdAt?: string;
  toolUseId?: string;
  taskType?: string;
  progressSummary?: string;
  lastToolName?: string;
  usage?: { totalTokens: number; toolUses: number; durationMs: number };
  output?: string;
  outputTruncated?: boolean;
  updatedAt?: string;
  completedAt?: string;
  completionSource?: "notification" | "update" | "snapshot" | "stop";
  delivery?: {
    status: "pending" | "requested" | "delivered" | "interrupted";
    turnId?: string;
  };
}
export interface LocalConversationTurn {
  inputMessageId?: string;
  backgroundDelivery?: { sourceTurnId: string; taskIds: string[] };
  backgroundTasks?: LocalBackgroundTask[];
  questions?: LocalUserQuestion[];
  requestId?: string;
  id: string;
  status: "running" | "completed" | "failed" | "cancelled";
  createdAt: string;
  completedAt?: string;
  error?: string;
  events: LocalConversationEvent[];
  remote?: {
    agentId: string;
    agentName: string;
    sessionId?: string;
    taskId?: string;
    contextThroughMessageId?: string;
    queueOwned?: boolean;
  };
}
export interface LocalUserQuestionInput {
  request_id: string;
  questions: {
    id: string;
    header?: string;
    question: string;
    options: { label: string; description?: string }[];
    multiSelect: boolean;
    allowFreeText: boolean;
  }[];
}
export interface LocalUserQuestion extends LocalUserQuestionInput {
  status: "pending" | "answered" | "cancelled";
  createdAt: string;
  updatedAt: string;
  answers?: Record<string, string | string[]>;
}
export interface LocalUserQuestionResult {
  answersByQuestionId: Record<string, string | string[]>;
}
export interface LocalConversationMessage {
  steered?: boolean;
  origin?: { conversationId: string; requestId: string };
  id: string;
  role: "user" | "assistant";
  content: string;
  contextContent?: string;
  turnId: string;
  createdAt: string;
  failed?: boolean;
  attachmentIds?: string[];
}
/** Keep internal image prompt markers out of displayed history and previews. */
export function localMessageDisplayContent(
  message?: Pick<
    LocalConversationMessage,
    "role" | "content" | "attachmentIds"
  >,
): string {
  if (!message) return "";
  if (message.role !== "user" || !message.attachmentIds?.length)
    return message.content;
  const content = message.content.replace(
    /(^|\n\n)\[Image attachment: [^\r\n]*\](?=\n\n|$)/g,
    "",
  );
  return content === message.content ? content : content.replace(/^\n\n/, "");
}
export interface LocalConversation {
  promptQueue?: LocalPromptQueue;
  side?: {
    sourceId: string;
    turnId?: string;
    quote?: string;
    requestId: string;
    context: { role: string; content: string }[];
    pending?: LocalSidePrompt;
  };
  id: string;
  title: string;
  provider: LocalProviderId;
  cwd: string;
  pinned: boolean;
  titleMode?: "auto" | "manual";
  titleGeneration?: "pending" | "complete" | "failed";
  archived: boolean;
  model: string | null;
  reasoningEffort?: LocalReasoningEffort | null;
  permissionMode?: LocalPermissionMode;
  providerSessionId?: string;
  providerContext?: { sessionId: string; throughMessageId: string };
  forkedFrom?: string;
  importedFrom?: { provider: LocalProviderId; sessionId: string };
  forkedFromTurnId?: string;
  forkedTurnId?: string;
  forkMode?: "native" | "saved-context";
  forkRequestId?: string;
  projectId?: string | null;
  createdAt: string;
  updatedAt: string;
  messages: LocalConversationMessage[];
  turns: LocalConversationTurn[];
}
export interface LocalImportCandidate {
  id: string;
  provider: LocalProviderId;
  title: string;
  cwd: string;
  createdAt: string;
  updatedAt: string;
  importedId?: string;
}
export type LocalContextReference =
  | { kind: "session" | "issue" | "agent"; id: string }
  | { kind: "member"; id: string; workspaceId: string };

export interface LocalSidePrompt {
  origin?: { conversationId: string; requestId: string };
  content: string;
  attachmentIds?: string[];
  references?: LocalContextReference[];
  model?: string;
  reasoningEffort?: LocalReasoningEffort | null;
  permissionMode?: LocalPermissionMode;
  requestId: string;
}
export interface LocalQueuedPrompt extends Omit<LocalSidePrompt, "model"> {
  model?: string | null;
  prompt?: string;
  display: LocalQueueDisplayPrompt;
  agents?: { id: string; name: string }[];
}
/** Serializable composer data; the main process has no GUI engine dependency. */
export interface LocalQueueDisplayPrompt {
  id: string;
  clientSubmitId?: string;
  createdAtUnixMs: number;
  displayPrompt?: string;
  content: readonly LocalPromptBlock[];
  runtimeContent?: readonly LocalPromptBlock[];
  requiredSettingsPatch?: { browserUse?: boolean; computerUse?: boolean };
}
export interface LocalPromptBlock {
  type: "text" | "image" | "file" | "skill" | "mention" | "connector";
  text?: string;
  mimeType?: string;
  data?: string;
  url?: string;
  attachmentId?: string;
  name?: string;
  path?: string;
  connectorKey?: string;
  uri?: string;
  hostPath?: string;
  uploadStatus?: string;
  assetId?: string;
  kind?: string;
  sizeBytes?: number;
}
export interface LocalPromptQueue {
  submissions?: import("./submission").LocalSubmission[];
  storeGeneration?: string;
  snapshotSeq?: number;
  revision: number;
  paused: boolean;
  dispatchingRequestId?: string;
  error?: string;
  items: LocalQueuedPrompt[];
}
export interface LocalPreviewLocation {
  root: string;
  path: string;
  name: string;
  kind: "directory" | "file";
}
/**
 * A previewable file. The host returns `fileRef`, a short-lived grant for the
 * HTTP file endpoint; Desktop fills `url` (images, PDF) or `text` from it.
 */
export interface LocalPreviewFile {
  name: string;
  path: string;
  root?: string;
  mimeType: string;
  size: number;
  fileRef?: string;
  url?: string;
  text?: string;
  truncated: boolean;
}
export interface LocalContextUsage {
  usedTokens: number | null;
  limitTokens: number | null;
  model: string | null;
  updatedAt: string | null;
}
export interface LocalUsageWindow {
  id: string;
  label: string;
  usedPercent: number;
  resetsAt: string | null;
}
export interface LocalAccountUsage {
  status: "available" | "unavailable" | "error";
  plan: string | null;
  windows: LocalUsageWindow[];
  updatedAt: string | null;
  stale?: boolean;
}
export interface LocalProviderUsage {
  provider: LocalProviderId;
  context: LocalContextUsage;
  account: LocalAccountUsage;
}

const sideId = z.string().min(1).max(160);
const sidePath = z.string().min(1).max(4096);
export const sideWorkspaceTabSchema = z.discriminatedUnion("kind", [
  z
    .object({
      id: sideId,
      kind: z.literal("web"),
      url: z
        .string()
        .url()
        .max(8192)
        .refine((value) => {
          const url = new URL(value);
          return (
            ["http:", "https:"].includes(url.protocol) &&
            !url.username &&
            !url.password
          );
        }),
      title: z.string().max(4096).optional(),
      loading: z.boolean().optional(),
      openedBy: z.enum(["user", "agent"]).optional(),
      turnId: sideId.optional(),
      keep: z.boolean().optional(),
    })
    .strict(),
  z
    .object({
      id: sideId,
      kind: z.literal("file"),
      root: sidePath,
      path: sidePath,
      fileKind: z.enum(["file", "directory"]),
      selectedPath: sidePath.optional(),
      line: z.number().int().positive().optional(),
    })
    .strict(),
  z.object({ id: sideId, kind: z.literal("side"), sessionId: sideId }).strict(),
]);
export const sideWorkspaceSyncSchema = z
  .object({
    conversationId: sideId,
    revision: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER),
    panelOpen: z.boolean(),
    visible: z.boolean().default(false),
    selectedTabId: sideId.nullable(),
    tabs: z.array(sideWorkspaceTabSchema).max(20),
  })
  .strict()
  .superRefine((value, ctx) => {
    if (
      new Set(value.tabs.map((tab) => tab.id)).size !== value.tabs.length ||
      (value.selectedTabId !== null &&
        !value.tabs.some((tab) => tab.id === value.selectedTabId))
    )
      ctx.addIssue({
        code: "custom",
        message: "Invalid Side selection or duplicate tab",
      });
  });
export type SideWorkspaceTab = z.infer<typeof sideWorkspaceTabSchema>;
export type SideWorkspaceSnapshot = z.infer<typeof sideWorkspaceSyncSchema>;
