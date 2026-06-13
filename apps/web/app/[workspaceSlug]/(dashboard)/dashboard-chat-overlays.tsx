"use client";

import dynamic from "next/dynamic";

const ChatWindow = dynamic(
  () => import("@multica/views/chat").then((mod) => mod.ChatWindow),
  { ssr: false },
);

const ChatFab = dynamic(
  () => import("@multica/views/chat").then((mod) => mod.ChatFab),
  { ssr: false },
);

export function DashboardChatOverlays() {
  return (
    <>
      <ChatWindow />
      <ChatFab />
    </>
  );
}
