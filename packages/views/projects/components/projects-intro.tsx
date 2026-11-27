"use client";

import {
  Calendar,
  Check,
  ChevronDown,
  ChevronRight,
  FolderGit,
  FolderKanban,
  Plus,
} from "@multica/ui/icons";
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
import { StatusIcon } from "../../issues/components/status-icon";
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
            id: "sidebar",
            title: t(($) => $.intro.slides.sidebar.title),
            description: t(($) => $.intro.slides.sidebar.description),
            art: <SidebarArt />,
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
        <div
          className="h-full rounded-full bg-brand"
          style={{ width: `${(PROGRESS.done / PROGRESS.total) * 100}%` }}
        />
      </div>
      <div className="mt-1.5 flex items-center justify-between text-muted-foreground tabular-nums">
        <span>{t(($) => $.intro.art.issue_count, PROGRESS)}</span>
        <span>{`${Math.round((PROGRESS.done / PROGRESS.total) * 100)}%`}</span>
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 border-t pt-3 text-muted-foreground">
        <span className="flex items-center gap-1.5">
          {t(($) => $.table.lead)}
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

function SidebarArt() {
  const { t } = useT("projects");
  const rows = [
    { icon: "🚀", name: t(($) => $.intro.art.project_website), count: 8, open: true },
    { icon: "📱", name: t(($) => $.intro.art.project_mobile), count: 14, open: false },
    { icon: "🧭", name: t(($) => $.intro.art.project_onboarding), count: 5, open: false },
  ];
  const issues = [
    { status: "in_progress", title: t(($) => $.intro.art.issue_hero) },
    { status: "in_review", title: t(($) => $.intro.art.issue_pricing) },
    { status: "todo", title: t(($) => $.intro.art.issue_blog) },
  ];
  return (
    <IntroArtCard className="mx-auto w-[85%] p-1.5 text-label">
      <div className="flex items-center gap-1.5 px-2 pt-1 pb-1.5 text-caption font-medium text-muted-foreground">
        <span className="flex-1">{t(($) => $.page.title)}</span>
        <Plus className="size-3" />
      </div>
      {rows.map((row) => (
        <div key={row.icon}>
          <div
            className={cn("flex items-center gap-1.5 rounded-md px-2 py-1.5", row.open && "bg-muted")}
          >
            {row.open ? (
              <ChevronDown className="size-3 shrink-0 text-faint-foreground" />
            ) : (
              <ChevronRight className="size-3 shrink-0 text-faint-foreground" />
            )}
            <span>{row.icon}</span>
            <span className={cn("flex-1 truncate", row.open && "font-medium")}>{row.name}</span>
            <span className="text-caption text-faint-foreground tabular-nums">{row.count}</span>
          </div>
          {row.open &&
            issues.map((issue) => (
              <div
                key={issue.status}
                className="flex items-center gap-2 py-1 pr-2 pl-8 text-caption text-muted-foreground"
              >
                <StatusIcon status={issue.status} className="size-3 shrink-0" />
                <span className="truncate">{issue.title}</span>
              </div>
            ))}
        </div>
      ))}
    </IntroArtCard>
  );
}
