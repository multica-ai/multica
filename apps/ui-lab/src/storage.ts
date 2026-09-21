import { emptyDraft, isDraft, sourceRevision, type Draft } from "./tokens";

export const STORAGE_KEY = "multica-ui-lab:v1";
export type SavedDesign = {
  id: string;
  name: string;
  draft: Draft;
  savedAt: string;
};
export type LabSession = { draft: Draft; designs: SavedDesign[] };
const freshSession = (): LabSession => ({ draft: emptyDraft(), designs: [] });
export function decodeSession(raw: string | null): LabSession {
  if (!raw) return freshSession();
  try {
    const data = JSON.parse(raw);
    if (
      data.version !== 1 ||
      !isDraft(data.draft) ||
      !Array.isArray(data.designs)
    )
      return freshSession();
    const designs = data.designs
      .filter((item: unknown): item is SavedDesign => {
        if (!item || typeof item !== "object") return false;
        const value = item as Record<string, unknown>;
        return (
          typeof value.id === "string" &&
          typeof value.name === "string" &&
          value.name.length > 0 &&
          value.name.length <= 60 &&
          typeof value.savedAt === "string" &&
          isDraft(value.draft)
        );
      })
      .slice(0, 20);
    return { draft: data.draft, designs };
  } catch {
    return freshSession();
  }
}
export function encodeSession(session: LabSession): string {
  return JSON.stringify({ version: 1, sourceRevision, ...session });
}
export function loadSession(): LabSession {
  try {
    return decodeSession(localStorage.getItem(STORAGE_KEY));
  } catch {
    return freshSession();
  }
}
