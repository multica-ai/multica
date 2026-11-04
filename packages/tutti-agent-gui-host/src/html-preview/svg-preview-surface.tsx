import { useEffect, useLayoutEffect, useRef, type MouseEvent, type ReactNode, type RefObject } from "react";
import { X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";

/** Promote the existing sandbox to the top layer without remounting its document. */
export function SvgPreviewSurface({ open, onOpenChange, title, closeLabel, trigger, children }: {
  open: boolean; onOpenChange(open: boolean): void; title: string; closeLabel: string;
  trigger: RefObject<HTMLButtonElement | null>; children: ReactNode;
}) {
  const wrapper = useRef<HTMLDivElement>(null);
  const dialog = useRef<HTMLDialogElement>(null);
  const close = useRef<HTMLButtonElement>(null);
  const inlineHeight = useRef(0);
  const backdropPress = useRef(false);
  useLayoutEffect(() => {
    const surface = dialog.current;
    if (!surface || !wrapper.current) return;
    if (open && !surface.open) {
      wrapper.current.style.height = `${inlineHeight.current}px`;
      surface.showModal();
      close.current?.focus({ preventScroll: true });
    } else if (!open && surface.open) {
      surface.close();
      wrapper.current.style.height = "";
      trigger.current?.focus({ preventScroll: true });
    }
    if (!open) {
      const measure = () => { if (wrapper.current && !surface.open) inlineHeight.current = wrapper.current.getBoundingClientRect().height; };
      measure();
      const observer = new ResizeObserver(measure);
      observer.observe(surface);
      return () => observer.disconnect();
    }
  }, [open, trigger]);
  useEffect(() => {
    const surface = dialog.current;
    return () => { if (surface?.open) surface.close(); };
  }, []);
  const outside = (event: MouseEvent<HTMLDialogElement>) => {
    if (event.target !== event.currentTarget) return false;
    const bounds = event.currentTarget.getBoundingClientRect();
    return event.clientX < bounds.left || event.clientX > bounds.right || event.clientY < bounds.top || event.clientY > bounds.bottom;
  };
  return <div ref={wrapper} className="flow-root">
    <dialog ref={dialog} role={open ? "dialog" : "presentation"} aria-label={open ? title : undefined}
      onCancel={(event) => { event.preventDefault(); event.stopPropagation(); onOpenChange(false); }}
      onClose={() => { if (!dialog.current?.open) onOpenChange(false); }}
      onPointerDown={(event) => { backdropPress.current = outside(event); }}
      onClick={(event) => { if (backdropPress.current && outside(event)) onOpenChange(false); backdropPress.current = false; }}
      className="relative m-0 block w-full max-w-none border-0 bg-transparent p-0 text-inherit open:fixed open:inset-0 open:m-auto open:h-[calc(100dvh-2rem)] open:max-h-none open:w-[calc(100vw-2rem)] open:overflow-hidden open:rounded-lg open:bg-background open:shadow-[var(--floating-shadow)] backdrop:bg-black/40 backdrop:backdrop-blur-xs">
      {children}
      {open && <Button ref={close} variant="ghost" size="icon-sm" aria-label={closeLabel} className="absolute right-2 top-2" onClick={() => onOpenChange(false)}><X /></Button>}
    </dialog>
  </div>;
}
