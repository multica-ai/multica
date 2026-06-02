"use client";

import { useState } from "react";
import type { ProjectHealth } from "@multica/core/types/project";
import { useCreateProjectUpdate } from "@multica/core/projects";
import { ContentEditor } from "../../editor";
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
  const [body, setBody] = useState("");
  const [resetKey, setResetKey] = useState(0);
  const createUpdate = useCreateProjectUpdate(wsId, projectId);

  const submit = () => {
    if (createUpdate.isPending) return;
    createUpdate.mutate(
      { health, body },
      {
        onSuccess: () => {
          setBody("");
          setHealth("on_track");
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
          defaultValue=""
          placeholder="Write a project update…"
          onUpdate={(markdown) => setBody(markdown)}
        />
      </div>
      <div className="mt-3 flex justify-end">
        <Button size="sm" onClick={submit} disabled={createUpdate.isPending}>
          {createUpdate.isPending ? "Posting…" : "Post update"}
        </Button>
      </div>
    </div>
  );
}
