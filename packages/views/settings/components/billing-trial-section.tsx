"use client";

import { Eye, Lock, Sparkles } from "@multica/ui/icons";
import { resolveWorkspaceTrial } from "@multica/core/billing/trial";
import type { PaidFeature, WorkspaceSubscriptionSummary } from "@multica/core/types";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Progress } from "@multica/ui/components/ui/progress";
import type { IconComponent } from "@multica/ui/icons";
import { useLocale, useT } from "../../i18n";
import { SettingsCard, SettingsRow, SettingsSection } from "./settings-layout";

// The paid features this section describes; not every paid feature has a
// trial summary.
type TrialFeature = Extract<PaidFeature, "private_projects" | "guests">;
const TRIAL_FEATURE_ICONS: Record<TrialFeature, IconComponent> = {
  private_projects: Lock,
  guests: Eye,
};
const TRIAL_FEATURES = Object.keys(TRIAL_FEATURE_ICONS) as TrialFeature[];

function formatDate(value: number, locale: string): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium" }).format(new Date(value));
}

/**
 * The Billing page's view of the no-card trial (MUL-9485): where the trial
 * stands and what happens to each Business feature when it ends, or, once
 * Cloud confirms it ended, a pointer to the plans below (or to an admin, for
 * someone who cannot subscribe).
 */
export function BillingTrialSection({
  summary,
  onChoosePlan,
}: {
  summary: WorkspaceSubscriptionSummary | null | undefined;
  /** Shows "Choose a plan" for a caller who may subscribe. */
  onChoosePlan?: () => void;
}) {
  const { t } = useT("billing");
  const locale = useLocale();
  // The status bar refetches the summary the moment the trial ends; this
  // section only reads it.
  const trial = resolveWorkspaceTrial(summary, Date.now());

  if (trial.kind === "ended") {
    // Only Cloud's word ends the trial here; this clock alone does not.
    if (!trial.confirmed) return null;
    const date = formatDate(trial.endsAt, locale);
    return (
      <Alert data-slot="billing-trial-ended">
        <Sparkles />
        <AlertTitle>{t(($) => $.trial.card.ended_title)}</AlertTitle>
        <AlertDescription>
          {onChoosePlan
            ? t(($) => $.trial.card.ended_description, { date })
            : t(($) => $.trial.card.ended_description_member, { date })}
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
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0 space-y-1">
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
              {/* The public pricing page is not released yet, so the plan
                  cards below are where plans are compared and chosen. */}
              {onChoosePlan ? (
                <Button size="sm" className="shrink-0" onClick={onChoosePlan}>
                  {t(($) => $.trial.card.choose_plan)}
                </Button>
              ) : null}
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
