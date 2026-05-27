/**
 * Priority picker route for an existing issue. See ./status.tsx for the
 * self-contained-route rationale.
 */
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { PriorityPickerBody } from "@/components/issue/pickers/priority-picker-body";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";
import { useWorkspaceStore } from "@/data/workspace-store";

export default function IssuePriorityPickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, id));
  const updateIssue = useUpdateIssue(id);

  return (
    <PriorityPickerBody
      value={issue?.priority ?? "none"}
      onChange={(next) => {
        updateIssue.mutate({ priority: next });
        router.back();
      }}
    />
  );
}
