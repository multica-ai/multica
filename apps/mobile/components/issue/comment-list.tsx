import { useMemo } from "react";
import { ActivityIndicator, Text, View } from "react-native";
import { useInfiniteQuery } from "@tanstack/react-query";
import { issueTimelineInfiniteOptions } from "@multica/core/issues/queries";
import { buildTimelineGroups } from "@multica/core/issues/timeline-view";
import type { TimelineEntry } from "@multica/core/types";

import { CommentCard } from "@/components/issue/comment-card";
import { ActivityRow } from "@/components/issue/activity-row";

interface Props {
  issueId: string;
  currentUserId?: string;
  onReplyPress: (entry: TimelineEntry) => void;
  onReactPress: (commentId: string) => void;
}

// Activity feed renderer.
//
// All grouping / coalescing / reply-tree logic lives in
// `@multica/core/issues/timeline-view` and is shared with web. Mobile only
// owns the rendering — see apps/mobile/CLAUDE.md "Cross-platform parity".
//
// Render shape:
//   Card                 ← thread (parent + recursively flattened replies)
//   Card                 ← thread
//   <activity strip>     ← consecutive activities, coalesced
//   Card                 ← thread
export function CommentList({
  issueId,
  currentUserId,
  onReplyPress,
  onReactPress,
}: Props) {
  const { data, isLoading, error } = useInfiniteQuery(
    issueTimelineInfiniteOptions(issueId),
  );

  const view = useMemo(() => {
    if (!data) return { groups: [] };
    // Backend returns newest-first; reverse for natural top-to-bottom reading
    // (mirrors web's useIssueTimeline `flat.reverse()` at line 112).
    const flat = data.pages.flatMap((p) => p.entries) as TimelineEntry[];
    return buildTimelineGroups([...flat].reverse());
  }, [data]);

  if (isLoading) {
    return (
      <View className="px-4 py-6 items-center">
        <ActivityIndicator />
      </View>
    );
  }

  if (error) {
    return (
      <View className="px-4 py-4">
        <Text className="text-destructive text-sm">
          Failed to load activity
        </Text>
      </View>
    );
  }

  if (view.groups.length === 0) {
    return (
      <View className="px-4 py-6">
        <Text className="text-muted-foreground text-sm text-center">
          No activity yet
        </Text>
      </View>
    );
  }

  return (
    <View>
      <View className="px-4 pt-4 pb-2">
        <Text className="text-muted-foreground text-xs uppercase tracking-wider">
          Activity
        </Text>
      </View>
      <View className="gap-3">
        {view.groups.map((group, idx) => {
          if (group.kind === "comment") {
            return (
              <CommentCard
                key={group.parent.id}
                issueId={issueId}
                parent={group.parent}
                replies={group.replies}
                currentUserId={currentUserId}
                onReplyPress={onReplyPress}
                onReactPress={onReactPress}
              />
            );
          }
          return (
            <View key={`act-${group.entries[0]!.id}-${idx}`}>
              {group.entries.map((entry) => (
                <ActivityRow key={entry.id} entry={entry} />
              ))}
            </View>
          );
        })}
      </View>
    </View>
  );
}
