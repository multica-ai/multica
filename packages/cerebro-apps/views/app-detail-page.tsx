"use client";

import { useEffect, useState } from "react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import type { AppDetail } from "../core";
import { getAppDetail } from "../core/api";
import { AppViewFrame } from "./app-view-frame";

export function AppDetailPage({ appId, runtimeBaseUrl }: { appId: string; runtimeBaseUrl: string }) {
  const enabled = useFeatureFlag("cerebro_mini_apps");
  const workspace = useCurrentWorkspace();
  const [app, setApp] = useState<AppDetail>();
  const [error, setError] = useState("");
  useEffect(() => { if (enabled && workspace) getAppDetail(appId, workspace.slug).then(setApp).catch((cause) => setError(cause instanceof Error ? cause.message : "Could not load app")); }, [appId, enabled, workspace]);
  if (!enabled) return null;
  if (error) return <div role="alert" className="p-6 text-sm text-destructive">{error}</div>;
  if (!workspace) return <div className="grid h-full place-items-center text-sm text-muted-foreground">Loading workspace context…</div>;
  if (!app) return <div className="grid h-full place-items-center text-sm text-muted-foreground">Loading app…</div>;
  const version = app.current_version;

  return <div className="h-full min-h-0 overflow-hidden bg-background">
    <h1 className="sr-only">{app.name}</h1>
    {version ? <AppViewFrame appId={app.id} version={version} title={app.name} src={`${runtimeBaseUrl.replace(/\/$/, "")}/apps/${app.id}/${version}/index.html`} /> : <div className="grid h-full place-items-center text-sm text-muted-foreground">Publish the first version to open the app.</div>}
  </div>;
}
