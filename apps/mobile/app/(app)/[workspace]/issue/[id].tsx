/**
 * Issue detail screen (V1).
 *
 * Read-mostly + comment composer. Property edits, replies, reactions,
 * attachments inline render, mention chips, and image lightbox are deferred
 * to V2+ — see /Users/qingnaiyuan/.claude/plans/plan-dynamic-narwhal.md.
 *
 * Header note: the parent _layout.tsx already declares the
 * `issue/[id]` Stack.Screen with title "Issue". We override that here once
 * the data lands so the navigation bar shows `MUL-123` (Linear-style).
 */
import { useCallback, useEffect, useState } from "react";
import {
  ActivityIndicator,
  KeyboardAvoidingView,
  Platform,
  View,
} from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useHeaderHeight } from "@react-navigation/elements";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { TimelineList } from "@/components/issue/timeline-list";
import { CommentComposer } from "@/components/issue/comment-composer";
import {
  issueDetailOptions,
  issueKeys,
  issueTimelineOptions,
} from "@/data/queries/issues";
import { useCreateComment } from "@/data/mutations/issues";
import { useIssueRealtime } from "@/data/realtime/use-issue-realtime";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useViewedIssuesStore } from "@/data/viewed-issues-store";

export default function IssueDetail() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const qc = useQueryClient();
  // KeyboardAvoidingView's `padding` behaviour calculates from screen top.
  // The native iOS Stack header above this screen takes ~88pt that the
  // padding doesn't subtract — without this offset, the comment composer
  // ends up under the keyboard by exactly the header height. See
  // https://reactnavigation.org/docs/use-header-height.
  const headerHeight = useHeaderHeight();

  const detail = useQuery(issueDetailOptions(wsId, id));
  const timeline = useQuery(issueTimelineOptions(wsId, id));
  const createComment = useCreateComment(id);

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

  // Lifted: long-press a comment → action sheet → "Reply" sets this; the
  // composer reads it to render a "Replying to <name>" chip and sends the
  // resulting comment with `parent_id`.
  const [replyingTo, setReplyingTo] = useState<{
    commentId: string;
    name: string;
  } | null>(null);

  const onRefresh = useCallback(async () => {
    await Promise.all([
      detail.refetch(),
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, id) }),
    ]);
  }, [detail, qc, wsId, id]);

  const onSubmitComment = useCallback(
    async (vars: { content: string; parentId?: string }) => {
      await createComment.mutateAsync(vars);
      setReplyingTo(null);
    },
    [createComment],
  );

  const onReplyTo = useCallback((commentId: string, name: string) => {
    setReplyingTo({ commentId, name });
  }, []);

  const onCancelReply = useCallback(() => setReplyingTo(null), []);

  const issue = detail.data;

  return (
    <SafeAreaView className="flex-1 bg-background" edges={["bottom"]}>
      <Stack.Screen
        options={{
          title: issue?.identifier ?? "Issue",
          headerBackTitle: "Back",
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
            Retry
          </Button>
        </View>
      ) : (
        <KeyboardAvoidingView
          behavior={Platform.OS === "ios" ? "padding" : undefined}
          keyboardVerticalOffset={headerHeight}
          className="flex-1"
        >
          <View className="flex-1">
            <TimelineList
              issue={issue}
              entries={timeline.data}
              timelineLoading={timeline.isLoading}
              refreshing={detail.isRefetching || timeline.isRefetching}
              onRefresh={onRefresh}
              onReplyTo={onReplyTo}
            />
          </View>
          <CommentComposer
            issueId={id}
            onSubmit={onSubmitComment}
            replyingTo={replyingTo}
            onCancelReply={onCancelReply}
          />
        </KeyboardAvoidingView>
      )}
    </SafeAreaView>
  );
}
