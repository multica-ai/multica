import { useEffect } from "react";
import {
  useHotkey as useTanStackHotkey,
  useHotkeySequence as useTanStackSequence,
} from "@tanstack/react-hotkeys";
import type { HotkeyCallback, RegisterableHotkey, HotkeySequence } from "@tanstack/hotkeys";
import type { UseHotkeyOptions } from "@tanstack/react-hotkeys";
import { useHotkeyRegistry, type HotkeyCommand } from "./registry";
import type { HotkeyScope } from "./scopes";

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

export interface UseScopedHotkeyOptions extends UseHotkeyOptions {
  /** Human-readable description for the cheat-sheet. */
  description?: string;
  /** Scope this hotkey is active in. Defaults to "global". */
  scope?: HotkeyScope;
  /** Grouping label for the cheat-sheet (e.g. "Navigation"). */
  group?: string;
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

/**
 * Scope-aware hotkey hook.
 *
 * Registers the hotkey with TanStack *and* adds metadata to the Zustand
 * registry for the cheat-sheet. The hotkey only fires when its scope is
 * currently active.
 */
export function useHotkey(
  id: string,
  hotkey: RegisterableHotkey,
  callback: HotkeyCallback,
  options: UseScopedHotkeyOptions = {},
) {
  const {
    description = "",
    scope = "global",
    group,
    ...tanstackOptions
  } = options;

  const register = useHotkeyRegistry((s) => s.register);
  const unregister = useHotkeyRegistry((s) => s.unregister);
  const isScopeActive = useHotkeyRegistry((s) => s.activeScopes.has(scope));

  // Register command metadata for cheat-sheet discovery.
  useEffect(() => {
    const cmd: HotkeyCommand = {
      id,
      keys: typeof hotkey === "string" ? hotkey : JSON.stringify(hotkey),
      description,
      scope,
      group,
    };
    register(cmd);
    return () => unregister(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, hotkey, description, scope, group]);

  useTanStackHotkey(hotkey, callback, {
    ...tanstackOptions,
    enabled: isScopeActive && (tanstackOptions.enabled ?? true),
    meta: { name: description, ...tanstackOptions.meta },
  });
}

// ---------------------------------------------------------------------------
// Sequence variant
// ---------------------------------------------------------------------------

export interface UseScopedSequenceOptions
  extends Omit<UseScopedHotkeyOptions, "keys"> {
  /** Timeout between keys in the sequence (ms). Default: TanStack default. */
  timeout?: number;
}

/**
 * Scope-aware hotkey *sequence* hook (e.g. `g i`).
 */
export function useHotkeySequence(
  id: string,
  sequence: HotkeySequence,
  callback: HotkeyCallback,
  options: UseScopedSequenceOptions = {},
) {
  const {
    description = "",
    scope = "global",
    group,
    timeout,
    ...tanstackOptions
  } = options;

  const register = useHotkeyRegistry((s) => s.register);
  const unregister = useHotkeyRegistry((s) => s.unregister);
  const isScopeActive = useHotkeyRegistry((s) => s.activeScopes.has(scope));

  useEffect(() => {
    const cmd: HotkeyCommand = {
      id,
      keys: sequence.join(" "),
      description,
      scope,
      group,
      sequence,
    };
    register(cmd);
    return () => unregister(id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, sequence.join(","), description, scope, group]);

  useTanStackSequence(sequence, callback, {
    ...tanstackOptions,
    enabled: isScopeActive && (tanstackOptions.enabled ?? true),
    timeout,
    meta: { name: description },
  });
}
