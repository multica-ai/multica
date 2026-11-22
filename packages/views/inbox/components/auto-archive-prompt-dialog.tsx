"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { notificationPreferenceOptions } from "@multica/core/notification-preferences/queries";
import { useResolveAutoArchivePrompt } from "@multica/core/notification-preferences/mutations";
import {
  AUTO_ARCHIVE_PRESETS,
  autoArchivePreferences,
  type AutoArchivePreset,
} from "@multica/core/notification-preferences/auto-archive-presets";
import type { NotificationPreferences } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { toast } from "sonner";
import { useT } from "../../i18n";

const PROMPT_ORDER: readonly AutoArchivePreset[] = ["handled", "completed", "never"];

/**
 * Asks a member who archived notifications before auto-archive changed which
 * of their actions should archive them now. Shown once, the first time they
 * open a notification from the Inbox; the answer covers every workspace where
 * it is pending. Closing it keeps what they have.
 */
export function AutoArchivePromptDialog({ wsId, active }: { wsId: string; active: boolean }) {
  const { t } = useT("inbox");
  const { t: tSettings } = useT("settings");
  const { data } = useQuery(notificationPreferenceOptions(wsId));
  const resolve = useResolveAutoArchivePrompt();
  const [choice, setChoice] = useState<AutoArchivePreset>("handled");
  const [answered, setAnswered] = useState(false);

  const answer = (preferences: NotificationPreferences) => {
    setAnswered(true);
    resolve.mutate(preferences, {
      onError: (err) =>
        toast.error(
          err instanceof Error && err.message
            ? err.message
            : tSettings(($) => $.notifications.toast_failed),
        ),
    });
  };

  return (
    <Dialog
      open={active && !answered && data?.auto_archive_prompt === true}
      onOpenChange={(open) => {
        if (!open) answer({});
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.auto_archive_prompt.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.auto_archive_prompt.description)}</DialogDescription>
        </DialogHeader>
        <div
          role="radiogroup"
          aria-label={t(($) => $.auto_archive_prompt.title)}
          className="flex flex-col gap-2"
        >
          {PROMPT_ORDER.map((preset) => {
            const selected = choice === preset;
            return (
              <button
                key={preset}
                type="button"
                role="radio"
                aria-checked={selected}
                onClick={() => setChoice(preset)}
                className={cn(
                  "flex flex-col gap-1 rounded-lg border border-border px-3 py-2.5 text-left transition-colors outline-none hover:bg-surface-hover focus-visible:ring-3 focus-visible:ring-ring/50",
                  selected && "border-primary ring-1 ring-primary hover:bg-transparent",
                )}
              >
                <span className="flex items-center gap-2 text-body font-medium">
                  <span
                    aria-hidden
                    className={cn(
                      "grid size-4 shrink-0 place-content-center rounded-full border border-input",
                      selected && "border-primary",
                    )}
                  >
                    {selected && <span className="size-2 rounded-full bg-primary" />}
                  </span>
                  {tSettings(($) => $.notifications.leaving.auto_presets[preset])}
                  {preset === "handled" && (
                    <Badge variant="secondary">
                      {tSettings(($) => $.notifications.leaving.auto_recommended)}
                    </Badge>
                  )}
                </span>
                <span className="pl-6 text-caption text-muted-foreground">
                  {t(($) => $.auto_archive_prompt.options[preset])}
                </span>
              </button>
            );
          })}
        </div>
        <DialogFooter className="items-center sm:justify-between">
          <span className="text-caption text-muted-foreground">
            {t(($) => $.auto_archive_prompt.hint)}
          </span>
          <Button onClick={() => answer(autoArchivePreferences(AUTO_ARCHIVE_PRESETS[choice]))}>
            {t(($) => $.auto_archive_prompt.confirm)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
