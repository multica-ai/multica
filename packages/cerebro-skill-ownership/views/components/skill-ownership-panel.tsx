"use client";

import { useState } from "react";
import { Crown, Shield, UserCircle2, X } from "lucide-react";
import { toast } from "sonner";
import type { MemberWithUser, Skill } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Badge } from "@multica/ui/components/ui/badge";
import { useUpdateSkillOwnership } from "../../core/mutations";

interface Props {
  skill: Skill;
  wsId: string;
  members: MemberWithUser[];
  canManage: boolean;
}

export function SkillOwnershipPanel({ skill, wsId, members, canManage }: Props) {
  const [editOpen, setEditOpen] = useState(false);
  const [selectedOwner, setSelectedOwner] = useState<string | null>(
    skill.owner_id ?? null,
  );
  const [selectedApprovers, setSelectedApprovers] = useState<string[]>(
    skill.approver_ids ?? [],
  );

  const mutation = useUpdateSkillOwnership(skill.id, wsId);

  const owner = members.find((m) => m.user_id === skill.owner_id);
  const approvers = (skill.approver_ids ?? [])
    .map((id) => members.find((m) => m.user_id === id))
    .filter(Boolean) as MemberWithUser[];

  const handleOpen = () => {
    setSelectedOwner(skill.owner_id ?? null);
    setSelectedApprovers(skill.approver_ids ?? []);
    setEditOpen(true);
  };

  const handleSave = async () => {
    try {
      await mutation.mutateAsync({
        owner_id: selectedOwner,
        approver_ids: selectedApprovers,
      });
      toast.success("Ownership updated");
      setEditOpen(false);
    } catch {
      toast.error("Failed to update ownership");
    }
  };

  const toggleApprover = (userId: string) => {
    setSelectedApprovers((prev) =>
      prev.includes(userId)
        ? prev.filter((id) => id !== userId)
        : [...prev, userId],
    );
  };

  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
          Ownership
        </h3>
        {canManage && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            onClick={handleOpen}
            className="h-5 px-1.5 text-xs"
          >
            Edit
          </Button>
        )}
      </div>

      <div className="space-y-2 text-xs">
        <div className="flex items-center gap-1.5">
          <Crown className="h-3 w-3 shrink-0 text-amber-500" />
          <span className="text-muted-foreground">Owner:</span>
          <span className="font-medium">
            {owner ? owner.name : <span className="italic text-muted-foreground">None</span>}
          </span>
        </div>

        <div className="flex items-start gap-1.5">
          <Shield className="mt-0.5 h-3 w-3 shrink-0 text-blue-500" />
          <span className="text-muted-foreground shrink-0">Approvers:</span>
          {approvers.length === 0 ? (
            <span className="italic text-muted-foreground">None</span>
          ) : (
            <div className="flex flex-wrap gap-1">
              {approvers.map((m) => (
                <Badge key={m.user_id} variant="secondary" className="h-4 text-xs">
                  {m.name}
                </Badge>
              ))}
            </div>
          )}
        </div>
      </div>

      <Dialog open={editOpen} onOpenChange={(v) => !mutation.isPending && setEditOpen(v)}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>Edit ownership</DialogTitle>
          </DialogHeader>

          <div className="space-y-4">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">
                Owner
              </label>
              <Select
                value={selectedOwner ?? "__none__"}
                onValueChange={(v) =>
                  setSelectedOwner(v === "__none__" ? null : v)
                }
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder="No owner" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__none__">
                    <span className="text-muted-foreground italic">No owner</span>
                  </SelectItem>
                  {members.map((m) => (
                    <SelectItem key={m.user_id} value={m.user_id}>
                      {m.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-1.5">
              <label className="text-xs font-medium text-muted-foreground">
                Approvers
              </label>
              <div className="max-h-40 overflow-y-auto rounded-md border p-1">
                {members.map((m) => {
                  const checked = selectedApprovers.includes(m.user_id);
                  return (
                    <button
                      key={m.user_id}
                      type="button"
                      onClick={() => toggleApprover(m.user_id)}
                      className={`flex w-full items-center gap-2 rounded px-2 py-1 text-left text-xs transition-colors hover:bg-accent ${checked ? "bg-accent/60" : ""}`}
                    >
                      <UserCircle2 className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                      <span className="flex-1 truncate">{m.name}</span>
                      {checked && (
                        <Shield className="h-3 w-3 shrink-0 text-blue-500" />
                      )}
                    </button>
                  );
                })}
              </div>
              {selectedApprovers.length > 0 && (
                <div className="flex flex-wrap gap-1 pt-0.5">
                  {selectedApprovers.map((id) => {
                    const m = members.find((m) => m.user_id === id);
                    if (!m) return null;
                    return (
                      <Badge
                        key={id}
                        variant="secondary"
                        className="h-5 gap-1 pr-1 text-xs"
                      >
                        {m.name}
                        <button
                          type="button"
                          onClick={() => toggleApprover(id)}
                          className="ml-0.5 rounded hover:text-destructive"
                        >
                          <X className="h-2.5 w-2.5" />
                        </button>
                      </Badge>
                    );
                  })}
                </div>
              )}
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setEditOpen(false)}
              disabled={mutation.isPending}
            >
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleSave}
              disabled={mutation.isPending}
            >
              {mutation.isPending ? "Saving…" : "Save"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
