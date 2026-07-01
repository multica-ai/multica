/**
 * Pinned items tab. Moved from more/pins.tsx; header is now the in-body
 * <Header> component since tab roots have headerShown: false.
 *
 * Architecture: PinnedItem only carries metadata (item_type + item_id).
 * Title/status/icon are fetched per-row via issueDetailOptions /
 * projectDetailOptions so live changes propagate automatically.
 */
import { useMemo } from "react";
import {
  ActivityIndicator,
  Pressable,
  RefreshControl,
  ScrollView,
  View,
} from "react-native";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import type { Issue, PinnedItem, Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Header } from "@/components/ui/header";
import { HeaderActions } from "@/components/ui/app-header-actions";
import { IssueRow } from "@/components/issue/issue-row";
import { ProjectRow } from "@/components/project/project-row";
import { pinListOptions } from "@/data/queries/pins";
import { useDeletePin } from "@/data/mutations/pins";
import { issueDetailOptions } from "@/data/queries/issues";
import { projectDetailOptions } from "@/data/queries/projects";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

export default function PinsTab() {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    pinListOptions(wsId, userId),
  );

  const pins = useMemo(
    () => [...(data ?? [])].sort((a, b) => a.position - b.position),
    [data],
  );

  return (
    <View className="flex-1 bg-background">
      <Header title="Pinned" right={<HeaderActions />} />
      {isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : error ? (
        <View className="flex-1 px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            Failed to load pins:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : pins.length === 0 ? (
        <View className="flex-1 items-center justify-center px-6">
          <Text className="text-sm text-muted-foreground text-center">
            No pins yet. Pin an issue or project from its actions menu to
            surface it here.
          </Text>
        </View>
      ) : (
        <ScrollView
          className="flex-1"
          contentContainerClassName="pb-6"
          refreshControl={
            <RefreshControl
              refreshing={isRefetching}
              onRefresh={() => refetch()}
            />
          }
          showsVerticalScrollIndicator={false}
        >
          {pins.map((pin, idx) => (
            <View key={pin.id}>
              {idx > 0 ? <View className="h-px bg-border ml-4" /> : null}
              <PinRow pin={pin} wsId={wsId} wsSlug={wsSlug} />
            </View>
          ))}
        </ScrollView>
      )}
    </View>
  );
}

function PinRow({
  pin,
  wsId,
  wsSlug,
}: {
  pin: PinnedItem;
  wsId: string | null;
  wsSlug: string | null;
}) {
  if (pin.item_type === "issue") {
    return <IssuePinRow pin={pin} wsId={wsId} wsSlug={wsSlug} />;
  }
  return <ProjectPinRow pin={pin} wsId={wsId} wsSlug={wsSlug} />;
}

function IssuePinRow({
  pin,
  wsId,
  wsSlug,
}: {
  pin: PinnedItem;
  wsId: string | null;
  wsSlug: string | null;
}) {
  const { data, isLoading } = useQuery(issueDetailOptions(wsId, pin.item_id));
  const issue = data && data.id ? (data as Issue) : null;

  if (isLoading) return <SkeletonRow />;
  if (!issue) return <MissingPinRow itemType="issue" itemId={pin.item_id} />;

  return (
    <IssueRow
      issue={issue}
      showStatus
      onPress={() => {
        if (wsSlug) router.push(`/${wsSlug}/issue/${issue.id}`);
      }}
    />
  );
}

function ProjectPinRow({
  pin,
  wsId,
  wsSlug,
}: {
  pin: PinnedItem;
  wsId: string | null;
  wsSlug: string | null;
}) {
  const { data, isLoading } = useQuery(
    projectDetailOptions(wsId, pin.item_id),
  );
  const project = data && data.id ? (data as Project) : null;

  if (isLoading) return <SkeletonRow />;
  if (!project)
    return <MissingPinRow itemType="project" itemId={pin.item_id} />;

  return (
    <ProjectRow
      project={project}
      onPress={() => {
        if (wsSlug) router.push(`/${wsSlug}/project/${project.id}`);
      }}
    />
  );
}

function SkeletonRow() {
  return (
    <View className="px-4 py-3 flex-row items-center gap-3">
      <View className="size-5 rounded bg-muted" />
      <View className="flex-1 h-4 rounded bg-muted" />
    </View>
  );
}

function MissingPinRow({
  itemType,
  itemId,
}: {
  itemType: "issue" | "project";
  itemId: string;
}) {
  const { colorScheme } = useColorScheme();
  const deletePin = useDeletePin();
  return (
    <Pressable
      onPress={() => deletePin.mutate({ itemType, itemId })}
      className="px-4 py-3 flex-row items-center gap-3 active:bg-secondary opacity-60"
      accessibilityLabel={`Unavailable ${itemType}, tap to unpin`}
    >
      <Ionicons
        name="alert-circle-outline"
        size={18}
        color={THEME[colorScheme].mutedForeground}
      />
      <Text className="flex-1 text-sm text-muted-foreground" numberOfLines={1}>
        Unavailable {itemType} — tap to unpin
      </Text>
    </Pressable>
  );
}
