/**
 * HarmonyOS port of apps/mobile/components/issue/create-form-attribute-row.tsx
 * — bottom chip row for the new-issue form. Mirrors attribute-row.tsx's
 * visual pattern but operates on the `useNewIssueDraftStore` instead of an
 * `issue` object + mutation. Tapping a chip fires the `onOpenPicker`
 * callback prop; the host screen presents the matching draft-picker sheet,
 * which reads/writes the same store, so the chip rehydrates automatically
 * when the sheet dismisses.
 *
 * Why a draft store: on iOS the picker routes were siblings of new-issue.tsx
 * in the Stack and couldn't reach into the screen's local state; the store
 * was the cross-screen channel. The sheets keep the same channel.
 */
import React from "react";
import { StyleSheet, View } from "react-native";
import { formatDateOnly } from "@multica/core/issues/date";
import { AttributeChip } from "./attribute-chip";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ProjectIcon } from "@/components/ui/project-icon";
import { StatusIcon } from "@/components/ui/status-icon";
import { Icon } from "@/components/ui/icon";
import { useActorLookup } from "@/data/use-actor-name";
import { useNewIssueDraftStore } from "@/data/stores/new-issue-draft-store";
import { PRIORITY_LABEL } from "@/lib/issue-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { withAlpha } from "@/lib/theme";
import { useThemeColors } from "@/lib/use-theme-colors";

/** Picker fields the new-issue draft form can open. */
export type NewIssuePickerField =
  | "status"
  | "priority"
  | "assignee"
  | "project"
  | "due-date";

export function CreateFormAttributeRow({
  onOpenPicker,
}: {
  onOpenPicker: (field: NewIssuePickerField) => void;
}) {
  const c = useThemeColors();
  const status = useNewIssueDraftStore((s) => s.status);
  const priority = useNewIssueDraftStore((s) => s.priority);
  const assignee = useNewIssueDraftStore((s) => s.assignee);
  const dueDate = useNewIssueDraftStore((s) => s.dueDate);
  const project = useNewIssueDraftStore((s) => s.project);

  const { getName } = useActorLookup();
  // The draft can hold a custom status the user picked in the sheet. (MUL-6243)
  const { categoryOf, colorOf, labelOf, iconOf } = useIssueStatuses();
  const assigneeLabel = assignee
    ? getName(assignee.type, assignee.id)
    : "Assignee";
  const priorityLabel =
    priority === "none" ? "Priority" : PRIORITY_LABEL[priority];

  const dim = withAlpha(c.mutedForeground, 0.9);

  return (
    <View>
      <View style={styles.row}>
        <AttributeChip
          icon={
            <StatusIcon
              status={status}
              category={categoryOf(status)}
              icon={iconOf(status)}
              color={colorOf(status)}
              size={12}
            />
          }
          label={labelOf(status)}
          variant="filled"
          onPress={() => onOpenPicker("status")}
        />
        <AttributeChip
          icon={<PriorityIcon priority={priority} />}
          label={priorityLabel}
          variant={priority === "none" ? "dimmed" : "filled"}
          onPress={() => onOpenPicker("priority")}
        />
        <AttributeChip
          icon={
            assignee ? (
              <ActorAvatar
                type={assignee.type}
                id={assignee.id}
                size={16}
              />
            ) : (
              <Icon name="person-circle-outline" size={16} color={dim} />
            )
          }
          label={assigneeLabel}
          variant={assignee ? "filled" : "dimmed"}
          onPress={() => onOpenPicker("assignee")}
        />
        <AttributeChip
          icon={
            <Icon
              name="calendar-outline"
              size={14}
              color={dueDate ? c.foreground : dim}
            />
          }
          label={dueDate ? formatDueDate(dueDate) : "Due date"}
          variant={dueDate ? "filled" : "dimmed"}
          onPress={() => onOpenPicker("due-date")}
        />
        <AttributeChip
          icon={
            project ? (
              <ProjectIcon icon={project.icon} size="sm" />
            ) : (
              <Icon name="folder-outline" size={14} color={dim} />
            )
          }
          label={project?.title ?? "Project"}
          variant={project ? "filled" : "dimmed"}
          onPress={() => onOpenPicker("project")}
        />
      </View>
    </View>
  );
}

// due_date is a calendar day — format timezone-safely (no offset day shift).
function formatDueDate(iso: string): string {
  return formatDateOnly(iso, { month: "short", day: "numeric" }) || "Due date";
}

// flex-row flex-wrap gap-2
const styles = StyleSheet.create({
  row: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: 8,
  },
});
