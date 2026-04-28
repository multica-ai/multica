"use client";

import * as React from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Pencil, Trash2, Download } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { artifactDetailOptions } from "@multica/core/artifacts/queries";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useDeleteArtifact } from "@multica/core/artifacts/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { useActorName } from "@multica/core/workspace/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { ArtifactBody } from "../components/artifact-body";
import { KindIcon, KIND_LABELS } from "../components/kind-icon";
import { MoveScopeMenu } from "../components/move-scope-menu";

function formatDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/**
 * The "people + things" line under the title — author and the issues / project
 * the document is connected to. Each is clickable: agent goes to the agent
 * profile (placeholder for now via /agents), members are non-link, issues and
 * projects deep-link.
 */
function ConnectionRow({ artifact }: { artifact: import("@multica/core/types").Artifact }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const { getActorName } = useActorName();
  // Resolve the issue title / identifier when the doc has any issue link.
  const linkedIssueId = artifact.issue_id ?? artifact.origin_issue_id;
  const { data: linkedIssue } = useQuery({
    ...issueDetailOptions(wsId, linkedIssueId ?? ""),
    enabled: Boolean(wsId && linkedIssueId),
  });
  const showOriginSeparately =
    artifact.origin_issue_id && artifact.origin_issue_id !== artifact.issue_id;

  return (
    <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
      <span className="flex items-center gap-1.5">
        <ActorAvatar
          actorType={artifact.author_type}
          actorId={artifact.author_id}
          size={18}
        />
        <span className="font-medium">
          {getActorName(artifact.author_type, artifact.author_id)}
        </span>
        {artifact.author_type === "agent" && (
          <Badge variant="outline" className="text-[10px]">
            agent
          </Badge>
        )}
      </span>
      {artifact.issue_id && (
        <span className="flex items-center gap-1 text-muted-foreground">
          on{" "}
          <a
            href={wsPaths.issueDetail(artifact.issue_id)}
            className="underline hover:text-foreground"
          >
            {linkedIssue?.identifier ?? "issue"}
            {linkedIssue?.title ? ` — ${linkedIssue.title}` : ""}
          </a>
        </span>
      )}
      {artifact.project_id && (
        <span className="flex items-center gap-1 text-muted-foreground">
          on{" "}
          <a
            href={wsPaths.projectDetail(artifact.project_id)}
            className="underline hover:text-foreground"
          >
            project
          </a>
        </span>
      )}
      {showOriginSeparately && artifact.origin_issue_id && (
        <span className="flex items-center gap-1 text-muted-foreground">
          from{" "}
          <a
            href={wsPaths.issueDetail(artifact.origin_issue_id)}
            className="underline hover:text-foreground"
          >
            {linkedIssue?.identifier ?? "issue"}
          </a>
        </span>
      )}
    </div>
  );
}

export function DocumentViewPage({ artifactId }: { artifactId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const router = useNavigation();
  const { data: artifact, isLoading, isError } = useQuery(
    artifactDetailOptions(wsId, artifactId),
  );
  const userId = useAuthStore((s) => s.user?.id);
  const remove = useDeleteArtifact();
  const [confirmDelete, setConfirmDelete] = React.useState(false);

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }
  if (isError || !artifact) {
    return (
      <div className="mx-auto w-full max-w-3xl px-8 py-6">
        <Button variant="ghost" size="sm" onClick={() => router.push(wsPaths.documents())}>
          <ArrowLeft className="mr-1 size-4" /> Documents
        </Button>
        <p className="mt-4 text-sm text-muted-foreground">
          This document is not available.
        </p>
      </div>
    );
  }

  const canEdit =
    artifact.author_type === "member" && artifact.author_id === userId;

  const handleDelete = async () => {
    await remove.mutateAsync(artifact);
    setConfirmDelete(false);
    router.push(wsPaths.documents());
  };

  return (
    <div className="mx-auto w-full max-w-3xl px-8 py-6">
      <div className="mb-3 flex items-center justify-between">
        <Button variant="ghost" size="sm" onClick={() => router.push(wsPaths.documents())}>
          <ArrowLeft className="mr-1 size-4" /> Documents
        </Button>
        <div className="flex items-center gap-1">
          {canEdit && (
            <>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => router.push(wsPaths.documentEdit(artifact.id))}
              >
                <Pencil className="mr-1 size-4" /> Edit
              </Button>
              <MoveScopeMenu artifact={artifact} />
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setConfirmDelete(true)}
                className="text-destructive hover:text-destructive"
              >
                <Trash2 className="mr-1 size-4" /> Delete
              </Button>
            </>
          )}
        </div>
      </div>

      <div className="mb-4 flex items-center gap-2">
        <KindIcon kind={artifact.kind} className="size-4 text-muted-foreground" />
        <Badge variant="secondary" className="text-xs font-normal">
          {KIND_LABELS[artifact.kind]}
        </Badge>
        <Badge variant="outline" className="text-xs uppercase">
          {artifact.format}
        </Badge>
        {artifact.author_type === "agent" && (
          <Badge variant="outline" className="text-xs">
            agent
          </Badge>
        )}
      </div>

      <h1 className="text-2xl font-semibold leading-tight">{artifact.title}</h1>
      <ConnectionRow artifact={artifact} />
      <p className="mt-1 text-xs text-muted-foreground">
        Updated {formatDateTime(artifact.updated_at)}
      </p>

      <div className="mt-6">
        {artifact.format === "pdf" && artifact.file_url ? (
          <div className="flex flex-col gap-2">
            <div className="flex items-center justify-between rounded border border-border bg-card/40 px-3 py-2 text-xs text-muted-foreground">
              <span>
                PDF
                {artifact.file_size_bytes
                  ? ` · ${formatBytes(artifact.file_size_bytes)}`
                  : ""}
              </span>
              <a
                href={artifact.file_url}
                download
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center gap-1 rounded px-2 py-1 text-xs hover:bg-accent"
              >
                <Download className="size-3.5" /> Download
              </a>
            </div>
            <iframe
              src={artifact.file_url}
              title={artifact.title}
              className="h-[80vh] w-full rounded border border-border bg-white"
            />
          </div>
        ) : artifact.format === "html" ? (
          <div
            className="prose prose-sm max-w-none dark:prose-invert"
            // PDFs come from our storage; HTML body is authored in-app, but
            // since agents can write artifacts we still scope to the prose
            // container. Document this as a known trust assumption.
            dangerouslySetInnerHTML={{ __html: artifact.body }}
          />
        ) : artifact.body ? (
          <div className="prose prose-sm max-w-none dark:prose-invert">
            <ArtifactBody body={artifact.body} />
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No content.</p>
        )}
      </div>

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete document?</AlertDialogTitle>
            <AlertDialogDescription>
              This permanently removes &ldquo;{artifact.title}&rdquo;. Cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} disabled={remove.isPending}>
              {remove.isPending ? "Deleting…" : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
