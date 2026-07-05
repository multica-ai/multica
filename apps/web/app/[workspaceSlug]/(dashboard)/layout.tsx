"use client";

import { usePathname } from "next/navigation";
import { DashboardLayout } from "@multica/views/layout";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { SearchCommand, SearchTrigger } from "@multica/views/search";
import { ChatFab, ChatWindow } from "@multica/views/chat";
import { WebNotificationBridge } from "@/components/web-notification-bridge";

export default function Layout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const isObitaPlusRoute = pathname.endsWith("/obitaplus");

  return (
    <DashboardLayout
      loadingIndicator={<MulticaIcon className="size-6" />}
      searchSlot={<SearchTrigger />}
      insetClassName={isObitaPlusRoute ? "md:m-0 md:rounded-none" : undefined}
      extra={
        <>
          <SearchCommand />
          {!isObitaPlusRoute && (
            <>
              <ChatWindow />
              <ChatFab />
            </>
          )}
          <WebNotificationBridge />
        </>
      }
    >
      {children}
    </DashboardLayout>
  );
}
