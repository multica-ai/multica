"use client";

import { ChatSessionHistoryPanel, ChatWindow } from "./chat-window";

export function ChatPage() {
  return (
    <div className="flex h-full min-h-0 flex-row bg-background">
      <div className="min-w-0 flex-1">
        <ChatWindow variant="page" showSessionHistoryTrigger={false} />
      </div>
      <ChatSessionHistoryPanel />
    </div>
  );
}
