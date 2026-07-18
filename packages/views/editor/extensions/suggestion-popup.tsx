"use client";

import type { ComponentType } from "react";
import { autoUpdate, computePosition, flip, offset, shift, size } from "@floating-ui/dom";
import { ReactRenderer } from "@tiptap/react";
import { exitSuggestion, type SuggestionKeyDownProps, type SuggestionProps } from "@tiptap/suggestion";
import type { PluginKey } from "@tiptap/pm/state";

/**
 * Keys that accept the currently highlighted suggestion row.
 *
 * `Enter` is the canonical accept (WAI-ARIA combobox guidance). Plain `Tab` is
 * an additive convenience that matches terminal / CLI / editor completion
 * muscle memory (MUL-3685). `Shift+Tab` and any `Ctrl/Cmd/Alt + Tab` are
 * deliberately NOT accept keys: they stay reverse focus navigation / OS window
 * switching, so standard keyboard accessibility is preserved.
 *
 * Centralizing the rule here keeps every picker built on
 * `createSuggestionPopupRender` (mention, slash-skill, builtin command, and any
 * future suggestion list) consistent instead of each list re-deciding what
 * counts as "accept". Callers use it in place of a bare `event.key === "Enter"`
 * check, so `Tab` becomes a strict alias of `Enter` inside their accept branch.
 */
export function isPickerAcceptKey(event: KeyboardEvent): boolean {
  if (event.key === "Enter") return true;
  return (
    event.key === "Tab" &&
    !event.shiftKey &&
    !event.ctrlKey &&
    !event.metaKey &&
    !event.altKey
  );
}

interface SuggestionPopupRenderOptions<
  TItem,
  TSelected = TItem,
  TRef = unknown,
  TComponentProps extends object = object,
> {
  pluginKey: PluginKey;
  component: ComponentType<TComponentProps>;
  getProps: (props: SuggestionProps<TItem, TSelected>) => TComponentProps;
  onKeyDown?: (
    ref: TRef | null | undefined,
    props: SuggestionKeyDownProps,
  ) => boolean;
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
  onKeyDown,
}: SuggestionPopupRenderOptions<TItem, TSelected, TRef, TComponentProps>) {
  return () => {
    let renderer: ReactRenderer<TRef> | null = null;
    let popup: HTMLDivElement | null = null;
    let removeOutsideListeners: (() => void) | null = null;
    let removeAutoUpdate: (() => void) | null = null;

    const cleanup = () => {
      removeOutsideListeners?.();
      removeOutsideListeners = null;
      removeAutoUpdate?.();
      removeAutoUpdate = null;
      renderer?.destroy();
      renderer = null;
      popup?.remove();
      popup = null;
    };

    const requestExit = (props: SuggestionProps<TItem, TSelected>) => {
      exitSuggestion(props.editor.view, pluginKey);
    };

    const isInsideSuggestionSurface = (
      target: EventTarget | null,
      props: SuggestionProps<TItem, TSelected>,
    ) => {
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
        placement: "top-start",
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
        el.dataset.side = placement.startsWith("top") ? "top" : "bottom";
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
        renderer = new ReactRenderer(component, {
          props: getProps(props),
          editor: props.editor,
        });

        const doc = props.editor.view.dom.ownerDocument;
        popup = doc.createElement("div");
        popup.style.position = "fixed";
        popup.style.zIndex = "50";
        popup.appendChild(renderer.element);
        doc.body.appendChild(popup);

        installOutsideListeners(props);
        trackPosition(popup, props.clientRect);
        updatePosition(popup, props.clientRect);
      },

      onUpdate: (props: SuggestionProps<TItem, TSelected>) => {
        renderer?.updateProps(getProps(props));
        if (popup) {
          trackPosition(popup, props.clientRect);
          updatePosition(popup, props.clientRect);
        }
      },

      onKeyDown: (props: SuggestionKeyDownProps) => {
        if (props.event.key === "Escape") {
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
