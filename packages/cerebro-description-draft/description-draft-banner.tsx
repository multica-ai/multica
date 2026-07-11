"use client";

import { AlertCircle } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";

export interface DescriptionDraftBannerProps {
  onRestore: () => void;
  onDiscard: () => void;
}

/**
 * FIR-2648 — shown above the issue description editor when a locally-saved
 * draft differs from what made it to the server (typically: the Cloudflare
 * Access session expired mid-edit and the debounced save never reached the
 * backend). English hardcoded on purpose, matching @multica/cerebro-comment-drafts'
 * DraftSavedHint: cerebro-zone component, no i18n key to thread through the
 * upstream description editor.
 */
export function DescriptionDraftBanner({ onRestore, onDiscard }: DescriptionDraftBannerProps) {
  return (
    <div
      data-testid="description-draft-banner"
      className="mb-2 flex items-center justify-between gap-2 rounded-md px-2.5 py-1.5 text-xs bg-amber-50 dark:bg-amber-950/40 text-amber-900 dark:text-amber-200 ring-1 ring-amber-200/60 dark:ring-amber-900/40"
    >
      <span className="flex items-center gap-1.5 min-w-0">
        <AlertCircle className="size-3.5 shrink-0" aria-hidden />
        <span className="truncate">
          Unsaved text from an earlier session was found — it may not have reached the server.
        </span>
      </span>
      <span className="flex items-center gap-1.5 shrink-0">
        <Button size="xs" variant="ghost" onClick={onDiscard}>
          Discard
        </Button>
        <Button size="xs" variant="outline" onClick={onRestore}>
          Restore
        </Button>
      </span>
    </div>
  );
}
