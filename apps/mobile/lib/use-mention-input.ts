/**
 * Shared mention-input state for any RN `<TextInput>` that wants `@mention`
 * support plus a `MarkdownToolbar`. Replaces the ~50 LOC mention boilerplate
 * that `new-issue.tsx` and `comment-composer.tsx` used to each carry.
 *
 * The hook owns:
 *
 *   - text / selection / markers — controlled state for the TextInput
 *   - mentioning — the active in-progress `@<query>` if any
 *   - handlers — drop-straight-onto-the-TextInput callbacks
 *   - suggestionBar — drop-straight-onto-the-MentionSuggestionBar props
 *   - insertAtCursor — generic "insert literal text at the caret" used by
 *                      all the toolbar buttons that aren't `@`
 *   - serialize / snapshot / restore / reset
 *
 * Snapshot / restore lets the caller implement optimistic submit + rollback
 * without coupling the policy to the hook (comment-composer does rollback;
 * new-issue prefers Alert and no rollback — both are fine).
 */
import { useCallback, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import type {
  NativeSyntheticEvent,
  TextInputSelectionChangeEventData,
} from "react-native";
import {
  insertMention,
  serializeMentions,
  tokenAtCursor,
  type MentionMarker,
} from "@/lib/mention-serialize";

export interface MentioningState {
  start: number;
  query: string;
}

export interface MentionInputSnapshot {
  text: string;
  markers: MentionMarker[];
  selection: { start: number; end: number };
}

export interface UseMentionInputReturn {
  text: string;
  /** Raw React state setter — accepts either a string or a functional
   *  updater. Callers doing post-await replacements (e.g. swapping an
   *  upload placeholder for the final markdown) MUST use the functional
   *  form to avoid losing typing the user did during the await. */
  setText: Dispatch<SetStateAction<string>>;
  selection: { start: number; end: number };
  setSelection: (sel: { start: number; end: number }) => void;
  markers: MentionMarker[];
  mentioning: MentioningState | null;
  handlers: {
    onChangeText: (next: string) => void;
    onSelectionChange: (
      e: NativeSyntheticEvent<TextInputSelectionChangeEventData>,
    ) => void;
    /** Toolbar `@` button. Inserts a literal `@` at the caret (with a
     *  leading space if needed so `tokenAtCursor` recognises it) and
     *  immediately triggers the suggestion bar. */
    onAtButtonPress: () => void;
  };
  suggestionBar: {
    visible: boolean;
    query: string;
    onSelect: (mention: MentionMarker) => void;
  };
  /** Generic literal-text insertion for toolbar buttons that aren't `@`.
   *  `cursorOffsetFromEnd` lets a button park the caret inside the inserted
   *  text — e.g. `insertAtCursor("\n```\n\n```", 4)` lands the caret in the
   *  empty middle line of a fenced code block. */
  insertAtCursor: (text: string, cursorOffsetFromEnd?: number) => void;
  /** Prepend `prefix` at the start of the current line (the line containing
   *  the caret). Used by list / checkbox / quote toolbar buttons — those
   *  semantically attach to a line, not the caret. */
  insertAtLineStart: (prefix: string) => void;
  serialize: () => string;
  snapshot: () => MentionInputSnapshot;
  restore: (snap: MentionInputSnapshot) => void;
  reset: () => void;
}

export function useMentionInput(): UseMentionInputReturn {
  const [text, setText] = useState("");
  const [selection, setSelection] = useState<{ start: number; end: number }>({
    start: 0,
    end: 0,
  });
  const [markers, setMarkers] = useState<MentionMarker[]>([]);
  const [mentioning, setMentioning] = useState<MentioningState | null>(null);

  // Refs mirror the latest text / selection so the two native event handlers
  // (`onChangeText` and `onSelectionChange`) can each see the OTHER's just-
  // applied value. Reading from React state via closures races with React's
  // batching: in the same native tick the first event would see stale text or
  // stale selection, and `tokenAtCursor` would miss the first `@` (cursor=0).
  // Refs sidestep that — every render syncs them, and every mutator below
  // writes through so handlers running in the same tick stay consistent.
  const textRef = useRef(text);
  const selectionRef = useRef(selection);
  textRef.current = text;
  selectionRef.current = selection;

  const recomputeMentioning = useCallback(
    (nextText: string, cursor: number) => {
      const token = tokenAtCursor(nextText, cursor);
      setMentioning(
        token ? { start: token.start, query: token.query } : null,
      );
    },
    [],
  );

  const onChangeText = useCallback(
    (next: string) => {
      textRef.current = next;
      setText(next);
      // Read selection from the ref so we get the cursor position
      // onSelectionChange may have just written in the same tick.
      recomputeMentioning(next, selectionRef.current.end);
    },
    [recomputeMentioning],
  );

  const onSelectionChange = useCallback(
    (e: NativeSyntheticEvent<TextInputSelectionChangeEventData>) => {
      const sel = e.nativeEvent.selection;
      selectionRef.current = sel;
      setSelection(sel);
      // Read text from the ref so we see what onChangeText may have just set.
      recomputeMentioning(textRef.current, sel.end);
    },
    [recomputeMentioning],
  );

  const onAtButtonPress = useCallback(() => {
    const t = textRef.current;
    const s = selectionRef.current;
    const before = t.slice(0, s.start);
    const after = t.slice(s.end);
    // Mention tokens require a word boundary before `@`. If the prior char
    // isn't whitespace (or start-of-text), pad with a space — otherwise the
    // suggestion bar won't trigger.
    const needsPad = before.length > 0 && !/\s$/.test(before);
    const inserted = (needsPad ? " " : "") + "@";
    const next = before + inserted + after;
    const cursor = before.length + inserted.length;
    textRef.current = next;
    selectionRef.current = { start: cursor, end: cursor };
    setText(next);
    setSelection({ start: cursor, end: cursor });
    recomputeMentioning(next, cursor);
  }, [recomputeMentioning]);

  const onSelectMention = useCallback(
    (mention: MentionMarker) => {
      if (!mentioning) return;
      const { newText, newSelection, marker } = insertMention(
        textRef.current,
        { start: mentioning.start, queryLength: mentioning.query.length },
        mention,
      );
      textRef.current = newText;
      selectionRef.current = newSelection;
      setText(newText);
      setSelection(newSelection);
      setMarkers((prev) => [...prev, marker]);
      setMentioning(null);
    },
    [mentioning],
  );

  const insertAtCursor = useCallback(
    (insert: string, cursorOffsetFromEnd = 0) => {
      const t = textRef.current;
      const s = selectionRef.current;
      const before = t.slice(0, s.start);
      const after = t.slice(s.end);
      const next = before + insert + after;
      const cursor = before.length + insert.length - cursorOffsetFromEnd;
      textRef.current = next;
      selectionRef.current = { start: cursor, end: cursor };
      setText(next);
      setSelection({ start: cursor, end: cursor });
      // Toolbar inserts (list / quote / code / inline image link) never
      // produce a mention — close the suggestion bar if it was open.
      setMentioning(null);
    },
    [],
  );

  const insertAtLineStart = useCallback(
    (prefix: string) => {
      const t = textRef.current;
      const s = selectionRef.current;
      const before = t.slice(0, s.start);
      const lastNewline = before.lastIndexOf("\n");
      // The line containing the caret starts after the previous \n, or at
      // index 0 if this is the first line.
      const lineStart = lastNewline === -1 ? 0 : lastNewline + 1;
      const next = t.slice(0, lineStart) + prefix + t.slice(lineStart);
      // Shift the caret right by prefix length so it stays in the same
      // visual position relative to what the user just typed.
      const cursor = s.end + prefix.length;
      textRef.current = next;
      selectionRef.current = { start: cursor, end: cursor };
      setText(next);
      setSelection({ start: cursor, end: cursor });
      setMentioning(null);
    },
    [],
  );

  const serialize = useCallback(
    () => serializeMentions(text, markers),
    [text, markers],
  );

  const snapshot = useCallback(
    (): MentionInputSnapshot => ({ text, markers, selection }),
    [text, markers, selection],
  );

  const restore = useCallback((snap: MentionInputSnapshot) => {
    textRef.current = snap.text;
    selectionRef.current = snap.selection;
    setText(snap.text);
    setMarkers(snap.markers);
    setSelection(snap.selection);
    setMentioning(null);
  }, []);

  const reset = useCallback(() => {
    textRef.current = "";
    selectionRef.current = { start: 0, end: 0 };
    setText("");
    setMarkers([]);
    setSelection({ start: 0, end: 0 });
    setMentioning(null);
  }, []);

  const handlers = useMemo(
    () => ({ onChangeText, onSelectionChange, onAtButtonPress }),
    [onChangeText, onSelectionChange, onAtButtonPress],
  );

  const suggestionBar = useMemo(
    () => ({
      visible: mentioning !== null,
      query: mentioning?.query ?? "",
      onSelect: onSelectMention,
    }),
    [mentioning, onSelectMention],
  );

  return {
    text,
    setText,
    selection,
    setSelection,
    markers,
    mentioning,
    handlers,
    suggestionBar,
    insertAtCursor,
    insertAtLineStart,
    serialize,
    snapshot,
    restore,
    reset,
  };
}
