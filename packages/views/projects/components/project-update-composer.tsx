"use client";

import { useRef, useState } from "react";
import type { ProjectHealth } from "@multica/core/types/project";
import { useCreateProjectUpdate } from "@multica/core/projects";
import { ContentEditor, type ContentEditorRef } from "../../editor";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";

interface ProjectUpdateComposerProps {
  wsId: string;
  projectId: string;
}

const HEALTH_OPTIONS: { value: ProjectHealth; label: string; dot: string }[] = [
  { value: "on_track", label: "On track", dot: "bg-emerald-500" },
  { value: "at_risk", label: "At risk", dot: "bg-amber-500" },
  { value: "off_track", label: "Off track", dot: "bg-red-500" },
];

export function ProjectUpdateComposer({ wsId, projectId }: ProjectUpdateComposerProps) {
  const [health, setHealth] = useState<ProjectHealth>("on_track");
  const [hasBody, setHasBody] = useState(false);
  const [resetKey, setResetKey] = useState(0);
  const editorRef = useRef<ContentEditorRef>(null);
  const createUpdate = useCreateProjectUpdate(wsId, projectId);

  const submit = () => {
    if (createUpdate.isPending) return;
    // Read the authoritative value straight from the editor — the debounced
    // onUpdate may not have flushed the final keystrokes yet.
    const body = (editorRef.current?.getMarkdown() ?? "").trim();
    if (!body) return;
    createUpdate.mutate(
      { health, body },
      {
        onSuccess: () => {
          setHealth("on_track");
          setHasBody(false);
          setResetKey((k) => k + 1);
        },
      },
    );
  };

  return (
    <div className="rounded-lg border border-border bg-card p-4">
      <div className="flex items-center gap-2">
        {HEALTH_OPTIONS.map((opt) => (
          <button
            key={opt.value}
            type="button"
            onClick={() => setHealth(opt.value)}
            className={cn(
              "inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs",
              health === opt.value ? "border-foreground" : "border-border text-muted-foreground",
            )}
          >
            <span className={cn("h-2 w-2 rounded-full", opt.dot)} />
            {opt.label}
          </button>
        ))}
      </div>
      <div className="mt-3">
        <ContentEditor
          key={`update-composer-${resetKey}`}
          ref={editorRef}
          defaultValue=""
          placeholder="Write a project update…"
          onUpdate={(markdown) => setHasBody(markdown.trim().length > 0)}
        />
      </div>
      <div className="mt-3 flex justify-end">
        <Button size="sm" onClick={submit} disabled={createUpdate.isPending || !hasBody}>
          {createUpdate.isPending ? "Posting…" : "Post update"}
        </Button>
      </div>
    </div>
  );
}
