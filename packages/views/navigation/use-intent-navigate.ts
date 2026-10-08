"use client";

import { useCallback } from "react";
import type { LinkClickIntent } from "./click-intent";
import { useNavigation } from "./context";

/**
 * Imperative sibling of AppLink / useRowLink for surfaces that receive the
 * mouse event through a callback (DataTable rows, command palette items)
 * instead of owning an anchor element: executes a resolved click intent
 * against the navigation adapter.
 *
 * Same platform semantics as useRowLink: desktop routes tab intents through
 * `openInNewTab` (background or foreground); web falls back to `window.open`
 * on the shareable URL — always a foreground browser tab, since JS cannot
 * open a background one.
 */
export function useIntentNavigate() {
  const navigation = useNavigation();

  return useCallback(
    (href: string, intent: LinkClickIntent, newTabTitle?: string) => {
      const { push, openInNewTab, getShareableUrl } = navigation;
      if (intent === "push") {
        push(href);
        return;
      }
      if (openInNewTab) {
        if (intent === "foreground-tab") {
          openInNewTab(href, newTabTitle, { activate: true });
        } else {
          openInNewTab(href, newTabTitle);
        }
        return;
      }
      window.open(getShareableUrl(href), "_blank", "noopener,noreferrer");
    },
    [navigation],
  );
}
