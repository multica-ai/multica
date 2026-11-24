"use client";

import type { ComponentProps, ReactNode } from "react";
import { motion } from "motion/react";
import { useReducedMotion } from "@multica/core/appearance";
import { Archive, ArchiveRestore } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { UI_EASE_OUT, UI_MOTION_DURATION } from "@multica/ui/lib/motion";
import { cn } from "@multica/ui/lib/utils";
import type { ShortcutActionId } from "@multica/core/shortcuts";
import { ActionShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";

/**
 * The floating group over an issue the Inbox page has open: the one place its
 * notification lives, whatever state that is in. Open, it offers what resolves
 * it, the issue's own actions first, then the notification's. Settled
 * (archived, snoozed), it says so, with the way back and the way on, in the
 * same place.
 *
 * It floats centred above the composer and takes no layout, so the reading
 * column never shifts when it appears, settles or goes. Its host is the
 * `@container/inbox-bar` (the reading column's width, not the window), so
 * labels give way by the room the group actually has: the issue's values and
 * the notification's keycaps first, then the notification's names. The
 * tooltips keep both.
 */
export function InboxBar({ settled = false, children }: { settled?: boolean; children: ReactNode }) {
  const reduceMotion = useReducedMotion();
  return (
    <motion.div
      role={settled ? "status" : "toolbar"}
      data-inbox-bar={settled ? "settled" : "open"}
      initial={reduceMotion ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: reduceMotion ? 0 : UI_MOTION_DURATION.fast, ease: UI_EASE_OUT }}
      className="pointer-events-auto flex min-w-0 max-w-full items-center rounded-lg border bg-background px-2 py-1.5 shadow-lg"
    >
      <motion.div
        key={settled ? "settled" : "open"}
        initial={reduceMotion ? false : { opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: reduceMotion ? 0 : UI_MOTION_DURATION.fast, ease: UI_EASE_OUT }}
        className="flex min-w-0 items-center gap-1"
      >
        {children}
      </motion.div>
    </motion.div>
  );
}

/** The open notification's group: the issue's actions, then the notification's own (`children`). */
export function InboxOpenBar({
  issueActions,
  children,
}: {
  /** Left out where the viewer cannot change the issue. */
  issueActions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <InboxBar>
      {issueActions}
      {issueActions && <InboxBarSeparator />}
      {children}
    </InboxBar>
  );
}

/**
 * What happened to a settled notification, in a word; `hint` says the rest on
 * hover, and to a screen reader with the word. A narrow group keeps only the
 * icon, so without a hint the tooltip carries the word itself.
 */
export function InboxBarStatus({ icon, label, hint }: { icon: ReactNode; label: ReactNode; hint?: ReactNode }) {
  const status = (
    <span className="flex min-w-0 items-center gap-1.5 px-1.5 text-caption text-muted-foreground">
      <span aria-hidden className="flex shrink-0 [&_svg]:size-3.5">
        {icon}
      </span>
      <span className="min-w-0 truncate @max-[32rem]/inbox-bar:sr-only">{label}</span>
      {hint && <span className="sr-only">{hint}</span>}
    </span>
  );
  return (
    <TooltipHint label={hint ?? label} side="top">
      {status}
    </TooltipHint>
  );
}

export function InboxBarSeparator() {
  return <span aria-hidden className="mx-1 h-4 w-px shrink-0 bg-border" />;
}

/**
 * A notification action in the group, named, with its shortcut beside it. A
 * narrow group drops the shortcut, then the name; the tooltip keeps both.
 */
export function InboxBarButton({
  icon,
  label,
  shortcut,
  variant = "ghost",
  className,
  ...props
}: Omit<ComponentProps<typeof Button>, "children"> & {
  icon: ReactNode;
  label: string;
  shortcut?: ShortcutActionId;
}) {
  return (
    <TooltipHint label={label} shortcut={shortcut && <ActionShortcutKeycaps action={shortcut} />} side="top">
      <Button
        variant={variant}
        size="sm"
        aria-label={label}
        className={cn("shrink-0", variant === "ghost" && "text-muted-foreground", className)}
        {...props}
      >
        {icon}
        <span className="@max-[32rem]/inbox-bar:hidden">{label}</span>
        {shortcut && <InboxBarKeycaps action={shortcut} />}
      </Button>
    </TooltipHint>
  );
}

/** A shortcut beside its action's name, the first thing a narrow group drops. */
export function InboxBarKeycaps({ action }: { action: ShortcutActionId }) {
  return (
    <span className="flex @max-[40rem]/inbox-bar:hidden">
      <ActionShortcutKeycaps action={action} />
    </span>
  );
}

/** Archiving is how an open notification is done with, so it leads. */
export function InboxArchiveButton({ onClick }: { onClick: () => void }) {
  const { t } = useT("inbox");
  return (
    <InboxBarButton
      icon={<Archive />}
      label={t(($) => $.context_menu.archive)}
      shortcut="archiveInboxItem"
      variant="default"
      onClick={onClick}
    />
  );
}

/** An archived notification's group: the way back into the Inbox. */
export function InboxArchivedBar({ onRestore }: { onRestore: () => void }) {
  const { t } = useT("inbox");
  return (
    <InboxBar settled>
      <InboxBarStatus icon={<Archive />} label={t(($) => $.kept.archived)} />
      <InboxBarButton
        icon={<ArchiveRestore />}
        label={t(($) => $.detail.restore)}
        shortcut="archiveInboxItem"
        variant="default"
        onClick={onRestore}
      />
    </InboxBar>
  );
}
