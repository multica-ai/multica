/**
 * HarmonyOS port of apps/mobile/components/issue/activity-row.tsx —
 * activity (non-comment) timeline row. Mirrors the visual contract of web's
 * issue-detail.tsx:1046-1100:
 *
 *   1. Single-line layout — `[lead icon] [actor name] [verb…truncate] [×N badge?] [time→]`
 *   2. Contextual lead icon by action:
 *        - status_changed   → StatusIcon for the NEW status (details.to)
 *        - priority_changed → PriorityIcon for the NEW priority (details.to)
 *        - due_date_changed / start_date_changed → calendar glyph
 *        - everything else  → small ActorAvatar (size 16)
 *   3. Whole row is `text-xs text-muted-foreground` (web parity). Actor name
 *      is `font-medium` but inherits the muted color — activity is supposed
 *      to feel quiet next to comment bubbles.
 *   4. Time is relative, right-aligned. Web shows absolute time in a hover
 *      tooltip; mobile has no hover so we rely on relative for v1.
 *   5. Coalesce ×N chip when `coalesced_count > 1`, except `task_completed` /
 *      `task_failed` which already bake the count into their phrase.
 *
 * The iOS CalendarGlyph draws with react-native-svg, which renders blank on
 * this platform (AGENTS.md); the same 16×16 geometry is expressed with
 * bordered/rounded Views (same trick as components/ui/status-icon.tsx).
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import type { IssuePriority, TimelineEntry } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { formatActivity } from "@/lib/format-activity";
import { timeAgo } from "@/lib/time-ago";
import { useActorLookup } from "@/data/use-actor-name";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import type { IssueStatusCatalog } from "@/lib/issue-status";
import { useThemeColors } from "@/lib/use-theme-colors";

/** 16×16 calendar outline: rounded rect body + two top tabs + header rule.
 *  Unit constants mirror the iOS SVG viewBox. */
function CalendarGlyph({ size = 14, stroke }: { size?: number; stroke: string }) {
  const k = size / 16;
  const s = (v: number) => v * k;
  return (
    <View style={{ width: size, height: size }}>
      {/* Body: rect(2, 3.5, 12, 10.5, rx 1.5) stroked 1.2 */}
      <View
        style={{
          position: "absolute",
          left: s(2),
          top: s(3.5),
          width: s(12),
          height: s(10.5),
          borderRadius: s(1.5),
          borderWidth: Math.max(StyleSheet.hairlineWidth, s(1.2)),
          borderColor: stroke,
        }}
      />
      {/* Left tab: line (5, 1.5)→(5, 4.5) */}
      <View
        style={{
          position: "absolute",
          left: s(5) - s(0.6),
          top: s(1.5),
          width: Math.max(StyleSheet.hairlineWidth, s(1.2)),
          height: s(3),
          borderRadius: s(0.6),
          backgroundColor: stroke,
        }}
      />
      {/* Right tab: line (11, 1.5)→(11, 4.5) */}
      <View
        style={{
          position: "absolute",
          left: s(11) - s(0.6),
          top: s(1.5),
          width: Math.max(StyleSheet.hairlineWidth, s(1.2)),
          height: s(3),
          borderRadius: s(0.6),
          backgroundColor: stroke,
        }}
      />
      {/* Header rule: line (2, 6.8)→(14, 6.8) */}
      <View
        style={{
          position: "absolute",
          left: s(2),
          top: s(6.8) - s(0.5),
          width: s(12),
          height: Math.max(StyleSheet.hairlineWidth, s(1)),
          backgroundColor: stroke,
        }}
      />
    </View>
  );
}

function LeadIcon({
  entry,
  catalog,
  mutedFg,
}: {
  entry: TimelineEntry;
  catalog: IssueStatusCatalog;
  mutedFg: string;
}) {
  const details = (entry.details ?? {}) as Record<string, string>;
  if (entry.action === "status_changed" && details.to) {
    // `details.to` is a status KEY: the glyph comes from its category and the
    // colour from the catalog, so a custom status is recognizable. (MUL-6243)
    return (
      <StatusIcon
        status={details.to}
        category={catalog.categoryOf(details.to)}
        icon={catalog.iconOf(details.to)}
        color={catalog.colorOf(details.to)}
        size={14}
      />
    );
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
      name={entry.actor_name}
      avatarUrl={entry.actor_avatar_url}
      size={16}
    />
  );
}

export function ActivityRow({ entry }: { entry: TimelineEntry }) {
  const { getName } = useActorLookup();
  const catalog = useIssueStatuses();
  const c = useThemeColors();
  const resolveName = (
    type: string | null | undefined,
    id: string | null | undefined,
  ): string => getName(type as "member" | "agent" | null | undefined, id);
  const actorName =
    entry.actor_name || resolveName(entry.actor_type, entry.actor_id);
  const verb = formatActivity(entry, resolveName, catalog.labelOf);
  const showCoalesceBadge =
    (entry.coalesced_count ?? 1) > 1 &&
    entry.action !== "task_completed" &&
    entry.action !== "task_failed";

  return (
    <View style={styles.row}>
      <View style={styles.iconSlot}>
        <LeadIcon entry={entry} catalog={catalog} mutedFg={c.mutedForeground} />
      </View>
      <Text
        style={[styles.text, { color: c.mutedForeground }]}
        numberOfLines={1}
      >
        <Text style={[styles.text, styles.name, { color: c.mutedForeground }]}>
          {actorName}
        </Text>
        {verb ? (
          <Text style={[styles.text, { color: c.mutedForeground }]}> {verb}</Text>
        ) : null}
      </Text>
      {showCoalesceBadge ? (
        <View style={[styles.badge, { backgroundColor: c.muted }]}>
          <Text style={[styles.badgeText, { color: c.mutedForeground }]}>
            ×{entry.coalesced_count}
          </Text>
        </View>
      ) : null}
      <Text style={[styles.text, styles.time, { color: c.mutedForeground }]}>
        {timeAgo(entry.created_at)}
      </Text>
    </View>
  );
}

// flex-row items-center px-4 gap-2
const styles = StyleSheet.create({
  row: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 16,
    gap: 8,
  },
  // w-4 items-center justify-center
  iconSlot: { width: 16, alignItems: "center", justifyContent: "center" },
  // text-xs
  text: { fontSize: 12 },
  // font-medium
  name: { fontWeight: "500" },
  // bg-muted rounded-xs px-1.5 py-0.5
  badge: {
    borderRadius: 2,
    paddingHorizontal: 6,
    paddingVertical: 2,
  },
  // text-xs font-medium tabular-nums
  badgeText: { fontSize: 12, fontWeight: "500", fontVariant: ["tabular-nums"] },
  // shrink-0
  time: { flexShrink: 0 },
});
