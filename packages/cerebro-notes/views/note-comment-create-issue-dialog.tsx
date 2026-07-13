"use client";

// NoteCommentCreateIssueDialog (FIR-3102) — create a standalone issue from a
// single note comment. Prefills the title from the comment's first line and
// lets the user pick a project and assignee before creating. The comment's text
// becomes the issue description server-side, and the new issue is linked back
// onto that exact comment (cerebro_note_comment.issue_id) so the panel shows an
// "opened <issue>" chip on the comment afterwards.
//
// This is the COMMENT-level coupling: unlike NoteCreateIssueDialog (which files
// an issue from the whole note and links it as a note reference), this couples
// at the comment. Mirrors NoteCreateIssueDialog's shape; adds project/assignee.
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
import { ProjectPicker } from "@multica/views/projects/components";
import { AssigneePicker } from "@multica/views/issues/components";
import type { IssueAssigneeType } from "@multica/core/types";
import {
  useCreateIssueFromNoteComment,
  commentIssueTitle,
  type NoteComment,
} from "../core";

export function NoteCommentCreateIssueDialog({
  noteId,
  comment,
  open,
  onOpenChange,
}: {
  noteId: string;
  comment: NoteComment;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const createIssue = useCreateIssueFromNoteComment(noteId);
  const [title, setTitle] = React.useState("");
  const [projectId, setProjectId] = React.useState<string | null>(null);
  const [assigneeType, setAssigneeType] =
    React.useState<IssueAssigneeType | null>(null);
  const [assigneeId, setAssigneeId] = React.useState<string | null>(null);
  const submitting = createIssue.isPending;

  // Re-seed the title from the comment each time the dialog opens, so it tracks
  // a comment edited since last time. Project/assignee start empty (unset).
  React.useEffect(() => {
    if (open) setTitle(commentIssueTitle(comment.body));
  }, [open, comment.body]);

  const handleCreate = () => {
    const trimmed = title.trim();
    if (!trimmed || submitting) return;
    createIssue.mutate(
      {
        commentId: comment.id,
        title: trimmed,
        projectId: projectId ?? undefined,
        // Only send an assignee when both are set (the picker always sets them
        // together; guard anyway so a half-set state never reaches the API).
        assigneeType: assigneeType && assigneeId ? assigneeType : undefined,
        assigneeId: assigneeType && assigneeId ? assigneeId : undefined,
      },
      {
        onSuccess: () => {
          // Stay in the note: the comment now carries its issue_id, so the panel
          // renders an "opened <issue>" chip on it (a link to open the issue).
          toast.success("Issue created from comment");
          onOpenChange(false);
        },
        onError: () => {
          toast.error("Could not create issue");
        },
      },
    );
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
            Create issue from comment
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3 py-1">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="note-comment-issue-title">Title</Label>
            <Input
              id="note-comment-issue-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Issue title"
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) handleCreate();
              }}
              autoFocus
            />
          </div>
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
            <div className="flex items-center gap-2">
              <Label className="text-muted-foreground">Project</Label>
              <ProjectPicker
                projectId={projectId}
                onUpdate={(u) => setProjectId(u.project_id ?? null)}
                align="start"
              />
            </div>
            <div className="flex items-center gap-2">
              <Label className="text-muted-foreground">Assignee</Label>
              <AssigneePicker
                assigneeType={assigneeType}
                assigneeId={assigneeId}
                onUpdate={(u) => {
                  setAssigneeType(
                    (u.assignee_type ?? null) as IssueAssigneeType | null,
                  );
                  setAssigneeId(u.assignee_id ?? null);
                }}
                align="start"
              />
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            The comment’s text becomes the issue description, and the new issue is
            linked back on this comment.
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
