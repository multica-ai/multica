import type { Editor, Range } from "@tiptap/core";

export type AgentFileMentionKind = "file" | "directory" | "unknown";
export type AgentMentionFileNavigationAction =
  | "agent-generated-folder"
  | "agent-generated-folder-back"
  | "workspace-folder"
  | "workspace-folder-back";
export type AgentMentionScope = "my_sessions" | "collab_sessions";
export type AgentMentionKind =
  | "file"
  | "agent-target"
  | "session"
  | "workspace-app"
  | "workspace-reference"
  | "workspace-app-factory"
  | "workspace-issue"
  | "custom";

export type AgentMentionReferenceSource = "app" | "task";

export interface AgentMentionFileItem {
  kind: "file";
  path: string;
  href: string;
  name: string;
  entryKind: AgentFileMentionKind;
  directoryPath: string;
  /** Present only for a regular file attached from the local composer. */
  attachmentId?: string;
  attachmentStatus?: AgentComposerFileMentionStatus;
  attachmentErrorCode?: string;
  score?: number;
  thumbnailUrl?: string | null;
  mentionNavigation?: AgentMentionFileNavigationAction;
  childCount?: number;
  /** Ephemeral picker-only relative path and owning-workspace presentation. */
  contextLabel?: string;
}

export type AgentComposerFileMentionStatus = "uploading" | "ready" | "error";

export interface AgentMentionSessionItem {
  kind: "session";
  href: string;
  workspaceId: string;
  targetId: string;
  agentTargetId?: string;
  name: string;
  title: string;
  scope: AgentMentionScope;
  initiatorUserId?: string;
  initiatorName: string;
  initiatorAvatarUrl?: string;
  agentName: string;
  /** Structured owner segment from the matching provenance Agent option. */
  agentOwnerLabel?: string;
  /** Structured Agent segment from the matching provenance Agent option. */
  agentLabel?: string;
  agentIconUrl?: string;
  status?: string;
  inputPreview?: string;
  summaryPreview?: string;
  updatedAtUnixMs?: number;
}

export interface AgentMentionWorkspaceIssueItem {
  kind: "workspace-issue";
  href: string;
  workspaceId: string;
  targetId: string;
  topicId?: string;
  name: string;
  title: string;
  creatorName?: string;
  iconUrl?: string;
  status?: string;
  statusLabel?: string;
  statusCategory?: string;
  contentPreview?: string;
  updatedAtUnixMs?: number;
}

export interface AgentMentionWorkspaceAppItem {
  kind: "workspace-app";
  href: string;
  workspaceId: string;
  targetId: string;
  appId: string;
  name: string;
  description?: string;
  iconUrl?: string;
  /** 应用是否能够提供产物文件(reference),决定 @ 面板行末尾是否展示「查看产物」入口。 */
  referencesListSupported?: boolean;
}

export interface AgentMentionAgentTargetItem {
  kind: "agent-target";
  href: string;
  workspaceId: string;
  targetId: string;
  name: string;
  description?: string;
  agentProviderId?: string;
  iconUrl?: string;
  availabilityStatus?: string;
}

export interface AgentMentionWorkspaceReferenceItem {
  kind: "workspace-reference";
  href: string;
  workspaceId: string;
  /** URI path id:source=app 时为 appId,source=task 时为 topicId。 */
  targetId: string;
  source: AgentMentionReferenceSource;
  /** 子级 id:app 子分组 / issueId。缺省表示整个 app / topic。 */
  groupId?: string;
  name: string;
  iconUrl?: string;
  /** 展示用文件数(来自 picker 节点 childCount);序列化不再展开文件。 */
  fileCount: number;
}

export interface AgentMentionWorkspaceAppFactoryItem {
  kind: "workspace-app-factory";
  href: string;
  workspaceId: string;
  targetId: string;
  jobId: string;
  name: string;
  action?: string;
  contextPath?: string;
}

// 宿主注册的自定义 mention(见 shared/agentCustomMentionKinds):href 是完整信息源
// (round-trip 无损),item 只承载通用展示字段;业务细节由宿主在注册的钩子里从 href 还原。
export interface AgentMentionCustomItem {
  kind: "custom";
  /** 注册表里的 kind(= mention:// providerId)。 */
  customKind: string;
  href: string;
  workspaceId: string;
  targetId: string;
  /** Canonical Markdown label, kept separate from the host's chip presentation. */
  sourceLabel: string;
  /** chip 第一行。 */
  name: string;
  /** chip 第二行(通用双行卡)。 */
  summary?: string;
}

export type AgentContextMentionItem =
  | AgentMentionFileItem
  | AgentMentionAgentTargetItem
  | AgentMentionSessionItem
  | AgentMentionWorkspaceAppItem
  | AgentMentionWorkspaceReferenceItem
  | AgentMentionWorkspaceAppFactoryItem
  | AgentMentionWorkspaceIssueItem
  | AgentMentionCustomItem;

export type AgentFileMentionItem = AgentContextMentionItem;

export interface AgentMentionSuggestionState {
  editor: Editor;
  range: Range;
  query: string;
  text: string;
  command: (item: AgentContextMentionItem) => void;
  clientRect?: (() => DOMRect | null) | null;
}

export type AgentFileMentionSuggestionState = AgentMentionSuggestionState;

export interface AgentFileMentionExtensionOptions {
  enableSuggestions?: boolean;
  onSuggestionChange?: (state: AgentMentionSuggestionState | null) => void;
  onSuggestionKeyDown?: (event: KeyboardEvent) => boolean;
  removeActionAriaLabel?: string;
  renderAsLink?: boolean;
  shouldSuppressSuggestion?: () => boolean;
}

export interface ParsedAgentMentionMarkdown {
  item: AgentContextMentionItem;
  end: number;
}
