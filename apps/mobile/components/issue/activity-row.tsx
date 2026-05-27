/**
 * Activity (non-comment) timeline row. Mirrors the visual contract of web's
 * `packages/views/issues/components/issue-detail.tsx:1046-1100`:
 *
 *   1. Single-line layout — `[lead icon] [actor name] [verb...truncate] [×N badge?] [time→]`
 *   2. Contextual lead icon by action:
 *        - status_changed   → StatusIcon for the NEW status (details.to)
 *        - priority_changed → PriorityIcon for the NEW priority (details.to)
 *        - due_date_changed → Calendar glyph
 *        - everything else  → small ActorAvatar (size 16)
 *      The icon does the recognition work — user sees the new state at a
 *      glance before they read the verb.
 *   3. Whole row is `text-xs text-muted-foreground` (web parity). Actor name
 *      is `font-medium` but inherits the muted color — activity is supposed
 *      to feel quiet next to comment bubbles.
 *   4. Time is **relative**, right-aligned (`ml-auto`). Web shows absolute
 *      time in a hover tooltip; mobile has no hover so we just rely on
 *      relative for v1 (long-press → absolute time can be V2).
 *   5. Coalesce ×N chip when `coalesced_count > 1`, except `task_completed` /
 *      `task_failed` which already bake the count into their phrase.
 */
import { View } from "react-native";
import Svg, { Line, Rect } from "react-native-svg";
import type {
  IssuePriority,
  IssueStatus,
  TimelineEntry,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { formatActivity } from "@/lib/format-activity";
import { timeAgo } from "@/lib/time-ago";
import { useActorLookup } from "@/data/use-actor-name";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

function CalendarGlyph({
  size = 14,
  stroke,
}: {
  size?: number;
  stroke: string;
}) {
  return (
    <Svg width={size} height={size} viewBox="0 0 16 16" fill="none">
      <Rect
        x={2}
        y={3.5}
        width={12}
        height={10.5}
        rx={1.5}
        stroke={stroke}
        strokeWidth={1.2}
      />
      <Line
        x1={5}
        y1={1.5}
        x2={5}
        y2={4.5}
        stroke={stroke}
        strokeWidth={1.2}
        strokeLinecap="round"
      />
      <Line
        x1={11}
        y1={1.5}
        x2={11}
        y2={4.5}
        stroke={stroke}
        strokeWidth={1.2}
        strokeLinecap="round"
      />
      <Line x1={2} y1={6.8} x2={14} y2={6.8} stroke={stroke} strokeWidth={1} />
    </Svg>
  );
}

function LeadIcon({
  entry,
  mutedFg,
}: {
  entry: TimelineEntry;
  mutedFg: string;
}) {
  const details = (entry.details ?? {}) as Record<string, string>;
  if (entry.action === "status_changed" && details.to) {
    return <StatusIcon status={details.to as IssueStatus} size={14} />;
  }
  if (entry.action === "priority_changed" && details.to) {
    return <PriorityIcon priority={details.to as IssuePriority} size={14} />;
  }
  if (
    entry.action === "due_date_changed" ||
    entry.action === "start_date_changed"
  ) {
    return <CalendarGlyph size={14} stroke={mutedFg} />;
  }
  return (
    <ActorAvatar
      type={entry.actor_type as "member" | "agent"}
      id={entry.actor_id}
      size={16}
    />
  );
}

export function ActivityRow({ entry }: { entry: TimelineEntry }) {
  const { getName } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;
  const resolveName = (
    type: string | null | undefined,
    id: string | null | undefined,
  ): string =>
    getName(type as "member" | "agent" | null | undefined, id);
  const actorName = resolveName(entry.actor_type, entry.actor_id);
  const verb = formatActivity(entry, resolveName);
  const showCoalesceBadge =
    (entry.coalesced_count ?? 1) > 1 &&
    entry.action !== "task_completed" &&
    entry.action !== "task_failed";

  return (
    <View className="flex-row items-center px-4 gap-2">
      <View className="w-4 items-center justify-center shrink-0">
        <LeadIcon entry={entry} mutedFg={mutedFg} />
      </View>
      <Text
        className="text-xs text-muted-foreground flex-1"
        numberOfLines={1}
      >
        <Text className="text-xs text-muted-foreground font-medium">
          {actorName}
        </Text>
        {verb ? (
          <Text className="text-xs text-muted-foreground"> {verb}</Text>
        ) : null}
      </Text>
      {showCoalesceBadge ? (
        <View className="bg-muted rounded px-1.5 py-0.5 shrink-0">
          <Text className="text-xs font-medium text-muted-foreground tabular-nums">
            ×{entry.coalesced_count}
          </Text>
        </View>
      ) : null}
      <Text className="text-xs text-muted-foreground shrink-0">
        {timeAgo(entry.created_at)}
      </Text>
    </View>
  );
}
