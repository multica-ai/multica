import { ActionSheetIOS, Pressable, Text, View } from "react-native";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import type { TimelineEntry } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";

import { ActorAvatar } from "@/components/ui/actor-avatar";
import { Markdown } from "@/components/ui/markdown";
import { CommentReactionBar } from "@/components/issue/comment-reaction-bar";
import { useTimeAgo } from "@/lib/use-time-ago";

interface Props {
  issueId: string;
  entry: TimelineEntry;
  currentUserId?: string;
  onReplyPress: (entry: TimelineEntry) => void;
  onReactPress: (commentId: string) => void;
}

// Single comment row used inside a CommentCard. Parent and replies share the
// same Card (see CommentCard), with sibling replies separated by a border-t.
// That co-location is what conveys thread membership — we don't draw "↳ to
// @parent" tags or indent reply rows.
//
// Long-press anywhere → ActionSheet (Reply / React / Copy). Short tap is a
// no-op so users can read without accidental triggers.
export function CommentRow({
  issueId,
  entry,
  currentUserId,
  onReplyPress,
  onReactPress,
}: Props) {
  const timeAgo = useTimeAgo(entry.created_at);
  const { getActorName } = useActorName();
  const authorName = getActorName(entry.actor_type, entry.actor_id);
  const content = entry.content ?? "";
  const reactions = entry.reactions ?? [];

  const handleLongPress = () => {
    Haptics.selectionAsync().catch(() => {});
    ActionSheetIOS.showActionSheetWithOptions(
      {
        options: ["Cancel", "Reply", "Add reaction", "Copy text"],
        cancelButtonIndex: 0,
        title: content
          ? content.replace(/\n/g, " ").slice(0, 80) +
            (content.length > 80 ? "…" : "")
          : authorName,
      },
      (i) => {
        if (i === 1) onReplyPress(entry);
        else if (i === 2) onReactPress(entry.id);
        else if (i === 3) {
          void Clipboard.setStringAsync(content);
          Haptics.notificationAsync(
            Haptics.NotificationFeedbackType.Success,
          ).catch(() => {});
        }
      },
    );
  };

  return (
    <Pressable
      onLongPress={handleLongPress}
      delayLongPress={300}
      android_disableSound
      className="px-4 py-3 active:bg-muted/30"
    >
      <View className="flex-row gap-3">
        <ActorAvatar
          type={entry.actor_type as "member" | "agent"}
          id={entry.actor_id}
          size={28}
        />
        <View className="flex-1">
          <View className="flex-row items-baseline gap-2">
            <Text className="text-foreground text-sm font-semibold">
              {authorName}
            </Text>
            {entry.actor_type === "agent" && (
              <Text className="text-muted-foreground text-xs">Agent</Text>
            )}
            <Text className="text-muted-foreground text-xs">{timeAgo}</Text>
          </View>
          <View className="mt-1">
            <Markdown content={content} variant="comment" />
          </View>
          {reactions.length > 0 && (
            <CommentReactionBar
              issueId={issueId}
              commentId={entry.id}
              reactions={reactions}
              currentUserId={currentUserId}
              onAddPress={() => onReactPress(entry.id)}
            />
          )}
        </View>
      </View>
    </Pressable>
  );
}
