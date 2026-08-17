"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import {
  SHORTCUT_ACTIONS,
  SHORTCUT_ACTION_BY_ID,
  isShortcutAllowedForAction,
  parseLegacyShortcut,
  shortcutChordEquals,
  type ShortcutActionId,
  type ShortcutChord,
} from "./definitions";

export type ShortcutOverrides = Partial<Record<ShortcutActionId, ShortcutChord | null>>;

const SHORTCUT_ACTION_IDS = new Set<string>(
  SHORTCUT_ACTIONS.map((action) => action.id),
);

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function sanitizeShortcutChord(value: unknown): ShortcutChord | null | undefined {
  if (value === null) return null;
  if (!isRecord(value) || typeof value.key !== "string" || value.key.length === 0) {
    return undefined;
  }
  const modifiers = value.modifiers;
  if (!isRecord(modifiers)) return undefined;
  const { primary, control, meta, alt, shift } = modifiers;
  if (
    typeof primary !== "boolean" ||
    typeof control !== "boolean" ||
    typeof meta !== "boolean" ||
    typeof alt !== "boolean" ||
    typeof shift !== "boolean"
  ) {
    return undefined;
  }
  return {
    key: value.key,
    modifiers: {
      primary,
      control,
      meta,
      alt,
      shift,
    },
  };
}

/** Drop unknown actions and malformed chords instead of letting storage crash matching/UI. */
export function sanitizeShortcutOverrides(value: unknown): ShortcutOverrides {
  if (!isRecord(value)) return {};
  const overrides: ShortcutOverrides = {};
  for (const [actionId, rawShortcut] of Object.entries(value)) {
    if (!SHORTCUT_ACTION_IDS.has(actionId)) continue;
    const shortcut = sanitizeShortcutChord(rawShortcut);
    if (
      shortcut !== undefined &&
      (shortcut === null || isShortcutAllowedForAction(
        actionId as ShortcutActionId,
        shortcut,
      ))
    ) {
      overrides[actionId as ShortcutActionId] = shortcut;
    }
  }
  return overrides;
}

interface ShortcutState {
  overrides: ShortcutOverrides;
  setShortcut: (actionId: ShortcutActionId, shortcut: ShortcutChord | null) => void;
  resetShortcut: (actionId: ShortcutActionId) => void;
  resetAll: () => void;
}

export function migrateShortcutState(
  persisted: unknown,
  version: number,
): { overrides: ShortcutOverrides } {
  if (version >= 2) {
    const current = isRecord(persisted) ? persisted : undefined;
    return { overrides: sanitizeShortcutOverrides(current?.overrides) };
  }
  const previous = isRecord(persisted) && isRecord(persisted.overrides)
    ? persisted.overrides
    : {};
  const overrides: ShortcutOverrides = {};
  for (const [actionId, value] of Object.entries(previous)) {
    if (!SHORTCUT_ACTION_IDS.has(actionId)) continue;
    if (value === null) {
      overrides[actionId as ShortcutActionId] = null;
      continue;
    }
    if (typeof value !== "string") continue;
    const shortcut = parseLegacyShortcut(value);
    // Invalid old data means "use the default", not "disable the action".
    if (shortcut) overrides[actionId as ShortcutActionId] = shortcut;
  }
  return { overrides: sanitizeShortcutOverrides(overrides) };
}

export const useShortcutStore = create<ShortcutState>()(
  persist(
    (set) => ({
      overrides: {},
      setShortcut: (actionId, shortcut) =>
        set((state) => {
          if (shortcut && !isShortcutAllowedForAction(actionId, shortcut)) {
            return state;
          }
          const next = { ...state.overrides };
          if (shortcutChordEquals(shortcut, SHORTCUT_ACTION_BY_ID[actionId].defaultShortcut)) {
            delete next[actionId];
          } else {
            next[actionId] = shortcut;
          }
          return { overrides: next };
        }),
      resetShortcut: (actionId) =>
        set((state) => {
          const next = { ...state.overrides };
          delete next[actionId];
          return { overrides: next };
        }),
      resetAll: () => set({ overrides: {} }),
    }),
    {
      name: "multica_keyboard_shortcuts",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ overrides: state.overrides }),
      version: 2,
      migrate: migrateShortcutState,
      // Zustand only calls `migrate` when versions differ. Sanitize during
      // every hydration as well so corrupt current-version storage is safe.
      merge: (persisted, current) => {
        const stored = isRecord(persisted) ? persisted : undefined;
        return {
          ...current,
          overrides: sanitizeShortcutOverrides(stored?.overrides),
        };
      },
    },
  ),
);

export function resolveShortcut(
  overrides: ShortcutOverrides,
  actionId: ShortcutActionId,
): ShortcutChord | null {
  return Object.prototype.hasOwnProperty.call(overrides, actionId)
    ? overrides[actionId] ?? null
    : SHORTCUT_ACTION_BY_ID[actionId].defaultShortcut;
}

export function useShortcut(actionId: ShortcutActionId): ShortcutChord | null {
  return useShortcutStore((state) => resolveShortcut(state.overrides, actionId));
}

export function getShortcut(actionId: ShortcutActionId): ShortcutChord | null {
  return resolveShortcut(useShortcutStore.getState().overrides, actionId);
}

export function findShortcutConflict(
  actionId: ShortcutActionId,
  shortcut: ShortcutChord,
): ShortcutActionId | null {
  for (const action of SHORTCUT_ACTIONS) {
    if (
      action.id !== actionId &&
      shortcutChordEquals(getShortcut(action.id), shortcut)
    ) {
      return action.id;
    }
  }
  return null;
}
