"use client";

import { DashboardLayout } from "@multica/views/layout";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { SearchCommand, SearchTrigger } from "@multica/views/search";
import { ChatFab, ChatWindow } from "@multica/views/chat";
import { StarterContentPrompt } from "@multica/views/onboarding";
import { IdleDetector } from "@multica/views/time-tracking/idle-detector";
import { TimerFinalStateGuard } from "@multica/views/time-tracking/timer-final-state-guard";

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
          <StarterContentPrompt />
          <IdleDetector />
          <TimerFinalStateGuard />
        </>
      }
    >
      {children}
    </DashboardLayout>
  );
}
