"use client";

import { cloneElement, useState, type ReactElement, type ReactNode } from "react";

/**
 * Defers mounting a popup component (Popover/Menu root + trigger machinery)
 * until the user first interacts with its trigger.
 *
 * Dense surfaces (board cards, list rows) mount several pickers per item, and
 * each one carries a Base UI root/trigger tree plus its own query
 * subscriptions — even though almost none are ever opened. Rendering a plain
 * lookalike trigger first and swapping in the real picker on interaction cuts
 * that per-item mount cost to zero for untouched items (MUL-4474 follow-up:
 * the tab-switch remount froze the main thread for seconds mostly on these).
 *
 * Upgrade triggers:
 * - `pointerenter` / `pointerdown` warm-mount the real picker *closed*. Base
 *   UI popover triggers open on the `click` event (see floating-ui
 *   `useClick`), so by the time the click lands the real trigger exists and
 *   handles it natively. Mounting on pointerdown alone would race the
 *   browser's click retargeting (mousedown target unmounts mid-gesture), so
 *   pointerenter does the work for mice; pointerdown covers stray cases.
 * - `Enter`/`Space` mount *and* open in one step: swapping elements would
 *   drop focus and swallow the key's click, so the shell opens the popup
 *   itself and lets the popup take focus.
 *
 * Only for uncontrolled usages: callers that pass `open`/`onOpenChange`/
 * `defaultOpen` need the real component from the start.
 */
export function DeferredPopup({
  trigger,
  triggerRender,
  triggerClassName,
  children,
}: {
  /** Trigger content, matching what the host passes to its popup trigger. */
  trigger?: ReactNode;
  /** Custom trigger element, matching the host's `triggerRender` prop. */
  triggerRender?: ReactElement<Record<string, unknown>>;
  /**
   * Class of the host's default trigger element. Must stay byte-identical to
   * the class the real (non-deferred) trigger renders with, so the swap is
   * invisible.
   */
  triggerClassName?: string;
  /** Renders the real component once upgraded. */
  children: (open: boolean, onOpenChange: (v: boolean) => void) => ReactNode;
}) {
  const [mounted, setMounted] = useState(false);
  const [open, setOpen] = useState(false);

  if (mounted) {
    return <>{children(open, setOpen)}</>;
  }

  const warm = () => setMounted(true);
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      e.stopPropagation();
      setMounted(true);
      setOpen(true);
    }
  };
  const handlers = {
    onPointerEnter: warm,
    onPointerDown: warm,
    onKeyDown: handleKeyDown,
    "aria-haspopup": "dialog" as const,
  };

  if (triggerRender) {
    // Mirror Base UI's render-prop semantics: the render element's own
    // children win over the component-level trigger content.
    if (triggerRender.props.children != null) {
      return cloneElement(triggerRender, handlers);
    }
    return cloneElement(triggerRender, handlers, trigger);
  }

  return (
    <button type="button" className={triggerClassName} {...handlers}>
      {trigger}
    </button>
  );
}
