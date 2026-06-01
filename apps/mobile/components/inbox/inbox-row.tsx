/**
 * Inbox row content — the visual half, no gesture wrapping. Pulled out of
 * (tabs)/inbox.tsx so swipeable-inbox-row.tsx can wrap it with the gesture
 * recognizer without duplicating layout. Keep this file purely presentational
 * — the swipe and the press behaviour live in the wrapper.
 *
 * Visual structure mirrors web's InboxListItem
 * (packages/views/inbox/components/inbox-list-item.tsx). Per
 * apps/mobile/CLAUDE.md "Visual alignment is baseline":
 *   - Right column stacks vertically: status icon on top row, time on bottom.
 *   - Secondary line uses the type-aware `InboxDetailLabel`, not raw body.
 */
import { Pressable, View } from "react-native";
import type { InboxItem } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { StatusIcon } from "@/components/ui/status-icon";
import { InboxDetailLabel } from "@/components/inbox/detail-label";
import { getInboxDisplayTitle } from "@/lib/inbox-display";
import { timeAgo } from "@/lib/time-ago";
import { cn } from "@/lib/utils";

interface Props {
  item: InboxItem;
  onPress: () => void;
}

export function InboxRow({ item, onPress }: Props) {
  const isUnread = !item.read;
  const displayTitle = getInboxDisplayTitle(item);
  const actorType = item.actor_type ?? item.recipient_type;
  const actorId = item.actor_id ?? item.recipient_id;

  return (
    <Pressable onPress={onPress} className="bg-background active:bg-secondary px-4 py-3">
      <View className="flex-row gap-3">
        <ActorAvatar type={actorType} id={actorId} size={36} showPresence />
        <View className="flex-1 min-w-0">
          {/* Top row: [unread dot + title] (left) | [status icon] (right) */}
          <View className="flex-row items-center gap-2">
            <View className="flex-row items-center gap-1.5 flex-1 min-w-0">
              {isUnread ? (
                <View className="size-1.5 rounded-full bg-brand shrink-0" />
              ) : null}
              <Text
                className={cn(
                  "flex-1 text-sm",
                  isUnread
                    ? "font-medium text-foreground"
                    : "text-muted-foreground",
                )}
                numberOfLines={1}
              >
                {displayTitle}
              </Text>
            </View>
            {item.issue_status ? (
              <StatusIcon status={item.issue_status} size={14} />
            ) : null}
          </View>
          {/* Bottom row: [type-aware detail label] (left) | [time] (right).
              Detail label mirrors web InboxDetailLabel — same per-type
              wording (Mentioned / Set status to ... / Assigned to ... / etc),
              not the raw markdown body. */}
          <View className="flex-row items-center gap-2 mt-0.5">
            <View className="flex-1 min-w-0">
              <InboxDetailLabel
                item={item}
                className={
                  isUnread
                    ? "text-muted-foreground"
                    : "text-muted-foreground/60"
                }
              />
            </View>
            <Text
              className={cn(
                "text-xs shrink-0",
                isUnread
                  ? "text-muted-foreground"
                  : "text-muted-foreground/60",
              )}
            >
              {timeAgo(item.created_at)}
            </Text>
          </View>
        </View>
      </View>
    </Pressable>
  );
}
