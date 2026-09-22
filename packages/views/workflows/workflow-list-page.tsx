"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { GitBranch, Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { workflowListOptions, workflowTemplateListOptions, useCreateWorkflow, useDeleteWorkflow } from "@multica/core/workflows";
import { Button } from "@multica/ui/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";

export function WorkflowListPage() {
  const { t } = useT("workflows");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const list = useQuery(workflowListOptions(wsId));
  const templates = useQuery(workflowTemplateListOptions(wsId));
  const create = useCreateWorkflow(wsId);
  const remove = useDeleteWorkflow(wsId);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  async function handleCreate(templateId?: string, name?: string) {
    try {
      const workflow = await create.mutateAsync({ name: name ?? t(($) => $.untitled), templateId });
      setCreateOpen(false);
      navigation.push(paths.workflowDetail(workflow.id));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-5">
        <div><h1 className="text-title font-semibold">{t(($) => $.title)}</h1><p className="mt-1 text-caption text-muted-foreground">{t(($) => $.subtitle)}</p></div>
        <div className="flex flex-wrap gap-2"><Button variant="outline" onClick={() => navigation.push(paths.workflowInbox())}>{t(($) => $.work_items_title)}</Button><Button onClick={() => setCreateOpen(true)} disabled={create.isPending}><Plus className="size-4" />{t(($) => $.create)}</Button></div>
      </header>
      <div className="min-h-0 flex-1 overflow-auto p-6">
        {list.isPending ? <p role="status">{t(($) => $.loading)}</p> : list.isError ? (
          <div role="alert" className="space-y-3"><p>{list.error.message}</p><Button variant="outline" onClick={() => list.refetch()}>{t(($) => $.retry)}</Button></div>
        ) : !list.data?.length ? (
          <div className="mx-auto flex max-w-md flex-col items-center gap-4 py-24 text-center">
            <GitBranch className="size-10 text-muted-foreground" /><h2 className="text-heading font-medium">{t(($) => $.empty)}</h2><p className="text-body text-muted-foreground">{t(($) => $.empty_hint)}</p><Button onClick={() => setCreateOpen(true)} disabled={create.isPending}>{t(($) => $.create)}</Button>
          </div>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {list.data.map((workflow) => (
              <article key={workflow.id} className="group flex min-w-0 items-start gap-2 rounded-xl border bg-card p-4">
                <AppLink href={paths.workflowDetail(workflow.id)} className="min-w-0 flex-1 rounded-sm focus-visible:outline-2 focus-visible:outline-ring">
                  <GitBranch className="mb-4 size-5 text-primary" /><h2 className="truncate text-body font-semibold">{workflow.name}</h2>
                  <p className="mt-1 line-clamp-2 text-caption text-muted-foreground">{workflow.description || t(($) => $.subtitle)}</p>
                  <p className="mt-4 text-micro text-muted-foreground">{t(($) => $.last_edited, { date: new Date(workflow.updatedAt).toLocaleString() })}</p>
                </AppLink>
                <Button variant="ghost" size="icon-sm" aria-label={t(($) => $.delete)} onClick={() => setDeleteId(workflow.id)}><Trash2 className="size-4" /></Button>
              </article>
            ))}
          </div>
        )}
      </div>
      <Dialog open={createOpen} onOpenChange={(open) => { if (!create.isPending) setCreateOpen(open); }}>
        <DialogContent>
          <DialogHeader><DialogTitle>{t(($) => $.create_title)}</DialogTitle><DialogDescription>{t(($) => $.create_hint)}</DialogDescription></DialogHeader>
          <div className="grid gap-2">
            {templates.isPending && <p role="status" className="text-caption text-muted-foreground">{t(($) => $.loading)}</p>}
            {templates.data?.map((template) => <Button key={`${template.id}:${template.version}`} variant="outline" className="h-auto justify-start whitespace-normal p-3 text-left" disabled={create.isPending} onClick={() => void handleCreate(template.id, template.name)}><span><span className="block font-medium">{template.name}</span><span className="mt-1 block text-caption font-normal text-muted-foreground">{template.description}</span></span></Button>)}
            {!templates.isPending && !templates.data?.length && <Button variant="outline" disabled={create.isPending} onClick={() => void handleCreate("blank")}>{t(($) => $.untitled)}</Button>}
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={!!deleteId} onOpenChange={(open) => { if (!open && !remove.isPending) setDeleteId(null); }}>
        <DialogContent><DialogHeader><DialogTitle>{t(($) => $.delete_title)}</DialogTitle><DialogDescription>{t(($) => $.delete_hint)}</DialogDescription></DialogHeader>
          <DialogFooter><Button variant="outline" disabled={remove.isPending} onClick={() => setDeleteId(null)}>{t(($) => $.cancel)}</Button><Button variant="destructive" disabled={remove.isPending} onClick={async () => {
            if (!deleteId) return;
            try { await remove.mutateAsync(deleteId); setDeleteId(null); } catch (error) { toast.error(error instanceof Error ? error.message : t(($) => $.error)); }
          }}>{t(($) => $.delete)}</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
