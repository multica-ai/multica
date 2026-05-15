/**
 * Workspace global search modal.
 *
 * Mirrors packages/views/search/search-command.tsx but is scoped to
 * search-only — mobile IA puts page nav in the More popover and
 * workspace switching in Settings, so a command-palette here would
 * duplicate them (see feedback_mobile_ia_main_vs_more).
 *
 * Result categories, ordering (projects first, issues second), debounce
 * (300ms), abort policy, and Recent rendering mirror the web source.
 * Highlight + snippet line for `match_source` matches preserves the
 * "why did this match" signal users rely on when scanning results.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  FlatList,
  KeyboardAvoidingView,
  Platform,
  Pressable,
  TextInput,
  View,
  type ListRenderItem,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQueries } from "@tanstack/react-query";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import type {
  Issue,
  IssueStatus,
  SearchIssueResult,
  SearchProjectResult,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ProjectIcon } from "@/components/ui/project-icon";
import { ProjectStatusIcon } from "@/components/ui/project-status-icon";
import { api } from "@/data/api";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  selectViewedIssueIds,
  useViewedIssuesStore,
} from "@/data/viewed-issues-store";
import { issueDetailOptions } from "@/data/queries/issues";
import { STATUS_LABEL } from "@/lib/issue-status";
import { projectStatusLabel } from "@/lib/project-status";

const DEBOUNCE_MS = 300;
const ISSUE_LIMIT = 20;
const PROJECT_LIMIT = 10;
const RECENT_LIMIT = 5;

// =====================================================
// HighlightText — mobile port of web's HighlightText
// =====================================================
// Web uses an HTML <mark> which doesn't exist in RN, so we segment the
// string ourselves and wrap matched parts in a styled <Text>. Same regex
// escape + case-insensitive substring match as
// packages/views/search/search-command.tsx:55-89.

interface HighlightTextProps {
  text: string;
  query: string;
  className?: string;
  numberOfLines?: number;
}

function HighlightText({
  text,
  query,
  className,
  numberOfLines,
}: HighlightTextProps) {
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
    <Text className={className} numberOfLines={numberOfLines}>
      {parts.map((p, i) =>
        p.hit ? (
          // Inline hex (yellow-200) instead of a Tailwind class because the
          // mobile tailwind.config.js intentionally curates its own palette
          // (no `yellow-*`) — see apps/mobile/CLAUDE.md "Visual tokens".
          <Text
            key={i}
            className="text-foreground"
            style={{ backgroundColor: "#fef08a" }}
          >
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

type RowItem =
  | { kind: "header"; key: string; title: string }
  | { kind: "issue"; key: string; issue: SearchIssueResult; query: string }
  | { kind: "project"; key: string; project: SearchProjectResult; query: string }
  | { kind: "recent"; key: string; issue: Issue };

function issueIconColor(status: IssueStatus): string {
  // Tag color for the status label at the end of an issue row.
  // Mirrors STATUS_CONFIG.iconColor (status-icon.tsx STATUS_COLOR) so the
  // text tint matches the leading status icon visually.
  switch (status) {
    case "in_progress":
      return "text-warning";
    case "in_review":
      return "text-success";
    case "done":
      return "text-info";
    case "blocked":
      return "text-destructive";
    default:
      return "text-muted-foreground";
  }
}

function navigateOnTap(slug: string | null, path: string) {
  // Search is `presentation: "modal"` (see (app)/[workspace]/_layout.tsx).
  // `router.replace` swaps the modal out for the destination in a single
  // atomic transition — the new screen renders with its own presentation
  // (default `card`), and the resulting history is `[..., inbox, detail]`,
  // so the user's back gesture lands on the screen that was under search.
  if (!slug) return;
  router.replace(path);
}

interface SearchIssueRowProps {
  item: SearchIssueResult;
  query: string;
  slug: string | null;
}

function SearchIssueRow({ item, query, slug }: SearchIssueRowProps) {
  // Web only renders the snippet line for comment matches
  // (packages/views/search/search-command.tsx:632) and the backend only
  // populates `matched_snippet` for comment matches anyway
  // (server/internal/handler/issue.go:592). Keep mobile strictly aligned.
  const showSnippet =
    item.match_source === "comment" && !!item.matched_snippet;
  const statusLabel = STATUS_LABEL[item.status as IssueStatus] ?? item.status;
  return (
    <Pressable
      onPress={() => navigateOnTap(slug, `/${slug}/issue/${item.id}`)}
      className="active:bg-secondary px-4 py-3"
    >
      <View className="flex-row items-center gap-3">
        <StatusIcon status={item.status as IssueStatus} size={14} />
        <PriorityIcon priority={item.priority} size={14} />
        <Text className="text-xs text-muted-foreground shrink-0 w-16">
          {item.identifier}
        </Text>
        <View className="flex-1">
          <HighlightText
            text={item.title}
            query={query}
            className="text-sm text-foreground"
            numberOfLines={1}
          />
        </View>
        <Text className={`text-xs shrink-0 ${issueIconColor(item.status as IssueStatus)}`}>
          {statusLabel}
        </Text>
      </View>
      {showSnippet ? (
        <View className="flex-row items-start gap-2 mt-1 pl-[68px]">
          <Ionicons
            name="chatbubble-outline"
            size={12}
            color="#71717a"
            style={{ marginTop: 2 }}
          />
          <View className="flex-1">
            <HighlightText
              text={item.matched_snippet ?? ""}
              query={query}
              className="text-xs text-muted-foreground"
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
  slug: string | null;
}

function SearchProjectRow({ item, query, slug }: SearchProjectRowProps) {
  const showSnippet =
    item.match_source === "description" && !!item.matched_snippet;
  return (
    <Pressable
      onPress={() => navigateOnTap(slug, `/${slug}/project/${item.id}`)}
      className="active:bg-secondary px-4 py-3"
    >
      <View className="flex-row items-center gap-3">
        <ProjectIcon icon={item.icon} size="md" />
        <View className="flex-1">
          <HighlightText
            text={item.title}
            query={query}
            className="text-sm text-foreground"
            numberOfLines={1}
          />
        </View>
        <View className="flex-row items-center gap-1.5 shrink-0">
          <ProjectStatusIcon status={item.status} size={12} />
          <Text className="text-xs text-muted-foreground">
            {projectStatusLabel(item.status)}
          </Text>
        </View>
      </View>
      {showSnippet ? (
        <View className="flex-row items-start mt-1 pl-[36px]">
          <View className="flex-1">
            <HighlightText
              text={item.matched_snippet ?? ""}
              query={query}
              className="text-xs text-muted-foreground"
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
  slug: string | null;
}

function RecentRow({ item, slug }: RecentRowProps) {
  const statusLabel = STATUS_LABEL[item.status as IssueStatus] ?? item.status;
  return (
    <Pressable
      onPress={() => navigateOnTap(slug, `/${slug}/issue/${item.id}`)}
      className="active:bg-secondary px-4 py-3"
    >
      <View className="flex-row items-center gap-3">
        <StatusIcon status={item.status as IssueStatus} size={14} />
        <Text className="text-xs text-muted-foreground shrink-0 w-16">
          {item.identifier}
        </Text>
        <Text className="flex-1 text-sm text-foreground" numberOfLines={1}>
          {item.title}
        </Text>
        <Text className={`text-xs shrink-0 ${issueIconColor(item.status as IssueStatus)}`}>
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

export default function SearchModal() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const slug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

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
        // the user dismissed the modal. Drift / network errors are already
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
  // single virtualised list covers Recent (empty-state) and (Projects +
  // Issues) results without nesting SectionList inside another scroller.
  const data = useMemo<RowItem[]>(() => {
    if (!trimmedQuery) {
      if (recentIssues.length === 0) return [];
      return [
        { kind: "header", key: "h-recent", title: "Recent" },
        ...recentIssues.map<RowItem>((issue) => ({
          kind: "recent",
          key: `r-${issue.id}`,
          issue,
        })),
      ];
    }
    const items: RowItem[] = [];
    if (results.projects.length > 0) {
      items.push({ kind: "header", key: "h-projects", title: "Projects" });
      for (const p of results.projects) {
        items.push({ kind: "project", key: `p-${p.id}`, project: p, query: trimmedQuery });
      }
    }
    if (results.issues.length > 0) {
      items.push({ kind: "header", key: "h-issues", title: "Issues" });
      for (const it of results.issues) {
        items.push({ kind: "issue", key: `i-${it.id}`, issue: it, query: trimmedQuery });
      }
    }
    return items;
  }, [trimmedQuery, recentIssues, results]);

  const renderItem = useCallback<ListRenderItem<RowItem>>(
    ({ item }) => {
      switch (item.kind) {
        case "header":
          return (
            <Text className="px-4 pt-4 pb-1 text-xs font-medium text-muted-foreground uppercase">
              {item.title}
            </Text>
          );
        case "issue":
          return <SearchIssueRow item={item.issue} query={item.query} slug={slug} />;
        case "project":
          return <SearchProjectRow item={item.project} query={item.query} slug={slug} />;
        case "recent":
          return <RecentRow item={item.issue} slug={slug} />;
      }
    },
    [slug],
  );

  return (
    <SafeAreaView className="flex-1 bg-background" edges={["bottom"]}>
      <KeyboardAvoidingView
        className="flex-1"
        behavior={Platform.OS === "ios" ? "padding" : undefined}
      >
        {/* Search input row */}
        <View className="flex-row items-center gap-3 border-b border-border px-4 py-2">
          <Ionicons name="search" size={20} color="#71717a" />
          <TextInput
            value={query}
            onChangeText={handleChange}
            placeholder="Search issues and projects"
            placeholderTextColor="#a1a1aa"
            autoFocus
            autoCorrect={false}
            autoCapitalize="none"
            returnKeyType="search"
            clearButtonMode="while-editing"
            className="flex-1 text-base text-foreground"
          />
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
              <View className="items-center justify-center py-12">
                <ActivityIndicator color="#71717a" />
              </View>
            ) : trimmedQuery && !hasResults ? (
              <View className="items-center justify-center py-12 px-6">
                <Text className="text-sm text-muted-foreground text-center">
                  No results for &ldquo;{trimmedQuery}&rdquo;
                </Text>
              </View>
            ) : !trimmedQuery && recentIssues.length === 0 ? (
              <View className="items-center justify-center py-12 px-6">
                <Text className="text-sm text-muted-foreground text-center">
                  Type to search issues and projects.
                </Text>
              </View>
            ) : null
          }
          ListFooterComponent={
            isLoading && hasResults ? (
              <View className="items-center justify-center py-4">
                <ActivityIndicator color="#71717a" />
              </View>
            ) : null
          }
        />
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}
