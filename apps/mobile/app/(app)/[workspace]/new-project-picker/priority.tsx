/**
 * Priority picker route for the in-progress new-project draft. See ./status.tsx.
 */
import { router } from "expo-router";
import { ProjectPriorityPickerBody } from "@/components/project/pickers/project-priority-picker-body";
import { useNewProjectDraftStore } from "@/data/stores/new-project-draft-store";

export default function NewProjectPriorityPickerRoute() {
  const priority = useNewProjectDraftStore((s) => s.priority);
  const setPriority = useNewProjectDraftStore((s) => s.setPriority);

  return (
    <ProjectPriorityPickerBody
      value={priority}
      onChange={(next) => {
        setPriority(next);
        router.back();
      }}
    />
  );
}
