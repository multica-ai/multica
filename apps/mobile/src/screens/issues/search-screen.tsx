import { useCallback, useEffect, useRef, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, TextInput, View } from "react-native";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { Clock, MessageSquare, Search as SearchIcon, Trash2, X } from "lucide-react-native";
import { useTranslation } from "react-i18next";
import { api } from "@multica/core/api";
import type { IssueStatus, SearchIssueResult } from "@multica/core/types";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { EmptyState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import { formatIssueStatus } from "../../i18n/format";
import { mobileStorage } from "../../platform/storage";
import { colors, radii, spacing } from "../../theme/tokens";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import {
  addSearchHistoryItem,
  clearSearchHistory,
  readSearchHistory,
  removeSearchHistoryItem,
} from "./search-history";

const SEARCH_DEBOUNCE_MS = 300;
const SEARCH_LIMIT = 20;

type Props = NativeStackScreenProps<RootStackParamList, "Search">;

export function SearchScreen({ navigation }: Props) {
  const { t } = useTranslation();
  const { workspace } = useMobileWorkspace();
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchIssueResult[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [hasSearched, setHasSearched] = useState(false);
  const [isError, setIsError] = useState(false);
  const [history, setHistory] = useState<string[]>(() => readSearchHistory(mobileStorage, workspace.id));
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    setHistory(readSearchHistory(mobileStorage, workspace.id));
  }, [workspace.id]);

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      abortRef.current?.abort();
    };
  }, []);

  const commitHistory = useCallback((value: string) => {
    setHistory(addSearchHistoryItem(mobileStorage, workspace.id, value));
  }, [workspace.id]);

  const runSearch = useCallback((value: string, options?: { commit?: boolean }) => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    abortRef.current?.abort();

    const trimmed = value.trim();
    if (!trimmed) {
      setResults([]);
      setIsLoading(false);
      setHasSearched(false);
      setIsError(false);
      return;
    }

    setIsLoading(true);
    setIsError(false);
    debounceRef.current = setTimeout(() => {
      const controller = new AbortController();
      abortRef.current = controller;

      void (async () => {
        try {
          const res = await api.searchIssues({
            q: trimmed,
            limit: SEARCH_LIMIT,
            include_closed: true,
            signal: controller.signal,
          });
          if (!controller.signal.aborted) {
            setResults(res.issues);
            setHasSearched(true);
            if (options?.commit) commitHistory(trimmed);
          }
        } catch {
          if (!controller.signal.aborted) {
            setResults([]);
            setIsError(true);
            setHasSearched(true);
          }
        } finally {
          if (!controller.signal.aborted) {
            setIsLoading(false);
          }
        }
      })();
    }, SEARCH_DEBOUNCE_MS);
  }, [commitHistory]);

  const handleChangeText = useCallback((value: string) => {
    setQuery(value);
    runSearch(value);
  }, [runSearch]);

  const submitSearch = useCallback(() => {
    runSearch(query, { commit: true });
  }, [query, runSearch]);

  const selectHistoryItem = useCallback((value: string) => {
    setQuery(value);
    runSearch(value, { commit: true });
  }, [runSearch]);

  const deleteHistoryItem = useCallback((value: string) => {
    setHistory(removeSearchHistoryItem(mobileStorage, workspace.id, value));
  }, [workspace.id]);

  const clearHistory = useCallback(() => {
    setHistory(clearSearchHistory(mobileStorage, workspace.id));
  }, [workspace.id]);

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title={t("issues.search_title")} />
      <View style={styles.content}>
        <View style={styles.searchField}>
          <SearchIcon color={colors.mutedForeground} size={18} strokeWidth={2} />
          <TextInput
            autoCapitalize="none"
            autoCorrect={false}
            autoFocus
            clearButtonMode="while-editing"
            onChangeText={handleChangeText}
            onSubmitEditing={submitSearch}
            placeholder={t("issues.search_placeholder")}
            placeholderTextColor={colors.mutedForeground}
            returnKeyType="search"
            style={styles.searchInput}
            value={query}
          />
          {isLoading ? <ActivityIndicator color={colors.mutedForeground} size="small" /> : null}
        </View>

        {isError ? (
          <EmptyState detail={t("common.check_connection")} title={t("issues.unable_to_search")} />
        ) : null}

        {!isError && !isLoading && hasSearched && results.length === 0 ? (
          <EmptyState title={t("issues.no_matching")} />
        ) : null}

        {!isError && !hasSearched && !query.trim() && history.length === 0 ? (
          <EmptyState detail={t("issues.search_empty_detail")} title={t("issues.search_empty_title")} />
        ) : null}

        {!isError && !hasSearched && !query.trim() && history.length > 0 ? (
          <SearchHistoryList
            history={history}
            onClear={clearHistory}
            onDelete={deleteHistoryItem}
            onSelect={selectHistoryItem}
          />
        ) : null}

        {!isError && results.length > 0 ? (
          <FlatList
            contentContainerStyle={styles.results}
            data={results}
            keyboardShouldPersistTaps="handled"
            keyExtractor={(item) => item.id}
            renderItem={({ item }) => (
              <SearchResultItem
                issue={item}
                onPress={() => {
                  commitHistory(query);
                  navigation.navigate("IssueDetail", { issueId: item.id });
                }}
              />
            )}
          />
        ) : null}
      </View>
    </Screen>
  );
}

function SearchHistoryList({
  history,
  onClear,
  onDelete,
  onSelect,
}: {
  history: string[];
  onClear: () => void;
  onDelete: (query: string) => void;
  onSelect: (query: string) => void;
}) {
  const { t } = useTranslation();

  return (
    <View style={styles.historyPanel}>
      <View style={styles.historyHeader}>
        <Text style={styles.historyTitle}>{t("issues.recent_searches")}</Text>
        <Pressable
          accessibilityRole="button"
          onPress={onClear}
          style={({ pressed }) => [styles.historyClearButton, pressed && styles.pressed]}
        >
          <Trash2 color={colors.mutedForeground} size={15} strokeWidth={2} />
          <Text style={styles.historyClearText}>{t("issues.clear_history")}</Text>
        </Pressable>
      </View>
      <View style={styles.historyList}>
        {history.map((item) => (
          <View key={item} style={styles.historyItem}>
            <Pressable
              accessibilityRole="button"
              onPress={() => onSelect(item)}
              style={({ pressed }) => [styles.historyItemMain, pressed && styles.pressed]}
            >
              <Clock color={colors.mutedForeground} size={16} strokeWidth={2} />
              <Text numberOfLines={1} style={styles.historyItemText}>{item}</Text>
            </Pressable>
            <Pressable
              accessibilityLabel={t("issues.delete_history_item", { query: item })}
              accessibilityRole="button"
              onPress={() => onDelete(item)}
              style={({ pressed }) => [styles.historyDeleteButton, pressed && styles.pressed]}
            >
              <X color={colors.mutedForeground} size={16} strokeWidth={2} />
            </Pressable>
          </View>
        ))}
      </View>
    </View>
  );
}

function SearchResultItem({
  issue,
  onPress,
}: {
  issue: SearchIssueResult;
  onPress: () => void;
}) {
  const { t } = useTranslation();
  const statusColor = getStatusColor(issue.status);

  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.resultItem, pressed && styles.pressed]}
    >
      <View style={styles.resultHeader}>
        <View style={[styles.statusDot, { backgroundColor: statusColor }]} />
        <Text style={styles.identifier}>{issue.identifier}</Text>
        <Text numberOfLines={1} style={styles.title}>{issue.title}</Text>
      </View>
      <View style={styles.resultMetaRow}>
        <Text style={[styles.status, { color: statusColor }]}>
          {formatIssueStatus(t, issue.status)}
        </Text>
        {issue.match_source === "comment" && issue.matched_snippet ? (
          <View style={styles.snippetRow}>
            <MessageSquare color={colors.mutedForeground} size={13} strokeWidth={2} />
            <Text numberOfLines={1} style={styles.snippet}>{issue.matched_snippet}</Text>
          </View>
        ) : null}
        {issue.match_source === "description" && issue.matched_snippet ? (
          <Text numberOfLines={1} style={styles.snippet}>{issue.matched_snippet}</Text>
        ) : null}
      </View>
    </Pressable>
  );
}

function getStatusColor(status: IssueStatus) {
  switch (status) {
    case "in_progress":
      return colors.warning;
    case "in_review":
      return colors.success;
    case "done":
      return colors.info;
    case "blocked":
      return colors.destructive;
    default:
      return colors.mutedForeground;
  }
}

const styles = StyleSheet.create({
  content: {
    flex: 1,
    padding: spacing.lg,
  },
  searchField: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    height: 48,
    paddingHorizontal: spacing.md,
  },
  searchInput: {
    color: colors.foreground,
    flex: 1,
    fontSize: 16,
    height: "100%",
    includeFontPadding: false,
    paddingVertical: 0,
  },
  results: {
    gap: spacing.sm,
    paddingTop: spacing.md,
    paddingBottom: spacing.xl,
  },
  historyPanel: {
    gap: spacing.md,
    paddingTop: spacing.lg,
  },
  historyHeader: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
  },
  historyTitle: {
    color: colors.foreground,
    fontSize: 14,
    fontWeight: "600",
  },
  historyClearButton: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.xs,
    minHeight: 36,
    paddingHorizontal: spacing.xs,
  },
  historyClearText: {
    color: colors.mutedForeground,
    fontSize: 13,
    fontWeight: "500",
  },
  historyList: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  historyItem: {
    alignItems: "center",
    borderBottomColor: colors.border,
    borderBottomWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    minHeight: 48,
  },
  historyItemMain: {
    alignItems: "center",
    flex: 1,
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 48,
    minWidth: 0,
    paddingLeft: spacing.md,
    paddingRight: spacing.sm,
  },
  historyItemText: {
    color: colors.foreground,
    flex: 1,
    fontSize: 15,
    lineHeight: 20,
  },
  historyDeleteButton: {
    alignItems: "center",
    height: 48,
    justifyContent: "center",
    width: 48,
  },
  resultItem: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    padding: spacing.md,
  },
  resultHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.sm,
  },
  statusDot: {
    borderRadius: 4,
    height: 8,
    width: 8,
  },
  identifier: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "600",
  },
  title: {
    color: colors.foreground,
    flex: 1,
    fontSize: 15,
    fontWeight: "500",
    lineHeight: 20,
  },
  resultMetaRow: {
    gap: spacing.xs,
    paddingLeft: 16,
  },
  status: {
    fontSize: 12,
    fontWeight: "500",
  },
  snippetRow: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.xs,
  },
  snippet: {
    color: colors.mutedForeground,
    flexShrink: 1,
    fontSize: 12,
    lineHeight: 17,
  },
  pressed: {
    opacity: 0.72,
  },
});
