"use client";

import { create } from "zustand";
import type {
  Agent,
  Autopilot,
  ChatSession,
  InboxItem,
  Issue,
  Project,
} from "../types";

/**
 * What the page on screen is about, registered by the page itself so the
 * command bar never has to reverse-engineer it from the URL. Handlers are
 * included where the action has page-local side effects the bar cannot
 * reproduce (moving the inbox selection off an archived row, the chat
 * page's archive/advance rule).
 */
export type CommandBarContext =
  | {
      kind: "issue";
      issue: Issue;
      /** Where to land after deleting it when there is no history to go back to. */
      onDeletedFallbackPath?: string;
    }
  | { kind: "issues"; issues: Issue[]; clearSelection: () => void }
  | { kind: "project"; project: Project }
  | { kind: "agent"; agent: Agent }
  | { kind: "autopilot"; autopilot: Autopilot }
  | {
      kind: "inbox";
      item: InboxItem | null;
      archive: (id: string) => void;
      markUnread: (id: string) => void;
      markAllRead: () => void;
      archiveAllRead: () => void;
    }
  | {
      kind: "chat";
      session: ChatSession;
      archive: (session: ChatSession) => void;
      changeProject: (projectId: string | null) => void;
    };

export type CommandBarContextKind = CommandBarContext["kind"];

export interface CommandBarContextEntry {
  token: string;
  context: CommandBarContext;
}

interface CommandBarContextState {
  /** Newest registration first; an update keeps the entry's position. */
  entries: CommandBarContextEntry[];
  register: (token: string, context: CommandBarContext) => void;
  unregister: (token: string) => void;
}

const EMPTY: CommandBarContextEntry[] = [];

export const useCommandBarContextStore = create<CommandBarContextState>()((set) => ({
  entries: EMPTY,
  register: (token, context) =>
    set((state) => {
      const index = state.entries.findIndex((entry) => entry.token === token);
      if (index === -1) {
        return { entries: [{ token, context }, ...state.entries] };
      }
      if (state.entries[index]!.context === context) return state;
      const entries = state.entries.slice();
      entries[index] = { token, context };
      return { entries };
    }),
  unregister: (token) =>
    set((state) => {
      if (!state.entries.some((entry) => entry.token === token)) return state;
      const entries = state.entries.filter((entry) => entry.token !== token);
      return { entries: entries.length > 0 ? entries : EMPTY };
    }),
}));

export function selectCommandBarContexts(state: CommandBarContextState) {
  return state.entries;
}
