"use client";

import { useEffect } from "react";
import { useChatStore } from "@multica/core/chat";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { ChatFab } from "./components/chat-fab";
import { ChatWindow } from "./components/chat-window";

/**
 * Mount point for the floating chat overlay (FAB + window). Rendered once in
 * each app shell's dashboard layout; owns the two gates that decide whether the
 * overlay exists at all:
 *
 *  1. The Settings → Chat preference (`floatingChatEnabled`). When a user turns
 *     the floating window off, Chat lives only in its dedicated tab.
 *  2. The Chat tab route itself. On `/:slug/chat` the full-page surface already
 *     owns the conversation, so a floating copy of the same `activeSessionId`
 *     would be pure duplication — hide it there.
 *
 * URL deep link: `?chat=<session-id>` opens the floating window bound to that
 * session. Distinct from `?session=` on the Chat tab — the two surfaces never
 * mount together (see gate 2 above), so there is no param collision. On mount
 * the param is consumed (activeSessionId + isOpen flipped), then the URL is
 * cleaned up so a refresh does not re-open the overlay.
 */
export function FloatingChat() {
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const isOpen = useChatStore((s) => s.isOpen);
  const { pathname, searchParams, replace } = useNavigation();
  const wsPaths = useWorkspacePaths();

  // URL → store: `?chat=<session-id>` deep link.
  // Fires only when the param is present AND the store is not already pointing
  // at that session — idempotent under StrictMode double-invoke. After applying,
  // strips the param so a refresh does not re-open the overlay on top of whatever
  // the user has since navigated to.
  useEffect(() => {
    const chatParam = searchParams.get("chat");
    if (!chatParam) return;
    const live = useChatStore.getState();
    if (live.activeSessionId !== chatParam) {
      useChatStore.getState().setActiveSession(chatParam);
    }
    if (!live.isOpen) {
      useChatStore.getState().setOpen(true);
    }
    // Strip the consumed param. Use the current pathname so this works from any
    // dashboard surface (issues, inbox, agents, etc.).
    const next = new URLSearchParams(searchParams);
    next.delete("chat");
    const suffix = next.toString() ? `?${next.toString()}` : "";
    replace(`${pathname}${suffix}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- consume URL param once
  }, [searchParams.get("chat")]);

  // store → URL: when the floating window is open with a session, reflect the
  // session id in the URL so the link is shareable / survives refresh. Only
  // writes when the URL is stale — skips the no-op replace that would fight
  // with the consumer effect above on mount.
  useEffect(() => {
    if (!enabled || !isOpen) return;
    const current = searchParams.get("chat");
    if (activeSessionId === current) return;
    const next = new URLSearchParams(searchParams);
    if (activeSessionId) {
      next.set("chat", activeSessionId);
    } else {
      next.delete("chat");
    }
    const suffix = next.toString() ? `?${next.toString()}` : "";
    replace(`${pathname}${suffix}`);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- react to store changes
  }, [activeSessionId, isOpen, enabled]);

  if (!enabled) return null;
  // Suppress on the Chat tab — it renders the same conversation full-page.
  if (pathname === wsPaths.chat() || pathname.startsWith(`${wsPaths.chat()}/`)) {
    return null;
  }

  return (
    <>
      <ChatWindow />
      <ChatFab />
    </>
  );
}
