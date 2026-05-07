"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, FileQuestion } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { artifactSearchOptions } from "@multica/cerebro-artifacts/core";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ListArtifactsParams } from "@multica/core/types";
import { ArtifactCard } from "./artifact-card";
import { ArtifactSheet } from "./artifact-sheet";
import { CreateArtifactSheet } from "./create-artifact-sheet";
import {
  ArtifactFilters,
  type ArtifactFilterState,
} from "./artifact-filters";

/**
 * Workspace-level Documents page. Cross-scope library spanning every project
 * and issue, plus workspace-scoped artifacts. Server-side filter via
 * /api/artifacts; new artifacts default to workspace scope.
 */
export function WorkspaceDocuments() {
  const wsId = useWorkspaceId();

  const [filters, setFilters] = React.useState<ArtifactFilterState>({
    q: "",
  });
  const [creating, setCreating] = React.useState(false);
  const [openId, setOpenId] = React.useState<string | null>(null);

  const params: ListArtifactsParams = React.useMemo(
    () => ({
      kind: filters.kind,
      scope: filters.scope,
      q: filters.q.trim() || undefined,
    }),
    [filters],
  );

  const { data: artifacts = [], isLoading } = useQuery(
    artifactSearchOptions(wsId, params),
  );

  return (
    <div className="mx-auto w-full max-w-5xl px-3 sm:px-6 md:px-8 py-4 sm:py-6">
      <div className="mb-4 flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Documents</h1>
          <p className="mt-0.5 text-sm text-muted-foreground">
            All artifacts across this workspace — reports, plans, decisions,
            diagrams, and notes.
          </p>
        </div>
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="mr-1 size-4" /> New artifact
        </Button>
      </div>

      <ArtifactFilters
        value={filters}
        onChange={setFilters}
        showScope
        className="mb-4"
      />

      {isLoading ? (
        <div className="text-sm text-muted-foreground">Loading…</div>
      ) : artifacts.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-lg border border-dashed border-border bg-card/40 px-6 py-12 text-center">
          <FileQuestion className="size-8 text-muted-foreground" />
          <div>
            <p className="text-sm font-medium">
              {filters.q || filters.kind || filters.scope
                ? "No artifacts match these filters"
                : "No artifacts yet"}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {filters.q || filters.kind || filters.scope
                ? "Try clearing some filters or searching for something else."
                : "Agents create artifacts via MCP. Humans can create them here too."}
            </p>
          </div>
          {!(filters.q || filters.kind || filters.scope) && (
            <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
              <Plus className="mr-1 size-4" /> Create one
            </Button>
          )}
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          {artifacts.map((a) => (
            <ArtifactCard
              key={a.id}
              artifact={a}
              showScope
              onClick={() => setOpenId(a.id)}
            />
          ))}
        </div>
      )}

      <CreateArtifactSheet
        workspaceScoped
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
