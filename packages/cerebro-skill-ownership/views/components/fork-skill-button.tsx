"use client";

import { useState } from "react";
import { GitFork, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { workspaceKeys } from "@multica/core/workspace/queries";
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
import { useNavigation } from "@multica/views/navigation";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { skillForksOptions } from "../../core/queries";

/**
 * Header action that forks a skill into a new skill owned by the current user.
 * Lives behind the `cerebro_skill_ownership` flag. On success it navigates to
 * the new fork's detail page and refreshes the skill list + the parent's fork
 * list so the lineage shows up immediately.
 */
export function ForkSkillButton({ skill }: { skill: Pick<Skill, "id" | "name" | "description"> }) {
  const enabled = useFeatureFlag("cerebro_skill_ownership");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const mutation = useMutation({
    mutationFn: () =>
      api.createSkillFork(skill.id, {
        name: name.trim(),
        description: description.trim() || undefined,
      }),
    onSuccess: (forked) => {
      qc.invalidateQueries({ queryKey: workspaceKeys.skills(wsId), exact: true });
      qc.invalidateQueries({
        queryKey: skillForksOptions(wsId, skill.id).queryKey,
      });
      setOpen(false);
      setName("");
      setDescription("");
      toast.success("Skill forket");
      navigation.push(paths.skillDetail(forked.id));
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : "Kunne ikke forke skill");
    },
  });

  if (!enabled) return null;

  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="xs"
        onClick={() => {
          setName(`${skill.name}-fork`);
          setDescription("");
          setOpen(true);
        }}
        className="text-muted-foreground"
      >
        <GitFork className="h-3.5 w-3.5" />
        Fork
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Fork skill</DialogTitle>
            <DialogDescription>
              Opretter en uafhængig kopi af “{skill.name}” med dig som ejer.
              Forken får sin egen versionshistorik og ejerskab.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1">
              <Label htmlFor="fork-name" className="text-xs text-muted-foreground">
                Navn
              </Label>
              <Input
                id="fork-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Navn på forken"
              />
            </div>
            <div className="space-y-1">
              <Label
                htmlFor="fork-description"
                className="text-xs text-muted-foreground"
              >
                Beskrivelse (valgfri)
              </Label>
              <Textarea
                id="fork-description"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                rows={2}
                className="resize-none text-sm"
                placeholder="Arver forælderens beskrivelse hvis tom"
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => setOpen(false)}
              disabled={mutation.isPending}
            >
              Annuller
            </Button>
            <Button
              type="button"
              onClick={() => mutation.mutate()}
              disabled={mutation.isPending || !name.trim()}
            >
              {mutation.isPending ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin" />
                  Forker…
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
    </>
  );
}
