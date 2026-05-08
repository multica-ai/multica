import { Text, View } from "react-native";
import type {
  TimelineEntry,
  IssueStatus,
  IssuePriority,
} from "@multica/core/types";
import { STATUS_CONFIG } from "@multica/core/issues/config/status";
import { PRIORITY_CONFIG } from "@multica/core/issues/config/priority";
import { useActorName } from "@multica/core/workspace/hooks";

import { ActorAvatar } from "@/components/ui/actor-avatar";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { IconSymbol } from "@/components/ui/icon-symbol";
import { useTimeAgo } from "@/lib/use-time-ago";

// One activity row in the timeline. Visual contract mirrors web's inline
// activity rendering in `packages/views/issues/components/issue-detail.tsx`:
//
//   <lead icon> <bold actor> <sentence> [×N badge] <timeAgo>
//
// `lead icon` is the *target's* icon when the action carries a target identity:
//   status_changed   → StatusIcon(details.to)
//   priority_changed → PriorityIcon(details.to)
//   due_date_changed → calendar SF symbol
//   anything else    → ActorAvatar (16px)
//
// This replaces the v1 "open circle + sentence" layout, which gave every
// activity the same visual weight regardless of meaning.
//
// Coalescing happens upstream in CommentList; we just render `coalesced_count`
// as an "×N" badge when present (and not already baked into the sentence by
// task_completed / task_failed).
export function ActivityRow({ entry }: { entry: TimelineEntry }) {
  const { getActorName } = useActorName();
  const timeAgo = useTimeAgo(entry.created_at);
  const actor = getActorName(entry.actor_type, entry.actor_id);
  const details = (entry.details ?? {}) as Record<string, string>;
  const action = entry.action ?? "";

  let leadIcon: React.ReactNode;
  if (action === "status_changed" && details.to) {
    leadIcon = <StatusIcon status={details.to as IssueStatus} size={14} />;
  } else if (action === "priority_changed" && details.to) {
    leadIcon = (
      <PriorityIcon priority={details.to as IssuePriority} size={14} />
    );
  } else if (action === "due_date_changed") {
    leadIcon = (
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      <IconSymbol
        name={"calendar" as any}
        size={14}
        color="hsl(240 4% 46%)"
      />
    );
  } else {
    leadIcon = (
      <ActorAvatar
        type={entry.actor_type as "member" | "agent"}
        id={entry.actor_id}
        size={16}
      />
    );
  }

  const sentence = formatSentence(entry, details, getActorName);
  const coalesce = entry.coalesced_count ?? 1;
  const showBadge =
    coalesce > 1 && action !== "task_completed" && action !== "task_failed";

  return (
    <View className="flex-row items-center gap-2 px-4 py-1.5">
      <View className="w-4 items-center justify-center">{leadIcon}</View>
      <Text
        className="text-muted-foreground text-xs flex-1"
        numberOfLines={2}
      >
        <Text className="text-foreground font-medium">{actor}</Text>
        <Text> {sentence}</Text>
      </Text>
      {showBadge && (
        <View className="px-1.5 py-0.5 rounded bg-muted">
          <Text className="text-muted-foreground text-xs font-medium">
            ×{coalesce}
          </Text>
        </View>
      )}
      <Text className="text-muted-foreground text-xs shrink-0">{timeAgo}</Text>
    </View>
  );
}

// Turn an activity entry into the sentence half (the bold actor name is
// rendered separately above). Mirrors web's formatActivity but without an i18n
// dependency — mobile is English-only in v1.
//
// PARITY HAZARD (see apps/mobile/CLAUDE.md): this is a re-implementation of
// `formatActivity` from packages/views/issues/components/issue-detail.tsx.
// The action-coverage list MUST stay a superset of web's. Extract to
// @multica/core/issues/timeline-format once mobile ships i18n.
function formatSentence(
  entry: TimelineEntry,
  details: Record<string, string>,
  resolveActorName: (type: string, id: string) => string,
): string {
  const action = entry.action ?? "";
  const count = entry.coalesced_count ?? 1;
  switch (action) {
    case "issue_created":
    case "created":
      return "created the issue";
    case "status_changed":
    case "status_change": {
      const to = details.to as IssueStatus | undefined;
      const label = to ? STATUS_CONFIG[to]?.label : undefined;
      return `set status to ${label ?? to ?? "?"}`;
    }
    case "priority_changed":
    case "priority_change": {
      const to = details.to as IssuePriority | undefined;
      const label = to ? PRIORITY_CONFIG[to]?.label : undefined;
      return `set priority to ${label ?? to ?? "?"}`;
    }
    case "assignee_changed":
    case "assignee_change": {
      const isSelfAssign =
        details.to_type === entry.actor_type && details.to_id === entry.actor_id;
      if (isSelfAssign) return "self-assigned";
      const toName =
        details.to_id && details.to_type
          ? resolveActorName(details.to_type, details.to_id)
          : null;
      if (toName) return `assigned to ${toName}`;
      if (details.from_id && !details.to_id) return "removed the assignee";
      return "changed the assignee";
    }
    case "due_date_changed":
    case "due_date_change": {
      if (!details.to) return "removed the due date";
      const formatted = new Date(details.to).toLocaleDateString("en-US", {
        month: "short",
        day: "numeric",
      });
      return `set due date to ${formatted}`;
    }
    case "title_changed":
      return "renamed the issue";
    case "description_updated":
      return "updated the description";
    case "labels_changed":
    case "labels_change":
      return "updated labels";
    case "linked_issue":
      return "linked an issue";
    case "task_completed":
      return count > 1 ? `completed ${count} tasks` : "completed a task";
    case "task_failed":
      return count > 1 ? `${count} tasks failed` : "a task failed";
    default:
      return action ? action.replace(/_/g, " ") : "performed an action";
  }
}
