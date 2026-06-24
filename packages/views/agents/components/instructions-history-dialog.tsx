"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Eye, History, Loader2, RotateCcw } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
import { ScrollArea } from "@multica/ui/components/ui/scroll-area";
import { api, ApiError } from "@multica/core/api";
import type { InstructionsHistoryItem, InstructionsHistoryScope } from "@multica/core/types";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

export const instructionsHistoryKey = (workspaceId: string, scope: InstructionsHistoryScope) =>
  ["workspaces", workspaceId, "instructions-history", scope] as const;

interface InstructionsHistoryDialogProps {
  workspaceId: string;
  scope: InstructionsHistoryScope;
  open: boolean;
  currentContent: string;
  onOpenChange: (open: boolean) => void;
  onRestore: (content: string) => Promise<void>;
}

// Centered modal (was a right-side Sheet). Shows the instructions version
// history for a scope's default config — list on the left, selected version's
// content on the right.
export function InstructionsHistoryDialog({
  workspaceId,
  scope,
  open,
  currentContent: _currentContent,
  onOpenChange,
  onRestore,
}: InstructionsHistoryDialogProps) {
  const { t } = useT("agents");
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [restoreCandidate, setRestoreCandidate] = useState<InstructionsHistoryItem | null>(null);
  const [restoring, setRestoring] = useState(false);

  const historyQuery = useQuery({
    queryKey: instructionsHistoryKey(workspaceId, scope),
    queryFn: () => api.listInstructionsHistory(workspaceId, scope),
    enabled: open,
  });

  const selectedVersion = useQuery({
    queryKey: [...instructionsHistoryKey(workspaceId, scope), selectedId],
    queryFn: () => api.getInstructionsHistory(workspaceId, selectedId!, scope),
    enabled: open && selectedId !== null,
  });

  const selectedItem = useMemo(
    () => historyQuery.data?.items.find((item) => item.id === selectedId) ?? null,
    [historyQuery.data?.items, selectedId],
  );

  const confirmRestore = async () => {
    if (!restoreCandidate) return;
    setRestoring(true);
    try {
      const detail = selectedVersion.data?.id === restoreCandidate.id
        ? selectedVersion.data
        : await api.getInstructionsHistory(workspaceId, restoreCandidate.id, scope);
      await onRestore(detail.content);
      setRestoreCandidate(null);
      setSelectedId(detail.id);
      toast.success(t(($) => $.history.restored_toast));
    } catch (e) {
      const msg = e instanceof ApiError ? e.message
        : e instanceof Error ? e.message
        : t(($) => $.history.restore_failed_toast);
      toast.error(msg);
    } finally {
      setRestoring(false);
    }
  };

  const items = historyQuery.data?.items ?? [];
  const selectedContent = selectedVersion.data?.content ?? "";
  const formatRelativeTime = (value: string) => {
    const time = new Date(value).getTime();
    if (!Number.isFinite(time)) return value;
    const diffMs = Date.now() - time;
    const minutes = Math.max(0, Math.floor(diffMs / 60000));
    if (minutes < 1) return t(($) => $.history.relative_now);
    if (minutes < 60) return t(($) => $.history.relative_minutes, { count: minutes });
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return t(($) => $.history.relative_hours, { count: hours });
    const days = Math.floor(hours / 24);
    if (days < 30) return t(($) => $.history.relative_days, { count: days });
    return new Date(value).toLocaleString();
  };

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="flex h-[75vh] max-h-[calc(100vh-2rem)] max-w-4xl flex-col gap-0 p-0 sm:max-w-4xl">
          <DialogHeader className="flex h-12 shrink-0 flex-row items-center gap-2 border-b px-4">
            <History className="h-4 w-4 text-muted-foreground" />
            <DialogTitle className="text-sm">{t(($) => $.history.title)}</DialogTitle>
            <DialogDescription className="sr-only">
              {scope === "system"
                ? t(($) => $.history.system_description)
                : t(($) => $.history.personal_description)}
            </DialogDescription>
          </DialogHeader>

          <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-2">
            <ScrollArea className="min-h-0 border-r">
              <div className="space-y-2 p-4">
                {historyQuery.isLoading && (
                  <div className="flex h-28 items-center justify-center">
                    <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                  </div>
                )}

                {historyQuery.isError && (
                  <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm text-destructive">
                    {t(($) => $.history.load_failed)}
                  </div>
                )}

                {!historyQuery.isLoading && !historyQuery.isError && items.length === 0 && (
                  <div className="rounded-md border border-dashed px-3 py-8 text-center text-sm text-muted-foreground">
                    {t(($) => $.history.empty)}
                  </div>
                )}

                {items.map((item, index) => {
                  const selected = item.id === selectedId;
                  const isCurrent = index === 0;
                  return (
                    <div
                      key={item.id}
                      className={`rounded-md border p-3 transition-colors ${
                        selected ? "border-foreground/30 bg-accent/50" : "bg-background hover:bg-accent/30"
                      }`}
                    >
                      <div className="flex items-start gap-3">
                        {item.actor_user_id ? (
                          <ActorAvatar actorType="member" actorId={item.actor_user_id} size={28} />
                        ) : (
                          <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-muted text-xs text-muted-foreground">
                            ?
                          </div>
                        )}
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                            <span className="text-sm font-medium">
                              {isCurrent
                                ? t(($) => $.history.current_version)
                                : t(($) => $.history.version_label, { count: items.length - index })}
                            </span>
                            <span className="text-xs text-muted-foreground">{formatRelativeTime(item.created_at)}</span>
                          </div>
                          <div className="mt-0.5 truncate text-xs text-muted-foreground">
                            {item.actor_name ?? t(($) => $.history.unknown_actor)}
                          </div>
                          <p className="mt-2 line-clamp-2 whitespace-pre-wrap text-xs text-muted-foreground">
                            {item.content_preview || t(($) => $.history.empty_content)}
                          </p>
                        </div>
                      </div>
                      <div className="mt-3 flex justify-end gap-2">
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          className="h-7 gap-1 text-xs"
                          onClick={() => setSelectedId(item.id)}
                        >
                          <Eye className="h-3 w-3" />
                          {t(($) => $.history.view)}
                        </Button>
                        {!isCurrent && (
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            className="h-7 gap-1 text-xs"
                            onClick={() => {
                              setSelectedId(item.id);
                              setRestoreCandidate(item);
                            }}
                          >
                            <RotateCcw className="h-3 w-3" />
                            {t(($) => $.history.restore)}
                          </Button>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </ScrollArea>

            <div className="flex min-h-0 flex-col">
              <div className="flex h-10 shrink-0 items-center justify-between border-b px-4">
                <div className="truncate text-sm font-medium">
                  {selectedItem ? t(($) => $.history.version_content) : t(($) => $.history.select_version)}
                </div>
                {selectedVersion.isFetching && <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />}
              </div>
              <ScrollArea className="min-h-0 flex-1">
                <pre className="whitespace-pre-wrap break-words p-4 font-mono text-xs leading-relaxed text-muted-foreground">
                  {selectedItem
                    ? selectedContent || (selectedVersion.isLoading ? t(($) => $.history.loading_content) : t(($) => $.history.empty_content))
                    : t(($) => $.history.select_hint)}
                </pre>
              </ScrollArea>
            </div>
          </div>
        </DialogContent>
      </Dialog>

      <AlertDialog open={restoreCandidate !== null} onOpenChange={(v) => { if (!v && !restoring) setRestoreCandidate(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.history.restore_dialog_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.history.restore_dialog_description, {
                actor: restoreCandidate?.actor_name ?? t(($) => $.history.unknown_actor),
                time: restoreCandidate ? formatRelativeTime(restoreCandidate.created_at) : "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={restoring}>{t(($) => $.history.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={confirmRestore} disabled={restoring}>
              {restoring && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {t(($) => $.history.restore_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
