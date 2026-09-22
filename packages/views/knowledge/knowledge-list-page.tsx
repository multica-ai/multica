"use client";

import { useState } from "react";
import { Library, Plus } from "lucide-react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { knowledgeBaseListOptions, useCreateKnowledgeBase } from "@multica/core/knowledge";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";

export function KnowledgeListPage() {
  const { t } = useT("knowledge");
  const workspaceId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const list = useInfiniteQuery(knowledgeBaseListOptions(workspaceId));
  const bases = list.data?.pages.flatMap((page) => page.bases) ?? [];
  const create = useCreateKnowledgeBase();
  const [formOpen, setFormOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [visibility, setVisibility] = useState<"private" | "workspace">("private");

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    try {
      const base = await create.mutateAsync({ name, description, visibility });
      setFormOpen(false);
      setName("");
      setDescription("");
      navigation.push(paths.knowledgeBase(base.id));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t(($) => $.error));
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex flex-wrap items-center justify-between gap-3 border-b px-6 py-5">
        <div>
          <h1 className="text-title font-semibold">{t(($) => $.title)}</h1>
          <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.subtitle)}</p>
        </div>
        <Button onClick={() => setFormOpen((open) => !open)}>
          <Plus className="size-4" />{t(($) => $.create)}
        </Button>
      </header>
      {formOpen && (
        <form onSubmit={submit} className="grid gap-3 border-b bg-muted/20 p-6 md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_12rem]">
          <div className="space-y-1.5">
            <Label htmlFor="knowledge-base-name">{t(($) => $.name)}</Label>
            <Input id="knowledge-base-name" value={name} onChange={(event) => setName(event.target.value)} required maxLength={200} autoFocus />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="knowledge-base-description">{t(($) => $.description)}</Label>
            <Textarea id="knowledge-base-description" value={description} onChange={(event) => setDescription(event.target.value)} rows={2} />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="knowledge-base-visibility">{t(($) => $.visibility)}</Label>
            <select id="knowledge-base-visibility" className="flex h-9 w-full rounded-md border bg-background px-3 text-caption" value={visibility} onChange={(event) => setVisibility(event.target.value as "private" | "workspace")}>
              <option value="private">{t(($) => $.private)}</option>
              <option value="workspace">{t(($) => $.workspace)}</option>
            </select>
          </div>
          <div className="flex items-end gap-2 md:col-span-3">
            <Button type="submit" disabled={create.isPending || !name.trim()}>{t(($) => $.create_submit)}</Button>
            <Button type="button" variant="ghost" onClick={() => setFormOpen(false)}>{t(($) => $.cancel)}</Button>
          </div>
        </form>
      )}
      <div className="min-h-0 flex-1 overflow-auto p-6">
        {list.isPending ? <p role="status">{t(($) => $.loading)}</p> : list.isError ? (
          <div role="alert" className="space-y-3"><p>{list.error.message}</p><Button variant="outline" onClick={() => list.refetch()}>{t(($) => $.retry)}</Button></div>
        ) : !bases.length ? (
          <div className="mx-auto flex max-w-md flex-col items-center gap-4 py-24 text-center">
            <Library className="size-10 text-muted-foreground" />
            <h2 className="text-heading font-medium">{t(($) => $.empty)}</h2>
            <p className="text-body text-muted-foreground">{t(($) => $.empty_hint)}</p>
            <Button onClick={() => setFormOpen(true)}><Plus className="size-4" />{t(($) => $.create)}</Button>
          </div>
        ) : (
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
            {bases.map((base) => (
              <AppLink key={base.id} href={paths.knowledgeBase(base.id)} className="rounded-xl border bg-card p-5 transition-colors hover:bg-accent/40 focus-visible:outline-2 focus-visible:outline-ring">
                <div className="flex items-start gap-3"><Library className="mt-0.5 size-5 shrink-0 text-primary" /><div className="min-w-0"><h2 className="truncate text-body font-semibold">{base.name}</h2><p className="mt-1 text-micro text-muted-foreground">{base.visibility === "workspace" ? t(($) => $.workspace) : t(($) => $.private)}</p></div></div>
                <p className="mt-4 line-clamp-3 text-caption text-muted-foreground">{base.description || t(($) => $.subtitle)}</p>
                <p className="mt-4 text-micro text-muted-foreground">{t(($) => $.created, { date: new Date(base.created_at).toLocaleString() })}</p>
              </AppLink>
            ))}
            {list.hasNextPage ? <div className="col-span-full flex justify-center pt-2"><Button variant="outline" onClick={() => void list.fetchNextPage()} disabled={list.isFetchingNextPage}>{list.isFetchingNextPage ? t(($) => $.loading) : t(($) => $.load_more)}</Button></div> : null}
          </div>
        )}
      </div>
    </div>
  );
}
