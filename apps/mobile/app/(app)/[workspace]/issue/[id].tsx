/**
 * Issue detail screen.
 *
 * Read-mostly + bottom "Comment" button. Tapping the button (or "Reply"
 * on a long-pressed comment) opens the new-comment modal at
 * issue/[id]/new-comment — full-screen composer with MentionSuggestionBar
 * and toolbar. The inline always-on composer was retired because it
 * competed with the timeline for vertical space and the KeyboardAvoiding
 * logic was clunky (user feedback). The new pattern matches iOS Mail
 * "Compose" / Slack thread reply.
 *
 * Header note: the parent _layout.tsx already declares the `issue/[id]`
 * Stack.Screen with title "Issue". We override that here once the data
 * lands so the navigation bar shows `MUL-123` (Linear-style).
 */
import { useCallback, useEffect } from "react";
import {
  ActionSheetIOS,
  ActivityIndicator,
  Alert,
  Linking,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import * as Clipboard from "expo-clipboard";
import type { Issue } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { TimelineList } from "@/components/issue/timeline-list";
import { AgentHeaderBadge } from "@/components/issue/agent-header-badge";
import { RunsSheet } from "@/components/issue/runs-sheet";
import {
  issueDetailOptions,
  issueKeys,
  issueTimelineOptions,
} from "@/data/queries/issues";
import { useDeleteIssue } from "@/data/mutations/issues";
import { useIssueRealtime } from "@/data/realtime/use-issue-realtime";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useViewedIssuesStore } from "@/data/viewed-issues-store";

export default function IssueDetail() {
  // `highlight` + `h` come from inbox deep-link (apps/mobile/app/(app)/
  // [workspace]/(tabs)/inbox.tsx). `highlight` is the target comment id;
  // `h` is a per-tap nonce so re-tapping the same row re-fires the
  // scroll-and-flash effect.
  const { id, workspace: wsSlug, highlight, h } = useLocalSearchParams<{
    id: string;
    workspace: string;
    highlight?: string;
    h?: string;
  }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const qc = useQueryClient();

  const detail = useQuery(issueDetailOptions(wsId, id));
  const timeline = useQuery(issueTimelineOptions(wsId, id));

  // Subscribe to per-issue WS events: status/priority/assignee/label
  // changes, comments, activity, reactions, agent task progress.
  // Mounted with `id` — cleans up automatically on navigate-away.
  // If another client deletes the issue we're viewing, pop back so the
  // user isn't stranded on a 404 detail page.
  useIssueRealtime(id, () => router.back());

  // Track viewed issues so the chat composer's `@` suggestion bar can
  // surface "Recent" — the user just looked at MUL-123, likely wants to
  // ask the agent about it next. Workspace-scoped + in-memory; see
  // data/viewed-issues-store.ts.
  useEffect(() => {
    if (wsId && id) {
      useViewedIssuesStore.getState().push(wsId, id);
    }
  }, [wsId, id]);

  const onRefresh = useCallback(async () => {
    await Promise.all([
      detail.refetch(),
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, id) }),
    ]);
  }, [detail, qc, wsId, id]);

  // Long-press on a comment → ActionSheet → Reply. Lifts to a route push so
  // the new-comment modal receives `parent` + `parentName` and titles
  // itself "Reply". No local state — the modal owns its own input.
  const onReplyTo = useCallback(
    (commentId: string, name: string) => {
      router.push({
        pathname: "/[workspace]/issue/[id]/new-comment",
        params: { workspace: wsSlug, id, parent: commentId, parentName: name },
      });
    },
    [wsSlug, id],
  );

  const openNewComment = useCallback(() => {
    router.push({
      pathname: "/[workspace]/issue/[id]/new-comment",
      params: { workspace: wsSlug, id },
    });
  }, [wsSlug, id]);

  const issue = detail.data;
  const deleteIssue = useDeleteIssue();

  // Three-dot menu: Copy link / Open on web (if web URL set) / Delete.
  // Mirrors apps/mobile/app/(app)/[workspace]/project/[id].tsx:99-148 — same
  // ActionSheetIOS + Alert.alert confirm pattern. Property edits (status,
  // priority, assignee, due_date) live on the IssueHeaderCard chips inside
  // the timeline list, not in this menu — one entry per action.
  const onPressMore = useCallback(() => {
    if (!issue || !wsSlug) return;
    const webUrl = process.env.EXPO_PUBLIC_WEB_URL;
    const issueLink = webUrl
      ? `${webUrl}/${wsSlug}/issue/${issue.identifier}`
      : null;
    const options: string[] = ["Cancel"];
    if (issueLink) options.push("Copy link");
    if (issueLink) options.push("Open on web");
    options.push("Delete issue");
    const destructiveIndex = options.length - 1;
    ActionSheetIOS.showActionSheetWithOptions(
      {
        options,
        cancelButtonIndex: 0,
        destructiveButtonIndex: destructiveIndex,
        title: issue.identifier,
      },
      (i) => {
        const label = options[i];
        if (label === "Copy link" && issueLink) {
          Clipboard.setStringAsync(issueLink);
        } else if (label === "Open on web" && issueLink) {
          Linking.openURL(issueLink);
        } else if (label === "Delete issue") {
          confirmDelete(issue, () =>
            deleteIssue.mutate(issue.id, {
              onSuccess: () => router.back(),
            }),
          );
        }
      },
    );
  }, [issue, wsSlug, deleteIssue]);

  return (
    <SafeAreaView className="flex-1 bg-background" edges={["bottom"]}>
      <Stack.Screen
        options={{
          title: issue?.identifier ?? "Issue",
          headerBackTitle: "Back",
          headerRight: issue
            ? () => (
                <View className="flex-row items-center gap-2">
                  {/* Ambient agent-working badge — renders null when no
                   *  active tasks, so it doesn't crowd the header in the
                   *  common case. See agent-header-badge.tsx. */}
                  <AgentHeaderBadge issueId={id} />
                  <IconButton
                    name="ellipsis-horizontal"
                    onPress={onPressMore}
                    accessibilityLabel="Issue actions"
                  />
                </View>
              )
            : undefined,
        }}
      />
      {detail.isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : detail.error || !issue ? (
        <View className="flex-1 items-center justify-center px-6 gap-3">
          <Text className="text-sm text-destructive text-center">
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
        <View className="flex-1">
          <TimelineList
            issue={issue}
            entries={timeline.data}
            timelineLoading={timeline.isLoading}
            refreshing={detail.isRefetching || timeline.isRefetching}
            onRefresh={onRefresh}
            onReplyTo={onReplyTo}
            highlightCommentId={highlight}
            highlightNonce={h}
          />
          {/* Bottom Comment button — push the new-comment modal. Replaces
           *  the old always-on inline composer (user feedback: cramped,
           *  keyboard avoidance clunky). Linear / Slack thread reply
           *  pattern. */}
          <View className="px-4 py-3 border-t border-border bg-background">
            <Button onPress={openNewComment}>
              <Text>Comment</Text>
            </Button>
          </View>
          {/* Mounted once at the page level so both the in-card
           *  AgentActivityRow and the Stack-header AgentHeaderBadge open
           *  the same sheet instance via useRunsSheetStore. */}
          <RunsSheet issueId={id} />
        </View>
      )}
    </SafeAreaView>
  );
}

function confirmDelete(issue: Issue, onConfirm: () => void) {
  Alert.alert(
    "Delete issue?",
    `${issue.identifier} and its comments, reactions, and attachments will be permanently deleted. This cannot be undone.`,
    [
      { text: "Cancel", style: "cancel" },
      { text: "Delete", style: "destructive", onPress: onConfirm },
    ],
  );
}
