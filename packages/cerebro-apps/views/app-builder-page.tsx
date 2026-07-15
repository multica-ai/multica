"use client";

import { useState, type ReactNode } from "react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentWorkspace } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { PageHeader } from "@multica/views/layout/page-header";
import { useNavigation } from "@multica/views/navigation";
import { createApp } from "../core/api";

export function AppBuilderPage() {
  const enabled = useFeatureFlag("cerebro_mini_apps");
  const workspace = useCurrentWorkspace();
  const navigation = useNavigation();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [folder, setFolder] = useState("Workspace apps");
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  if (!enabled) return null;
  if (!workspace) return <div className="grid h-full place-items-center text-sm text-muted-foreground">Loading workspace context…</div>;

  return <div className="flex h-full flex-col">
    <PageHeader><div><h1 className="text-sm font-semibold">Build app</h1><p className="text-[11px] text-muted-foreground">Create the app shell, then add versions and workflows through the shared API or CLI.</p></div></PageHeader>
    <div className="flex-1 overflow-y-auto p-4 sm:p-6">
      <form className="mx-auto max-w-xl space-y-5 rounded-xl border bg-card p-5" onSubmit={async (event) => {
        event.preventDefault(); setError(""); setSaving(true);
        try {
          const app = await createApp({ name: name.trim(), slug: slugify(name), description: description.trim(), folder: folder.trim() }, workspace.slug);
          navigation.push(`/${workspace.slug}/apps/${app.id}`);
        } catch (cause) { setError(cause instanceof Error ? cause.message : "Could not create app"); }
        finally { setSaving(false); }
      }}>
        <Field label="Name"><Input value={name} onChange={(event) => setName(event.target.value)} required /></Field>
        <Field label="Description"><Textarea value={description} onChange={(event) => setDescription(event.target.value)} required /></Field>
        <Field label="Folder"><Input value={folder} onChange={(event) => setFolder(event.target.value)} required /></Field>
        {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        <div className="flex justify-end gap-2"><Button type="button" variant="outline" onClick={() => navigation.push(`/${workspace.slug}/apps`)}>Cancel</Button><Button type="submit" disabled={saving}>{saving ? "Creating…" : "Create app"}</Button></div>
      </form>
    </div>
  </div>;
}

function Field({ label, children }: { label: string; children: ReactNode }) { return <Label className="grid gap-2"><span>{label}</span>{children}</Label>; }
function slugify(value: string) { return value.trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, ""); }
