/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/more/projects.tsx.
 * Projects browse page. Flat FlatList over the workspace's projects.
 *
 * Sort: client-side by `updated_at` desc — most recently touched at top.
 * Mirrors web's default list ordering. WS `project:*` events keep the cache
 * fresh via the listing-level realtime hook (`useProjectsRealtime`), so
 * pull-to-refresh is rarely needed but kept for the cellular-edge case where
 * a WS reconnect missed events.
 *
 * Navigation deltas vs iOS: the native Stack header becomes ScreenHeader
 * (title + "+" button), and row taps / create go through callback props
 * owned by the route registry (import-cycle rule).
 */
import { useMemo } from "react";
import {
  ActivityIndicator,
  FlatList,
  RefreshControl,
  StyleSheet,
  View,
} from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { ScreenHeader } from "@/src/screens/screen-header";
import { useNav } from "@/src/navigation/navigator";
import { ProjectRow } from "@/components/project/project-row";
import { projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useThemeColors } from "@/lib/use-theme-colors";

export function MoreProjectsScreen({
  onOpenProject,
  onCreateProject,
}: {
  onOpenProject: (projectId: string) => void;
  onCreateProject: () => void;
}) {
  const nav = useNav<{ pop: () => void }>();
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    projectListOptions(wsId),
  );

  const sorted = useMemo(() => {
    if (!data) return [];
    return [...data].sort(
      (a, b) =>
        new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
    );
  }, [data]);

  return (
    <View style={[styles.screen, { backgroundColor: c.background }]}>
      <ScreenHeader
        title="Projects"
        onBack={() => nav.pop()}
        right={
          <IconButton
            name="add"
            onPress={onCreateProject}
            accessibilityLabel="New project"
          />
        }
      />

      {isLoading ? (
        <View style={styles.loading}>
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View style={styles.errorWrap}>
          <Text style={[styles.errorText, { color: c.destructive }]}>
            Failed to load projects:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : sorted.length === 0 ? (
        <EmptyState onCreate={onCreateProject} />
      ) : (
        <FlatList
          data={sorted}
          keyExtractor={(item) => item.id}
          ItemSeparatorComponent={Separator}
          renderItem={({ item }) => (
            <ProjectRow
              project={item}
              onPress={() => onOpenProject(item.id)}
            />
          )}
          refreshControl={
            <RefreshControl refreshing={isRefetching} onRefresh={refetch} />
          }
          contentContainerStyle={styles.listContent}
        />
      )}
    </View>
  );
}

function Separator() {
  const c = useThemeColors();
  // h-px bg-border ml-4
  return <View style={[styles.separator, { backgroundColor: c.border }]} />;
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  const c = useThemeColors();
  return (
    <View style={styles.emptyWrap}>
      <Text style={[styles.emptyTitle, { color: c.foreground }]}>
        No projects yet
      </Text>
      <Button variant="default" onPress={onCreate}>
        <Text>Create project</Text>
      </Button>
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  // flex-1 items-center justify-center
  loading: { flex: 1, alignItems: "center", justifyContent: "center" },
  // px-4 gap-3 pt-4
  errorWrap: { paddingHorizontal: 16, gap: 12, paddingTop: 16 },
  // text-sm
  errorText: { fontSize: 14 },
  listContent: { paddingBottom: 24 },
  separator: { height: 1, marginLeft: 16 },
  // flex-1 items-center justify-center px-6 gap-4
  emptyWrap: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
    gap: 16,
  },
  // text-base font-medium
  emptyTitle: { fontSize: 16, fontWeight: "500" },
});
