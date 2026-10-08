"use client";

import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  type MouseEvent,
  type ReactNode,
} from "react";
import { X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../i18n";

/**
 * A native top-layer dialog keeps the preview in its original DOM parent.
 * Moving an iframe into a portal would reload its browsing context, even if
 * React kept the same element. The wrapper reserves the inline height while
 * the dialog is open, so outer scroll containers keep their layout.
 */
export function HtmlPreviewSurface({
  open,
  inlineHeight,
  onOpenChange,
  title,
  children,
}: {
  open: boolean;
  /** The inline body's height, or zero when its preview is hidden. */
  inlineHeight: number;
  onOpenChange: (open: boolean) => void;
  title: string;
  children: ReactNode;
}) {
  const { t } = useT("editor");
  const titleId = useId();
  const dialogRef = useRef<HTMLDialogElement>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const backdropPress = useRef(false);

  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
      closeRef.current?.focus();
    } else if (!open && dialog.open) {
      dialog.close();
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const body = document.body;
    const previous = body.style.overflow;
    body.style.overflow = "hidden";
    return () => {
      if (body.style.overflow === "hidden") body.style.overflow = previous;
    };
  }, [open]);

  useEffect(() => {
    const dialog = dialogRef.current;
    return () => {
      if (dialog?.open) dialog.close();
    };
  }, []);

  const outside = (event: MouseEvent<HTMLDialogElement>) => {
    if (event.target !== event.currentTarget) return false;
    const rect = event.currentTarget.getBoundingClientRect();
    return (
      event.clientX < rect.left ||
      event.clientX > rect.right ||
      event.clientY < rect.top ||
      event.clientY > rect.bottom
    );
  };

  return (
    <div style={open ? { height: inlineHeight } : undefined}>
      <dialog
        ref={dialogRef}
        role={open ? "dialog" : "presentation"}
        aria-labelledby={open ? titleId : undefined}
        onClose={() => onOpenChange(false)}
        onKeyDown={(event) => {
          if (open && event.key === "Escape") event.stopPropagation();
        }}
        onPointerDown={(event) => {
          backdropPress.current = outside(event);
        }}
        onClick={(event) => {
          if (backdropPress.current && outside(event)) dialogRef.current?.close();
          backdropPress.current = false;
        }}
        className="relative m-0 block max-h-none w-full max-w-none border-0 bg-transparent p-0 text-inherit open:fixed open:inset-0 open:m-auto open:h-[min(90dvh,calc(100dvh-2rem))] open:w-[min(72rem,calc(100vw-2rem))] open:overflow-hidden open:rounded-xl open:bg-surface-raised open:shadow-[var(--floating-shadow)] open:ring-1 open:ring-surface-border backdrop:bg-black/10 backdrop:backdrop-blur-xs"
      >
        {children}
        {open && (
          <>
            <span id={titleId} className="sr-only">
              {title}
            </span>
            <Button
              ref={closeRef}
              type="button"
              variant="ghost"
              size="icon-sm"
              className="absolute top-2 right-2"
              aria-label={t(($) => $.attachment.close)}
              onClick={() => dialogRef.current?.close()}
            >
              <X />
            </Button>
          </>
        )}
      </dialog>
    </div>
  );
}
