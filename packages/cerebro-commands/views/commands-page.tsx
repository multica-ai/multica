"use client";

import { useMemo, useState, type FormEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Pencil, Plus, SquareTerminal, Trash2, X } from "lucide-react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@multica/ui/components/ui/table";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@multica/ui/components/ui/alert-dialog";
import { createCommand, deleteCommand, updateCommand } from "../api";
import { commandKeys, commandsListOptions } from "../queries";
import type { CerebroCommand, CommandInput } from "../types";

const EMPTY_FORM: CommandInput = { key: "", title: "", description: "", argv: [""] };

export function CommandsPage() {
  const enabled = useFeatureFlag("cerebro_workflows");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const workspaceId = workspace?.id ?? "";
  const list = useQuery(commandsListOptions(workspaceId));
  const [search, setSearch] = useState("");
  const [form, setForm] = useState<CommandInput>(EMPTY_FORM);
  const [editing, setEditing] = useState<CerebroCommand | null>(null);
  const [deleting, setDeleting] = useState<CerebroCommand | null>(null);
  const [showForm, setShowForm] = useState(false);

  const closeForm = () => { setForm(EMPTY_FORM); setEditing(null); setShowForm(false); };
  const startCreate = () => { setForm(EMPTY_FORM); setEditing(null); setShowForm(true); };
  const startEdit = (item: CerebroCommand) => { setForm({ key: item.key, title: item.title, description: item.description, argv: [...item.argv] }); setEditing(item); setShowForm(true); };

  const save = useMutation({
    mutationFn: () => {
      const input = { ...form, argv: form.argv.map((arg) => arg.trim()).filter(Boolean) };
      return editing ? updateCommand(editing.id, input) : createCommand(input);
    },
    onSuccess: () => { queryClient.invalidateQueries({ queryKey: commandKeys.list(workspaceId) }); closeForm(); },
  });
  const remove = useMutation({ mutationFn: deleteCommand, onSuccess: () => { queryClient.invalidateQueries({ queryKey: commandKeys.list(workspaceId) }); setDeleting(null); } });

  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    if (!query) return list.data ?? [];
    return (list.data ?? []).filter((item) => `${item.title} ${item.key} ${item.description} ${item.argv.join(" ")}`.toLowerCase().includes(query));
  }, [list.data, search]);
  // "Nothing here" and "nothing matched your search" are different answers, and
  // only the second one tells the user to clear the box.
  const emptyMessage = search.trim() ? "No commands match your search" : "No commands yet";

  if (!enabled) return null;
  if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;

  const submit = (event: FormEvent) => { event.preventDefault(); save.mutate(); };
  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3">
        <div className="flex items-center gap-3">
          <Button size="icon-sm" variant="ghost" aria-label="Back to workflows" onClick={() => navigation.push(`/${workspace.slug}/workflows`)}><ArrowLeft /></Button>
          <div className="flex min-w-0 flex-col"><h1 className="text-sm font-semibold">Command library</h1><p className="truncate text-xs text-muted-foreground">Reusable, exact command arguments for Issue workflows</p></div>
        </div>
        <Button size="sm" onClick={showForm ? closeForm : startCreate}>{showForm ? <X className="size-4" /> : <Plus className="size-4" />}{showForm ? "Close" : "New command"}</Button>
      </PageHeader>
      <div className="min-h-0 flex-1 overflow-y-auto p-6">
        <div className="mx-auto flex max-w-5xl flex-col gap-5">
          {showForm && (
            <form onSubmit={submit} className="grid gap-4 rounded-lg border bg-card p-5 shadow-sm">
              <div><h2 className="text-sm font-semibold">{editing ? "Edit command" : "Create command"}</h2><p className="text-xs text-muted-foreground">Each argument is stored separately, so spaces and flags are never reinterpreted by a shell.</p></div>
              <div className="grid gap-4 sm:grid-cols-2"><Field label="Title"><Input required value={form.title} onChange={(event) => setForm((current) => ({ ...current, title: event.target.value }))} placeholder="Frontend tests" /></Field><Field label="Key"><Input required value={form.key} onChange={(event) => setForm((current) => ({ ...current, key: event.target.value }))} placeholder="frontend-tests" /></Field></div>
              <Field label="Description"><Textarea rows={2} value={form.description} onChange={(event) => setForm((current) => ({ ...current, description: event.target.value }))} placeholder="What this command verifies and when to use it" /></Field>
              <Field label="Arguments">
                <div className="grid gap-2">
                  {form.argv.map((arg, index) => <div key={index} className="flex gap-2"><Input required value={arg} aria-label={`Argument ${index + 1}`} onChange={(event) => setForm((current) => ({ ...current, argv: current.argv.map((value, itemIndex) => itemIndex === index ? event.target.value : value) }))} placeholder={index === 0 ? "pnpm" : "test"} /><Button type="button" size="icon" variant="ghost" aria-label={`Remove argument ${index + 1}`} disabled={form.argv.length === 1} onClick={() => setForm((current) => ({ ...current, argv: current.argv.filter((_, itemIndex) => itemIndex !== index) }))}><Trash2 className="size-4" /></Button></div>)}
                  <Button type="button" size="sm" variant="outline" className="w-fit" onClick={() => setForm((current) => ({ ...current, argv: [...current.argv, ""] }))}><Plus className="size-4" />Add argument</Button>
                </div>
              </Field>
              {save.isError && <p className="text-sm text-destructive">{save.error instanceof Error ? save.error.message : "Could not save command"}</p>}
              <Button type="submit" className="w-fit" disabled={save.isPending || form.argv.every((arg) => !arg.trim())}>{save.isPending ? "Saving…" : "Save command"}</Button>
            </form>
          )}

          <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="text-sm font-semibold">Catalog</h2><p className="text-xs text-muted-foreground">Choose these commands from any Command step.</p></div><Input className="max-w-xs" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search commands…" /></div>
          {list.isError && <p className="text-sm text-destructive">Failed to load commands.</p>}
          {list.isLoading ? <div className="grid gap-2" aria-label="Loading commands">{[0, 1, 2].map((row) => <div key={row} className="h-14 animate-pulse rounded-lg border bg-muted/40" />)}</div> : filtered.length === 0 ? <div className="rounded-lg border border-dashed p-10 text-center"><SquareTerminal className="mx-auto mb-3 size-6 text-muted-foreground" /><p className="text-sm font-medium">{emptyMessage}</p><p className="mt-1 text-xs text-muted-foreground">Create one here, then reuse it in an Issue workflow.</p></div> : (
            <>
              {/* Desktop: the full table. Mobile: the same rows as cards, so the
                  actions stay on screen instead of behind a sideways scroll. */}
              <div className="hidden overflow-hidden rounded-lg border md:block">
                <Table><TableHeader><TableRow><TableHead>Command</TableHead><TableHead>Arguments</TableHead><TableHead className="text-right">Actions</TableHead></TableRow></TableHeader><TableBody>{filtered.map((item) => <TableRow key={item.id}><TableCell><div className="font-medium">{item.title}</div><div className="text-xs text-muted-foreground">{item.description || item.key}</div></TableCell><TableCell><code className="break-all text-xs">{item.argv.join(" ")}</code></TableCell><TableCell><div className="flex items-center justify-end gap-1"><Badge variant="outline">{item.key}</Badge><Button size="icon-sm" variant="ghost" aria-label={`Edit ${item.title}`} onClick={() => startEdit(item)}><Pencil className="size-4" /></Button><Button size="icon-sm" variant="ghost" aria-label={`Delete ${item.title}`} onClick={() => setDeleting(item)}><Trash2 className="size-4" /></Button></div></TableCell></TableRow>)}</TableBody></Table>
              </div>

              <div className="grid gap-3 md:hidden" aria-label="Commands">
                {filtered.map((item) => <article key={item.id} className="grid min-w-0 gap-3 rounded-lg border bg-card p-4 text-card-foreground">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2"><span className="truncate font-medium">{item.title}</span><Badge variant="outline">{item.key}</Badge></div>
                    {item.description && <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">{item.description}</p>}
                  </div>
                  <code className="min-w-0 break-all text-xs text-muted-foreground">{item.argv.join(" ")}</code>
                  <div className="flex flex-wrap items-center gap-2 border-t pt-3">
                    <Button size="sm" variant="ghost" className="mr-auto" onClick={() => startEdit(item)}><Pencil className="size-4" />Edit</Button>
                    <Button size="sm" variant="ghost" className="text-destructive" onClick={() => setDeleting(item)}><Trash2 className="size-4" />Delete</Button>
                  </div>
                </article>)}
              </div>
            </>
          )}
        </div>
      </div>
      <AlertDialog open={deleting !== null} onOpenChange={(open) => { if (!open) setDeleting(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader><AlertDialogTitle>Delete command?</AlertDialogTitle><AlertDialogDescription>Delete “{deleting?.title}” from the shared library. Saved workflows keep their copied arguments.</AlertDialogDescription></AlertDialogHeader>
          <AlertDialogFooter><AlertDialogCancel>Cancel</AlertDialogCancel><AlertDialogAction className="bg-destructive text-destructive-foreground hover:bg-destructive/90" disabled={!deleting || remove.isPending} onClick={() => deleting && remove.mutate(deleting.id)}>{remove.isPending ? "Deleting…" : "Delete command"}</AlertDialogAction></AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <div className="grid gap-1.5"><Label>{label}</Label>{children}</div>;
}
