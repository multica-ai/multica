import { create } from "zustand";
import { api } from "@multica/core/api";
import type { LocalV2Scope } from "@multica/core/local-conversation/v2";
import type { RestoreDraftRequest } from "@multica/views/chat/use-chat-draft-restore";

export function issueConversationScopeKey(scope: LocalV2Scope) {
  return JSON.stringify([api.getBaseUrl(), scope.userId, scope.workspaceId]);
}

export function issueConversationDraftKey(scope: LocalV2Scope, deviceId: string, sessionId: string) {
  return JSON.stringify([issueConversationScopeKey(scope), deviceId, sessionId]);
}

interface IssueConversationState {
  drafts: Record<string, RestoreDraftRequest>;
  pending: Record<string, boolean>;
  begin(key: string): boolean;
  finish(key: string): void;
  offer(key: string, draft: RestoreDraftRequest): void;
  applied(key: string, id: string): void;
}

/** Holds a menu request until its target composer acknowledges the draft. */
export const useIssueConversationStore = create<IssueConversationState>((set, get) => ({
  drafts: {},
  pending: {},
  begin: (key) => {
    if (get().pending[key]) return false;
    set((state) => ({ pending: { ...state.pending, [key]: true } }));
    return true;
  },
  finish: (key) => set((state) => {
    const pending = { ...state.pending };
    delete pending[key];
    return { pending };
  }),
  offer: (key, draft) => set((state) => ({ drafts: { ...state.drafts, [key]: draft } })),
  applied: (key, id) => set((state) => {
    if (state.drafts[key]?.id !== id) return state;
    const drafts = { ...state.drafts };
    delete drafts[key];
    return { drafts };
  }),
}));
