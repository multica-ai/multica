/**
 * Mobile InboxDetailLabel — type-aware second-line for inbox rows.
 *
 * Mirrors packages/views/inbox/components/inbox-detail-label.tsx exactly:
 * for each InboxItemType the user sees the same label they would see on
 * web/desktop. This is a Behavioral parity concern — if web shows "Set
 * status to ✓ Done", mobile must show "Set status to ✓ Done" (rendered
 * with mobile primitives, not the literal HTML).
 *
 * Copy is i18n-driven and mirrors the web namespace structure.
 */
import { View } from "react-native";
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
import { i18n, useT } from "@/lib/i18n";
import { cn } from "@/lib/utils";

// Mirrors PRIORITY_CONFIG.label in packages/core/issues/config/priority.ts
const PRIORITY_LABEL: Record<IssuePriority, string> = {
  urgent: "issues:priority.urgent",
  high: "issues:priority.high",
  medium: "issues:priority.medium",
  low: "issues:priority.low",
  none: "issues:priority.none",
};

// Mirrors useTypeLabels in packages/views/inbox/components/inbox-detail-label.tsx
const TYPE_KEY: Record<InboxItemType, string> = {
  issue_assigned: "type.assigned",
  issue_subscribed: "type.subscribed",
  unassigned: "type.unassigned",
  assignee_changed: "type.reassigned",
  status_changed: "type.status_changed",
  priority_changed: "type.priority_changed",
  start_date_changed: "type.start_date_changed",
  due_date_changed: "type.due_date_changed",
  new_comment: "type.new_comment",
  mentioned: "type.mentioned",
  review_requested: "type.review_requested",
  task_completed: "type.task_completed",
  task_failed: "type.task_failed",
  agent_blocked: "type.agent_blocked",
  agent_completed: "type.agent_completed",
  reaction_added: "type.reaction_added",
  quick_create_done: "type.quick_create_done",
  quick_create_failed: "type.quick_create_failed",
  quick_create_unconfirmed: "type.quick_create_unconfirmed",
  autopilot_paused: "type.autopilot_paused",
  autopilot_quota_exceeded: "type.autopilot_quota_exceeded",
};

// due_date is a calendar day — format timezone-safely (no offset day shift).
function shortDate(dateStr: string): string {
  return formatDateOnly(
    dateStr,
    { month: "short", day: "numeric" },
    i18n.resolvedLanguage ?? i18n.language,
  );
}

function singleLine(value: string | null | undefined): string {
  return (value ?? "").replace(/\s+/g, " ").trim();
}

export function InboxDetailLabel({
  item,
  className,
}: {
  item: InboxItem;
  className?: string;
}) {
  const { getName } = useActorLookup();
  const { t } = useT("inbox");
  // `details.to` is a status KEY and may be a custom one, so its name, colour
  // and glyph all resolve through the workspace catalog. (MUL-6243)
  const { categoryOf, colorOf, labelOf, iconOf } = useIssueStatuses();
  const details = item.details ?? {};

  // Cases with inline icons → Row layout.
  if (item.type === "status_changed" && details.to) {
    const status = details.to;
    return (
      <View className={cn("flex-row items-center gap-1", className)}>
        <Text className="text-xs text-muted-foreground">
          {t("type.set_status")}
        </Text>
        <StatusIcon
          status={status}
          category={categoryOf(status)}
          icon={iconOf(status)} color={colorOf(status)}
          size={12}
        />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {labelOf(status)}
        </Text>
      </View>
    );
  }

  if (item.type === "priority_changed" && details.to) {
    const priority = details.to as IssuePriority;
    return (
      <View className={cn("flex-row items-center gap-1", className)}>
        <Text className="text-xs text-muted-foreground">
          {t("type.set_priority")}
        </Text>
        <PriorityIcon priority={priority} size={12} />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {t(PRIORITY_LABEL[priority])}
        </Text>
      </View>
    );
  }

  // Single-string cases.
  const text: string = (() => {
    switch (item.type) {
      case "issue_assigned":
      case "assignee_changed":
        if (details.new_assignee_id) {
          const name = getName(
            (details.new_assignee_type ?? "member") as "member" | "agent",
            details.new_assignee_id,
          );
          return t("type.assigned_to", { name });
        }
        return t(TYPE_KEY[item.type]);
      case "unassigned":
        return t("type.removed_assignee");
      case "due_date_changed":
        return details.to
          ? t("type.set_due_date", { date: shortDate(details.to) })
          : t("type.removed_due_date");
      case "new_comment":
        return singleLine(item.body) || t(TYPE_KEY[item.type]);
      case "reaction_added":
        return details.emoji
          ? t("type.reacted_with", { emoji: details.emoji })
          : t(TYPE_KEY[item.type]);
      case "quick_create_done":
        return details.identifier
          ? t("type.created_with_agent", { identifier: details.identifier })
          : t(TYPE_KEY[item.type]);
      case "quick_create_failed": {
        const detail = singleLine(details.error) || singleLine(item.body);
        return detail ? t("type.failed_with_detail", { detail }) : t(TYPE_KEY[item.type]);
      }
      // Mirrors packages/views/inbox/components/inbox-detail-label.tsx: the
      // unconfirmed outcome deliberately drops the "Failed:" prefix, because
      // the issue may actually have been created.
      case "quick_create_unconfirmed": {
        const detail = singleLine(details.error) || singleLine(item.body);
        return detail || t(TYPE_KEY[item.type]);
      }
      case "autopilot_quota_exceeded":
        return t("type.run_limit_blocked");
      default:
        return t(TYPE_KEY[item.type]) ?? item.type;
    }
  })();

  return (
    <Text
      className={cn("text-xs text-muted-foreground", className)}
      numberOfLines={1}
    >
      {text}
    </Text>
  );
}
