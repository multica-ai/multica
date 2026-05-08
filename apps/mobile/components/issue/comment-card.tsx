import { View } from "react-native";
import type { TimelineEntry } from "@multica/core/types";

import { CommentRow } from "@/components/issue/comment-row";

interface Props {
  issueId: string;
  parent: TimelineEntry;
  replies: TimelineEntry[];
  currentUserId?: string;
  onReplyPress: (entry: TimelineEntry) => void;
  onReactPress: (commentId: string) => void;
}

// One thread = one Card. Parent on top, replies stacked beneath, separated
// only by a faint divider. Mirrors web's comment-card.tsx visual contract:
// the Card itself communicates "these comments belong together" — no indent,
// no "↳ to @parent" tag.
//
// Differences vs web:
//   - No Collapsible toggle. On a phone the timeline is short enough that
//     hiding bodies is more friction than reward.
//   - No inline ReplyInput at the bottom. Replying happens via the long-press
//     ActionSheet → sets composer.replyTo → the bottom sticky composer shows
//     the iMessage-style quote bar + autofocuses. One composer to rule them
//     all means thumbs stay at the bottom of the screen.
export function CommentCard({
  issueId,
  parent,
  replies,
  currentUserId,
  onReplyPress,
  onReactPress,
}: Props) {
  return (
    <View className="mx-3 rounded-2xl bg-card border border-border overflow-hidden">
      <CommentRow
        issueId={issueId}
        entry={parent}
        currentUserId={currentUserId}
        onReplyPress={onReplyPress}
        onReactPress={onReactPress}
      />
      {replies.map((reply) => (
        <View key={reply.id} className="border-t border-border/50">
          <CommentRow
            issueId={issueId}
            entry={reply}
            currentUserId={currentUserId}
            onReplyPress={onReplyPress}
            onReactPress={onReactPress}
          />
        </View>
      ))}
    </View>
  );
}
