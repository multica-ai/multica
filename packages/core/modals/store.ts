"use client";

import { create } from "zustand";

type ModalType =
  | "create-issue"
  | "quick-create-issue"
  | "create-project"
  | "create-squad"
  | "feedback"
  | "issue-set-parent"
  | "issue-mark-duplicate"
  | "issue-add-child"
  | "issue-add-blocker"
  | "issue-add-blocking"
  | "project-add-issue"
  | "project-rename"
  | "issue-rename"
  | "issue-delete-confirm"
  | "issue-run-confirm"
  | "issue-workflow-handoff"
  | null;

// Each reason is a Cloud-enforced limit whose recovery dialog offers billing
// actions. Add a reason here when a new limit gets its own recovery copy.
export type LimitRecoveryReason = "autopilot_quota";

interface ModalStore {
  modal: ModalType;
  data: Record<string, unknown> | null;
  limitRecoveryWorkspaceId: string | null;
  limitRecoveryReason: LimitRecoveryReason;
  open: (modal: NonNullable<ModalType>, data?: Record<string, unknown> | null) => void;
  close: () => void;
  showLimitRecovery: (workspaceId: string, reason: LimitRecoveryReason) => void;
  dismissLimitRecovery: () => void;
}

export const useModalStore = create<ModalStore>((set) => ({
  modal: null,
  data: null,
  limitRecoveryWorkspaceId: null,
  limitRecoveryReason: "autopilot_quota",
  open: (modal, data = null) => set({ modal, data }),
  close: () => set({ modal: null, data: null }),
  showLimitRecovery: (workspaceId, reason) =>
    set({
      limitRecoveryWorkspaceId: workspaceId,
      limitRecoveryReason: reason,
    }),
  dismissLimitRecovery: () => set({ limitRecoveryWorkspaceId: null }),
}));
