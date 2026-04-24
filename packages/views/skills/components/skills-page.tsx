"use client";

import { useMemo, useRef, useState } from "react";
import {
  AlertCircle,
  AlertTriangle,
  BookOpen,
  ChevronRight,
  Download,
  FileText,
  HardDrive,
  Lock,
  Pencil,
  Plus,
  Search,
} from "lucide-react";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
  Skill,
} from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { timeAgo } from "@multica/core/utils";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  selectSkillAssignments,
  skillListOptions,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { AppLink, useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { canEditSkill } from "../hooks/use-can-edit-skill";
import { readOrigin, totalFileCount } from "../lib/origin";
import { CreateSkillDialog } from "./create-skill-dialog";

type FilterKey = "all" | "used" | "unused" | "mine";

// ---------------------------------------------------------------------------
// Source cell — "Source · Added by" column (order matches column header).
// ---------------------------------------------------------------------------

function SourceCell({
  skill,
  creator,
  runtime,
}: {
  skill: Skill;
  creator: MemberWithUser | null;
  runtime: AgentRuntime | null;
}) {
  const origin = readOrigin(skill);

  let icon = <Pencil className="h-3 w-3 shrink-0" />;
  let label = "Created manually";
  if (origin.type === "runtime_local") {
    icon = <HardDrive className="h-3 w-3 shrink-0" />;
    label = runtime
      ? `From ${runtime.name}`
      : origin.provider
        ? `From ${origin.provider} runtime`
        : "From a runtime";
  } else if (origin.type === "clawhub") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = "From ClawHub";
  } else if (origin.type === "skills_sh") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = "From Skills.sh";
  }

  return (
    <div className="min-w-0">
      {/* `flex min-w-0` (NOT inline-flex) so truncate on the inner span fires
          when the label is longer than the grid cell. */}
      <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
        {icon}
        <span className="min-w-0 truncate">{label}</span>
      </div>
      {creator && (
        <div className="mt-1 flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
          <ActorAvatar
            name={creator.name}
            initials={creator.name.slice(0, 2).toUpperCase()}
            avatarUrl={creator.avatar_url}
            size={14}
          />
          <span className="min-w-0 truncate">by {creator.name}</span>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Agent avatar stack
// ---------------------------------------------------------------------------

function AgentAssignees({ agents }: { agents: Agent[] }) {
  if (agents.length === 0) {
    return <span className="text-xs text-muted-foreground/70">— unused</span>;
  }
  const visible = agents.slice(0, 3);
  const extra = agents.length - visible.length;
  return (
    <div className="flex items-center -space-x-1.5">
      {visible.map((a) => (
        <Tooltip key={a.id}>
          <TooltipTrigger
            render={
              <span className="inline-flex rounded-full ring-2 ring-background">
                <ActorAvatar
                  name={a.name}
                  initials={a.name.slice(0, 2).toUpperCase()}
                  avatarUrl={a.avatar_url}
                  isAgent
                  size={22}
                />
              </span>
            }
          />
          <TooltipContent>{a.name}</TooltipContent>
        </Tooltip>
      ))}
      {extra > 0 && (
        <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-muted text-xs font-medium text-muted-foreground ring-2 ring-background">
          +{extra}
        </span>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Row + header
// ---------------------------------------------------------------------------

const ROW_GRID =
  "grid grid-cols-[minmax(0,1.6fr)_minmax(0,0.8fr)_minmax(0,1.2fr)_minmax(0,6rem)_auto] items-center gap-4";

function SkillRow({
  skill,
  agents,
  creator,
  runtime,
  canEdit,
  href,
}: {
  skill: Skill;
  agents: Agent[];
  creator: MemberWithUser | null;
  runtime: AgentRuntime | null;
  canEdit: boolean;
  href: string;
}) {
  return (
    <AppLink
      href={href}
      className={`group ${ROW_GRID} border-b px-4 py-3 text-sm transition-colors hover:bg-accent/60`}
    >
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="truncate font-medium">{skill.name}</span>
          {!canEdit && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Lock className="h-3 w-3 shrink-0 text-muted-foreground/60" />
                }
              />
              <TooltipContent>
                Read-only — only creator or admin can edit
              </TooltipContent>
            </Tooltip>
          )}
          <span className="inline-flex shrink-0 items-center gap-0.5 font-mono text-xs text-muted-foreground/70">
            <FileText className="h-3 w-3" />
            {totalFileCount(skill)}
          </span>
        </div>
        <div
          className={`mt-0.5 line-clamp-1 text-xs ${
            skill.description
              ? "text-muted-foreground"
              : "italic text-muted-foreground/50"
          }`}
        >
          {skill.description || "No description"}
        </div>
      </div>
      <div className="min-w-0">
        <AgentAssignees agents={agents} />
      </div>
      <SourceCell skill={skill} creator={creator} runtime={runtime} />
      <div className="min-w-0 whitespace-nowrap text-xs text-muted-foreground">
        {timeAgo(skill.updated_at)}
      </div>
      <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/40 transition-colors group-hover:text-muted-foreground" />
    </AppLink>
  );
}

function ListColumnHeader() {
  return (
    <div
      className={`${ROW_GRID} shrink-0 border-b bg-muted/30 px-4 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground`}
    >
      <span>Name</span>
      <span>Used by</span>
      <span>Source · Added by</span>
      <span>Updated</span>
      <span className="w-4" />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Scope tab — matches Issues/MyIssues header pattern
// ---------------------------------------------------------------------------

const SCOPES: { value: FilterKey; label: string; description: string }[] = [
  { value: "all", label: "All", description: "All skills in this workspace" },
  { value: "used", label: "In use", description: "Skills assigned to at least one agent" },
  { value: "unused", label: "Unused", description: "Skills not assigned to any agent" },
  { value: "mine", label: "Created by me", description: "Skills you created" },
];

// ---------------------------------------------------------------------------
// Page header bar — uses shared PageHeader so the mobile sidebar trigger and
// h-12 chrome stay consistent with every other dashboard list page.
// ---------------------------------------------------------------------------

function PageHeaderBar({
  totalCount,
  onCreate,
}: {
  totalCount: number;
  onCreate: () => void;
}) {
  return (
    <PageHeader className="justify-between px-5">
      <div className="flex items-center gap-2">
        <BookOpen className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">Skills</h1>
        {totalCount > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {totalCount}
          </span>
        )}
      </div>
      <Button type="button" size="sm" onClick={onCreate}>
        <Plus className="h-3 w-3" />
        New skill
      </Button>
    </PageHeader>
  );
}

// ---------------------------------------------------------------------------
// Card toolbar — search + scope filters, kept inside the card because they
// operate on the table content. Page-level actions (New skill) live in the
// PageHeader instead.
// ---------------------------------------------------------------------------

function CardToolbar({
  search,
  setSearch,
  filter,
  setFilter,
}: {
  search: string;
  setSearch: (v: string) => void;
  filter: FilterKey;
  setFilter: (v: FilterKey) => void;
}) {
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search skills…"
          className="h-8 w-64 pl-8 text-sm"
        />
      </div>
      {SCOPES.map((s) => (
        <Tooltip key={s.value}>
          <TooltipTrigger
            render={
              <Button
                variant="outline"
                size="sm"
                className={
                  filter === s.value
                    ? "bg-accent text-accent-foreground hover:bg-accent/80"
                    : "text-muted-foreground"
                }
                onClick={() => setFilter(s.value)}
              >
                {s.label}
              </Button>
            }
          />
          <TooltipContent side="bottom">{s.description}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <BookOpen className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">No skills yet</h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        Create your first skill, import one from a URL, or copy one from a
        connected runtime — and every agent in the workspace can use it.
      </p>
      <Button type="button" onClick={onCreate} size="sm" className="mt-5">
        <Plus className="h-3 w-3" />
        New skill
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function SkillsPage() {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);

  const {
    data: skills = [],
    isLoading,
    error: listError,
    refetch: refetchList,
  } = useQuery(skillListOptions(wsId));
  const { data: agents = [], error: agentsError } = useQuery(
    agentListOptions(wsId),
  );
  const { data: members = [], error: membersError } = useQuery(
    memberListOptions(wsId),
  );
  const { data: runtimes = [], error: runtimesError } = useQuery(
    runtimeListOptions(wsId),
  );

  const [search, setSearch] = useState("");
  const [filter, setFilter] = useState<FilterKey>("all");
  const [createOpen, setCreateOpen] = useState(false);

  const scrollRef = useRef<HTMLDivElement>(null);
  const fadeStyle = useScrollFade(scrollRef);

  // Derive assignments ONCE per agents-identity. Stable reference across
  // unrelated agent refetches — see selectSkillAssignments' doc.
  const assignments = useMemo(
    () => selectSkillAssignments(agents),
    [agents],
  );

  const membersById = useMemo(() => {
    const map = new Map<string, MemberWithUser>();
    for (const m of members) map.set(m.user_id, m);
    return map;
  }, [members]);

  const runtimesById = useMemo(() => {
    const map = new Map<string, AgentRuntime>();
    for (const r of runtimes) map.set(r.id, r);
    return map;
  }, [runtimes]);

  const myRole =
    members.find((m) => m.user_id === currentUserId)?.role ?? null;

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const byAssignment = (s: Skill) =>
      (assignments.get(s.id)?.length ?? 0) > 0;

    return skills.filter((s) => {
      if (
        q &&
        !s.name.toLowerCase().includes(q) &&
        !s.description.toLowerCase().includes(q)
      ) {
        return false;
      }
      if (filter === "used" && !byAssignment(s)) return false;
      if (filter === "unused" && byAssignment(s)) return false;
      if (filter === "mine" && s.created_by !== currentUserId) return false;
      return true;
    });
  }, [skills, assignments, search, filter, currentUserId]);

  const handleCreated = (skill: Skill) => {
    navigation.push(paths.skillDetail(skill.id));
  };

  // --- Loading ---
  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <PageHeaderBar totalCount={0} onCreate={() => setCreateOpen(true)} />
        <div className="flex flex-1 min-h-0 flex-col gap-4 p-6">
          <div className="space-y-3 pl-4">
            <Skeleton className="h-5 w-full max-w-2xl rounded-md" />
            <Skeleton className="h-14 w-full max-w-3xl rounded-md" />
          </div>
          <div className="flex flex-1 min-h-0 flex-col overflow-hidden rounded-lg border">
            <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
              <Skeleton className="h-8 w-64 rounded-md" />
              <Skeleton className="h-7 w-12 rounded-md" />
              <Skeleton className="h-7 w-14 rounded-md" />
              <Skeleton className="h-7 w-16 rounded-md" />
            </div>
            <div className="space-y-2 p-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-14 w-full rounded-md" />
              ))}
            </div>
          </div>
        </div>
      </div>
    );
  }

  // --- List request error ---
  if (listError) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <PageHeaderBar totalCount={0} onCreate={() => setCreateOpen(true)} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <div>
            <p className="text-sm font-medium">Couldn&rsquo;t load skills</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {listError instanceof Error
                ? listError.message
                : "Something went wrong fetching the skill list."}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => refetchList()}
          >
            Try again
          </Button>
        </div>
      </div>
    );
  }

  const totalCount = skills.length;
  const showEmpty = totalCount === 0;
  const supportingQueryDown =
    !!agentsError || !!membersError || !!runtimesError;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeaderBar
        totalCount={totalCount}
        onCreate={() => setCreateOpen(true)}
      />

      {/* Non-blocking banner when supporting queries fail — list still renders
          but creator/runtime/permission attribution is incomplete. */}
      {supportingQueryDown && (
        <div
          role="status"
          className="flex shrink-0 items-start gap-2 border-b bg-warning/10 px-6 py-2 text-xs text-muted-foreground"
        >
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          <span>
            Some workspace data failed to load. Creator attribution, runtime
            names, or edit permissions may appear incomplete.
          </span>
        </div>
      )}

      {/* Page body — padding here keeps the card from touching the chrome,
          and `gap-4` separates the intro block from the table card. */}
      <div className="flex flex-1 min-h-0 flex-col gap-4 p-6">
        {!showEmpty && (
          <div className="space-y-3 pl-4">
            <p className="text-base text-foreground">
              Instructions any agent in this workspace can use.{" "}
              <a
                href="https://multica.ai/docs/skills"
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs text-muted-foreground underline decoration-muted-foreground/30 underline-offset-4 transition-colors hover:text-foreground"
              >
                Learn more about Skills →
              </a>
            </p>
            <div className="max-w-3xl rounded-r-md border-l-2 border-l-brand bg-brand/5 px-4 py-3 text-xs leading-relaxed text-muted-foreground">
              <span className="font-medium text-foreground">
                Shared with your workspace.
              </span>{" "}
              Anyone can create a skill, import one from a URL, or copy one
              from their local runtime — and every agent can use it.{" "}
              <span className="font-semibold text-brand">
                Local runtime skills stay private until you copy one here.
              </span>
            </div>
          </div>
        )}

        {showEmpty ? (
          <div className="flex flex-1 items-center justify-center">
            <EmptyState onCreate={() => setCreateOpen(true)} />
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 flex-col overflow-hidden rounded-lg border bg-background">
            <CardToolbar
              search={search}
              setSearch={setSearch}
              filter={filter}
              setFilter={setFilter}
            />
            {filtered.length === 0 ? (
              <div className="flex flex-1 flex-col items-center justify-center gap-2 px-4 py-16 text-center text-muted-foreground">
                <Search className="h-8 w-8 text-muted-foreground/40" />
                <p className="text-sm">No matches</p>
                <p className="max-w-xs text-xs">
                  {search
                    ? `No skills match "${search}"${filter !== "all" ? " in this filter" : ""}.`
                    : "No skills match this filter."}{" "}
                  Try a different query.
                </p>
              </div>
            ) : (
              <>
                <ListColumnHeader />
                <div
                  ref={scrollRef}
                  style={fadeStyle}
                  className="flex-1 min-h-0 overflow-y-auto"
                >
                  <div>
                    {filtered.map((skill) => {
                      const origin = readOrigin(skill);
                      const runtime =
                        origin.type === "runtime_local" && origin.runtime_id
                          ? runtimesById.get(origin.runtime_id) ?? null
                          : null;
                      return (
                        <SkillRow
                          key={skill.id}
                          skill={skill}
                          agents={assignments.get(skill.id) ?? []}
                          creator={
                            skill.created_by
                              ? membersById.get(skill.created_by) ?? null
                              : null
                          }
                          runtime={runtime}
                          canEdit={canEditSkill(skill, {
                            userId: currentUserId,
                            role: myRole,
                          })}
                          href={paths.skillDetail(skill.id)}
                        />
                      );
                    })}
                  </div>
                </div>
              </>
            )}
          </div>
        )}
      </div>

      {createOpen && (
        <CreateSkillDialog
          onClose={() => setCreateOpen(false)}
          onCreated={handleCreated}
        />
      )}
    </div>
  );
}
