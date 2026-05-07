"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, FileQuestion } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { artifactsByProjectOptions } from "@multica/cerebro-artifacts/core";
import { useWorkspaceId } from "@multica/core/hooks";
import { ArtifactCard } from "./artifact-card";
import { ArtifactSheet } from "./artifact-sheet";
import { CreateArtifactSheet } from "./create-artifact-sheet";
import {
  ArtifactFilters,
  applyArtifactFilter,
  type ArtifactFilterState,
} from "./artifact-filters";

/**
 * Documents tab content for a project. Shows project-scoped artifacts as
 * a list of rich cards. Includes a "+ New artifact" button that opens a
 * create sheet.
 */
export function ProjectDocuments({ projectId }: { projectId: string }) {
  const wsId = useWorkspaceId();
  const { data: artifacts = [], isLoading } = useQuery(
    artifactsByProjectOptions(wsId, projectId),
  );
  const [creating, setCreating] = React.useState(false);
  const [openId, setOpenId] = React.useState<string | null>(null);
  const [filters, setFilters] = React.useState<ArtifactFilterState>({ q: "" });

  const filtered = React.useMemo(
    () => applyArtifactFilter(artifacts, filters),
    [artifacts, filters],
  );

  return (
    <div className="mx-auto w-full max-w-4xl px-3 sm:px-6 md:px-8 py-4 sm:py-6">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-base font-semibold">Documents</h2>
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="mr-1 size-4" /> New artifact
        </Button>
      </div>

      {artifacts.length > 0 && (
        <ArtifactFilters
          value={filters}
          onChange={setFilters}
          className="mb-4"
        />
      )}

      {isLoading ? (
        <div className="text-sm text-muted-foreground">Loading…</div>
      ) : artifacts.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border bg-card/40 px-6 py-10 text-center">
          <FileQuestion className="size-8 text-muted-foreground" />
          <div>
            <p className="text-sm font-medium">No documents yet</p>
            <p className="mt-1 text-xs text-muted-foreground">
              Reports, plans, decisions, diagrams, and notes scoped to this
              project show up here. Agents can create them via MCP, and so can
              you.
            </p>
          </div>
          <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
            <Plus className="mr-1 size-4" /> Create one
          </Button>
        </div>
      ) : filtered.length === 0 ? (
        <div className="rounded-lg border border-dashed border-border bg-card/40 px-4 py-8 text-center text-sm text-muted-foreground">
          No artifacts match these filters.
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {filtered.map((a) => (
            <ArtifactCard
              key={a.id}
              artifact={a}
              onClick={() => setOpenId(a.id)}
            />
          ))}
        </div>
      )}

      <CreateArtifactSheet
        projectId={projectId}
        open={creating}
        onOpenChange={setCreating}
      />

      {openId && (
        <ArtifactSheet
          artifactId={openId}
          open={Boolean(openId)}
          onOpenChange={(open) => !open && setOpenId(null)}
        />
      )}
    </div>
  );
}
