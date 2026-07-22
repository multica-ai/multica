/**
 * Runtime detail — read-only. Metadata, health, a static CLI-update
 * signal (no action, no polling), and the agents configured to use this
 * runtime. No separate detail endpoint — found by id in the already-
 * fetched runtimeListOptions list, matching desktop's own approach. No
 * create/edit/delete/connect on mobile — see
 * docs/superpowers/specs/2026-07-09-mobile-runtimes-browse-design.md.
 */
import { useMemo } from "react";
import { ActivityIndicator, ScrollView, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { Stack, useLocalSearchParams } from "expo-router";
import { useTranslation } from "react-i18next";
import { Ionicons } from "@expo/vector-icons";
import {
  deriveRuntimeHealth,
  readRuntimeCliVersion,
} from "@multica/core/runtimes";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { SectionGroup } from "@/components/ui/section-group";
import {
  latestCliVersionOptions,
  runtimeListOptions,
} from "@/data/queries/runtimes";
import { agentListOptions } from "@/data/queries/agents";
import { useActorLookup } from "@/data/use-actor-name";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { timeAgo } from "@/lib/time-ago";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";
import { HEALTH_DOT_CLASS } from "@/lib/runtime-health";
import { runtimeNeedsUpdate } from "@/lib/runtime-update-check";

export default function RuntimeDetailPage() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { t } = useTranslation("runtimes");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const { getName } = useActorLookup();
  const currentUserId = useAuthStore((s) => s.user?.id);

  const {
    data: runtimes,
    isLoading,
    error,
    refetch,
  } = useQuery(runtimeListOptions(wsId));
  const { data: agents } = useQuery(agentListOptions(wsId));
  const { data: latestVersion } = useQuery(latestCliVersionOptions());

  const runtime = runtimes?.find((r) => r.id === id);
  const notFound = !isLoading && !runtime;

  const runtimeAgents = useMemo(() => {
    if (!agents || !runtime) return [];
    return agents.filter((a) => a.runtime_id === runtime.id);
  }, [agents, runtime]);

  const cliVersion = runtime ? readRuntimeCliVersion(runtime.metadata) : "";
  const needsUpdate = runtime
    ? runtimeNeedsUpdate(runtime, latestVersion, currentUserId)
    : false;
  const health = runtime ? deriveRuntimeHealth(runtime, Date.now()) : null;

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{ title: runtime?.name || t("detail.header_default_title") }}
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
              {runtime!.name}
            </Text>
            <View className="flex-row items-center gap-1.5">
              <View
                className={`size-2 rounded-full ${HEALTH_DOT_CLASS[health!]}`}
              />
              <Text className="text-sm text-muted-foreground">
                {t(`health.${health}.label`)}
              </Text>
            </View>
          </View>

          <SectionGroup>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.owner_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {runtime!.owner_id ? getName("member", runtime!.owner_id) : "—"}
              </Text>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.mode_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {t(`detail.mode.${runtime!.runtime_mode}`)}
              </Text>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.visibility_label")}
              </Text>
              <View className="flex-row items-center gap-1.5">
                <Ionicons
                  name={
                    runtime!.visibility === "public"
                      ? "globe-outline"
                      : "lock-closed-outline"
                  }
                  size={14}
                  color={mutedFg}
                />
                <Text className="text-sm text-foreground">
                  {t(`detail.visibility.${runtime!.visibility}`)}
                </Text>
              </View>
            </View>
            {runtime!.device_info ? (
              <View className="flex-row items-center justify-between px-4 py-3">
                <Text className="text-sm text-muted-foreground">
                  {t("detail.device_label")}
                </Text>
                <Text className="text-sm text-foreground" numberOfLines={1}>
                  {runtime!.device_info}
                </Text>
              </View>
            ) : null}
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.created_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {timeAgo(runtime!.created_at)}
              </Text>
            </View>
            <View className="flex-row items-center justify-between px-4 py-3">
              <Text className="text-sm text-muted-foreground">
                {t("detail.updated_label")}
              </Text>
              <Text className="text-sm text-foreground">
                {timeAgo(runtime!.updated_at)}
              </Text>
            </View>
          </SectionGroup>

          {cliVersion ? (
            <SectionGroup>
              <View className="flex-row items-center justify-between px-4 py-3">
                <Text className="text-sm text-muted-foreground">
                  {t("detail.cli_version_label")}
                </Text>
                <Text className="text-sm text-foreground">{cliVersion}</Text>
              </View>
              {needsUpdate ? (
                <View className="px-4 py-3">
                  <Text className="text-sm text-warning">
                    {t("detail.update_available", { version: latestVersion })}
                  </Text>
                </View>
              ) : null}
            </SectionGroup>
          ) : null}

          <SectionGroup title={t("detail.agents_title")}>
            {runtimeAgents.length === 0 ? (
              <View className="px-4 py-3.5">
                <Text className="text-sm text-muted-foreground">
                  {t("detail.agents_empty")}
                </Text>
              </View>
            ) : (
              runtimeAgents.map((agent) => (
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
        </ScrollView>
      )}
    </View>
  );
}
