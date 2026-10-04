/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/more/pins.tsx.
 * Pinned items list — mirrors the role of web's sidebar "Pinned" section,
 * one screen up the navigation tree because phones have no sidebar.
 *
 * Architecture invariant (matches web): `PinnedItem` only carries metadata
 * (`item_type` + `item_id`). Title / status / icon are fetched per-row via
 * `issueDetailOptions` / `projectDetailOptions`, so when an issue's status
 * or a project's title changes via `issue:updated` / `project:updated`,
 * this list updates automatically — no cross-entity invalidate on pinKeys
 * is needed. Do NOT inline the display fields into the pin row; that
 * couples this view to a stale snapshot. See packages/core/types/pin.ts
 * top comment.
 *
 * Rendering split by `item_type`:
 *   - issue → existing `<IssueRow>` (used by my-issues / more-issues /
 *     project-related-issues), `showStatus` because pins are heterogeneous
 *     (no section grouping by status).
 *   - project → existing `<ProjectRow>` (used by more-projects).
 *
 * Missing / no-permission rows: the detail query may 404 (issue/project
 * deleted, user lost access, server returned a parseWithFallback fallback
 * with an empty id). We render a low-emphasis placeholder so the user can
 * unpin it from here — otherwise a dead pin stays forever.
 */
import { useMemo } from "react";
import { ActivityIndicator, RefreshControl, ScrollView, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery } from "@tanstack/react-query";
import type { Issue, PinnedItem, Project } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Icon } from "@/components/ui/icon";
import { IssueRow } from "@/components/issue/issue-row";
import { ProjectRow } from "@/components/project/project-row";
import { ScreenHeader } from "@/src/screens/screen-header";
import { useNav } from "@/src/navigation/navigator";
import { pinListOptions } from "@/data/queries/pins";
import { useDeletePin } from "@/data/mutations/pins";
import { issueDetailOptions } from "@/data/queries/issues";
import { projectDetailOptions } from "@/data/queries/projects";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useThemeColors } from "@/lib/use-theme-colors";

export function MorePinsScreen({
  onOpenIssue,
  onOpenProject,
}: {
  onOpenIssue: (issueId: string) => void;
  onOpenProject: (projectId: string) => void;
}) {
  const nav = useNav<{ pop: () => void }>();
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const userId = useAuthStore((s) => s.user?.id ?? null);

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    pinListOptions(wsId, userId),
  );

  // Sort by `position` ascending so the order matches web's sidebar
  // (the reorder endpoint writes 1-based positions there too).
  const pins = useMemo(
    () => [...(data ?? [])].sort((a, b) => a.position - b.position),
    [data],
  );

  if (isLoading) {
    return (
      <View style={[styles.screen, { backgroundColor: c.background }]}>
        <ScreenHeader title="Pinned" onBack={() => nav.pop()} />
        <View style={styles.loading}>
          <ActivityIndicator />
        </View>
      </View>
    );
  }

  if (error) {
    return (
      <View style={[styles.screen, { backgroundColor: c.background }]}>
        <ScreenHeader title="Pinned" onBack={() => nav.pop()} />
        <View style={styles.errorWrap}>
          <Text style={[styles.errorText, { color: c.destructive }]}>
            Failed to load pins:{" "}
            {error instanceof Error ? error.message : "unknown error"}
          </Text>
          <Button variant="outline" onPress={() => refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      </View>
    );
  }

  if (pins.length === 0) {
    return (
      <View style={[styles.screen, { backgroundColor: c.background }]}>
        <ScreenHeader title="Pinned" onBack={() => nav.pop()} />
        <View style={styles.emptyWrap}>
          <Text style={[styles.emptyText, { color: c.mutedForeground }]}>
            No pins yet. Pin an issue or project from its actions menu to
            surface it here.
          </Text>
        </View>
      </View>
    );
  }

  return (
    <View style={[styles.screen, { backgroundColor: c.background }]}>
      <ScreenHeader title="Pinned" onBack={() => nav.pop()} />
      <ScrollView
        style={styles.list}
        contentContainerStyle={styles.listContent}
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
            {idx > 0 ? (
              <View style={[styles.separator, { backgroundColor: c.border }]} />
            ) : null}
            <PinRow
              pin={pin}
              onOpenIssue={onOpenIssue}
              onOpenProject={onOpenProject}
            />
          </View>
        ))}
      </ScrollView>
    </View>
  );
}

function PinRow({
  pin,
  onOpenIssue,
  onOpenProject,
}: {
  pin: PinnedItem;
  onOpenIssue: (issueId: string) => void;
  onOpenProject: (projectId: string) => void;
}) {
  if (pin.item_type === "issue") {
    return (
      <IssuePinRow pin={pin} onOpenIssue={onOpenIssue} />
    );
  }
  return <ProjectPinRow pin={pin} onOpenProject={onOpenProject} />;
}

function IssuePinRow({
  pin,
  onOpenIssue,
}: {
  pin: PinnedItem;
  onOpenIssue: (issueId: string) => void;
}) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data, isLoading } = useQuery(issueDetailOptions(wsId, pin.item_id));
  // EMPTY_ISSUE_FALLBACK has an empty id — treat as deleted/no-access.
  const issue = data && data.id ? (data as Issue) : null;

  if (isLoading) return <SkeletonRow />;
  if (!issue)
    return <MissingPinRow itemType="issue" itemId={pin.item_id} />;

  return (
    <IssueRow
      issue={issue}
      showStatus
      onPress={() => onOpenIssue(issue.id)}
    />
  );
}

function ProjectPinRow({
  pin,
  onOpenProject,
}: {
  pin: PinnedItem;
  onOpenProject: (projectId: string) => void;
}) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
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
      onPress={() => onOpenProject(project.id)}
    />
  );
}

function SkeletonRow() {
  const c = useThemeColors();
  return (
    <View style={styles.skeletonRow}>
      <View style={[styles.skeletonIcon, { backgroundColor: c.muted }]} />
      <View style={[styles.skeletonTitle, { backgroundColor: c.muted }]} />
    </View>
  );
}

/**
 * Renders for pins whose target issue/project was deleted or revoked.
 * Tapping triggers unpin so the user can clean it up; no destination
 * navigation since there's nothing to navigate to. Subtle styling so
 * it doesn't dominate the list of live pins.
 */
function MissingPinRow({
  itemType,
  itemId,
}: {
  itemType: "issue" | "project";
  itemId: string;
}) {
  const c = useThemeColors();
  const deletePin = useDeletePin();
  return (
    <Pressable
      onPress={() => deletePin.mutate({ itemType, itemId })}
      style={({ pressed }) => [
        styles.missingRow,
        pressed ? { backgroundColor: c.secondary } : null,
      ]}
      accessibilityLabel={`Unavailable ${itemType}, tap to unpin`}
    >
      <Icon
        name="alert-circle-outline"
        size={18}
        color={c.mutedForeground}
      />
      <Text style={[styles.missingLabel, { color: c.mutedForeground }]} numberOfLines={1}>
        Unavailable {itemType} — tap to unpin
      </Text>
    </Pressable>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1 },
  loading: { flex: 1, alignItems: "center", justifyContent: "center" },
  // px-4 gap-3 pt-4
  errorWrap: { paddingHorizontal: 16, gap: 12, paddingTop: 16 },
  errorText: { fontSize: 14 },
  // flex-1 items-center justify-center px-6
  emptyWrap: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 24,
  },
  // text-sm text-center
  emptyText: { fontSize: 14, textAlign: "center" },
  list: { flex: 1 },
  // pb-6
  listContent: { paddingBottom: 24 },
  // h-px ml-4
  separator: { height: 1, marginLeft: 16 },
  // px-4 py-3 flex-row items-center gap-3
  skeletonRow: {
    paddingHorizontal: 16,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  // size-5 rounded-xs
  skeletonIcon: { width: 20, height: 20, borderRadius: 2 },
  // flex-1 h-4 rounded-xs
  skeletonTitle: { flex: 1, height: 16, borderRadius: 2 },
  // px-4 py-3 flex-row items-center gap-3 opacity-60
  missingRow: {
    paddingHorizontal: 16,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    opacity: 0.6,
  },
  // flex-1 text-sm
  missingLabel: { flex: 1, fontSize: 14 },
});
