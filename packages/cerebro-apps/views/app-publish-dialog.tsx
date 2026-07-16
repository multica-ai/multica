"use client";

import { useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";

export function AppPublishDialog({ defaultVersion, saving, onCancel, onPublish }: {
  defaultVersion: string;
  saving: boolean;
  onCancel: () => void;
  onPublish: (version: string, releaseNotes: string) => Promise<void>;
}) {
  const [version, setVersion] = useState(defaultVersion);
  const [releaseNotes, setReleaseNotes] = useState("");
  return <div role="dialog" aria-label="Publish app version" className="absolute inset-0 z-20 grid place-items-center bg-background/80 p-4 backdrop-blur-sm">
    <form className="w-full max-w-md space-y-4 rounded-xl border bg-card p-5 shadow-xl" onSubmit={(event) => { event.preventDefault(); void onPublish(version, releaseNotes); }}>
      <div><h2 className="font-semibold">Publish app</h2><p className="text-sm text-muted-foreground">Create an immutable version from the current files.</p></div>
      <Label className="grid gap-2"><span>Version</span><Input value={version} onChange={(event) => setVersion(event.target.value)} required /></Label>
      <Label className="grid gap-2"><span>Release notes</span><Textarea value={releaseNotes} onChange={(event) => setReleaseNotes(event.target.value)} required /></Label>
      <div className="flex justify-end gap-2"><Button type="button" variant="outline" onClick={onCancel}>Cancel</Button><Button type="submit" disabled={saving || releaseNotes.trim() === ""}>{saving ? "Publishing…" : "Publish version"}</Button></div>
    </form>
  </div>;
}
