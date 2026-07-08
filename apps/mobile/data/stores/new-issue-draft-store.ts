/**
 * Draft state for the New Issue modal (`app/(app)/[workspace]/new-issue.tsx`).
 *
 * Why a store instead of local useState: the formSheet picker routes
 * (`new-issue-picker/status.tsx`, etc.) live in a separate Stack screen and
 * have no React parent-child relationship with the new-issue modal. They
 * need a way to read the current draft value and write the new selection
 * back without prop-drilling through the router. A small Zustand store is
 * the minimum-viable cross-screen channel.
 *
 * Lifecycle: `reset()` runs from `new-issue.tsx` when the user dismisses
 * the modal (either submit succeeds or they cancel) so the next open
 * starts clean. Seed-from-comment params still go through local useState
 * inside the screen (description text is a controlled input that doesn't
 * cross routes); only the attribute-chip values live here.
 *
 * Workspace lifecycle: this draft is workspace-scoped (e.g. an `assignee`
 * id only resolves in the workspace whose memberlist seeded it). When the
 * user switches workspaces, the draft is invalid. Reset is wired in
 * `app/(app)/[workspace]/_layout.tsx` via `useResetOnWorkspaceChange()` —
 * that's the only place that calls it on workspace-id transitions.
 */
import { useEffect, useRef } from "react";
import { create } from "zustand";
import type {
  IssuePriority,
  IssueStatus,
  Project,
} from "@multica/core/types";
import type { AssigneeValue } from "@/components/issue/pickers/assignee-picker-body";

interface NewIssueDraftState {
  status: IssueStatus;
  priority: IssuePriority;
  assignee: AssigneeValue;
  dueDate: string | null;
  project: Project | null;
  setStatus: (next: IssueStatus) => void;
  setPriority: (next: IssuePriority) => void;
  setAssignee: (next: AssigneeValue) => void;
  setDueDate: (next: string | null) => void;
  setProject: (next: Project | null) => void;
  reset: () => void;
}

const INITIAL: Pick<
  NewIssueDraftState,
  "status" | "priority" | "assignee" | "dueDate" | "project"
> = {
  status: "todo",
  priority: "none",
  assignee: null,
  dueDate: null,
  project: null,
};

export const useNewIssueDraftStore = create<NewIssueDraftState>((set) => ({
  ...INITIAL,
  setStatus: (next) => set({ status: next }),
  setPriority: (next) => set({ priority: next }),
  setAssignee: (next) => set({ assignee: next }),
  setDueDate: (next) => set({ dueDate: next }),
  setProject: (next) => set({ project: next }),
  reset: () => set({ ...INITIAL }),
}));

/**
 * Clears the new-issue draft store whenever the active workspace id
 * changes. Mounted once from the workspace `_layout.tsx`; relies on the
 * workspace store being the source of truth. The `useRef` gate ensures
 * the first mount is a no-op — we only fire `reset()` when the id
 * actually changes from one value to another, so a fresh app launch that
 * resolves the workspace into a non-null id doesn't pointlessly stomp
 * the already-INITIAL store on every cold start.
 */
export function useNewIssueDraftResetOnWorkspaceChange(wsId: string | null) {
  const prevRef = useRef(wsId);
  useEffect(() => {
    if (prevRef.current !== wsId) {
      useNewIssueDraftStore.getState().reset();
      prevRef.current = wsId;
    }
  }, [wsId]);
}
