/**
 * HarmonyOS port of apps/mobile/app/(app)/[workspace]/issue/[id].tsx —
 * issue detail screen.
 *
 * Read-mostly timeline with an inline comment composer pinned to the
 * bottom (InlineCommentComposer). Header: the hand-rolled navigator has
 * no native bar, so ScreenHeader renders `issue.identifier` as the title
 * (Linear-style) with the ambient agent-working badge + "…" actions on
 * the right.
 *
 * "…" menu: the iOS ActionSheetIOS becomes a BottomSheet menu (rows styled
 * like components/nav/more-menu.tsx, same actions — Pin/Unpin, Edit
 * details, Copy link, Open on web, Delete). Delete confirms via
 * Alert.alert per iOS HIG (destructive actions need a second tap).
 *
 * The iOS formSheet picker routes (issue/[id]/picker/*) become BottomSheet
 * sheets mounted from here via local state; the edit modal becomes
 * IssueEditSheet. The runs formSheet route becomes the pushed
 * IssueRunsScreen via the `onOpenRuns` callback prop (the route registry
 * supplies it — this file never imports the app shell's route type,
 * import-cycle rule).
 *
 * Props `highlightCommentId` / `highlightNonce` mirror the iOS inbox
 * deep-link params; the inbox flow can pass them when wired.
 */
import React, { useCallback, useEffect, useState } from "react";
import { ActivityIndicator, Alert, Linking, StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { Icon } from "@/components/ui/icon";
import { TimelineList } from "@/components/issue/timeline-list";
import { AgentHeaderBadge } from "@/components/issue/agent-header-badge";
import { InlineCommentComposer } from "@/components/issue/inline-comment-composer";
import { IssueEditSheet } from "@/components/issue/issue-edit-sheet";
import type { IssuePickerField } from "@/components/issue/attribute-row";
import {
  IssueStatusPickerSheet,
} from "@/components/issue/pickers/status";
import { IssuePriorityPickerSheet } from "@/components/issue/pickers/priority";
import { IssueAssigneePickerSheet } from "@/components/issue/pickers/assignee";
import { IssueLabelPickerSheet } from "@/components/issue/pickers/label";
import { IssueProjectPickerSheet } from "@/components/issue/pickers/project";
import { IssueDueDatePickerSheet } from "@/components/issue/pickers/due-date";
import { setStringAsync as clipboardSetStringAsync } from "@/lib/clipboard";
import {
  issueDetailOptions,
  issueKeys,
  issueTimelineOptions,
} from "@/data/queries/issues";
import { useDeleteIssue } from "@/data/mutations/issues";
import { pinListOptions } from "@/data/queries/pins";
import { useCreatePin, useDeletePin } from "@/data/mutations/pins";
import { useAuthStore } from "@/data/auth-store";
import { useIssueRealtime } from "@/data/realtime/use-issue-realtime";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useViewedIssuesStore } from "@/data/viewed-issues-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { SafeAreaView } from "@/lib/safe-area";
import { ScreenHeader } from "./screen-header";
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { useNav } from "@/src/navigation/navigator";
import { useThemeColors } from "@/lib/use-theme-colors";

/** Local stack-nav view — pop is all this screen needs from the navigator. */
type DetailNav = { pop: () => void };

interface Props {
  issueId: string;
  /** Opens the issue's runs surface (the route registry pushes the
   *  IssueRunsScreen). */
  onOpenRuns: (issueId: string) => void;
  /** Inbox deep-link highlight, forwarded to the timeline. */
  highlightCommentId?: string;
  /** Per-tap nonce for the highlight effect. */
  highlightNonce?: string;
}

export function IssueDetailScreen({
  issueId,
  onOpenRuns,
  highlightCommentId,
  highlightNonce,
}: Props) {
  const c = useThemeColors();
  const nav = useNav<DetailNav>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const qc = useQueryClient();

  const detail = useQuery(issueDetailOptions(wsId, issueId));
  const timeline = useQuery(issueTimelineOptions(wsId, issueId));

  // Subscribe to per-issue WS events: status/priority/assignee/label
  // changes, comments, activity, reactions, agent task progress.
  // Mounted with `issueId` — cleans up automatically on navigate-away.
  // If another client deletes the issue we're viewing, pop back so the
  // user isn't stranded on a 404 detail page.
  useIssueRealtime(issueId, () => nav.pop());

  // Track viewed issues so the chat composer's `@` suggestion bar can
  // surface "Recent" — the user just looked at MUL-123, likely wants to
  // ask the agent about it next. Workspace-scoped + in-memory; see
  // data/viewed-issues-store.ts.
  useEffect(() => {
    if (wsId && issueId) {
      useViewedIssuesStore.getState().push(wsId, issueId);
    }
  }, [wsId, issueId]);

  // Screen-scoped composer state — clear on unmount so re-entering the
  // issue starts from a clean slate (no stale text-selection comment id,
  // no stale "Replying to X" target). Both stores are singletons used by
  // the long-press menu.
  useEffect(() => {
    return () => {
      useCommentSelectStore.getState().clear();
      useReplyTargetStore.getState().clear();
    };
  }, []);

  const onRefresh = useCallback(async () => {
    await Promise.all([
      detail.refetch(),
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) }),
    ]);
  }, [detail, qc, wsId, issueId]);

  const issue = detail.data;
  const deleteIssue = useDeleteIssue();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: pins } = useQuery(pinListOptions(wsId, userId));
  const isPinned =
    !!issue &&
    !!pins?.some((p) => p.item_type === "issue" && p.item_id === issue.id);
  const createPin = useCreatePin();
  const deletePin = useDeletePin();

  // EXPO_PUBLIC_WEB_URL on iOS; the harmony bundle inlines MULTICA_WEB_URL
  // instead (babel transform — see AGENTS.md env variants).
  const wsUrl = process.env.MULTICA_WEB_URL;
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const issueLink =
    issue && wsUrl && wsSlug ? `${wsUrl}/${wsSlug}/issue/${issue.identifier}` : null;

  // Sheet visibility is local state; the sheets mount from here like the
  // iOS formSheet routes mounted from the stack.
  const [menuOpen, setMenuOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [pickerField, setPickerField] = useState<IssuePickerField | null>(null);

  const closePicker = useCallback(() => setPickerField(null), []);

  const onOpenPicker = useCallback(
    (field: IssuePickerField) => setPickerField(field),
    [],
  );

  const onDelete = () => {
    if (!issue) return;
    Alert.alert(
      "Delete issue?",
      `${issue.identifier} and its comments, reactions, and attachments will be permanently deleted. This cannot be undone.`,
      [
        { text: "Cancel", style: "cancel" },
        {
          text: "Delete",
          style: "destructive",
          onPress: () =>
            deleteIssue.mutate(issue.id, {
              onSuccess: () => nav.pop(),
            }),
        },
      ],
    );
  };

  return (
    <SafeAreaView
      edges={["bottom"]}
      style={[styles.screen, { backgroundColor: c.background }]}
    >
      <ScreenHeader
        title={issue?.identifier ?? "Issue"}
        onBack={() => nav.pop()}
        right={
          issue ? (
            <View style={styles.headerRight}>
              {/* Ambient agent-working badge — renders null when no
               *  active tasks, so it doesn't crowd the header in the
               *  common case. See agent-header-badge.tsx. */}
              <AgentHeaderBadge issueId={issueId} onOpenRuns={onOpenRuns} />
              <IconButton
                name="ellipsis-horizontal"
                onPress={() => setMenuOpen(true)}
                accessibilityLabel="Issue actions"
              />
            </View>
          ) : undefined
        }
      />
      {detail.isLoading ? (
        <View style={styles.loading}>
          <ActivityIndicator />
        </View>
      ) : detail.error || !issue ? (
        <View style={styles.errorWrap}>
          <Text style={[styles.errorText, { color: c.destructive }]}>
            Failed to load issue:{" "}
            {detail.error instanceof Error
              ? detail.error.message
              : "not found"}
          </Text>
          <Button variant="outline" onPress={() => detail.refetch()}>
            <Text>Retry</Text>
          </Button>
        </View>
      ) : (
        <View style={styles.fill}>
          <TimelineList
            issue={issue}
            entries={timeline.data}
            timelineLoading={timeline.isLoading}
            refreshing={detail.isRefetching || timeline.isRefetching}
            onRefresh={onRefresh}
            onOpenPicker={onOpenPicker}
            onOpenRuns={onOpenRuns}
            highlightCommentId={highlightCommentId}
            highlightNonce={highlightNonce}
          />
          <InlineCommentComposer issueId={issueId} />
        </View>
      )}

      {/* "…" actions — the ActionSheetIOS stand-in. Same content order as
       *  the iOS sheet; property edits live on the header chips inside the
       *  timeline list, not in this menu — one entry per action. */}
      <BottomSheet visible={menuOpen} onClose={() => setMenuOpen(false)}>
        {issue ? (
          <View style={styles.menu}>
            <MenuRow
              label={isPinned ? "Unpin" : "Pin"}
              icon={isPinned ? "pin-outline" : "pin"}
              onPress={() => {
                setMenuOpen(false);
                if (isPinned) {
                  deletePin.mutate({ itemType: "issue", itemId: issue.id });
                } else {
                  createPin.mutate({ item_type: "issue", item_id: issue.id });
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
            {issueLink ? (
              <MenuRow
                label="Copy link"
                icon="copy-outline"
                onPress={() => {
                  setMenuOpen(false);
                  void clipboardSetStringAsync(issueLink);
                }}
              />
            ) : null}
            {issueLink ? (
              <MenuRow
                label="Open on web"
                icon="open-outline"
                onPress={() => {
                  setMenuOpen(false);
                  void Linking.openURL(issueLink);
                }}
              />
            ) : null}
            <MenuRow
              label="Delete issue"
              icon="trash-outline"
              destructive
              onPress={() => {
                setMenuOpen(false);
                onDelete();
              }}
            />
            <View style={[styles.menuSeparator, { backgroundColor: c.border }]} />
            <MenuRow label="Cancel" onPress={() => setMenuOpen(false)} />
          </View>
        ) : null}
      </BottomSheet>

      <IssueEditSheet
        issueId={issueId}
        visible={editOpen}
        onClose={() => setEditOpen(false)}
      />

      {/* The six iOS picker routes, as sheets. All read the issue from the
       *  detail cache and fire useUpdateIssue themselves. */}
      <IssueStatusPickerSheet
        issueId={issueId}
        visible={pickerField === "status"}
        onClose={closePicker}
      />
      <IssuePriorityPickerSheet
        issueId={issueId}
        visible={pickerField === "priority"}
        onClose={closePicker}
      />
      <IssueAssigneePickerSheet
        issueId={issueId}
        visible={pickerField === "assignee"}
        onClose={closePicker}
      />
      <IssueLabelPickerSheet
        issueId={issueId}
        visible={pickerField === "label"}
        onClose={closePicker}
      />
      <IssueProjectPickerSheet
        issueId={issueId}
        visible={pickerField === "project"}
        onClose={closePicker}
      />
      <IssueDueDatePickerSheet
        issueId={issueId}
        visible={pickerField === "due-date"}
        onClose={closePicker}
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
  fill: { flex: 1 },
  headerRight: { flexDirection: "row", alignItems: "center", gap: 8 },
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
  // text-sm text-center
  errorText: { fontSize: 14, textAlign: "center" },
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
