/**
 * Mobile InboxDetailLabel — type-aware second-line for inbox rows.
 *
 * Mirrors packages/views/inbox/components/inbox-detail-label.tsx exactly:
 * for each InboxItemType the user sees the same label they would see on
 * web/desktop. This is a Behavioral parity concern — if web shows "Set
 * status to ✓ Done", mobile must show "Set status to ✓ Done" (rendered
 * with mobile primitives, not the literal HTML).
 *
 * i18n-driven via the "inbox" namespace (locales/en/inbox.json,
 * locales/zh-Hans/inbox.json) — mirrors the namespace structure web uses.
 */
import { View } from "react-native";
import { useTranslation } from "react-i18next";
import type {
  InboxItem,
  InboxItemType,
  IssueStatus,
  IssuePriority,
} from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { useActorLookup } from "@/data/use-actor-name";
import { cn } from "@/lib/utils";

// due_date is a calendar day — format timezone-safely (no offset day shift).
function shortDate(dateStr: string): string {
  return formatDateOnly(dateStr, { month: "short", day: "numeric" }, "en-US");
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
  const { t } = useTranslation("inbox");
  const details = item.details ?? {};

  const statusLabel = (status: IssueStatus) =>
    t(`status_label.${status}`, { defaultValue: status });
  const priorityLabel = (priority: IssuePriority) =>
    t(`priority_label.${priority}`, { defaultValue: priority });
  const typeLabel = (type: InboxItemType) =>
    t(`type_label.${type}`, { defaultValue: type });

  // Cases with inline icons → Row layout.
  if (item.type === "status_changed" && details.to) {
    const status = details.to as IssueStatus;
    return (
      <View className={cn("flex-row items-center gap-1", className)}>
        <Text className="text-xs text-muted-foreground">
          {t("detail.set_status_to")}
        </Text>
        <StatusIcon status={status} size={12} />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {statusLabel(status)}
        </Text>
      </View>
    );
  }

  if (item.type === "priority_changed" && details.to) {
    const priority = details.to as IssuePriority;
    return (
      <View className={cn("flex-row items-center gap-1", className)}>
        <Text className="text-xs text-muted-foreground">
          {t("detail.set_priority_to")}
        </Text>
        <PriorityIcon priority={priority} size={12} />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {priorityLabel(priority)}
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
          return t("detail.assigned_to", { name });
        }
        return typeLabel(item.type);
      case "unassigned":
        return t("detail.removed_assignee");
      case "due_date_changed":
        return details.to
          ? t("detail.set_due_date_to", { date: shortDate(details.to) })
          : t("detail.removed_due_date");
      case "new_comment":
        return singleLine(item.body) || typeLabel(item.type);
      case "reaction_added":
        return details.emoji
          ? t("detail.reacted_with", { emoji: details.emoji })
          : typeLabel(item.type);
      case "quick_create_done":
        return details.identifier
          ? t("detail.created_with_agent", { identifier: details.identifier })
          : typeLabel(item.type);
      case "quick_create_failed": {
        const detail = singleLine(details.error) || singleLine(item.body);
        return detail
          ? t("detail.failed_with_detail", { detail })
          : typeLabel(item.type);
      }
      default:
        return typeLabel(item.type);
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
