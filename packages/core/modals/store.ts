"use client";

import { create } from "zustand";

type ModalType =
  | "create-issue"
  | "quick-create-issue"
  | "create-project"
  | "create-squad"
  | "feedback"
  | "issue-set-parent"
  | "issue-add-child"
  | "issue-delete-confirm"
  | "issue-run-confirm"
  | null;

interface ModalStore {
  modal: ModalType;
  data: Record<string, unknown> | null;
  issueLimitRecoveryWorkspaceId: string | null;
  open: (modal: NonNullable<ModalType>, data?: Record<string, unknown> | null) => void;
  close: () => void;
  showIssueLimitRecovery: (workspaceId: string) => void;
  dismissIssueLimitRecovery: () => void;
}

export const useModalStore = create<ModalStore>((set) => ({
  modal: null,
  data: null,
  issueLimitRecoveryWorkspaceId: null,
  open: (modal, data = null) => set({ modal, data }),
  close: () => set({ modal: null, data: null }),
  showIssueLimitRecovery: (workspaceId) =>
    set({ issueLimitRecoveryWorkspaceId: workspaceId }),
  dismissIssueLimitRecovery: () =>
    set({ issueLimitRecoveryWorkspaceId: null }),
}));
