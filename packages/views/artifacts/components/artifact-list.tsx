"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import {
  artifactsByIssueOptions,
  artifactsByProjectOptions,
} from "@multica/core/artifacts/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { cn } from "@multica/ui/lib/utils";
import type { Artifact } from "@multica/core/types";
import { ArtifactCard } from "./artifact-card";
import { ArtifactSheet } from "./artifact-sheet";

type Scope =
  | { issueId: string; projectId?: undefined }
  | { issueId?: undefined; projectId: string };

export type ArtifactListProps = Scope & {
  className?: string;
  emptyState?: React.ReactNode;
  heading?: React.ReactNode;
};

/**
 * Renders a list of artifacts for an issue or a project. Clicking a card opens
 * the artifact in a side sheet for view/edit. Hidden when there are no
 * artifacts and no `emptyState` is provided.
 */
export function ArtifactList(props: ArtifactListProps) {
  const wsId = useWorkspaceId();
  const issueId = props.issueId;
  const projectId = props.projectId;

  const issueQuery = useQuery(artifactsByIssueOptions(wsId, issueId ?? ""));
  const projectQuery = useQuery(artifactsByProjectOptions(wsId, projectId ?? ""));
  const { data, isLoading } = issueId ? issueQuery : projectQuery;

  const [openId, setOpenId] = React.useState<string | null>(null);

  if (isLoading) return null;
  if (!data || data.length === 0) {
    return props.emptyState ? (
      <div className={cn("flex flex-col gap-2", props.className)}>
        {props.heading}
        {props.emptyState}
      </div>
    ) : null;
  }

  return (
    <div className={cn("flex flex-col gap-2", props.className)}>
      {props.heading}
      <div className="flex flex-col gap-2">
        {data.map((a: Artifact) => (
          <ArtifactCard key={a.id} artifact={a} onClick={() => setOpenId(a.id)} />
        ))}
      </div>
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
