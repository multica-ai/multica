"use client";

// FIR-394 — "Mind mig" on a specific chat message. A small footer action
// (mirrors the Copy button) that opens the shared reminder time-picker and
// creates a reminder anchored to THIS chat message on the unified reminder
// entity. Gated by the cerebro_reminders flag so it only appears when the
// reminder feature is on. Lives in the cerebro zone; the upstream chat message
// list pulls it in via a marked CEREBRO-PATCH import.

import { useState } from "react";
import { BellPlus } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import {
  ReminderSheet,
  useCerebroInboxStrings,
  toDateTimeLocalValue,
  nextBusinessDayNineAm,
} from "@multica/cerebro-inbox";

import { useCreateChatMessageReminder } from "../../mutations";

export function CerebroChatMessageReminderAction({
  messageId,
}: {
  messageId: string;
}) {
  const enabled = useFeatureFlag("cerebro_reminders");
  const isMobile = useIsMobile();
  const strings = useCerebroInboxStrings();
  const create = useCreateChatMessageReminder();
  const [open, setOpen] = useState(false);
  const [customReminder, setCustomReminder] = useState(() =>
    toDateTimeLocalValue(nextBusinessDayNineAm()),
  );

  if (!enabled) return null;

  // The server auto-suggests the reminder text from the chat message body, so
  // we only need to send the message id + when.
  const schedule = (remindAt: Date) => {
    create.mutate(
      { chat_message_id: messageId, remind_at: remindAt.toISOString() },
      {
        onSuccess: () => {
          setOpen(false);
          toast.success(strings.comment_remind_created);
        },
        onError: () => toast.error(strings.comment_remind_failed),
      },
    );
    setCustomReminder(toDateTimeLocalValue(remindAt));
  };

  return (
    <>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant="ghost"
              size="icon-xs"
              className="text-muted-foreground/70 hover:text-foreground"
              onClick={() => setOpen(true)}
              aria-label={strings.remind_title}
            />
          }
        >
          <BellPlus />
        </TooltipTrigger>
        <TooltipContent side="top">{strings.remind_title}</TooltipContent>
      </Tooltip>

      <ReminderSheet
        isMobile={isMobile}
        customReminder={customReminder}
        open={open}
        strings={strings}
        onCustomReminderChange={setCustomReminder}
        onOpenChange={setOpen}
        onSchedule={schedule}
      />
    </>
  );
}
