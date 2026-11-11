"use client";

import {
  AlarmClock,
  CalendarClock,
  Clock,
  GitMerge,
  GitPullRequest,
  Plus,
  type IconComponent,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { formatAvatarEmoji } from "@multica/ui/lib/avatar-emoji";
import { cn } from "@multica/ui/lib/utils";
import {
  FeatureIntro,
  FeatureIntroCarousel,
  IntroArtCard,
  IntroArtConnector,
  IntroArtTag,
} from "../../layout/feature-intro";
import { StatusIcon } from "../../issues/components/status-icon";
import { useLocale, useT } from "../../i18n";
import { parseCron } from "./schedule-editor/cron-mapping";
import { useDescribeSchedule } from "./schedule-editor/describe";

const TURING = { name: "Turing", initials: "T", avatarUrl: formatAvatarEmoji("🧩"), isAgent: true };
const ISSUE_ID = "MUL-219";
const PR_NUMBER = "#284";
const WAKE_AT = new Date(2026, 10, 5, 14, 0);
const WEEKDAYS_AT_NINE = "0 9 * * 1-5";

export function WakeupsIntro({ onCreate }: { onCreate: () => void }) {
  const { t } = useT("autopilots");

  return (
    <FeatureIntro
      icon={AlarmClock}
      label={t(($) => $.wakeups.title)}
      title={t(($) => $.wakeups.intro.title)}
      description={t(($) => $.wakeups.intro.description)}
      docs="issue-wakeups"
      columns={[
        t(($) => $.wakeups.issue),
        t(($) => $.wakeups.target_agent),
        t(($) => $.wakeups.source),
        t(($) => $.wakeups.ends),
        t(($) => $.wakeups.runs_7d),
        t(($) => $.wakeups.enabled),
      ]}
      action={
        <Button onClick={onCreate}>
          <Plus aria-hidden="true" />
          {t(($) => $.wakeups.create)}
        </Button>
      }
    >
      <FeatureIntroCarousel
        label={t(($) => $.wakeups.intro.title)}
        slides={[
          {
            id: "event",
            title: t(($) => $.wakeups.intro.slides.event.title),
            description: t(($) => $.wakeups.intro.slides.event.description),
            art: <EventArt />,
          },
          {
            id: "time",
            title: t(($) => $.wakeups.intro.slides.time.title),
            description: t(($) => $.wakeups.intro.slides.time.description),
            art: <TimeArt />,
          },
          {
            id: "ends",
            title: t(($) => $.wakeups.intro.slides.ends.title),
            description: t(($) => $.wakeups.intro.slides.ends.description),
            art: <EndsArt />,
          },
        ]}
      />
    </FeatureIntro>
  );
}

function EventArt() {
  const { t } = useT("autopilots");
  const { t: tIssues } = useT("issues");
  return (
    <div className="flex w-full flex-col">
      <IntroArtCard>
        <div className="flex items-center gap-2">
          <StatusIcon status="in_progress" className="size-3.5 shrink-0" />
          <span className="shrink-0 text-muted-foreground tabular-nums">{ISSUE_ID}</span>
          <span className="truncate font-medium">{t(($) => $.wakeups.intro.art.issue_inbox)}</span>
        </div>
        <div className="mt-2 flex items-center gap-1.5 pl-5.5 text-muted-foreground">
          <AlarmClock className="size-3 shrink-0" />
          <span className="truncate">{tIssues(($) => $.wakeups.cond.pr_merged)}</span>
          <IntroArtTag>
            <GitMerge className="size-3" />
            {PR_NUMBER}
          </IntroArtTag>
        </div>
      </IntroArtCard>
      <IntroArtConnector />
      <IntroArtCard highlighted>
        <div className="flex items-center gap-2">
          <ActorAvatar {...TURING} size="sm" />
          <span className="font-medium">{TURING.name}</span>
          <IntroArtTag className="bg-brand/10 text-brand">
            {tIssues(($) => $.wakeups.triggered_by_wakeup)}
          </IntroArtTag>
        </div>
        <div className="mt-1 pl-7 text-muted-foreground">{t(($) => $.wakeups.intro.art.reply)}</div>
      </IntroArtCard>
    </div>
  );
}

function TimeArt() {
  const { t } = useT("autopilots");
  const locale = useLocale();
  const describeSchedule = useDescribeSchedule();
  const rows: { icon: IconComponent; label: string; detail: string }[] = [
    {
      icon: GitPullRequest,
      label: t(($) => $.wakeups.event),
      detail: t(($) => $.wakeups.intro.art.event_examples),
    },
    {
      icon: Clock,
      label: t(($) => $.wakeups.at),
      detail: new Intl.DateTimeFormat(locale, {
        month: "short",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
      }).format(WAKE_AT),
    },
    {
      icon: CalendarClock,
      label: t(($) => $.wakeups.recurring),
      detail: describeSchedule(parseCron(WEEKDAYS_AT_NINE, "UTC")) ?? WEEKDAYS_AT_NINE,
    },
  ];
  return (
    <IntroArtCard className="p-1.5">
      <div className="px-1.5 pt-1 pb-1.5 font-medium text-muted-foreground">
        {t(($) => $.wakeups.trigger)}
      </div>
      {rows.map(({ icon: Icon, label, detail }, i) => (
        <div
          key={label}
          className={cn("flex items-center gap-2 rounded-md px-1.5 py-1.5", i === rows.length - 1 && "bg-muted")}
        >
          <span className="flex size-6 shrink-0 items-center justify-center rounded-md bg-surface ring-1 ring-surface-border">
            <Icon className="size-3.5" />
          </span>
          <span className="w-16 shrink-0 truncate font-medium">{label}</span>
          <span className="truncate text-muted-foreground">{detail}</span>
        </div>
      ))}
    </IntroArtCard>
  );
}

function EndsArt() {
  const { t } = useT("autopilots");
  const rows = [
    { id: ISSUE_ID, status: "in_progress", title: t(($) => $.wakeups.intro.art.issue_inbox), ended: false },
    { id: "MUL-224", status: "in_review", title: t(($) => $.wakeups.intro.art.issue_board), ended: false },
    { id: "MUL-207", status: "done", title: t(($) => $.wakeups.intro.art.issue_paste), ended: true },
  ];
  return (
    <IntroArtCard className="p-1.5">
      {rows.map((row, i) => (
        <div
          key={row.id}
          className={cn(
            "flex items-center gap-2 px-1.5 py-1.5",
            i > 0 && "border-t",
            row.ended && "opacity-55",
          )}
        >
          <StatusIcon status={row.status} className="size-3.5 shrink-0" />
          <span className="flex-1 truncate">{row.title}</span>
          <IntroArtTag>
            {row.ended ? t(($) => $.wakeups.issue_closed) : t(($) => $.wakeups.until_issue_ends)}
          </IntroArtTag>
        </div>
      ))}
    </IntroArtCard>
  );
}
