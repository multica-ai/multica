/**
 * Project detail screen. Single column, scrolling:
 *
 *   Header card (icon + title + description, tap → edit)
 *   Properties section (Status / Priority / Lead — tap chip → picker)
 *   Resources section (read-only by default, "Add" button → resource form)
 *   Related issues (Open / Done bucketed list)
 *
 * Per-record realtime: `useProjectRealtime(id, onDeleted=back)` subscribes
 * to `project:updated` (full replace) and `project:deleted` (pop back).
 *
 * Right-top "…" menu → Edit / Delete. Delete asks for confirmation via
 * `Alert.alert` (destructive actions need a second tap).
 */
import { useCallback } from "react";
import {
  ActivityIndicator,
  Alert,
  Linking,
  RefreshControl,
  ScrollView,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Ionicons } from "@expo/vector-icons";
import { useTheme } from "@react-navigation/native";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { ProjectHeaderCard } from "@/components/project/project-header-card";
import { ProjectPropertiesSection } from "@/components/project/project-properties-section";
import { ProjectRelatedIssues } from "@/components/project/project-related-issues";
import { ProjectResourcesSection } from "@/components/project/project-resources-section";
import {
  projectDetailOptions,
  projectResourcesOptions,
} from "@/data/queries/projects";
import { issueKeys } from "@/data/queries/issue-keys";
import { useDeleteProject } from "@/data/mutations/projects";
import { pinListOptions } from "@/data/queries/pins";
import { useCreatePin, useDeletePin } from "@/data/mutations/pins";
import { useAuthStore } from "@/data/auth-store";
import { useProjectRealtime } from "@/data/realtime/use-project-realtime";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useT } from "@/lib/i18n";

export default function ProjectDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const qc = useQueryClient();

  const detail = useQuery(projectDetailOptions(wsId, id));
  const deleteProject = useDeleteProject(id);

  // Per-record realtime — when another client deletes the project we're
  // viewing, pop back so the user isn't stranded on a 404.
  useProjectRealtime(id, () => router.back());

  const onRefresh = useCallback(async () => {
    await Promise.all([
      detail.refetch(),
      qc.invalidateQueries({ queryKey: projectResourcesOptions(wsId, id).queryKey }),
      qc.invalidateQueries({
        queryKey: [...issueKeys.list(wsId), "byProject", id],
      }),
    ]);
  }, [detail, qc, wsId, id]);

  const project = detail.data;
  const { colors } = useTheme();

  // EMPTY_PROJECT carries an empty id — parseWithFallback returned the
  // fallback because the response shape drifted. Treat as "not found".
  const projectMissing = !project || project.id === "";

  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: pins } = useQuery(pinListOptions(wsId, userId));
  const isPinned =
    !!project &&
    !!pins?.some(
      (p) => p.item_type === "project" && p.item_id === project.id,
    );
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const { t } = useT("projects");

  const onDelete = () => {
    Alert.alert(
      t("delete.title"),
      t("delete.message"),
      [
        { text: t("common:actions.cancel"), style: "cancel" },
        {
          text: t("common:actions.delete"),
          style: "destructive",
          onPress: () => {
            deleteProject.mutate(undefined, {
              onSuccess: () => router.back(),
            });
          },
        },
      ],
    );
  };

  return (
    <SafeAreaView className="flex-1 bg-background" edges={["bottom"]}>
      <Stack.Screen
        options={{
          title: project?.title || t("navigation:routes.project"),
          headerBackTitle: t("common:actions.back"),
          headerRight: project
            ? () => (
                <DropdownMenu>
                  <DropdownMenuTrigger
                    className="h-10 w-10 items-center justify-center rounded-md active:bg-accent"
                    accessibilityRole="button"
                    accessibilityLabel={t("menu.project_actions")}
                  >
                    <Ionicons
                      name="ellipsis-horizontal"
                      size={20}
                      color={colors.text}
                    />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent className="w-56">
                    <DropdownMenuItem
                      onPress={() =>
                        isPinned
                          ? deletePin.mutate({
                              itemType: "project",
                              itemId: project.id,
                            })
                          : createPin.mutate({
                              item_type: "project",
                              item_id: project.id,
                            })
                      }
                    >
                      <Text>{isPinned ? t("menu.unpin") : t("menu.pin")}</Text>
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onPress={() =>
                        wsSlug && router.push(`/${wsSlug}/project/${id}/edit`)
                      }
                    >
                      <Text>{t("menu.edit_details")}</Text>
                    </DropdownMenuItem>
                    {process.env.EXPO_PUBLIC_WEB_URL ? (
                      <DropdownMenuItem
                        onPress={() =>
                          Linking.openURL(
                            `${process.env.EXPO_PUBLIC_WEB_URL}/${wsSlug}/projects/${id}`,
                          )
                        }
                      >
                        <Text>{t("menu.open_web")}</Text>
                      </DropdownMenuItem>
                    ) : null}
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      onPress={onDelete}
                      variant="destructive"
                    >
                      <Text>{t("common:actions.delete")}</Text>
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
              )
            : undefined,
        }}
      />
      {detail.isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : detail.error || projectMissing ? (
        <View className="flex-1 items-center justify-center px-6 gap-3">
          <Text className="text-sm text-destructive text-center">
            {t("errors.load_failed", {
              message:
                detail.error instanceof Error
                  ? detail.error.message
                  : "not found",
            })}
          </Text>
          <Button variant="outline" onPress={() => detail.refetch()}>
            <Text>{t("common:actions.retry")}</Text>
          </Button>
        </View>
      ) : (
        <ScrollView
          contentContainerClassName="pb-10"
          refreshControl={
            <RefreshControl
              refreshing={detail.isRefetching}
              onRefresh={onRefresh}
            />
          }
          keyboardDismissMode="on-drag"
        >
          <ProjectHeaderCard
            project={project}
            onEdit={() => {
              if (wsSlug) router.push(`/${wsSlug}/project/${id}/edit`);
            }}
          />
          <ProjectPropertiesSection
            project={project}
            onPressStatus={() => {
              if (wsSlug)
                router.push({
                  pathname: "/[workspace]/project/[id]/picker/status",
                  params: { workspace: wsSlug, id },
                });
            }}
            onPressPriority={() => {
              if (wsSlug)
                router.push({
                  pathname: "/[workspace]/project/[id]/picker/priority",
                  params: { workspace: wsSlug, id },
                });
            }}
            onPressLead={() => {
              if (wsSlug)
                router.push({
                  pathname: "/[workspace]/project/[id]/picker/lead",
                  params: { workspace: wsSlug, id },
                });
            }}
          />
          <ProjectResourcesSection
            projectId={id}
            onAdd={() => {
              if (wsSlug)
                router.push({
                  pathname: "/[workspace]/project/[id]/add-resource",
                  params: { workspace: wsSlug, id },
                });
            }}
          />
          <View className="h-3" />
          <ProjectRelatedIssues projectId={id} />
        </ScrollView>
      )}
    </SafeAreaView>
  );
}
