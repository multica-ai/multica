"use client";

import { useState } from "react";
import { ChevronDown, ChevronRight, GitFork, Loader2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
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
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Badge } from "@multica/ui/components/ui/badge";
import { skillForksOptions } from "../../core/queries";
import { useCreateSkillFork } from "../../core/mutations";

interface Props {
  skill: Skill;
  wsId: string;
}

export function SkillForkPanel({ skill, wsId }: Props) {
  const [open, setOpen] = useState(false);
  const [forkOpen, setForkOpen] = useState(false);
  const [forkName, setForkName] = useState(`${skill.name}-fork`);
  const [forkDesc, setForkDesc] = useState("");

  const { data: forks = [], isLoading } = useQuery(skillForksOptions(skill.id));
  const mutation = useCreateSkillFork(skill.id, wsId);

  const handleFork = async () => {
    if (!forkName.trim()) return;
    try {
      await mutation.mutateAsync({
        name: forkName.trim(),
        description: forkDesc.trim() || undefined,
      });
      toast.success(`Forked as "${forkName.trim()}"`);
      setForkOpen(false);
      setForkName(`${skill.name}-fork`);
      setForkDesc("");
    } catch {
      toast.error("Failed to fork skill");
    }
  };

  return (
    <div>
      <div className="flex items-center justify-between">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wider text-muted-foreground hover:text-foreground"
        >
          <GitFork className="h-3 w-3" />
          Forks
          {forks.length > 0 && (
            <Badge variant="secondary" className="h-4 text-xs">
              {forks.length}
            </Badge>
          )}
          {open ? (
            <ChevronDown className="h-3 w-3" />
          ) : (
            <ChevronRight className="h-3 w-3" />
          )}
        </button>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="h-5 gap-1 px-1.5 text-xs"
          onClick={() => setForkOpen(true)}
        >
          <GitFork className="h-3 w-3" />
          Fork
        </Button>
      </div>

      {open && (
        <div className="mt-2 space-y-1">
          {isLoading && (
            <div className="text-xs text-muted-foreground">Loading…</div>
          )}
          {!isLoading && forks.length === 0 && (
            <div className="rounded-md border border-dashed px-3 py-3 text-center text-xs text-muted-foreground">
              No forks yet.
            </div>
          )}
          {forks.map((fork) => (
            <div
              key={fork.id}
              className="rounded-md border bg-card px-2.5 py-1.5"
            >
              <div className="flex items-center gap-1.5">
                <GitFork className="h-3 w-3 shrink-0 text-muted-foreground" />
                <span className="truncate font-mono text-xs font-medium">
                  {fork.forked_name ?? fork.forked_skill_id.slice(0, 8)}
                </span>
                {fork.current_version && (
                  <Badge variant="outline" className="h-4 text-xs">
                    {fork.current_version}
                  </Badge>
                )}
              </div>
              {fork.forked_description && (
                <div className="mt-0.5 truncate text-xs text-muted-foreground">
                  {fork.forked_description}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <Dialog
        open={forkOpen}
        onOpenChange={(v) => !mutation.isPending && setForkOpen(v)}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>Fork "{skill.name}"</DialogTitle>
            <DialogDescription>
              Creates a copy of this skill you can edit independently.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3">
            <div className="space-y-1.5">
              <Label htmlFor="fork-name" className="text-xs">
                Name <span className="text-destructive">*</span>
              </Label>
              <Input
                id="fork-name"
                value={forkName}
                onChange={(e) => setForkName(e.target.value)}
                className="h-8 font-mono text-sm"
              />
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="fork-desc" className="text-xs">
                Description{" "}
                <span className="text-muted-foreground">(optional)</span>
              </Label>
              <Textarea
                id="fork-desc"
                value={forkDesc}
                onChange={(e) => setForkDesc(e.target.value)}
                placeholder="What makes this fork different…"
                rows={2}
                className="resize-none text-sm"
              />
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setForkOpen(false)}
              disabled={mutation.isPending}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleFork}
              disabled={mutation.isPending || !forkName.trim()}
            >
              {mutation.isPending ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  Forking…
                </>
              ) : (
                <>
                  <GitFork className="h-3 w-3" />
                  Fork skill
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
