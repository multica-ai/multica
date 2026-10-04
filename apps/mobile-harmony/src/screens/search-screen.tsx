/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/search.tsx —
 * workspace global search.
 *
 * Mirrors packages/views/search/search-command.tsx but is scoped to
 * search-only — mobile IA puts page nav in the More popover and workspace
 * switching in Settings, so a command-palette here would duplicate them.
 *
 * Result categories, ordering (live projects, then live issues, then a
 * trailing Cancelled section — see lib/search-rows.ts), debounce (300ms),
 * abort policy, and Recent rendering mirror the web source. Highlight +
 * snippet line for `match_source` matches preserves the "why did this
 * match" signal users rely on when scanning results.
 *
 * Platform deltas from the iOS file:
 *   - The iOS `use-native-search-bar` modal chrome (blur, cancel button) is
 *     iOS-only; here the search field is a plain auto-focused TextInput in a
 *     bordered header row, with an inline clear button standing in for
 *     `clearButtonMode="while-editing"`.
 *   - The iOS `router.replace` on row tap becomes onOpenIssue/onOpenProject
 *     callbacks — the shell owns the stack push for issue/project detail.
 */
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, FlatList, StyleSheet, TextInput, View, type ListRenderItem, type StyleProp, type TextStyle } from "react-native";
import { useKeyboardHeight } from "@/lib/use-keyboard-height";
import { Pressable } from "@/components/ui/pressable";
import { useQueries } from "@tanstack/react-query";
import type {
  Issue,
  IssueStatusCategory,
  SearchIssueResult,
  SearchProjectResult,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ProjectIcon } from "@/components/ui/project-icon";
import { ProjectStatusIcon } from "@/components/ui/project-status-icon";
import { Icon } from "@/components/ui/icon";
import { SafeAreaView } from "@/lib/safe-area";
import { api } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  selectViewedIssueIds,
  useViewedIssuesStore,
} from "@/data/viewed-issues-store";
import { issueDetailOptions } from "@/data/queries/issues";
import { issueColumnCategory } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { projectStatusLabel } from "@/lib/project-status";
import { buildSearchRows, type RowItem } from "@/lib/search-rows";
import type { ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

const DEBOUNCE_MS = 300;
const ISSUE_LIMIT = 20;
const PROJECT_LIMIT = 10;
const RECENT_LIMIT = 5;

// =====================================================
// HighlightText — port of web's HighlightText
// =====================================================
// Web uses an HTML <mark> which doesn't exist in RN, so we segment the
// string ourselves and wrap matched parts in a styled <Text>. Same regex
// escape + case-insensitive substring match as
// packages/views/search/search-command.tsx:55-89.

interface HighlightTextProps {
  text: string;
  query: string;
  style?: StyleProp<TextStyle>;
  numberOfLines?: number;
}

function HighlightText({
  text,
  query,
  style,
  numberOfLines,
}: HighlightTextProps) {
  const c = useThemeColors();
  const parts = useMemo(() => {
    const q = query.trim();
    if (!q) return [{ text, hit: false }];
    const escaped = q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const regex = new RegExp(`(${escaped})`, "gi");
    const out: { text: string; hit: boolean }[] = [];
    let last = 0;
    let m: RegExpExecArray | null;
    while ((m = regex.exec(text)) !== null) {
      if (m.index > last) out.push({ text: text.slice(last, m.index), hit: false });
      out.push({ text: m[0], hit: true });
      last = regex.lastIndex;
    }
    if (last < text.length) out.push({ text: text.slice(last), hit: false });
    return out.length > 0 ? out : [{ text, hit: false }];
  }, [text, query]);

  return (
    <Text style={style} numberOfLines={numberOfLines}>
      {parts.map((p, i) =>
        p.hit ? (
          // Inline hex (yellow-200) instead of a theme token for parity with
          // the iOS screen, which hardcodes the same highlight color.
          <Text key={i} style={{ color: c.foreground, backgroundColor: "#fef08a" }}>
            {p.text}
          </Text>
        ) : (
          <Text key={i}>{p.text}</Text>
        ),
      )}
    </Text>
  );
}

// =====================================================
// Row item types — drives the single FlatList render
// =====================================================
// RowItem + buildSearchRows live in lib/search-rows.ts so the ordering rules
// (including the cancelled partition) are testable without mounting the screen.

function issueStatusColor(category: IssueStatusCategory): string {
  // Tag color for the status label at the end of an issue row. Mirrors the
  // tailwind tokens the iOS screen resolves via classes (text-warning /
  // text-info / text-muted-foreground → apps/mobile/tailwind.config.js hex).
  // Keyed on CATEGORY: a custom status inherits its category's tint.
  switch (category) {
    case "started":
      return "#eab308";
    case "done":
      return "#3b82f6";
    default:
      return "#71717a";
  }
}

interface SearchIssueRowProps {
  item: SearchIssueResult;
  query: string;
  onOpen: (issueId: string) => void;
  c: ThemeColors;
  s: ReturnType<typeof styles>;
}

function SearchIssueRow({ item, query, onOpen, c, s }: SearchIssueRowProps) {
  // Web only renders the snippet line for comment matches
  // (packages/views/search/search-command.tsx:632) and the backend only
  // populates `matched_snippet` for comment matches anyway
  // (server/internal/handler/issue.go:592). Keep mobile strictly aligned.
  const showSnippet =
    item.match_source === "comment" && !!item.matched_snippet;
  const { colorOf, labelOf, iconOf } = useIssueStatuses();
  const category = issueColumnCategory(item);
  const statusLabel = labelOf(item.status);
  return (
    <Pressable
      onPress={() => onOpen(item.id)}
      style={({ pressed }) => [s.row, pressed ? s.pressedRow : null]}
    >
      <View style={s.issueMain}>
        <StatusIcon
          status={item.status}
          category={category}
          icon={iconOf(item.status)}
          color={colorOf(item.status)}
          size={14}
        />
        <PriorityIcon priority={item.priority} size={14} />
        <Text style={[s.identifier, { color: c.mutedForeground }]}>
          {item.identifier}
        </Text>
        <View style={s.titleWrap}>
          <HighlightText
            text={item.title}
            query={query}
            style={s.title}
            numberOfLines={1}
          />
        </View>
        <Text style={[s.statusLabel, { color: issueStatusColor(category) }]}>
          {statusLabel}
        </Text>
      </View>
      {showSnippet ? (
        <View style={s.snippetRowIssue}>
          <View style={s.snippetIcon}>
            <Icon name="chatbubble-outline" size={12} color="#71717a" />
          </View>
          <View style={s.titleWrap}>
            <HighlightText
              text={item.matched_snippet ?? ""}
              query={query}
              style={[s.snippet, { color: c.mutedForeground }]}
              numberOfLines={1}
            />
          </View>
        </View>
      ) : null}
    </Pressable>
  );
}

interface SearchProjectRowProps {
  item: SearchProjectResult;
  query: string;
  onOpen: (projectId: string) => void;
  c: ThemeColors;
  s: ReturnType<typeof styles>;
}

function SearchProjectRow({ item, query, onOpen, c, s }: SearchProjectRowProps) {
  const showSnippet =
    item.match_source === "description" && !!item.matched_snippet;
  return (
    <Pressable
      onPress={() => onOpen(item.id)}
      style={({ pressed }) => [s.row, pressed ? s.pressedRow : null]}
    >
      <View style={s.issueMain}>
        <ProjectIcon icon={item.icon} size="md" />
        <View style={s.titleWrap}>
          <HighlightText
            text={item.title}
            query={query}
            style={s.title}
            numberOfLines={1}
          />
        </View>
        <View style={s.projectStatusWrap}>
          <ProjectStatusIcon status={item.status} size={12} />
          <Text style={[s.projectStatusLabel, { color: c.mutedForeground }]}>
            {projectStatusLabel(item.status)}
          </Text>
        </View>
      </View>
      {showSnippet ? (
        <View style={s.snippetRowProject}>
          <View style={s.titleWrap}>
            <HighlightText
              text={item.matched_snippet ?? ""}
              query={query}
              style={[s.snippet, { color: c.mutedForeground }]}
              numberOfLines={1}
            />
          </View>
        </View>
      ) : null}
    </Pressable>
  );
}

interface RecentRowProps {
  item: Issue;
  onOpen: (issueId: string) => void;
  c: ThemeColors;
  s: ReturnType<typeof styles>;
}

function RecentRow({ item, onOpen, c, s }: RecentRowProps) {
  const { colorOf, labelOf, iconOf } = useIssueStatuses();
  const category = issueColumnCategory(item);
  const statusLabel = labelOf(item.status);
  return (
    <Pressable
      onPress={() => onOpen(item.id)}
      style={({ pressed }) => [s.row, pressed ? s.pressedRow : null]}
    >
      <View style={s.issueMain}>
        <StatusIcon
          status={item.status}
          category={category}
          icon={iconOf(item.status)}
          color={colorOf(item.status)}
          size={14}
        />
        <Text style={[s.identifier, { color: c.mutedForeground }]}>
          {item.identifier}
        </Text>
        <Text style={[s.title, { color: c.foreground }]} numberOfLines={1}>
          {item.title}
        </Text>
        <Text style={[s.statusLabel, { color: issueStatusColor(category) }]}>
          {statusLabel}
        </Text>
      </View>
    </Pressable>
  );
}

// =====================================================
// Screen
// =====================================================

interface SearchResultsState {
  issues: SearchIssueResult[];
  projects: SearchProjectResult[];
}

const EMPTY_RESULTS: SearchResultsState = { issues: [], projects: [] };

export function SearchScreen({
  onOpenIssue,
  onOpenProject,
  onClose,
}: {
  onOpenIssue?: (issueId: string) => void;
  onOpenProject?: (projectId: string) => void;
  onClose?: () => void;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const keyboardHeight = useKeyboardHeight();
  const wsId = useWorkspaceStore((state) => state.currentWorkspaceId);

  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchResultsState>(EMPTY_RESULTS);
  const [isLoading, setIsLoading] = useState(false);

  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  // Recent — mirrors mention-suggestion-bar.tsx:85-95.
  const viewedIds = useViewedIssuesStore(selectViewedIssueIds(wsId));
  const recentIds = useMemo(
    () => viewedIds.slice(0, RECENT_LIMIT),
    [viewedIds],
  );
  const recentQueries = useQueries({
    queries: recentIds.map((id) => issueDetailOptions(wsId, id)),
  });
  const recentIssues = useMemo<Issue[]>(
    () =>
      recentQueries
        .map((q) => q.data)
        .filter((i): i is Issue => !!i),
    [recentQueries],
  );

  // Cleanup pending debounce + abort on unmount. Without this, navigating
  // away mid-request leaves a dangling timeout + an in-flight fetch whose
  // setState would warn against an unmounted component.
  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      if (abortRef.current) abortRef.current.abort();
    };
  }, []);

  const runSearch = useCallback((q: string) => {
    // Race-correctness: clear the pending debounce AND abort any in-flight
    // controller BEFORE the early-return / state writes below. The abort
    // is synchronous (signal.aborted flips immediately), so the post-await
    // guard in the timeout body will skip stale `setResults` / `setIsLoading`
    // even if the network response arrives later.
    if (debounceRef.current) clearTimeout(debounceRef.current);
    if (abortRef.current) abortRef.current.abort();

    if (!q.trim()) {
      setResults(EMPTY_RESULTS);
      setIsLoading(false);
      return;
    }

    setIsLoading(true);
    debounceRef.current = setTimeout(async () => {
      const controller = new AbortController();
      abortRef.current = controller;
      try {
        const [issueRes, projectRes] = await Promise.all([
          api.searchIssues(
            { q: q.trim(), limit: ISSUE_LIMIT, include_closed: true },
            { signal: controller.signal },
          ),
          api.searchProjects(
            { q: q.trim(), limit: PROJECT_LIMIT, include_closed: true },
            { signal: controller.signal },
          ),
        ]);
        if (!controller.signal.aborted) {
          setResults({ issues: issueRes.issues, projects: projectRes.projects });
          setIsLoading(false);
        }
      } catch {
        // Abort throws here too; ignore — a newer request is in flight, or
        // the user left the screen. Drift / network errors are already
        // logged inside parseWithFallback + the api logger.
        if (!controller.signal.aborted) setIsLoading(false);
      }
    }, DEBOUNCE_MS);
  }, []);

  const handleChange = useCallback(
    (value: string) => {
      setQuery(value);
      runSearch(value);
    },
    [runSearch],
  );

  const trimmedQuery = query.trim();
  const hasResults =
    results.issues.length > 0 || results.projects.length > 0;

  // Build the FlatList data. One flat array of discriminated rows means a
  // single virtualised list covers Recent (empty-state) and the search results
  // without nesting SectionList inside another scroller. Ordering lives in
  // buildSearchRows (lib/search-rows.ts).
  const data = useMemo<RowItem[]>(
    () =>
      buildSearchRows({
        query,
        issues: results.issues,
        projects: results.projects,
        recentIssues,
      }),
    [query, results, recentIssues],
  );

  const openIssue = useCallback(
    (issueId: string) => {
      onOpenIssue?.(issueId);
    },
    [onOpenIssue],
  );

  const openProject = useCallback(
    (projectId: string) => {
      onOpenProject?.(projectId);
    },
    [onOpenProject],
  );

  const renderItem = useCallback<ListRenderItem<RowItem>>(
    ({ item }) => {
      switch (item.kind) {
        case "header":
          return (
            <Text
              style={[s.sectionHeader, { color: c.mutedForeground }]}
            >
              {item.title}
            </Text>
          );
        case "issue":
          return (
            <SearchIssueRow
              item={item.issue}
              query={item.query}
              onOpen={openIssue}
              c={c}
              s={s}
            />
          );
        case "project":
          return (
            <SearchProjectRow
              item={item.project}
              query={item.query}
              onOpen={openProject}
              c={c}
              s={s}
            />
          );
        case "recent":
          return (
            <RecentRow item={item.issue} onOpen={openIssue} c={c} s={s} />
          );
      }
    },
    [c, s, openIssue, openProject],
  );

  return (
    <SafeAreaView edges={["top"]} style={s.screen}>
      <View style={[s.flex, { paddingBottom: keyboardHeight }]}>
        {/* Search input row — iOS's native search bar stand-in. */}
        <View style={[s.searchRow, { borderBottomColor: c.border }]}>
          {onClose ? (
            <Pressable
              onPress={onClose}
              accessibilityLabel="Close search"
              style={s.closeButton}
              hitSlop={8}
            >
              <Icon name="chevron-back" size={22} color={c.foreground} />
            </Pressable>
          ) : null}
          <Icon name="search" size={20} color={c.mutedForeground} />
          <TextInput
            value={query}
            onChangeText={handleChange}
            placeholder="Search issues and projects"
            placeholderTextColor={c.mutedForeground}
            autoFocus
            autoCorrect={false}
            autoCapitalize="none"
            returnKeyType="search"
            style={[s.input, { color: c.foreground }]}
          />
          {query.length > 0 ? (
            <Pressable
              onPress={() => handleChange("")}
              accessibilityLabel="Clear search"
              hitSlop={8}
              style={s.clearButton}
            >
              <Icon name="close-circle" size={18} color={c.mutedForeground} />
            </Pressable>
          ) : null}
        </View>

        {/* Body */}
        <FlatList
          data={data}
          renderItem={renderItem}
          keyExtractor={(item) => item.key}
          keyboardShouldPersistTaps="handled"
          keyboardDismissMode="on-drag"
          ListEmptyComponent={
            isLoading ? (
              <View style={s.spinnerWrap}>
                <ActivityIndicator color={c.mutedForeground} />
              </View>
            ) : trimmedQuery && !hasResults ? (
              <View style={s.emptyWrap}>
                <Text style={[s.emptyText, { color: c.mutedForeground }]}>
                  No results for “{trimmedQuery}”
                </Text>
              </View>
            ) : null
          }
          ListFooterComponent={
            isLoading && hasResults ? (
              <View style={s.footerWrap}>
                <ActivityIndicator color={c.mutedForeground} />
              </View>
            ) : null
          }
        />
      </View>
    </SafeAreaView>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    flex: { flex: 1 },
    screen: { flex: 1, backgroundColor: c.background },
    searchRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      borderBottomWidth: 1,
      paddingHorizontal: 16,
      paddingVertical: 8,
    },
    closeButton: { marginRight: -4 },
    clearButton: { marginLeft: -4 },
    input: { flex: 1, fontSize: 16, paddingVertical: 0 },
    sectionHeader: {
      paddingHorizontal: 16,
      paddingTop: 16,
      paddingBottom: 4,
      fontSize: 12,
      fontWeight: "500",
      textTransform: "uppercase",
    },
    row: { paddingHorizontal: 16, paddingVertical: 12 },
    pressedRow: { backgroundColor: c.secondary },
    issueMain: { flexDirection: "row", alignItems: "center", gap: 12 },
    identifier: { fontSize: 12, flexShrink: 0, width: 64 },
    titleWrap: { flex: 1 },
    title: { fontSize: 14, color: c.foreground },
    statusLabel: { fontSize: 12, flexShrink: 0 },
    projectStatusWrap: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      flexShrink: 0,
    },
    projectStatusLabel: { fontSize: 12 },
    snippetRowIssue: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 8,
      marginTop: 4,
      paddingLeft: 68,
    },
    snippetRowProject: {
      flexDirection: "row",
      alignItems: "flex-start",
      marginTop: 4,
      paddingLeft: 36,
    },
    snippetIcon: { marginTop: 2 },
    snippet: { fontSize: 12 },
    spinnerWrap: { alignItems: "center", justifyContent: "center", paddingVertical: 48 },
    emptyWrap: {
      alignItems: "center",
      justifyContent: "center",
      paddingVertical: 48,
      paddingHorizontal: 24,
    },
    emptyText: { fontSize: 14, textAlign: "center" },
    footerWrap: { alignItems: "center", justifyContent: "center", paddingVertical: 16 },
  });
