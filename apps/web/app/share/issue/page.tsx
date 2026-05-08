"use client";

// PWA Web Share Target landing page.
//
// Flow:
//   1. The OS share-sheet POSTs to /share/issue. The service worker (app/sw.ts)
//      intercepts the POST, stashes form data + file blobs in IndexedDB under
//      a token, and 303-redirects here as a GET with `?draft=<token>`.
//   2. This page reads the draft from IDB, prompts for login if needed, then
//      lets the user pick a workspace + project, edit the title/description,
//      and submit via the regular createIssue endpoint.
//   3. Files are uploaded through the standard /api/upload-file endpoint and
//      passed in as `attachment_ids` so the resulting issue is indistinguishable
//      from one created from the in-app dialog.
//
// The route lives at the root of the app (not under [workspaceSlug]) because
// the OS dispatches the share intent without any workspace context — workspace
// gets resolved here, after auth.

import { Suspense, useEffect, useMemo, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Loader2, Paperclip, AlertTriangle } from "lucide-react";

import { useAuthStore } from "@multica/core/auth";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import { setCurrentWorkspace } from "@multica/core/platform";
import { paths } from "@multica/core/paths";
import { api } from "@multica/core/api";
import type { Workspace, Project } from "@multica/core/types";

import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { toast } from "sonner";

import {
  loadShareDraft,
  deleteShareDraft,
  type ShareDraft,
} from "@/app/cerebro-share-draft-idb";

const FALLBACK_TITLE_LIMIT = 80;

// Title isn't always sent by source apps — when the share contains only
// `text` (typical for "share this paragraph" actions), derive a short title
// from the first non-empty line. Falls back to the empty string so the user
// is forced to fill it in before submit.
function deriveTitle(draft: ShareDraft): string {
  if (draft.title.trim()) return draft.title.trim();
  const firstLine = draft.text.split(/\r?\n/).find((l) => l.trim());
  if (!firstLine) return "";
  return firstLine.length > FALLBACK_TITLE_LIMIT
    ? firstLine.slice(0, FALLBACK_TITLE_LIMIT - 1) + "…"
    : firstLine;
}

// When the source app sends both `text` and `url` (e.g. Safari "Share Page"),
// Multica's description should preserve both. The url goes on its own line
// at the bottom so it stays clickable in the rendered markdown without
// disrupting any prose the user wrote.
function deriveDescription(draft: ShareDraft): string {
  const text = draft.text.trim();
  const url = draft.url.trim();
  if (text && url && !text.includes(url)) return `${text}\n\n${url}`;
  return text || url;
}

export default function Page() {
  return (
    <Suspense fallback={<CenteredSpinner />}>
      <SharePageContent />
    </Suspense>
  );
}

function SharePageContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const draftToken = searchParams.get("draft");
  const oversized = searchParams.get("oversized");
  const errorCode = searchParams.get("error");

  const user = useAuthStore((s) => s.user);
  const isAuthLoading = useAuthStore((s) => s.isLoading);

  // Auth-gate the page. Preserves the draft token in `next=` so that login
  // bounces the user right back here with the same draft to consume.
  useEffect(() => {
    if (isAuthLoading) return;
    if (!user) {
      const next = draftToken
        ? `/share/issue?draft=${encodeURIComponent(draftToken)}`
        : "/share/issue";
      router.replace(`${paths.login()}?next=${encodeURIComponent(next)}`);
    }
  }, [user, isAuthLoading, draftToken, router]);

  const [draft, setDraft] = useState<ShareDraft | null>(null);
  const [draftLoadError, setDraftLoadError] = useState<string | null>(null);
  const [draftLoaded, setDraftLoaded] = useState(false);

  useEffect(() => {
    if (!draftToken) {
      setDraftLoaded(true);
      return;
    }
    let cancelled = false;
    loadShareDraft(draftToken)
      .then((result) => {
        if (cancelled) return;
        setDraft(result);
        setDraftLoaded(true);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setDraftLoadError(
          err instanceof Error ? err.message : "Failed to load shared content",
        );
        setDraftLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, [draftToken]);

  if (isAuthLoading || !user) {
    return <CenteredSpinner />;
  }

  if (errorCode === "save_failed") {
    return (
      <ErrorScreen
        title="Couldn't save the shared content"
        body="Something went wrong while storing the share offline. Try sharing again from the source app."
      />
    );
  }

  if (!draftToken) {
    return (
      <ErrorScreen
        title="No shared content"
        body="This page is the landing page for content shared into Multica from another app. Open it from your share-sheet to use it."
      />
    );
  }

  if (!draftLoaded) {
    return <CenteredSpinner />;
  }

  if (draftLoadError) {
    return <ErrorScreen title="Couldn't read shared content" body={draftLoadError} />;
  }

  if (!draft) {
    return (
      <ErrorScreen
        title="Shared content has expired"
        body="Shared drafts are kept for one hour. Share again from the source app to start over."
      />
    );
  }

  return (
    <ShareForm
      draft={draft}
      oversizedFilenames={oversized ? oversized.split("|").filter(Boolean) : []}
    />
  );
}

function ShareForm({
  draft,
  oversizedFilenames,
}: {
  draft: ShareDraft;
  oversizedFilenames: string[];
}) {
  const router = useRouter();
  const qc = useQueryClient();
  const { data: workspaces = [], isLoading: wsLoading } = useQuery(workspaceListOptions());

  const [workspaceId, setWorkspaceId] = useState<string | undefined>(undefined);
  // Auto-select the first workspace once the list loads; the user can swap
  // via the dropdown if they have more than one.
  useEffect(() => {
    if (workspaceId || workspaces.length === 0) return;
    setWorkspaceId(workspaces[0]!.id);
  }, [workspaceId, workspaces]);

  const selectedWorkspace = useMemo<Workspace | undefined>(
    () => workspaces.find((w) => w.id === workspaceId),
    [workspaces, workspaceId],
  );

  // Mirror the chosen workspace into the global slug singleton so api.* calls
  // attach the correct X-Workspace-Slug header. The dashboard layout normally
  // owns this; we're outside the dashboard tree so we have to set it ourselves.
  // Reset to null on unmount so other routes that mount before a workspace is
  // chosen don't see a stale slug.
  useEffect(() => {
    if (!selectedWorkspace) return;
    setCurrentWorkspace(selectedWorkspace.slug, selectedWorkspace.id);
    return () => {
      setCurrentWorkspace(null, null);
    };
  }, [selectedWorkspace]);

  const [title, setTitle] = useState(() => deriveTitle(draft));
  const [description, setDescription] = useState(() => deriveDescription(draft));
  const [projectId, setProjectId] = useState<string | undefined>(undefined);
  const [submitting, setSubmitting] = useState(false);

  const projectsQuery = useQuery({
    queryKey: ["share", "projects", workspaceId ?? ""],
    queryFn: () => api.listProjects(),
    enabled: !!workspaceId,
  });
  const projects: Project[] = projectsQuery.data?.projects ?? [];

  // Drop the IDB row + reset the slug singleton. Called both on a successful
  // submit and on cancel — once the user has interacted, the draft is no
  // longer useful and shouldn't survive into a subsequent share.
  const cleanup = async () => {
    setCurrentWorkspace(null, null);
    try {
      await deleteShareDraft(draft.token);
    } catch {
      // Cleanup is best-effort; TTL prune covers the case where the delete
      // failed (quota, db-locked).
    }
  };

  const onCancel = async () => {
    await cleanup();
    if (selectedWorkspace) {
      router.replace(paths.workspace(selectedWorkspace.slug).issues());
    } else {
      router.replace(paths.root());
    }
  };

  const onSubmit = async () => {
    if (submitting) return;
    if (!selectedWorkspace) {
      toast.error("Pick a workspace first");
      return;
    }
    if (!title.trim()) {
      toast.error("Add a title");
      return;
    }
    if (!projectId) {
      toast.error("Pick a project");
      return;
    }
    setSubmitting(true);
    try {
      // Upload attached files first so we can pass attachment_ids inline
      // when creating the issue. Sequential upload keeps memory pressure low
      // on phones (multipart uploads buffer the whole file in memory) and
      // avoids hammering the server with parallel multipart bodies.
      const attachmentIds: string[] = [];
      for (const file of draft.files) {
        const f = new File([file.blob], file.name, { type: file.type });
        const att = await api.uploadFile(f);
        attachmentIds.push(att.id);
      }

      const issue = await api.createIssue({
        title: title.trim(),
        description: description.trim() || undefined,
        project_id: projectId,
        attachment_ids: attachmentIds.length > 0 ? attachmentIds : undefined,
      });

      // Surface the new issue immediately on the workspace's issues list
      // when the user lands there — saves a refetch round-trip. Failure here
      // (cache miss, etc.) is non-fatal.
      qc.invalidateQueries({ queryKey: ["issues"] });
      await cleanup();
      router.replace(paths.workspace(selectedWorkspace.slug).issueDetail(issue.id));
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Failed to create issue";
      toast.error(msg);
      setSubmitting(false);
    }
  };

  if (wsLoading) {
    return <CenteredSpinner />;
  }

  if (workspaces.length === 0) {
    return (
      <ErrorScreen
        title="No workspace"
        body="Create or join a workspace before sharing content into Multica."
        action={
          <Button onClick={() => router.replace(paths.newWorkspace())}>
            Create workspace
          </Button>
        }
      />
    );
  }

  return (
    <main className="mx-auto flex min-h-dvh max-w-xl flex-col gap-4 p-4 pb-[max(env(safe-area-inset-bottom),1rem)]">
      <header className="flex items-center justify-between">
        <h1 className="text-lg font-semibold">New issue from share</h1>
      </header>

      {oversizedFilenames.length > 0 && (
        <div className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm">
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600" />
          <div>
            <div className="font-medium">Some files were skipped</div>
            <div className="text-muted-foreground">
              Files larger than 5MB are too big to share into Multica:{" "}
              {oversizedFilenames.join(", ")}.
            </div>
          </div>
        </div>
      )}

      <div className="flex flex-col gap-2">
        <Label htmlFor="ws-picker">Workspace</Label>
        {workspaces.length === 1 ? (
          <div className="rounded-md border bg-muted/40 px-3 py-2 text-sm">
            {workspaces[0]!.name}
          </div>
        ) : (
          <Select
            value={workspaceId ?? ""}
            onValueChange={(v) => setWorkspaceId(v || undefined)}
          >
            <SelectTrigger id="ws-picker">
              <SelectValue placeholder="Pick a workspace" />
            </SelectTrigger>
            <SelectContent>
              {workspaces.map((w) => (
                <SelectItem key={w.id} value={w.id}>
                  {w.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="title">Title</Label>
        <Input
          id="title"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Issue title"
          autoFocus={!title}
        />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="description">Description</Label>
        <Textarea
          id="description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          rows={6}
          placeholder="Add context (optional)"
        />
      </div>

      <div className="flex flex-col gap-2">
        <Label htmlFor="project">Project</Label>
        <Select value={projectId ?? ""} onValueChange={(v) => setProjectId(v || undefined)}>
          <SelectTrigger id="project">
            <SelectValue placeholder={projectsQuery.isLoading ? "Loading…" : "Pick a project"} />
          </SelectTrigger>
          <SelectContent>
            {projects.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {p.title}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {draft.files.length > 0 && (
        <div className="flex flex-col gap-2">
          <Label>Attachments</Label>
          <ul className="flex flex-col gap-1">
            {draft.files.map((f, i) => (
              <li
                key={`${f.name}-${i}`}
                className="flex items-center gap-2 rounded-md border bg-muted/30 px-3 py-2 text-sm"
              >
                <Paperclip className="size-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{f.name}</span>
                <span className="ml-auto shrink-0 text-xs text-muted-foreground">
                  {formatBytes(f.blob.size)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}

      <div className="mt-auto flex items-center justify-end gap-2 pt-4">
        <Button
          type="button"
          variant="ghost"
          onClick={onCancel}
          disabled={submitting}
        >
          Cancel
        </Button>
        <Button
          type="button"
          onClick={onSubmit}
          disabled={submitting || !title.trim() || !projectId}
        >
          {submitting ? (
            <>
              <Loader2 className="mr-2 size-4 animate-spin" />
              Creating…
            </>
          ) : (
            "Create issue"
          )}
        </Button>
      </div>
    </main>
  );
}

function CenteredSpinner() {
  return (
    <div className="flex min-h-dvh items-center justify-center">
      <Loader2 className="size-6 animate-spin text-muted-foreground" />
    </div>
  );
}

function ErrorScreen({
  title,
  body,
  action,
}: {
  title: string;
  body: string;
  action?: React.ReactNode;
}) {
  return (
    <main className="mx-auto flex min-h-dvh max-w-md flex-col items-center justify-center gap-3 p-6 text-center">
      <h1 className="text-lg font-semibold">{title}</h1>
      <p className="text-sm text-muted-foreground">{body}</p>
      {action}
    </main>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}
