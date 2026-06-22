"use client";

import { useQuery, queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { Artifact } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { FileText } from "lucide-react";

/**
 * ArtifactMentionChip — visual + interactive card for `mention://artifact/<id>`.
 *
 * FIR-1800: an agent's output is an artifact (document/note), not a raw file
 * upload. A reference to that artifact travels inside a comment / chat / DM /
 * channel body as a `mention://artifact/<id>` markdown link; the readonly and
 * markdown renderers delegate the token here. We resolve the id to the real
 * artifact and render a compact WHITE card (white = real document; the grey
 * rows are uploaded files). A note IS an artifact, so click opens the same
 * full-page note editor (`documentDetail`) the document cards use — the rule
 * established in FIR-1782 / PR #1700.
 *
 * Gated by `cerebro_artifact_references`. When the flag is off, or the artifact
 * cannot be resolved (deleted / no access / still loading), we fall back to a
 * neutral inline label so the reference never disappears silently.
 */

function artifactDetailOptions(wsId: string, id: string, enabled: boolean) {
  return queryOptions({
    queryKey: ["artifacts", wsId, "detail", id],
    queryFn: () => api.getArtifact(id),
    enabled: Boolean(enabled && wsId && id),
    staleTime: 60_000,
  });
}

const CARD_CLASS =
  "artifact-mention not-prose inline-flex max-w-72 items-center gap-1.5 rounded-md border " +
  "border-border bg-card mx-0.5 px-2 py-0.5 align-middle text-xs no-underline";

export interface ArtifactMentionChipProps {
  artifactId: string;
  /** Shown when the artifact can't be resolved (deleted, permissions, …). */
  fallbackLabel?: string;
  className?: string;
}

export function ArtifactMentionChip({
  artifactId,
  fallbackLabel,
  className,
}: ArtifactMentionChipProps) {
  const enabled = useFeatureFlag("cerebro_artifact_references");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push, openInNewTab } = useNavigation();

  const { data: artifact } = useQuery(
    artifactDetailOptions(wsId, artifactId, enabled),
  );

  const cls = className ? `${CARD_CLASS} ${className}` : CARD_CLASS;

  // Flag off → render the link text plainly, no card.
  if (!enabled) {
    return (
      <span className="mx-0.5 font-medium text-primary">
        {fallbackLabel ?? "document"}
      </span>
    );
  }

  // Loading / not resolvable → a neutral chip so nothing disappears.
  if (!artifact) {
    return (
      <span className={`${cls} text-muted-foreground`}>
        <FileText className="h-3.5 w-3.5 shrink-0" />
        <span className="truncate">{fallbackLabel ?? "document"}</span>
      </span>
    );
  }

  const docPath = paths.documentDetail(artifact.id);
  const label = artifact.title || fallbackLabel || "document";

  const handleClick = (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.metaKey || e.ctrlKey || e.shiftKey) {
      if (openInNewTab) {
        openInNewTab(docPath, label);
      } else {
        window.open(docPath, "_blank", "noopener,noreferrer");
      }
      return;
    }
    push(docPath);
  };

  return (
    <a
      href={docPath}
      onClick={handleClick}
      title={label}
      className={`${cls} cursor-pointer transition-colors hover:bg-accent`}
    >
      <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      <span className="truncate text-foreground">{label}</span>
    </a>
  );
}

export type { Artifact };
