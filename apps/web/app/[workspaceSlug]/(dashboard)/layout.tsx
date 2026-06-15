"use client";

import { useParams } from "next/navigation";
import { DashboardLayout } from "@multica/views/layout";
import { MulticaIcon } from "@multica/ui/components/common/multica-icon";
import { SearchCommand, SearchTrigger } from "@multica/views/search";
import { StarterContentPrompt } from "@multica/views/onboarding";
// CEREBRO: in-browser push opt-in banner (TECH-3548). Browser-only by design.
import { BrowserPushPrompt } from "@multica/cerebro-notifications/views";

export default function Layout({ children }: { children: React.ReactNode }) {
  const params = useParams<{ workspaceSlug?: string }>();
  const slug = params?.workspaceSlug ?? "";

  return (
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
      {slug ? (
        <BrowserPushPrompt settingsHref={`/${slug}/settings?tab=notifications`} />
      ) : null}
      {children}
    </DashboardLayout>
  );
}
