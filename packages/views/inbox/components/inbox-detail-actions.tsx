"use client";

import { Archive, Clock } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { InboxSnoozeMenu } from "./inbox-snooze-menu";

/**
 * Snooze and archive for the notification the inbox has open, in the detail
 * header. These are the two ways to say "not now" and "done with it" without
 * replying, so they sit where the reader's eye already is, with their keys.
 *
 * `compact` drops the words: a phone header has no room for them.
 */
export function InboxDetailActions({
  compact,
  snoozed,
  snoozeOpen,
  onSnoozeOpenChange,
  onSnooze,
  onUnsnooze,
  onArchive,
}: {
  compact: boolean;
  snoozed: boolean;
  snoozeOpen: boolean;
  onSnoozeOpenChange: (open: boolean) => void;
  onSnooze: (until: Date) => void;
  onUnsnooze: () => void;
  onArchive: () => void;
}) {
  const { t } = useT("inbox");
  const snoozeLabel = t(($) => $.snooze.action);
  const archiveLabel = t(($) => $.detail.archive);

  return (
    <>
      <InboxSnoozeMenu
        open={snoozeOpen}
        onOpenChange={onSnoozeOpenChange}
        snoozed={snoozed}
        onSnooze={onSnooze}
        onUnsnooze={onUnsnooze}
        trigger={
          compact ? (
            <IconButton
              label={snoozeLabel}
              shortcut={<ActionShortcutKeycaps action="snoozeInboxItem" />}
              tooltipSide="bottom"
              className="text-muted-foreground"
            >
              <Clock />
            </IconButton>
          ) : (
            <Button variant="ghost" size="sm" className="shrink-0 gap-1.5 text-muted-foreground">
              <Clock />
              {snoozeLabel}
              <ActionShortcutKeycaps action="snoozeInboxItem" />
            </Button>
          )
        }
      />
      {compact ? (
        <IconButton
          label={archiveLabel}
          shortcut={<ActionShortcutKeycaps action="archiveInboxItem" />}
          tooltipSide="bottom"
          className="text-muted-foreground"
          onClick={onArchive}
        >
          <Archive />
        </IconButton>
      ) : (
        <Button
          variant="ghost"
          size="sm"
          className="shrink-0 gap-1.5 text-muted-foreground"
          onClick={onArchive}
        >
          <Archive />
          {archiveLabel}
          <ActionShortcutKeycaps action="archiveInboxItem" />
        </Button>
      )}
      {/* Sets the notification's two actions apart from the issue's own. */}
      {!compact && <span aria-hidden className="mx-1 h-4 w-px shrink-0 bg-border" />}
    </>
  );
}
