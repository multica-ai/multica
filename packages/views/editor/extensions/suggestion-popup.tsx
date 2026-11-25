"use client";

import type { ComponentType } from "react";
import { autoUpdate, computePosition, flip, offset, shift, size } from "@floating-ui/dom";
import { ReactRenderer } from "@tiptap/react";
import { exitSuggestion, type SuggestionKeyDownProps, type SuggestionProps } from "@tiptap/suggestion";
import type { PluginKey } from "@tiptap/pm/state";
import { prefersReducedMotion } from "@multica/core/appearance";
import { contextMenuOrigin } from "@multica/ui/lib/context-menu-origin";
import { UI_EASE_OUT_CSS, UI_MOTION_DURATION } from "@multica/ui/lib/motion";

interface SuggestionPopupRenderOptions<
  TItem,
  TSelected = TItem,
  TRef = unknown,
  TComponentProps extends object = object,
> {
  pluginKey: PluginKey;
  component: ComponentType<TComponentProps>;
  getProps: (props: SuggestionProps<TItem, TSelected>) => TComponentProps;
  /** Refresh an active popup when its externally supplied catalog changes. */
  subscribe?: (refresh: () => void) => () => void;
  onKeyDown?: (
    ref: TRef | null | undefined,
    props: SuggestionKeyDownProps,
  ) => boolean;
  /** Preferred side; `flip` still moves it when that side lacks room. */
  placement?: "top-start" | "bottom-start";
}

export function createSuggestionPopupRender<
  TItem,
  TSelected = TItem,
  TRef = unknown,
  TComponentProps extends object = object,
>({
  pluginKey,
  component,
  getProps,
  subscribe,
  onKeyDown,
  placement = "top-start",
}: SuggestionPopupRenderOptions<TItem, TSelected, TRef, TComponentProps>) {
  return () => {
    let renderer: ReactRenderer<TRef> | null = null;
    let popup: HTMLDivElement | null = null;
    let removeOutsideListeners: (() => void) | null = null;
    let removeAutoUpdate: (() => void) | null = null;
    let unsubscribe: (() => void) | undefined;
    let currentProps: SuggestionProps<TItem, TSelected> | null = null;
    let entered = false;

    const cleanup = () => {
      unsubscribe?.();
      unsubscribe = undefined;
      currentProps = null;
      entered = false;
      removeOutsideListeners?.();
      removeOutsideListeners = null;
      removeAutoUpdate?.();
      removeAutoUpdate = null;
      const exitingPopup = popup;
      const exitingRenderer = renderer;
      popup = null;
      renderer = null;
      if (exitingPopup && typeof exitingPopup.animate === "function" && !prefersReducedMotion()) {
        const side = exitingPopup.dataset.side === "bottom" ? "bottom" : "top";
        const dy = side === "top" ? 4 : -4;
        exitingPopup.style.pointerEvents = "none";
        const anim = exitingPopup.animate(
          [
            { opacity: 1, transform: "translateY(0) scale(1)" },
            { opacity: 0, transform: `translateY(${dy}px) scale(0.97)` },
          ],
          { duration: UI_MOTION_DURATION.micro * 1000, easing: "cubic-bezier(0.4, 0, 1, 1)", fill: "forwards" },
        );
        const finish = () => {
          exitingRenderer?.destroy();
          exitingPopup.remove();
        };
        anim.onfinish = finish;
        anim.oncancel = finish;
      } else {
        exitingRenderer?.destroy();
        exitingPopup?.remove();
      }
    };

    const requestExit = (props: SuggestionProps<TItem, TSelected>) => {
      exitSuggestion(props.editor.view, pluginKey);
    };

    const isInsideSuggestionSurface = (
      eventTarget: EventTarget | null,
      props: SuggestionProps<TItem, TSelected>,
    ) => {
      const target = contextMenuOrigin(eventTarget);
      if (!(target instanceof Node)) return false;
      return props.editor.view.dom.contains(target) || !!popup?.contains(target);
    };

    const installOutsideListeners = (props: SuggestionProps<TItem, TSelected>) => {
      removeOutsideListeners?.();
      const doc = props.editor.view.dom.ownerDocument;
      const win = doc.defaultView ?? window;

      const onPointerDown = (event: PointerEvent) => {
        if (isInsideSuggestionSurface(event.target, props)) return;
        requestExit(props);
      };

      const onFocusIn = (event: FocusEvent) => {
        if (isInsideSuggestionSurface(event.target, props)) return;
        requestExit(props);
      };

      const onWindowBlur = () => {
        requestExit(props);
      };

      doc.addEventListener("pointerdown", onPointerDown, true);
      doc.addEventListener("focusin", onFocusIn, true);
      win.addEventListener("blur", onWindowBlur);

      removeOutsideListeners = () => {
        doc.removeEventListener("pointerdown", onPointerDown, true);
        doc.removeEventListener("focusin", onFocusIn, true);
        win.removeEventListener("blur", onWindowBlur);
      };
    };

    const updatePosition = (
      el: HTMLDivElement,
      clientRect: (() => DOMRect | null) | null | undefined,
    ) => {
      if (!clientRect) return;
      const virtualEl = {
        getBoundingClientRect: () => clientRect() ?? new DOMRect(),
      };
      computePosition(virtualEl, el, {
        // Open upward by default. The dominant hosts are bottom-anchored
        // composers (chat input, issue comment/reply) whose roomy side is above
        // the caret; preferring that side means `size` clamps to a large budget
        // with no visible compression, and `flip` only sends it down in the rare
        // case the caret sits near the viewport top. A `bottom-start` default
        // instead stayed down whenever *any* space existed below — even when far
        // more room was above — which read as "mostly opens down and gets
        // squashed". Document-body editors (issue/project description, agent
        // instructions) also host this popup; there the caret usually has room
        // both ways, so `flip` keeps them on-screen regardless of the default.
        // The Docs block menu opts into `bottom-start`: it reads down the page.
        placement,
        strategy: "fixed",
        middleware: [
          offset(6),
          flip({ padding: 8 }),
          shift({ padding: 8 }),
          size({
            padding: 8,
            apply({ availableHeight }) {
              // Publish the viewport-aware height budget as a CSS variable so
              // the list component (the real scroll container) can clamp its own
              // max-height to `min(designMax, availableHeight)`. Writing
              // maxHeight on this wrapper is inert — the wrapper does not clip,
              // the inner list scrolls — so it would let a fixed-height list
              // overflow past the viewport edge. No floor here: when space is
              // tight a short scrollable list beats one clipped off-screen.
              el.style.setProperty(
                "--suggestion-available-height",
                // Guard against a negative budget (caret scrolled just out of
                // the boundary): a negative max-height is invalid CSS and would
                // drop the clamp entirely, letting the list overflow.
                `${Math.max(0, Math.round(availableHeight))}px`,
              );
            },
          }),
        ],
      }).then(({ x, y, placement }) => {
        if (popup !== el) return;
        el.style.left = `${x}px`;
        el.style.top = `${y}px`;
        const side = placement.startsWith("top") ? "top" : "bottom";
        el.dataset.side = side;
        if (!entered) {
          entered = true;
          el.style.opacity = "";
          el.style.transformOrigin = side === "top" ? "bottom left" : "top left";
          if (typeof el.animate === "function" && !prefersReducedMotion()) {
            const dy = side === "top" ? 4 : -4;
            el.animate(
              [
                { opacity: 0, transform: `translateY(${dy}px) scale(0.97)` },
                { opacity: 1, transform: "translateY(0) scale(1)" },
              ],
              { duration: UI_MOTION_DURATION.fast * 1000, easing: UI_EASE_OUT_CSS },
            );
          }
        }
      });
    };

    const trackPosition = (
      el: HTMLDivElement,
      clientRect: (() => DOMRect | null) | null | undefined,
    ) => {
      removeAutoUpdate?.();
      removeAutoUpdate = null;
      if (!clientRect) return;
      const virtualEl = {
        getBoundingClientRect: () => clientRect() ?? new DOMRect(),
      };
      removeAutoUpdate = autoUpdate(virtualEl, el, () => updatePosition(el, clientRect), {
        ancestorResize: true,
        ancestorScroll: true,
        elementResize: true,
        layoutShift: true,
      });
    };

    return {
      onStart: (props: SuggestionProps<TItem, TSelected>) => {
        currentProps = props;
        renderer = new ReactRenderer(component, {
          props: getProps(props),
          editor: props.editor,
        });

        const doc = props.editor.view.dom.ownerDocument;
        popup = doc.createElement("div");
        popup.style.position = "fixed";
        popup.style.zIndex = "50";
        if (typeof popup.animate === "function" && !prefersReducedMotion()) {
          popup.style.opacity = "0";
        }
        const owner = props.editor.view.dom.closest("[data-editor-overlay-owner]")?.getAttribute("data-editor-overlay-owner");
        if (owner) popup.setAttribute("data-editor-overlay-owner", owner);
        popup.appendChild(renderer.element);
        doc.body.appendChild(popup);

        installOutsideListeners(props);
        trackPosition(popup, props.clientRect);
        updatePosition(popup, props.clientRect);
        unsubscribe = subscribe?.(() => {
          if (!currentProps || !renderer) return;
          renderer.updateProps(getProps(currentProps));
          if (popup) updatePosition(popup, currentProps.clientRect);
        });
      },

      onUpdate: (props: SuggestionProps<TItem, TSelected>) => {
        currentProps = props;
        renderer?.updateProps(getProps(props));
        if (popup) {
          trackPosition(popup, props.clientRect);
          updatePosition(popup, props.clientRect);
        }
      },

      onKeyDown: (props: SuggestionKeyDownProps) => {
        // `Esc` is the legacy alias the suggestion plugin also matches on.
        if (props.event.key === "Escape" || props.event.key === "Esc") {
          // Escape while a picker is open must close ONLY the picker. Returning
          // true makes ProseMirror call preventDefault(), but ProseMirror never
          // stops propagation, and Base UI's dismiss layer listens for Escape on
          // `document` in the bubble phase without consulting defaultPrevented.
          // Without stopPropagation the same keypress that closes this popup
          // also closes the host Dialog and discards the draft (MUL-5429).
          // This handler is the only layer that knows a picker is open, so
          // stopping here fixes every host at once (create-issue dialog, chat
          // input, comment composer).
          props.event.preventDefault();
          props.event.stopPropagation();
          cleanup();
          return true;
        }
        return onKeyDown?.(renderer?.ref, props) ?? false;
      },

      onExit: () => {
        cleanup();
      },
    };
  };
}
