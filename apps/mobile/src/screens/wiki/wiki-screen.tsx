import { useCallback, useMemo } from "react";
import { FlatList, Pressable, RefreshControl, ScrollView, StyleSheet, Text, View } from "react-native";
import { useNavigation, useRoute, type RouteProp } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { buildWikiTree, useWikiPageDetail, useWikiPageList, type WikiPageTreeNode } from "@multica/core/wiki";
import { useActorName } from "@multica/core/workspace/hooks";
import { BookOpen, ChevronRight, FileText } from "lucide-react-native";
import { EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { MarkdownText } from "../../components/ui/markdown";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

type WikiNavigation = NativeStackNavigationProp<RootStackParamList>;
type WikiDetailRoute = RouteProp<RootStackParamList, "WikiDetail">;

type WikiListItem = {
  depth: number;
  node: WikiPageTreeNode;
};

export function WikiScreen() {
  const { t } = useTranslation();
  const navigation = useNavigation<WikiNavigation>();
  const { workspace } = useMobileWorkspace();
  const {
    data: pages = [],
    isError,
    isLoading,
    isRefetching,
    refetch,
  } = useWikiPageList(workspace.id);
  const items = useMemo(() => flattenTreeWithDepth(buildWikiTree(pages)), [pages]);

  if (isLoading) return <LoadingState />;
  if (isError) {
    return <EmptyState detail={t("common.pull_to_retry")} title={t("wiki.unable_to_load")} />;
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title={t("wiki.title")} />
      <FlatList
        contentContainerStyle={items.length === 0 ? styles.emptyList : styles.list}
        data={items}
        keyExtractor={(item) => item.node.id}
        ListEmptyComponent={
          <EmptyState detail={t("wiki.empty_detail")} title={t("wiki.empty_title")} />
        }
        onRefresh={refetch}
        refreshing={isRefetching}
        renderItem={({ item }) => (
          <WikiPageRow
            item={item}
            onPress={() => navigation.navigate("WikiDetail", { pageId: item.node.id })}
          />
        )}
        showsVerticalScrollIndicator={false}
      />
    </Screen>
  );
}

export function WikiDetailScreen() {
  const { t } = useTranslation();
  const navigation = useNavigation<WikiNavigation>();
  const route = useRoute<WikiDetailRoute>();
  const { workspace } = useMobileWorkspace();
  const { getActorName } = useActorName();
  const {
    data: page,
    isError,
    isLoading,
    isRefetching,
    refetch,
  } = useWikiPageDetail(workspace.id, route.params.pageId);
  const openIssueMention = useCallback(
    (issueId: string) => {
      navigation.navigate("IssueDetail", { issueId });
    },
    [navigation],
  );

  if (isLoading) return <LoadingState />;
  if (isError || !page) {
    return <EmptyState detail={t("common.pull_to_retry")} title={t("wiki.unable_to_load")} />;
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar onBack={() => navigation.goBack()} title={page.title || t("wiki.untitled")} />
      <ScrollView
        contentContainerStyle={styles.detailContent}
        refreshControl={
          <RefreshControl
            onRefresh={() => {
              void refetch();
            }}
            refreshing={isRefetching}
            tintColor={colors.foreground}
          />
        }
        showsVerticalScrollIndicator={false}
      >
        <View style={styles.detailHeader}>
          <View style={styles.detailIconWrap}>
            <BookOpen color={colors.foreground} size={22} />
          </View>
          <View style={styles.detailTitleWrap}>
            <Text style={styles.detailTitle}>{page.title || t("wiki.untitled")}</Text>
            <WikiPageMeta
              createdAt={page.created_at}
              createdBy={page.created_by}
              getActorName={getActorName}
              updatedAt={page.updated_at}
              updatedBy={page.updated_by}
            />
          </View>
        </View>
        {page.content.trim() ? (
          <MarkdownText content={page.content} onIssueMentionPress={openIssueMention} />
        ) : (
          <View style={styles.noContent}>
            <Text style={styles.noContentTitle}>{t("wiki.no_content")}</Text>
          </View>
        )}
      </ScrollView>
    </Screen>
  );
}

function WikiPageRow({
  item,
  onPress,
}: {
  item: WikiListItem;
  onPress: () => void;
}) {
  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [
        styles.row,
        { marginLeft: Math.min(item.depth, 4) * spacing.lg },
        pressed && styles.pressed,
      ]}
    >
      <View style={styles.rowIconWrap}>
        <FileText color={colors.mutedForeground} size={18} />
      </View>
      <View style={styles.rowText}>
        <Text numberOfLines={1} style={styles.rowTitle}>
          {item.node.title}
        </Text>
        {item.node.children.length > 0 ? (
          <Text style={styles.rowMeta}>{item.node.children.length}</Text>
        ) : null}
      </View>
      <ChevronRight color={colors.mutedForeground} size={18} />
    </Pressable>
  );
}

function WikiPageMeta({
  createdAt,
  createdBy,
  getActorName,
  updatedAt,
  updatedBy,
}: {
  createdAt: string;
  createdBy: string | null;
  getActorName: (type: string, id: string) => string;
  updatedAt: string;
  updatedBy: string | null;
}) {
  const { t } = useTranslation();
  const creatorName = resolveMemberName(createdBy, getActorName, t);
  const updaterName = resolveMemberName(updatedBy, getActorName, t);
  const showUpdated = createdAt !== updatedAt;

  return (
    <View style={styles.detailMetaGroup}>
      <Text style={styles.detailMeta}>
        {t("wiki.created_by", { name: creatorName })}
        <Text style={styles.detailMetaSeparator}> · </Text>
        {formatWikiRelativeTime(t, createdAt)}
      </Text>
      {showUpdated ? (
        <Text style={styles.detailMeta}>
          {t("wiki.updated_by", { name: updaterName })}
          <Text style={styles.detailMetaSeparator}> · </Text>
          {formatWikiRelativeTime(t, updatedAt)}
        </Text>
      ) : null}
    </View>
  );
}

function resolveMemberName(
  userId: string | null,
  getActorName: (type: string, id: string) => string,
  t: (key: string, options?: Record<string, unknown>) => string,
) {
  if (!userId) return t("wiki.unknown_author");
  const name = getActorName("member", userId);
  return name === "Unknown" ? t("wiki.unknown_author") : name;
}

function flattenTreeWithDepth(nodes: WikiPageTreeNode[]): WikiListItem[] {
  const out: WikiListItem[] = [];
  const visit = (node: WikiPageTreeNode, depth: number) => {
    out.push({ node, depth });
    node.children.forEach((child) => visit(child, depth + 1));
  };
  nodes.forEach((node) => visit(node, 0));
  return out;
}

function formatWikiRelativeTime(
  t: (key: string, options?: Record<string, unknown>) => string,
  value: string,
) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  const diffMs = Date.now() - date.getTime();
  const diffMin = Math.floor(diffMs / 60000);
  if (diffMin < 1) return t("wiki.just_now");
  if (diffMin < 60) return t("wiki.minutes_ago", { count: diffMin });
  const diffHours = Math.floor(diffMin / 60);
  if (diffHours < 24) return t("wiki.hours_ago", { count: diffHours });
  const diffDays = Math.floor(diffHours / 24);
  if (diffDays < 30) return t("wiki.days_ago", { count: diffDays });
  return date.toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

const styles = StyleSheet.create({
  list: {
    gap: spacing.sm,
    padding: spacing.md,
  },
  emptyList: {
    flexGrow: 1,
  },
  row: {
    alignItems: "center",
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    gap: spacing.sm,
    minHeight: 58,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  pressed: {
    opacity: 0.75,
  },
  rowIconWrap: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 34,
    justifyContent: "center",
    width: 34,
  },
  rowText: {
    flex: 1,
    minWidth: 0,
  },
  rowTitle: {
    color: colors.foreground,
    fontSize: 15,
    fontWeight: "600",
  },
  rowMeta: {
    color: colors.mutedForeground,
    fontSize: 12,
    marginTop: 2,
  },
  detailContent: {
    gap: spacing.lg,
    padding: spacing.lg,
  },
  detailHeader: {
    alignItems: "center",
    flexDirection: "row",
    gap: spacing.md,
  },
  detailIconWrap: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 42,
    justifyContent: "center",
    width: 42,
  },
  detailTitleWrap: {
    flex: 1,
    minWidth: 0,
  },
  detailTitle: {
    color: colors.foreground,
    fontSize: 20,
    fontWeight: "700",
    lineHeight: 26,
  },
  detailMeta: {
    color: colors.mutedForeground,
    fontSize: 13,
    lineHeight: 18,
  },
  detailMetaGroup: {
    gap: 2,
    marginTop: spacing.xs,
  },
  detailMetaSeparator: {
    color: colors.mutedForeground,
  },
  noContent: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    padding: spacing.lg,
  },
  noContentTitle: {
    color: colors.mutedForeground,
    fontSize: 14,
    lineHeight: 20,
  },
});
