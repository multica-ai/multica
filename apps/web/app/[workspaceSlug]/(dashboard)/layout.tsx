"use client";

import dynamic from "next/dynamic";
import { DashboardLayout } from "@multica/views/layout/dashboard-layout";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { SearchTrigger } from "@multica/views/search/trigger";

const SearchCommand = dynamic(
  () => import("@multica/views/search/command").then((m) => m.SearchCommand),
  { ssr: false },
);
const ChatWindow = dynamic(
  () => import("@multica/views/chat/window").then((m) => m.ChatWindow),
  { ssr: false },
);
const ChatFab = dynamic(
  () => import("@multica/views/chat/fab").then((m) => m.ChatFab),
  { ssr: false },
);

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <DashboardLayout
      loadingIndicator={<MulticaIcon className="size-6" />}
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
