/**
 * HarmonyOS port of apps/mobile/components/issue/attribute-row.tsx —
 * issue-detail attribute chip row. Each editable attribute renders as a
 * tappable chip; tapping fires the `onOpenPicker` callback prop and the
 * host screen presents the matching picker sheet, which reads the issue
 * from the TanStack Query detail cache and fires its own mutation — no
 * onChange round-trip back here (same contract as the iOS route map).
 */
import React, { useMemo } from "react";
import { StyleSheet, View, type ViewStyle } from "react-native";
import { useQuery } from "@tanstack/react-query";
import type { Issue, IssuePriority } from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";
import { Text } from "@/components/ui/text";
import { StatusIcon } from "@/components/ui/status-icon";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { ProjectIcon } from "@/components/ui/project-icon";
import { AttributeChip } from "./attribute-chip";
import { useActorLookup } from "@/data/use-actor-name";
import { findProject, projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { PRIORITY_LABEL as PRIORITY_FULL_LABEL } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

// Chip placeholder shortens `none` from "No priority" → "Priority" so the
// unset chip reads as a placeholder, not as a confusing assigned value.
const PRIORITY_CHIP_LABEL: Record<IssuePriority, string> = {
  ...PRIORITY_FULL_LABEL,
  none: "Priority",
};

/** The picker fields the issue-detail attribute row can open. The host
 *  screen maps each field to its picker sheet (the iOS route map). */
export type IssuePickerField =
  | "status"
  | "priority"
  | "assignee"
  | "label"
  | "project"
  | "due-date";

// due_date is a calendar day — format timezone-safely so the day never shifts
// with the viewer's offset. Mirrors web's formatDate in list-row/board-card.
function formatDueDate(iso: string | null): string | null {
  if (!iso) return null;
  return formatDateOnly(iso, { month: "short", day: "numeric" }, "en-US") || null;
}

export function AttributeRow({
  issue,
  onOpenPicker,
}: {
  issue: Issue;
  onOpenPicker: (field: IssuePickerField) => void;
}) {
  const c = useThemeColors();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { getName } = useActorLookup();
  // The chip shows the issue's own status, which may be a custom one — name
  // and colour come from the workspace catalog, the glyph from its category.
  // (MUL-6243)
  const { categoryOf, colorOf, labelOf, iconOf } = useIssueStatuses();

  // Project read-only — fetch list to look up the title + icon. Cheap
  // (cached after first issue-detail visit).
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const project = useMemo(
    () => findProject(projects, issue.project_id),
    [projects, issue.project_id],
  );

  const labels = issue.labels ?? [];

  const assigneeValue =
    issue.assignee_type && issue.assignee_id
      ? { type: issue.assignee_type, id: issue.assignee_id }
      : null;

  const assigneeName = assigneeValue
    ? getName(assigneeValue.type, assigneeValue.id)
    : null;
  const dueLabel = formatDueDate(issue.due_date);

  // border-dashed border-muted-foreground/40 — theme-aware so dark mode
  // flips (the iOS classes compiled to the muted-foreground token).
  const dashed: ViewStyle = {
    borderWidth: 1,
    borderStyle: "dashed",
    borderColor: withAlpha(c.mutedForeground, 0.4),
  };

  return (
    <View style={styles.row}>
      {/* Status — always shown */}
      <AttributeChip
        icon={
          <StatusIcon
            status={issue.status}
            category={categoryOf(issue.status)}
            icon={iconOf(issue.status)}
            color={colorOf(issue.status)}
            size={14}
          />
        }
        label={labelOf(issue.status)}
        variant="filled"
        onPress={() => onOpenPicker("status")}
      />

      {/* Priority */}
      <AttributeChip
        icon={<PriorityIcon priority={issue.priority} size={14} />}
        label={PRIORITY_CHIP_LABEL[issue.priority]}
        variant={issue.priority === "none" ? "dimmed" : "filled"}
        onPress={() => onOpenPicker("priority")}
      />

      {/* Assignee */}
      {assigneeValue ? (
        <AttributeChip
          icon={
            <ActorAvatar
              type={assigneeValue.type}
              id={assigneeValue.id}
              size={16}
            />
          }
          label={assigneeName ?? "Unknown"}
          variant="filled"
          onPress={() => onOpenPicker("assignee")}
        />
      ) : (
        <AttributeChip
          icon={
            <View style={[styles.assigneePlaceholder, dashed]} />
          }
          label="Assignee"
          variant="dimmed"
          onPress={() => onOpenPicker("assignee")}
        />
      )}

      {/* Each existing label renders as its own chip. Tap opens the
          label picker (multi-select toggle). No quick-detach gesture
          on the chip itself in v1 — Linear iOS uses long-press for
          that, deferred until requested. */}
      {labels.map((label) => (
        <AttributeChip
          key={label.id}
          icon={
            <View
              style={[styles.labelDot, { backgroundColor: label.color }]}
            />
          }
          label={label.name}
          variant="filled"
          onPress={() => onOpenPicker("label")}
        />
      ))}
      {labels.length === 0 ? (
        <AttributeChip
          icon={
            <Text
              style={[
                styles.labelPlaceholder,
                { color: withAlpha(c.mutedForeground, 0.7) },
              ]}
            >
              ◯
            </Text>
          }
          label="Label"
          variant="dimmed"
          onPress={() => onOpenPicker("label")}
        />
      ) : null}

      {/* Project */}
      {project ? (
        <AttributeChip
          icon={<ProjectIcon icon={project.icon} size="sm" />}
          label={project.title}
          variant="filled"
          onPress={() => onOpenPicker("project")}
        />
      ) : (
        <AttributeChip
          icon={<View style={[styles.projectPlaceholder, dashed]} />}
          label="Project"
          variant="dimmed"
          onPress={() => onOpenPicker("project")}
        />
      )}

      {/* Due date */}
      <AttributeChip
        icon={
          <Text style={[styles.dueGlyph, { color: withAlpha(c.mutedForeground, 0.8) }]}>
            📅
          </Text>
        }
        label={dueLabel ?? "Due date"}
        variant={dueLabel ? "filled" : "dimmed"}
        onPress={() => onOpenPicker("due-date")}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  // flex-row flex-wrap gap-2
  row: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
  },
  // size-4 rounded-full
  assigneePlaceholder: { width: 16, height: 16, borderRadius: 8 },
  // size-2.5 rounded-full
  labelDot: { width: 10, height: 10, borderRadius: 5 },
  // text-xs
  labelPlaceholder: { fontSize: 12 },
  // size-3.5 rounded-sm
  projectPlaceholder: { width: 14, height: 14, borderRadius: 2 },
  // text-xs
  dueGlyph: { fontSize: 12 },
});
