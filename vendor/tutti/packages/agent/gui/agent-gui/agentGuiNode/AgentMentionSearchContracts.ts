import type { AgentContextMentionItem } from "./agentRichText/agentFileMentionExtension";
import type { AgentContextMentionProvider } from "./agentContextMentionProvider";
import { AGENT_CONTEXT_MENTION_PROVIDER_IDS } from "./agentContextMentionProvider";
import type { AgentMentionSearchDiagnosticLog } from "./agentMentionSearchDiagnostics";
import type { RuntimeDiagnosticsDetailValue } from "../../shared/contracts/dto/debug";
import { AGENT_MENTION_FILTER_TAB_ORDER } from "./agentMentionSearchHelpers";
import { agentMentionFilterLabel } from "./AgentMentionLabels";
import type {
  MentionPaletteGroup,
  MentionPaletteState
} from "@tutti-os/ui-rich-text/at-panel";

export type AgentMentionFilterId =
  | "session"
  | "file"
  | "issue"
  | "agent"
  | "member"
  | "app";
export type AgentMentionStaticGroupId =
  | "apps"
  | "agents"
  | "members"
  | "files"
  | "opened_files"
  | "agent_generated_files"
  | "my_sessions"
  | "collab_sessions"
  | "issues";
export type AgentMentionIssueTopicGroupId = `issue-topic:${string}`;
export type AgentMentionProvenanceGroupId = `agent:${string}`;
export type AgentMentionMemberGroupId = `member:${string}`;
export type AgentMentionGroupId =
  | AgentMentionStaticGroupId
  | AgentMentionIssueTopicGroupId
  | AgentMentionProvenanceGroupId
  | AgentMentionMemberGroupId;

export type AgentMentionRawGroupId =
  | Exclude<
      AgentMentionStaticGroupId,
      "files" | "my_sessions" | "collab_sessions"
    >
  | "sessions";
export type AgentMentionRawGroups = Record<
  AgentMentionRawGroupId,
  AgentContextMentionItem[]
>;
export type AgentMentionTotalCounts = Partial<
  Record<AgentMentionGroupId, number>
>;

export interface AgentMentionIssueTopicGroup {
  id: `issue-topic:${string}`;
  providerGroupId: string;
  label: string;
  items: AgentContextMentionItem[];
  totalCount: number;
  nextPageToken: string | null;
  loadMoreStatus: "idle" | "loading" | "error";
  loadMoreError: string | null;
}

export interface AgentMentionBrowseCategory {
  id: AgentMentionFilterId;
  label: string;
}

export type AgentMentionGroup = MentionPaletteGroup<AgentContextMentionItem>;

export type AgentMentionSearchState =
  MentionPaletteState<AgentContextMentionItem>;

export interface AgentMentionSearchControllerOptions {
  contextMentionProviders?: readonly AgentContextMentionProvider[];
  /** Host-owned filter IDs that must not appear in the mention palette. */
  hiddenFilterIds?: readonly string[];
  debounceMs?: number;
  fileLimit?: number;
  issueLimit?: number;
  browseCacheTtlMs?: number;
  providerTimeoutMs?: number;
  diagnosticInfoLogger?: (payload: AgentMentionSearchDiagnosticLog) => void;
  diagnosticNow?: () => number;
  diagnosticSlowThresholdMs?: number;
}

export type AgentMentionSearchListener = (
  state: AgentMentionSearchState
) => void;

export interface AgentMentionLifecycleDiagnosticLog {
  event:
    | "browse.open"
    | "browse.preload"
    | "browse.cache"
    | "browse.fetch.start"
    | "browse.fetch.dedupe"
    | "browse.fetch.success"
    | "browse.fetch.error"
    | "browse.apply.skipped";
  details: Record<string, RuntimeDiagnosticsDetailValue>;
}

export const DEFAULT_DEBOUNCE_MS = 120;
export const DEFAULT_FILE_LIMIT = 30;
export const DEFAULT_ISSUE_LIMIT = 25;
export const DEFAULT_SESSION_LIMIT = 30;
export const DEFAULT_PROVIDER_TIMEOUT_MS = 3500;
export const DEFAULT_DIAGNOSTIC_SLOW_THRESHOLD_MS = 250;
export const DEFAULT_BROWSE_CACHE_TTL_MS = 30_000;
export const AGENT_MENTION_LIFECYCLE_LOG_PREFIX =
  "[agent-gui] mention-lifecycle";

// default ("en") runtime, since the agent GUI i18n locale is only synced once the
// AgentGuiI18nProvider renders.
export function buildBrowseCategories(
  hiddenFilterIds: readonly string[] = []
): AgentMentionBrowseCategory[] {
  const hidden = new Set(
    hiddenFilterIds.map((id) => id.trim()).filter(Boolean)
  );
  return AGENT_MENTION_FILTER_TAB_ORDER.filter((id) => !hidden.has(id)).map(
    (id) => ({
      id,
      label: agentMentionFilterLabel(id)
    })
  );
}

export const {
  agentGeneratedFile: AGENT_GENERATED_FILE_PROVIDER_ID,
  agentSession: AGENT_SESSION_PROVIDER_ID,
  agentTarget: AGENT_TARGET_PROVIDER_ID,
  file: FILE_PROVIDER_ID,
  workspaceApp: WORKSPACE_APP_PROVIDER_ID,
  workspaceIssue: WORKSPACE_ISSUE_PROVIDER_ID
} = AGENT_CONTEXT_MENTION_PROVIDER_IDS;
