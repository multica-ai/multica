"use client";

import { useState } from "react";
import { Loader2, Send } from "lucide-react";
import { toast } from "sonner";
import type { Skill } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { useCreateSkillChangeRequest } from "../../core/mutations";

interface DraftFile {
  path: string;
  content: string;
}

interface Props {
  skill: Skill;
  /** Current draft content of SKILL.md */
  content: string;
  /** Current draft extra files */
  files: DraftFile[];
  /** Current draft name (for display only) */
  name: string;
  onDiscard: () => void;
}

export function SkillProposeBar({
  skill,
  content,
  files,
  onDiscard,
}: Props) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");

  const mutation = useCreateSkillChangeRequest(skill.id);

  const handleSubmit = async () => {
    if (!title.trim()) return;
    try {
      await mutation.mutateAsync({
        title: title.trim(),
        description: description.trim() || undefined,
        proposed_version: `proposed-${skill.current_version}`,
        proposed_content: content,
        proposed_files: files.length > 0 ? files : undefined,
      });
      toast.success("Change proposed — the owner will be notified.");
      setOpen(false);
      setTitle("");
      setDescription("");
      onDiscard();
    } catch {
      toast.error("Failed to submit proposal");
    }
  };

  return (
    <>
      <div
        role="status"
        aria-live="polite"
        className="flex flex-wrap items-center gap-2 border-t bg-blue-500/5 px-4 py-2"
      >
        <span className="h-1.5 w-1.5 rounded-full bg-blue-500" />
        <span className="text-xs text-muted-foreground">
          You&apos;re viewing a read-only skill — your edits will be proposed for review.
        </span>
        <div className="ml-auto flex items-center gap-1.5">
          <Button type="button" variant="ghost" size="xs" onClick={onDiscard}>
            Discard
          </Button>
          <Button
            type="button"
            size="xs"
            onClick={() => setOpen(true)}
            className="gap-1 bg-blue-600 text-white hover:bg-blue-700"
          >
            <Send className="h-3 w-3" />
            Propose change
          </Button>
        </div>
      </div>

      <Dialog open={open} onOpenChange={(v) => !mutation.isPending && setOpen(v)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Propose a change</DialogTitle>
            <DialogDescription>
              Your proposal will be sent to the skill owner for review.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="proposal-title" className="text-xs">
                Title <span className="text-destructive">*</span>
              </Label>
              <Input
                id="proposal-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="e.g. Add error-handling section"
                className="h-8 text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="proposal-desc" className="text-xs">
                Rationale{" "}
                <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Textarea
                id="proposal-desc"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Why this change improves the skill…"
                rows={3}
                className="resize-none text-sm"
              />
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setOpen(false)}
              disabled={mutation.isPending}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleSubmit}
              disabled={mutation.isPending || !title.trim()}
            >
              {mutation.isPending ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  Submitting…
                </>
              ) : (
                <>
                  <Send className="h-3 w-3" />
                  Submit proposal
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
