import { useMemo } from "react";
import {
  FlatList,
  Pressable,
  RefreshControl,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { useNavigation } from "@react-navigation/native";
import type { NativeStackNavigationProp } from "@react-navigation/native-stack";
import { useTranslation } from "react-i18next";
import { autopilotDetailOptions, autopilotListOptions } from "@multica/core/autopilots";
import { useCoreQuery } from "@multica/core/provider";
import type { Autopilot } from "@multica/core/types";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { ChevronRight, Plus, Zap } from "lucide-react-native";
import { EmptyState, LoadingState, Screen } from "../../components/ui/primitives";
import { ScreenTitleBar } from "../../components/ui/screen-title-bar";
import type { RootStackParamList } from "../../navigation/root-navigator";
import { useMobileWorkspace } from "../../navigation/workspace-context";
import { colors, radii, spacing } from "../../theme/tokens";
import {
  describeTrigger,
  formatRelativeDate,
  getActorName,
  getPrimaryTrigger,
  localizedExecutionModeLabel,
  localizedStatusLabel,
} from "./autopilot-mobile-utils";

type AutopilotsNavigation = NativeStackNavigationProp<RootStackParamList>;

export function AutopilotsScreen() {
  const { t } = useTranslation();
  const navigation = useNavigation<AutopilotsNavigation>();
  const { workspace } = useMobileWorkspace();
  const {
    data: autopilots = [],
    isError,
    isLoading,
    isRefetching,
    refetch,
  } = useCoreQuery(autopilotListOptions(workspace.id));
  const { data: agents = [] } = useCoreQuery(agentListOptions(workspace.id));
  const { data: squads = [] } = useCoreQuery(squadListOptions(workspace.id));

  const sorted = useMemo(
    () =>
      [...autopilots].sort((a, b) => {
        const aTime = new Date(a.last_run_at ?? a.updated_at).getTime();
        const bTime = new Date(b.last_run_at ?? b.updated_at).getTime();
        return bTime - aTime;
      }),
    [autopilots],
  );

  if (isLoading) return <LoadingState />;

  if (isError) {
    return (
      <Screen padded={false} safeArea={false}>
        <ScreenTitleBar onBack={() => navigation.goBack()} title={t("autopilots.title")} />
        <EmptyState detail={t("common.pull_to_retry")} title={t("autopilots.unable_to_load")} />
      </Screen>
    );
  }

  return (
    <Screen padded={false} safeArea={false}>
      <ScreenTitleBar
        onBack={() => navigation.goBack()}
        right={
          <Pressable
            accessibilityLabel={t("autopilots.create")}
            accessibilityRole="button"
            onPress={() => navigation.navigate("AutopilotForm", undefined)}
            style={({ pressed }) => [styles.titleIconButton, pressed && styles.pressed]}
          >
            <Plus color={colors.foreground} size={20} />
          </Pressable>
        }
        title={t("autopilots.title")}
      />
      <FlatList
        contentContainerStyle={sorted.length === 0 ? styles.emptyList : styles.list}
        data={sorted}
        keyExtractor={(item) => item.id}
        refreshControl={
          <RefreshControl
            refreshing={isRefetching}
            tintColor={colors.foreground}
            onRefresh={() => {
              void refetch();
            }}
          />
        }
        ListEmptyComponent={
          <View style={styles.emptyWrap}>
            <Zap color={colors.mutedForeground} size={34} />
            <EmptyState
              detail={t("autopilots.empty_detail")}
              title={t("autopilots.empty_title")}
            />
            <Pressable
              accessibilityRole="button"
              onPress={() => navigation.navigate("AutopilotForm", undefined)}
              style={({ pressed }) => [styles.primaryButton, pressed && styles.pressed]}
            >
              <Plus color={colors.primaryForeground} size={16} />
              <Text style={styles.primaryButtonText}>{t("autopilots.create")}</Text>
            </Pressable>
          </View>
        }
        renderItem={({ item }) => (
          <AutopilotCard
            autopilot={item}
            assigneeName={getActorName(item.assignee_type, item.assignee_id, agents, squads)}
            onPress={() => navigation.navigate("AutopilotDetail", { autopilotId: item.id })}
            workspaceId={workspace.id}
          />
        )}
      />
    </Screen>
  );
}

function AutopilotCard({
  assigneeName,
  autopilot,
  onPress,
  workspaceId,
}: {
  assigneeName: string;
  autopilot: Autopilot;
  onPress: () => void;
  workspaceId: string;
}) {
  const { t } = useTranslation();
  const { data } = useCoreQuery({
    ...autopilotDetailOptions(workspaceId, autopilot.id),
    enabled: autopilot.status !== "archived",
  });
  const triggerSummary = describeTrigger(getPrimaryTrigger(data?.triggers ?? []));

  return (
    <Pressable
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => [styles.card, pressed && styles.pressed]}
    >
      <View style={styles.cardHeader}>
        <View style={styles.titleRow}>
          <View style={styles.iconWrap}>
            <Zap color={colors.foreground} size={18} />
          </View>
          <View style={styles.titleText}>
            <Text numberOfLines={1} style={styles.title}>
              {autopilot.title}
            </Text>
            <Text numberOfLines={1} style={styles.subtitle}>
              {assigneeName}
            </Text>
          </View>
        </View>
        <ChevronRight color={colors.mutedForeground} size={18} />
      </View>
      <View style={styles.metaGrid}>
        <Meta label={t("autopilots.status_label")} value={localizedStatusLabel(t, autopilot.status)} />
        <Meta
          label={t("autopilots.output_mode")}
          value={localizedExecutionModeLabel(t, autopilot.execution_mode)}
        />
        <Meta label={t("autopilots.trigger")} value={triggerSummary} />
        <Meta
          label={t("autopilots.last_run")}
          value={formatRelativeDate(autopilot.last_run_at)}
        />
      </View>
    </Pressable>
  );
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <View style={styles.metaItem}>
      <Text numberOfLines={1} style={styles.metaLabel}>
        {label}
      </Text>
      <Text numberOfLines={1} style={styles.metaValue}>
        {value}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  titleIconButton: {
    alignItems: "center",
    borderRadius: radii.md,
    height: 40,
    justifyContent: "center",
    width: 40,
  },
  pressed: {
    opacity: 0.72,
  },
  list: {
    gap: spacing.md,
    padding: spacing.lg,
  },
  emptyList: {
    flexGrow: 1,
    padding: spacing.lg,
  },
  emptyWrap: {
    alignItems: "center",
    flex: 1,
    gap: spacing.md,
    justifyContent: "center",
  },
  primaryButton: {
    alignItems: "center",
    backgroundColor: colors.primary,
    borderRadius: radii.md,
    flexDirection: "row",
    gap: spacing.xs,
    minHeight: 44,
    paddingHorizontal: spacing.lg,
  },
  primaryButtonText: {
    color: colors.primaryForeground,
    fontSize: 14,
    fontWeight: "600",
  },
  card: {
    backgroundColor: colors.card,
    borderColor: colors.border,
    borderRadius: radii.md,
    borderWidth: StyleSheet.hairlineWidth,
    gap: spacing.md,
    padding: spacing.md,
  },
  cardHeader: {
    alignItems: "center",
    flexDirection: "row",
    justifyContent: "space-between",
  },
  titleRow: {
    alignItems: "center",
    flex: 1,
    flexDirection: "row",
    gap: spacing.sm,
    minWidth: 0,
  },
  iconWrap: {
    alignItems: "center",
    backgroundColor: colors.muted,
    borderRadius: radii.md,
    height: 34,
    justifyContent: "center",
    width: 34,
  },
  titleText: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    color: colors.foreground,
    fontSize: 16,
    fontWeight: "600",
  },
  subtitle: {
    color: colors.mutedForeground,
    fontSize: 13,
    marginTop: 2,
  },
  metaGrid: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: spacing.sm,
  },
  metaItem: {
    backgroundColor: colors.muted,
    borderRadius: radii.sm,
    flexBasis: "47%",
    flexGrow: 1,
    gap: 2,
    minWidth: 0,
    paddingHorizontal: spacing.sm,
    paddingVertical: spacing.sm,
  },
  metaLabel: {
    color: colors.mutedForeground,
    fontSize: 11,
    fontWeight: "600",
  },
  metaValue: {
    color: colors.foreground,
    fontSize: 13,
    fontWeight: "500",
  },
});
