import type { RefObject } from "react";
import type { DropAnimationFunction } from "@dnd-kit/core";
import { CSS } from "@dnd-kit/utilities";
import { prefersReducedMotion } from "@multica/core/appearance";
import { UI_EASE_OUT_CSS } from "@multica/ui/lib/motion";

export const ISSUE_DROP_DURATION_MS = 170;

/** Shared lifted overlay presentation across Board, List, and Swimlane drags. */
export const ISSUE_DRAG_OVERLAY_CLASS =
  "rotate-1 scale-[1.01] cursor-grabbing opacity-95 shadow-lg shadow-black/10 motion-reduce:rotate-0 motion-reduce:scale-100";

/**
 * Drop animation for issue drags across Board, List, and Swimlane views.
 *
 * Glides the floating overlay into the settled target slot while straightening
 * its lifted tilt/scale/shadow back to rest over 170ms (`UI_EASE_OUT_CSS`),
 * keeping the underlying slot hidden until landing so the two never flash
 * side-by-side. Skips immediately when the drag was cancelled, intercepted
 * (workflow block/handoff, archived status, sort-locked no-op), or when
 * reduced motion is enabled.
 */
export function createIssueDropAnimation(
  shouldSkipRef: RefObject<boolean>,
): DropAnimationFunction {
  return ({ active, dragOverlay, transform }) => {
    if (shouldSkipRef.current || prefersReducedMotion()) {
      return;
    }

    const overlayNode = dragOverlay.node;
    if (typeof overlayNode.animate !== "function") {
      return;
    }

    const delta = {
      x: dragOverlay.rect.left - active.rect.left,
      y: dragOverlay.rect.top - active.rect.top,
    };
    const scale = {
      scaleX:
        transform.scaleX !== 1
          ? (active.rect.width * transform.scaleX) / dragOverlay.rect.width
          : 1,
      scaleY:
        transform.scaleY !== 1
          ? (active.rect.height * transform.scaleY) / dragOverlay.rect.height
          : 1,
    };
    const finalTransform = {
      x: transform.x - delta.x,
      y: transform.y - delta.y,
      ...scale,
    };

    // `visibility`, not `opacity`: the slot's own opacity transition would
    // fade it out on drop and back in after landing.
    const previousActiveVisibility = active.node.style.visibility;
    active.node.style.visibility = "hidden";

    const innerCard = overlayNode.firstElementChild as HTMLElement | null;
    const timing: KeyframeAnimationOptions = {
      duration: ISSUE_DROP_DURATION_MS,
      easing: UI_EASE_OUT_CSS,
      fill: "forwards",
    };

    const outerAnim = overlayNode.animate(
      [
        { transform: CSS.Transform.toString(transform) ?? "none" },
        { transform: CSS.Transform.toString(finalTransform) ?? "none" },
      ],
      timing,
    );

    const innerStyle = innerCard ? getComputedStyle(innerCard) : null;
    const fromRotate =
      innerStyle?.rotate && innerStyle.rotate !== "none"
        ? innerStyle.rotate
        : "0deg";
    const fromScale =
      innerStyle?.scale && innerStyle.scale !== "none" ? innerStyle.scale : "1";

    const innerAnim =
      innerCard && innerStyle && typeof innerCard.animate === "function"
        ? innerCard.animate(
            [
              {
                // Tailwind v4 `rotate-*` and `scale-*` write the standalone
                // `rotate` and `scale` properties, not `transform`.
                rotate: fromRotate,
                scale: fromScale,
                opacity: innerStyle.opacity,
                boxShadow: innerStyle.boxShadow,
              },
              {
                rotate: "0deg",
                scale: "1",
                opacity: "1",
                boxShadow: "none",
              },
            ],
            timing,
          )
        : null;

    let cleaned = false;
    const cleanup = () => {
      if (cleaned) return;
      cleaned = true;
      active.node.style.visibility = previousActiveVisibility;
      innerAnim?.cancel();
    };

    return new Promise<void>((resolve) => {
      const done = () => {
        cleanup();
        resolve();
      };
      outerAnim.onfinish = done;
      outerAnim.oncancel = done;
    });
  };
}
