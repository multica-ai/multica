/**
 * Mobile InboxDetailLabel — type-aware second-line for inbox rows.
 *
 * Mirrors packages/views/inbox/components/inbox-detail-label.tsx exactly:
 * for each InboxItemType the user sees the same label they would see on
 * web/desktop. This is a Behavioral parity concern — if web shows "Set
 * status to ✓ Done", mobile must show "Set status to ✓ Done" (rendered
 * with mobile primitives, not the literal HTML).
 *
 * Web is i18n-driven (useT). Mobile v1 is English-only; when mobile ships
 * i18n, mirror the namespace structure.
 */
import { View } from "react-native";
import type {
  InboxItem,
  InboxItemType,
  IssueStatus,
  IssuePriority,
} from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { useActorLookup } from "@/data/use-actor-name";
import { cn } from "@/lib/utils";
import { getInboxStringDetail } from "@/lib/inbox-display";

// Mirrors STATUS_CONFIG.label in packages/core/issues/config/status.ts
const STATUS_LABEL: Record<IssueStatus, string> = {
  backlog: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  done: "Done",
  blocked: "Blocked",
  cancelled: "Cancelled",
};

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
  agent_draft_done: "Agent draft ready",
  agent_draft_failed: "Agent draft failed",
  skill_find_done: "Skill recommendations ready",
  skill_find_failed: "Skill finder failed",
};

function shortDate(dateStr: string): string {
  if (!dateStr) return "";
  return new Date(dateStr).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
  });
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
  const detail = (key: string) => getInboxStringDetail(item, key);

  // Cases with inline icons → Row layout.
  if (item.type === "status_changed" && detail("to")) {
    const status = detail("to") as IssueStatus;
    return (
      <View className={cn("flex-row items-center gap-1", className)}>
        <Text className="text-xs text-muted-foreground">Set status to</Text>
        <StatusIcon status={status} size={12} />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {STATUS_LABEL[status] ?? status}
        </Text>
      </View>
    );
  }

  if (item.type === "priority_changed" && detail("to")) {
    const priority = detail("to") as IssuePriority;
    return (
      <View className={cn("flex-row items-center gap-1", className)}>
        <Text className="text-xs text-muted-foreground">Set priority to</Text>
        <PriorityIcon priority={priority} size={12} />
        <Text className="text-xs text-muted-foreground" numberOfLines={1}>
          {PRIORITY_LABEL[priority] ?? priority}
        </Text>
      </View>
    );
  }

  // Single-string cases.
  const text = (() => {
    switch (item.type) {
      case "issue_assigned":
      case "assignee_changed": {
        const assigneeID = detail("new_assignee_id");
        if (assigneeID) {
          const name = getName(
            (detail("new_assignee_type") || "member") as "member" | "agent",
            assigneeID,
          );
          return `Assigned to ${name}`;
        }
        return TYPE_LABEL[item.type];
      }
      case "unassigned":
        return "Removed assignee";
      case "due_date_changed": {
        const to = detail("to");
        return to
          ? `Set due date to ${shortDate(to)}`
          : "Removed due date";
      }
      case "new_comment":
        return singleLine(item.body) || TYPE_LABEL[item.type];
      case "reaction_added": {
        const emoji = detail("emoji");
        return emoji
          ? `Reacted with ${emoji}`
          : TYPE_LABEL[item.type];
      }
      case "quick_create_done": {
        const identifier = detail("identifier");
        return identifier
          ? `Created with agent: ${identifier}`
          : TYPE_LABEL[item.type];
      }
      case "quick_create_failed": {
        const failureDetail = singleLine(detail("error")) || singleLine(item.body);
        return failureDetail ? `Failed: ${failureDetail}` : TYPE_LABEL[item.type];
      }
      default:
        return TYPE_LABEL[item.type] ?? item.type;
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
