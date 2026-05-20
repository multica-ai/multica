"use client";

import { Plug } from "lucide-react";
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@multica/ui/components/ui/empty";
import { useT } from "../../i18n";

// GitHub now lives in its own Settings tab (see github-tab.tsx). Until other
// third-party integrations land, this tab is intentionally an empty state —
// it stays in the IA so deep links and muscle memory don't break.
export function IntegrationsTab() {
  const { t } = useT("settings");
  return (
    <div className="space-y-4">
      <section className="space-y-4">
        <h2 className="text-sm font-semibold">{t(($) => $.integrations.section_title)}</h2>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <Plug className="h-4 w-4" />
            </EmptyMedia>
            <EmptyTitle>{t(($) => $.integrations.empty_title)}</EmptyTitle>
            <EmptyDescription>
              {t(($) => $.integrations.empty_description)}
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <p className="text-xs text-muted-foreground">
              {t(($) => $.integrations.manage_hint)}
            </p>
          </EmptyContent>
        </Empty>
      </section>
    </div>
  );
}
