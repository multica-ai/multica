"use client";

import { DashboardLayout } from "@wallts/views/layout";
import { WalltsIcon } from "@wallts/ui/components/common/wallts-icon";
import { SearchCommand, SearchTrigger } from "@wallts/views/search";
import { ChatFab, ChatWindow } from "@wallts/views/chat";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardLayout
      loadingIndicator={<WalltsIcon className="size-6" />}
      searchSlot={<SearchTrigger />}
      extra={
        <>
          <SearchCommand />
          <ChatWindow />
          <ChatFab />
        </>
      }
    >
      {children}
    </DashboardLayout>
  );
}
