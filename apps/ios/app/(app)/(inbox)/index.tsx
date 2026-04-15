import { useMemo, useCallback } from "react";
import { View, FlatList, RefreshControl, Text, StyleSheet } from "react-native";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  inboxListOptions,
  inboxKeys,
  deduplicateInboxItems,
} from "@multica/core/inbox/queries";
import { InboxRow } from "@/components/InboxRow";
import { colors } from "@/lib/theme";

export default function InboxScreen() {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();

  const { data: rawItems = [], isLoading } = useQuery(inboxListOptions(wsId));

  const items = useMemo(() => deduplicateInboxItems(rawItems), [rawItems]);

  const handleRefresh = useCallback(() => {
    qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });
  }, [qc, wsId]);

  return (
    <View style={styles.container}>
      <FlatList
        data={items}
        keyExtractor={(item) => item.id}
        renderItem={({ item }) => <InboxRow item={item} />}
        ItemSeparatorComponent={Separator}
        refreshControl={
          <RefreshControl refreshing={isLoading} onRefresh={handleRefresh} />
        }
        contentContainerStyle={items.length === 0 ? styles.empty : undefined}
        ListEmptyComponent={
          <View style={styles.emptyContainer}>
            <Text style={styles.emptyTitle}>All caught up</Text>
            <Text style={styles.emptyText}>
              {isLoading ? "Loading..." : "No new notifications"}
            </Text>
          </View>
        }
      />
    </View>
  );
}

function Separator() {
  return <View style={styles.separator} />;
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  separator: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: colors.border,
    marginLeft: 34,
  },
  empty: {
    flex: 1,
  },
  emptyContainer: {
    flex: 1,
    justifyContent: "center",
    alignItems: "center",
    paddingTop: 80,
  },
  emptyTitle: {
    fontSize: 18,
    fontWeight: "600",
    color: colors.foreground,
    marginBottom: 8,
  },
  emptyText: {
    fontSize: 14,
    color: colors.mutedForeground,
  },
});
