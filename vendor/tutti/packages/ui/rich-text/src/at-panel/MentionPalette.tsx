import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type JSX,
  type MutableRefObject,
  type ReactNode
} from "react";
import {
  FolderFailedFilledIcon,
  KeyboardFilledIcon
} from "@tutti-os/ui-system/icons";
import { UnderlineTabs } from "@tutti-os/ui-system/components";
import { cn } from "@tutti-os/ui-system/utils";
import { flattenMentionPaletteEntries } from "./mentionPaletteEntries.ts";
import { mentionPaletteExpandLabel } from "./mentionPaletteModel.ts";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { useMentionScrollPagination } from "@multica/tutti-agent-gui-host/mention-scroll-pagination";
import { MentionPaletteScrollbar } from "./mentionPaletteScrollbar.tsx";
import type {
  MentionPaletteGroup,
  MentionPaletteProps,
  MentionPaletteState,
  MentionPaletteTheme
} from "./mentionPaletteTypes.ts";
import "./mentionPalette.css";

const DEFAULT_THEME = {
  classNames: {
    palette: "rich-text-at-mention-palette",
    header: "rich-text-at-mention-palette-header",
    footer: "rich-text-at-mention-palette-footer",
    tabs: "rich-text-at-mention-palette-tabs",
    scrollRegion: "rich-text-at-mention-palette-scroll-region",
    scrollbar: "rich-text-at-mention-palette-scrollbar",
    scrollbarThumb: "rich-text-at-mention-palette-scrollbar-thumb",
    hint: "rich-text-at-mention-palette-hint",
    hintItem: "rich-text-at-mention-palette-hint-item",
    hintButton: "rich-text-at-mention-palette-hint-button",
    hintSeparator: "rich-text-at-mention-palette-hint-separator",
    shortcut: "rich-text-at-mention-palette-shortcut",
    shortcutArrow: "rich-text-at-mention-palette-shortcut--arrow",
    shortcutButton: "rich-text-at-mention-palette-shortcut-button",
    shortcutGroup: "rich-text-at-mention-palette-shortcut-group"
  },
  testIds: {
    emptyState: "rich-text-at-mention-palette-empty-state",
    hint: "rich-text-at-mention-palette-hint",
    scrollbar: "rich-text-at-mention-palette-scrollbar",
    loadingSpinner: "rich-text-at-mention-loading-spinner"
  },
  groupDividerAttribute: "data-rich-text-at-mention-group-divider"
} as const;

interface ResolvedMentionPaletteTheme {
  classNames: Required<NonNullable<MentionPaletteTheme["classNames"]>>;
  testIds: Required<NonNullable<MentionPaletteTheme["testIds"]>>;
  groupDividerAttribute: string;
}

function resolveMentionPaletteTheme(
  theme: MentionPaletteTheme | undefined
): ResolvedMentionPaletteTheme {
  return {
    classNames: { ...DEFAULT_THEME.classNames, ...theme?.classNames },
    testIds: { ...DEFAULT_THEME.testIds, ...theme?.testIds },
    groupDividerAttribute:
      theme?.groupDividerAttribute ?? DEFAULT_THEME.groupDividerAttribute
  };
}

function mentionPaletteRootClassName(
  theme: ResolvedMentionPaletteTheme
): string {
  return cn(
    DEFAULT_THEME.classNames.palette,
    theme.classNames.palette,
    paletteStyles.palette
  );
}

const paletteStyles = {
  palette: "rich-text-at-mention-palette__shell",
  header: "rich-text-at-mention-palette__header",
  footer: "rich-text-at-mention-palette__footer",
  scrollShell: "rich-text-at-mention-palette__scroll-shell",
  scrollBody: "rich-text-at-mention-palette__scroll-body",
  groups: "rich-text-at-mention-palette__groups",
  group: "rich-text-at-mention-palette__group",
  groupDivider: "rich-text-at-mention-palette__group-divider",
  groupLabel: "rich-text-at-mention-palette__group-label",
  groupItems: "rich-text-at-mention-palette__group-items",
  groupEmpty: "rich-text-at-mention-palette__group-empty",
  rowButton: "rich-text-at-mention-palette__row-button",
  expandButton: "rich-text-at-mention-palette__expand-button",
  emptyState: "rich-text-at-mention-palette__empty-state",
  emptyStateInner: "rich-text-at-mention-palette__empty-state-inner",
  emptyStateIcon: "rich-text-at-mention-palette__empty-state-icon",
  emptyStateText: "rich-text-at-mention-palette__empty-state-text",
  loading: "rich-text-at-mention-palette__loading",
  loadingSpinner: "rich-text-at-mention-palette__loading-spinner"
} as const;

const MENTION_PALETTE_LOADING_MIN_VISIBLE_MS = 320;

export function MentionPalette<TItem>(
  props: MentionPaletteProps<TItem>
): JSX.Element {
  "use memo";
  const {
    state,
    highlightedKey,
    getItemKey,
    isItemDisabled,
    renderItem,
    labels,
    hintLabels,
    maxHeightPx,
    onHighlightChange,
    onSelectItem,
    onSelectCategory,
    onSelectFilter,
    onExpandGroup,
    onCycleFilter,
    onMoveSelection,
    onNavigateHierarchy,
    renderListFooter,
    headerActions,
    loadingBanner,
    scrollHighlightedIntoViewCentered = false,
    theme: themeProp
  } = props;
  const theme = resolveMentionPaletteTheme(themeProp);

  const highlightedOptionRef = useRef<HTMLButtonElement | null>(null);
  const scrollBodyRef = useRef<HTMLDivElement | null>(null);
  useMentionScrollPagination(scrollBodyRef, state, onExpandGroup);
  const loadingVisibleUntilRef = useRef(0);
  const loadingHideTimerRef = useRef<ReturnType<typeof setTimeout> | null>(
    null
  );
  const [loadingIndicatorVisible, setLoadingIndicatorVisible] = useState(
    state.status === "loading"
  );

  const interactiveEntries = flattenMentionPaletteEntries(
    state,
    (item, groupId) => getItemKey(item, findGroup(state.groups, groupId)),
    isItemDisabled
      ? (item, groupId) =>
          isItemDisabled(item, findGroup(state.groups, groupId))
      : undefined
  );
  const hasInteractiveEntries = interactiveEntries.some(
    (entry) => entry.type === "item" || entry.type === "expand"
  );

  const showLoadingState =
    loadingIndicatorVisible &&
    (!hasInteractiveEntries || state.mode === "browse");
  const showLoadingBanner =
    Boolean(loadingBanner) &&
    loadingIndicatorVisible &&
    hasInteractiveEntries &&
    state.mode === "results";

  useEffect(() => {
    const highlightedElement = highlightedOptionRef.current;
    if (!highlightedElement) {
      return;
    }
    const scrollContainer = scrollBodyRef.current;
    if (!scrollContainer || !scrollContainer.contains(highlightedElement)) {
      highlightedElement.scrollIntoView({ block: "nearest" });
      return;
    }
    if (scrollHighlightedIntoViewCentered) {
      centerElementInScrollContainer(scrollContainer, highlightedElement);
      return;
    }
    scrollElementIntoScrollContainerNearest(
      scrollContainer,
      highlightedElement
    );
  }, [highlightedKey, scrollHighlightedIntoViewCentered]);

  useEffect(() => {
    if (loadingHideTimerRef.current !== null) {
      clearTimeout(loadingHideTimerRef.current);
      loadingHideTimerRef.current = null;
    }

    if (state.status === "loading") {
      loadingVisibleUntilRef.current =
        Date.now() + MENTION_PALETTE_LOADING_MIN_VISIBLE_MS;
      setLoadingIndicatorVisible(true);
      return;
    }

    const remainingMs = loadingVisibleUntilRef.current - Date.now();
    if (remainingMs <= 0) {
      setLoadingIndicatorVisible(false);
      return;
    }

    loadingHideTimerRef.current = setTimeout(() => {
      loadingHideTimerRef.current = null;
      setLoadingIndicatorVisible(false);
    }, remainingMs);

    return () => {
      if (loadingHideTimerRef.current !== null) {
        clearTimeout(loadingHideTimerRef.current);
        loadingHideTimerRef.current = null;
      }
    };
  }, [state.status]);

  const paletteMaxHeightStyle: CSSProperties | undefined =
    maxHeightPx > 0 ? { maxHeight: `${maxHeightPx}px` } : undefined;

  if (state.status === "error") {
    return (
      <div
        className={mentionPaletteRootClassName(theme)}
        style={paletteMaxHeightStyle}
        role="listbox"
        aria-label={labels.listbox ?? labels.tabHint}
      >
        <MentionPaletteEmptyState
          icon="folder-failed"
          label={labels.error}
          testId={theme.testIds.emptyState}
        />
      </div>
    );
  }

  const isBrowse = state.mode === "browse";

  let body: ReactNode;
  if (showLoadingState) {
    body = (
      <MentionPaletteLoading
        label={labels.loading}
        spinnerTestId={theme.testIds.loadingSpinner}
      />
    );
  } else if (state.groups.length === 0) {
    body = (
      <MentionPaletteEmptyState
        icon={isBrowse ? "keyboard" : "folder-failed"}
        label={labels.empty}
        testId={theme.testIds.emptyState}
      />
    );
  } else {
    body = (
      <MentionPaletteGroups
        state={state}
        highlightedKey={highlightedKey}
        highlightedOptionRef={highlightedOptionRef}
        getItemKey={getItemKey}
        isItemDisabled={isItemDisabled}
        renderItem={renderItem}
        onHighlightChange={onHighlightChange}
        onSelectItem={onSelectItem}
        onExpandGroup={onExpandGroup}
        renderListFooter={renderListFooter}
        groupDividerAttribute={theme.groupDividerAttribute}
      />
    );
  }

  return (
    <div
      className={mentionPaletteRootClassName(theme)}
      style={paletteMaxHeightStyle}
    >
      <div className={cn(theme.classNames.header, paletteStyles.header)}>
        <div className="rich-text-at-mention-palette__tabs-layout">
          <UnderlineTabs
            tabs={state.categories.map((category) => ({
              value: category.id,
              label: category.label
            }))}
            value={state.filter}
            onValueChange={isBrowse ? onSelectCategory : onSelectFilter}
            className={theme.classNames.tabs}
            preventMouseDownDefault
          />
        </div>
        {headerActions ? (
          <div className="rich-text-at-mention-palette__header-actions">
            {headerActions}
          </div>
        ) : null}
        {showLoadingBanner ? loadingBanner : null}
      </div>
      <div className={paletteStyles.scrollShell}>
        <div
          ref={scrollBodyRef}
          className={cn(
            theme.classNames.scrollRegion,
            paletteStyles.scrollBody
          )}
          role="listbox"
          aria-label={labels.listbox ?? labels.tabHint}
        >
          {body}
        </div>
        <MentionPaletteScrollbar
          scrollBodyRef={scrollBodyRef}
          className={theme.classNames.scrollbar}
          thumbClassName={theme.classNames.scrollbarThumb}
          testId={theme.testIds.scrollbar}
        />
      </div>
      <div className={cn(theme.classNames.footer, paletteStyles.footer)}>
        <MentionPaletteHint
          ariaLabel={labels.tabHint}
          cycleFilterLabel={hintLabels.cycleFilter}
          moveSelectionLabel={hintLabels.moveSelection}
          navigateHierarchyLabel={hintLabels.navigateHierarchy}
          onCycleFilter={onCycleFilter}
          onMoveSelection={onMoveSelection}
          onNavigateHierarchy={onNavigateHierarchy}
          classNames={theme.classNames}
          testId={theme.testIds.hint}
        />
      </div>
    </div>
  );
}

function centerElementInScrollContainer(
  container: HTMLElement,
  element: HTMLElement
): void {
  const containerRect = container.getBoundingClientRect();
  const elementRect = element.getBoundingClientRect();
  const currentScrollTop = container.scrollTop;
  const elementTop = elementRect.top - containerRect.top + currentScrollTop;
  const centeredScrollTop =
    elementTop - (container.clientHeight - elementRect.height) / 2;
  const maxScrollTop = Math.max(
    0,
    container.scrollHeight - container.clientHeight
  );
  const nextScrollTop = Math.min(Math.max(0, centeredScrollTop), maxScrollTop);
  container.scrollTo({ top: nextScrollTop, behavior: "auto" });
}

function scrollElementIntoScrollContainerNearest(
  container: HTMLElement,
  element: HTMLElement
): void {
  const containerRect = container.getBoundingClientRect();
  const elementRect = element.getBoundingClientRect();
  let nextScrollTop = container.scrollTop;

  if (elementRect.top < containerRect.top) {
    nextScrollTop -= containerRect.top - elementRect.top;
  } else if (elementRect.bottom > containerRect.bottom) {
    nextScrollTop += elementRect.bottom - containerRect.bottom;
  } else {
    return;
  }

  const maxScrollTop = Math.max(
    0,
    container.scrollHeight - container.clientHeight
  );
  container.scrollTo({
    top: Math.min(Math.max(0, nextScrollTop), maxScrollTop),
    behavior: "auto"
  });
}

function findGroup<TItem>(
  groups: MentionPaletteState<TItem>["groups"],
  groupId: string
): MentionPaletteGroup<TItem> {
  const group = groups.find((candidate) => candidate.id === groupId);
  if (!group) {
    throw new Error(`MentionPalette: unknown group id "${groupId}"`);
  }
  return group;
}

function MentionPaletteGroups<TItem>({
  state,
  highlightedKey,
  highlightedOptionRef,
  getItemKey,
  isItemDisabled,
  renderItem,
  onHighlightChange,
  onSelectItem,
  onExpandGroup,
  renderListFooter,
  groupDividerAttribute
}: {
  state: MentionPaletteState<TItem>;
  highlightedKey: string | null;
  highlightedOptionRef: MutableRefObject<HTMLButtonElement | null>;
  getItemKey: (item: TItem, group: MentionPaletteGroup<TItem>) => string;
  isItemDisabled?: (item: TItem, group: MentionPaletteGroup<TItem>) => boolean;
  renderItem: (
    item: TItem,
    ctx: { active: boolean; group: MentionPaletteGroup<TItem> }
  ) => ReactNode;
  onHighlightChange: (key: string) => void;
  onSelectItem: (item: TItem, group: MentionPaletteGroup<TItem>) => void;
  onExpandGroup: (groupId: string) => void;
  renderListFooter?: () => ReactNode;
  groupDividerAttribute: string;
}): JSX.Element {
  return (
    <div className={paletteStyles.groups}>
      {state.groups.map((group, index) => {
        const showGroupDivider = index > 0 && !group.hideTopDivider;
        const isLoadingMore = group.expandStatus === "loading" || state.status === "loading";
        return (
          <section
            key={group.id}
            className={cn(paletteStyles.group, group.sectionClassName)}
          >
            {showGroupDivider ? (
              <div
                className={paletteStyles.groupDivider}
                {...{ [groupDividerAttribute]: "true" }}
                aria-hidden="true"
              />
            ) : null}
            {group.label ? (
              <div className={paletteStyles.groupLabel}>{group.label}</div>
            ) : null}
            <div className={paletteStyles.groupItems}>
              {group.items.length === 0 && group.emptyLabel ? (
                <div className={paletteStyles.groupEmpty}>
                  {group.emptyLabel}
                </div>
              ) : null}
              {group.items.map((item) => {
                const entryKey = `${group.id}:${getItemKey(item, group)}`;
                const isHighlighted = entryKey === highlightedKey;
                const disabled = isItemDisabled?.(item, group) ?? false;
                return (
                  <button
                    key={entryKey}
                    ref={isHighlighted ? highlightedOptionRef : null}
                    type="button"
                    className={paletteStyles.rowButton}
                    role="option"
                    disabled={disabled}
                    aria-disabled={disabled || undefined}
                    aria-selected={isHighlighted}
                    data-highlighted={isHighlighted ? "" : undefined}
                    onPointerMove={() => {
                      if (!disabled && !isHighlighted) {
                        onHighlightChange(entryKey);
                      }
                    }}
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => {
                      if (!disabled) {
                        onSelectItem(item, group);
                      }
                    }}
                  >
                    {renderItem(item, { active: isHighlighted, group })}
                  </button>
                );
              })}
              {group.hasMore ? (
                <button
                  key={`expand:${group.id}`}
                  ref={
                    `expand:${group.id}` === highlightedKey
                      ? highlightedOptionRef
                      : null
                  }
                  type="button"
                  className={paletteStyles.expandButton}
                  data-load-more-group={group.id}
                  disabled={isLoadingMore}
                  aria-busy={isLoadingMore || undefined}
                  data-highlighted={
                    `expand:${group.id}` === highlightedKey ? "" : undefined
                  }
                  onPointerMove={() => {
                    if (`expand:${group.id}` !== highlightedKey) {
                      onHighlightChange(`expand:${group.id}`);
                    }
                  }}
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={() => {
                    if (!isLoadingMore) {
                      onExpandGroup(group.id);
                    }
                  }}
                >
                  {isLoadingMore ? <><Spinner aria-hidden="true" />{group.expandLoadingLabel ?? mentionPaletteExpandLabel(group)}</> : mentionPaletteExpandLabel(group)}
                </button>
              ) : null}
            </div>
          </section>
        );
      })}
      {renderListFooter?.()}
    </div>
  );
}

function MentionPaletteEmptyState({
  icon = "folder-failed",
  label,
  testId
}: {
  icon?: "folder-failed" | "keyboard";
  label: string;
  testId: string;
}): JSX.Element {
  "use memo";
  const EmptyStateIcon =
    icon === "keyboard" ? KeyboardFilledIcon : FolderFailedFilledIcon;

  return (
    <div
      className={paletteStyles.emptyState}
      data-empty-state-icon={icon}
      data-testid={testId}
    >
      <div className={paletteStyles.emptyStateInner}>
        <EmptyStateIcon
          className={paletteStyles.emptyStateIcon}
          aria-hidden="true"
        />
        <span className={paletteStyles.emptyStateText}>{label}</span>
      </div>
    </div>
  );
}

function MentionPaletteLoading({
  label,
  spinnerTestId
}: {
  label: string;
  spinnerTestId: string;
}): JSX.Element {
  "use memo";
  return (
    <div className={paletteStyles.loading}>
      <span
        aria-hidden="true"
        className={paletteStyles.loadingSpinner}
        data-testid={spinnerTestId}
      />
      <span>{label}</span>
    </div>
  );
}

function MentionPaletteHint({
  ariaLabel,
  cycleFilterLabel,
  moveSelectionLabel,
  navigateHierarchyLabel,
  onCycleFilter,
  onMoveSelection,
  onNavigateHierarchy,
  classNames,
  testId
}: {
  ariaLabel: string;
  cycleFilterLabel: string;
  moveSelectionLabel: string;
  navigateHierarchyLabel?: string;
  onCycleFilter: (delta: 1 | -1) => void;
  onMoveSelection: (delta: 1 | -1) => void;
  onNavigateHierarchy?: (delta: 1 | -1) => void;
  classNames: ResolvedMentionPaletteTheme["classNames"];
  testId: string;
}): JSX.Element {
  "use memo";
  const showHierarchyHints = Boolean(
    navigateHierarchyLabel && onNavigateHierarchy
  );
  return (
    <div
      className={classNames.hint}
      aria-label={ariaLabel}
      data-testid={testId}
    >
      <span className={classNames.hintItem} data-tooltip={cycleFilterLabel}>
        <span className={classNames.shortcutGroup}>
          <button
            className={cn(classNames.shortcut, classNames.shortcutButton)}
            type="button"
            aria-label={`Tab ${cycleFilterLabel}`}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => onCycleFilter(1)}
          >
            {/* i18n-check-ignore: Keyboard key label. */}
            Tab
          </button>
          {!showHierarchyHints ? (
            <>
              <button
                className={cn(
                  classNames.shortcut,
                  classNames.shortcutArrow,
                  classNames.shortcutButton
                )}
                type="button"
                aria-label={`← ${cycleFilterLabel}`}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => onCycleFilter(-1)}
              >
                ←
              </button>
              <button
                className={cn(
                  classNames.shortcut,
                  classNames.shortcutArrow,
                  classNames.shortcutButton
                )}
                type="button"
                aria-label={`→ ${cycleFilterLabel}`}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => onCycleFilter(1)}
              >
                →
              </button>
            </>
          ) : null}
        </span>
        <span>{cycleFilterLabel}</span>
      </span>
      {showHierarchyHints ? (
        <>
          <span className={classNames.hintSeparator} aria-hidden="true">
            ｜
          </span>
          <span
            className={classNames.hintItem}
            data-tooltip={navigateHierarchyLabel}
          >
            <span className={classNames.shortcutGroup}>
              <button
                className={cn(
                  classNames.shortcut,
                  classNames.shortcutArrow,
                  classNames.shortcutButton
                )}
                type="button"
                aria-label={`← ${navigateHierarchyLabel}`}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => onNavigateHierarchy?.(-1)}
              >
                ←
              </button>
              <button
                className={cn(
                  classNames.shortcut,
                  classNames.shortcutArrow,
                  classNames.shortcutButton
                )}
                type="button"
                aria-label={`→ ${navigateHierarchyLabel}`}
                onMouseDown={(event) => event.preventDefault()}
                onClick={() => onNavigateHierarchy?.(1)}
              >
                →
              </button>
            </span>
            <span>{navigateHierarchyLabel}</span>
          </span>
        </>
      ) : null}
      <span className={classNames.hintSeparator} aria-hidden="true">
        ｜
      </span>
      <span className={classNames.hintItem} data-tooltip={moveSelectionLabel}>
        <span className={classNames.shortcutGroup}>
          <button
            className={cn(
              classNames.shortcut,
              classNames.shortcutArrow,
              classNames.shortcutButton
            )}
            type="button"
            aria-label={`↑ ${moveSelectionLabel}`}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => onMoveSelection(-1)}
          >
            ↑
          </button>
          <button
            className={cn(
              classNames.shortcut,
              classNames.shortcutArrow,
              classNames.shortcutButton
            )}
            type="button"
            aria-label={`↓ ${moveSelectionLabel}`}
            onMouseDown={(event) => event.preventDefault()}
            onClick={() => onMoveSelection(1)}
          >
            ↓
          </button>
        </span>
        <span>{moveSelectionLabel}</span>
      </span>
    </div>
  );
}
