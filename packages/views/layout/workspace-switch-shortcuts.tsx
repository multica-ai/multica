"use client";

import { useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { paths, useCurrentWorkspace } from "@multica/core/paths";
import {
  createShortcutChord,
  getShortcutPlatform,
  isEditableShortcutTarget,
  isPortalLayerShortcutTarget,
  type ShortcutChord,
} from "@multica/core/shortcuts";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { useNavigation } from "../navigation";
import { shouldIgnoreGlobalShortcutEvent } from "./global-shortcuts";

const MAX_NUMBERED_WORKSPACES = 9;
const EMPTY_WORKSPACES: { id: string; slug: string }[] = [];

/**
 * Ctrl+Alt+1…9 (also Alt+Shift+1…9) opens the Nth workspace of the switcher list.
 *
 * Not a rebindable action: it is nine positional chords, and neither of the
 * obvious single-modifier families is free. Mod+1…9 selects browser and
 * desktop tabs (see PRIMARY_RESERVED_KEYS), and Alt+1…9 selects tabs in
 * Chrome and Firefox on Linux, where the page never receives the keydown.
 *
 * The match is deliberately tolerant on the modifiers: one of Alt / AltGraph
 * plus one of Ctrl / Shift, or Ctrl+Meta. Keyboards disagree on what the
 * Option key sends — a Mac keyboard on Linux may report AltGraph, and a
 * dual-mode one in Windows mode sends Super — and a chord that silently does
 * nothing on one of them reads as broken.
 *
 * Matched on the physical key (`event.code`) rather than `event.key`: with
 * Shift held the logical key is "!" on QWERTY, and on AZERTY the unshifted top
 * row is not digits at all, so only the key position means "the Nth".
 */
export function workspaceSwitchIndex(event: KeyboardEvent): number | null {
  const alt = event.altKey || event.getModifierState?.("AltGraph") === true;
  // Ctrl+Meta counts as Ctrl+Alt: a dual-mode Mac keyboard in its Windows mode
  // sends Super from the key printed "option". Meta alone, or with Shift, stays
  // out — desktops own Super+digit and Super+Shift+digit.
  const ctrlMeta = event.ctrlKey && event.metaKey && !alt;
  if (!ctrlMeta && (event.metaKey || !alt || !(event.ctrlKey || event.shiftKey))) return null;
  const match = /^Digit([1-9])$/.exec(event.code);
  return match ? Number(match[1]) - 1 : null;
}

/**
 * The chord shown for the workspace at `index` — Ctrl+Alt+N, ⌃⌥N on macOS —
 * or null past the ninth. On macOS the Control key is the literal `control`
 * modifier (`primary` would render ⌘); elsewhere `primary` is Ctrl.
 */
export function workspaceSwitchShortcut(index: number): ShortcutChord | null {
  if (index < 0 || index >= MAX_NUMBERED_WORKSPACES) return null;
  const mac = getShortcutPlatform() === "macos";
  return createShortcutChord(String(index + 1), mac ? { control: true, alt: true } : { primary: true, alt: true });
}

/** Listens for Ctrl+Alt+1…9 (or Alt+Shift+1…9) and switches to that workspace. */
export function WorkspaceSwitchShortcuts() {
  const navigation = useNavigation();
  const currentId = useCurrentWorkspace()?.id;
  const { data: workspaces = EMPTY_WORKSPACES } = useQuery(workspaceListOptions());

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (shouldIgnoreGlobalShortcutEvent(event)) return;
      const index = workspaceSwitchIndex(event);
      if (index === null) return;
      // Option+digit types a character on macOS, AltGr+digit does on many
      // layouts, and an open menu or dialog owns the keyboard: none of them
      // may be hijacked.
      if (isEditableShortcutTarget(event.target) || isPortalLayerShortcutTarget(event.target)) {
        return;
      }
      const target = workspaces[index];
      if (!target) return;
      event.preventDefault();
      if (target.id === currentId) return;
      // A push into another workspace's path is routed through the platform's
      // workspace switch by the navigation adapter.
      navigation.push(paths.workspace(target.slug).issues());
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [navigation, workspaces, currentId]);

  return null;
}
