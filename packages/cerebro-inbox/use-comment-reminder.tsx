"use client";

// FIR-2643 — drives the "Remind me" entry in the shared comment menu. All
// state, gating, mutation and UI stay in the cerebro zone; the shared
// comment-card only calls this hook and renders the returned `dialog` outside
// its dropdown (the dropdown unmounts its own subtree on close, so the dialog
// must live as a sibling). `enabled` lets the menu hide the item entirely when
// the cerebro_comment_reminders flag is switched off.

import { useCallback, useState } from "react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { CerebroCommentReminderSheet } from "./components/cerebro-comment-reminder-sheet";
import { useCerebroInboxStrings } from "./strings";

export function useCommentReminder(issueId: string) {
  const enabled = useFeatureFlag("cerebro_comment_reminders");
  const strings = useCerebroInboxStrings();
  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState<{ commentId: string; content: string } | null>(null);

  const openReminder = useCallback((commentId: string, content: string) => {
    setTarget({ commentId, content });
    setOpen(true);
  }, []);

  const dialog =
    enabled && target ? (
      <CerebroCommentReminderSheet
        open={open}
        onOpenChange={setOpen}
        issueId={issueId}
        commentId={target.commentId}
        commentContent={target.content}
      />
    ) : null;

  return { enabled, label: strings.comment_remind_action, openReminder, dialog };
}
