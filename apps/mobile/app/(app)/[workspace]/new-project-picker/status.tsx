/**
 * Status picker route for the in-progress new-project draft. Reads/writes
 * `useNewProjectDraftStore` — the project/new.tsx modal owns the draft and
 * reads from the same store. See ../project/new.tsx for the lifecycle, and
 * ../new-issue-picker/status.tsx for the mirror pattern.
 */
import { router } from "expo-router";
import { ProjectStatusPickerBody } from "@/components/project/pickers/project-status-picker-body";
import { useNewProjectDraftStore } from "@/data/stores/new-project-draft-store";

export default function NewProjectStatusPickerRoute() {
  const status = useNewProjectDraftStore((s) => s.status);
  const setStatus = useNewProjectDraftStore((s) => s.setStatus);

  return (
    <ProjectStatusPickerBody
      value={status}
      onChange={(next) => {
        setStatus(next);
        router.back();
      }}
    />
  );
}
