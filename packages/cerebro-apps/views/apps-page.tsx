"use client";

import { Blocks, Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { PageHeader } from "@multica/views/layout/page-header";
import { AppLink } from "@multica/views/navigation";
import type { AppAdminSummary, AppFolder, CatalogApp } from "../core";
import { createAppFolder, deleteAppFolder, installAllergenFormatter, listAppAdminOverview, listAppFolders, listApps, moveAppToFolder, updateAppFolder } from "../core/api";

export function AppsPage() {
  const enabled = useFeatureFlag("cerebro_mini_apps");
  const workspace = useCurrentWorkspace();
  const [apps, setApps] = useState<CatalogApp[]>([]);
  const [admin, setAdmin] = useState<Map<string, AppAdminSummary>>(new Map());
  const [appFolders, setAppFolders] = useState<AppFolder[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!enabled || !workspace) return;
    listApps(workspace.slug)
      .then((result) => setApps(result.apps))
      .catch((cause: unknown) => setError(cause instanceof Error ? cause.message : "Could not load apps"));
    listAppAdminOverview(workspace.slug).then((items) => setAdmin(new Map(items.map((item) => [item.id, item])))).catch(() => undefined);
    listAppFolders(workspace.slug).then(setAppFolders).catch(() => undefined);
  }, [enabled, workspace]);
  const folders = useMemo(() => {
    const grouped = new Map<string, CatalogApp[]>();
    for (const app of apps) {
      const folder = app.folder || "Workspace apps";
      grouped.set(folder, [...(grouped.get(folder) ?? []), app]);
    }
    return grouped;
  }, [apps]);
  if (!enabled) return null;
  if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;
  return <div className="flex h-full flex-col">
    <PageHeader className="justify-between gap-3"><div><h1 className="text-sm font-semibold">Apps</h1><p className="text-[11px] text-muted-foreground">Focused tools and data workflows for this workspace</p></div><AppLink href={`/${workspace.slug}/apps/new`} className={buttonVariants({ size: "sm" })}><Plus className="size-4" />Build app</AppLink></PageHeader>
    <div className="flex-1 overflow-y-auto p-6">
      {error && <div className="mb-4 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">Failed to load apps. {error}</div>}
      {admin.size > 0 ? <FolderManager apps={apps} folders={appFolders} workspaceSlug={workspace.slug} refresh={() => Promise.all([listApps(workspace.slug).then((result) => setApps(result.apps)), listAppFolders(workspace.slug).then(setAppFolders)]).then(() => undefined)} /> : null}
      {apps.length === 0 && !error && <div className="grid min-h-64 place-items-center rounded-2xl border border-dashed text-center"><div><Blocks className="mx-auto mb-3 size-8 text-muted-foreground" /><h2 className="font-semibold">No apps yet</h2><p className="mt-1 text-sm text-muted-foreground">Build the first focused tool for this workspace.</p><Button className="mt-4" variant="outline" onClick={() => installAllergenFormatter(workspace.slug).then(() => listApps(workspace.slug)).then((result) => setApps(result.apps)).catch((cause: unknown) => setError(cause instanceof Error ? cause.message : "Could not install Allergen Formatter"))}>Install Allergen Formatter</Button></div></div>}
      <div className="space-y-8">{Array.from(folders, ([folder, items]) => <section key={folder}><h2 className="mb-3 text-sm font-semibold">{folder}</h2><div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{items.map((app) => <AppLink key={app.id} href={`/${workspace.slug}/apps/${app.id}`} className="group rounded-xl border bg-card p-4 transition hover:-translate-y-0.5 hover:shadow-sm"><div className="flex items-start justify-between gap-3"><div className="grid size-10 place-items-center rounded-lg bg-muted"><Blocks className="size-5" /></div><span className="rounded-full bg-muted px-2 py-1 text-[11px] text-muted-foreground">{app.current_version ? `v${app.current_version}` : "Draft"}</span></div><h3 className="mt-4 font-semibold">{app.name}</h3><p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{app.description || "No description"}</p>{admin.get(app.id) ? <AppAdminMeta summary={admin.get(app.id)!} /> : null}</AppLink>)}</div></section>)}</div>
    </div>
  </div>;
}

function FolderManager({ apps, folders, refresh, workspaceSlug }: { apps: CatalogApp[]; folders: AppFolder[]; refresh: () => Promise<void>; workspaceSlug: string }) {
  const [name, setName] = useState("");
  const [parent, setParent] = useState("");
  const [appId, setAppId] = useState(apps[0]?.id ?? "");
  const [folderId, setFolderId] = useState(folders[0]?.id ?? "");
  return <section className="mb-6 rounded-xl border bg-card p-4" aria-label="App folder management">
    <h2 className="font-semibold">Catalog folders</h2><p className="mb-3 text-xs text-muted-foreground">Create nested folders, rename them, and move apps.</p>
    <div className="grid gap-2 md:grid-cols-[1fr_1fr_auto]"><input className="rounded-md border bg-background px-3 py-2 text-sm" aria-label="New folder name" value={name} onChange={(event) => setName(event.target.value)} placeholder="Folder name" /><select className="rounded-md border bg-background px-3 py-2 text-sm" aria-label="Parent folder" value={parent} onChange={(event) => setParent(event.target.value)}><option value="">Top level</option>{folders.map((folder) => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</select><Button size="sm" disabled={!name.trim()} onClick={() => createAppFolder(name, parent, workspaceSlug).then(() => { setName(""); return refresh(); })}>Create folder</Button></div>
    <div className="mt-3 space-y-2">{folders.map((folder) => <FolderRow key={folder.id} folder={folder} folders={folders} refresh={refresh} workspaceSlug={workspaceSlug} />)}</div>
    {apps.length > 0 && folders.length > 0 ? <div className="mt-3 grid gap-2 border-t pt-3 md:grid-cols-[1fr_1fr_auto]"><select className="rounded-md border bg-background px-3 py-2 text-sm" aria-label="App to move" value={appId} onChange={(event) => setAppId(event.target.value)}>{apps.map((app) => <option key={app.id} value={app.id}>{app.name}</option>)}</select><select className="rounded-md border bg-background px-3 py-2 text-sm" aria-label="Destination folder" value={folderId} onChange={(event) => setFolderId(event.target.value)}>{folders.map((folder) => <option key={folder.id} value={folder.id}>{folder.name}</option>)}</select><Button size="sm" onClick={() => moveAppToFolder(folderId, appId, workspaceSlug).then(refresh)}>Move app</Button></div> : null}
  </section>;
}

function FolderRow({ folder, folders, refresh, workspaceSlug }: { folder: AppFolder; folders: AppFolder[]; refresh: () => Promise<void>; workspaceSlug: string }) {
  const [name, setName] = useState(folder.name);
  const [parent, setParent] = useState(folder.parent_id ?? "");
  return <div className="grid gap-2 md:grid-cols-[1fr_1fr_auto_auto]"><input className="rounded-md border bg-background px-3 py-2 text-sm" aria-label={`Rename ${folder.name}`} value={name} onChange={(event) => setName(event.target.value)} /><select className="rounded-md border bg-background px-3 py-2 text-sm" aria-label={`Parent for ${folder.name}`} value={parent} onChange={(event) => setParent(event.target.value)}><option value="">Top level</option>{folders.filter((candidate) => candidate.id !== folder.id).map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.name}</option>)}</select><Button size="sm" variant="outline" onClick={() => updateAppFolder(folder.id, name, parent, workspaceSlug).then(refresh)}>Save</Button><Button size="sm" variant="ghost" onClick={() => deleteAppFolder(folder.id, workspaceSlug).then(refresh)}>Delete</Button></div>;
}

function AppAdminMeta({ summary }: { summary: AppAdminSummary }) {
  return <dl className="mt-4 grid grid-cols-2 gap-2 border-t pt-3 text-xs">
    <div><dt className="text-muted-foreground">Owner</dt><dd>{summary.owner || "Unassigned"}</dd></div>
    <div><dt className="text-muted-foreground">Health</dt><dd>{summary.health}</dd></div>
    <div><dt className="text-muted-foreground">Runs</dt><dd>{summary.runs} ({summary.failed_runs} failed)</dd></div>
    <div><dt className="text-muted-foreground">Spend</dt><dd>{(summary.spend_cents / 100).toFixed(2)}</dd></div>
    <div className="col-span-2"><dt className="text-muted-foreground">Approved scopes</dt><dd>{summary.approved_scopes.length}</dd></div>
    <div className="col-span-2"><dt className="text-muted-foreground">Data touched</dt><dd className="truncate">{summary.touched.join(", ") || "None"}</dd></div>
  </dl>;
}
