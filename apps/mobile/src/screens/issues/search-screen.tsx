import { useCallback, useEffect, useRef, useState } from "react";
import { ActivityIndicator, FlatList, Pressable, StyleSheet, Text, TextInput, View } from "react-native";
import type { NativeStackScreenProps } from "@react-navigation/native-stack";
import { MessageSquare, Search as SearchIcon } from "lucide-react-native";
import { api } from "@multica/core/api";
import { STATUS_CONFIG } from "@multica/core/issues/config/status";
import type { IssueStatus, SearchIssueResult } from "@multica/core/types";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { EmptyState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import { colors, radii, spacing } from "../../theme/tokens";

const SEARCH_DEBOUNCE_MS = 300;
const SEARCH_LIMIT = 20;

type Props = NativeStackScreenProps<RootStackParamList, "Search">;

export function SearchScreen({ navigation }: Props) {
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<SearchIssueResult[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [hasSearched, setHasSearched] = useState(false);
  const [isError, setIsError] = useState(false);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
      abortRef.current?.abort();
    };
  }, []);

  const runSearch = useCallback((value: string) => {
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
  }, []);

  const handleChangeText = useCallback((value: string) => {
    setQuery(value);
    runSearch(value);
  }, [runSearch]);

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title="Search" />
      <View style={styles.content}>
        <View style={styles.searchField}>
          <SearchIcon color={colors.mutedForeground} size={18} strokeWidth={2} />
          <TextInput
            autoCapitalize="none"
            autoCorrect={false}
            autoFocus
            clearButtonMode="while-editing"
            onChangeText={handleChangeText}
            placeholder="Search issues..."
            placeholderTextColor={colors.mutedForeground}
            returnKeyType="search"
            style={styles.searchInput}
            value={query}
          />
          {isLoading ? <ActivityIndicator color={colors.mutedForeground} size="small" /> : null}
        </View>

        {isError ? (
          <EmptyState detail="Check your connection and try again." title="Unable to search issues" />
        ) : null}

        {!isError && !isLoading && hasSearched && results.length === 0 ? (
          <EmptyState title="No matching issues" />
        ) : null}

        {!isError && !hasSearched && !query.trim() ? (
          <EmptyState detail="Search by issue title, description, identifier, or comment." title="Search issues" />
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
                onPress={() => navigation.navigate("IssueDetail", { issueId: item.id })}
              />
            )}
          />
        ) : null}
      </View>
    </Screen>
  );
}

function SearchResultItem({
  issue,
  onPress,
}: {
  issue: SearchIssueResult;
  onPress: () => void;
}) {
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
          {STATUS_CONFIG[issue.status].label}
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
