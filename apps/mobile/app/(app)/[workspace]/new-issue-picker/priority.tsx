/**
 * Priority picker route for the in-progress new-issue draft. See ./status.tsx.
 */
import { router } from "expo-router";
import { PriorityPickerBody } from "@/components/issue/pickers/priority-picker-body";
import { useNewIssueDraftStore } from "@/data/stores/new-issue-draft-store";

export default function NewIssuePriorityPickerRoute() {
  const priority = useNewIssueDraftStore((s) => s.priority);
  const setPriority = useNewIssueDraftStore((s) => s.setPriority);

  return (
    <PriorityPickerBody
      value={priority}
      onChange={(next) => {
        setPriority(next);
        router.back();
      }}
    />
  );
}
