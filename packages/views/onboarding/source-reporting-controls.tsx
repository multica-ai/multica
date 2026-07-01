"use client";

import { useId } from "react";
import { Switch } from "@multica/ui/components/ui/switch";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

export function SourceReportingControls({
  domainConsent,
  onDomainConsentChange,
  className,
}: {
  domainConsent: boolean;
  onDomainConsentChange: (enabled: boolean) => void;
  className?: string;
}) {
  const { t } = useT("onboarding");
  const labelId = useId();

  return (
    <section
      className={cn(
        "rounded-lg border bg-muted/30 p-4",
        className,
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
        <p className="min-w-0 flex-1 basis-64 text-sm leading-relaxed text-muted-foreground">
          {t(($) => $.source_reporting.description)}
        </p>

        <div className="flex shrink-0 items-center gap-3">
          <div id={labelId} className="text-sm font-medium text-foreground">
            {t(($) => $.source_reporting.domain.label)}
          </div>
          <Switch
            checked={domainConsent}
            onCheckedChange={onDomainConsentChange}
            aria-labelledby={labelId}
          />
        </div>
      </div>
    </section>
  );
}
