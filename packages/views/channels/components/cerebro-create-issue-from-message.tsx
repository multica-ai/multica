// CEREBRO-PATCH(channels-create-issue-from-message): TECH-2909 — dialog for creating
// an issue from a channel message. Prefills title + description from the message
// content and posts a thread reply with a mention link once the issue is created.
"use client";

import { useState, useEffect } from "react";
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
import { useCreateIssue, useCreateComment } from "@multica/core/issues/mutations";
import type { TimelineEntry } from "@multica/core/types";

function defaultTitle(content: string): string {
  const first = (content.split("\n")[0] ?? "").replace(/^#+\s*/, "").trim();
  return first.length > 80 ? `${first.slice(0, 80)}…` : first || "Nyt issue";
}

interface CreateIssueFromMessageDialogProps {
  open: boolean;
  onClose: () => void;
  channelId: string;
  entry: TimelineEntry;
}

export function CreateIssueFromMessageDialog({
  open,
  onClose,
  channelId,
  entry,
}: CreateIssueFromMessageDialogProps) {
  const [title, setTitle] = useState(() => defaultTitle(entry.content ?? ""));
  const [isSubmitting, setIsSubmitting] = useState(false);
  const createIssue = useCreateIssue();

  // Reset form state each time the dialog opens so stale title/isSubmitting don't persist.
  useEffect(() => {
    if (open) {
      setTitle(defaultTitle(entry.content ?? ""));
      setIsSubmitting(false);
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
  const { mutateAsync: createComment } = useCreateComment(channelId);

  const handleCreate = async () => {
    if (!title.trim() || isSubmitting) return;
    setIsSubmitting(true);
    let issue: Awaited<ReturnType<typeof createIssue.mutateAsync>>;
    try {
      issue = await createIssue.mutateAsync({
        title: title.trim(),
        description: entry.content ?? "",
      });
    } catch {
      toast.error("Issue kunne ikke oprettes");
      setIsSubmitting(false);
      return;
    }
    // Issue created — close dialog immediately so user gets feedback fast.
    // Thread reply is best-effort; failure shows a separate toast.
    toast.success("Issue oprettet");
    onClose();
    try {
      await createComment({
        content: `Issue oprettet fra denne besked: [${issue.identifier}](mention://issue/${issue.id})`,
        parentId: entry.id,
      });
    } catch {
      toast.error("Trådsvar kunne ikke postes — issue er oprettet");
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => { if (!v && !isSubmitting) onClose(); }}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ListPlus className="h-4 w-4" />
            Opret issue fra besked
          </DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-3 py-2">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="cifm-title">Titel</Label>
            <Input
              id="cifm-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Issue-titel"
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) handleCreate();
              }}
              autoFocus
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label>Beskedindhold</Label>
            <div className="max-h-24 overflow-y-auto rounded-md border bg-muted/30 px-3 py-2 text-sm text-muted-foreground whitespace-pre-wrap">
              {entry.content}
            </div>
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={isSubmitting}>
            Annuller
          </Button>
          <Button
            onClick={handleCreate}
            disabled={!title.trim() || isSubmitting}
          >
            Opret issue
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
