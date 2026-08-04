"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@multica/ui/components/ui/hover-card";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useIsCoarsePointer } from "./use-coarse-pointer";

const FOCUSABLE_ANCESTOR_SELECTOR =
  'a[href], button:not([disabled]), [role="button"]:not([aria-disabled="true"]), [tabindex]:not([tabindex="-1"])';

/**
 * Shared chrome for actor profile peeks (agent / member / squad).
 *
 * - Fine pointer: HoverCard on dwell (desktop status quo).
 * - Coarse + narrow: tap opens a bottom Sheet.
 * - Coarse + wide: tap opens a controlled Popover.
 *
 * Tap open paths stop propagation so nested list-row clicks do not fire.
 */
export function ActorAvatarHoverCardShell({
  content,
  children,
}: {
  content: ReactNode;
  children: ReactNode;
}) {
  const coarse = useIsCoarsePointer();
  const isMobile = useIsMobile();

  if (!coarse) {
    return <FinePointerHoverShell content={content}>{children}</FinePointerHoverShell>;
  }
  if (isMobile) {
    return <CoarseSheetShell content={content}>{children}</CoarseSheetShell>;
  }
  return <CoarsePopoverShell content={content}>{children}</CoarsePopoverShell>;
}

function FinePointerHoverShell({
  content,
  children,
}: {
  content: ReactNode;
  children: ReactNode;
}) {
  const triggerRef = useRef<HTMLSpanElement>(null);
  const [standalone, setStandalone] = useState(false);

  useEffect(() => {
    const el = triggerRef.current;
    if (!el) return;
    const ancestor = el.parentElement?.closest(FOCUSABLE_ANCESTOR_SELECTOR);
    setStandalone(!ancestor);
  }, []);

  return (
    <HoverCard>
      <HoverCardTrigger
        render={<span ref={triggerRef} />}
        tabIndex={standalone ? 0 : -1}
        className={
          standalone
            ? "inline-flex cursor-pointer rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            : "inline-flex cursor-pointer"
        }
      >
        {children}
      </HoverCardTrigger>
      <HoverCardContent align="start" className="w-72">
        {content}
      </HoverCardContent>
    </HoverCard>
  );
}

function CoarseSheetShell({
  content,
  children,
}: {
  content: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <span
        role="button"
        tabIndex={0}
        data-slot="actor-avatar-touch-trigger"
        className="inline-flex cursor-pointer rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        onClick={(event) => {
          event.preventDefault();
          event.stopPropagation();
          setOpen(true);
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter" || event.key === " ") {
            event.preventDefault();
            event.stopPropagation();
            setOpen(true);
          }
        }}
      >
        {children}
      </span>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent
          side="bottom"
          className="max-h-[85dvh] gap-0 rounded-t-xl p-0"
          data-slot="actor-avatar-touch-sheet"
        >
          <SheetHeader className="sr-only">
            <SheetTitle>Profile</SheetTitle>
          </SheetHeader>
          <div className="overflow-y-auto p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
            {content}
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}

function CoarsePopoverShell({
  content,
  children,
}: {
  content: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <span
            data-slot="actor-avatar-touch-trigger"
            className="inline-flex cursor-pointer rounded-full focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            onClick={(event) => {
              event.preventDefault();
              event.stopPropagation();
            }}
          />
        }
      >
        {children}
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-72"
        data-slot="actor-avatar-touch-popover"
      >
        {content}
      </PopoverContent>
    </Popover>
  );
}
