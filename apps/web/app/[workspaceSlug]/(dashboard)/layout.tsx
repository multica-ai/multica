"use client";

import { DashboardLayout } from "@multica/views/layout";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { SearchCommand, SearchTrigger } from "@multica/views/search";
import { StarterContentPrompt } from "@multica/views/onboarding";
import {
  WORKSPACE_RAIL_WIDTH_PX,
  WorkspaceRail,
} from "@multica/views/workspace/workspace-rail";

export default function Layout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-svh w-full">
      <WorkspaceRail />
      {/* `--sidebar-offset-left` shifts the AppSidebar's fixed positioning
          by the rail width so the rail stays visible to its left. The
          sidebar primitive defaults this var to 0 elsewhere, so other
          usages are unaffected. See sidebar.tsx (sidebar-container). */}
      <div
        className="flex min-w-0 flex-1"
        style={
          {
            "--sidebar-offset-left": `${WORKSPACE_RAIL_WIDTH_PX}px`,
          } as React.CSSProperties
        }
      >
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
