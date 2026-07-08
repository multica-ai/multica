/**
 * Assignee picker route for an existing issue. Uses the native iOS Stack
 * header + UISearchController (registered in ../_layout.tsx with
 * `headerShown: true` + title); the search bar wiring is encapsulated in
 * `useNativeSearchBar`.
 */
import { useLocalSearchParams, router } from "expo-router";
import { useQuery } from "@tanstack/react-query";
import { AssigneePickerBody } from "@/components/issue/pickers/assignee-picker-body";
import { issueDetailOptions } from "@/data/queries/issues";
import { useUpdateIssue } from "@/data/mutations/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";

export default function IssueAssigneePickerRoute() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { data: issue } = useQuery(issueDetailOptions(wsId, id));
  const updateIssue = useUpdateIssue(id);
  const query = useNativeSearchBar("Search people", { autoFocus: true });

  const value =
    issue?.assignee_type && issue?.assignee_id
      ? { type: issue.assignee_type, id: issue.assignee_id }
      : null;

  return (
    <AssigneePickerBody
      value={value}
      query={query}
      onChange={(next) => {
        if (next === null) {
          updateIssue.mutate({ assignee_type: null, assignee_id: null });
        } else {
          updateIssue.mutate({
            assignee_type: next.type,
            assignee_id: next.id,
          });
        }
        router.back();
      }}
    />
  );
}
