"use client";

import dynamic from "next/dynamic";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

const ChatPage = dynamic(
  () => import("@multica/views/chat").then((mod) => mod.ChatPage),
  {
    ssr: false,
    loading: () => (
      <div className="flex h-full min-h-0 items-center justify-center bg-background text-sm text-muted-foreground" />
    ),
  },
);

export function ChatRouteClient() {
  return (
    <ErrorBoundary>
      <ChatPage />
    </ErrorBoundary>
  );
}
