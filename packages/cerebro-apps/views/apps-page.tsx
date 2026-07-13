"use client";

import { Blocks, Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { PageHeader } from "@multica/views/layout/page-header";
import { AppLink } from "@multica/views/navigation";
import type { CatalogApp } from "../core";

export function AppsPage() {
  const enabled = useFeatureFlag("cerebro_mini_apps");
  const workspace = useCurrentWorkspace();
  const [apps, setApps] = useState<CatalogApp[]>([]);
  const [error, setError] = useState("");
  useEffect(() => {
    if (!enabled || !workspace) return;
    fetch("/api/cerebro/apps", { credentials: "include" })
      .then(async (response) => {
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        return response.json() as Promise<{ apps: CatalogApp[] }>;
      })
      .then((result) => setApps(result.apps))
      .catch((cause: unknown) => setError(cause instanceof Error ? cause.message : "Could not load apps"));
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
    <PageHeader className="justify-between gap-3"><div><h1 className="text-sm font-semibold">Apps</h1><p className="text-[11px] text-muted-foreground">Focused tools and data workflows for this workspace</p></div><Button size="sm"><Plus className="size-4" />Build app</Button></PageHeader>
    <div className="flex-1 overflow-y-auto p-6">
      {error && <div className="mb-4 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">Failed to load apps. {error}</div>}
      {apps.length === 0 && !error && <div className="grid min-h-64 place-items-center rounded-2xl border border-dashed text-center"><div><Blocks className="mx-auto mb-3 size-8 text-muted-foreground" /><h2 className="font-semibold">No apps yet</h2><p className="mt-1 text-sm text-muted-foreground">Build the first focused tool for this workspace.</p></div></div>}
      <div className="space-y-8">{Array.from(folders, ([folder, items]) => <section key={folder}><h2 className="mb-3 text-sm font-semibold">{folder}</h2><div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">{items.map((app) => <AppLink key={app.id} href={`/${workspace.slug}/apps/${app.id}`} className="group rounded-xl border bg-card p-4 transition hover:-translate-y-0.5 hover:shadow-sm"><div className="flex items-start justify-between gap-3"><div className="grid size-10 place-items-center rounded-lg bg-muted"><Blocks className="size-5" /></div><span className="rounded-full bg-muted px-2 py-1 text-[11px] text-muted-foreground">{app.current_version ? `v${app.current_version}` : "Draft"}</span></div><h3 className="mt-4 font-semibold">{app.name}</h3><p className="mt-1 line-clamp-2 text-sm text-muted-foreground">{app.description || "No description"}</p></AppLink>)}</div></section>)}</div>
    </div>
  </div>;
}
