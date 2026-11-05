import { renderMulticaMentionRow } from "@multica/tutti-agent-gui-host/mention-row";
import {
  normalizeAgentActivityDisplayStatus,
  type AgentActivityDisplayStatus
} from "@tutti-os/agent-activity-core";
import {
  MentionPaletteFromState,
  flattenMentionPaletteEntries,
  renderMentionRow,
  type MentionPaletteEntry,
  type MentionPaletteState,
  type MentionPaletteTheme,
  type MentionRowClassNames,
  type MentionRowItem,
  type MentionRowStatusTag,
  type MentionRowStatusTone
} from "@tutti-os/ui-rich-text/at-panel";
import { resolveIssueManagerStatusPresentation } from "@tutti-os/workspace-issue-manager/ui";
import { observer } from "mobx-react-lite";
import type { ReactNode } from "react";
import { getActiveUiLanguage, translate } from "../../i18n/index";
import { managedAgentRoundedIconUrl } from "../../shared/managedAgentIcons";
import { roomIssueStatusLabel } from "../../shared/roomIssueStatusLabel";
import { userAvatarPlaceholderUrl } from "../../shared/userAvatarPlaceholder";
import { workspaceAgentActivityStatusLabel } from "../../shared/workspaceAgentActivityStatusLabel";
import {
  resolveAgentMentionFileThumbnailUrl,
  resolveAgentMentionFileVisualKind
} from "../shared/mentionFilePresentation";
import {
  agentMentionEmptyGroupLabel,
  agentMentionFilterLabel,
  agentMentionGroupLabel
} from "./AgentMentionLabels";
import {
  type AgentMentionBrowseCategory,
  type AgentMentionFilterId,
  type AgentMentionGroup,
  type AgentMentionGroupId,
  type AgentMentionSearchState
} from "./AgentMentionSearchController";
import { agentGeneratedMentionItemKey } from "./agentMentionAgentGeneratedFilesPresentation";
import {
  AGENT_MENTION_FILTER_TAB_ORDER,
  mentionGroupExpandCount
} from "./agentMentionSearchHelpers";
import type { AgentContextMentionItem } from "./agentRichText/agentFileMentionExtension";

export interface AgentMentionPaletteEntry {
  key: string;
  type: "category" | "item" | "expand";
  categoryId?: AgentMentionBrowseCategory["id"];
  groupId?: AgentMentionGroupId;
  item?: AgentContextMentionItem;
}

export interface AgentFileMentionPaletteProps {
  state: AgentMentionSearchState;
  highlightedKey: string | null;
  label: string;
  loadingLabel: string;
  emptyLabel: string;
  errorLabel: string;
  tabHintLabel: string;
  maxHeightPx: number;
  shouldCenterHighlightedItem?: boolean;
  onHighlightChange: (key: string) => void;
  onSelectItem: (entry: AgentContextMentionItem) => void;
  onSelectCategory: (categoryId: AgentMentionBrowseCategory["id"]) => void;
  onSelectFilter: (filter: AgentMentionFilterId) => void;
  onExpandGroup: (groupId: AgentMentionGroupId) => void;
  onNavigateHierarchy?: (delta: 1 | -1) => void;
  onNavigateIntoItem?: (item: AgentContextMentionItem) => void;
  /**
   * 可选:点击 issue / app 行末尾的「查看产物」入口时回调(打开引用 picker 并定位)。
   * 仅 workspace-issue / workspace-app 行渲染该入口。
   */
  onOpenReferences?: (item: AgentContextMentionItem) => void;
  /** Controlled provenance filter supplied by the AgentGUI orchestration layer. */
  provenanceFilterControl?: ReactNode;
}

const AGENT_MENTION_PALETTE_THEME: MentionPaletteTheme = {
  classNames: {
    palette: "agent-gui-node__mention-palette",
    header: "agent-gui-node__mention-palette-header",
    footer: "agent-gui-node__mention-palette-footer",
    tabs: "agent-gui-node__mention-palette-tabs",
    scrollRegion: "agent-gui-node__mention-palette-scroll-region",
    scrollbar:
      "workspace-agents-status-panel__scrollbar agent-gui-node__mention-palette-scrollbar",
    scrollbarThumb: "workspace-agents-status-panel__scrollbar-thumb",
    hint: "agent-gui-node__mention-palette-hint",
    hintItem: "agent-gui-node__mention-palette-hint-item",
    hintButton: "agent-gui-node__mention-palette-hint-button",
    hintSeparator: "agent-gui-node__mention-palette-hint-separator",
    shortcut: "agent-gui-node__mention-palette-shortcut",
    shortcutArrow: "agent-gui-node__mention-palette-shortcut--arrow",
    shortcutButton: "agent-gui-node__mention-palette-shortcut-button",
    shortcutGroup: "agent-gui-node__mention-palette-shortcut-group"
  },
  testIds: {
    emptyState: "agent-gui-mention-palette-empty-state",
    hint: "agent-gui-mention-palette-hint",
    scrollbar: "agent-gui-mention-palette-scrollbar",
    loadingSpinner: "agent-mention-loading-spinner"
  },
  groupDividerAttribute: "data-agent-mention-group-divider"
};

/**
 * The agent composer's existing structural row class names. Passing these to
 * {@link renderMentionRow} keeps the rendered DOM byte-identical to the
 * pre-refactor markup (the agent stylesheet `agentactivity.css` owns these
 * rules and the agent spec greps them), while the shared component defaults to
 * package-owned `rich-text-at-mention-*` names for other surfaces.
 */
const AGENT_MENTION_ROW_CLASS_NAMES: MentionRowClassNames = {
  fileIcon: "agent-gui-node__mention-file-icon",
  fileThumb: "agent-gui-node__mention-file-thumb",
  kindIcon: "tsh-agent-object-token__kind-icon",
  avatarImgUserPlaceholder:
    "workspace-agents-status-panel__avatar-img--user-placeholder"
};

/**
 * Stable per-item key suffix. The shared shell composes the full entry key as
 * `${group.id}:${agentMentionItemKey(item)}`, matching the agent's historical
 * `${group.id}:${item.kind}:${...}` format so highlight keys stay compatible.
 */
export function agentMentionItemKey(item: AgentContextMentionItem): string {
  return `${item.kind}:${
    item.kind === "file" ? agentGeneratedMentionItemKey(item) : item.targetId
  }`;
}

export function isAgentMentionItemDisabled(
  item: AgentContextMentionItem
): boolean {
  return (
    item.kind === "agent-target" &&
    item.availabilityStatus !== undefined &&
    item.availabilityStatus !== "ready" &&
    item.availabilityStatus !== "available"
  );
}

export function flattenAgentMentionPaletteEntries(
  state: AgentMentionSearchState
): AgentMentionPaletteEntry[] {
  return flattenMentionPaletteEntries(
    state,
    (item) => agentMentionItemKey(item),
    isAgentMentionItemDisabled
  ).map((entry: MentionPaletteEntry): AgentMentionPaletteEntry => {
    if (entry.type === "item") {
      const item =
        entry.groupId !== undefined && entry.itemIndex !== undefined
          ? state.groups.find((group) => group.id === entry.groupId)?.items[
              entry.itemIndex
            ]
          : undefined;
      return {
        key: entry.key,
        type: "item",
        groupId: entry.groupId as AgentMentionGroupId | undefined,
        item
      };
    }
    return {
      key: entry.key,
      type: entry.type,
      categoryId: entry.categoryId as AgentMentionFilterId | undefined,
      groupId: entry.groupId as AgentMentionGroupId | undefined
    };
  });
}

export function groupStartKeys(state: AgentMentionSearchState): string[] {
  if (state.mode === "browse") {
    return state.categories.map((category) => `category:${category.id}`);
  }
  return state.groups
    .map((group) => {
      const firstItem = group.items.find(
        (item) => !isAgentMentionItemDisabled(item)
      );
      if (firstItem) {
        return `${group.id}:${agentMentionItemKey(firstItem)}`;
      }
      if (group.hasMore) {
        return `expand:${group.id}`;
      }
      return null;
    })
    .filter((key): key is string => key !== null);
}

export const AgentFileMentionPalette = observer(
  function AgentFileMentionPalette({
    state,
    highlightedKey,
    label,
    loadingLabel,
    emptyLabel,
    errorLabel,
    tabHintLabel,
    maxHeightPx,
    shouldCenterHighlightedItem = false,
    onHighlightChange,
    onSelectItem,
    onSelectCategory,
    onSelectFilter,
    onExpandGroup,
    onNavigateHierarchy,
    onNavigateIntoItem,
    onOpenReferences,
    provenanceFilterControl
  }: AgentFileMentionPaletteProps): React.JSX.Element {
    "use memo";
    const openReferencesLabel = translate(
      "agentHost.agentGui.mentionOpenReferences"
    );
    const navigateIntoLabel = translate(
      "agentHost.agentGui.fileMentionEnterFolder"
    );
    const filter = state.filter as AgentMentionFilterId;
    const highlightedBrowseCategory = highlightedKey?.startsWith("category:")
      ? highlightedKey.slice("category:".length)
      : null;
    const showBrowseHint = shouldShowBrowseSearchHint({
      browseFilter: filter,
      groups: state.groups,
      highlightedBrowseCategory,
      mode: state.mode
    });

    // Browse mode carries its own category list (with labels); results mode tabs
    // are the fixed agent filter order. The shared shell renders a single tab
    // source, so resolve the right one here.
    const categories =
      state.mode === "browse"
        ? state.categories
        : AGENT_MENTION_FILTER_TAB_ORDER.map((id) => ({
            id,
            label: agentMentionFilterLabel(id)
          }));

    // When the agent wants the single keyboard browse hint we hand the shell an
    // empty group list so it renders its (keyboard-icon) empty state with our
    // computed hint copy. Otherwise we map the real groups, decorating each with
    // the agent-specific label / empty / expand / spacing chrome.
    const shellState: MentionPaletteState<AgentContextMentionItem> =
      showBrowseHint
        ? { ...state, categories, groups: [] }
        : {
            ...state,
            categories,
            groups: state.groups.map((group, index) =>
              decorateMentionGroup(
                group,
                index,
                state.groups,
                filter,
                state.query
              )
            )
          };

    const emptyLabelForShell = showBrowseHint
      ? browseHintForFilter(filter)
      : resolveMentionPaletteEmptyLabel({
          emptyLabel,
          filter,
          mode: state.mode,
          query: state.query
        });

    return (
      <MentionPaletteFromState<AgentContextMentionItem>
        state={shellState}
        highlightedKey={highlightedKey}
        getItemKey={agentMentionItemKey}
        isItemDisabled={isAgentMentionItemDisabled}
        renderItem={(item) =>
          renderMulticaMentionRow(
            item,
            getActiveUiLanguage(),
            agentMentionItemToRowItem(item),
            {
              classNames: AGENT_MENTION_ROW_CLASS_NAMES,
              dataAttributeMode: "agent",
              ...(onNavigateIntoItem && canNavigateIntoMentionItem(item)
                ? {
                    onNavigateInto: () => onNavigateIntoItem(item),
                    navigateIntoLabel
                  }
                : {}),
              ...(onOpenReferences && isReferenceableMentionItem(item)
                ? {
                    onOpenReferences: () => onOpenReferences(item),
                    openReferencesLabel
                  }
                : {})
            }
          )
        }
        labels={{
          loading: loadingLabel,
          empty: emptyLabelForShell,
          error: errorLabel,
          tabHint: tabHintLabel,
          listbox: label
        }}
        hintLabels={{
          cycleFilter: translate(
            "agentHost.agentGui.fileMentionSwitchCategory"
          ),
          moveSelection: translate(
            "agentHost.agentGui.fileMentionSwitchSelection"
          ),
          navigateHierarchy: translate(
            "agentHost.agentGui.fileMentionNavigateHierarchy"
          )
        }}
        maxHeightPx={maxHeightPx}
        headerActions={
          filter === "session" || filter === "file" || filter === "issue"
            ? provenanceFilterControl
            : undefined
        }
        scrollHighlightedIntoViewCentered={shouldCenterHighlightedItem}
        theme={AGENT_MENTION_PALETTE_THEME}
        callbacks={{
          onHighlightChange,
          onActiveCategoryIdChange: (categoryId) => {
            onSelectCategory(categoryId as AgentMentionBrowseCategory["id"]);
            onSelectFilter(categoryId as AgentMentionFilterId);
          },
          onExpandGroup: (groupId) =>
            onExpandGroup(groupId as AgentMentionGroupId),
          onSelectItem
        }}
        onNavigateHierarchy={onNavigateHierarchy}
      />
    );
  }
);

export const AgentContextMentionPalette = AgentFileMentionPalette;
export type AgentContextMentionPaletteProps = AgentFileMentionPaletteProps;

/**
 * Map a controller group onto the shared shell group, layering in the
 * agent-specific chrome the generic shell intentionally omits: translated
 * group / empty / expand labels, file-search chrome suppression, and the extra
 * top margin between the "my sessions" and "collab sessions" groups.
 */
function decorateMentionGroup(
  group: AgentMentionGroup,
  index: number,
  groups: ReadonlyArray<AgentMentionGroup>,
  filter: AgentMentionFilterId,
  query: string
): AgentMentionGroup {
  const groupId = group.id as AgentMentionGroupId;
  const suppressChrome = shouldSuppressFileSearchGroupChrome(filter, query);
  const followsMySessions =
    groupId === "collab_sessions" &&
    (groups[index - 1]?.id as AgentMentionGroupId) === "my_sessions";
  const showLabel = shouldRenderMentionGroupLabel({
    filter,
    groupCount: groups.length,
    groupId,
    groupLabel: group.label,
    query
  });
  const groupLabel = group.label ?? agentMentionGroupLabel(groupId);
  return {
    ...group,
    label: showLabel ? groupLabel : undefined,
    emptyLabel: suppressChrome
      ? undefined
      : (group.emptyLabel ?? agentMentionEmptyGroupLabel(groupId, query)),
    expandLabel: group.hasMore
      ? translate("agentHost.agentGui.contextPickerExpandMore", {
          count: mentionGroupExpandCount(group, filter)
        })
      : undefined,
    expandLoadingLabel: translate(
      "agentHost.agentGui.contextPickerLoadMoreLoading"
    ),
    expandErrorLabel: translate(
      "agentHost.agentGui.contextPickerLoadMoreRetry"
    ),
    sectionClassName: followsMySessions ? "mt-2" : undefined,
    hideTopDivider: suppressChrome
  };
}

function shouldSuppressFileSearchGroupChrome(
  filter: AgentMentionFilterId,
  query: string
): boolean {
  return filter === "file" && query.trim().length > 0;
}

function resolveMentionPaletteEmptyLabel(input: {
  emptyLabel: string;
  filter: AgentMentionFilterId;
  mode: AgentMentionSearchState["mode"];
  query: string;
}): string {
  if (
    input.mode === "results" &&
    input.filter === "file" &&
    input.query.trim().length > 0
  ) {
    return translate("agentHost.agentGui.mentionNoMatchingFiles");
  }
  if (input.filter === "session") {
    return agentMentionEmptyGroupLabel("my_sessions", input.query);
  }
  if (input.filter === "app") {
    return agentMentionEmptyGroupLabel("apps", input.query);
  }
  if (input.filter === "agent") {
    return agentMentionEmptyGroupLabel("agents", input.query);
  }
  if (input.filter === "issue") {
    return agentMentionEmptyGroupLabel("issues", input.query);
  }
  return input.emptyLabel;
}

function shouldRenderMentionGroupLabel(input: {
  filter: AgentMentionFilterId;
  groupCount: number;
  groupId: AgentMentionGroupId;
  groupLabel?: string;
  query: string;
}): boolean {
  if (shouldSuppressFileSearchGroupChrome(input.filter, input.query)) {
    return false;
  }
  if (input.groupCount !== 1) {
    return true;
  }
  if (input.groupId.startsWith("agent:")) {
    return true;
  }
  return (
    (input.groupLabel ?? agentMentionGroupLabel(input.groupId)) !==
    agentMentionFilterLabel(input.filter)
  );
}

/**
 * Resolve an agent mention item into the shared, display-ready
 * {@link MentionRowItem} view-model the generic {@link renderMentionRow}
 * consumes. All agent-specific bits — i18n via {@link translate}, the user
 * avatar placeholder asset, the managed-agent rounded icon, and the activity /
 * issue status labels — are resolved here so the shared renderer stays pure.
 */
function agentMentionItemToRowItem(
  item: AgentContextMentionItem
): MentionRowItem {
  if (item.kind === "file") {
    const visualKind = resolveAgentMentionFileVisualKind({
      entryKind: item.entryKind,
      href: item.href,
      mentionNavigation: item.mentionNavigation,
      name: item.name,
      path: item.path
    });
    const isNavigableFolder =
      item.mentionNavigation === "agent-generated-folder" ||
      item.mentionNavigation === "workspace-folder";
    const childCountLabel =
      isNavigableFolder && typeof item.childCount === "number"
        ? translate("agentHost.agentGui.mentionFolderChildCount", {
            count: item.childCount
          })
        : null;
    return {
      kind: "file",
      name: item.name,
      contextLabel: item.contextLabel ?? null,
      visualKind,
      thumbnailUrl: resolveAgentMentionFileThumbnailUrl(item) ?? null,
      childCountLabel,
      entryKind: item.entryKind,
      mentionNavigation: item.mentionNavigation
    };
  }

  if (item.kind === "session") {
    const isMySession = item.scope === "my_sessions";
    const participantTruncatableSegments =
      item.agentOwnerLabel && item.agentLabel ? [item.agentOwnerLabel] : null;
    const participantFixedSuffix =
      participantTruncatableSegments && item.agentLabel
        ? ` · ${item.agentLabel}`
        : null;
    return {
      kind: "session",
      participant: item.agentName,
      participantTruncatableSegments,
      participantFixedSuffix,
      summary: item.title,
      userAvatarUrl: isMySession ? null : (item.initiatorAvatarUrl ?? null),
      userAvatarPlaceholderUrl,
      agentIconUrl:
        item.agentIconUrl?.trim() ||
        managedAgentRoundedIconUrl(
          mentionSessionAgentProvider(item) ?? item.agentName
        ),
      showUserAvatar: !isMySession,
      statusTag: agentSessionStatusTag(item.status)
    };
  }

  if (item.kind === "workspace-app") {
    return {
      kind: "app",
      name: item.name,
      description: mentionDescriptionWithoutTerminalPeriod(item.description),
      iconUrl: item.iconUrl ?? null
    };
  }

  if (item.kind === "agent-target") {
    return {
      kind: "app",
      name: item.name,
      description: mentionDescriptionWithoutTerminalPeriod(item.description),
      iconUrl: item.iconUrl ?? managedAgentRoundedIconUrl(item.agentProviderId),
      statusTag: agentTargetAvailabilityStatusTag(item.availabilityStatus)
    };
  }

  if (item.kind === "workspace-reference") {
    return {
      kind: "app",
      name: item.name,
      description: null,
      iconUrl: item.iconUrl ?? null
    };
  }

  if (item.kind === "workspace-app-factory") {
    return {
      kind: "app-factory",
      name: item.name
    };
  }

  if (item.kind === "custom") {
    // 自定义 mention 只经 draftPrompt prefill 进入 composer,不出现在 @ 面板候选;
    // 兜底按通用条目展示(label 即注册方给的 name)。
    return {
      kind: "issue",
      title: item.name,
      creatorName: null,
      statusTag: null
    };
  }

  return {
    kind: "issue",
    title: item.title,
    creatorName: item.creatorName ?? null,
    iconUrl: item.iconUrl ?? null,
    statusTag: agentIssueStatusTag(item.status)
  };
}

function mentionDescriptionWithoutTerminalPeriod(
  description: string | null | undefined
): string | null {
  const normalizedDescription = description?.trimEnd();
  return normalizedDescription
    ? normalizedDescription.replace(/[。.]+$/u, "")
    : null;
}

/**
 * 仅 workspace-issue 行,以及声明了能够提供产物文件(reference)的 workspace-app 行,
 * 才在行末尾展示「查看产物」入口。应用是否能提供产物文件由其 manifest 的
 * references 能力决定(`referencesListSupported`),而非硬编码应用名单。
 */
function isReferenceableMentionItem(item: AgentContextMentionItem): boolean {
  if (item.kind === "workspace-issue") {
    return true;
  }
  if (item.kind === "workspace-app") {
    return item.referencesListSupported === true;
  }
  return false;
}

function canNavigateIntoMentionItem(item: AgentContextMentionItem): boolean {
  return (
    item.kind === "file" &&
    (item.mentionNavigation === "agent-generated-folder" ||
      item.mentionNavigation === "workspace-folder")
  );
}

function mentionSessionAgentProvider(
  item: Extract<AgentContextMentionItem, { kind: "session" }>
): string | null {
  const queryStart = item.href.indexOf("?");
  if (queryStart < 0) {
    return null;
  }
  return new URLSearchParams(item.href.slice(queryStart + 1)).get("provider");
}

function agentSessionStatusTag(
  status: string | undefined
): MentionRowStatusTag | null {
  if (!status) {
    return null;
  }
  const activityStatus = normalizeAgentActivityDisplayStatus(status);
  return {
    label: workspaceAgentActivityStatusLabel(activityStatus),
    tone: mentionStatusTone(activityStatus),
    pulse: activityStatus === "working" || activityStatus === "waiting",
    variant: "activity",
    dataStatus: activityStatus
  };
}

/**
 * Shared agent targets remain discoverable while their owner is offline. Their
 * availability badge communicates whether the target can currently be invoked.
 */
function agentTargetAvailabilityStatusTag(
  availabilityStatus: string | undefined
): MentionRowStatusTag {
  const available =
    availabilityStatus === undefined ||
    availabilityStatus === "ready" ||
    availabilityStatus === "available";
  return {
    label: translate(
      available
        ? "agentHost.agentGui.mentionAgentTargetAvailable"
        : "agentHost.agentGui.mentionAgentTargetUnavailable"
    ),
    tone: available ? "green" : "neutral",
    variant: "activity",
    dataStatus: available ? "available" : "unavailable"
  };
}

function agentIssueStatusTag(
  status: string | undefined
): MentionRowStatusTag | null {
  if (!status) {
    return null;
  }
  const presentation = resolveIssueManagerStatusPresentation(status);
  return {
    label: roomIssueStatusLabel(status),
    tone: presentation.mentionTone,
    variant: "issue",
    dataStatus: presentation.dataStatus
  };
}

function mentionStatusTone(
  status: AgentActivityDisplayStatus
): MentionRowStatusTone {
  if (status === "working") {
    return "blue";
  }
  if (status === "waiting" || status === "canceled") {
    return "amber";
  }
  if (status === "completed" || status === "idle") {
    return "green";
  }
  if (status === "failed") {
    return "red";
  }
  return "neutral";
}

function browseHintForFilter(filter: AgentMentionFilterId): string {
  switch (filter) {
    case "agent":
      return translate("agentHost.agentGui.contextPickerBrowseAgentHint");
    case "app":
      return translate("agentHost.agentGui.contextPickerBrowseAppHint");
    case "file":
      return translate("agentHost.agentGui.contextPickerBrowseFileHint");
    case "session":
      return translate("agentHost.agentGui.contextPickerBrowseSessionHint");
    case "issue":
      return translate("agentHost.agentGui.contextPickerBrowseIssueHint");
  }
}

function hasInteractiveGroupEntries(
  groups: ReadonlyArray<AgentMentionGroup>
): boolean {
  return groups.some((group) => group.items.length > 0 || group.hasMore);
}

function isFileBrowseGroupsOnlyEmpty(
  groups: ReadonlyArray<AgentMentionGroup>
): boolean {
  const fileGroups = groups.filter(
    (group) =>
      group.id === "opened_files" || group.id === "agent_generated_files"
  );
  if (fileGroups.length === 0) {
    return false;
  }
  return fileGroups.every(
    (group) => group.items.length === 0 && !group.hasMore
  );
}

function shouldShowBrowseSearchHint(input: {
  browseFilter: AgentMentionFilterId;
  groups: ReadonlyArray<AgentMentionGroup>;
  highlightedBrowseCategory: string | null;
  mode: AgentMentionSearchState["mode"];
}): boolean {
  if (input.mode !== "browse" || hasInteractiveGroupEntries(input.groups)) {
    return false;
  }
  if (input.groups.length === 0) {
    return true;
  }
  if (
    input.highlightedBrowseCategory !== null &&
    input.highlightedBrowseCategory !== input.browseFilter
  ) {
    return true;
  }
  return (
    input.browseFilter === "file" && isFileBrowseGroupsOnlyEmpty(input.groups)
  );
}
