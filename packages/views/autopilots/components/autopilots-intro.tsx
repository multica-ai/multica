"use client";

import { CalendarClock, Plus, Zap } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { FeatureIntro } from "../../layout/feature-intro";
import { useT } from "../../i18n";
import { AUTOPILOT_TEMPLATES, type AutopilotTemplate } from "./autopilot-templates";
import { useDescribeSchedule } from "./schedule-editor/describe";

export function AutopilotsIntro({
  onCreate,
}: {
  onCreate: (template?: AutopilotTemplate) => void;
}) {
  const { t } = useT("autopilots");
  const describeSchedule = useDescribeSchedule();

  return (
    <FeatureIntro
      icon={Zap}
      label={t(($) => $.intro.label)}
      title={t(($) => $.intro.title)}
      description={t(($) => $.intro.description)}
      docs="autopilots"
      aside="wide"
      columns={[
        t(($) => $.page.table.name),
        t(($) => $.page.table.trigger),
        t(($) => $.page.table.assignee),
        t(($) => $.page.table.last_run),
        t(($) => $.page.table.next_run),
        t(($) => $.page.table.status),
      ]}
      action={
        <Button onClick={() => onCreate()}>
          <Plus aria-hidden="true" />
          {t(($) => $.page.new_autopilot)}
        </Button>
      }
    >
      <div className="text-caption font-medium text-muted-foreground">
        {t(($) => $.intro.templates)}
      </div>
      <ul
        aria-label={t(($) => $.intro.templates)}
        className="mt-3 grid grid-cols-1 gap-3 @lg/intro:grid-cols-2"
      >
        {AUTOPILOT_TEMPLATES.map((tpl) => {
          const Icon = tpl.icon;
          const schedule = describeSchedule({ ...tpl.schedule, timezone: "UTC", raw: null });
          return (
            <li key={tpl.id} className="flex">
              <button
                type="button"
                onClick={() => onCreate(tpl)}
                className="flex w-full items-start gap-3 rounded-lg bg-surface p-3 text-left shadow-[var(--menu-shadow)] ring-1 ring-surface-border transition-colors hover:bg-surface-hover focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              >
                <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-muted">
                  <Icon aria-hidden="true" className="size-4" />
                </span>
                <span className="flex min-w-0 flex-col">
                  <span className="text-body font-medium">
                    {t(($) => $.templates[tpl.id].title)}
                  </span>
                  <span className="mt-0.5 line-clamp-2 text-caption text-muted-foreground">
                    {t(($) => $.templates[tpl.id].summary)}
                  </span>
                  {schedule && (
                    <span className="mt-2 flex items-center gap-1 text-micro text-muted-foreground">
                      <CalendarClock aria-hidden="true" className="size-3 shrink-0" />
                      <span className="truncate">{schedule}</span>
                    </span>
                  )}
                </span>
              </button>
            </li>
          );
        })}
      </ul>
    </FeatureIntro>
  );
}
