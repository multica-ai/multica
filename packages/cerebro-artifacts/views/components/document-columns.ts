"use client";

import * as React from "react";

// FIR-4163: the Documents list used to hard-code its grid template, so the
// column set was fixed. Columns are now data: each one declares its own track
// width, the grid template is derived from whichever columns are visible, and
// the choice is remembered per browser (same approach as the notes comments
// rail width). "Name" and the row-actions cell are structural — they are always
// rendered and are not part of the toggleable set.

export type DocumentColumnKey =
  | "kind"
  | "format"
  | "author"
  | "linked"
  | "location"
  | "modified"
  | "size";

export interface DocumentColumn {
  key: DocumentColumnKey;
  label: string;
  /** CSS grid track for this column on desktop. */
  width: string;
  /** Shown until the user says otherwise. */
  defaultVisible: boolean;
}

export const DOCUMENT_COLUMNS: DocumentColumn[] = [
  { key: "kind", label: "Kind", width: "100px", defaultVisible: true },
  { key: "format", label: "Format", width: "80px", defaultVisible: true },
  { key: "author", label: "Author", width: "160px", defaultVisible: true },
  { key: "linked", label: "Linked to", width: "120px", defaultVisible: true },
  { key: "location", label: "Location", width: "150px", defaultVisible: true },
  { key: "modified", label: "Modified", width: "110px", defaultVisible: true },
  { key: "size", label: "Size", width: "90px", defaultVisible: false },
];

const SELECT_TRACK = "40px";
const NAME_TRACK = "minmax(220px,1fr)";
const ACTIONS_TRACK = "44px";

export const DOCUMENT_COLUMNS_STORAGE_KEY = "documents:columns";

function defaultVisible(): DocumentColumnKey[] {
  return DOCUMENT_COLUMNS.filter((c) => c.defaultVisible).map((c) => c.key);
}

function readStored(): DocumentColumnKey[] | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.localStorage.getItem(DOCUMENT_COLUMNS_STORAGE_KEY);
    if (!raw) return null;
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return null;
    const known = new Set<string>(DOCUMENT_COLUMNS.map((c) => c.key));
    // Drop anything we no longer know about so a renamed column can't wedge
    // the list into an empty state.
    return parsed.filter(
      (k): k is DocumentColumnKey => typeof k === "string" && known.has(k),
    );
  } catch {
    return null;
  }
}

/**
 * Visible-column state for the Documents list, persisted per browser.
 */
export function useDocumentColumns() {
  const [visible, setVisible] = React.useState<DocumentColumnKey[]>(
    defaultVisible,
  );

  // Read on mount, not during render: the server render must match the first
  // client render, otherwise Next.js hydration warns.
  React.useEffect(() => {
    const stored = readStored();
    if (stored) setVisible(stored);
  }, []);

  const persist = React.useCallback((next: DocumentColumnKey[]) => {
    setVisible(next);
    if (typeof window === "undefined") return;
    try {
      window.localStorage.setItem(
        DOCUMENT_COLUMNS_STORAGE_KEY,
        JSON.stringify(next),
      );
    } catch {
      // Private mode / quota — the choice just doesn't survive a reload.
    }
  }, []);

  const isVisible = React.useCallback(
    (key: DocumentColumnKey) => visible.includes(key),
    [visible],
  );

  const toggle = React.useCallback(
    (key: DocumentColumnKey) => {
      persist(
        visible.includes(key)
          ? visible.filter((k) => k !== key)
          : DOCUMENT_COLUMNS.filter(
              (c) => c.key === key || visible.includes(c.key),
            ).map((c) => c.key),
      );
    },
    [persist, visible],
  );

  const reset = React.useCallback(() => persist(defaultVisible()), [persist]);

  // Ordered by DOCUMENT_COLUMNS so the header, every row and the grid template
  // can never disagree about column order.
  const columns = React.useMemo(
    () => DOCUMENT_COLUMNS.filter((c) => visible.includes(c.key)),
    [visible],
  );

  const gridTemplate = React.useMemo(
    () =>
      [
        SELECT_TRACK,
        NAME_TRACK,
        ...columns.map((c) => c.width),
        ACTIONS_TRACK,
      ].join(" "),
    [columns],
  );

  // Select + Name + toggleable columns + actions.
  const cellCount = columns.length + 3;

  return { columns, isVisible, toggle, reset, gridTemplate, cellCount };
}

export type DocumentColumnsState = ReturnType<typeof useDocumentColumns>;
