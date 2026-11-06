"use client";

import { useEffect, useMemo, useState } from "react";
import {
  GitBranch,
  RefreshCw,
  Search,
  Trash2,
  UserPlus,
} from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  codeGraphRepositoriesOptions,
  codeGraphRepositoryOptions,
} from "@multica/core/workspace/queries";
import {
  useDeleteCodeGraphRepository,
  useCancelCodeGraphRepository,
  useSyncCodeGraphRepository,
} from "@multica/core/workspace/mutations";
import type {
  CodeGraphRepository,
  CodeGraphRepositoryStatus,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { CodeGraphTools } from "./code-graph-tools";
import {
  GitCredentialManager,
  RepositoryGitAuth,
  useCanManageGit,
} from "./git-credentials";
import { RegisterRepositoryDialog } from "./register-repository-dialog";
import { RepositoryUsageScope } from "./repository-usage-scope";
import { CodeGraphIntro } from "./code-graph-intro";
import { featureDocsHref } from "../../layout/feature-intro";
import { useLocale } from "../../i18n";
import { RepositoryAssignmentsDialog } from "./repository-assignments-dialog";
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogTrigger,
} from "@multica/ui/components/ui/alert-dialog";

const STATUS_FILTERS = ["", "ready", "processing", "failed"] as const;

function statusVariant(status: CodeGraphRepositoryStatus) {
  if (status === "failed") return "destructive" as const;
  if (status === "ready") return "secondary" as const;
  return "outline" as const;
}

export function CodeGraphPage() {
  const { t } = useT("layout");
  const wsId = useWorkspaceId();
  const locale = useLocale();
  const [search, setSearch] = useState("");
  const [status, setStatus] = useState<"" | CodeGraphRepositoryStatus>("");
  const [assigningRepo, setAssigningRepo] =
    useState<CodeGraphRepository | null>(null);
  const canManage = useCanManageGit();
  const filters = useMemo(() => ({ search, status }), [search, status]);
  const { data, isLoading } = useQuery(
    codeGraphRepositoriesOptions(wsId, filters),
  );
  const syncRepo = useSyncCodeGraphRepository(wsId);

  const items = data?.items ?? [];
  const stats = data?.stats ?? {
    total: 0,
    ready: 0,
    processing: 0,
    pending: 0,
    failed: 0,
    files: 0,
  };
  function openAssignDialog(repo: CodeGraphRepository) {
    setAssigningRepo(repo);
  }

  if (data && stats.total === 0 && !search && !status)
    return <CodeGraphIntro wsId={wsId} canManage={canManage} />;

  return (
    <div className="flex h-full min-h-0 flex-col bg-background">
      <div className="shrink-0 border-b border-border bg-surface px-6 py-5">
        <div className="flex flex-col gap-4 md:flex-row md:items-start md:justify-between">
          <div>
            <h1 className="text-title-lg font-semibold tracking-normal text-foreground">
              {t(($) => $.nav.code_graph)}
            </h1>
            <p className="mt-2 text-body text-muted-foreground">
              {t(($) => $.code_graph.count, { count: stats.total })}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <a
              href={featureDocsHref("codegraph", locale)}
              target="_blank"
              rel="noopener noreferrer"
              className="self-center px-2 text-caption underline underline-offset-4"
            >
              {t(($) => $.feature_intro.read_docs)}
            </a>
            <GitCredentialManager />
            {canManage && <RegisterRepositoryDialog wsId={wsId} />}
          </div>
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-6 overflow-auto px-6 py-6">
        {syncRepo.error ? (
          <p role="alert" className="text-destructive">
            {syncRepo.error.message}
          </p>
        ) : null}
        <div className="grid shrink-0 gap-3 md:grid-cols-5">
          <Stat label={t(($) => $.code_graph.total)} value={stats.total} />
          <Stat label={t(($) => $.code_graph.ready)} value={stats.ready} />
          <Stat
            label={t(($) => $.code_graph.processing)}
            value={stats.processing}
          />
          <Stat label={t(($) => $.code_graph.pending)} value={stats.pending} />
          <Stat label={t(($) => $.code_graph.files)} value={stats.files} />
        </div>

        <div className="flex shrink-0 flex-col gap-3 md:flex-row md:items-center md:justify-end">
          <div className="relative w-full md:w-96">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="pl-9"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
              placeholder={t(($) => $.code_graph.search)}
            />
          </div>
          <div className="flex overflow-hidden rounded-md border border-border">
            {STATUS_FILTERS.map((item) => (
              <button
                key={item || "all"}
                type="button"
                className={cn(
                  "h-9 border-r border-border px-4 text-body last:border-r-0 hover:bg-surface-hover",
                  status === item &&
                    "bg-primary text-primary-foreground hover:bg-primary",
                )}
                onClick={() => setStatus(item)}
              >
                {t(($) => $.code_graph[item || "all"])}
              </button>
            ))}
          </div>
        </div>

        <div className="shrink-0 overflow-hidden rounded-lg border border-border bg-surface">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t(($) => $.code_graph.repository)}</TableHead>
                <TableHead>{t(($) => $.code_graph.branch)}</TableHead>
                <TableHead>{t(($) => $.code_graph.status)}</TableHead>
                <TableHead>{t(($) => $.code_graph.files)}</TableHead>
                <TableHead>{t(($) => $.code_graph.updated)}</TableHead>
                <TableHead className="w-40 text-right">
                  {t(($) => $.code_graph.actions)}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className="h-28 text-center text-muted-foreground"
                  >
                    {t(($) => $.code_graph.loading)}
                  </TableCell>
                </TableRow>
              ) : items.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className="h-28 text-center text-muted-foreground"
                  >
                    {t(($) => $.code_graph.empty)}
                  </TableCell>
                </TableRow>
              ) : (
                items.map((repo) => (
                  <RepositoryRow
                    key={repo.id}
                    repo={repo}
                    onAssign={() => openAssignDialog(repo)}
                    onSync={() => syncRepo.mutate(repo.id)}
                  />
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </div>

      {assigningRepo && (
        <RepositoryAssignmentsDialog
          key={`${wsId}:${assigningRepo.id}`}
          wsId={wsId}
          repo={assigningRepo}
          onClose={() => setAssigningRepo(null)}
        />
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-lg border border-border bg-surface p-4">
      <div className="text-body font-medium text-foreground">{label}</div>
      <div className="mt-4 text-display-sm font-semibold text-foreground">
        {value}
      </div>
    </div>
  );
}

function RepositoryRow({
  repo,
  onAssign,
  onSync,
}: {
  repo: CodeGraphRepository;
  onAssign: () => void;
  onSync: () => void;
}) {
  const { t } = useT("layout");
  const wsId = useWorkspaceId();
  const cancelBuild = useCancelCodeGraphRepository(wsId);
  const deleteRepo = useDeleteCodeGraphRepository(wsId);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const active = repo.status === "pending" || repo.status === "processing";
  const cancelling = repo.progress?.cancel_requested && active;
  const busy = cancelBuild.isPending || deleteRepo.isPending;
  const error = cancelBuild.error || deleteRepo.error;
  return (
    <TableRow>
      <TableCell>
        <div className="flex min-w-0 items-center gap-2">
          <GitBranch className="size-4 shrink-0 text-muted-foreground" />
          <div className="min-w-0">
            <div className="truncate font-medium">{repo.repo_name}</div>
            <div className="truncate text-caption text-muted-foreground">
              {repo.repo_url}
            </div>
            {Boolean(repo.agents?.length) && (
              <div
                className="mt-1 truncate text-caption text-muted-foreground"
                title={repo.agents?.map((agent) => agent.name).join(", ")}
              >
                {t(($) => $.code_graph.assigned_agents)}:{" "}
                {repo.agents?.map((agent) => agent.name).join(", ")}
              </div>
            )}
          </div>
        </div>
      </TableCell>
      <TableCell>{repo.branch}</TableCell>
      <TableCell>
        <Badge
          variant={
            repo.progress?.phase === "cancelled"
              ? "secondary"
              : statusVariant(repo.status)
          }
        >
          {t(($) =>
            repo.progress?.phase === "cancelled"
              ? $.code_graph.build_cancelled
              : $.code_graph[repo.status],
          )}
        </Badge>
        <BuildProgress repo={repo} />
        {repo.sync_error ? (
          <div className="mt-1 max-w-72 break-words text-caption text-destructive">
            {repo.sync_error}
          </div>
        ) : null}
        {error ? (
          <p
            role="alert"
            className="max-w-72 break-words text-caption text-destructive"
          >
            {error.message}
          </p>
        ) : null}
      </TableCell>
      <TableCell>{repo.file_count}</TableCell>
      <TableCell>
        {repo.updated_at ? new Date(repo.updated_at).toLocaleString() : "-"}
      </TableCell>
      <TableCell className="text-right">
        <div className="flex justify-end gap-1">
          <RepositoryDetails repo={repo} />
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={onAssign}
            title={t(($) => $.code_graph.scope.choose_agents)}
            aria-label={t(($) => $.code_graph.scope.choose_agents)}
          >
            <UserPlus className="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            disabled={active || busy}
            onClick={onSync}
            title={t(($) => $.code_graph.sync)}
            aria-label={t(($) => $.code_graph.sync)}
          >
            <RefreshCw className="size-4" />
          </Button>
          {active ? (
            <Button
              variant="outline"
              size="sm"
              disabled={busy || cancelling}
              onClick={() => cancelBuild.mutate(repo.id)}
            >
              {t(($) =>
                cancelling
                  ? $.code_graph.build_cancelling
                  : $.code_graph.build_cancel,
              )}
            </Button>
          ) : null}
          <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
            <AlertDialogTrigger
              render={
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t(($) => $.code_graph.delete)}
                />
              }
            >
              <Trash2 className="size-4" />
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>
                  {t(($) => $.code_graph.delete)} · {repo.repo_name}
                </AlertDialogTitle>
                <AlertDialogDescription>
                  {t(($) => $.code_graph.build_delete_help)}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <BuildProgress repo={repo} />
              {error ? (
                <p role="alert" className="text-destructive">
                  {error.message}
                </p>
              ) : null}
              <AlertDialogFooter>
                <Button variant="outline" onClick={() => setDeleteOpen(false)}>
                  {t(($) => $.code_graph.cancel)}
                </Button>
                <Button
                  variant="destructive"
                  disabled={busy || cancelling}
                  onClick={() => {
                    if (active) cancelBuild.mutate(repo.id);
                    else
                      deleteRepo.mutate(repo.id, {
                        onSuccess: () => setDeleteOpen(false),
                      });
                  }}
                >
                  {t(($) =>
                    active
                      ? cancelling
                        ? $.code_graph.build_cancelling
                        : $.code_graph.build_cancel
                      : $.code_graph.delete,
                  )}
                </Button>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
      </TableCell>
    </TableRow>
  );
}

function BuildProgress({ repo }: { repo: CodeGraphRepository }) {
  const { t } = useT("layout");
  const p = repo.progress;
  const [now, setNow] = useState(() => Date.now());
  const running = p && !p.finished_at;
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [running]);
  if (!p) return null;
  const phases = {
    queued: t(($) => $.code_graph.pending),
    cloning: t(($) => $.code_graph.build_cloning),
    preparing: t(($) => $.code_graph.build_preparing),
    extracting: t(($) => $.code_graph.build_extracting),
    publishing: t(($) => $.code_graph.build_publishing),
    cancelling: t(($) => $.code_graph.build_cancelling),
    waiting_cloning: t(($) => $.code_graph.build_waiting_cloning),
    waiting_extracting: t(($) => $.code_graph.build_waiting_extracting),
    waiting_publishing: t(($) => $.code_graph.build_waiting_publishing),
    cancelled: t(($) => $.code_graph.build_cancelled),
    build_failed: t(($) => $.code_graph.failed),
    interrupted: t(($) => $.code_graph.failed),
  };
  const phase =
    p.status === "ready"
      ? t(($) => $.code_graph.ready)
      : (phases[p.phase as keyof typeof phases] ??
        t(($) => $.code_graph[repo.status]));
  const end = p.finished_at ? Date.parse(p.finished_at) : now;
  const seconds = (start: string) =>
    Math.max(0, Math.floor((end - Date.parse(start)) / 1000));
  return (
    <div className="mt-1 max-w-72 text-caption text-muted-foreground">
      {p.status !== "ready" && p.phase !== "unknown" ? <p>{phase}</p> : null}
      <p>
        {t(($) => $.code_graph.build_elapsed, {
          seconds: seconds(p.started_at ?? p.created_at),
        })}
      </p>
      {p.phase_started_at ? (
        <p>
          {t(($) => $.code_graph.build_last_phase)}{" "}
          {new Date(p.phase_started_at).toLocaleString()}
        </p>
      ) : null}
      {!p.finished_at &&
      p.phase_started_at &&
      seconds(p.phase_started_at) > 120 ? (
        <p>{t(($) => $.code_graph.build_slow)}</p>
      ) : null}
    </div>
  );
}

function RepositoryDetails({ repo }: { repo: CodeGraphRepository }) {
  const { t } = useT("layout");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const [tab, setTab] = useState<string>("overview");
  const details = useQuery(
    codeGraphRepositoryOptions(wsId, open ? repo.id : ""),
  );
  const sync = useSyncCodeGraphRepository(wsId);
  const canManage = useCanManageGit();
  const current = details.data ?? repo;
  return (
    <>
      <Dialog
        open={open}
        onOpenChange={(value) => {
          setOpen(value);
          if (!value) setTab("overview");
        }}
      >
        <DialogTrigger render={<Button variant="ghost" size="sm" />}>
          {t(($) => $.code_graph.details)}
        </DialogTrigger>
        <DialogContent
          className="flex h-[min(46rem,86dvh)] min-h-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-4xl"
          aria-describedby={undefined}
        >
          <DialogHeader className="shrink-0 px-6 pb-4 pt-6 pr-12">
            <p className="mb-1 text-caption text-muted-foreground">
              {t(($) => $.code_graph.details_title)}
            </p>
            <DialogTitle className="break-words text-title">
              {current.repo_name}
            </DialogTitle>
            <p className="break-all text-caption text-muted-foreground">
              {current.repo_url}
            </p>
            <div className="mt-3 flex flex-wrap items-center gap-3">
              <span className="rounded border px-2 py-0.5 font-mono text-caption">
                {current.branch}
              </span>
              <Badge
                variant={
                  current.progress?.phase === "cancelled"
                    ? "secondary"
                    : statusVariant(current.status)
                }
              >
                {t(($) =>
                  current.progress?.phase === "cancelled"
                    ? $.code_graph.build_cancelled
                    : $.code_graph[current.status],
                )}
              </Badge>
            </div>
          </DialogHeader>
          <Tabs
            value={tab}
            onValueChange={(value) => setTab(String(value))}
            className="min-h-0 flex-1 gap-0"
          >
            <div className="shrink-0 border-b px-6">
              <TabsList variant="line" className="gap-5">
                <TabsTrigger value="overview">
                  {t(($) => $.code_graph.git_auth.overview)}
                </TabsTrigger>
                <TabsTrigger value="usage">
                  {t(($) => $.code_graph.scope.title)}
                </TabsTrigger>
                <TabsTrigger value="access">
                  {t(($) => $.code_graph.git_auth.access)}
                </TabsTrigger>
                <TabsTrigger value="advanced">
                  {t(($) => $.code_graph.scope.advanced)}
                </TabsTrigger>
              </TabsList>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto px-6 py-6">
              <TabsContent value="overview" className="space-y-7">
                <div className="grid grid-cols-3 divide-x rounded-lg border py-4">
                  {[
                    {
                      label: t(($) => $.code_graph.files),
                      value: current.file_count,
                    },
                    {
                      label: t(($) => $.code_graph.git_auth.symbols),
                      value: current.symbol_count ?? 0,
                    },
                    {
                      label: t(($) => $.code_graph.git_auth.edges),
                      value: current.edge_count ?? 0,
                    },
                  ].map((item) => (
                    <div key={item.label} className="min-w-0 px-4">
                      <p className="text-caption text-muted-foreground">
                        {item.label}
                      </p>
                      <p className="mt-1 text-title font-semibold">
                        {item.value}
                      </p>
                    </div>
                  ))}
                </div>
                <section className="space-y-3">
                  <h3 className="font-medium">
                    {t(($) => $.code_graph.git_auth.current_graph)}
                  </h3>
                  <dl className="divide-y">
                    <div className="flex justify-between gap-4 py-3">
                      <dt className="text-muted-foreground">
                        {t(($) => $.code_graph.branch)}
                      </dt>
                      <dd className="break-all">{current.branch}</dd>
                    </div>
                    <div className="flex justify-between gap-4 py-3">
                      <dt className="text-muted-foreground">
                        {t(($) => $.code_graph.git_auth.code_version)}
                      </dt>
                      <dd className="font-mono text-caption">
                        {current.commit_hash?.slice(0, 12) || "—"}
                      </dd>
                    </div>
                    <div className="flex flex-wrap justify-between gap-3 py-3">
                      <dt className="text-muted-foreground">
                        {t(($) => $.code_graph.git_auth.last_sync)}
                      </dt>
                      <dd>
                        {current.last_indexed_at
                          ? new Date(current.last_indexed_at).toLocaleString()
                          : "—"}
                      </dd>
                    </div>
                    <div className="flex flex-wrap justify-between gap-3 py-3">
                      <dt className="text-muted-foreground">
                        {t(($) => $.code_graph.git_auth.authentication)}
                      </dt>
                      <dd className="flex items-center gap-3">
                        {current.auth_mode === "token"
                          ? t(($) => $.code_graph.git_auth.token_mode)
                          : current.auth_mode === "anonymous"
                            ? t(($) => $.code_graph.git_auth.anonymous)
                            : "—"}
                        {canManage && (
                          <Button
                            variant="ghost"
                            size="xs"
                            onClick={() => setTab("access")}
                          >
                            {t(($) => $.code_graph.git_auth.manage_access)}
                          </Button>
                        )}
                      </dd>
                    </div>
                  </dl>
                  {current.sync_error && (
                    <p role="alert" className="break-words text-destructive">
                      {current.sync_error}
                    </p>
                  )}
                </section>
                <section className="space-y-2">
                  <h3 className="font-medium">
                    {t(($) => $.code_graph.scope.title)}
                  </h3>
                  {details.isError ? (
                    <div role="alert" className="text-caption text-destructive">
                      {t(($) => $.code_graph.load_failed)}
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => void details.refetch()}
                      >
                        {t(($) => $.code_graph.retry)}
                      </Button>
                    </div>
                  ) : (
                    <p className="text-body text-muted-foreground">
                      {current.is_workspace_shared
                        ? t(($) => $.code_graph.scope.sharing_on)
                        : (current.agents?.length ?? 0) +
                              (current.projects?.length ?? 0) >
                            0
                          ? t(($) => $.code_graph.scope.assignment_summary, {
                              agents: current.agents?.length ?? 0,
                              projects: current.projects?.length ?? 0,
                            })
                          : t(($) => $.code_graph.scope.unassigned_help)}
                    </p>
                  )}
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.code_graph.scope.task_help)}
                  </p>
                </section>
              </TabsContent>
              <TabsContent value="advanced" className="space-y-5">
                <details className="rounded-lg border p-4">
                  <summary className="cursor-pointer text-body font-medium">
                    {t(($) => $.code_graph.scope.query_checks)}
                  </summary>
                  <p className="my-3 text-caption text-muted-foreground">
                    {t(($) => $.code_graph.scope.query_checks_help)}
                  </p>
                  <CodeGraphTools wsId={wsId} repo={current} />
                </details>
                <details className="border-t pt-4">
                  <summary className="cursor-pointer text-caption text-muted-foreground">
                    {t(($) => $.code_graph.git_auth.history)}
                  </summary>
                  <div className="mt-3 space-y-3">
                    <BuildProgress repo={current} />
                    {current.builds?.map((build) => (
                      <div
                        key={build.id}
                        className="rounded border p-3 text-caption"
                      >
                        <p>
                          {build.status} · {build.commit.slice(0, 12)}
                        </p>
                        <p className="mt-1 break-all font-mono text-muted-foreground">
                          {build.id}
                        </p>
                        {build.error && (
                          <p className="mt-2 break-words text-destructive">
                            {build.error}
                          </p>
                        )}
                      </div>
                    ))}
                  </div>
                </details>
              </TabsContent>
              <TabsContent value="access" className="space-y-7">
                <RepositoryGitAuth repo={current} />
              </TabsContent>
              <TabsContent value="usage" className="space-y-5">
                <RepositoryUsageScope wsId={wsId} repo={current} />
              </TabsContent>
            </div>
          </Tabs>
          <div className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-t px-6 py-4">
            <p className="max-w-sm text-caption text-muted-foreground">
              {t(($) => $.code_graph.git_auth.sync_short)}
            </p>
            <div className="flex gap-2">
              {canManage && (
                <Button
                  variant="outline"
                  disabled={
                    sync.isPending ||
                    current.status === "processing" ||
                    current.status === "pending"
                  }
                  onClick={() => sync.mutate(current.id)}
                >
                  {t(($) => $.code_graph.sync)}
                </Button>
              )}
              <Button
                onClick={() => {
                  if (tab === "usage") {
                    setOpen(false);
                    setTab("overview");
                  } else setTab("usage");
                }}
              >
                {t(($) =>
                  tab === "usage"
                    ? $.code_graph.scope.done
                    : $.code_graph.scope.configure,
                )}
              </Button>
            </div>
            {sync.error && (
              <p role="alert" className="w-full text-caption text-destructive">
                {sync.error.message}
              </p>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
