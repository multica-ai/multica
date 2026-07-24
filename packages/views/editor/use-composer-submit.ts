"use client";

/**
 * The unified await-then-render send contract for every composer (MUL-5181).
 *
 * Before this hook, comment-input, reply-input, comment-card (edit),
 * create-issue, quick-create, project, and feedback each hand-copied the same
 * six-step "pessimistic submit": read markdown, guard empty/in-flight, re-read
 * the upload gate, lock + spin, await the server, and clear only on success
 * (keep the draft on failure). The copies drifted — some guarded single-flight
 * with a ref, some with a state boolean a render behind; some re-checked the
 * upload gate at submit time, some trusted the disabled button. This
 * centralizes the contract so every surface behaves identically.
 *
 * It is deliberately await-then-render, NOT optimistic: the composer keeps the
 * user's text and attachments in place (editor locked, button spinning) until
 * the server accepts the submission, then clears. A slow send never looks like
 * "posted but the box is still full", and a rejected send keeps the draft for
 * retry instead of silently dropping it.
 */

import { useCallback, useRef, useState, type RefObject } from "react";
import type { ContentEditorRef } from "./content-editor";
import type { UploadGate } from "./use-upload-gate";

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
      if (accepted) o.onAccepted?.();
    } catch {
      // A thrown send is a rejection: keep the draft, let the caller's
      // onSubmit surface its own error toast.
    } finally {
      inFlight.current = false;
      setSubmitting(false);
    }
  }, []);

  return { submitting, submit };
}
