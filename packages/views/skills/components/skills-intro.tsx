"use client";

import { Laptop, Lock, Plus, Users } from "@multica/ui/icons";
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
import { useT } from "../../i18n";
import { SkillIcon } from "../lib/skill-icon";
import type { CreateSkillMethod } from "./create-skill-dialog";
import { SKILL_CREATE_METHODS, SkillCreateMenu } from "./skill-list-controls";

const AGENTS = [
  { name: "Kepler", initials: "K", avatarUrl: formatAvatarEmoji("🔭"), isAgent: true },
  { name: "Turing", initials: "T", avatarUrl: formatAvatarEmoji("🧩"), isAgent: true },
  { name: "Ada", initials: "A", avatarUrl: formatAvatarEmoji("🌸"), isAgent: true },
  { name: "Nova", initials: "N", avatarUrl: formatAvatarEmoji("✨"), isAgent: true },
];

const SHARED_SKILL = "pr-review";
const LOCAL_SKILLS = ["frontend-design", "release-notes"];
const MACHINE = "studio-mbp";
const mono = "font-mono text-caption";

export function SkillsIntro({
  onCreate,
}: {
  onCreate: (method: CreateSkillMethod) => void;
}) {
  const { t } = useT("skills");

  return (
    <FeatureIntro
      icon={SkillIcon}
      label={t(($) => $.page.title)}
      title={t(($) => $.intro.title)}
      description={t(($) => $.intro.description)}
      docs="skills"
      columns={[
        t(($) => $.table.name),
        t(($) => $.table.used_by),
        t(($) => $.table.source),
        t(($) => $.table.created_by),
        t(($) => $.table.updated),
      ]}
      action={
        <SkillCreateMenu
          onCreate={onCreate}
          align="start"
          trigger={
            <Button>
              <Plus aria-hidden="true" />
              {t(($) => $.page.new_skill)}
            </Button>
          }
        />
      }
    >
      <FeatureIntroCarousel
        label={t(($) => $.intro.title)}
        slides={[
          {
            id: "methods",
            title: t(($) => $.intro.slides.methods.title),
            description: t(($) => $.intro.slides.methods.description),
            art: <MethodsArt />,
          },
          {
            id: "assign",
            title: t(($) => $.intro.slides.assign.title),
            description: t(($) => $.intro.slides.assign.description),
            art: <AssignArt />,
          },
          {
            id: "share",
            title: t(($) => $.intro.slides.share.title),
            description: t(($) => $.intro.slides.share.description),
            art: <ShareArt />,
          },
        ]}
      />
    </FeatureIntro>
  );
}

function MethodsArt() {
  const { t } = useT("skills");
  return (
    <IntroArtCard className="p-1.5">
      {SKILL_CREATE_METHODS.map(({ key, icon: Icon }, i) => (
        <div
          key={key}
          className={cn("flex items-center gap-2.5 rounded-md p-1.5", i === 2 && "bg-muted")}
        >
          <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-surface ring-1 ring-surface-border">
            <Icon className="size-3.5" />
          </span>
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium">{t(($) => $.create.method[key].title)}</span>
            <span className="truncate text-micro text-muted-foreground">
              {t(($) => $.create.method[key].desc)}
            </span>
          </span>
        </div>
      ))}
    </IntroArtCard>
  );
}

function AssignArt() {
  const { t } = useT("skills");
  return (
    <div className="flex w-full flex-col">
      <IntroArtCard>
        <div className="flex items-center gap-2">
          <SkillIcon className="size-3.5 shrink-0" />
          <span className={cn(mono, "flex-1 truncate font-medium")}>{SHARED_SKILL}</span>
          <IntroArtTag>{t(($) => $.table.source_github)}</IntroArtTag>
        </div>
        <div className="mt-1 truncate text-muted-foreground">
          {t(($) => $.intro.art.skill_summary)}
        </div>
      </IntroArtCard>
      <IntroArtConnector />
      <IntroArtCard className="p-1.5">
        <div className="px-1.5 pt-1 pb-1.5 font-medium text-muted-foreground">
          {t(($) => $.table.used_by)}
        </div>
        {AGENTS.slice(0, 2).map((agent) => (
          <div key={agent.name} className="flex items-center gap-2 px-1.5 py-1">
            <ActorAvatar {...agent} size="sm" />
            <span className="font-medium">{agent.name}</span>
          </div>
        ))}
        <div className="flex items-center gap-2 px-1.5 py-1 text-muted-foreground">
          <Plus className="size-3.5" />
          {t(($) => $.intro.art.add_to_agent)}
        </div>
      </IntroArtCard>
    </div>
  );
}

function ShareArt() {
  const { t } = useT("skills");
  return (
    <div className="flex w-full flex-col">
      <IntroArtCard className="p-1.5">
        <div className="flex items-center gap-1.5 px-1.5 pt-1 pb-1.5 font-medium text-muted-foreground">
          <Laptop className="size-3.5 shrink-0" />
          <span className="truncate">{MACHINE} · {t(($) => $.intro.art.local_skills)}</span>
        </div>
        {LOCAL_SKILLS.map((name, i) => (
          <div
            key={name}
            className={cn("flex items-center gap-2 rounded-md px-1.5 py-1", i === 0 && "bg-muted")}
          >
            <span className={cn(mono, "flex-1 truncate")}>{name}</span>
            <span className="flex shrink-0 items-center gap-1 text-faint-foreground">
              <Lock className="size-3" />
              {t(($) => $.intro.art.private)}
            </span>
          </div>
        ))}
      </IntroArtCard>
      <IntroArtConnector />
      <IntroArtCard highlighted>
        <div className="flex items-center gap-2">
          <span className={cn(mono, "flex-1 truncate font-medium")}>{LOCAL_SKILLS[0]}</span>
          <span className="flex shrink-0 items-center gap-1 font-medium text-brand">
            <Users className="size-3.5" />
            {t(($) => $.intro.art.workspace_shared)}
          </span>
        </div>
        <div className="mt-2 flex items-center gap-1 text-muted-foreground">
          {AGENTS.map((agent) => (
            <ActorAvatar key={agent.name} {...agent} size="xs" />
          ))}
          <span className="ml-1 truncate">{t(($) => $.intro.art.all_agents)}</span>
        </div>
      </IntroArtCard>
    </div>
  );
}
