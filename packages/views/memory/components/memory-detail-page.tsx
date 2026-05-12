"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  Archive,
  ArchiveRestore,
  CheckCircle,
  MoreHorizontal,
  Trash2,
} from "lucide-react";
import {
  memoryDetailOptions,
  MEMORY_KIND_LABELS,
  useUpdateMemoryArtifact,
  useArchiveMemoryArtifact,
  useRestoreMemoryArtifact,
  useVerifyMemoryArtifact,
  useDeleteMemoryArtifact,
} from "@multica/core/memory";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useActorName } from "@multica/core/workspace/hooks";
import type { MemoryArtifact, MemoryArtifactKind } from "@multica/core/types";
import { ContentEditor, type ContentEditorRef, TitleEditor } from "../../editor";
import { ActorAvatar } from "../../common/actor-avatar";
import { useNavigation, AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { toast } from "sonner";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useT } from "../../i18n";

const KIND_BADGE: Record<MemoryArtifactKind, string> = {
  wiki_page: "bg-sky-500/10 text-sky-600 dark:text-sky-400",
  agent_note: "bg-violet-500/10 text-violet-600 dark:text-violet-400",
  runbook: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  decision: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
};

const STALE_THRESHOLD_DAYS = 30;

function isStale(artifact: MemoryArtifact): boolean {
  const freshDate =
    artifact.verified_at && artifact.verified_at > artifact.updated_at
      ? artifact.verified_at
      : artifact.updated_at;
  const days = Math.floor(
    (Date.now() - new Date(freshDate).getTime()) / (1000 * 60 * 60 * 24),
  );
  return days >= STALE_THRESHOLD_DAYS;
}

interface MemoryDetailPageProps {
  id: string;
}

export function MemoryDetailPage({ id }: MemoryDetailPageProps) {
  const { t } = useT("memory");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const { getActorName } = useActorName();

  const { data: artifact, isLoading } = useQuery(
    memoryDetailOptions(wsId, id),
  );

  const updateArtifact = useUpdateMemoryArtifact();
  const archiveArtifact = useArchiveMemoryArtifact();
  const restoreArtifact = useRestoreMemoryArtifact();
  const verifyArtifact = useVerifyMemoryArtifact();
  const deleteArtifact = useDeleteMemoryArtifact();

  // Track the editor's ref so we can pull the markdown on title-blur
  // (saving title implies we should also persist any in-flight content
  // edits — otherwise switching focus loses them).
  const contentRef = useRef<ContentEditorRef>(null);

  // Local title state mirrors the server-side artifact.title until the
  // user types — at which point we drift and only sync back on save or
  // remount.
  const [titleDraft, setTitleDraft] = useState("");
  useEffect(() => {
    if (artifact) setTitleDraft(artifact.title);
  }, [artifact?.id, artifact?.title]);

  if (isLoading || !artifact) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader className="px-5">
          <AppLink
            href={wsPaths.memory()}
            className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
          >
            <ArrowLeft className="h-3.5 w-3.5" />
            <span>{t(($) => $.detail.back)}</span>
          </AppLink>
        </PageHeader>
        <div className="flex-1 px-8 py-6 space-y-3">
          <Skeleton className="h-6 w-24" />
          <Skeleton className="h-9 w-2/3" />
          <Skeleton className="h-32 w-full" />
        </div>
      </div>
    );
  }

  const isArchived = artifact.archived_at != null;
  const authorName = getActorName(artifact.author_type, artifact.author_id);

  const saveTitle = (next: string) => {
    const trimmed = next.trim();
    if (!trimmed || trimmed === artifact.title) return;
    updateArtifact.mutate(
      { id: artifact.id, title: trimmed },
      {
        onError: () => toast.error(t(($) => $.detail.toast_update_title_failed)),
      },
    );
  };

  const saveContent = (markdown: string) => {
    if (markdown === artifact.content) return;
    updateArtifact.mutate(
      { id: artifact.id, content: markdown },
      {
        onError: () => toast.error(t(($) => $.detail.toast_save_content_failed)),
      },
    );
  };

  const handleArchive = () => {
    archiveArtifact.mutate(artifact.id, {
      onSuccess: () => toast.success(t(($) => $.detail.toast_archived)),
      onError: () => toast.error(t(($) => $.detail.toast_archive_failed)),
    });
  };

  const handleRestore = () => {
    restoreArtifact.mutate(artifact.id, {
      onSuccess: () => toast.success(t(($) => $.detail.toast_restored)),
      onError: () => toast.error(t(($) => $.detail.toast_restore_failed)),
    });
  };

  const handleVerify = () => {
    verifyArtifact.mutate(artifact.id, {
      onSuccess: () => toast.success(t(($) => $.detail.toast_verified)),
      onError: () => toast.error(t(($) => $.detail.toast_verify_failed)),
    });
  };

  const handleDelete = () => {
    if (!window.confirm(t(($) => $.detail.delete_confirm))) {
      return;
    }
    deleteArtifact.mutate(artifact.id, {
      onSuccess: () => {
        toast.success(t(($) => $.detail.toast_deleted));
        router.push(wsPaths.memory());
      },
      onError: () => toast.error(t(($) => $.detail.toast_delete_failed)),
    });
  };

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <AppLink
          href={wsPaths.memory()}
          className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
          <span>{t(($) => $.detail.back)}</span>
        </AppLink>
        <div className="flex items-center gap-2">
          {isArchived && (
            <span className="text-[10px] uppercase tracking-wider rounded bg-muted px-2 py-0.5 text-muted-foreground">
              {t(($) => $.detail.archived_badge)}
            </span>
          )}
          {isArchived ? (
            <Button size="sm" variant="outline" onClick={handleRestore}>
              <ArchiveRestore className="h-3.5 w-3.5 mr-1" />
              {t(($) => $.detail.restore)}
            </Button>
          ) : (
            <>
              <Button
                size="sm"
                variant="outline"
                onClick={handleVerify}
                disabled={verifyArtifact.isPending}
              >
                <CheckCircle className="h-3.5 w-3.5 mr-1" />
                {t(($) => $.detail.verify)}
              </Button>
              <Button size="sm" variant="outline" onClick={handleArchive}>
                <Archive className="h-3.5 w-3.5 mr-1" />
                {t(($) => $.detail.archive)}
              </Button>
            </>
          )}
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button size="sm" variant="ghost" className="h-7 w-7 p-0">
                  <MoreHorizontal className="h-3.5 w-3.5" />
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-44">
              <DropdownMenuItem
                onClick={handleDelete}
                className="text-destructive focus:text-destructive"
              >
                <Trash2 className="h-3.5 w-3.5 mr-1.5" />
                {t(($) => $.detail.delete_forever)}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto max-w-3xl px-8 py-6 space-y-3">
          <span
            className={cn(
              "inline-flex items-center rounded-md px-2 py-0.5 text-[10px] font-medium uppercase tracking-wide",
              KIND_BADGE[artifact.kind],
            )}
          >
            {MEMORY_KIND_LABELS[artifact.kind]}
          </span>
          {/* Inline title — TitleEditor commits on blur/Enter. We rely on
              the optimistic updateArtifact mutation to keep the displayed
              title in sync; titleDraft just feeds the editor's defaultValue
              so remounts don't blow away pending edits. */}
          <TitleEditor
            key={artifact.id}
            defaultValue={titleDraft}
            placeholder={t(($) => $.detail.untitled)}
            className="text-2xl font-semibold"
            onChange={setTitleDraft}
            onSubmit={() => saveTitle(titleDraft)}
            onBlur={() => saveTitle(titleDraft)}
          />

          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <ActorAvatar
              actorType={artifact.author_type}
              actorId={artifact.author_id}
              size={16}
            />
            <span>{authorName}</span>
            <span>·</span>
            <span>
              {t(($) => $.detail.updated_on, { date: new Date(artifact.updated_at).toLocaleDateString() })}
            </span>
            <span>·</span>
            <span>
              {artifact.verified_at
                ? t(($) => $.detail.verified_on, { date: new Date(artifact.verified_at).toLocaleDateString() })
                : t(($) => $.detail.never_verified)}
            </span>
            {artifact.tags.length > 0 && (
              <>
                <span>·</span>
                <div className="flex items-center gap-1 flex-wrap">
                  {artifact.tags.map((tag) => (
                    <span
                      key={tag}
                      className="rounded-full border px-2 py-0.5 text-[10px]"
                    >
                      {tag}
                    </span>
                  ))}
                </div>
              </>
            )}
          </div>

          {!isArchived && isStale(artifact) && (
            <div className="rounded-md border border-amber-200 bg-amber-50 dark:border-amber-800 dark:bg-amber-950/30 px-4 py-3 text-sm text-amber-800 dark:text-amber-300">
              {t(($) => $.detail.stale_warning)}
            </div>
          )}

          <div className="pt-2">
            <ContentEditor
              key={artifact.id}
              ref={contentRef}
              defaultValue={artifact.content}
              placeholder={t(($) => $.detail.content_placeholder)}
              onUpdate={saveContent}
              debounceMs={1000}
            />
          </div>
        </div>
      </div>
    </div>
  );
}
