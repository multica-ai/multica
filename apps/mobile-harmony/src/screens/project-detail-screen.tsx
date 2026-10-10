/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/project/[id].tsx.
 * Project detail screen. Single column, scrolling:
 *
 *   Header card (icon + title + description, tap → edit sheet)
 *   Properties section (Status / Priority / Lead — tap row → picker sheet)
 *   Resources section (read-only by default, "Add" → add-resource sheet)
 *   Related issues (status-bucketed list)
 *
 * Per-record realtime: `useProjectRealtime(projectId, onDeleted)` subscribes
 * to `project:updated` (full replace) and `project:deleted` (pop back).
 *
 * Right-top "…" menu: the iOS ActionSheetIOS becomes a bottom-sheet menu
 * (rows styled like components/nav/more-menu.tsx) with the same actions —
 * Pin/Unpin, Edit details, Open on web, Delete. Delete asks for
 * confirmation via `Alert.alert` per iOS HIG (destructive actions need
 * a second tap).
 *
 * Navigation deltas vs iOS: back/pop goes through the local stack navigator
 * (`useNav`), and opening a related issue is an `onOpenIssue` callback prop
 * supplied by the route registry — this file never imports the app shell's
 * route type (import-cycle rule).
 */
import { useCallback, useState } from "react";
import { ActivityIndicator, Alert, Linking, RefreshControl, ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { SafeAreaView } from "@/lib/safe-area";
import { Icon } from "@/components/ui/icon";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { ScreenHeader } from "@/src/screens/screen-header";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { useNav } from "@/src/navigation/navigator";
import { ProjectHeaderCard } from "@/components/project/project-header-card";
import { ProjectPropertiesSection } from "@/components/project/project-properties-section";
import { ProjectRelatedIssues } from "@/components/project/project-related-issues";
import { ProjectResourcesSection } from "@/components/project/project-resources-section";
import {
  ProjectStatusPickerSheet,
  ProjectPriorityPickerSheet,
  ProjectLeadPickerSheet,
} from "@/components/project/pickers/project-picker-sheets";
import { ProjectEditSheet } from "@/components/project/project-edit-sheet";
import { ProjectAddResourceSheet } from "@/components/project/project-add-resource-sheet";
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
import { useThemeColors } from "@/lib/use-theme-colors";

/** Local stack-nav view — pop is all this screen needs from the navigator. */
type DetailNav = { pop: () => void };

export function ProjectDetailScreen({
  projectId,
  onOpenIssue,
}: {
  projectId: string;
  onOpenIssue: (issueId: string) => void;
}) {
  const nav = useNav<DetailNav>();
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const qc = useQueryClient();

  const detail = useQuery(projectDetailOptions(wsId, projectId));
  const deleteProject = useDeleteProject(projectId);

  // Per-record realtime — when another client deletes the project we're
  // viewing, pop back so the user isn't stranded on a 404.
  useProjectRealtime(projectId, () => nav.pop());

  // Sheet visibility is local state; the sheets mount from here like the
  // iOS formSheet routes mounted from the stack.
  const [menuOpen, setMenuOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [addResourceOpen, setAddResourceOpen] = useState(false);
  const [statusPickerOpen, setStatusPickerOpen] = useState(false);
  const [priorityPickerOpen, setPriorityPickerOpen] = useState(false);
  const [leadPickerOpen, setLeadPickerOpen] = useState(false);

  const onRefresh = useCallback(async () => {
    await Promise.all([
      detail.refetch(),
      qc.invalidateQueries({
        queryKey: projectResourcesOptions(wsId, projectId).queryKey,
      }),
      qc.invalidateQueries({
        queryKey: [...issueKeys.list(wsId), "byProject", projectId],
      }),
    ]);
  }, [detail, qc, wsId, projectId]);

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

  // EXPO_PUBLIC_WEB_URL on iOS; the harmony bundle inlines MULTICA_WEB_URL
  // instead (babel transform — see AGENTS.md env variants).
  const wsUrl = process.env.MULTICA_WEB_URL;

  const onDelete = () => {
    Alert.alert(
      "Delete project?",
      "This cannot be undone. Issues in this project will become unassigned from any project.",
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () => {
            deleteProject.mutate(undefined, {
              onSuccess: () => nav.pop(),
            });
          },
        },
      ],
    );
  };

  const title = project?.title || "Project";

  return (
    <SafeAreaView
      edges={["bottom"]}
      style={[styles.screen, { backgroundColor: c.background }]}
    >
      <ScreenHeader
        title={title}
        onBack={() => nav.pop()}
        right={
          project ? (
            <IconButton
              name="ellipsis-horizontal"
              onPress={() => setMenuOpen(true)}
              accessibilityLabel="Project actions"
            />
          ) : undefined
        }
      />
      {detail.isLoading ? (
        <View style={styles.loading}>
          <ActivityIndicator />
        </View>
      ) : detail.error || projectMissing ? (
        <View style={styles.errorWrap}>
          <Text style={[styles.errorText, { color: c.destructive }]}>
            Failed to load project:{" "}
            {detail.error instanceof Error
              ? detail.error.message
              : "not found"}
          </Text>
          <Button variant="outline" onPress={() => detail.refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : (
        <ScrollView
          contentContainerStyle={styles.content}
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
            onEdit={() => setEditOpen(true)}
          />
          <ProjectPropertiesSection
            project={project}
            onPressStatus={() => setStatusPickerOpen(true)}
            onPressPriority={() => setPriorityPickerOpen(true)}
            onPressLead={() => setLeadPickerOpen(true)}
          />
          <ProjectResourcesSection
            projectId={projectId}
            onAdd={() => setAddResourceOpen(true)}
          />
          <View style={styles.sectionGap} />
          <ProjectRelatedIssues
            projectId={projectId}
            onOpenIssue={onOpenIssue}
          />
        </ScrollView>
      )}

      {/* "…" actions — the ActionSheetIOS stand-in. */}
      <BottomSheet visible={menuOpen} onClose={() => setMenuOpen(false)}>
        {project ? (
          <View style={styles.menu}>
            <MenuRow
              label={isPinned ? "Unpin" : "Pin"}
              icon={isPinned ? "pin-outline" : "pin"}
              onPress={() => {
                setMenuOpen(false);
                if (isPinned) {
                  deletePin.mutate({ itemType: "project", itemId: project.id });
                } else {
                  createPin.mutate({ item_type: "project", item_id: project.id });
                }
              }}
            />
            <MenuRow
              label="Edit details"
              icon="create-outline"
              onPress={() => {
                setMenuOpen(false);
                setEditOpen(true);
              }}
            />
            {wsUrl ? (
              <MenuRow
                label="Open on web"
                icon="open-outline"
                onPress={() => {
                  setMenuOpen(false);
                  if (wsUrl && wsSlug) {
                    void Linking.openURL(
                      `${wsUrl}/${wsSlug}/projects/${projectId}`,
                    );
                  }
                }}
              />
            ) : null}
            <MenuRow
              label="Delete"
              icon="trash-outline"
              destructive
              onPress={() => {
                setMenuOpen(false);
                onDelete();
              }}
            />
            <View style={[styles.menuSeparator, { backgroundColor: c.border }]} />
            <MenuRow
              label="Cancel"
              onPress={() => setMenuOpen(false)}
            />
          </View>
        ) : null}
      </BottomSheet>

      <ProjectEditSheet
        projectId={projectId}
        visible={editOpen}
        onClose={() => setEditOpen(false)}
      />
      <ProjectAddResourceSheet
        projectId={projectId}
        visible={addResourceOpen}
        onClose={() => setAddResourceOpen(false)}
      />
      <ProjectStatusPickerSheet
        projectId={projectId}
        visible={statusPickerOpen}
        onClose={() => setStatusPickerOpen(false)}
      />
      <ProjectPriorityPickerSheet
        projectId={projectId}
        visible={priorityPickerOpen}
        onClose={() => setPriorityPickerOpen(false)}
      />
      <ProjectLeadPickerSheet
        projectId={projectId}
        visible={leadPickerOpen}
        onClose={() => setLeadPickerOpen(false)}
      />
    </SafeAreaView>
  );
}

/**
 * Action-sheet row. Same content order as the iOS ActionSheetIOS options;
 * destructive rows render in the destructive token color.
 */
function MenuRow({
  label,
  icon,
  destructive,
  onPress,
}: {
  label: string;
  icon?: string;
  destructive?: boolean;
  onPress: () => void;
}) {
  const c = useThemeColors();
  const color = destructive ? c.destructive : c.foreground;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      style={({ pressed }) => [
        styles.menuRow,
        pressed ? { backgroundColor: c.secondary } : null,
      ]}
      onPress={onPress}
    >
      {icon ? <Icon name={icon} size={18} color={color} /> : null}
      <Text style={[styles.menuLabel, { color }]}>{label}</Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  // flex-1 items-center justify-center
  loading: { flex: 1, alignItems: "center", justifyContent: "center" },
  // flex-1 items-center justify-center px-6 gap-3
  errorWrap: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
    gap: 12,
  },
  // text-sm text-destructive text-center
  errorText: { fontSize: 14, textAlign: "center" },
  // pb-10
  content: { paddingBottom: 40 },
  // h-3
  sectionGap: { height: 12 },
  menu: { paddingHorizontal: 8, paddingBottom: 8 },
  menuRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    height: 44,
    borderRadius: 8,
    paddingHorizontal: 12,
  },
  menuLabel: { fontSize: 16 },
  menuSeparator: { height: 1, marginVertical: 6 },
});
