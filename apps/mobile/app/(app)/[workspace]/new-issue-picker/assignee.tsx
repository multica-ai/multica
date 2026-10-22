/**
 * Assignee picker route for the in-progress new-issue draft. See ./status.tsx.
 * Uses the same iOS-native nav header + UISearchController pattern as
 * `issue/[id]/picker/assignee.tsx`, with the search bar wiring encapsulated
 * in `useNativeSearchBar`.
 */
import { router } from "expo-router";
import { AssigneePickerBody } from "@/components/issue/pickers/assignee-picker-body";
import { useNewIssueDraftStore } from "@/data/stores/new-issue-draft-store";
import { useNativeSearchBar } from "@/lib/use-native-search-bar";
import { useT } from "@/lib/i18n";

export default function NewIssueAssigneePickerRoute() {
  const { t } = useT("issues");
  const assignee = useNewIssueDraftStore((s) => s.assignee);
  const setAssignee = useNewIssueDraftStore((s) => s.setAssignee);
  const query = useNativeSearchBar(t("picker.search_people"), { autoFocus: true });

  return (
    <AssigneePickerBody
      value={assignee}
      query={query}
      onChange={(next) => {
        setAssignee(next);
        router.back();
      }}
    />
  );
}
