"use client";

import { Calendar, Check, FolderGit, FolderKanban, Pin, Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { formatAvatarEmoji } from "@multica/ui/lib/avatar-emoji";
import { cn } from "@multica/ui/lib/utils";
import { PROJECT_STATUS_CONFIG } from "@multica/core/projects/config";
import {
  FeatureIntro,
  FeatureIntroCarousel,
  IntroArtCard,
  IntroArtConnector,
  IntroArtTag,
} from "../../layout/feature-intro";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { useLocale, useT } from "../../i18n";
import { useProjectPriorityLabels, useProjectStatusLabels } from "./labels";

const TARGET_DATE = new Date(2026, 10, 20);
const PROGRESS = { done: 13, total: 21 };
const LEAD = { name: "Mia", initials: "M" };
const AGENT = { name: "Turing", initials: "T", avatarUrl: formatAvatarEmoji("🧩"), isAgent: true };
const REPOS = ["acme/website", "acme/brand"];

export function ProjectsIntro({ onCreate }: { onCreate: () => void }) {
  const { t } = useT("projects");

  return (
    <FeatureIntro
      icon={FolderKanban}
      label={t(($) => $.page.title)}
      title={t(($) => $.intro.title)}
      description={t(($) => $.intro.description)}
      docs="projects"
      columns={[
        t(($) => $.table.name),
        t(($) => $.table.priority),
        t(($) => $.table.status),
        t(($) => $.table.progress),
        t(($) => $.table.lead),
        t(($) => $.table.issues),
      ]}
      action={
        <Button onClick={onCreate}>
          <Plus aria-hidden="true" />
          {t(($) => $.page.new_project)}
        </Button>
      }
    >
      <FeatureIntroCarousel
        label={t(($) => $.intro.title)}
        slides={[
          {
            id: "progress",
            title: t(($) => $.intro.slides.progress.title),
            description: t(($) => $.intro.slides.progress.description),
            art: <ProgressArt />,
          },
          {
            id: "context",
            title: t(($) => $.intro.slides.context.title),
            description: t(($) => $.intro.slides.context.description),
            art: <ContextArt />,
          },
          {
            id: "pinned",
            title: t(($) => $.intro.slides.pinned.title),
            description: t(($) => $.intro.slides.pinned.description),
            art: <PinnedArt />,
          },
        ]}
      />
    </FeatureIntro>
  );
}

function ProgressArt() {
  const { t } = useT("projects");
  const locale = useLocale();
  const statusLabels = useProjectStatusLabels();
  const priorityLabels = useProjectPriorityLabels();
  const status = PROJECT_STATUS_CONFIG.in_progress;

  return (
    <IntroArtCard className="p-3">
      <div className="flex items-center gap-2">
        <span className="text-body">🚀</span>
        <span className="flex-1 truncate text-body font-medium">
          {t(($) => $.intro.art.project_website)}
        </span>
        <span
          className={cn(
            "shrink-0 rounded-xs px-1.5 py-0.5 font-medium",
            status.badgeBg,
            status.badgeText,
          )}
        >
          {statusLabels.in_progress}
        </span>
      </div>
      <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-muted">
        <div className="h-full w-[62%] rounded-full bg-brand" />
      </div>
      <div className="mt-1.5 flex items-center justify-between text-muted-foreground tabular-nums">
        <span>{t(($) => $.table.progress)}</span>
        <span>{`${PROGRESS.done} / ${PROGRESS.total}`}</span>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 border-t pt-3 text-muted-foreground">
        <span className="flex items-center gap-1.5">
          <ActorAvatar {...LEAD} size="xs" />
          <span className="text-foreground">{LEAD.name}</span>
        </span>
        <span className="flex items-center gap-1">
          <Calendar className="size-3.5" />
          {new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }).format(TARGET_DATE)}
        </span>
        <span className="flex items-center gap-1">
          <PriorityIcon priority="high" />
          {priorityLabels.high}
        </span>
      </div>
    </IntroArtCard>
  );
}

function ContextArt() {
  const { t } = useT("projects");
  return (
    <div className="flex w-full flex-col">
      <IntroArtCard>
        <div className="font-medium text-muted-foreground">
          {t(($) => $.intro.art.context_label)}
        </div>
        <p className="mt-1 line-clamp-3">{t(($) => $.intro.art.context_body)}</p>
        <div className="mt-2 flex flex-wrap gap-1.5">
          {REPOS.map((repo) => (
            <IntroArtTag key={repo}>
              <FolderGit className="size-3" />
              {repo}
            </IntroArtTag>
          ))}
        </div>
      </IntroArtCard>
      <IntroArtConnector />
      <IntroArtCard highlighted>
        <div className="flex items-center gap-2">
          <ActorAvatar {...AGENT} size="sm" />
          <span className="font-medium">{AGENT.name}</span>
          <span className="truncate text-muted-foreground">{t(($) => $.intro.art.run_started)}</span>
        </div>
        <div className="mt-1 flex items-center gap-1 pl-7 font-medium text-brand">
          <Check className="size-3.5 shrink-0" />
          <span className="truncate">{t(($) => $.intro.art.context_loaded)}</span>
        </div>
      </IntroArtCard>
    </div>
  );
}

function PinnedArt() {
  const { t } = useT("projects");
  const rows = [
    { icon: "🚀", name: t(($) => $.intro.art.project_website), count: 21 },
    { icon: "📱", name: t(($) => $.intro.art.project_mobile), count: 14 },
    { icon: "🧭", name: t(($) => $.intro.art.project_onboarding), count: 5 },
  ];
  return (
    <IntroArtCard className="mx-auto w-4/5 p-1.5 text-label">
      <div className="flex items-center gap-1.5 px-2 pt-1 pb-1.5 text-caption font-medium text-muted-foreground">
        <Pin className="size-3" />
        {t(($) => $.intro.art.pinned)}
      </div>
      {rows.map((row, i) => (
        <div
          key={row.icon}
          className={cn("flex items-center gap-2 rounded-md px-2 py-1.5", i === 0 && "bg-muted")}
        >
          <span>{row.icon}</span>
          <span className={cn("flex-1 truncate", i === 0 && "font-medium")}>{row.name}</span>
          <span className="text-caption text-faint-foreground tabular-nums">{row.count}</span>
        </div>
      ))}
    </IntroArtCard>
  );
}
