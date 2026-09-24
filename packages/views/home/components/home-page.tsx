"use client";

import { useEffect, useState } from "react";
import { House } from "lucide-react";
import { useHomeLastVisitStore, useNeedsMe } from "@multica/core/home";
import { useWorkspaceId } from "@multica/core/hooks";
import { PageHeader } from "../../layout/page-header";
import { useLocale, useT } from "../../i18n";
import { ChangesSection } from "./changes-section";
import { MyPlateRail } from "./my-plate-rail";
import { NeedsMeSection } from "./needs-me-section";
import { useArchiveAnsweredMentions } from "./use-archive-answered-mentions";

export function HomePage() {
  const wsId = useWorkspaceId();
  // Keyed so a workspace switch without a remount starts a fresh visit with
  // that workspace's own "since" boundary.
  return <HomeContent key={wsId} wsId={wsId} />;
}

function HomeContent({ wsId }: { wsId: string }) {
  const { t } = useT("home");
  const locale = useLocale();
  const { items, replied, isLoading, inboxItems } = useNeedsMe(wsId);
  useArchiveAnsweredMentions(items);

  // Resolved once per mount and held: the "since" line must not move while
  // the reader is working through it. See resolveHomeSince for when it does.
  const [since] = useState(() => useHomeLastVisitStore.getState().beginVisit(wsId));
  useEffect(() => () => useHomeLastVisitStore.getState().endVisit(wsId), [wsId]);

  const today = new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    weekday: "short",
  }).format(new Date());

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader>
        <House className="size-4 text-muted-foreground" />
        <h1 className="text-body font-medium">{t(($) => $.page.title)}</h1>
        <span className="ml-auto truncate text-caption text-muted-foreground">{today}</span>
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto grid w-full max-w-[1200px] gap-x-10 gap-y-8 px-4 py-6 md:px-8 lg:grid-cols-[minmax(0,1fr)_300px]">
          <div className="flex min-w-0 flex-col gap-8">
            <NeedsMeSection items={items} replied={replied} isLoading={isLoading} />
            <ChangesSection inboxItems={inboxItems} since={since} isLoading={isLoading} />
          </div>
          <aside className="min-w-0">
            <MyPlateRail />
          </aside>
        </div>
      </div>
    </div>
  );
}
