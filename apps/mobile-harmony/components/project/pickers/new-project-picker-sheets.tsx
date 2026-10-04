/**
 * HarmonyOS ports of the new-project draft picker routes
 * (`apps/mobile/app/(app)/[workspace]/new-project-picker/{status,priority}.tsx`).
 * They read/write `useNewProjectDraftStore` — the new-project screen owns
 * the draft and reads the same store, so the sheet only needs to flip the
 * attribute and dismiss (see data/stores/new-project-draft-store.ts).
 */
import { BottomSheet } from "@/src/navigation/bottom-sheet";
import { ProjectStatusPickerBody } from "./project-status-picker-body";
import { ProjectPriorityPickerBody } from "./project-priority-picker-body";
import { useNewProjectDraftStore } from "@/data/stores/new-project-draft-store";

interface SheetProps {
  visible: boolean;
  onClose: () => void;
}

export function NewProjectStatusPickerSheet({ visible, onClose }: SheetProps) {
  const status = useNewProjectDraftStore((s) => s.status);
  const setStatus = useNewProjectDraftStore((s) => s.setStatus);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <ProjectStatusPickerBody
        value={status}
        onChange={(next) => {
          setStatus(next);
          onClose();
        }}
      />
    </BottomSheet>
  );
}

export function NewProjectPriorityPickerSheet({ visible, onClose }: SheetProps) {
  const priority = useNewProjectDraftStore((s) => s.priority);
  const setPriority = useNewProjectDraftStore((s) => s.setPriority);

  return (
    <BottomSheet visible={visible} onClose={onClose}>
      <ProjectPriorityPickerBody
        value={priority}
        onChange={(next) => {
          setPriority(next);
          onClose();
        }}
      />
    </BottomSheet>
  );
}
