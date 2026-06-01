"use client";

import { useState } from "react";
import { GitPullRequest, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
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
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { skillOwnershipKeys } from "../../core/queries";

const SEMVER = /^\d+\.\d+\.\d+$/;

/** Bumps the patch component of an X.Y.Z version; falls back to 0.0.1. */
function bumpPatch(version: string): string {
  const m = SEMVER.exec(version);
  if (!m) return "0.0.1";
  const parts = version.split(".").map((n) => parseInt(n, 10));
  return `${parts[0]}.${parts[1]}.${(parts[2] ?? 0) + 1}`;
}

/**
 * Dialog to open a change request against a skill. Pre-fills the proposed
 * content with the skill's current SKILL.md and the proposed version with the
 * next patch bump, so a contributor just edits and submits. The backend
 * enforces semver > current and snapshots files on approval; we carry the
 * skill's current files through unchanged (this UI edits the SKILL.md body).
 */
export function CreateChangeRequestDialog({
  skill,
  wsId,
  open,
  onOpenChange,
}: {
  skill: Skill;
  wsId: string;
  open: boolean;
  onOpenChange: (v: boolean) => void;
}) {
  const qc = useQueryClient();
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [proposedVersion, setProposedVersion] = useState(() =>
    bumpPatch(skill.current_version),
  );
  const [proposedContent, setProposedContent] = useState(skill.content);
  const [seededFor, setSeededFor] = useState<string | null>(null);

  // Reseed the editable fields whenever the dialog opens for a (possibly
  // updated) skill, without clobbering edits while it stays open.
  const seedKey = `${skill.id}@${skill.updated_at}`;
  if (open && seededFor !== seedKey) {
    setSeededFor(seedKey);
    setTitle("");
    setDescription("");
    setProposedVersion(bumpPatch(skill.current_version));
    setProposedContent(skill.content);
  }

  const mutation = useMutation({
    mutationFn: () =>
      api.createSkillChangeRequest(skill.id, {
        title: title.trim(),
        description: description.trim() || undefined,
        proposed_version: proposedVersion.trim(),
        proposed_content: proposedContent,
        proposed_files: (skill.files ?? []).map((f) => ({
          path: f.path,
          content: f.content,
        })),
      }),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: skillOwnershipKeys.changeRequests(wsId, skill.id),
      });
      onOpenChange(false);
      toast.success("Forslag oprettet");
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Kunne ikke oprette forslag",
      );
    },
  });

  const versionValid = SEMVER.test(proposedVersion.trim());

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>Foreslå ændring</DialogTitle>
          <DialogDescription>
            Forslag til “{skill.name}”. Når en ejer eller godkender godkender
            forslaget, opdateres skillen og en ny version gemmes.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-[1fr_8rem]">
            <div className="space-y-1">
              <Label htmlFor="cr-title" className="text-xs text-muted-foreground">
                Titel
              </Label>
              <Input
                id="cr-title"
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                placeholder="Kort beskrivelse af ændringen"
              />
            </div>
            <div className="space-y-1">
              <Label
                htmlFor="cr-version"
                className="text-xs text-muted-foreground"
              >
                Ny version
              </Label>
              <Input
                id="cr-version"
                value={proposedVersion}
                onChange={(e) => setProposedVersion(e.target.value)}
                placeholder="X.Y.Z"
                aria-invalid={!versionValid}
                className="font-mono"
              />
            </div>
          </div>
          <div className="space-y-1">
            <Label
              htmlFor="cr-description"
              className="text-xs text-muted-foreground"
            >
              Beskrivelse (valgfri)
            </Label>
            <Textarea
              id="cr-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={2}
              className="resize-none text-sm"
              placeholder="Hvorfor denne ændring?"
            />
          </div>
          <div className="space-y-1">
            <Label
              htmlFor="cr-content"
              className="text-xs text-muted-foreground"
            >
              Foreslået SKILL.md
            </Label>
            <Textarea
              id="cr-content"
              value={proposedContent}
              onChange={(e) => setProposedContent(e.target.value)}
              rows={12}
              className="resize-y font-mono text-xs"
            />
          </div>
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="ghost"
            onClick={() => onOpenChange(false)}
            disabled={mutation.isPending}
          >
            Annuller
          </Button>
          <Button
            type="button"
            onClick={() => mutation.mutate()}
            disabled={mutation.isPending || !title.trim() || !versionValid}
          >
            {mutation.isPending ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                Opretter…
              </>
            ) : (
              <>
                <GitPullRequest className="h-3 w-3" />
                Opret forslag
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
