"use client";

import { useCallback, useEffect, useState } from "react";

interface EditedValue {
  value: string;
  edited: boolean;
  edit: (value: string) => void;
  markSaved: (value: string) => void;
}

/**
 * A form field that follows the saved value until the user edits it, so a form
 * left open neither shows nor writes back a value changed elsewhere. An edit
 * ends when its own save has succeeded and the saved value has caught up:
 * an optimistic value that is rolled back must not take the typed text with it.
 */
export function useEditedValue(savedValue: string, resetKey: string | undefined): EditedValue {
  const [draft, setDraft] = useState<{ value: string; saved: boolean } | null>(null);

  useEffect(() => {
    setDraft(null);
  }, [resetKey]);

  useEffect(() => {
    if (draft?.saved && draft.value === savedValue) setDraft(null);
  }, [draft, savedValue]);

  const edit = useCallback((value: string) => setDraft({ value, saved: false }), []);
  const markSaved = useCallback((value: string) => {
    setDraft((current) =>
      current?.value === value ? { value, saved: true } : current,
    );
  }, []);

  return { value: draft ? draft.value : savedValue, edited: draft !== null, edit, markSaved };
}
