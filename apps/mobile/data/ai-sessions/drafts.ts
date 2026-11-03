import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import * as SecureStore from "expo-secure-store";
import type { SessionScope } from "./client";

export function aiDraftKey(scope: SessionScope, id: string) { return JSON.stringify([scope.userId, scope.workspaceId, scope.deviceId, id]); }

export interface DraftAttachment { id: string; name: string; status: "uploading" | "uploaded" | "failed" }

export interface PendingSend { requestId: string; text: string; attachments: string[]; queued?: boolean; queueContent?: string; createdAt?: number; }

interface AIDrafts {
  drafts: Record<string, string>; setDraft(key: string, text: string): void;
  pending: Record<string, PendingSend>;
  setPending(key: string, value: PendingSend | null): void;
  attachments: Record<string, DraftAttachment[]>;
  setAttachments(key: string, update: (current: DraftAttachment[]) => DraftAttachment[]): void;
}

/** A transfer does not survive the app; only attachments the device has stored do. */
export function persistedAttachments(attachments: Record<string, DraftAttachment[]>) {
  return Object.fromEntries(Object.entries(attachments).flatMap(([key, files]) => {
    const stored = files.filter((file) => file.status === "uploaded");
    return stored.length ? [[key, stored]] : [];
  }));
}

export const useAIDrafts = create(persist<AIDrafts, [], [], Pick<AIDrafts, "drafts" | "pending" | "attachments">>((set) => ({
  attachments: {}, setAttachments: (key, update) => set((state) => {
    const attachments = { ...state.attachments };
    const next = update(attachments[key] ?? []);
    if (next.length) attachments[key] = next; else delete attachments[key];
    return { attachments };
  }),
  pending: {}, setPending: (key, value) => set((state) => {
    const pending = { ...state.pending };
    if (value) pending[key] = value; else delete pending[key];
    return { pending };
  }),
  drafts: {}, setDraft: (key, text) => set((state) => {
    const drafts = { ...state.drafts };
    if (text) drafts[key] = text; else delete drafts[key];
    return { drafts };
  }),
}), { name: "mobile.ai-session-drafts", partialize: ({ drafts, pending, attachments }) => ({ drafts, pending, attachments: persistedAttachments(attachments) }), storage: createJSONStorage(() => ({
  getItem: SecureStore.getItemAsync,
  setItem: (key, value) => SecureStore.setItemAsync(key, value),
  removeItem: SecureStore.deleteItemAsync,
})) }));
