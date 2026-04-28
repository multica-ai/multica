"use client";

import { DashboardLayout } from "@multica/views/layout";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { SearchCommand, SearchTrigger } from "@multica/views/search";
import { StarterContentPrompt } from "@multica/views/onboarding";
import { WorkspaceRail } from "@multica/views/workspace/workspace-rail";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-svh w-full">
      <WorkspaceRail />
      <div className="flex min-w-0 flex-1">
        <DashboardLayout
          loadingIndicator={<MulticaIcon className="size-6" />}
          searchSlot={<SearchTrigger />}
          extra={
            <>
              <SearchCommand />
              <StarterContentPrompt />
            </>
          }
        >
          {children}
        </DashboardLayout>
      </div>
    </div>
  );
}
