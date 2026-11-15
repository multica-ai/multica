const OVERLAY_OWNER = "data-editor-overlay-owner";
const origins = new WeakMap<Element, Element>();

/**
 * A ref for a context menu's popup that records the element the menu was
 * opened on. The popup is portaled to the body, so a press or focus in it
 * reads as outside every overlay around that element. Overlays that check
 * containment resolve the event through `contextMenuOrigin`; overlays that
 * check `data-editor-overlay-owner` find the origin's owner on the popup.
 * Either way, picking Cut or Paste from a field's own menu stays inside it.
 */
export function contextMenuOriginRef(origin: Element | null) {
  return (popup: Element | null) => {
    if (!popup || !origin) return;
    origins.set(popup, origin);
    const owner = origin.closest(`[${OVERLAY_OWNER}]`)?.getAttribute(OVERLAY_OWNER);
    if (owner) popup.setAttribute(OVERLAY_OWNER, owner);
    return () => {
      origins.delete(popup);
      if (owner) popup.removeAttribute(OVERLAY_OWNER);
    };
  };
}

/**
 * Where an event target belongs: the element its context menu was opened on
 * when the target is inside one, otherwise the target itself.
 */
export function contextMenuOrigin<T extends EventTarget | null>(target: T): T | Element {
  for (let node: Node | null = target instanceof Node ? target : null; node; node = node.parentNode) {
    const origin = node instanceof Element ? origins.get(node) : undefined;
    if (origin) return origin;
  }
  return target;
}
