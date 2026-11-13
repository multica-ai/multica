"use client";

import { useEffect, useMemo, useState } from "react";
import { History, Loader2, RotateCcw } from "@multica/ui/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type {
  MemberWithUser,
  Skill,
  SkillVersionSummary,
} from "@multica/core/types";
import { api } from "@multica/core/api";
import {
  skillDetailOptions,
  skillVersionDetailOptions,
  skillVersionsOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER, PAGE_RAIL } from "../../layout/page-header";
import { useT, useTimeAgo } from "../../i18n";
import { FileViewer, type FileMode } from "./file-viewer";

const SKILL_MD = "SKILL.md";

function useSourceLabel() {
  const { t } = useT("skills");
  return (v: SkillVersionSummary): string => {
    switch (v.source) {
      case "baseline":
        return t(($) => $.detail.versions.source.baseline);
      case "create":
        return t(($) => $.detail.versions.source.create);
      case "import":
        return t(($) => $.detail.versions.source.import);
      case "refresh":
        return t(($) => $.detail.versions.source.refresh);
      case "restore":
        return v.restored_from_version !== null
          ? t(($) => $.detail.versions.source.restore, {
              version: v.restored_from_version,
            })
          : t(($) => $.detail.versions.source.update);
      default:
        return t(($) => $.detail.versions.source.update);
    }
  };
}

/**
 * Read-only content history. Every save already records a version, so the
 * only action here is Restore, which the server turns into a new latest
 * version: history only moves forward and a restore can itself be restored.
 */
export function SkillVersionsTab({
  skill,
  wsId,
  canEdit,
  hasUnsavedChanges,
  members,
  onRestored,
}: {
  skill: Skill;
  wsId: string;
  canEdit: boolean;
  hasUnsavedChanges: boolean;
  members: MemberWithUser[];
  /** Fired after caches are updated; the detail page adopts the result. */
  onRestored: (updated: Skill) => void;
}) {
  const { t } = useT("skills");
  const timeAgo = useTimeAgo();
  const qc = useQueryClient();
  const sourceLabel = useSourceLabel();

  const {
    data: versions = [],
    isLoading,
    error,
  } = useQuery(skillVersionsOptions(wsId, skill.id));
  const latest = versions[0]?.version ?? null;

  const [selected, setSelected] = useState<number | null>(null);
  // Follow the newest version until the user picks one, and drop a selection
  // that no longer exists.
  const selectedVersion =
    selected !== null && versions.some((v) => v.version === selected)
      ? selected
      : latest;

  const { data: detail, isLoading: detailLoading } = useQuery(
    skillVersionDetailOptions(wsId, skill.id, selectedVersion),
  );

  const [selectedPath, setSelectedPath] = useState(SKILL_MD);
  const [mode, setMode] = useState<FileMode>("preview");
  const filePaths = useMemo(
    () => [SKILL_MD, ...(detail?.files ?? []).map((f) => f.path)],
    [detail],
  );
  useEffect(() => {
    if (!filePaths.includes(selectedPath)) setSelectedPath(SKILL_MD);
  }, [filePaths, selectedPath]);
  const selectedContent =
    selectedPath === SKILL_MD
      ? (detail?.content ?? "")
      : (detail?.files.find((f) => f.path === selectedPath)?.content ?? "");

  const [confirmOpen, setConfirmOpen] = useState(false);
  const [restoring, setRestoring] = useState(false);

  const memberName = (userId: string | null) =>
    (userId && members.find((m) => m.user_id === userId)?.name) ||
    t(($) => $.detail.versions.unknown_author);

  const handleRestore = async () => {
    if (selectedVersion === null) return;
    setRestoring(true);
    try {
      const updated = await api.restoreSkillVersion(skill.id, selectedVersion);
      if (updated.id === skill.id) {
        qc.setQueryData(skillDetailOptions(wsId, skill.id).queryKey, updated);
      } else {
        // Schema fallback (malformed response): refetch instead of seeding
        // the cache with the empty placeholder.
        qc.invalidateQueries({
          queryKey: skillDetailOptions(wsId, skill.id).queryKey,
          exact: true,
        });
      }
      qc.invalidateQueries({
        queryKey: skillVersionsOptions(wsId, skill.id).queryKey,
        exact: true,
      });
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), exact: true });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
      toast.success(
        t(($) => $.detail.versions.toast_restored, { version: selectedVersion }),
      );
      setConfirmOpen(false);
      setSelected(null);
      if (updated.id === skill.id) onRestored(updated);
    } catch (e) {
      toast.error(
        e instanceof Error && e.message
          ? e.message
          : t(($) => $.detail.versions.toast_restore_failed),
      );
    } finally {
      setRestoring(false);
    }
  };

  if (isLoading) {
    return (
      <div className={cn(PAGE_RAIL, PAGE_GUTTER, "space-y-3 py-6")}>
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-5/6" />
      </div>
    );
  }

  if (error) {
    return (
      <div className={cn(PAGE_RAIL, PAGE_GUTTER, "py-6 text-caption text-muted-foreground")}>
        {t(($) => $.detail.versions.load_failed)}
      </div>
    );
  }

  if (versions.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center gap-2 py-16 text-center">
        <History className="h-6 w-6 text-faint-foreground" />
        <p className="max-w-sm text-caption text-muted-foreground">
          {t(($) => $.detail.versions.empty)}
        </p>
      </div>
    );
  }

  const selectedSummary = versions.find((v) => v.version === selectedVersion);
  const isLatest = selectedVersion === latest;

  return (
    <div className={cn(PAGE_RAIL, "flex min-h-full flex-col md:h-full md:flex-row")}>
      <aside
        className={cn(
          "shrink-0 border-b border-surface-border py-3 md:w-64 md:overflow-y-auto md:border-b-0 md:border-r md:py-4",
          PAGE_GUTTER,
        )}
      >
        <div
          role="tablist"
          aria-orientation="vertical"
          aria-label={t(($) => $.detail.versions.list_aria)}
          className="flex flex-col gap-0.5"
        >
          {versions.map((v) => {
            const active = v.version === selectedVersion;
            return (
              <button
                key={v.id}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setSelected(v.version)}
                className={cn(
                  "flex flex-col gap-0.5 rounded-md px-2.5 py-2 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  active ? "bg-accent text-accent-foreground" : "hover:bg-accent/60",
                )}
              >
                <span className="flex items-center gap-2">
                  <span className="font-mono text-caption font-medium">
                    {t(($) => $.detail.versions.version_label, { version: v.version })}
                  </span>
                  {v.version === latest && (
                    <span className="rounded-full border px-1.5 text-micro text-muted-foreground">
                      {t(($) => $.detail.versions.current)}
                    </span>
                  )}
                  <span className="ml-auto shrink-0 text-micro text-muted-foreground">
                    {timeAgo(v.created_at)}
                  </span>
                </span>
                <span className="truncate text-caption text-muted-foreground">
                  {sourceLabel(v)} · {memberName(v.created_by)}
                </span>
              </button>
            );
          })}
        </div>
      </aside>

      <section className="flex min-h-[32rem] min-w-0 flex-1 flex-col md:min-h-0">
        <div className="flex h-10 shrink-0 items-center gap-3 border-b px-3 sm:px-4">
          <div
            role="tablist"
            aria-label={t(($) => $.detail.files.list_aria)}
            className="flex min-w-0 items-center gap-1 overflow-x-auto"
          >
            {filePaths.map((p) => (
              <button
                key={p}
                type="button"
                role="tab"
                aria-selected={p === selectedPath}
                onClick={() => setSelectedPath(p)}
                className={cn(
                  "h-6 shrink-0 rounded-xs px-2 font-mono text-caption transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  p === selectedPath
                    ? "bg-muted text-foreground"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {p}
              </button>
            ))}
          </div>
          <div className="ml-auto flex shrink-0 items-center gap-1">
            <div
              role="group"
              aria-label={t(($) => $.detail.files.mode_aria)}
              className="flex items-center gap-0.5 rounded-md bg-muted p-0.5"
            >
              {(["preview", "raw"] as const).map((value) => (
                <button
                  key={value}
                  type="button"
                  aria-pressed={mode === value}
                  onClick={() => setMode(value)}
                  className={cn(
                    "h-6 rounded-xs px-2 text-caption font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                    mode === value
                      ? "bg-surface text-foreground shadow-sm"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {value === "preview"
                    ? t(($) => $.detail.files.mode_preview)
                    : t(($) => $.detail.files.mode_raw)}
                </button>
              ))}
            </div>
            {canEdit && !isLatest && (
              <Button
                type="button"
                variant="outline"
                size="xs"
                className="gap-1"
                onClick={() => setConfirmOpen(true)}
              >
                <RotateCcw className="h-3 w-3" />
                {t(($) => $.detail.versions.restore)}
              </Button>
            )}
          </div>
        </div>

        <div className="min-h-0 flex-1">
          {detailLoading || !detail ? (
            <div className="space-y-3 p-4">
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-3/4" />
            </div>
          ) : (
            <FileViewer
              key={`${selectedVersion}:${selectedPath}`}
              path={selectedPath}
              content={selectedContent}
              mode={mode}
              readOnly
              onChange={() => {}}
            />
          )}
        </div>
      </section>

      <Dialog
        open={confirmOpen}
        onOpenChange={(v) => {
          if (!restoring) setConfirmOpen(v);
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>
              {t(($) => $.detail.versions.dialog.title, {
                version: selectedSummary?.version ?? 0,
              })}
            </DialogTitle>
            <DialogDescription>
              {t(($) => $.detail.versions.dialog.description, {
                version: selectedSummary?.version ?? 0,
              })}
            </DialogDescription>
          </DialogHeader>
          {hasUnsavedChanges && (
            <div className="rounded-md bg-warning/10 px-3 py-2 text-caption text-muted-foreground">
              {t(($) => $.detail.versions.dialog.unsaved_warning)}
            </div>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => setConfirmOpen(false)}
              disabled={restoring}
            >
              {t(($) => $.detail.versions.dialog.cancel)}
            </Button>
            <Button type="button" onClick={handleRestore} disabled={restoring}>
              {restoring ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {t(($) => $.detail.versions.dialog.restoring)}
                </>
              ) : (
                <>
                  <RotateCcw className="h-3 w-3" />
                  {t(($) => $.detail.versions.dialog.confirm)}
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
