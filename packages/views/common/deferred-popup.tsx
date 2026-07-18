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
 * Upgrade triggers — deliberately only events that END a gesture:
 * - `click` mounts the real component and opens it in one step. Opening on
 *   click matches Base UI's own trigger timing exactly, and because nothing
 *   swaps the element mid-gesture (no pointerenter/pointerdown warming),
 *   every event of the gesture lands on a node that is still attached —
 *   both for real pointers and for synthetic sequences (tests, assistive
 *   tech). The in-flight click is stopped from propagating so the popup's
 *   just-mounted outside-press dismissal doesn't treat it as an outside
 *   click and close the popover in the same breath.
 * - `Enter`/`Space` do the same for keyboard.
 *
 * Only for uncontrolled usages: callers that pass `open`/`onOpenChange`/
 * `defaultOpen` need the real component from the start.
 */
export function DeferredPopup({
  trigger,
  triggerRender,
  triggerClassName,
  ariaHasPopup = "dialog",
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
  /** ARIA popup kind of the real trigger ("dialog" for popovers, "menu" for
   *  dropdown menus) so the cold trigger reads the same to assistive tech. */
  ariaHasPopup?: "dialog" | "menu";
  /** Renders the real component once upgraded. */
  children: (open: boolean, onOpenChange: (v: boolean) => void) => ReactNode;
}) {
  const [mounted, setMounted] = useState(false);
  const [open, setOpen] = useState(false);

  if (mounted) {
    return <>{children(open, setOpen)}</>;
  }

  const mountOpen = () => {
    setMounted(true);
    setOpen(true);
  };
  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      e.stopPropagation();
      mountOpen();
    }
  };
  const handlers = {
    onClick: (e: React.MouseEvent) => {
      e.stopPropagation();
      mountOpen();
    },
    onKeyDown: handleKeyDown,
    "aria-haspopup": ariaHasPopup,
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
