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
import {
  useDeleteArtifact,
  useUpdateArtifact,
} from "@multica/core/artifacts/mutations";
import { Input } from "@multica/ui/components/ui/input";
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

function normalizeTitle(s: string): string {
  return s.replace(/\s+/g, " ").trim().toLowerCase();
}

// Agents sometimes save HTML artifacts as a full <!DOCTYPE html> document
// (with their own <style>), and inlining that via dangerouslySetInnerHTML
// leaks the body styles into the host app — that's the "looks weird" we
// see in JEH-285. Detect full-document bodies and isolate them in an iframe.
function isFullHtmlDocument(body: string): boolean {
  const head = body.slice(0, 256).toLowerCase();
  return head.includes("<!doctype html") || /<html[\s>]/.test(head);
}

// Authors (humans and agents) often repeat the document title as a leading
// H1 inside the body. Strip it so the page H1 isn't shown twice. Only
// applies to inline-rendered bodies (markdown / HTML snippets); full HTML
// documents render in an iframe so a duplicate H1 there is benign.
function stripLeadingTitleHeading(body: string, title: string, format: string): string {
  if (!body || !title) return body;
  const want = normalizeTitle(title);
  if (format === "html") {
    const m = body.match(/^\s*<h1[^>]*>([\s\S]*?)<\/h1>\s*/i);
    if (m && normalizeTitle((m[1] ?? "").replace(/<[^>]+>/g, "")) === want) {
      return body.slice(m[0].length);
    }
    return body;
  }
  // markdown (and md-like default)
  const m = body.match(/^\s*#\s+([^\n]+)\n+/);
  if (m && normalizeTitle(m[1] ?? "") === want) {
    return body.slice(m[0].length);
  }
  return body;
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
  const { getActorName, getMemberName } = useActorName();
  // Resolve the issue title / identifier when the doc has any issue link.
  const linkedIssueId = artifact.issue_id ?? artifact.origin_issue_id;
  const { data: linkedIssue } = useQuery({
    ...issueDetailOptions(wsId, linkedIssueId ?? ""),
    enabled: Boolean(wsId && linkedIssueId),
  });
  const showOriginSeparately =
    artifact.origin_issue_id && artifact.origin_issue_id !== artifact.issue_id;
  const showRequesterSeparately =
    artifact.requester_user_id &&
    !(
      artifact.author_type === "member" &&
      artifact.author_id === artifact.requester_user_id
    );

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
      {showRequesterSeparately && artifact.requester_user_id && (
        <span className="flex items-center gap-1.5 text-muted-foreground">
          for{" "}
          <ActorAvatar
            actorType="member"
            actorId={artifact.requester_user_id}
            size={16}
          />
          <span className="font-medium text-foreground">
            {getMemberName(artifact.requester_user_id)}
          </span>
        </span>
      )}
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
  const update = useUpdateArtifact();
  const [confirmDelete, setConfirmDelete] = React.useState(false);
  const [renaming, setRenaming] = React.useState(false);
  const [titleDraft, setTitleDraft] = React.useState("");

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    );
  }
  if (isError || !artifact) {
    return (
      <div className="mx-auto w-full max-w-3xl px-4 py-4 md:px-8 md:py-6">
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

  const startRename = () => {
    setTitleDraft(artifact.title);
    setRenaming(true);
  };
  const commitRename = async () => {
    const next = titleDraft.trim();
    if (!next || next === artifact.title) {
      setRenaming(false);
      return;
    }
    await update.mutateAsync({ id: artifact.id, data: { title: next } });
    setRenaming(false);
  };
  const cancelRename = () => {
    setRenaming(false);
    setTitleDraft(artifact.title);
  };

  const renderedBody = stripLeadingTitleHeading(
    artifact.body ?? "",
    artifact.title,
    artifact.format,
  );

  return (
    <div className="h-full overflow-y-auto">
    <div className="mx-auto w-full max-w-3xl px-4 py-4 md:px-8 md:py-6">
      <div className="mb-3 flex items-center justify-between">
        <Button variant="ghost" size="sm" onClick={() => router.push(wsPaths.documents())}>
          <ArrowLeft className="mr-1 size-4" /> Documents
        </Button>
        <div className="flex items-center gap-1">
          {canEdit && (
            <>
              <Button variant="ghost" size="sm" onClick={startRename}>
                <Pencil className="mr-1 size-4" /> Rename
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => router.push(wsPaths.documentEdit(artifact.id))}
              >
                <Pencil className="mr-1 size-4" /> Edit body
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

      {renaming ? (
        <Input
          autoFocus
          value={titleDraft}
          onChange={(e) => setTitleDraft(e.target.value)}
          onBlur={commitRename}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              commitRename();
            } else if (e.key === "Escape") {
              e.preventDefault();
              cancelRename();
            }
          }}
          className="text-2xl font-semibold leading-tight h-auto py-1"
        />
      ) : (
        <h1
          className={
            "text-2xl font-semibold leading-tight" +
            (canEdit ? " cursor-text hover:bg-accent/30 rounded px-1 -mx-1" : "")
          }
          onClick={canEdit ? startRename : undefined}
          title={canEdit ? "Click to rename" : undefined}
        >
          {artifact.title}
        </h1>
      )}
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
          isFullHtmlDocument(renderedBody) ? (
            <iframe
              srcDoc={renderedBody}
              title={artifact.title}
              sandbox="allow-scripts allow-same-origin allow-popups"
              className="h-[80vh] w-full rounded border border-border bg-white"
            />
          ) : (
            <div
              className="prose prose-sm max-w-none dark:prose-invert"
              // HTML snippets are authored in-app and rendered inline; agents
              // can write artifacts so this remains a known trust assumption.
              dangerouslySetInnerHTML={{ __html: renderedBody }}
            />
          )
        ) : renderedBody ? (
          <div className="prose prose-sm max-w-none dark:prose-invert">
            <ArtifactBody body={renderedBody} />
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
    </div>
  );
}
