/**
 * The scrolling timeline. ASC chronological — oldest at top, newest near the
 * bottom (above the composer). Pull-to-refresh refetches issue + timeline.
 *
 * Backend returns the full timeline in one shot (server-side pagination
 * was dropped in #2322 — p99 ~30 entries per issue, cursor walking only
 * created bugs at reply-thread boundaries). The previous "Pull to load
 * older" UX and top-edge `fetchOlder` trigger are gone.
 *
 * Uses native FlatList (mobile baseline doesn't include FlashList — see
 * apps/mobile/CLAUDE.md "Tech-stack baseline"). For the issue volumes the
 * product targets, FlatList is fine.
 */
import { useMemo } from "react";
import { ActivityIndicator, FlatList, RefreshControl, View } from "react-native";
import type { Issue, TimelineEntry } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { IssueHeaderCard } from "./issue-header-card";
import { IssueDescription } from "./issue-description";
import { IssueReactionRow } from "./issue-reaction-row";
import { ActivityRow } from "./activity-row";
import { CommentCard } from "./comment-card";
import { coalesceTimeline } from "@/lib/timeline-coalesce";
import { buildTimelineRows, type TimelineRow } from "@/lib/timeline-thread";

interface Props {
  issue: Issue;
  entries: TimelineEntry[] | undefined;
  timelineLoading: boolean;
  refreshing: boolean;
  onRefresh: () => void;
  /** Long-press → Reply on a comment bubbles up via this callback. The
   *  issue page lifts replyingTo state and feeds it back into the composer. */
  onReplyTo: (commentId: string, name: string) => void;
}

export function TimelineList({
  issue,
  entries,
  timelineLoading,
  refreshing,
  onRefresh,
  onReplyTo,
}: Props) {
  // Server already returns ASC oldest-first. Pipeline:
  //   1. coalesceTimeline → merge consecutive identical activities
  //   2. buildTimelineRows → reorder so replies sit adjacent to their parent
  //      and tag each reply with `replyTo` for the card to render the
  //      "↪ Replying to" header + thread-line border. This is the mobile
  //      flat-list interpretation of web's recursive reply tree.
  const data = useMemo<TimelineRow[]>(() => {
    if (!entries) return [];
    return buildTimelineRows(coalesceTimeline(entries));
  }, [entries]);

  const ListHeader = (
    <View>
      <IssueHeaderCard issue={issue} />
      <IssueDescription description={issue.description} />
      <IssueReactionRow issue={issue} />
      <View className="px-4 pt-4 pb-2 border-t border-border">
        <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
          Activity
        </Text>
      </View>
      {timelineLoading && (!entries || entries.length === 0) ? (
        <View className="py-6 items-center">
          <ActivityIndicator />
        </View>
      ) : null}
    </View>
  );

  return (
    <FlatList
      data={data}
      keyExtractor={(row) => row.entry.id}
      ListHeaderComponent={ListHeader}
      renderItem={({ item }) =>
        item.entry.type === "comment" ? (
          <CommentCard
            entry={item.entry}
            replies={item.replies}
            issueId={issue.id}
            onReplyTo={onReplyTo}
          />
        ) : (
          <ActivityRow entry={item.entry} />
        )
      }
      refreshControl={
        <RefreshControl refreshing={refreshing} onRefresh={onRefresh} />
      }
      // gap-3 between every row gives uniform 12px spacing — matches web's
      // `<div className="mt-4 flex flex-col gap-3">` outer container. With
      // this owning the spacing, the row components themselves drop their
      // own py so we don't double-up vertical breathing room.
      contentContainerClassName="pb-4 gap-3"
    />
  );
}
