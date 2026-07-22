/**
 * Issue-detail attribute chip row. Linear iOS-inspired layout: each
 * editable attribute renders as a tappable chip; tapping pushes a
 * formSheet picker route. The route reads the issue from the TanStack
 * Query detail cache and fires its own mutation — no onChange callback
 * round-trip back to AttributeRow.
 *
 * Picker route map (every entry is registered in `_layout.tsx` with
 * shared SHEET_OPTIONS — formSheet + iOS native grabber + explicit
 * numeric detents):
 *   status    →  issue/[id]/picker/status
 *   priority  →  issue/[id]/picker/priority
 *   assignee  →  issue/[id]/picker/assignee
 *   labels    →  issue/[id]/picker/label   (multi-select, stays open)
 *   project   →  issue/[id]/picker/project
 *   due_date  →  issue/[id]/picker/due-date
 */
import { useMemo } from "react";
import { View } from "react-native";
import { router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type { Issue } from "@multica/core/types";
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
import { STATUS_LABEL, PRIORITY_LABEL } from "@/lib/issue-status";

/**
 * The picker fields the issue-detail attribute row can open. Bound to a
 * map of typed Expo Router pathnames so typos become compile errors
 * (previously the call site used `as never` on a template string, which
 * silently accepted anything).
 */
type IssuePickerField =
  | "status"
  | "priority"
  | "assignee"
  | "label"
  | "project"
  | "due-date";

const ISSUE_PICKER_PATHNAMES = {
  status: "/[workspace]/issue/[id]/picker/status",
  priority: "/[workspace]/issue/[id]/picker/priority",
  assignee: "/[workspace]/issue/[id]/picker/assignee",
  label: "/[workspace]/issue/[id]/picker/label",
  project: "/[workspace]/issue/[id]/picker/project",
  "due-date": "/[workspace]/issue/[id]/picker/due-date",
} as const satisfies Record<IssuePickerField, string>;

// due_date is a calendar day — format timezone-safely so the day never shifts
// with the viewer's offset. Mirrors web's formatDate in list-row/board-card.
function formatDueDate(iso: string | null): string | null {
  if (!iso) return null;
  return formatDateOnly(iso, { month: "short", day: "numeric" }, "en-US") || null;
}

export function AttributeRow({ issue }: { issue: Issue }) {
  const { t } = useTranslation("issues");
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { getName } = useActorLookup();

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
  // Chip placeholder shortens `none` from "No priority" → "Priority" so the
  // unset chip reads as a placeholder, not as a confusing assigned value.
  const priorityChipLabel =
    issue.priority === "none"
      ? t("attribute.priority_placeholder")
      : PRIORITY_LABEL[issue.priority];

  const openPicker = (field: IssuePickerField) => {
    if (!wsSlug) return;
    router.push({
      pathname: ISSUE_PICKER_PATHNAMES[field],
      params: { workspace: wsSlug, id: issue.id },
    });
  };

  return (
    <View className="flex-row flex-wrap gap-2">
      {/* Status — always shown */}
      <AttributeChip
        icon={<StatusIcon status={issue.status} size={14} />}
        label={STATUS_LABEL[issue.status]}
        variant="filled"
        onPress={() => openPicker("status")}
      />

      {/* Priority */}
      <AttributeChip
        icon={<PriorityIcon priority={issue.priority} size={14} />}
        label={priorityChipLabel}
        variant={issue.priority === "none" ? "dimmed" : "filled"}
        onPress={() => openPicker("priority")}
      />

      {/* Assignee */}
      {assigneeValue ? (
        <AttributeChip
          icon={
            <ActorAvatar
              type={assigneeValue.type}
              id={assigneeValue.id}
              size={16}
              showPresence
            />
          }
          label={assigneeName ?? t("attribute.assignee_unknown")}
          variant="filled"
          onPress={() => openPicker("assignee")}
        />
      ) : (
        <AttributeChip
          icon={
            <View className="size-4 rounded-full border border-dashed border-muted-foreground/40" />
          }
          label={t("attribute.assignee_placeholder")}
          variant="dimmed"
          onPress={() => openPicker("assignee")}
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
              className="size-2.5 rounded-full"
              style={{ backgroundColor: label.color }}
            />
          }
          label={label.name}
          variant="filled"
          onPress={() => openPicker("label")}
        />
      ))}
      {labels.length === 0 ? (
        <AttributeChip
          icon={<Text className="text-xs text-muted-foreground/70">◯</Text>}
          label={t("attribute.label_placeholder")}
          variant="dimmed"
          onPress={() => openPicker("label")}
        />
      ) : null}

      {/* Project */}
      {project ? (
        <AttributeChip
          icon={<ProjectIcon icon={project.icon} size="sm" />}
          label={project.title}
          variant="filled"
          onPress={() => openPicker("project")}
        />
      ) : (
        <AttributeChip
          icon={
            <View className="size-3.5 rounded-sm border border-dashed border-muted-foreground/40" />
          }
          label={t("attribute.project_placeholder")}
          variant="dimmed"
          onPress={() => openPicker("project")}
        />
      )}

      {/* Due date */}
      <AttributeChip
        icon={<Text className="text-xs text-muted-foreground/80">📅</Text>}
        label={dueLabel ?? t("attribute.due_date_placeholder")}
        variant={dueLabel ? "filled" : "dimmed"}
        onPress={() => openPicker("due-date")}
      />
    </View>
  );
}
