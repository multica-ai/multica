/**
 * Status picker route for the in-progress new-issue draft. Reads/writes
 * `useNewIssueDraftStore` — the new-issue.tsx modal owns the draft and
 * reads from the same store. See ../new-issue.tsx for the lifecycle.
 */
import { router } from "expo-router";
import { StatusPickerBody } from "@/components/issue/pickers/status-picker-body";
import { useNewIssueDraftStore } from "@/data/stores/new-issue-draft-store";

export default function NewIssueStatusPickerRoute() {
  const status = useNewIssueDraftStore((s) => s.status);
  const setStatus = useNewIssueDraftStore((s) => s.setStatus);

  return (
    <StatusPickerBody
      value={status}
      onChange={(next) => {
        setStatus(next);
        router.back();
      }}
    />
  );
}
