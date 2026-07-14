"use client";

import { useCallback, useEffect, useRef, useState, type RefObject } from "react";

/**
 * Minimal imperative surface useLazyEditor needs from the wrapped editor.
 * ContentEditorRef and TitleEditorRef both satisfy it structurally.
 */
export interface LazyEditorHandle {
  focus: () => void;
  focusAtCoords?: (coords: { x: number; y: number }) => void;
  uploadFile?: (file: File) => void;
}

export interface UseLazyEditorOptions {
  /**
   * Mount the real editor immediately instead of the static stand-in.
   * The one legitimate case is an unsent draft: its existence proves edit
   * intent, and hydrating it into a visible editor matches the pre-lazy
   * behavior exactly.
   */
  initialActive?: boolean;
  /** The host's own editor ref — used for post-swap focus and queued uploads. */
  editorRef: RefObject<LazyEditorHandle | null>;
  /**
   * When this value changes, the hook returns to the static stand-in DURING
   * that same render (setState-during-render, React's derived-state pattern)
   * — not in an effect. An effect-based reset is one commit too late: the
   * render that carries the new key would still see `active/ready === true`,
   * visibly mount a keyed editor for the NEW subject, assemble a full Tiptap
   * instance, and only then throw it away — re-paying the exact main-thread
   * cost this hook exists to avoid, and blanking the region for a beat.
   * Hosts that remount per subject (via `key`) don't need this.
   */
  resetKey?: unknown;
}

/**
 * Shared controller for readonly-first editors (issue title / description /
 * comment / reply). Tiptap instantiation costs ~70-460ms per editor on the
 * main thread, so every always-mounted editor makes opening an issue jankier
 * (measured: 2-4 editors per open froze the click for 0.4-1.3s). The pattern:
 *
 *   1. Render a cheap static stand-in while `!active`.
 *   2. A click (or queued upload, or Enter on the stand-in) calls `activate`,
 *      which mounts the real editor HIDDEN — the stand-in stays visible so
 *      the content never blanks while Tiptap assembles.
 *   3. The editor's `onReady` flips `ready`; the host swaps visibility in
 *      that commit, and the effect below (running after that commit, so the
 *      editor is laid out) lands the caret at the activating click's
 *      coordinates and flushes any uploads queued against the stand-in.
 *
 * Host render contract:
 *   {lazy.active && <div className={lazy.ready ? undefined : "hidden"}>
 *     <Editor ref={editorRef} onReady={lazy.onReady} ... /></div>}
 *   {!lazy.ready && <StaticStandIn onClick={e => lazy.activate({x: e.clientX, y: e.clientY})} />}
 *
 * Hosts that outlive a subject switch without remounting (the web issue
 * route) must pass `resetKey`; keyed hosts get the reset for free by
 * remounting.
 */
export function useLazyEditor({
  initialActive = false,
  editorRef,
  resetKey,
}: UseLazyEditorOptions) {
  const [active, setActive] = useState(initialActive);
  const [ready, setReady] = useState(false);
  const focusCoordsRef = useRef<{ x: number; y: number } | null>(null);
  const focusPendingRef = useRef(false);
  const pendingFilesRef = useRef<File[]>([]);

  // Render-phase reset on subject change — see the `resetKey` option doc.
  const [prevResetKey, setPrevResetKey] = useState(resetKey);
  if (resetKey !== prevResetKey) {
    setPrevResetKey(resetKey);
    setActive(initialActive);
    setReady(false);
    focusCoordsRef.current = null;
    focusPendingRef.current = false;
    pendingFilesRef.current = [];
  }

  const activate = useCallback(
    (coords?: { x: number; y: number }) => {
      focusCoordsRef.current = coords ?? null;
      focusPendingRef.current = true;
      setActive(true);
      // Already swapped in (e.g. keyboard re-activation after a blur):
      // focus straight away, there is no ready-effect coming.
      if (ready) {
        focusPendingRef.current = false;
        const target = focusCoordsRef.current;
        focusCoordsRef.current = null;
        if (target && editorRef.current?.focusAtCoords) editorRef.current.focusAtCoords(target);
        else editorRef.current?.focus();
      }
    },
    [ready, editorRef],
  );

  const onReady = useCallback(() => setReady(true), []);

  // Post-swap work — runs after the commit that revealed the ready editor,
  // so focusAtCoords can resolve layout and uploads insert into a live doc.
  useEffect(() => {
    if (!ready) return;
    if (focusPendingRef.current) {
      focusPendingRef.current = false;
      const coords = focusCoordsRef.current;
      focusCoordsRef.current = null;
      if (coords && editorRef.current?.focusAtCoords) editorRef.current.focusAtCoords(coords);
      else editorRef.current?.focus();
    }
    const pending = pendingFilesRef.current;
    pendingFilesRef.current = [];
    for (const file of pending) editorRef.current?.uploadFile?.(file);
  }, [ready, editorRef]);

  /** Upload now when the editor is live; otherwise queue and summon it. */
  const uploadOrQueue = useCallback(
    (files: File[]) => {
      if (ready) {
        for (const file of files) editorRef.current?.uploadFile?.(file);
        return;
      }
      pendingFilesRef.current.push(...files);
      setActive(true);
    },
    [ready, editorRef],
  );

  return { active, ready, activate, onReady, uploadOrQueue };
}
