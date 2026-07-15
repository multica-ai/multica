"use client";

import { useEffect, useState } from "react";
import { Play, Workflow } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { PageHeader } from "@multica/views/layout/page-header";
import type { AppDetail, AppWorkflowDefinition } from "../core";
import { createWorkflow, getAppDetail, testWorkflow } from "../core/api";
import { AppViewFrame } from "./app-view-frame";
import { WorkflowBuilder } from "./workflow-builder";

const demoWorkflow: AppWorkflowDefinition = { schema_version: "1", trigger: { id: "trigger", type: "manual", config: {} }, steps: [{ id: "read", type: "registry.read", config: { resource_id: "products" } }] };

export function AppDetailPage({ appId, runtimeBaseUrl }: { appId: string; runtimeBaseUrl: string }) {
  const enabled = useFeatureFlag("cerebro_mini_apps");
  const workspace = useCurrentWorkspace();
  const [app, setApp] = useState<AppDetail>();
  const [workflow, setWorkflow] = useState<AppWorkflowDefinition>(demoWorkflow);
  const [sample, setSample] = useState<unknown>({ source: "app-detail" });
  const [result, setResult] = useState("");
  const [error, setError] = useState("");
  useEffect(() => { if (enabled && workspace) getAppDetail(appId, workspace.slug).then((value) => { setApp(value); if (value.workflows[0]) setWorkflow(value.workflows[0].definition); }).catch((cause) => setError(cause instanceof Error ? cause.message : "Could not load app")); }, [appId, enabled, workspace]);
  if (!enabled) return null;
  if (error) return <div role="alert" className="p-6 text-sm text-destructive">{error}</div>;
  if (!workspace) return <div className="grid h-full place-items-center text-sm text-muted-foreground">Loading workspace context…</div>;
  if (!app) return <div className="grid h-full place-items-center text-sm text-muted-foreground">Loading app…</div>;
  const version = app.current_version;

  return <div className="flex h-full flex-col">
    <PageHeader className="justify-between gap-3"><div><h1 className="text-sm font-semibold">{app.name}</h1><p className="text-[11px] text-muted-foreground">{app.description}</p></div><span className="rounded-full bg-muted px-2 py-1 text-xs">{version ? `v${version}` : "Draft"}</span></PageHeader>
    <div className="grid flex-1 min-h-0 gap-4 overflow-y-auto p-4 lg:grid-cols-[minmax(0,1.2fr)_minmax(360px,0.8fr)] lg:p-6">
      <section className="min-h-[420px] overflow-hidden rounded-xl border bg-card">
        <div className="border-b px-4 py-3"><h2 className="font-semibold">App preview</h2><p className="text-xs text-muted-foreground">The published app runs in its isolated view.</p></div>
        {version ? <AppViewFrame title={app.name} src={`${runtimeBaseUrl.replace(/\/$/, "")}/apps/${app.id}/${version}/index.html`} /> : <div className="grid min-h-80 place-items-center text-sm text-muted-foreground">Publish the first version to open the app.</div>}
      </section>
      <section className="rounded-xl border bg-card p-4">
        <div className="mb-4 flex items-start justify-between gap-3"><div><h2 className="flex items-center gap-2 font-semibold"><Workflow className="size-4" />Workflow</h2><p className="text-xs text-muted-foreground">Configure the same JSON contract that agents and the API use.</p></div><Button size="sm" onClick={async () => {
          setResult(""); setError("");
          try {
            let workflowId = app.workflows[0]?.id;
            if (!workflowId) workflowId = (await createWorkflow({ app_id: app.id, name: `${app.name} workflow`, version: "1.0.0", definition: workflow }, workspace.slug)).id;
            const run = await testWorkflow(workflowId, sample, workspace.slug);
            setResult(run.status === "succeeded" ? "Workflow test succeeded" : `Workflow test ${run.status}`);
          } catch (cause) { setError(cause instanceof Error ? cause.message : "Workflow test failed"); }
        }}><Play className="size-3.5" />Test workflow</Button></div>
        <WorkflowBuilder value={workflow} onChange={setWorkflow} onTestStep={(_, value) => setSample(value)} />
        {result && <p className="mt-4 rounded-lg bg-emerald-500/10 p-3 text-sm text-emerald-700">{result}</p>}
      </section>
    </div>
  </div>;
}
