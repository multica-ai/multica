"use client";

import { useEffect, useMemo, useState } from "react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { createAppTemplate, type AppSourceFile } from "../core/app-template";
import { getAppDetail, publishAppVersion } from "../core/api";
import { validateAppManifest } from "../core/schema";
import { AppPublishDialog } from "./app-publish-dialog";

export function AppEditorPage({ appId }: { appId: string }) {
  const enabled = useFeatureFlag("cerebro_mini_apps");
  const workspace = useCurrentWorkspace();
  const [files, setFiles] = useState<AppSourceFile[]>([]);
  const [activePath, setActivePath] = useState("app.json");
  const [previewing, setPreviewing] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!enabled || !workspace) return;
    void getAppDetail(appId, workspace.slug)
      .then((app) => setFiles(createAppTemplate(app.name, "0.1.0")))
      .catch((cause) => setError(cause instanceof Error ? cause.message : "Could not load app"));
  }, [appId, enabled, workspace]);

  const validationError = useMemo(() => validatePackage(files), [files]);
  const active = files.find((file) => file.path === activePath);
  const previewHTML = files.find((file) => file.path === "frontend/index.html")?.content ?? "";
  if (!enabled) return null;
  if (!workspace) return <div className="grid h-full place-items-center text-sm text-muted-foreground">Loading workspace context…</div>;
  if (error) return <div role="alert" className="p-6 text-sm text-destructive">{error}</div>;
  if (files.length === 0) return <div className="grid h-full place-items-center text-sm text-muted-foreground">Loading app files…</div>;

  return <div className="relative flex h-full min-h-0 flex-col bg-background">
    <div className="flex flex-wrap items-center gap-2 border-b p-3">
      <label className="inline-flex h-8 cursor-pointer items-center rounded-lg border px-3 text-sm">Import files<input aria-label="Import files" className="sr-only" type="file" multiple onChange={(event) => { void importFiles(event.currentTarget.files).then((next) => { if (next.length > 0) { setFiles(next); setActivePath(next[0]?.path ?? "app.json"); } }); }} /></label>
      <Button type="button" variant="outline" onClick={() => exportPackage(files)}>Export package</Button>
      <Button type="button" variant="outline" onClick={() => setPreviewing((value) => !value)}>{previewing ? "Edit files" : "Preview"}</Button>
      <button type="button" className={buttonVariants({ className: "ml-auto" })} disabled={validationError !== ""} onClick={() => setPublishing(true)}>Publish</button>
    </div>
    {validationError && <p role="alert" className="border-b px-4 py-2 text-sm text-destructive">{validationError}</p>}
    {previewing ? <iframe title="App preview" sandbox="allow-scripts" className="h-full w-full border-0" srcDoc={previewHTML} /> : <div className="flex min-h-0 flex-1 flex-col sm:flex-row">
      <div role="tablist" aria-label="App files" className="flex shrink-0 gap-1 overflow-x-auto border-b p-2 sm:w-56 sm:flex-col sm:border-b-0 sm:border-r">
        {files.map((file) => <button key={file.path} role="tab" aria-selected={file.path === activePath} className="rounded-md px-2 py-1.5 text-left text-xs hover:bg-muted aria-selected:bg-muted" onClick={() => setActivePath(file.path)}>{file.path}</button>)}
      </div>
      {active && <Textarea aria-label={`Source for ${active.path}`} className="min-h-0 flex-1 resize-none rounded-none border-0 font-mono text-xs focus-visible:ring-0" value={active.content} onChange={(event) => setFiles((current) => current.map((file) => file.path === active.path ? { ...file, content: event.target.value } : file))} />}
    </div>}
    {publishing && <AppPublishDialog defaultVersion="0.1.0" saving={saving} onCancel={() => setPublishing(false)} onPublish={async (version, releaseNotes) => {
      setSaving(true); setError("");
      try { await publishAppVersion(appId, { version, release_notes: releaseNotes, files }, workspace.slug); setPublishing(false); }
      catch (cause) { setError(cause instanceof Error ? cause.message : "Could not publish app"); }
      finally { setSaving(false); }
    }} />}
  </div>;
}

function validatePackage(files: AppSourceFile[]): string {
  const appFile = files.find((file) => file.path === "app.json");
  if (!appFile) return "app.json is required";
  let parsed: { manifest?: unknown };
  try { parsed = JSON.parse(appFile.content) as { manifest?: unknown }; }
  catch { return "app.json must contain valid JSON"; }
  const errors = validateAppManifest(parsed.manifest);
  if (errors.length > 0) return errors[0] ?? "app.json is invalid";
  const manifest = parsed.manifest as { frontend?: { entry?: string }; backend?: { entry?: string } };
  for (const entry of [manifest.frontend?.entry, manifest.backend?.entry].filter(Boolean)) if (!files.some((file) => file.path === entry)) return `Missing entrypoint ${entry}`;
  return "";
}

async function importFiles(list: FileList | null): Promise<AppSourceFile[]> {
  if (!list) return [];
  const files = await Promise.all(Array.from(list).slice(0, 100).map(async (file) => ({
    path: (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name,
    content: await file.text(),
    media_type: file.type || mediaTypeForPath(file.name),
  })));
  return files.filter((file) => file.path !== "" && !file.path.split("/").includes(".."));
}

function exportPackage(files: AppSourceFile[]) {
  const blob = new Blob([JSON.stringify({ files }, null, 2)], { type: "application/json" });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url; anchor.download = "multica-mini-app.json"; anchor.click();
  URL.revokeObjectURL(url);
}

function mediaTypeForPath(path: string): string {
  if (path.endsWith(".json")) return "application/json";
  if (path.endsWith(".html")) return "text/html; charset=utf-8";
  if (path.endsWith(".js") || path.endsWith(".mjs")) return "text/javascript; charset=utf-8";
  return "text/plain; charset=utf-8";
}
