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
 * Right-top "…" menu (cross-platform action sheet) → Edit / Delete. Delete
 * asks for confirmation via `Alert.alert` per iOS HIG (destructive actions
 * need a second tap).
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
import { useActionSheet } from "@expo/react-native-action-sheet";
import { SafeAreaView } from "react-native-safe-area-context";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
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

export default function ProjectDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const qc = useQueryClient();
  const { showActionSheetWithOptions } = useActionSheet();
  const { t } = useTranslation("projects");

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

  const onPressMore = () => {
    if (!project) return;
    const wsUrl = process.env.EXPO_PUBLIC_WEB_URL;
    const cancelLabel = t("detail.menu.cancel");
    const pinLabel = t("detail.menu.pin");
    const unpinLabel = t("detail.menu.unpin");
    const editDetailsLabel = t("detail.menu.edit_details");
    const openOnWebLabel = t("detail.menu.open_on_web");
    const deleteProjectLabel = t("detail.menu.delete_project");
    const options = [
      cancelLabel,
      isPinned ? unpinLabel : pinLabel,
      editDetailsLabel,
      ...(wsUrl ? [openOnWebLabel] : []),
      deleteProjectLabel,
    ];
    const destructiveIndex = options.length - 1;
    showActionSheetWithOptions(
      {
        options,
        cancelButtonIndex: 0,
        destructiveButtonIndex: destructiveIndex,
      },
      (i) => {
        if (i === undefined) return;
        const label = options[i];
        if (label === pinLabel) {
          createPin.mutate({ item_type: "project", item_id: project.id });
          return;
        }
        if (label === unpinLabel) {
          deletePin.mutate({ itemType: "project", itemId: project.id });
          return;
        }
        if (label === editDetailsLabel) {
          if (wsSlug) router.push(`/${wsSlug}/project/${id}/edit`);
          return;
        }
        if (label === openOnWebLabel && wsUrl) {
          Linking.openURL(`${wsUrl}/${wsSlug}/projects/${id}`);
          return;
        }
        if (i === destructiveIndex) {
          onDelete();
        }
      },
    );
  };

  const onDelete = () => {
    Alert.alert(
      t("detail.delete_confirm.title"),
      t("detail.delete_confirm.message"),
      [
        { text: t("detail.delete_confirm.cancel"), style: "cancel" },
        {
          text: t("detail.delete_confirm.confirm"),
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
          title: project?.title || t("detail.header_default_title"),
          headerBackTitle: t("detail.header_back_title"),
          headerRight: project
            ? () => (
                <IconButton
                  name="ellipsis-horizontal"
                  onPress={onPressMore}
                  accessibilityLabel={t("detail.actions_accessibility_label")}
                />
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
            {t("detail.error.load_prefix")}{" "}
            {detail.error instanceof Error
              ? detail.error.message
              : t("detail.error.not_found")}
          </Text>
          <Button variant="outline" onPress={() => detail.refetch()}>
            <Text>{t("detail.error.retry")}</Text>
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
