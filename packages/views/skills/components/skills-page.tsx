"use client";

import { useMemo, useState } from "react";
import {
  AlertCircle,
  AlertTriangle,
  BookOpen,
  Plus,
  Search,
} from "lucide-react";
import type {
  AgentRuntime,
  MemberWithUser,
  Skill,
} from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { getCoreRowModel, useReactTable } from "@tanstack/react-table";
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
import { Button } from "@multica/ui/components/ui/button";
import { DataTable } from "@multica/ui/components/ui/data-table";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { canEditSkill } from "../hooks/use-can-edit-skill";
import { readOrigin } from "../lib/origin";
import { CreateSkillDialog } from "./create-skill-dialog";
import { type SkillRow, useSkillColumns } from "./skill-columns";
import { useT } from "../../i18n";

type FilterKey = "all" | "used" | "unused" | "mine";

const SCOPE_KEYS: FilterKey[] = ["all", "used", "unused", "mine"];

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
  const { t } = useT("skills");
  return (
    <PageHeader className="justify-between px-5">
      <div className="flex items-center gap-2">
        <BookOpen className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
        {totalCount > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {totalCount}
          </span>
        )}
        <p className="ml-2 hidden text-xs text-muted-foreground md:block">
          {t(($) => $.page.tagline)}{" "}
          <a
            href="https://multica.ai/docs/skills"
            target="_blank"
            rel="noopener noreferrer"
            className="underline decoration-muted-foreground/30 underline-offset-4 transition-colors hover:text-foreground"
          >
            {t(($) => $.page.learn_more)}
          </a>
        </p>
      </div>
      <Button type="button" size="sm" onClick={onCreate}>
        <Plus className="h-3 w-3" />
        {t(($) => $.page.new_skill)}
      </Button>
    </PageHeader>
  );
}

// ---------------------------------------------------------------------------
// Card toolbar — search + scope filters
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
  const { t } = useT("skills");
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t(($) => $.page.search_placeholder)}
          className="h-8 w-64 pl-8 text-sm"
        />
      </div>
      {SCOPE_KEYS.map((scope) => (
        <Tooltip key={scope}>
          <TooltipTrigger
            render={
              <Button
                variant="outline"
                size="sm"
                className={
                  filter === scope
                    ? "bg-accent text-accent-foreground hover:bg-accent/80"
                    : "text-muted-foreground"
                }
                onClick={() => setFilter(scope)}
              >
                {t(($) => $.page.scopes[scope].label)}
              </Button>
            }
          />
          <TooltipContent side="bottom">
            {t(($) => $.page.scopes[scope].description)}
          </TooltipContent>
        </Tooltip>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

function EmptyState({ onCreate }: { onCreate: () => void }) {
  const { t } = useT("skills");
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <BookOpen className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">{t(($) => $.page.empty.title)}</h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        {t(($) => $.page.empty.description)}
      </p>
      <Button type="button" onClick={onCreate} size="sm" className="mt-5">
        <Plus className="h-3 w-3" />
        {t(($) => $.page.new_skill)}
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function SkillsPage() {
  const { t } = useT("skills");
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

  const skillRows = useMemo<SkillRow[]>(() => {
    return filtered.map((skill) => {
      const origin = readOrigin(skill);
      const runtime =
        origin.type === "runtime_local" && origin.runtime_id
          ? runtimesById.get(origin.runtime_id) ?? null
          : null;
      return {
        skill,
        agents: assignments.get(skill.id) ?? [],
        creator: skill.created_by
          ? membersById.get(skill.created_by) ?? null
          : null,
        runtime,
        canEdit: canEditSkill(skill, {
          userId: currentUserId,
          role: myRole,
        }),
      };
    });
  }, [
    filtered,
    assignments,
    membersById,
    runtimesById,
    currentUserId,
    myRole,
  ]);

  const columns = useSkillColumns();

  const table = useReactTable({
    data: skillRows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    enableColumnResizing: true,
  });

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
            <p className="text-sm font-medium">
              {t(($) => $.page.list_error.title)}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {listError instanceof Error
                ? listError.message
                : t(($) => $.page.list_error.fallback)}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => refetchList()}
          >
            {t(($) => $.page.list_error.retry)}
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

      {supportingQueryDown && (
        <div
          role="status"
          className="flex shrink-0 items-start gap-2 border-b bg-warning/10 px-6 py-2 text-xs text-muted-foreground"
        >
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          <span>{t(($) => $.page.supporting_data_warning)}</span>
        </div>
      )}

      <div className="flex flex-1 min-h-0 flex-col gap-4 p-6">
        {!showEmpty && (
          <div className="max-w-3xl rounded-r-md border-l-2 border-l-brand bg-brand/5 px-4 py-3 text-xs leading-relaxed text-muted-foreground">
            <span className="font-medium text-foreground">
              {t(($) => $.page.intro_banner.title)}
            </span>{" "}
            {t(($) => $.page.intro_banner.body)}{" "}
            <span className="font-semibold text-brand">
              {t(($) => $.page.intro_banner.highlight)}
            </span>
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
                <p className="text-sm">{t(($) => $.page.no_matches.title)}</p>
                <p className="max-w-xs text-xs">
                  {search
                    ? t(($) => $.page.no_matches.with_query, {
                        query: search,
                        filterSuffix:
                          filter !== "all"
                            ? t(($) => $.page.no_matches.with_query_filter_suffix)
                            : "",
                      })
                    : t(($) => $.page.no_matches.filter_only)}
                  {t(($) => $.page.no_matches.try_different)}
                </p>
              </div>
            ) : (
              <DataTable
                table={table}
                onRowClick={(row) =>
                  navigation.push(paths.skillDetail(row.original.skill.id))
                }
              />
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
