/**
 * Skills browse page. Flat FlatList over the workspace's skills, read-only
 * (no create/edit/delete — see docs/superpowers/specs/2026-07-08-mobile-
 * skills-browse-design.md). Sort: client-side by `updated_at` desc, mirrors
 * the Projects list's default ordering.
 */
import { useMemo } from "react";
import { ActivityIndicator, FlatList, RefreshControl, View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { useTranslation } from "react-i18next";
import { selectSkillAssignments } from "@multica/core/workspace/queries";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { SkillRow } from "@/components/skill/skill-row";
import { skillListOptions } from "@/data/queries/skills";
import { agentListOptions } from "@/data/queries/agents";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function SkillsPage() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { t } = useTranslation("skills");

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    skillListOptions(wsId),
  );
  const { data: agents } = useQuery(agentListOptions(wsId));

  const assignments = useMemo(
    () => selectSkillAssignments(agents),
    [agents],
  );

  const sorted = useMemo(() => {
    if (!data) return [];
    return [...data].sort(
      (a, b) =>
        new Date(b.updated_at).getTime() - new Date(a.updated_at).getTime(),
    );
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
            <SkillRow
              skill={item}
              usedByCount={assignments.get(item.id)?.length ?? 0}
              onPress={() => {
                if (wsSlug) router.push(`/${wsSlug}/more/skills/${item.id}`);
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
