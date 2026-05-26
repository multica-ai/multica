import { create } from "zustand";
import type { HotkeyScope } from "./scopes";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface HotkeyCommand {
  /** Unique command id, e.g. "issue.create" */
  id: string;
  /** TanStack hotkey string, e.g. "Mod+K" */
  keys: string;
  /** Human-readable description shown in the cheat-sheet. */
  description: string;
  /** Scope this command is active in. */
  scope: HotkeyScope;
  /** Optional grouping label for the cheat-sheet (e.g. "Navigation"). */
  group?: string;
  /** For sequences — the key array, e.g. ["G","I"]. */
  sequence?: string[];
}

interface RegistryState {
  /** All registered commands keyed by id. */
  commands: Map<string, HotkeyCommand>;
  /** Currently active scopes. `global` is always implicitly active. */
  activeScopes: Set<HotkeyScope>;
}

interface RegistryActions {
  register: (cmd: HotkeyCommand) => void;
  unregister: (id: string) => void;
  activateScope: (scope: HotkeyScope) => void;
  deactivateScope: (scope: HotkeyScope) => void;
  /** Replace the full set of active scopes (global is always kept). */
  setActiveScopes: (scopes: HotkeyScope[]) => void;
  isScopeActive: (scope: HotkeyScope) => boolean;
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

export const useHotkeyRegistry = create<RegistryState & RegistryActions>(
  (set, get) => ({
    commands: new Map(),
    activeScopes: new Set<HotkeyScope>(["global"]),

    register: (cmd) =>
      set((s) => {
        const next = new Map(s.commands);
        next.set(cmd.id, cmd);
        return { commands: next };
      }),

    unregister: (id) =>
      set((s) => {
        const next = new Map(s.commands);
        next.delete(id);
        return { commands: next };
      }),

    activateScope: (scope) =>
      set((s) => {
        const next = new Set(s.activeScopes);
        next.add(scope);
        return { activeScopes: next };
      }),

    deactivateScope: (scope) =>
      set((s) => {
        if (scope === "global") return s; // never deactivate global
        const next = new Set(s.activeScopes);
        next.delete(scope);
        return { activeScopes: next };
      }),

    setActiveScopes: (scopes) =>
      set(() => {
        const next = new Set<HotkeyScope>(scopes);
        next.add("global"); // always keep global
        return { activeScopes: next };
      }),

    isScopeActive: (scope) => get().activeScopes.has(scope),
  }),
);
