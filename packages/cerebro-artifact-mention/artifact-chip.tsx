"use client";

import { useQuery, queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { Artifact, ArtifactFolder } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "@multica/views/navigation";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { FileText, Folder } from "lucide-react";

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
 * FIR-2697 part 4: an agent-authored document that is attached to a chat/issue
 * always has a folder (the server files a folder-less one at attach time), and
 * the card shows + links to that folder. The folder segment is gated by
 * `cerebro_attachment_folder`; it only appears once the artifact actually has a
 * folder, so nothing shows for legacy folder-less references.
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

// The artifact response carries only `folder_id`, not the folder name, so we
// resolve the name from the (small, cached) folder list for that surface. The
// query is shared across every chip pointing at the same surface.
function folderListOptions(wsId: string, surface: string, enabled: boolean) {
  return queryOptions({
    queryKey: ["artifact-folders", wsId, surface],
    queryFn: () => api.listArtifactFolders(surface),
    enabled: Boolean(enabled && wsId && surface),
    staleTime: 60_000,
  });
}

const CARD_CLASS =
  "artifact-mention not-prose inline-flex max-w-96 items-center gap-1 rounded-md border " +
  "border-border bg-card mx-0.5 px-2 py-0.5 align-middle text-xs no-underline";

const LINK_CLASS =
  "inline-flex min-w-0 items-center gap-1.5 no-underline hover:underline";

export interface ArtifactMentionChipProps {
  artifactId: string;
  /** Shown when the artifact can't be resolved (deleted, permissions, …). */
  fallbackLabel?: string;
  className?: string;
}

// A note artifact lives in the note folder tree; every other kind lives in the
// document tree. Mirrors the server-side surface split (artifact_folder.kind).
function surfaceForKind(kind: string): "document" | "note" {
  return kind === "note" ? "note" : "document";
}

export function ArtifactMentionChip({
  artifactId,
  fallbackLabel,
  className,
}: ArtifactMentionChipProps) {
  const enabled = useFeatureFlag("cerebro_artifact_references");
  const folderEnabled = useFeatureFlag("cerebro_attachment_folder");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { openInNewTab } = useNavigation();

  const { data: artifact } = useQuery(
    artifactDetailOptions(wsId, artifactId, enabled),
  );

  const surface = artifact ? surfaceForKind(artifact.kind) : "document";
  // Only fetch folders when we have a resolved artifact that actually has one
  // and the part-4 flag is on — otherwise there is nothing to show.
  const foldersEnabled = Boolean(
    enabled && folderEnabled && artifact?.folder_id,
  );
  const { data: folders } = useQuery(
    folderListOptions(wsId, surface, foldersEnabled),
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

  const folder: ArtifactFolder | undefined = artifact.folder_id
    ? folders?.find((f) => f.id === artifact.folder_id)
    : undefined;
  const folderPath =
    folder &&
    (surface === "note"
      ? paths.notesFolder(folder.id)
      : paths.documentsFolder(folder.id));

  // FIR-1782: a document/note card always opens in a NEW tab/window so the user
  // keeps their current context (the issue or comment they clicked from). The
  // folder link follows the same rule — opening the folder view never navigates
  // the user away from where they clicked.
  const openPath = (path: string, title: string) => (e: React.MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    if (openInNewTab) {
      openInNewTab(path, title);
    } else {
      window.open(path, "_blank", "noopener,noreferrer");
    }
  };

  return (
    <span className={`${cls} cursor-default`} title={label}>
      <a
        href={docPath}
        onClick={openPath(docPath, label)}
        target="_blank"
        rel="noopener noreferrer"
        title={label}
        className={`${LINK_CLASS} cursor-pointer text-foreground`}
      >
        <FileText className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
        <span className="truncate">{label}</span>
      </a>
      {folder && folderPath && (
        <>
          <span aria-hidden className="shrink-0 text-muted-foreground/50">
            /
          </span>
          <a
            href={folderPath}
            onClick={openPath(folderPath, folder.name)}
            target="_blank"
            rel="noopener noreferrer"
            title={`In folder: ${folder.name}`}
            className={`${LINK_CLASS} cursor-pointer text-muted-foreground hover:text-foreground`}
          >
            <Folder className="h-3.5 w-3.5 shrink-0" />
            <span className="truncate">{folder.name}</span>
          </a>
        </>
      )}
    </span>
  );
}

export type { Artifact };
