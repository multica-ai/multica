import { useCallback, useEffect } from "react";
import {
  ActionSheetIOS,
  ActivityIndicator,
  Linking,
  View,
} from "react-native";
import { router } from "expo-router";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import * as Clipboard from "expo-clipboard";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { IconButton } from "@/components/ui/icon-button";
import { StatusIcon } from "@/components/ui/status-icon";
import { TimelineList } from "@/components/issue/timeline-list";
import { InlineCommentComposer } from "@/components/issue/inline-comment-composer";
import {
  issueDetailOptions,
  issueKeys,
  issueTimelineOptions,
} from "@/data/queries/issues";
import { useIssueRealtime } from "@/data/realtime/use-issue-realtime";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useViewedIssuesStore } from "@/data/viewed-issues-store";
import { useCommentSelectStore } from "@/data/comment-select-store";
import { useReplyTargetStore } from "@/data/stores/reply-target-store";
import { STATUS_LABEL } from "@/lib/issue-status";

interface IssueDetailPaneProps {
  issueId: string;
  workspaceSlug: string;
  onClose?: () => void;
  onDeleted?: () => void;
}

/**
 * iPad-owned issue detail container. Product semantics intentionally reuse
 * the phone detail's queries, timeline pipeline, composer, form-sheet
 * pickers, and per-record realtime hook; only navigation differs. Selecting
 * another row swaps this pane instead of pushing a new screen.
 */
export function IssueDetailPane({
  issueId,
  workspaceSlug,
  onClose,
  onDeleted,
}: IssueDetailPaneProps) {
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const qc = useQueryClient();
  const detail = useQuery(issueDetailOptions(wsId, issueId));
  const timeline = useQuery(issueTimelineOptions(wsId, issueId));

  const handleDeleted = useCallback(() => onDeleted?.(), [onDeleted]);
  useIssueRealtime(issueId, handleDeleted);

  useEffect(() => {
    if (wsId && issueId) {
      useViewedIssuesStore.getState().push(wsId, issueId);
    }
  }, [wsId, issueId]);

  useEffect(() => {
    return () => {
      useCommentSelectStore.getState().clear();
      useReplyTargetStore.getState().clear();
    };
  }, [issueId]);

  const onRefresh = useCallback(async () => {
    await Promise.all([
      detail.refetch(),
      qc.invalidateQueries({ queryKey: issueKeys.timeline(wsId, issueId) }),
    ]);
  }, [detail, issueId, qc, wsId]);

  const issue = detail.data;

  const openStatus = () => {
    router.push(`/${workspaceSlug}/issue/${issueId}/picker/status`);
  };

  const openActions = () => {
    if (!issue) return;
    const webUrl = process.env.EXPO_PUBLIC_WEB_URL;
    const issueLink = webUrl
      ? `${webUrl}/${workspaceSlug}/issue/${issue.identifier}`
      : null;
    const options = ["Cancel", "Edit details"];
    if (issueLink) options.push("Copy link", "Open on web");

    ActionSheetIOS.showActionSheetWithOptions(
      { options, cancelButtonIndex: 0, title: issue.identifier },
      (index) => {
        const action = options[index];
        if (action === "Edit details") {
          router.push(`/${workspaceSlug}/issue/${issue.id}/edit`);
        } else if (action === "Copy link" && issueLink) {
          void Clipboard.setStringAsync(issueLink);
        } else if (action === "Open on web" && issueLink) {
          void Linking.openURL(issueLink);
        }
      },
    );
  };

  return (
    <View className="flex-1 bg-background">
      <View className="h-14 flex-row items-center border-b border-border px-3">
        <View className="min-w-0 flex-1 px-1">
          <Text className="text-xs text-muted-foreground" numberOfLines={1}>
            {issue?.identifier ?? "Issue"}
          </Text>
          <Text className="text-sm font-semibold" numberOfLines={1}>
            {issue?.title ?? "Loading issue…"}
          </Text>
        </View>
        {issue ? (
          <Button
            variant="outline"
            size="sm"
            onPress={openStatus}
            accessibilityLabel="Change issue status"
            className="mr-1"
          >
            <StatusIcon status={issue.status} size={14} />
            <Text numberOfLines={1}>{STATUS_LABEL[issue.status]}</Text>
          </Button>
        ) : null}
        <IconButton
          name="ellipsis-horizontal"
          onPress={openActions}
          disabled={!issue}
          accessibilityLabel="Issue actions"
        />
        {onClose ? (
          <IconButton
            name="close"
            onPress={onClose}
            accessibilityLabel="Close issue detail"
          />
        ) : null}
      </View>

      {detail.isLoading ? (
        <View className="flex-1 items-center justify-center">
          <ActivityIndicator />
        </View>
      ) : detail.error || !issue ? (
        <View className="flex-1 items-center justify-center px-6 gap-3">
          <Text className="text-sm text-destructive text-center">
            Failed to load issue: {detail.error instanceof Error ? detail.error.message : "not found"}
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
          />
          <InlineCommentComposer issueId={issueId} />
        </View>
      )}
    </View>
  );
}
