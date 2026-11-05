"use client";

import type { ReactNode } from "react";
import { Bot, ChevronRight, GitPullRequest, Laptop, Package, Plus, Search } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { formatAvatarEmoji } from "@multica/ui/lib/avatar-emoji";
import { cn } from "@multica/ui/lib/utils";
import {
  FeatureIntro,
  FeatureIntroCarousel,
  IntroArtCard,
  IntroArtTag,
} from "../../layout/feature-intro";
import { StatusIcon } from "../../issues/components/status-icon";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { useT } from "../../i18n";

const TURING = { name: "Turing", initials: "T", avatarUrl: formatAvatarEmoji("🧩"), isAgent: true };
const KEPLER = { name: "Kepler", initials: "K", avatarUrl: formatAvatarEmoji("🔭"), isAgent: true };
const MIA = { name: "Mia", initials: "M" };
const MACHINE = "studio-mbp";
const HARNESS = "Claude Code";
const MODEL = "Sonnet 4.5";
const ISSUE_ID = "MUL-142";
const PR_TITLE = "#284 fix(pricing): mobile layout";
const SOURCE_FILE = "pricing-card.tsx";
const TEST_RUN = "pnpm test pricing ✓ 12 passed";

export function AgentsIntro({
  onCreate,
  onInstallPack,
  checklist,
}: {
  onCreate: () => void;
  onInstallPack: () => void;
  checklist?: ReactNode;
}) {
  const { t } = useT("agents");

  return (
    <FeatureIntro
      footer={checklist}
      icon={Bot}
      label={t(($) => $.page.title)}
      title={t(($) => $.intro.title)}
      description={t(($) => $.intro.description)}
      docs="agents"
      columns={[
        t(($) => $.columns.agent),
        t(($) => $.columns.status),
        t(($) => $.columns.owner),
        t(($) => $.columns.runtime),
        t(($) => $.columns.model),
        t(($) => $.columns.last_active),
      ]}
      action={
        <>
          <Button variant="outline" onClick={onInstallPack}>
            <Package aria-hidden="true" />
            {t(($) => $.page.install_pack)}
          </Button>
          <Button onClick={onCreate}>
            <Plus aria-hidden="true" />
            {t(($) => $.page.new_agent)}
          </Button>
        </>
      }
    >
      <FeatureIntroCarousel
        label={t(($) => $.intro.title)}
        slides={[
          {
            id: "describe",
            title: t(($) => $.intro.slides.describe.title),
            description: t(($) => $.intro.slides.describe.description),
            art: <DescribeArt />,
          },
          {
            id: "assign",
            title: t(($) => $.intro.slides.assign.title),
            description: t(($) => $.intro.slides.assign.description),
            art: <AssignArt />,
          },
          {
            id: "deliver",
            title: t(($) => $.intro.slides.deliver.title),
            description: t(($) => $.intro.slides.deliver.description),
            art: <DeliverArt />,
          },
        ]}
      />
    </FeatureIntro>
  );
}

function DescribeArt() {
  const { t } = useT("agents");
  return (
    <IntroArtCard className="p-3">
      <div className="text-body font-medium">
        {t(($) => $.creation_studio.builder.empty_title)}
      </div>
      <div className="mt-2 min-h-14 rounded-md p-2 ring-1 ring-input">
        {t(($) => $.intro.art.prompt)}
        <span className="ml-px inline-block h-3 w-px translate-y-0.5 bg-brand" />
      </div>
      <div className="mt-2.5 flex flex-wrap items-center gap-1.5">
        <IntroArtTag>
          <Laptop className="size-3" />
          {MACHINE}
        </IntroArtTag>
        <IntroArtTag>
          <ProviderLogo provider="claude" className="size-3" />
          {HARNESS}
        </IntroArtTag>
        <IntroArtTag>{MODEL}</IntroArtTag>
      </div>
    </IntroArtCard>
  );
}

function OnlineDot() {
  return <span className="size-1.5 shrink-0 rounded-full bg-success" />;
}

function AssignArt() {
  const { t } = useT("agents");
  const { t: tIssues } = useT("issues");
  const { t: tRuntimes } = useT("runtimes");
  const online = tRuntimes(($) => $.health.online.label);
  const agents = [TURING, KEPLER];
  return (
    <div className="flex w-full flex-col">
      <IntroArtCard>
        <div className="flex items-center gap-2">
          <StatusIcon status="todo" className="size-3.5 shrink-0" />
          <span className="shrink-0 text-muted-foreground tabular-nums">{ISSUE_ID}</span>
          <span className="truncate font-medium">{t(($) => $.intro.art.issue_title)}</span>
        </div>
      </IntroArtCard>
      <IntroArtCard className="-mt-1 ml-auto w-3/4 p-0 pb-1">
        <div className="flex items-center gap-1.5 border-b px-2.5 py-1.5 text-muted-foreground">
          <Search className="size-3" />
          {tIssues(($) => $.pickers.assignee.search_placeholder)}
        </div>
        <div className="px-2.5 pt-1.5 pb-0.5 text-micro text-muted-foreground">
          {tIssues(($) => $.pickers.assignee.agents_group)}
        </div>
        {agents.map((agent, i) => (
          <div
            key={agent.name}
            className={cn("mx-1 flex items-center gap-2 rounded-md px-1.5 py-1", i === 0 && "bg-muted")}
          >
            <ActorAvatar {...agent} size="xs" />
            <span className="flex-1 truncate font-medium">{agent.name}</span>
            <span className="flex shrink-0 items-center gap-1 text-micro text-muted-foreground">
              <OnlineDot />
              {online}
            </span>
          </div>
        ))}
        <div className="px-2.5 pt-1.5 pb-0.5 text-micro text-muted-foreground">
          {tIssues(($) => $.pickers.assignee.members_group)}
        </div>
        <div className="mx-1 flex items-center gap-2 px-1.5 py-1">
          <ActorAvatar {...MIA} size="xs" />
          <span className="truncate font-medium">{MIA.name}</span>
        </div>
      </IntroArtCard>
    </div>
  );
}

function DeliverArt() {
  const { t } = useT("agents");
  const { t: tCommon } = useT("common");
  const { t: tIssues } = useT("issues");
  return (
    <IntroArtCard className="p-3">
      <div className="flex items-center gap-2">
        <ActorAvatar {...TURING} size="sm" />
        <span className="font-medium">{TURING.name}</span>
        <span className="text-muted-foreground">· {tCommon(($) => $.time.just_now)}</span>
      </div>
      <div className="mt-1.5 pl-7">
        <p>{t(($) => $.intro.art.comment)}</p>
        <div className="mt-2 flex items-center gap-1.5 rounded-md bg-muted px-2 py-1.5">
          <GitPullRequest className="size-3 shrink-0" />
          <span className="flex-1 truncate font-medium">{PR_TITLE}</span>
          <span className="shrink-0 font-medium text-success">
            {tIssues(($) => $.detail.pull_request_merge_ready)}
          </span>
        </div>
        <div className="mt-2 font-mono text-micro text-muted-foreground">
          {[t(($) => $.intro.art.step_read, { file: SOURCE_FILE }), TEST_RUN].map((step) => (
            <div key={step} className="flex items-center gap-1">
              <ChevronRight className="size-3 shrink-0" />
              <span className="truncate">{step}</span>
            </div>
          ))}
        </div>
      </div>
    </IntroArtCard>
  );
}
