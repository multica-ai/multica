"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { NotebookPen, Lock } from "lucide-react";
import {
  artifactsByIssueOptions,
  artifactsByProjectOptions,
  coupledNotesForIssueOptions,
  type CoupledNote,
} from "@multica/cerebro-artifacts/core";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
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
 *
 * FIR-1621: on an issue, also shows notes coupled to that issue (two-way
 * note↔issue link). A note is an artifact, so it opens in the same sheet.
 * Gated by `cerebro_note_agent_collab`.
 */
export function ArtifactList(props: ArtifactListProps) {
  const wsId = useWorkspaceId();
  const issueId = props.issueId;
  const projectId = props.projectId;
  const collabEnabled = useFeatureFlag("cerebro_note_agent_collab");

  const issueQuery = useQuery(artifactsByIssueOptions(wsId, issueId ?? ""));
  const projectQuery = useQuery(artifactsByProjectOptions(wsId, projectId ?? ""));
  const { data, isLoading } = issueId ? issueQuery : projectQuery;

  const coupledQuery = useQuery({
    ...coupledNotesForIssueOptions(wsId, issueId ?? ""),
    enabled: Boolean(wsId && issueId && collabEnabled),
  });
  const coupledNotes = collabEnabled ? (coupledQuery.data ?? []) : [];

  const [openId, setOpenId] = React.useState<string | null>(null);

  const artifacts = data ?? [];
  const hasArtifacts = artifacts.length > 0;
  const hasCoupled = coupledNotes.length > 0;

  if (isLoading) return null;
  if (!hasArtifacts && !hasCoupled) {
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
      {hasArtifacts && (
        <div className="flex flex-col gap-2">
          {artifacts.map((a: Artifact) => (
            <ArtifactCard key={a.id} artifact={a} onClick={() => setOpenId(a.id)} />
          ))}
        </div>
      )}
      {hasCoupled && (
        <div className="flex flex-col gap-1.5">
          <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
            <NotebookPen className="size-3.5" />
            Coupled notes
          </div>
          {coupledNotes.map((n: CoupledNote) => (
            <CoupledNoteCard key={n.id} note={n} onClick={() => setOpenId(n.id)} />
          ))}
        </div>
      )}
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

function noteExcerpt(body: string, maxLen = 160): string {
  const stripped = body
    .replace(/```[\s\S]*?```/g, " ")
    .replace(/[#*_`>~|-]+/g, " ")
    .replace(/\s+/g, " ")
    .trim();
  if (stripped.length <= maxLen) return stripped;
  return stripped.slice(0, maxLen).trimEnd() + "…";
}

function CoupledNoteCard({
  note,
  onClick,
}: {
  note: CoupledNote;
  onClick: () => void;
}) {
  const title = note.title.trim() || "Untitled note";
  const body = noteExcerpt(note.body);
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full flex-col gap-1 rounded-md border bg-card px-3 py-2 text-left transition-colors hover:bg-accent"
    >
      <div className="flex items-center gap-1.5">
        <NotebookPen className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate text-sm font-medium">{title}</span>
        {note.visibility === "private" && (
          <Lock className="size-3 shrink-0 text-muted-foreground" />
        )}
      </div>
      {body && (
        <span className="line-clamp-2 text-xs text-muted-foreground">{body}</span>
      )}
    </button>
  );
}
