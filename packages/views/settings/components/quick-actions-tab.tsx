"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Archive,
  ArchiveRestore,
  Globe,
  Lock,
  MoreHorizontal,
  Pencil,
  Plus,
  Search,
  Trash2,
  TriangleAlert,
  Zap,
} from "@multica/ui/icons";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  quickActionListOptions,
  useCreateQuickAction,
  useDeleteQuickAction,
  useUpdateQuickAction,
} from "@multica/core/quick-actions";
import type {
  QuickAction,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { QuickActionDialog } from "../../quick-actions/components/quick-action-dialog";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useLocale, useT } from "../../i18n";
import { SettingsTab } from "./settings-layout";

// Quick Actions catalog (MUL-5465).
//
// Ordering is by use_count DESC, not created_at: the list's job is to answer
// "what does this workspace actually use", and recency of creation says
// nothing about that. Rows that have never been used sort last, which is also
// where the "unused" cleanup signal belongs.
//
// Layout deliberately mirrors the Labels and Properties tabs: same search +
// primary-action row, same bordered card, same responsive column grid that
// collapses to stacked rows under `md`, same overflow menu. These three are
// the workspace's "catalog of small named things" and reading as one surface
// matters more than any per-tab cleverness.

const UNUSED_DAYS_THRESHOLD = 90;

/**
 * A usage figure is only worth flagging once it has had time to be used. A
 * freshly created action is "never used" by definition, so colouring it on day
 * one trains people to ignore the colour — the signal has to mean "this has
 * been sitting here unused", not "this is new".
 */
function isStale(action: QuickAction): boolean {
  const sinceLastUse = daysSince(action.last_used_at);
  if (sinceLastUse !== null) return sinceLastUse >= UNUSED_DAYS_THRESHOLD;
  const age = daysSince(action.created_at);
  return age !== null && age >= UNUSED_DAYS_THRESHOLD;
}

function daysSince(iso: string | null): number | null {
  if (!iso) return null;
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return null;
  return Math.floor((Date.now() - then) / 86_400_000);
}

type QuickActionsT = ReturnType<typeof useT<"settings">>["t"];

function VisibilityBadge({ action, t }: { action: QuickAction; t: QuickActionsT }) {
  // Server-driven enum: an unknown value must land on a generic fallback
  // rather than rendering nothing (API compatibility rule).
  switch (action.visibility) {
    case "public":
      return (
        <Badge variant="secondary" className="gap-1 font-normal">
          <Globe className="size-3" />
          {t(($) => $.quick_actions.visibility_public)}
        </Badge>
      );
    case "private":
      return (
        <Badge variant="outline" className="gap-1 font-normal">
          <Lock className="size-3" />
          {t(($) => $.quick_actions.visibility_private)}
        </Badge>
      );
    default:
      return (
        <Badge variant="outline" className="font-normal">
          {action.visibility}
        </Badge>
      );
  }
}

/**
 * The bound target plus its CURRENT reachability, as plain metadata.
 *
 * This replaces a derived "broken" flag: a `public` action whose agent has
 * since gone private simply reads wrong here — public badge, "private" target
 * — without a bespoke error state to maintain. The cost is that nobody is
 * actively notified; that trade was made deliberately (drift is rare and the
 * failure is loud at click time, not silent).
 */
function TargetLine({ action, t }: { action: QuickAction; t: QuickActionsT }) {
  if (action.assignee_type === "") {
    return <span>{t(($) => $.quick_actions.no_target)}</span>;
  }
  if (action.target_missing === true) {
    return (
      <span className="inline-flex items-center gap-1 text-destructive">
        <TriangleAlert className="size-3" />
        {t(($) => $.quick_actions.target_missing)}
      </span>
    );
  }
  const mismatched = action.visibility === "public" && action.target_public !== true;
  return (
    <span className={cn("inline-flex items-center gap-1", mismatched && "text-warning")}>
      {mismatched ? <TriangleAlert className="size-3" /> : null}
      {action.target_name}
      {action.target_public !== true ? ` · ${t(($) => $.quick_actions.target_private)}` : ""}
    </span>
  );
}

export function QuickActionsTab() {
  const { t } = useT("settings");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const { data: actions = [], isLoading } = useQuery(quickActionListOptions(wsId, true));

  const [query, setQuery] = useState("");
  const [editing, setEditing] = useState<QuickAction | null>(null);
  const [creating, setCreating] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<QuickAction | null>(null);

  const createMutation = useCreateQuickAction();
  const updateMutation = useUpdateQuickAction();
  const deleteMutation = useDeleteQuickAction();

  // Usage-first ordering; never-used rows fall to the bottom where the
  // "unused" signal is actionable.
  const sorted = useMemo(
    () =>
      [...actions].sort((a, b) => {
        if (a.status !== b.status) return a.status === "active" ? -1 : 1;
        if (b.use_count !== a.use_count) return b.use_count - a.use_count;
        return a.name.localeCompare(b.name);
      }),
    [actions],
  );

  // Name + target search, matching the Labels tab's client-side filter: the
  // catalog is capped at 30 rows, so there is nothing to gain from a server
  // round-trip here.
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return sorted;
    return sorted.filter(
      (a) =>
        a.name.toLowerCase().includes(q) ||
        (a.target_name ?? "").toLowerCase().includes(q),
    );
  }, [sorted, query]);

  const handleArchiveToggle = async (action: QuickAction) => {
    const next = action.status === "active" ? "archived" : "active";
    try {
      await updateMutation.mutateAsync({ id: action.id, status: next });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) return;
    try {
      await deleteMutation.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : String(error));
    }
  };

  return (
    <SettingsTab
      title={t(($) => $.quick_actions.title)}
      description={t(($) => $.quick_actions.description)}
      scope="workspace"
    >
      <div className="space-y-5">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="relative w-full sm:max-w-sm">
            <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(event) => setQuery(event.target.value)}
              placeholder={t(($) => $.quick_actions.search_placeholder)}
              className="pl-9"
            />
          </div>
          <Button className="gap-2" onClick={() => setCreating(true)}>
            <Plus className="size-4" />
            {t(($) => $.quick_actions.add)}
          </Button>
        </div>

        <div className="overflow-hidden rounded-lg border border-surface-border bg-card">
          <div className="hidden grid-cols-[minmax(10rem,1fr)_minmax(9rem,1fr)_6rem_5rem_7rem_2rem] gap-4 border-b border-surface-border bg-muted/20 px-4 py-2.5 text-caption font-medium text-muted-foreground md:grid">
            <span>{t(($) => $.quick_actions.columns.name)}</span>
            <span>{t(($) => $.quick_actions.columns.target)}</span>
            <span>{t(($) => $.quick_actions.columns.visibility)}</span>
            <span>{t(($) => $.quick_actions.columns.usage)}</span>
            <span>{t(($) => $.quick_actions.columns.updated)}</span>
            <span />
          </div>

          {isLoading ? (
            <div className="px-4 py-12 text-center text-body text-muted-foreground">
              {t(($) => $.quick_actions.loading)}
            </div>
          ) : filtered.length === 0 ? (
            <div className="px-4 py-12 text-center">
              <Zap className="mx-auto size-6 text-faint-foreground" />
              <p className="mt-3 text-body font-medium">
                {query
                  ? t(($) => $.quick_actions.no_results)
                  : t(($) => $.quick_actions.empty_title)}
              </p>
              {!query ? (
                <p className="mx-auto mt-1 max-w-sm text-caption text-muted-foreground">
                  {t(($) => $.quick_actions.empty_hint)}
                </p>
              ) : null}
            </div>
          ) : (
            <div className="divide-y divide-surface-border">
              {filtered.map((action) => {
                const stale = isStale(action);
                return (
                  <div
                    key={action.id}
                    className={cn(
                      "grid gap-2 px-4 py-3 md:grid-cols-[minmax(10rem,1fr)_minmax(9rem,1fr)_6rem_5rem_7rem_2rem] md:items-center md:gap-4",
                      action.status !== "active" && "opacity-60",
                    )}
                  >
                    <div className="flex min-w-0 items-center gap-2">
                      <Zap className="size-3.5 shrink-0 text-muted-foreground" />
                      <span className="truncate text-body font-medium">{action.name}</span>
                      {action.status !== "active" ? (
                        <Badge variant="outline" className="shrink-0 font-normal">
                          {t(($) => $.quick_actions.archived)}
                        </Badge>
                      ) : null}
                    </div>
                    <div className="min-w-0 truncate text-caption text-muted-foreground md:text-body">
                      <TargetLine action={action} t={t} />
                    </div>
                    <div className="min-w-0">
                      <VisibilityBadge action={action} t={t} />
                    </div>
                    <span
                      className={cn(
                        "text-caption tabular-nums text-muted-foreground md:text-body",
                        stale && "text-warning",
                      )}
                    >
                      {action.use_count === 0
                        ? t(($) => $.quick_actions.never_used)
                        : t(($) => $.quick_actions.used_count, { count: action.use_count })}
                    </span>
                    <span className="text-caption text-muted-foreground">
                      {new Date(action.updated_at).toLocaleDateString(locale)}
                    </span>
                    <DropdownMenu>
                      <DropdownMenuTrigger
                        render={
                          <IconButton
                            variant="ghost"
                            size="icon-sm"
                            label={t(($) => $.quick_actions.actions_open, { name: action.name })}
                          >
                            <MoreHorizontal className="size-4" />
                          </IconButton>
                        }
                      />
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => setEditing(action)}>
                          <Pencil className="size-4" />
                          {t(($) => $.quick_actions.edit)}
                        </DropdownMenuItem>
                        <DropdownMenuItem onClick={() => void handleArchiveToggle(action)}>
                          {action.status === "active" ? (
                            <>
                              <Archive className="size-4" />
                              {t(($) => $.quick_actions.archive)}
                            </>
                          ) : (
                            <>
                              <ArchiveRestore className="size-4" />
                              {t(($) => $.quick_actions.unarchive)}
                            </>
                          )}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          variant="destructive"
                          onClick={() => setDeleteTarget(action)}
                        >
                          <Trash2 className="size-4" />
                          {t(($) => $.quick_actions.delete)}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>

      <QuickActionDialog
        open={creating || editing !== null}
        action={editing}
        onOpenChange={(open) => {
          if (!open) {
            setCreating(false);
            setEditing(null);
          }
        }}
        onSubmit={async (form) => {
          if (editing) {
            await updateMutation.mutateAsync({
              id: editing.id,
              name: form.name,
              description: form.description,
              assignee_type: form.assigneeId ? form.assigneeType : "",
              assignee_id: form.assigneeId,
              prompt: form.prompt,
              visibility: form.visibility,
            });
          } else {
            await createMutation.mutateAsync({
              name: form.name,
              description: form.description,
              assignee_type: form.assigneeId ? form.assigneeType : "",
              assignee_id: form.assigneeId,
              prompt: form.prompt,
              visibility: form.visibility,
            });
          }
        }}
      />

      <AlertDialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.quick_actions.delete_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.quick_actions.delete_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.quick_actions.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete}>
              {t(($) => $.quick_actions.delete)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsTab>
  );
}
