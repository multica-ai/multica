import { useMemo, useState } from "react";
import { FlatList, Pressable, StyleSheet, Text, View } from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { BOARD_STATUSES, STATUS_CONFIG } from "@multica/core/issues/config/status";
import { useIssueList } from "@multica/core/issues/hooks";
import type { Issue, IssueStatus } from "@multica/core/types";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { Button, EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { WorkspaceHeader } from "../../components/ui/workspace-header";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";

export function IssuesScreen() {
  const navigation = useNavigation<NativeStackNavigationProp<RootStackParamList>>();
  const { workspace } = useMobileWorkspace();
  const [status, setStatus] = useState<IssueStatus>("todo");
  const { data: issues = [], isLoading, isError } = useIssueList(workspace.id);
  const filteredIssues = useMemo(
    () => issues.filter((issue) => issue.status === status),
    [issues, status],
  );

  return (
    <Screen>
      <View style={styles.headerRow}>
        <WorkspaceHeader centered />
        <Button
          onPress={() => navigation.navigate("CreateIssue")}
          style={styles.newButton}
          variant="secondary"
        >
          New
        </Button>
      </View>
      <View style={styles.statusTabs}>
        {BOARD_STATUSES.map((item) => (
          <Pressable
            key={item}
            onPress={() => setStatus(item)}
            style={[styles.statusTab, item === status && styles.statusTabActive]}
          >
            <Text style={[styles.statusText, item === status && styles.statusTextActive]}>
              {STATUS_CONFIG[item].label}
            </Text>
          </Pressable>
        ))}
      </View>
      {isLoading ? <LoadingState /> : null}
      {isError ? (
        <EmptyState detail="Pull to retry once the connection is available." title="Unable to load issues" />
      ) : null}
      {!isLoading && !isError ? (
        <FlatList
          contentContainerStyle={styles.list}
          data={filteredIssues}
          keyExtractor={(item) => item.id}
          ListEmptyComponent={<EmptyState title="No issues in this status" />}
          renderItem={({ item }) => (
            <IssueCard
              issue={item}
              onPress={() => navigation.navigate("IssueDetail", { issueId: item.id })}
            />
          )}
        />
      ) : null}
    </Screen>
  );
}

function IssueCard({ issue, onPress }: { issue: Issue; onPress: () => void }) {
  return (
    <Pressable onPress={onPress} style={styles.card}>
      <View style={styles.cardHeader}>
        <Text style={styles.identifier}>{issue.identifier}</Text>
        <Text style={styles.priority}>{issue.priority}</Text>
      </View>
      <Text style={styles.issueTitle}>{issue.title}</Text>
      <Text style={styles.meta}>
        Updated {new Date(issue.updated_at).toLocaleDateString()}
      </Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  headerRow: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "center",
    minHeight: 44,
    position: "relative",
    zIndex: 20,
  },
  newButton: {
    position: "absolute",
    right: 0,
  },
  statusTabs: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
    marginBottom: spacing.md,
  },
  statusTab: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  },
  statusTabActive: {
    backgroundColor: colors.primary,
    borderColor: colors.primary,
  },
  statusText: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  statusTextActive: {
    color: colors.primaryForeground,
  },
  list: {
    gap: spacing.sm,
    paddingBottom: spacing.xl,
  },
  card: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.sm,
    padding: spacing.md,
  },
  cardHeader: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
  },
  identifier: {
    color: colors.mutedForeground,
    fontSize: 12,
    fontWeight: "500",
  },
  priority: {
    color: colors.warning,
    fontSize: 12,
    fontWeight: "500",
    textTransform: "capitalize",
  },
  issueTitle: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "500",
    lineHeight: 22,
  },
  meta: {
    color: colors.mutedForeground,
    fontSize: 12,
  },
});
