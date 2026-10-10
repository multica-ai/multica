/**
 * Inbox row content — the visual half, no gesture wrapping. HarmonyOS port
 * of apps/mobile/components/inbox/inbox-row.tsx; kept purely presentational
 * — the swipe and the press behaviour live in the wrapper.
 *
 * Visual structure mirrors web's InboxListItem
 * (packages/views/inbox/components/inbox-list-item.tsx). Per
 * apps/mobile/CLAUDE.md "Visual alignment is baseline":
 *   - Right column stacks vertically: status icon on top row, time on bottom.
 *   - Secondary line uses the type-aware `InboxDetailLabel`, not raw body.
 *
 * Platform deltas from the iOS file (RNOH 0.82 — see AGENTS.md):
 *   - NativeWind classes are explicit StyleSheet styles (p-4=16, py-3=12,
 *     gap-3=12, gap-2=8, gap-1.5=6, size-1.5=6, text-sm=14, text-xs=12,
 *     mt-0.5=2); the read-state dimming is text-muted-foreground/60 through
 *     withAlpha().
 *   - ActorAvatar has no directory lookup here: the member/agent name and
 *     avatar resolve through the ported useActorLookup; `system` actors keep
 *     the avatar's built-in glyph. The `showPresence` overlay is dropped —
 *     live presence is not wired on this platform yet.
 */
import { StyleSheet, View } from "react-native";
import { Pressable } from "@/components/ui/pressable";
import type { InboxItem } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { StatusIcon } from "@/components/ui/status-icon";
import { InboxDetailLabel } from "@/components/inbox/detail-label";
import { getInboxDisplayTitle } from "@/lib/inbox-display";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { timeAgo } from "@/lib/time-ago";
import { withAlpha, type ThemeColors } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";
import { useActorLookup } from "@/data/use-actor-name";

interface Props {
  item: InboxItem;
  onPress: () => void;
}

export function InboxRow({ item, onPress }: Props) {
  const c = useThemeColors();
  const s = styles(c);
  const { getName, getAvatarUrl } = useActorLookup();
  const isUnread = !item.read;
  const { categoryOf, colorOf, iconOf } = useIssueStatuses();
  const displayTitle = getInboxDisplayTitle(item);
  const actorType = item.actor_type ?? item.recipient_type;
  const actorId = item.actor_id ?? item.recipient_id;
  // Directory lookup is explicit on this platform; `system` actors render the
  // avatar's built-in settings glyph and never hit the member/agent lists.
  const avatarName =
    actorType === "system" ? undefined : getName(actorType, actorId);
  const avatarUrl =
    actorType === "system" ? null : getAvatarUrl(actorType, actorId);
  const dimColor = withAlpha(c.mutedForeground, 0.6);

  return (
    <Pressable
      onPress={onPress}
      style={({ pressed }) => [
        s.row,
        { backgroundColor: pressed ? c.secondary : c.background },
      ]}
    >
      <View style={s.layout}>
        <ActorAvatar
          type={actorType}
          id={actorId}
          name={avatarName}
          avatarUrl={avatarUrl}
          size={36}
        />
        <View style={s.column}>
          {/* Top row: [unread dot + title] (left) | [status icon] (right) */}
          <View style={s.topRow}>
            <View style={s.titleCluster}>
              {isUnread ? <View style={s.unreadDot} /> : null}
              <Text
                style={[
                  s.title,
                  {
                    color: isUnread ? c.foreground : c.mutedForeground,
                    fontWeight: isUnread ? "500" : "400",
                  },
                ]}
                numberOfLines={1}
              >
                {displayTitle}
              </Text>
            </View>
            {/* The glyph is per category, so it alone cannot tell "In Review"
                from a custom "Human Review" — a move between two statuses of
                one category would leave this row pixel-identical and read as
                "the inbox never updated" (MUL-6395). Colour is what carries a
                custom status's identity; `colorOf` is null for a built-in,
                which keeps it on its category token. */}
            {item.issue_status ? (
              <StatusIcon
                status={item.issue_status}
                category={categoryOf(item.issue_status)}
                icon={iconOf(item.issue_status)}
                color={colorOf(item.issue_status)}
                size={14}
              />
            ) : null}
          </View>
          {/* Bottom row: [type-aware detail label] (left) | [time] (right).
              Detail label mirrors web InboxDetailLabel — same per-type
              wording (Mentioned / Set status to ... / Assigned to ... / etc),
              not the raw markdown body. */}
          <View style={s.bottomRow}>
            <View style={s.detailWrap}>
              <InboxDetailLabel item={item} dimmed={!isUnread} />
            </View>
            <Text
              style={[
                s.time,
                { color: isUnread ? c.mutedForeground : dimColor },
              ]}
            >
              {timeAgo(item.created_at)}
            </Text>
          </View>
        </View>
      </View>
    </Pressable>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    // bg-background active:bg-secondary px-4 py-3
    row: { paddingHorizontal: 16, paddingVertical: 12 },
    // flex-row gap-3
    layout: { flexDirection: "row", gap: 12 },
    // flex-1 min-w-0
    column: { flex: 1, minWidth: 0 },
    // flex-row items-center gap-2
    topRow: { flexDirection: "row", alignItems: "center", gap: 8 },
    // flex-row items-center gap-1.5 flex-1 min-w-0
    titleCluster: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      flex: 1,
      minWidth: 0,
    },
    // size-1.5 rounded-full bg-brand shrink-0
    unreadDot: {
      width: 6,
      height: 6,
      borderRadius: 3,
      backgroundColor: c.brand,
      flexShrink: 0,
    },
    // flex-1 text-sm
    title: { fontSize: 14, flex: 1 },
    // flex-row items-center gap-2 mt-0.5
    bottomRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      marginTop: 2,
    },
    // flex-1 min-w-0
    detailWrap: { flex: 1, minWidth: 0 },
    // text-xs shrink-0
    time: { fontSize: 12, flexShrink: 0 },
  });
