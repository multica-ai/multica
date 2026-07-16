"use client";

import { Blocks, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@multica/ui/components/ui/table";
import { PageHeader } from "@multica/views/layout/page-header";
import { AppLink } from "@multica/views/navigation";
import type { AppAdminSummary, CatalogApp } from "../core";
import { installAllergenFormatter, listAppAdminOverview, listApps } from "../core/api";

export function AppsPage() {
  const enabled = useFeatureFlag("cerebro_mini_apps");
  const workspace = useCurrentWorkspace();
  const [apps, setApps] = useState<CatalogApp[]>([]);
  const [admin, setAdmin] = useState<Map<string, AppAdminSummary>>(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!enabled || !workspace) return;
    let current = true;
    setLoading(true);
    setError("");
    void listApps(workspace.slug)
      .then((result) => { if (current) setApps(result.apps); })
      .catch((cause: unknown) => { if (current) setError(cause instanceof Error ? cause.message : "Could not load apps"); })
      .finally(() => { if (current) setLoading(false); });
    void listAppAdminOverview(workspace.slug)
      .then((items) => { if (current) setAdmin(new Map(items.map((item) => [item.id, item]))); })
      .catch(() => undefined);
    return () => { current = false; };
  }, [enabled, workspace]);

  if (!enabled) return null;
  if (!workspace) return <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading workspace context…</div>;

  const refresh = () => listApps(workspace.slug).then((result) => setApps(result.apps));

  return <div className="flex h-full flex-col">
    <PageHeader className="justify-between gap-3">
      <div><h1 className="text-sm font-semibold">Apps</h1><p className="text-[11px] text-muted-foreground">Focused tools built with the Multica app SDK</p></div>
      <AppLink href={`/${workspace.slug}/apps/new`} className={buttonVariants({ size: "sm" })}><Plus className="size-4" />Build app</AppLink>
    </PageHeader>
    <div className="min-h-0 flex-1 overflow-y-auto p-3 sm:p-6">
      {error && <div className="mb-4 rounded-lg border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">Failed to load apps. {error}</div>}
      {loading ? <div className="grid min-h-48 place-items-center text-sm text-muted-foreground">Loading apps…</div> : null}
      {!loading && apps.length === 0 && !error ? <EmptyApps onInstall={() => installAllergenFormatter(workspace.slug).then(refresh).catch((cause: unknown) => setError(cause instanceof Error ? cause.message : "Could not install Allergen Formatter"))} /> : null}
      {!loading && apps.length > 0 ? <div className="overflow-x-auto rounded-lg border bg-card">
        <Table aria-label="Workspace apps" className="min-w-[760px]">
          <TableHeader><TableRow>
            <TableHead className="w-[34%]">App</TableHead><TableHead>Collection</TableHead><TableHead>Owner</TableHead><TableHead>Version</TableHead><TableHead>Status</TableHead><TableHead>Health</TableHead>
          </TableRow></TableHeader>
          <TableBody>{apps.map((app) => {
            const summary = admin.get(app.id);
            return <TableRow key={app.id}>
              <TableCell><AppLink href={`/${workspace.slug}/apps/${app.id}`} className="flex min-w-0 items-center gap-3 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">
                <span className="grid size-8 shrink-0 place-items-center rounded-md bg-muted"><Blocks className="size-4" /></span>
                <span className="min-w-0"><span className="block truncate font-medium">{app.name}</span><span className="block truncate text-xs text-muted-foreground">{app.description || "No description"}</span></span>
              </AppLink></TableCell>
              <TableCell>{app.folder || "Unassigned"}</TableCell>
              <TableCell>{summary?.owner || "Unassigned"}</TableCell>
              <TableCell className="font-mono text-xs">{app.current_version || "Draft"}</TableCell>
              <TableCell><Badge variant="outline">{titleCase(app.status)}</Badge></TableCell>
              <TableCell><span className="inline-flex items-center gap-2"><span className={`size-2 rounded-full ${healthTone(summary?.health)}`} />{titleCase(summary?.health ?? "unknown")}</span></TableCell>
            </TableRow>;
          })}</TableBody>
        </Table>
      </div> : null}
    </div>
  </div>;
}

function EmptyApps({ onInstall }: { onInstall: () => void }) {
  return <div className="grid min-h-64 place-items-center rounded-xl border border-dashed text-center"><div><Blocks className="mx-auto mb-3 size-8 text-muted-foreground" /><h2 className="font-semibold">No apps yet</h2><p className="mt-1 text-sm text-muted-foreground">Build the first focused tool for this workspace.</p><Button className="mt-4" variant="outline" onClick={onInstall}>Install Allergen Formatter</Button></div></div>;
}

function titleCase(value: string): string {
  return value.charAt(0).toUpperCase() + value.slice(1).replaceAll("_", " ");
}

function healthTone(health?: AppAdminSummary["health"]): string {
  if (health === "healthy") return "bg-emerald-500";
  if (health === "attention") return "bg-amber-500";
  return "bg-muted-foreground/50";
}
