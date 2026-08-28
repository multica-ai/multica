/**
 * Issue detail screen.
 *
 * Read-mostly timeline with an inline comment composer pinned to the
 * bottom (`<InlineCommentComposer>`). The composer is a single
 * `<TextInput>` + mention suggestion bar — no modal route, no toolbar,
 * no draft persistence. Sticks to the keyboard via `KeyboardStickyView`.
 *
 * Header note: the parent _layout.tsx already declares the `issue/[id]`
 * Stack.Screen with title "Issue". We override that here once the data
 * lands so the navigation bar shows `MUL-123` (Linear-style).
 *
 * Right-top "…" menu uses @rn-primitives DropdownMenu (pure JS, cross-
 * platform). Previous ActionSheetIOS implementation crashed on Android.
 */
import { useCallback, useEffect, useRef } from "react";
import {
  ActivityIndicator,
  Alert,
  Linking,
  View,
} from "react-native";
import { Stack, router, useLocalSearchParams } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import * as Clipboard from "expo-clipboard";
import type { Issue } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import {
  TimelineList,
  type TimelineListHandle,
} from "@/components/issue/timeline-list";
import { AgentHeaderBadge } from "@/components/issue/agent-header-badge";
import { InlineCommentComposer } from "@/components/issue/inline-comment-composer";
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
import { getWebUrl } from "@/data/server-store";
import { useViewedIssuesStore } from "@/data/viewed-issues-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import i18n from "i18next";
import { useT } from "@/lib/use-t";

export default function IssueDetail() {
  const { t } = useT("issues");
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

  // Screen-scoped composer state — clear on unmount so re-entering the
  // issue starts from a clean slate (no stale text-selection comment id,
  // no stale "Replying to X" target). Both stores are singletons used by
  // the long-press action sheet.
  useEffect(() => {
    return () => {
      useCommentSelectStore.getState().clear();
      useReplyTargetStore.getState().clear();
    };
  }, []);

  const onRefresh = useCallback(async () => {
    await Promise.all([
      detail.refetch(),
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, id) }),
    ]);
  }, [detail, qc, wsId, id]);

  const issue = detail.data;
  const deleteIssue = useDeleteIssue();
  const userId = useAuthStore((s) => s.user?.id ?? null);
  const { data: pins } = useQuery(pinListOptions(wsId, userId));
  const isPinned =
    !!issue &&
    !!pins?.some((p) => p.item_type === "issue" && p.item_id === issue.id);
  const createPin = useCreatePin();
  const deletePin = useDeletePin();

  // Three-dot menu: Pin/Unpin / Edit details / Copy link / Open on web / Delete.
  // Cross-platform: uses @rn-primitives DropdownMenu (pure JS, no native deps).
  const issueLink = issue && wsSlug
    ? `${getWebUrl()}/${wsSlug}/issue/${issue.identifier}`
    : "";
  const onTogglePin = useCallback(() => {
    if (!issue) return;
    if (isPinned) {
      deletePin.mutate({ itemType: "issue", itemId: issue.id });
    } else {
      createPin.mutate({ item_type: "issue", item_id: issue.id });
    }
  }, [issue, isPinned, createPin, deletePin]);
  const onEditDetails = useCallback(() => {
    if (wsSlug && issue) router.push(`/${wsSlug}/issue/${issue.id}/edit`);
  }, [wsSlug, issue]);
  const onCopyLink = useCallback(() => {
    if (issueLink) Clipboard.setStringAsync(issueLink);
  }, [issueLink]);
  const onOpenOnWeb = useCallback(() => {
    if (issueLink) Linking.openURL(issueLink);
  }, [issueLink]);
  const onDelete = useCallback(() => {
    if (!issue) return;
    confirmDelete(issue, () =>
      deleteIssue.mutate(issue.id, {
        onSuccess: () => router.back(),
      }),
    );
  }, [issue, deleteIssue]);

  // Timeline imperative handle (RUYI-28): reporting the server comment id
  // of THIS user's just-published comment lets the timeline expand the
  // owning root without guessing from last-row diffs (which also fired for
  // other users' realtime arrivals).
  const timelineRef = useRef<TimelineListHandle>(null);
  const onCommentPublished = useCallback((commentId: string) => {
    timelineRef.current?.expandPublished(commentId);
  }, []);

  return (
    <View className="flex-1 bg-background">
      <Stack.Screen
        options={{
          // 兜底串走 i18n：`_layout.tsx` 的同名路由 title 已本地化，这里
          // 若留英文硬编码，加载期非英文用户会先闪一下 "Issue" 再变。
          // `Stack.Screen` 在组件 return 内，求值时机晚于 initI18n()。
          title: issue?.identifier ?? i18n.t("layout:tab.issue", "Issue"),
          headerBackTitle: "Back",
          headerRight: issue
            ? () => (
                <View className="flex-row items-center gap-2">
                  {/* Ambient agent-working badge — renders null when no
                   *  active tasks, so it doesn't crowd the header in the
                   *  common case. See agent-header-badge.tsx. */}
                  <AgentHeaderBadge issueId={id} />
                  {/* Comments directory (RUYI-28) — browse/filter root
                   *  comments; tapping an entry expands + locates that
                   *  thread in the timeline below. */}
                  <IconButton
                    name="list"
                    accessibilityLabel={t(
                      "mobile.detail.comments_open_a11y",
                      "Browse comments",
                    )}
                    onPress={
                      wsSlug
                        ? () =>
                            router.push(`/${wsSlug}/issue/${issue.id}/comments`)
                        : undefined
                    }
                  />
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <IconButton
                          name="ellipsis-horizontal"
                          accessibilityLabel={t(
                            "mobile.detail.actions_a11y",
                            "Issue actions",
                          )}
                        />
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="w-44">
                        <DropdownMenuItem onPress={onTogglePin}>
                          <Text>{isPinned ? t("detail.unpin_tooltip", "Unpin from sidebar") : t("detail.pin_tooltip", "Pin to sidebar")}</Text>
                        </DropdownMenuItem>
                        <DropdownMenuItem onPress={onEditDetails}>
                          <Text>
                            {t("mobile.detail.edit_details", "Edit details")}
                          </Text>
                        </DropdownMenuItem>
                        <DropdownMenuItem onPress={onCopyLink}>
                          <Text>{t("actions.copy_link", "Copy link")}</Text>
                        </DropdownMenuItem>
                        <DropdownMenuItem onPress={onOpenOnWeb}>
                          <Text>
                            {t("mobile.detail.open_on_web", "Open on web")}
                          </Text>
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem variant="destructive" onPress={onDelete}>
                          <Text>{t("actions.delete_issue", "Delete issue")}</Text>
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
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
            {/* 同 select-workspace：整句插值，不做「失败：<详情>」拼接。 */}
            {t("mobile.detail.load_failed", "Failed to load issue: {{reason}}", {
              reason:
                detail.error instanceof Error
                  ? detail.error.message
                  : t("mobile.detail.not_found", "not found"),
            })}
          </Text>
          <Button variant="outline" onPress={() => detail.refetch()}>
            <Text>{t("mobile.detail.retry", "Retry")}</Text>
          </Button>
        </View>
      ) : (
        <View className="flex-1">
          <TimelineList
            ref={timelineRef}
            issue={issue}
            entries={timeline.data}
            timelineLoading={timeline.isLoading}
            refreshing={detail.isRefetching || timeline.isRefetching}
            onRefresh={onRefresh}
            highlightCommentId={highlight}
            highlightNonce={h}
            onCommentPublished={onCommentPublished}
          />
          <InlineCommentComposer
            issueId={id}
            onPublished={onCommentPublished}
          />
        </View>
      )}
    </View>
  );
}

function confirmDelete(issue: Issue, onConfirm: () => void) {
  // 普通函数（由 onDelete 回调调用），不能调 Hook——直接用 i18n.t("ns:key")。
  Alert.alert(
    i18n.t("modals:delete_issue.title", "Delete issue"),
    `${issue.identifier} ` + i18n.t("modals:delete_issue.description", "This will permanently delete this issue and all its comments. This action cannot be undone."),
    [
      { text: i18n.t("common:cancel", "Cancel"), style: "cancel" },
      { text: i18n.t("modals:delete_issue.confirm", "Delete"), style: "destructive", onPress: onConfirm },
    ],
  );
}
