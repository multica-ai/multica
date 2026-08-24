/**
 * Shared issue row used by every list-style issue surface on mobile —
 * (tabs)/my-issues, more/issues (workspace-wide), and project detail's
 * related-issues bucket.
 *
 * Layout mirrors web's `packages/views/issues/components/list-row.tsx`:
 *   [status?]  priority  identifier  title  …  assignee
 *
 * `showStatus` is opt-in because the grouped lists already carry the status
 * CATEGORY in their section header (rendering the glyph again per-row would be
 * visual noise). Ungrouped surfaces that mix statuses — the pins list — ask for
 * it. New callers should default to false unless they mix multiple statuses
 * inside a single ungrouped list.
 *
 * The `<CustomStatusChip>` is NOT gated on `showStatus`: a section header names
 * a category, so "Code Review" and "QA" both land under In Review and the row
 * is the only place left to tell them apart. It stays silent for built-in
 * statuses, so a workspace without custom statuses renders exactly as before.
 * (MUL-6243)
 *
 * Behavioral parity:
 *   - Same `Issue` type, same `assignee_type`/`assignee_id` semantics
 *     (root CLAUDE.md "Data identity must agree").
 *   - Mirrors web `packages/views/issues/components/list-row.tsx:52`:
 *     render the assignee whenever `assignee_type && assignee_id` are both
 *     truthy — `ActorAvatar` itself handles member / agent / squad rendering
 *     (rounded square + people glyph or `squad.avatar_url` for squads). A
 *     future fourth enum value falls through to ActorAvatar's initials
 *     fallback, which is the real "enum drift downgrades, not crashes"
 *     behavior — earlier whitelist (member/agent only) silently dropped
 *     squad assignees instead.
 */
import { Pressable, View } from "react-native";
import type { Issue } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { StatusIcon } from "@/components/ui/status-icon";
import { CustomStatusChip } from "@/components/issue/custom-status-chip";
import { issueColumnCategory } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";

interface Props {
  issue: Issue;
  onPress: () => void;
  /** Render the status icon inline at the start of the row. Default: false. */
  showStatus?: boolean;
}

export function IssueRow({ issue, onPress, showStatus = false }: Props) {
  // One catalog read for both the icon's colour and the chip — see the
  // divergence note in `custom-status-chip.tsx`.
  const catalog = useIssueStatuses();
  return (
    <Pressable onPress={onPress} className="active:bg-secondary px-4 py-3">
      <View className="flex-row items-center gap-3">
        {/* The glyph is per CATEGORY, so a custom status draws its category's
            icon rather than falling back to Todo's; the colour is what tells
            two statuses of one category apart. (MUL-6243) */}
        {showStatus ? (
          <StatusIcon
            status={issue.status}
            category={issueColumnCategory(issue)}
            color={catalog.colorOf(issue.status)}
            size={14}
          />
        ) : null}
        <PriorityIcon priority={issue.priority} size={14} />
        <Text className="text-xs text-muted-foreground shrink-0 w-16">
          {issue.identifier}
        </Text>
        <View className="flex-1 flex-row items-center gap-1.5 min-w-0">
          <Text className="text-sm text-foreground shrink" numberOfLines={1}>
            {issue.title}
          </Text>
          <CustomStatusChip status={issue.status} catalog={catalog} />
        </View>
        {issue.assignee_type && issue.assignee_id ? (
          <ActorAvatar
            type={issue.assignee_type}
            id={issue.assignee_id}
            size={20}
            showPresence
          />
        ) : null}
      </View>
    </Pressable>
  );
}
