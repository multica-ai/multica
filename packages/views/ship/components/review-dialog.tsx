"use client";

// Phase 6.5 — submit a GitHub PR review without leaving Multica.
//
// The diff itself still renders on GitHub (we link out via "View diff");
// the review-submission UI lives in this dialog. Three submit buttons
// (Approve / Request changes / Comment only) map to GitHub's three
// review events. Body validation mirrors GitHub:
//   - APPROVE: empty body OK
//   - REQUEST_CHANGES: requires a non-empty body
//   - COMMENT: requires a non-empty body
//
// Why a dedicated component rather than reusing ChipButton's inline flow?
//   - Three buttons, not one — the chip's single-action affordance doesn't
//     fit. Inlining a tri-state chip would require either a popover (UI
//     friction for what's already a multi-line operation) or a hidden
//     action menu (worse discoverability).
//   - The textarea state needs to live somewhere; a chip can't own it.
//   - Request-changes confirmation is a second dialog, kept as an
//     AlertDialog so the user has to deliberately confirm — request-
//     changes blocks the PR until dismissed and is meaningfully more
//     consequential than approve / comment.

import { useState } from "react";
import { ExternalLink, AlertCircle } from "lucide-react";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { useSubmitPullRequestReview } from "@multica/core/ship";
import type { PullRequest, SubmitPullRequestReviewRequest } from "@multica/core/types";
import { useT } from "../../i18n";

interface ReviewDialogProps {
  pr: PullRequest;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

type ReviewEvent = SubmitPullRequestReviewRequest["event"];

export function ReviewDialog({ pr, open, onOpenChange }: ReviewDialogProps) {
  const { t } = useT("ship");
  const submit = useSubmitPullRequestReview(pr.id);

  // Local state. We deliberately don't lift the textarea body into the
  // mutation — the user might cancel before submitting, and a stale
  // body in TanStack state would feel like a bug on reopen.
  const [body, setBody] = useState("");
  // Tracks which submit button is in flight so each shows its own
  // spinner; the mutation's isPending is shared across all three.
  const [pendingEvent, setPendingEvent] = useState<ReviewEvent | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [confirmChanges, setConfirmChanges] = useState(false);

  const trimmedBody = body.trim();
  const hasBody = trimmedBody.length > 0;

  const submitForEvent = async (event: ReviewEvent) => {
    setPendingEvent(event);
    setErrorMessage(null);
    try {
      const result = await submit.mutateAsync({
        event,
        body: trimmedBody,
      });
      if (result.status === "succeeded") {
        toast.success(t(($) => $.chips.review.toast_success));
        // Reset and close. Body cleared so a follow-up review on the
        // same PR starts fresh; if we left it the user might
        // accidentally re-submit identical text.
        setBody("");
        onOpenChange(false);
      } else {
        // The server returned 200 with a failed/in_progress status —
        // surface the recorded error inline rather than as a toast so
        // the user can copy/paste it without it disappearing.
        setErrorMessage(result.error || t(($) => $.chips.toast_generic_failure));
      }
    } catch (err) {
      // Non-2xx → ApiError with the server's `error` string.
      setErrorMessage(
        err instanceof Error ? err.message : t(($) => $.chips.toast_generic_failure),
      );
    } finally {
      setPendingEvent(null);
    }
  };

  const handleApprove = () => {
    void submitForEvent("APPROVE");
  };
  const handleComment = () => {
    if (!hasBody) return;
    void submitForEvent("COMMENT");
  };
  const handleRequestChangesClick = () => {
    if (!hasBody) return;
    setConfirmChanges(true);
  };
  const handleRequestChangesConfirm = () => {
    setConfirmChanges(false);
    void submitForEvent("REQUEST_CHANGES");
  };

  // The "View diff" deep-link goes to the PR's Files tab so the user
  // lands on the unified diff rather than the conversation. Clicking
  // it leaves Multica for GitHub in a new tab; we don't try to embed
  // the diff inline in v6.5 (that's a future scope).
  const diffUrl = pr.html_url ? `${pr.html_url}/files` : "";

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent
          className="sm:max-w-lg"
          // Prevent click events from bubbling up to a parent <a>
          // (the card is wrapped in an anchor for the GitHub deep
          // link). Same defensive pattern ChipButton uses.
          onClick={(e) => e.stopPropagation()}
          onKeyDown={(e) => e.stopPropagation()}
        >
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.chips.review.dialog_title, {
                number: pr.number,
                title: pr.title,
              })}
            </DialogTitle>
            <DialogDescription>
              {diffUrl && (
                <a
                  href={diffUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1 text-primary hover:underline"
                  data-testid="review-dialog-view-diff"
                >
                  <ExternalLink className="size-3.5" aria-hidden />
                  {t(($) => $.chips.review.view_diff_link)}
                </a>
              )}
            </DialogDescription>
          </DialogHeader>

          <div className="flex flex-col gap-2">
            <label
              htmlFor="review-dialog-body"
              className="text-xs font-medium text-muted-foreground"
            >
              {t(($) => $.chips.review.body_label)}
            </label>
            <Textarea
              id="review-dialog-body"
              value={body}
              onChange={(e) => setBody(e.target.value)}
              placeholder={t(($) => $.chips.review.body_placeholder)}
              // Six visible rows balances "obvious it accepts paragraphs"
              // with "doesn't push the buttons below the fold on a
              // small laptop".
              rows={6}
              disabled={submit.isPending}
            />
          </div>

          {errorMessage && (
            <div
              role="alert"
              className="flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/5 p-2 text-xs text-destructive"
              data-testid="review-dialog-error"
            >
              <AlertCircle className="mt-0.5 size-3.5 shrink-0" aria-hidden />
              <div>
                <div className="font-medium">
                  {t(($) => $.chips.review.error_banner_title)}
                </div>
                <div className="text-destructive/80">{errorMessage}</div>
              </div>
            </div>
          )}

          {/* Footer button row. Three submit buttons + an explicit close
              affordance. We don't use DialogFooter's bottom-bar style
              because the three submit buttons read better as a row of
              equals than as right-aligned secondary/primary. */}
          <div className="flex flex-wrap items-center justify-end gap-2 pt-1">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={submit.isPending}
            >
              {t(($) => $.chips.review.close_dialog)}
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={handleComment}
              // GitHub returns 422 for COMMENT without a body; disabling
              // here turns that into a no-op rather than a server-side
              // error round-trip.
              disabled={!hasBody || submit.isPending}
              data-testid="review-dialog-submit-comment"
            >
              {pendingEvent === "COMMENT" ? (
                <Spinner className="size-3" aria-hidden />
              ) : null}
              {t(($) => $.chips.review.submit_comment)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={handleRequestChangesClick}
              // REQUEST_CHANGES requires a body just like COMMENT.
              disabled={!hasBody || submit.isPending}
              data-testid="review-dialog-submit-request-changes"
            >
              {pendingEvent === "REQUEST_CHANGES" ? (
                <Spinner className="size-3" aria-hidden />
              ) : null}
              {t(($) => $.chips.review.submit_request_changes)}
            </Button>
            <Button
              type="button"
              // Approve uses default variant — green/primary on the
              // theme. We don't add a custom green-only color because
              // the design system doesn't have one and the primary
              // already reads as "go ahead".
              onClick={handleApprove}
              disabled={submit.isPending}
              data-testid="review-dialog-submit-approve"
            >
              {pendingEvent === "APPROVE" ? (
                <Spinner className="size-3" aria-hidden />
              ) : null}
              {t(($) => $.chips.review.submit_approve)}
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Request-changes confirmation. AlertDialog because the affordance
          blocks the PR — a stray click should not auto-block. */}
      <AlertDialog open={confirmChanges} onOpenChange={setConfirmChanges}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t(($) => $.chips.review.request_changes_confirm_title)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.chips.review.request_changes_confirm_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>
              {t(($) => $.chips.confirm_cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleRequestChangesConfirm}
            >
              {t(($) => $.chips.review.request_changes_confirm_action)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
