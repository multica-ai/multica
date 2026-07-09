/**
 * Runtimes browse page. Flat FlatList over the workspace's runtimes,
 * read-only (no connect/delete/profile config — see
 * docs/superpowers/specs/2026-07-09-mobile-runtimes-browse-design.md).
 * Sort: online first, then last_seen_at desc (surfaces live runtimes at
 * the top).
 */
import { useMemo } from "react";
import {
  ActivityIndicator,
  FlatList,
  RefreshControl,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { RuntimeRow } from "@/components/runtime/runtime-row";
import { runtimeListOptions } from "@/data/queries/runtimes";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function RuntimesPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { t } = useTranslation("runtimes");

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    runtimeListOptions(wsId),
  );

  const sorted = useMemo(() => {
    if (!data) return [];
    return [...data].sort((a, b) => {
      if (a.status !== b.status) return a.status === "online" ? -1 : 1;
      const aTime = a.last_seen_at ? new Date(a.last_seen_at).getTime() : 0;
      const bTime = b.last_seen_at ? new Date(b.last_seen_at).getTime() : 0;
      return bTime - aTime;
    });
  }, [data]);

  return (
    <SafeAreaView className="flex-1 bg-background" edges={[]}>
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            {t("list.error.load_prefix")}{" "}
            {error instanceof Error ? error.message : t("list.error.unknown")}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>{t("list.error.retry")}</Text>
          </Button>
        </View>
      ) : sorted.length === 0 ? (
        <View className="flex-1 items-center justify-center px-6 gap-2">
          <Text className="text-base font-medium text-foreground">
            {t("list.empty.title")}
          </Text>
          <Text className="text-sm text-muted-foreground text-center">
            {t("list.empty.message")}
          </Text>
        </View>
      ) : (
        <FlatList
          data={sorted}
          keyExtractor={(item) => item.id}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          renderItem={({ item }) => (
            <RuntimeRow
              runtime={item}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/more/runtimes/${item.id}`);
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
