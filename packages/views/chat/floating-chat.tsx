"use client";

import { useEffect } from "react";
import { useChatStore } from "@multica/core/chat";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { ChatFab } from "./components/chat-fab";
import { ChatWindow } from "./components/chat-window";
import { useChatKeyboardShortcut } from "./components/use-chat-keyboard-shortcut";

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
 * URL addressability: the floating window syncs `activeSessionId` with
 * `?chat=<session-id>` so conversations can be deep-linked and survive refresh.
 * This mirrors the ChatPage's `?session=` behavior but uses a distinct param
 * to avoid conflicts when both surfaces coexist.
 */
export function FloatingChat() {
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const { pathname, searchParams, replace } = useNavigation();
  const wsPaths = useWorkspacePaths();

  const urlChatSession = searchParams.get("chat") || null;

  // Register the keyboard shortcut (Cmd/Ctrl+K) when the floating window is enabled.
  useChatKeyboardShortcut();

  // URL → store: deep link, refresh, notification click.
  // Only sync when the floating window is enabled and we're not on the Chat tab.
  useEffect(() => {
    if (!enabled) return;
    if (urlChatSession !== useChatStore.getState().activeSessionId) {
      setActiveSession(urlChatSession);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- react to URL only
  }, [urlChatSession, enabled]);

  // store → URL: sync activeSessionId to URL when the floating window is open.
  // Only update when the window is visible to avoid fighting with ChatPage's
  // ?session= parameter.
  useEffect(() => {
    if (!enabled) return;
    const isOpen = useChatStore.getState().isOpen;
    if (!isOpen) return;

    const live = useChatStore.getState().activeSessionId;
    const current = searchParams.get("chat") || null;
    if (live !== current) {
      // Preserve other search params (like ?session= from ChatPage if somehow present).
      const params = new URLSearchParams(searchParams);
      if (live) {
        params.set("chat", live);
      } else {
        params.delete("chat");
      }
      const queryString = params.toString();
      const newUrl = queryString ? `${pathname}?${queryString}` : pathname;
      replace(newUrl);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- react to store only when open
  }, [activeSessionId, enabled, pathname, replace, searchParams]);

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
