/**
 * Mobile InboxDetailLabel — type-aware second-line for inbox rows.
 *
 * HarmonyOS port of apps/mobile/components/inbox/detail-label.tsx. Mirrors
 * packages/views/inbox/components/inbox-detail-label.tsx exactly: for each
 * InboxItemType the user sees the same label they would see on web/desktop.
 * This is a Behavioral parity concern — if web shows "Set status to ✓ Done",
 * mobile must show "Set status to ✓ Done" (rendered with mobile primitives,
 * not the literal HTML).
 *
 * Web is i18n-driven (useT). Mobile v1 is English-only; when mobile ships
 * i18n, mirror the namespace structure.
 *
 * Platform deltas from the iOS file (RNOH 0.82 — see AGENTS.md):
 *   - NativeWind className styling is replaced by explicit StyleSheet styles.
 *     The iOS caller passed the read/unread dimming as a `className` prop
 *     (text-muted-foreground vs text-muted-foreground/60); here that is the
 *     `dimmed` boolean, resolved against the theme token with withAlpha().
 *     As on iOS, the dim only reaches the single-string case — the two icon
 *     rows hardcode their label color (the iOS className landed on a View,
 *     which cannot take a text color, so those rows never dimmed).
 */
import { StyleSheet, View, type TextStyle } from "react-native";
import type {
  InboxItem,
  InboxItemType,
  IssuePriority,
} from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { useActorLookup } from "@/data/use-actor-name";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { useThemeColors } from "@/lib/use-theme-colors";
import { withAlpha, type ThemeColors } from "@/lib/theme";

// Mirrors PRIORITY_CONFIG.label in packages/core/issues/config/priority.ts
const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: "Urgent",
  high: "High",
  medium: "Medium",
  low: "Low",
  none: "No priority",
};

// Mirrors useTypeLabels in packages/views/inbox/components/inbox-detail-label.tsx
const TYPE_LABEL: Record<InboxItemType, string> = {
  issue_assigned: "Assigned",
  issue_subscribed: "Subscribed",
  unassigned: "Unassigned",
  assignee_changed: "Reassigned",
  status_changed: "Status changed",
  priority_changed: "Priority changed",
  start_date_changed: "Start date changed",
  due_date_changed: "Due date changed",
  new_comment: "New comment",
  mentioned: "Mentioned",
  review_requested: "Review requested",
  task_completed: "Task completed",
  task_failed: "Task failed",
  agent_blocked: "Agent blocked",
  agent_completed: "Agent completed",
  reaction_added: "Reaction added",
  quick_create_done: "Quick-create done",
  quick_create_failed: "Quick-create failed",
  quick_create_unconfirmed: "Quick-create needs a check",
  autopilot_paused: "Autopilot paused",
  autopilot_quota_exceeded: "Autopilot run limit reached",
  children_done: "Sub-issues finished",
};

// due_date is a calendar day — format timezone-safely (no offset day shift).
function shortDate(dateStr: string): string {
  return formatDateOnly(dateStr, { month: "short", day: "numeric" }, "en-US");
}

function singleLine(value: string | null | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

export function InboxDetailLabel({
  item,
  dimmed = false,
}: {
  item: InboxItem;
  /** Read rows dim to muted-foreground/60, mirroring the iOS className. */
  dimmed?: boolean;
}) {
  const c = useThemeColors();
  const s = styles(c);
  const { getName } = useActorLookup();
  // `details.to` is a status KEY and may be a custom one, so its name, colour
  // and glyph all resolve through the workspace catalog. (MUL-6243)
  const { categoryOf, colorOf, labelOf, iconOf } = useIssueStatuses();
  const details = item.details ?? {};
  const dimColor = withAlpha(c.mutedForeground, 0.6);

  // Cases with inline icons → Row layout.
  if (item.type === "status_changed" && details.to) {
    const status = details.to;
    return (
      <View style={s.iconRow}>
        <Text style={s.muted}>Set status to</Text>
        <StatusIcon
          status={status}
          category={categoryOf(status)}
          icon={iconOf(status)}
          color={colorOf(status)}
          size={12}
        />
        <Text style={s.muted} numberOfLines={1}>
          {labelOf(status)}
        </Text>
      </View>
    );
  }

  if (item.type === "priority_changed" && details.to) {
    const priority = details.to as IssuePriority;
    return (
      <View style={s.iconRow}>
        <Text style={s.muted}>Set priority to</Text>
        <PriorityIcon priority={priority} size={12} />
        <Text style={s.muted} numberOfLines={1}>
          {PRIORITY_LABEL[priority] ?? priority}
        </Text>
      </View>
    );
  }

  // Single-string cases.
  const text = (() => {
    switch (item.type) {
      case "issue_assigned":
      case "assignee_changed":
        if (details.new_assignee_id) {
          const name = getName(
            (details.new_assignee_type ?? "member") as "member" | "agent",
            details.new_assignee_id,
          );
          return `Assigned to ${name}`;
        }
        return TYPE_LABEL[item.type];
      case "unassigned":
        return "Removed assignee";
      case "due_date_changed":
        return details.to
          ? `Set due date to ${shortDate(details.to)}`
          : "Removed due date";
      case "new_comment":
        return singleLine(item.body) || TYPE_LABEL[item.type];
      case "reaction_added":
        return details.emoji
          ? `Reacted with ${details.emoji}`
          : TYPE_LABEL[item.type];
      case "quick_create_done":
        return details.identifier
          ? `Created with agent: ${details.identifier}`
          : TYPE_LABEL[item.type];
      case "quick_create_failed": {
        const detail = singleLine(details.error) || singleLine(item.body);
        return detail ? `Failed: ${detail}` : TYPE_LABEL[item.type];
      }
      // Mirrors packages/views/inbox/components/inbox-detail-label.tsx: the
      // unconfirmed outcome deliberately drops the "Failed:" prefix, because
      // the issue may actually have been created.
      case "quick_create_unconfirmed": {
        const detail = singleLine(details.error) || singleLine(item.body);
        return detail || TYPE_LABEL[item.type];
      }
      case "autopilot_quota_exceeded":
        return "Run blocked because the limit was reached";
      case "children_done": {
        // The stage arrives as a JSON number; details are typed as strings.
        const stage = details.stage != null ? String(details.stage) : "";
        if (stage) return `Stage ${stage}'s sub-issues finished`;
        return TYPE_LABEL[item.type];
      }
      default:
        return TYPE_LABEL[item.type] ?? item.type;
    }
  })();

  return (
    <Text
      style={[s.muted, dimmed && { color: dimColor }]}
      numberOfLines={1}
    >
      {text}
    </Text>
  );
}

const styles = (c: ThemeColors) =>
  StyleSheet.create({
    // text-xs text-muted-foreground (the base the iOS className merged onto)
    muted: { fontSize: 12, color: c.mutedForeground } as TextStyle,
    // flex-row items-center gap-1
    iconRow: { flexDirection: "row", alignItems: "center", gap: 4 },
  });
