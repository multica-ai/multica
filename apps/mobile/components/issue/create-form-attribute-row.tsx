/**
 * Bottom chip row + picker sheets for the new-issue form. Mirrors
 * `attribute-row.tsx`'s visual pattern but operates on form state
 * (controlled props + setters) instead of an `issue` object + mutation.
 *
 * Reuses (zero-modification):
 *  - StatusPickerSheet / PriorityPickerSheet / AssigneePickerSheet /
 *    DueDatePickerSheet / ProjectPickerSheet
 *  - AttributeChip
 *  - StatusIcon / PriorityIcon / ActorAvatar / ProjectIcon
 *
 * Chip "value present" rule: a chip is `filled` when the form value
 * differs from the default (todo / none / null). When at default it
 * renders `dimmed` with a placeholder label.
 */
import { useState } from "react";
import { ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useQuery } from "@tanstack/react-query";
import type { IssuePriority, IssueStatus } from "@multica/core/types";
import { AttributeChip } from "@/components/issue/attribute-chip";
import {
  AssigneePickerSheet,
  type AssigneeValue,
} from "@/components/issue/pickers/assignee-picker-sheet";
import { DueDatePickerSheet } from "@/components/issue/pickers/due-date-picker-sheet";
import { PriorityPickerSheet } from "@/components/issue/pickers/priority-picker-sheet";
import { ProjectPickerSheet } from "@/components/issue/pickers/project-picker-sheet";
import { StatusPickerSheet } from "@/components/issue/pickers/status-picker-sheet";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ProjectIcon } from "@/components/ui/project-icon";
import { StatusIcon } from "@/components/ui/status-icon";
import { useActorLookup } from "@/data/use-actor-name";
import { projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { PRIORITY_LABEL, STATUS_LABEL } from "@/lib/issue-status";

interface Props {
  status: IssueStatus;
  onStatusChange: (next: IssueStatus) => void;
  priority: IssuePriority;
  onPriorityChange: (next: IssuePriority) => void;
  assignee: AssigneeValue;
  onAssigneeChange: (next: AssigneeValue) => void;
  dueDate: string | null;
  onDueDateChange: (next: string | null) => void;
  projectId: string | null;
  onProjectIdChange: (next: string | null) => void;
}

export function CreateFormAttributeRow({
  status,
  onStatusChange,
  priority,
  onPriorityChange,
  assignee,
  onAssigneeChange,
  dueDate,
  onDueDateChange,
  projectId,
  onProjectIdChange,
}: Props) {
  const [statusOpen, setStatusOpen] = useState(false);
  const [priorityOpen, setPriorityOpen] = useState(false);
  const [assigneeOpen, setAssigneeOpen] = useState(false);
  const [dueOpen, setDueOpen] = useState(false);
  const [projectOpen, setProjectOpen] = useState(false);

  const { getName } = useActorLookup();
  const assigneeLabel = assignee
    ? getName(assignee.type, assignee.id)
    : "Assignee";
  const priorityLabel =
    priority === "none" ? "Priority" : PRIORITY_LABEL[priority];

  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: projects } = useQuery(projectListOptions(wsId));
  const project = projectId
    ? projects?.find((p) => p.id === projectId)
    : undefined;
  // While the list fetches, show "…" so the filled chip isn't visually
  // ambiguous with the unselected "Project" placeholder.
  const projectLabel = projectId ? project?.title ?? "…" : "Project";

  return (
    <View>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerClassName="flex-row gap-2 px-4 py-3"
      >
        <AttributeChip
          icon={<StatusIcon status={status} size={12} />}
          label={STATUS_LABEL[status]}
          variant="filled"
          onPress={() => setStatusOpen(true)}
        />
        <AttributeChip
          icon={<PriorityIcon priority={priority} />}
          label={priorityLabel}
          variant={priority === "none" ? "dimmed" : "filled"}
          onPress={() => setPriorityOpen(true)}
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
              <Ionicons
                name="person-circle-outline"
                size={16}
                color="#a1a1aa"
              />
            )
          }
          label={assigneeLabel}
          variant={assignee ? "filled" : "dimmed"}
          onPress={() => setAssigneeOpen(true)}
        />
        <AttributeChip
          icon={
            <Ionicons
              name="calendar-outline"
              size={14}
              color={dueDate ? undefined : "#a1a1aa"}
            />
          }
          label={dueDate ? formatDueDate(dueDate) : "Due date"}
          variant={dueDate ? "filled" : "dimmed"}
          onPress={() => setDueOpen(true)}
        />
        <AttributeChip
          icon={
            project ? (
              <ProjectIcon icon={project.icon} size="sm" />
            ) : (
              <Ionicons name="folder-outline" size={14} color="#a1a1aa" />
            )
          }
          label={projectLabel}
          variant={projectId ? "filled" : "dimmed"}
          onPress={() => setProjectOpen(true)}
        />
      </ScrollView>

      <StatusPickerSheet
        visible={statusOpen}
        value={status}
        onChange={onStatusChange}
        onClose={() => setStatusOpen(false)}
      />
      <PriorityPickerSheet
        visible={priorityOpen}
        value={priority}
        onChange={onPriorityChange}
        onClose={() => setPriorityOpen(false)}
      />
      <AssigneePickerSheet
        visible={assigneeOpen}
        value={assignee}
        onChange={onAssigneeChange}
        onClose={() => setAssigneeOpen(false)}
      />
      <DueDatePickerSheet
        visible={dueOpen}
        value={dueDate}
        onChange={onDueDateChange}
        onClose={() => setDueOpen(false)}
      />
      <ProjectPickerSheet
        visible={projectOpen}
        value={projectId}
        onChange={onProjectIdChange}
        onClose={() => setProjectOpen(false)}
      />
    </View>
  );
}

function formatDueDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "Due date";
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
