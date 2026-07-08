/**
 * Projects browse page. Flat FlatList over the workspace's projects.
 *
 * Title and `+` button live in the native iOS Stack header (declared via
 * Stack.Screen options in parent `_layout.tsx`, overridden here to add
 * `headerRight`). Rendering an in-body title row on top of the native bar
 * would stack two "Projects" labels vertically.
 *
 * Sort: client-side by `updated_at` desc — most recently touched at top.
 * Mirrors web's default list ordering. WS `project:*` events keep the cache
 * fresh via the listing-level realtime hook (`useProjectsRealtime` in
 * `_layout.tsx`), so pull-to-refresh is rarely needed but kept for the
 * cellular-edge case where a WS reconnect missed events.
 */
import { useCallback, useMemo } from "react";
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

export default function ProjectsPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);

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

  const goCreate = useCallback(() => {
    if (wsSlug) router.push(`/${wsSlug}/project/new`);
  }, [wsSlug]);

  const headerRight = useCallback(() => {
    return <PlusButton onPress={goCreate} />;
  }, [goCreate]);

  return (
    <SafeAreaView className="flex-1 bg-background" edges={[]}>
      <Stack.Screen options={{ headerRight }} />

      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Failed to load projects:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : sorted.length === 0 ? (
        <EmptyState onCreate={goCreate} />
      ) : (
        <FlatList
          data={sorted}
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
  return (
    <IconButton
      name="add"
      onPress={onPress}
      accessibilityLabel="New project"
    />
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <View className="flex-1 items-center justify-center px-6 gap-4">
      <Text className="text-base font-medium text-foreground">
        No projects yet
      </Text>
      <Text className="text-sm text-muted-foreground text-center">
        Group related issues into a project to track progress and assign a
        lead.
      </Text>
      <Button variant="default" onPress={onCreate}>
        <Text>Create project</Text>
      </Button>
    </View>
  );
}
