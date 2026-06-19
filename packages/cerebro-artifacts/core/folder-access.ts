// FIR-1590: folder-level access control — shared labels for the folder access
// picker. Mirrors the per-note visibility vocabulary so the two surfaces read
// the same to users.
import type { FolderVisibility } from "@multica/core/types";

export const FOLDER_VISIBILITY_VALUES: FolderVisibility[] = [
  "private",
  "shared",
  "workspace",
];

export const FOLDER_VISIBILITY_LABELS: Record<FolderVisibility, string> = {
  private: "Only you",
  shared: "Selected colleagues",
  workspace: "Whole team",
};

// A folder with no explicit visibility (legacy / never restricted) behaves as
// "Whole team".
export function folderVisibility(
  v: FolderVisibility | null | undefined,
): FolderVisibility {
  return v ?? "workspace";
}
