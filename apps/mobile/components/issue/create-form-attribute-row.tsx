/**
 * Bottom chip row for the new-issue form. Mirrors `attribute-row.tsx`'s
 * visual pattern but operates on the `useNewIssueDraftStore` instead of an
 * `issue` object + mutation. Tapping a chip pushes a formSheet picker
 * route under `new-issue-picker/<field>` — the route reads/writes the same
 * draft store, so the chip rehydrates automatically when the sheet
 * dismisses.
 *
 * Why a draft store: the picker routes are siblings of new-issue.tsx in
 * the Stack — they can't reach into the new-issue screen's local state.
 * The draft store is the cross-screen channel.
 */
import { View } from "react-native";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { AttributeChip } from "@/components/issue/attribute-chip";
import { ActorAvatar } from "@/components/ui/actor-avatar";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ProjectIcon } from "@/components/ui/project-icon";
import { StatusIcon } from "@/components/ui/status-icon";
import { useActorLookup } from "@/data/use-actor-name";
import { useNewIssueDraftStore } from "@/data/stores/new-issue-draft-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { PRIORITY_LABEL, STATUS_LABEL } from "@/lib/issue-status";

/**
 * Picker fields the new-issue draft form can open. Bound to a typed map
 * of Expo Router pathnames so typos become compile errors (previously
 * the call site used `as never` on a template string).
 */
type NewIssuePickerField =
  | "status"
  | "priority"
  | "assignee"
  | "project"
  | "due-date";

const NEW_ISSUE_PICKER_PATHNAMES = {
  status: "/[workspace]/new-issue-picker/status",
  priority: "/[workspace]/new-issue-picker/priority",
  assignee: "/[workspace]/new-issue-picker/assignee",
  project: "/[workspace]/new-issue-picker/project",
  "due-date": "/[workspace]/new-issue-picker/due-date",
} as const satisfies Record<NewIssuePickerField, string>;

export function CreateFormAttributeRow() {
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const status = useNewIssueDraftStore((s) => s.status);
  const priority = useNewIssueDraftStore((s) => s.priority);
  const assignee = useNewIssueDraftStore((s) => s.assignee);
  const dueDate = useNewIssueDraftStore((s) => s.dueDate);
  const project = useNewIssueDraftStore((s) => s.project);

  const { getName } = useActorLookup();
  const assigneeLabel = assignee
    ? getName(assignee.type, assignee.id)
    : "Assignee";
  const priorityLabel =
    priority === "none" ? "Priority" : PRIORITY_LABEL[priority];

  const open = (field: NewIssuePickerField) => {
    if (!wsSlug) return;
    router.push({
      pathname: NEW_ISSUE_PICKER_PATHNAMES[field],
      params: { workspace: wsSlug },
    });
  };

  return (
    <View>
      <View className="flex-row flex-wrap gap-2">
        <AttributeChip
          icon={<StatusIcon status={status} size={12} />}
          label={STATUS_LABEL[status]}
          variant="filled"
          onPress={() => open("status")}
        />
        <AttributeChip
          icon={<PriorityIcon priority={priority} />}
          label={priorityLabel}
          variant={priority === "none" ? "dimmed" : "filled"}
          onPress={() => open("priority")}
        />
        <AttributeChip
          icon={
            assignee ? (
              <ActorAvatar
                type={assignee.type}
                id={assignee.id}
                size={16}
                showPresence
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
          onPress={() => open("assignee")}
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
          onPress={() => open("due-date")}
        />
        <AttributeChip
          icon={
            project ? (
              <ProjectIcon icon={project.icon} size="sm" />
            ) : (
              <Ionicons name="folder-outline" size={14} color="#a1a1aa" />
            )
          }
          label={project?.title ?? "Project"}
          variant={project ? "filled" : "dimmed"}
          onPress={() => open("project")}
        />
      </View>
    </View>
  );
}

function formatDueDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "Due date";
  return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
