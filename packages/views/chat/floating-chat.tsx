"use client";

import { useChatStore } from "@multica/core/chat";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { useIsGuest } from "@multica/core/permissions";
import { useNavigation } from "../navigation";
import { ChatFab } from "./components/chat-fab";
import { ChatWindow } from "./components/chat-window";
import { isFloatingChatRouteSuppressed } from "./floating-chat-visibility";

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
 * A guest (MUL-9420) never chats with agents, so it gets no overlay.
 */
export function FloatingChat() {
  const enabled = useChatStore((s) => s.floatingChatEnabled);
  const { pathname } = useNavigation();
  const wsPaths = useWorkspacePaths();
  const guest = useIsGuest(useCurrentWorkspace()?.id);

  if (!enabled || guest) return null;
  // Suppress on the Chat tab — it renders the same conversation full-page.
  if (isFloatingChatRouteSuppressed(pathname, wsPaths.chat())) return null;

  return (
    <>
      <ChatWindow />
      <ChatFab />
    </>
  );
}
