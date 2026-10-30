/**
 * Projects browse page. Lifecycle views project the full workspace list.
 *
 * Title and `+` button live in the native iOS Stack header (declared via
 * Stack.Screen options in parent `_layout.tsx`, overridden here to add
 * `headerRight`). Rendering an in-body title row on top of the native bar
 * would stack two "Projects" labels vertically.
 *
 * Sort: client-side by `updated_at` desc — most recently touched at top.
 * WS `project:*` events keep the cache
 * fresh via the listing-level realtime hook (`useProjectsRealtime` in
 * `_layout.tsx`), so pull-to-refresh is rarely needed but kept for the
 * cellular-edge case where a WS reconnect missed events.
 */
import { useCallback, useMemo, useState } from "react";
import SegmentedControl from "@react-native-segmented-control/segmented-control";
import { matchesProjectLifecycle, PROJECT_LIFECYCLE_VIEWS, type ProjectLifecycleView } from "@multica/core/projects/lifecycle";
import {
  ActivityIndicator,
  FlatList,
  RefreshControl,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { Stack, router } from "expo-router";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { ProjectRow } from "@/components/project/project-row";
import { projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useT } from "@/lib/i18n";

export default function ProjectsPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  return <ProjectsBrowser key={wsId} wsId={wsId} wsSlug={wsSlug} />;
}

function ProjectsBrowser({ wsId, wsSlug }: { wsId: string | null; wsSlug: string | null }) {
  const { t } = useT("projects");
  const [lifecycleView, setLifecycleView] = useState<ProjectLifecycleView>("active");

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    projectListOptions(wsId),
  );

  const sorted = useMemo(() => {
    if (!data) return [];
    return data.filter((p) => matchesProjectLifecycle(p.status, lifecycleView)).sort(
      (a, b) =>
        new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
    );
  }, [data, lifecycleView]);

  const goCreate = useCallback(() => {
    if (wsSlug) router.push(`/${wsSlug}/project/new`);
  }, [wsSlug]);

  const headerRight = useCallback(() => {
    return <PlusButton onPress={goCreate} />;
  }, [goCreate]);

  return (
    <SafeAreaView className="flex-1 bg-background" edges={[]}>
      <Stack.Screen options={{ headerRight }} />
      <View className="px-4 py-2">
        <SegmentedControl
          accessibilityLabel={t("lifecycle.label")}
          values={PROJECT_LIFECYCLE_VIEWS.map((view) =>
            `${t(`lifecycle.${view}`)} (${data?.filter((p) => matchesProjectLifecycle(p.status, view)).length ?? 0})`,
          )}
          selectedIndex={PROJECT_LIFECYCLE_VIEWS.indexOf(lifecycleView)}
          onChange={(event) => {
            const view = PROJECT_LIFECYCLE_VIEWS[event.nativeEvent.selectedSegmentIndex];
            if (view) setLifecycleView(view);
          }}
        />
      </View>

      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            {t("errors.load_failed", {
              message: error instanceof Error ? error.message : "unknown",
            })}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>{t("common:actions.retry")}</Text>
          </Button>
        </View>
      ) : data?.length === 0 ? (
        <EmptyState onCreate={goCreate} />
      ) : (
        <FlatList
          data={sorted}
          ListEmptyComponent={
            <View className="items-center px-6 py-12">
              <Text>{t(`lifecycle.empty.${lifecycleView}`)}</Text>
            </View>
          }
          keyExtractor={(item) => item.id}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          renderItem={({ item }) => (
            <ProjectRow
              project={item}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/project/${item.id}`);
              }}
            />
          )}
          refreshControl={
            <RefreshControl refreshing={isRefetching} onRefresh={refetch} />
          }
          contentContainerClassName="pb-6"
        />
      )}
    </SafeAreaView>
  );
}

function PlusButton({ onPress }: { onPress: () => void }) {
  const { t } = useT("projects");
  return (
    <IconButton
      name="add"
      onPress={onPress}
      accessibilityLabel={t("navigation:routes.new_project")}
    />
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  const { t } = useT("projects");
  return (
    <View className="flex-1 items-center justify-center px-6 gap-4">
      <Text className="text-base font-medium text-foreground">
        {t("list.empty")}
      </Text>
      <Button variant="default" onPress={onCreate}>
        <Text>{t("common:actions.create")}</Text>
      </Button>
    </View>
  );
}
