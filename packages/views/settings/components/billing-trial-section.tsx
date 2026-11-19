"use client";

import { Lock, Sparkles } from "@multica/ui/icons";
import { resolveWorkspaceTrial } from "@multica/core/billing/trial";
import type { PaidFeature, WorkspaceSubscriptionSummary } from "@multica/core/types";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Progress } from "@multica/ui/components/ui/progress";
import type { IconComponent } from "@multica/ui/icons";
import { useLocale, useT } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

const TRIAL_FEATURE_ICONS: Record<PaidFeature, IconComponent> = {
  private_projects: Lock,
};
const TRIAL_FEATURES = Object.keys(TRIAL_FEATURE_ICONS) as PaidFeature[];

function formatDate(value: number, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}

/**
 * The Billing page's view of the no-card trial (MUL-9485): where the trial
 * stands and what happens to each Business feature when it ends, or, once it
 * has ended, a pointer to the plans below.
 */
export function BillingTrialSection({
  summary,
}: {
  summary: WorkspaceSubscriptionSummary | null | undefined;
}) {
  const { t } = useT("billing");
  const locale = useLocale();
  // The status bar refetches the summary the moment the trial ends; this
  // section only reads it.
  const trial = resolveWorkspaceTrial(summary, Date.now());

  if (trial.kind === "ended") {
    return (
      <Alert data-slot="billing-trial-ended">
        <Sparkles />
        <AlertTitle>{t(($) => $.trial.card.ended_title)}</AlertTitle>
        <AlertDescription>
          {t(($) => $.trial.card.ended_description, { date: formatDate(trial.endsAt, locale) })}
        </AlertDescription>
      </Alert>
    );
  }
  if (trial.kind !== "active") return null;

  const endsOn = formatDate(trial.endsAt, locale);
  return (
    <>
      <SettingsSection title={t(($) => $.trial.card.title)}>
        <SettingsCard>
          <div className="space-y-4 p-4 sm:p-5" data-slot="billing-trial">
            <div className="space-y-1">
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="text-title-sm font-semibold">{t(($) => $.workspace.plan.business)}</h3>
                <Badge
                  variant="secondary"
                  className={trial.warning ? "bg-warning/10 text-warning tabular-nums" : "bg-brand/10 text-brand tabular-nums"}
                >
                  {t(($) => $.trial.card.badge, { count: trial.daysLeft })}
                </Badge>
              </div>
              <p className="max-w-[65ch] text-body text-muted-foreground">
                {t(($) => $.trial.card.description, { date: endsOn })}
              </p>
            </div>
            <Progress
              value={(trial.day / trial.totalDays) * 100}
              aria-label={t(($) => $.trial.card.progress, { day: trial.day, total: trial.totalDays })}
            />
            <div className="flex flex-wrap justify-between gap-2 text-caption text-muted-foreground tabular-nums">
              <span>{t(($) => $.trial.card.started, { date: formatDate(trial.startedAt, locale) })}</span>
              <span>{t(($) => $.trial.card.progress, { day: trial.day, total: trial.totalDays })}</span>
              <span>{t(($) => $.trial.card.ends, { date: endsOn })}</span>
            </div>
          </div>
        </SettingsCard>
      </SettingsSection>
      <SettingsSection
        title={t(($) => $.trial.features.title)}
        description={t(($) => $.trial.features.description)}
      >
        <SettingsCard>
          {TRIAL_FEATURES.map((feature) => {
            const Icon = TRIAL_FEATURE_ICONS[feature];
            return (
              <SettingsRow
                key={feature}
                label={
                  <span className="inline-flex items-center gap-2">
                    <Icon aria-hidden="true" className="size-4 text-muted-foreground" />
                    {t(($) => $.trial.features[feature].name)}
                  </span>
                }
                description={t(($) => $.trial.features[feature].after)}
              >
                {null}
              </SettingsRow>
            );
          })}
        </SettingsCard>
      </SettingsSection>
    </>
  );
}
