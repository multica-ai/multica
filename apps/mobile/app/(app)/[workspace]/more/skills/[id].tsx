/**
 * Skill detail — read-only. Metadata, the agents that use this skill, and
 * its file list. Tapping a file pushes the read-only file viewer
 * (more/skills/[id]/file/[...path]). No create/edit/delete on mobile —
 * see docs/superpowers/specs/2026-07-08-mobile-skills-browse-design.md.
 */
import { useMemo } from "react";
import { ActivityIndicator, ScrollView, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useTranslation } from "react-i18next";
import { selectSkillAssignments } from "@multica/core/workspace/queries";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { NavRow, SectionGroup } from "@/components/ui/section-group";
import { SkillOriginBadge } from "@/components/skill/skill-origin-badge";
import { skillDetailOptions } from "@/data/queries/skills";
import { agentListOptions } from "@/data/queries/agents";
import { useActorLookup } from "@/data/use-actor-name";
import { useWorkspaceStore } from "@/data/workspace-store";
import { timeAgo } from "@/lib/time-ago";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export default function SkillDetailPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { t } = useTranslation("skills");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const { getName } = useActorLookup();

  const { data: skill, isLoading, error, refetch } = useQuery(
    skillDetailOptions(wsId, id),
  );
  const { data: agents } = useQuery(agentListOptions(wsId));

  const usedBy = useMemo(() => {
    if (!skill || !agents) return [];
    return selectSkillAssignments(agents).get(skill.id) ?? [];
  }, [skill, agents]);

  const goFile = (path: string) => {
    if (!wsSlug || !skill) return;
    const encodedPath = path.split("/").map(encodeURIComponent).join("/");
    router.push(`/${wsSlug}/more/skills/${skill.id}/file/${encodedPath}`);
  };

  const notFound = !isLoading && (!skill || skill.id === "");

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{
          title: skill?.name || t("detail.header_default_title"),
        }}
      />
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error || notFound ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            {t("detail.error.load_prefix")}{" "}
            {error instanceof Error ? error.message : t("detail.error.unknown")}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>{t("detail.error.retry")}</Text>
          </Button>
        </View>
      ) : (
        <ScrollView contentContainerClassName="px-4 py-4 gap-6">
          <View className="gap-2">
            <Text className="text-xl font-semibold text-foreground">
              {skill!.name}
            </Text>
            {skill!.description ? (
              <Text className="text-sm text-muted-foreground">
                {skill!.description}
              </Text>
            ) : null}
            <SkillOriginBadge skill={skill!} />
          </View>

          <SectionGroup>
            <View className="flex-row items-center gap-3 px-4 py-3.5">
              <ActorAvatar type="member" id={skill!.created_by} size={28} />
              <View className="flex-1">
                <Text className="text-sm font-medium text-foreground">
                  {getName("member", skill!.created_by)}
                </Text>
                <Text className="text-xs text-muted-foreground">
                  {t("detail.creator_label")}
                </Text>
              </View>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.created_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {timeAgo(skill!.created_at)}
              </Text>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.updated_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {timeAgo(skill!.updated_at)}
              </Text>
            </View>
          </SectionGroup>

          <SectionGroup title={t("detail.used_by_title")}>
            {usedBy.length === 0 ? (
              <View className="px-4 py-3.5">
                <Text className="text-sm text-muted-foreground">
                  {t("detail.used_by_empty")}
                </Text>
              </View>
            ) : (
              usedBy.map((agent) => (
                <View
                  key={agent.id}
                  className="flex-row items-center gap-3 px-4 py-3"
                >
                  <ActorAvatar type="agent" id={agent.id} size={24} />
                  <Text className="text-sm text-foreground">{agent.name}</Text>
                </View>
              ))
            )}
          </SectionGroup>

          <SectionGroup title={t("detail.files_title")}>
            <NavRow
              onPress={() => goFile("SKILL.md")}
              chevronColor={mutedFg}
              title={t("detail.file_root_label")}
            />
            {skill!.files.map((file) => (
              <NavRow
                key={file.id}
                onPress={() => goFile(file.path)}
                chevronColor={mutedFg}
                title={file.path}
              />
            ))}
          </SectionGroup>
        </ScrollView>
      )}
    </View>
  );
}
