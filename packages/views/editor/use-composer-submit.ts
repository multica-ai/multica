"use client";

/**
 * The unified await-then-render send contract for every composer (MUL-5181).
 *
 * Before this hook, comment-input, reply-input, comment-card (edit),
 * create-issue, and quick-create each hand-copied the same six-step
 * "pessimistic submit": read markdown, guard empty/in-flight, re-read
 * the upload gate, lock + spin, await the server, and clear only on success
 * (keep the draft on failure). The copies drifted — some guarded single-flight
 * with a ref, some with a state boolean a render behind; some re-checked the
 * upload gate at submit time, some trusted the disabled button. This
 * centralizes the contract so every surface behaves identically.
 *
 * It is deliberately await-then-render, NOT optimistic: the composer keeps the
 * user's text and attachments in place (send affordance locked and spinning)
 * until the server accepts the submission, then clears. A slow send never
 * looks like "posted but the box is still full", and a rejected send keeps the
 * draft for retry instead of silently dropping it. The editor itself stays
 * interactive (Tiptap cannot toggle editable post-mount), so callers' accepted
 * handlers guard on a submit-time snapshot: success clears ONLY the draft it
 * submitted, and anything typed during the request survives.
 *
 * It also owns the post-send FOCUS decision (`afterAccepted`). Where the caret
 * lands after a send is a per-surface product call — a reply box the user keeps
 * writing in refocuses, a top-level comment ends the turn and drops the caret —
 * but the mechanics are identical everywhere and easy to get wrong (must run
 * after the clear, must survive a dialog focus trap, must not steal focus the
 * user moved elsewhere mid-flight). Those live here once.
 */

import { useCallback, useEffect, useRef, useState, type RefObject } from "react";
import type { ContentEditorRef } from "./content-editor";
import type { UploadGate } from "./use-upload-gate";

/**
 * What happens to keyboard focus after an accepted submit.
 *  - `refocus`: put the caret back in the composer so the user keeps writing.
 *  - `blur`: drop the caret, because the send ended the turn and the composer
 *    should stop reading as "still writing".
 *  - `none`: leave focus exactly where the send left it.
 */
export type ComposerAfterAccepted = "refocus" | "blur" | "none";

export interface ComposerSubmitOptions {
  editorRef: RefObject<ContentEditorRef | null>;
  uploadGate: UploadGate;
  /**
   * Perform the send. Resolve `true` when the server accepted the submission
   * (the composer then clears via `onAccepted`); resolve `false` to keep the
   * draft in place for retry. Throwing is treated as `false`.
   */
  onSubmit: (content: string) => Promise<boolean>;
  /**
   * Run once after an accepted submit: clear the editor, attachments, and the
   * persisted draft. Not run on rejection.
   */
  onAccepted?: () => void;
  /** Normalize the raw markdown before the empty-guard and `onSubmit`. */
  normalize?: (raw: string) => string;
  /**
   * Focus handling once `onAccepted` has cleared the composer. Defaults to
   * `none`.
   *
   * INVARIANT: a surface whose `onAccepted` can decline to clear must pass a
   * FUNCTION and resolve to `none` on those paths. Every composer here has a
   * stale-submit guard that keeps text typed during the request — and chat can
   * be sent fire-and-forget while the user is on another session, leaving the
   * shared editor on someone else's draft. Acting on a document the accepted
   * handler deliberately left alone drops the caret out of a sentence the user
   * is still writing. A literal mode is only safe when acceptance ALWAYS
   * scrubs the editor.
   */
  afterAccepted?: ComposerAfterAccepted | (() => ComposerAfterAccepted);
  /**
   * Root element of the composer, used only by `refocus`. Focus is taken back
   * only when it still belongs to this composer — the send button, the editor
   * itself, or nothing at all. Without this the composer would yank the caret
   * away from a field the user moved to while a slow send was in flight.
   * Omitting it refocuses unconditionally.
   */
  containerRef?: RefObject<HTMLElement | null>;
}

export interface ComposerSubmit {
  /** True from submit start until the server settles. Drives lock + spinner. */
  submitting: boolean;
  /** Invoke from the send button and the Cmd/Ctrl+Enter shortcut. */
  submit: () => Promise<void>;
}

const defaultNormalize = (raw: string) => raw.replace(/(\n\s*)+$/, "").trim();

export function useComposerSubmit(opts: ComposerSubmitOptions): ComposerSubmit {
  const [submitting, setSubmitting] = useState(false);
  // Synchronous single-flight. `submitting` is a render behind, so a second
  // Enter in the same tick would slip past a state-only guard and double-send.
  const inFlight = useRef(false);
  const optsRef = useRef(opts);
  optsRef.current = opts;
  // The focus effect lands a frame after acceptance, by which time this
  // composer may be gone (an inline edit closes on save) — never touch a ref
  // whose owner unmounted, and never leave a queued frame behind.
  const mountedRef = useRef(true);
  const focusFrameRef = useRef<number | null>(null);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      if (focusFrameRef.current !== null) {
        cancelAnimationFrame(focusFrameRef.current);
        focusFrameRef.current = null;
      }
    };
  }, []);

  const runAfterAccepted = useCallback(() => {
    const o = optsRef.current;
    // Resolve the mode NOW, not inside the frame: callers decide from state
    // that acceptance just settled (chat's "did we actually scrub the shared
    // editor" flag), and a frame later that state can already have moved on.
    const mode =
      typeof o.afterAccepted === "function"
        ? o.afterAccepted()
        : (o.afterAccepted ?? "none");
    if (mode === "none") return;
    // Defer one frame. `onAccepted` has just cleared the editor and the draft,
    // and a composer inside a dialog is still competing with the focus trap,
    // which bounces focus back to the first focusable header control on the
    // next tick if we grab it synchronously.
    if (focusFrameRef.current !== null) cancelAnimationFrame(focusFrameRef.current);
    focusFrameRef.current = requestAnimationFrame(() => {
      focusFrameRef.current = null;
      if (!mountedRef.current) return;
      if (mode === "blur") {
        optsRef.current.editorRef.current?.blur();
        return;
      }
      const container = optsRef.current.containerRef?.current;
      const active = typeof document === "undefined" ? null : document.activeElement;
      // `body` means the clear already dropped focus — that is still "ours".
      const ownsFocus =
        !container || !active || active === document.body || container.contains(active);
      if (!ownsFocus) return;
      optsRef.current.editorRef.current?.focus();
    });
  }, []);

  const submit = useCallback(async () => {
    const o = optsRef.current;
    const raw = o.editorRef.current?.getMarkdown() ?? "";
    const content = (o.normalize ?? defaultNormalize)(raw);
    if (!content || inFlight.current) return;
    // Submit-time upload re-check: the disabled button is a frame behind, and
    // Cmd+Enter / Enter-on-title bypass the button entirely.
    if (o.uploadGate.isBlocked()) return;

    inFlight.current = true;
    setSubmitting(true);
    try {
      const accepted = await o.onSubmit(content);
      if (accepted) {
        o.onAccepted?.();
        runAfterAccepted();
      }
    } catch {
      // A thrown send is a rejection: keep the draft, let the caller's
      // onSubmit surface its own error toast.
    } finally {
      inFlight.current = false;
      setSubmitting(false);
    }
  }, [runAfterAccepted]);

  return { submitting, submit };
}
