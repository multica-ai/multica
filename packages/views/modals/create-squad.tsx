"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import { useNavigation } from "../navigation";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@multica/ui/components/ui/dialog";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { toast } from "sonner";
import { isImeComposing } from "@multica/core/utils";
import { ActorAvatar } from "../common/actor-avatar";
import { useT } from "../i18n";
import type { Agent } from "@multica/core/types";

export function CreateSquadModal({ onClose }: { onClose: () => void }) {
  const { t } = useT("modals");
  const router = useNavigation();
  const wsPaths = useWorkspacePaths();
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const activeAgents = agents.filter((a: Agent) => !a.archived_at && a.runtime_id);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [leaderId, setLeaderId] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async () => {
    if (!name.trim() || !leaderId || submitting) return;
    setSubmitting(true);
    try {
      const squad = await api.createSquad({
        name: name.trim(),
        description: description.trim() || undefined,
        leader_id: leaderId,
      });
      queryClient.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
      onClose();
      toast.success(t(($) => $.create_squad.toast_created));
      router.push(wsPaths.squadDetail(squad.id));
    } catch {
      toast.error(t(($) => $.create_squad.toast_failed));
      setSubmitting(false);
    }
  };

  return (
    <Dialog open onOpenChange={(v) => { if (!v) onClose(); }}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.create_squad.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.create_squad.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 min-w-0">
          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create_squad.name_label)}</Label>
            <Input
              autoFocus
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t(($) => $.create_squad.name_placeholder)}
              className="mt-1"
              onKeyDown={(e) => {
                if (isImeComposing(e)) return;
                if (e.key === "Enter") handleSubmit();
              }}
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create_squad.description_label)}</Label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder={t(($) => $.create_squad.description_placeholder)}
              rows={3}
              className="mt-1 w-full rounded-md border bg-transparent px-3 py-2 text-sm outline-none resize-none focus:ring-1 focus:ring-ring"
            />
          </div>

          <div>
            <Label className="text-xs text-muted-foreground">{t(($) => $.create_squad.leader_label)}</Label>
            <p className="text-xs text-muted-foreground mt-0.5 mb-1.5">
              {t(($) => $.create_squad.leader_hint)}
            </p>
            <div className="grid gap-1.5 max-h-40 overflow-y-auto rounded-lg border p-1.5">
              {activeAgents.length === 0 ? (
                <p className="px-2 py-3 text-center text-xs text-muted-foreground">
                  {t(($) => $.create_squad.no_agents)}
                </p>
              ) : (
                activeAgents.map((a: Agent) => (
                  <button
                    key={a.id}
                    type="button"
                    onClick={() => setLeaderId(a.id)}
                    className={`flex items-center gap-2.5 rounded-md px-2.5 py-2 text-sm transition-colors ${
                      leaderId === a.id
                        ? "border border-primary bg-primary/5"
                        : "hover:bg-muted"
                    }`}
                  >
                    <ActorAvatar actorType="agent" actorId={a.id} size={24} showStatusDot />
                    <div className="text-left min-w-0 flex-1">
                      <div className="font-medium truncate">{a.name}</div>
                      {a.description && (
                        <div className="text-xs text-muted-foreground truncate">{a.description}</div>
                      )}
                    </div>
                  </button>
                ))
              )}
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>{t(($) => $.create_squad.cancel)}</Button>
          <Button onClick={handleSubmit} disabled={!name.trim() || !leaderId || submitting}>
            {submitting ? t(($) => $.create_squad.submitting) : t(($) => $.create_squad.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
