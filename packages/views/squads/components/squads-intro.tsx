"use client";

import { Crown, Plus, Rocket, Users } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { formatAvatarEmoji } from "@multica/ui/lib/avatar-emoji";
import { cn } from "@multica/ui/lib/utils";
import {
  FeatureIntro,
  FeatureIntroCarousel,
  IntroArtCard,
  IntroArtConnector,
  IntroArtFork,
  IntroArtTag,
} from "../../layout/feature-intro";
import { StatusIcon } from "../../issues/components/status-icon";
import { useT } from "../../i18n";

const ADA = { name: "Ada", initials: "A", avatarUrl: formatAvatarEmoji("🌸"), isAgent: true };
const TURING = { name: "Turing", initials: "T", avatarUrl: formatAvatarEmoji("🧩"), isAgent: true };
const KEPLER = { name: "Kepler", initials: "K", avatarUrl: formatAvatarEmoji("🔭"), isAgent: true };
const MIA = { name: "Mia", initials: "M" };
const ISSUE_ID = "MUL-128";

export function SquadsIntro({
  onCreate,
  onQuickstart,
}: {
  onCreate: () => void;
  onQuickstart: () => void;
}) {
  const { t } = useT("squads");

  return (
    <FeatureIntro
      icon={Users}
      label={t(($) => $.page.title)}
      title={t(($) => $.intro.title)}
      description={t(($) => $.intro.description)}
      docs="squads"
      columns={[
        t(($) => $.page.table.name),
        t(($) => $.page.table.leader),
        t(($) => $.page.table.members),
        t(($) => $.page.table.creator),
        t(($) => $.page.table.created),
      ]}
      action={
        <>
          <Button variant="outline" onClick={onQuickstart}>
            <Rocket aria-hidden="true" />
            {t(($) => $.page.quickstart_button)}
          </Button>
          <Button onClick={onCreate}>
            <Plus aria-hidden="true" />
            {t(($) => $.page.new_button)}
          </Button>
        </>
      }
    >
      <FeatureIntroCarousel
        label={t(($) => $.intro.title)}
        slides={[
          {
            id: "route",
            title: t(($) => $.intro.slides.route.title),
            description: t(($) => $.intro.slides.route.description),
            art: <RouteArt />,
          },
          {
            id: "roles",
            title: t(($) => $.intro.slides.roles.title),
            description: t(($) => $.intro.slides.roles.description),
            art: <RolesArt />,
          },
          {
            id: "progress",
            title: t(($) => $.intro.slides.progress.title),
            description: t(($) => $.intro.slides.progress.description),
            art: <ProgressArt />,
          },
        ]}
      />
    </FeatureIntro>
  );
}

function LeaderChip() {
  const { t } = useT("squads");
  return (
    <IntroArtTag className="bg-warning/15 font-medium text-warning">
      <Crown className="size-3" />
      {t(($) => $.members_tab.leader_chip)}
    </IntroArtTag>
  );
}

function RouteArt() {
  const { t } = useT("squads");
  return (
    <div className="flex w-full flex-col">
      <IntroArtCard>
        <div className="flex items-center gap-2">
          <StatusIcon status="todo" className="size-3.5 shrink-0" />
          <span className="shrink-0 text-muted-foreground tabular-nums">{ISSUE_ID}</span>
          <span className="truncate font-medium">{t(($) => $.intro.art.issue_dark_mode)}</span>
        </div>
        <div className="mt-2 flex items-center gap-1.5 pl-5.5">
          <span className="text-muted-foreground">{t(($) => $.intro.art.assignee)}</span>
          <span className="flex min-w-0 items-center gap-1 rounded-full bg-muted py-0.5 pr-2 pl-0.5">
            <ActorAvatar name={t(($) => $.intro.art.squad_name)} initials="F" isSquad size="xs" />
            <span className="truncate font-medium">{t(($) => $.intro.art.squad_name)}</span>
          </span>
        </div>
      </IntroArtCard>
      <IntroArtConnector />
      <IntroArtCard className="mx-auto w-4/5">
        <div className="flex items-center gap-2">
          <ActorAvatar {...ADA} size="sm" />
          <span className="font-medium">{ADA.name}</span>
          <LeaderChip />
        </div>
        <div className="mt-1 truncate pl-7 text-muted-foreground">
          {t(($) => $.intro.art.delegating)}
        </div>
      </IntroArtCard>
      <IntroArtFork />
      <div className="grid grid-cols-2 gap-2">
        <MemberStatusCard
          member={TURING}
          status={t(($) => $.members_tab.status_working)}
          highlighted
        />
        <MemberStatusCard member={KEPLER} status={t(($) => $.members_tab.status_idle)} />
      </div>
    </div>
  );
}

function MemberStatusCard({
  member,
  status,
  highlighted = false,
}: {
  member: typeof TURING;
  status: string;
  highlighted?: boolean;
}) {
  return (
    <IntroArtCard highlighted={highlighted}>
      <div className="flex items-center gap-2">
        <ActorAvatar {...member} size="xs" />
        <span className="truncate font-medium">{member.name}</span>
      </div>
      <div
        className={cn(
          "mt-1 truncate pl-6",
          highlighted ? "font-medium text-brand" : "text-muted-foreground",
        )}
      >
        {status}
      </div>
    </IntroArtCard>
  );
}

function RolesArt() {
  const { t } = useT("squads");
  const rows = [
    { member: ADA, role: null },
    { member: TURING, role: t(($) => $.intro.art.role_frontend) },
    { member: KEPLER, role: t(($) => $.intro.art.role_testing) },
    { member: MIA, role: t(($) => $.intro.art.role_product) },
  ];
  return (
    <IntroArtCard className="p-1.5">
      <div className="flex items-center justify-between gap-2 px-1.5 pt-1 pb-1.5 font-medium text-muted-foreground">
        <span className="truncate">
          {t(($) => $.members_tab.section_title)} · {rows.length}
        </span>
        <span className="flex shrink-0 items-center gap-1">
          <Plus className="size-3" />
          {t(($) => $.members_tab.add_member_button)}
        </span>
      </div>
      {rows.map(({ member, role }, i) => (
        <div
          key={member.name}
          className={cn("flex items-center gap-2 rounded-md px-1.5 py-1.5", i === 0 && "bg-muted")}
        >
          <ActorAvatar {...member} size="sm" />
          <span className="flex-1 truncate font-medium">{member.name}</span>
          {role === null ? <LeaderChip /> : <IntroArtTag>{role}</IntroArtTag>}
        </div>
      ))}
    </IntroArtCard>
  );
}

function ProgressArt() {
  const { t } = useT("squads");
  const rows = [
    { id: ISSUE_ID, status: "in_progress", title: t(($) => $.intro.art.issue_dark_mode), member: TURING },
    { id: "MUL-124", status: "in_review", title: t(($) => $.intro.art.issue_board_columns), member: KEPLER },
    { id: "MUL-131", status: "todo", title: t(($) => $.intro.art.issue_inbox_gestures), member: ADA },
    { id: "MUL-117", status: "done", title: t(($) => $.intro.art.issue_paste_images), member: TURING },
  ];
  return (
    <IntroArtCard className="p-1.5">
      <div className="flex items-center gap-1.5 px-1.5 pt-1 pb-1.5">
        <ActorAvatar name={t(($) => $.intro.art.squad_name)} initials="F" isSquad size="xs" />
        <span className="truncate font-medium">{t(($) => $.intro.art.squad_name)}</span>
        <span className="shrink-0 text-muted-foreground">· {t(($) => $.detail.recent_work)}</span>
      </div>
      {rows.map((row) => (
        <div key={row.id} className="flex items-center gap-2 border-t px-1.5 py-1.5">
          <StatusIcon status={row.status} className="size-3.5 shrink-0" />
          <span className="shrink-0 text-muted-foreground tabular-nums">{row.id}</span>
          <span className="flex-1 truncate">{row.title}</span>
          <ActorAvatar {...row.member} size="xs" />
        </div>
      ))}
    </IntroArtCard>
  );
}
