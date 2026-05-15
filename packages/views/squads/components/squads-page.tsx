"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions, memberListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { useAuthStore } from "@multica/core/auth";
import { useSquadsViewStore } from "@multica/core/squads/stores";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { Users, Plus, Search, Bot, User } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { useModalStore } from "@multica/core/modals";
import type { Agent, Squad } from "@multica/core/types";
import { useT } from "../../i18n";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

type Scope = "mine" | "all";

export function SquadsPage() {
  const { t } = useT("squads");
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const p = useWorkspacePaths();
  const currentUser = useAuthStore((s) => s.user);
  const { data: squads = [], isLoading } = useQuery({
    ...squadListOptions(wsId),
    enabled: !!wsId,
  });
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const agentsById = useMemo(() => {
    const m = new Map<string, Agent>();
    for (const a of agents) m.set(a.id, a);
    return m;
  }, [agents]);

  const membersByUserId = useMemo(() => {
    const m = new Map<string, { name: string; avatar_url: string | null }>();
    for (const mem of members) m.set(mem.user_id, { name: mem.name, avatar_url: mem.avatar_url });
    return m;
  }, [members]);

  const scope = useSquadsViewStore((s) => s.scope);
  const setScope = useSquadsViewStore((s) => s.setScope);
  const [search, setSearch] = useState("");

  const scopeCounts = useMemo(() => {
    let mine = 0;
    if (currentUser) {
      for (const s of squads) mine += s.creator_id === currentUser.id ? 1 : 0;
    }
    return { all: squads.length, mine };
  }, [squads, currentUser]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return squads.filter((s) => {
      if (scope === "mine" && currentUser && s.creator_id !== currentUser.id) return false;
      if (q && !s.name.toLowerCase().includes(q) && !matchesPinyin(s.name, q) && !s.description.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [squads, scope, currentUser, search]);

  return (
    <div className="flex flex-1 flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Users className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
          {!isLoading && squads.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{squads.length}</span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={() => useModalStore.getState().open("create-squad")}>
          <Plus className="size-3.5 mr-1.5" />
          {t(($) => $.page.new_button)}
        </Button>
      </PageHeader>

      <div className="flex-1 flex flex-col overflow-hidden">
        {isLoading ? (
          <div className="p-6 text-muted-foreground text-sm">Loading...</div>
        ) : squads.length === 0 ? (
          <div className="flex flex-col items-center justify-center gap-3 py-16 text-center">
            <Users className="size-10 text-muted-foreground/50" />
            <p className="text-sm text-muted-foreground">{t(($) => $.page.empty_no_squads)}</p>
          </div>
        ) : (
          <>
            <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
              <div className="relative">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search squads..."
                  className="h-8 w-64 pl-8 text-sm"
                />
              </div>
              <ScopeSegment scope={scope} setScope={setScope} counts={scopeCounts} />
              <span className="ml-auto font-mono text-xs tabular-nums text-muted-foreground/70">
                {filtered.length} / {squads.length}
              </span>
            </div>

            {filtered.length === 0 ? (
              <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
                <p className="text-sm text-muted-foreground">{t(($) => $.page.empty_no_match)}</p>
              </div>
            ) : (
              <div className="flex-1 overflow-y-auto p-4">
                <div className="grid gap-3">
                  {filtered.map((squad) => (
                    <SquadCard key={squad.id} squad={squad} leader={agentsById.get(squad.leader_id)} creator={membersByUserId.get(squad.creator_id)} href={p.squadDetail(squad.id)} />
                  ))}
                </div>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

function SquadCard({ squad, leader, creator, href }: { squad: Squad; leader?: Agent; creator?: { name: string; avatar_url: string | null }; href: string }) {
  return (
    <AppLink
      href={href}
      className="flex items-center gap-4 rounded-lg border p-4 hover:bg-accent/50 transition-colors"
    >
      <SquadAvatar squad={squad} />
      <div className="flex-1 min-w-0">
        <p className="font-medium truncate">{squad.name}</p>
        {squad.description && (
          <p className="text-sm text-muted-foreground truncate mt-0.5">{squad.description}</p>
        )}
        <div className="flex items-center gap-3 mt-2">
          {leader && (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Bot className="size-3" />
              <span className="truncate max-w-[120px]">{leader.name}</span>
            </div>
          )}
          {creator && (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <User className="size-3" />
              <span className="truncate max-w-[120px]">{creator.name}</span>
            </div>
          )}
        </div>
      </div>
    </AppLink>
  );
}

function ScopeSegment({ scope, setScope, counts }: { scope: Scope; setScope: (v: Scope) => void; counts: { all: number; mine: number } }) {
  return (
    <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      <ScopeButton active={scope === "mine"} label="Mine" count={counts.mine} onClick={() => setScope("mine")} />
      <ScopeButton active={scope === "all"} label="All" count={counts.all} onClick={() => setScope("all")} />
    </div>
  );
}

function ScopeButton({ active, label, count, onClick }: { active: boolean; label: string; count: number; onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium transition-colors ${
        active ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
      }`}
    >
      <span>{label}</span>
      <span className={`font-mono tabular-nums ${active ? "text-muted-foreground/80" : "text-muted-foreground/50"}`}>
        {count}
      </span>
    </button>
  );
}

function SquadAvatar({ squad }: { squad: Squad }) {
  const initials = squad.name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
  if (squad.avatar_url) {
    return (
      <ActorAvatarBase
        name={squad.name}
        initials={initials}
        avatarUrl={squad.avatar_url}
        size={36}
        className="rounded-md"
      />
    );
  }
  return (
    <div
      className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground"
      title={squad.name}
    >
      <Users className="h-4 w-4" />
    </div>
  );
}
