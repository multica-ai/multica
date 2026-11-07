import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import * as SecureStore from "expo-secure-store";
import type { SessionScope } from "./client";
import { EMPTY_REFERENCES, type AIReference, type PreparedAIMessage } from "./references";

export function aiDraftKey(scope: SessionScope, id: string) { return JSON.stringify([scope.userId, scope.workspaceId, scope.deviceId, id]); }

export interface PendingSend { requestId: string; text: string; attachments: string[]; queued?: boolean; queuePaused?: boolean; queueContent?: string; createdAt?: number; prepared?: PreparedAIMessage; }
export interface DraftAttachment { id: string; name: string; status: "uploading" | "uploaded" | "failed"; uri?:string; mimeType?:string|null }
export const EMPTY_DRAFT_ATTACHMENTS: DraftAttachment[] = [];

interface AIDrafts {
  drafts: Record<string, string>; setDraft(key: string, text: string): void;
  references: Record<string, AIReference[]>;
  setReferences(key: string, references: AIReference[]): void;
  attachments: Record<string, DraftAttachment[]>;
  setAttachments(key: string, value: DraftAttachment[] | ((files: DraftAttachment[]) => DraftAttachment[])): void;
  complete(key: string, request: PendingSend): void;
  takeOwnership(key:string,request:PendingSend):void;
  pending: Record<string, PendingSend>;
  setPending(key: string, value: PendingSend | null): void;
}

function submittedDraft(state:AIDrafts,key:string,request:PendingSend){
 const drafts={...state.drafts},references={...state.references},attachments={...state.attachments};
 if(drafts[key]===request.text)delete drafts[key];
 const sent=request.prepared?.references??EMPTY_REFERENCES;
 if(JSON.stringify((references[key]??EMPTY_REFERENCES).map(({kind,id})=>({kind,id})))===JSON.stringify(sent))delete references[key];
 const files=attachments[key]?.filter((file)=>!request.attachments.includes(file.id));
 if(files?.length)attachments[key]=files;else delete attachments[key];
 return {drafts,references,attachments};
}

/** A transfer does not survive the app; only attachments the device has stored do. */
export function persistedAttachments(attachments: Record<string, DraftAttachment[]>) {
  return Object.fromEntries(Object.entries(attachments).flatMap(([key, files]) => {
    const stored = files.filter((file) => file.status === "uploaded" || !!file.uri).map((file) => file.status === "uploading" ? { ...file, status: "failed" as const } : file);
    return stored.length ? [[key, stored]] : [];
  }));
}

export const useAIDrafts = create(persist<AIDrafts, [], [], Pick<AIDrafts, "drafts" | "pending" | "attachments" | "references">>((set) => ({
  attachments: {}, setAttachments: (key, value) => set((state) => {
    const attachments = { ...state.attachments };
    const files = typeof value === "function" ? value(attachments[key] ?? EMPTY_DRAFT_ATTACHMENTS) : value;
    if (files.length) attachments[key] = files; else delete attachments[key];
    return { attachments };
  }),
  references: {}, setReferences: (key, value) => set((state) => {
    const references = { ...state.references };
    if (value.length) references[key] = value; else delete references[key];
    return { references };
  }),
  takeOwnership:(key,request)=>set((state)=>submittedDraft(state,key,request)),
  complete: (key, request) => set((state) => {
    // A late acknowledgement must not clear a newer draft or pending request.
    if (state.pending[key]?.requestId !== request.requestId) return state;
    const {drafts,references,attachments}=submittedDraft(state,key,request);
    const pending={...state.pending};
    delete pending[key];
    return { drafts, references, attachments, pending };
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
}), { name: "mobile.ai-session-drafts", partialize: ({ drafts, pending, attachments, references }) => ({ drafts, pending, references, attachments: persistedAttachments(attachments) }), storage: createJSONStorage(() => ({
  getItem: SecureStore.getItemAsync,
  setItem: (key, value) => SecureStore.setItemAsync(key, value),
  removeItem: SecureStore.deleteItemAsync,
})) }));
