"use client";

// NoteCoupleAndSend (FIR-1676) — closes the gap in the agent-collaboration flow:
// when a note has a comment that @-tags an agent but the note is not yet coupled
// to a task, the panel used to dead-end at "go link it in References". Tagging an
// agent should be enough to act. This offers the two routes Jesper asked for,
// inline, right where the unsent-comments notice lives:
//
//   - "Create issue & send" — make a new issue from the note (its body becomes
//     the description), link it back on the note, then dispatch the tagged
//     comments to the new issue's agent.
//   - "Send to existing…" — pick an existing issue (reusing the same issue
//     picker the References add-flow uses), couple the note to it, then dispatch.
//
// Both end in the same place: the note is coupled and the tagged comments are
// sent. The selected passage each comment is anchored to already rides along in
// the dispatched bundle (server buildBundle), so the agent sees WHICH text the
// remark is about — that is the "expose the connection to the agent" half.
import * as React from "react";
import { ListPlus, Link2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { useCreateIssue } from "@multica/core/issues/mutations";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAddNoteReference, useSendNoteComments } from "../core";
import { NoteAddReferenceDialog } from "./note-references";

function truncate(s: string, max = 60): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s;
}

// deriveTitle seeds the new-issue title from the note: the first non-empty line,
// stripped of leading markdown so a heading like "# Plan" becomes "Plan". Falls
// back to "Note" so the title is never empty. The user can edit it before
// creating.
function deriveTitle(body: string): string {
  const line = (body ?? "")
    .split("\n")
    .map((l) => l.trim())
    .find(Boolean);
  const cleaned = (line ?? "")
    .replace(/^#+\s*/, "")
    .replace(/[*_`>]/g, "")
    .trim();
  return truncate(cleaned || "Note", 80);
}

export function NoteCoupleAndSend({
  noteId,
  noteBody,
  unsentCount,
}: {
  noteId: string;
  noteBody: string;
  unsentCount: number;
}) {
  const createIssue = useCreateIssue();
  const addReference = useAddNoteReference(noteId);
  const send = useSendNoteComments(noteId);
  const paths = useWorkspacePaths();
  const [createOpen, setCreateOpen] = React.useState(false);
  const [pickOpen, setPickOpen] = React.useState(false);
  const [title, setTitle] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  // Re-seed the title from the note each time the dialog opens so it tracks edits
  // made since last time.
  React.useEffect(() => {
    if (createOpen) setTitle(deriveTitle(noteBody));
  }, [createOpen, noteBody]);

  // Couple the note to the issue, then send all unsent tagged comments to it.
  // The reference is awaited first because the server resolves the coupling from
  // the note's references when it dispatches — the link must exist before send.
  async function coupleAndSend(refInput: {
    ref_id: string;
    label: string;
    url: string;
  }) {
    await addReference.mutateAsync({ object: "issue", ...refInput });
    // The note had no coupling before this (NoteCoupleAndSend only renders when
    // uncoupled), so it now has exactly one — no destination hint needed.
    const res = await send.mutateAsync({});
    const n = res.sent.length;
    toast.success(
      n === 1
        ? "Linked the task and sent 1 comment to its agent"
        : `Linked the task and sent ${n} comments to its agent`,
    );
  }

  async function handleCreate() {
    const t = title.trim();
    if (!t || busy) return;
    setBusy(true);
    try {
      const issue = await createIssue.mutateAsync({
        title: t,
        description: noteBody ?? "",
      });
      await coupleAndSend({
        ref_id: issue.id,
        label: truncate(`${issue.identifier} ${issue.title}`),
        url: paths.issueDetail(issue.id),
      });
      setCreateOpen(false);
    } catch {
      toast.error("Couldn't create the issue and send. Try again.");
    } finally {
      setBusy(false);
    }
  }

  async function handlePick(issue: {
    id: string;
    identifier: string;
    title: string;
  }) {
    setPickOpen(false);
    setBusy(true);
    try {
      await coupleAndSend({
        ref_id: issue.id,
        label: truncate(`${issue.identifier} ${issue.title}`),
        url: paths.issueDetail(issue.id),
      });
    } catch {
      toast.error("Couldn't link the issue and send. Try again.");
    } finally {
      setBusy(false);
    }
  }

  const pending = busy || send.isPending || addReference.isPending;

  return (
    <div className="flex flex-col gap-2">
      <p className="text-[11px] text-muted-foreground">
        This note isn’t linked to a task yet. Create one from the note, or link
        an existing task — the {unsentCount === 1 ? "comment" : "comments"} go to
        its agent.
      </p>
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" onClick={() => setCreateOpen(true)} disabled={pending}>
          <ListPlus className="size-3.5" /> Create issue &amp; send
        </Button>
        <Button
          size="sm"
          variant="secondary"
          onClick={() => setPickOpen(true)}
          disabled={pending}
        >
          <Link2 className="size-3.5" /> Send to existing…
        </Button>
      </div>

      <Dialog
        open={createOpen}
        onOpenChange={(v) => {
          if (!v && busy) return;
          setCreateOpen(v);
        }}
      >
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-base">
              <ListPlus className="size-4" />
              Create issue &amp; send
            </DialogTitle>
          </DialogHeader>
          <div className="flex flex-col gap-3 py-1">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="note-couple-title">Title</Label>
              <Input
                id="note-couple-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="Issue title"
                autoFocus
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !e.shiftKey) handleCreate();
                }}
              />
            </div>
            <p className="text-xs text-muted-foreground">
              The note’s content becomes the issue description, the issue is
              linked back on this note, and the{" "}
              {unsentCount === 1
                ? "tagged comment is sent"
                : "tagged comments are sent"}{" "}
              to the agent.
            </p>
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => setCreateOpen(false)}
              disabled={busy}
            >
              Cancel
            </Button>
            <Button onClick={handleCreate} disabled={!title.trim() || busy}>
              {busy ? "Working…" : "Create & send"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <NoteAddReferenceDialog
        noteId={noteId}
        open={pickOpen}
        onOpenChange={setPickOpen}
        onPicked={handlePick}
      />
    </div>
  );
}
