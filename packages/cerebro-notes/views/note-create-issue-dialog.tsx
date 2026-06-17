"use client";

// NoteCreateIssueDialog (TECH-3690) — create an issue straight from a note,
// opened from the note's "⋯" actions menu. Prefills the title from the note's
// title/first line and the description from the note body, then links the new
// issue back onto the note as a reference so the two stay connected. Mirrors
// the channel "create issue from message" flow (cerebro-create-issue-from-message).
import * as React from "react";
import { ListPlus } from "lucide-react";
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
import { useNavigation } from "@multica/views/navigation";
import { useAddNoteReference, firstLineTitle } from "../core";
import type { Note } from "../core";

function truncate(s: string, max = 60): string {
  return s.length > max ? `${s.slice(0, max - 1)}…` : s;
}

export function NoteCreateIssueDialog({
  note,
  open,
  onOpenChange,
}: {
  note: Note;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const createIssue = useCreateIssue();
  const addReference = useAddNoteReference(note.id);
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const [title, setTitle] = React.useState("");
  const [submitting, setSubmitting] = React.useState(false);

  // Re-seed the title from the note each time the dialog opens, so it tracks
  // edits the user made to the note since last time.
  React.useEffect(() => {
    if (open) setTitle(firstLineTitle(note));
  }, [open, note]);

  const handleCreate = async () => {
    const trimmed = title.trim();
    if (!trimmed || submitting) return;
    setSubmitting(true);
    let issue: Awaited<ReturnType<typeof createIssue.mutateAsync>>;
    try {
      issue = await createIssue.mutateAsync({
        title: trimmed,
        description: note.body ?? "",
      });
    } catch {
      toast.error("Could not create issue");
      setSubmitting(false);
      return;
    }
    // Link the issue back onto the note so the two stay connected. Best-effort:
    // the issue already exists, so a failed link just shows a softer toast.
    try {
      addReference.mutate({
        object: "issue",
        ref_id: issue.id,
        label: truncate(`${issue.identifier} ${issue.title}`),
        url: paths.issueDetail(issue.id),
      });
    } catch {
      /* reference is a nicety, not required */
    }
    toast.success(`Created ${issue.identifier}`);
    onOpenChange(false);
    setSubmitting(false);
    navigation.push(paths.issueDetail(issue.id));
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v && submitting) return;
        onOpenChange(v);
      }}
    >
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-base">
            <ListPlus className="size-4" />
            Create issue from note
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3 py-1">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="note-issue-title">Title</Label>
            <Input
              id="note-issue-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Issue title"
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) handleCreate();
              }}
              autoFocus
            />
          </div>
          <p className="text-xs text-muted-foreground">
            The note’s content becomes the issue description, and the new issue
            is linked back on this note.
          </p>
        </div>
        <DialogFooter>
          <Button
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={submitting}
          >
            Cancel
          </Button>
          <Button onClick={handleCreate} disabled={!title.trim() || submitting}>
            Create issue
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
