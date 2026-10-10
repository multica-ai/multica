"use client";

import { useState } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { LoaderCircle, RefreshCw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { gongfengRepositoriesOptions, vcsKeys } from "@multica/core/vcs";
import type { GongfengRepository, VCSConnection } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

export function GongfengRepositoryPicker({ wsId, connection, onClose, onImport }: {
  wsId: string;
  connection: VCSConnection;
  onClose: () => void;
  onImport?: (repositories: GongfengRepository[]) => Promise<boolean>;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState(new Map<number, GongfengRepository>());
  const [busy, setBusy] = useState(false);
  const query = useInfiniteQuery({
    ...gongfengRepositoriesOptions(wsId, connection.id, search),
    refetchInterval: 10_000,
  });
  const enabled = query.data?.pages[0]?.selected ?? [];
  const enabledIds = new Set(enabled.map((repo) => repo.id));
  const repositories = [...new Map(query.data?.pages.flatMap((page) => page.repositories).map((repo) => [repo.id, repo])).values()];

  async function refresh() { await qc.invalidateQueries({ queryKey: [...vcsKeys.all(wsId), "repositories", connection.id] }); }
  async function save() {
    if (busy || selected.size === 0) return;
    setBusy(true);
    try {
      const chosen = [...selected.values()];
      for (const repo of chosen) {
        if (!enabledIds.has(repo.id)) await api.enableGongfengRepository(wsId, connection.id, repo.id);
      }
      await refresh();
      if (onImport && !(await onImport(chosen))) return;
      setSelected(new Map());
      if (onImport) onClose();
    } catch (error) {
      await refresh();
      toast.error(error instanceof Error ? error.message : t(($) => $.vcs.gongfeng_sync_failed));
    } finally { setBusy(false); }
  }
  async function manage(repo: GongfengRepository, remove: boolean) {
    if (busy) return;
    setBusy(true);
    try {
      if (remove) await api.disableGongfengRepository(wsId, connection.id, repo.id);
      else await api.enableGongfengRepository(wsId, connection.id, repo.id, true);
      await refresh();
    } catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.vcs.gongfeng_sync_failed)); }
    finally { setBusy(false); }
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open && !busy) onClose(); }}>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.vcs.gongfeng_repositories)}</DialogTitle>
          <DialogDescription>{t(($) => $.vcs.gongfeng_repositories_hint)}</DialogDescription>
        </DialogHeader>
        <div className="min-h-0 space-y-4 overflow-y-auto">
          {enabled.length > 0 ? (
            <div className="space-y-2" aria-label={t(($) => $.vcs.gongfeng_selected)}>
              {enabled.map((repo) => (
                <div key={repo.id} className="flex items-start gap-2 rounded-md border p-3">
                  <div className="min-w-0 flex-1">
                    <p className="break-all text-caption font-medium">{repo.path}</p>
                    <p className={`mt-1 text-caption ${repo.sync_error ? "text-destructive" : "text-muted-foreground"}`}>
                      {repo.sync_error || (repo.syncing ? t(($) => $.vcs.gongfeng_syncing) : t(($) => $.vcs.gongfeng_synced))}
                    </p>
                    <p className={`mt-1 text-caption ${repo.webhook_configured ? "text-muted-foreground" : "text-warning"}`}>
                      {repo.webhook_configured ? t(($) => $.vcs.gongfeng_webhook_configured) : t(($) => $.vcs.gongfeng_webhook_not_configured)}
                    </p>
                  </div>
                  <Button variant="ghost" size="icon-sm" disabled={busy} aria-label={`${t(($) => $.vcs.gongfeng_retry)}: ${repo.path}`} onClick={() => void manage(repo, false)}><RefreshCw /></Button>
                  <Button variant="ghost" size="icon-sm" disabled={busy} aria-label={`${t(($) => $.vcs.gongfeng_remove)}: ${repo.path}`} onClick={() => void manage(repo, true)}><Trash2 /></Button>
                </div>
              ))}
            </div>
          ) : null}
          <Input aria-label={t(($) => $.vcs.gongfeng_search)} placeholder={t(($) => $.vcs.gongfeng_search)} value={search} onChange={(e) => setSearch(e.target.value)} disabled={busy} />
          {query.isPending ? <p role="status" className="text-caption text-muted-foreground">{t(($) => $.vcs.gongfeng_loading)}</p> : null}
          {query.error ? (
            <div role="alert" className="space-y-2 text-caption text-destructive">
              <p>{query.error.message}</p>
              <Button variant="outline" size="sm" onClick={() => void query.refetch()}>{t(($) => $.vcs.gongfeng_retry)}</Button>
            </div>
          ) : null}
          {!query.isPending && !query.error && repositories.length === 0 ? <p className="text-caption text-muted-foreground">{t(($) => $.vcs.gongfeng_empty)}</p> : null}
          <div className="space-y-1">
            {repositories.map((repo) => (
              <label key={repo.id} className={`flex cursor-pointer items-start gap-3 rounded-md p-2 hover:bg-muted ${selected.has(repo.id) ? "bg-muted" : ""}`}>
                <Checkbox checked={selected.has(repo.id)} disabled={busy || repo.archived || (!onImport && enabledIds.has(repo.id)) || (!!onImport && !repo.clone_url)} onCheckedChange={(checked) => setSelected((previous) => {
                  const next = new Map(previous); if (checked) next.set(repo.id, repo); else next.delete(repo.id); return next;
                })} />
                <span className="min-w-0 break-all text-caption">{repo.path}{repo.archived ? ` · ${t(($) => $.vcs.gongfeng_archived)}` : ""}</span>
              </label>
            ))}
          </div>
          {query.hasNextPage ? <Button variant="outline" size="sm" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>{t(($) => $.vcs.gongfeng_load_more)}</Button> : null}
        </div>
        <DialogFooter>
          <Button variant="outline" disabled={busy} onClick={onClose}>{t(($) => $.vcs.webhook_done)}</Button>
          <Button disabled={busy || selected.size === 0 || !!query.error} aria-busy={busy || undefined} onClick={() => void save()}>
            {busy ? <LoaderCircle className="animate-spin" /> : null}
            {onImport ? t(($) => $.vcs.gongfeng_import) : t(($) => $.vcs.gongfeng_enable)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
