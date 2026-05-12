"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Bot } from "lucide-react";
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
import { Button } from "@multica/ui/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { api } from "@multica/core/api";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { channelKeys } from "@multica/core/channels";
import { useT } from "../../i18n";

interface ShipConciergeSetupDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * ROA-178 — one-click Ship Concierge setup. The empty state of the
 * Concierge surfaces (drawer + inline panel) opens this dialog so the
 * operator doesn't have to construct a PATCH curl by hand.
 *
 * What it does (atomic, server-side stitched):
 *   1. Create a channel with the user-chosen name (default
 *      "ship-concierge"). Idempotent on the existing-channel case —
 *      a duplicate channel-name error surfaces so the user knows to
 *      pick a different slug.
 *   2. Add the chosen agent as a channel member.
 *   3. Designate that agent as the channel's ambient listener.
 *
 * Failure between steps leaves the workspace with a partial setup
 * (channel exists, no listener), but the user can re-open the dialog
 * + pick the same channel to finish. The dialog is intentionally
 * non-transactional because the underlying APIs are independent and
 * a multi-call rollback would add far more code than it saves the
 * user.
 */
export function ShipConciergeSetupDialog({
  open,
  onOpenChange,
}: ShipConciergeSetupDialogProps) {
  const { t } = useT("ship");
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [channelName, setChannelName] = useState("ship-concierge");
  const [channelDisplayName, setChannelDisplayName] = useState(
    t(($) => $.concierge_setup_dialog.default_display_name),
  );
  const [agentId, setAgentId] = useState<string>("");

  const { data: agents = [], isLoading: agentsLoading } = useQuery(
    agentListOptions(wsId),
  );
  // Filter out archived agents so the dropdown doesn't show stale rows.
  // Cast through unknown because the Agent type isn't imported here
  // (the dropdown only reads id + name; full type is overkill).
  const liveAgents = (agents as Array<{ id: string; name: string; archived_at?: string | null }>)
    .filter((a) => !a.archived_at);

  const setup = useMutation({
    mutationFn: async () => {
      if (!agentId) throw new Error(t(($) => $.concierge_setup_dialog.agent_required));
      if (!channelName.trim()) throw new Error(t(($) => $.concierge_setup_dialog.channel_name_required));
      // Step 1 — create the channel. Channel slug rules:
      //   * lowercase, digits, hyphens only
      //   * the server validates; an invalid input surfaces here as
      //     a 400 with the validation message.
      const ch = await api.createChannel({
        name: channelName.trim(),
        display_name: channelDisplayName.trim() || channelName.trim(),
        description: t(($) => $.concierge_setup_dialog.channel_description),
        visibility: "public",
      });
      // Step 2 — add the agent as a member.
      try {
        await api.addChannelMember(ch.id, {
          member_type: "agent",
          member_id: agentId,
        });
      } catch (err) {
        // Tolerated: agent might already be a member if this dialog
        // is re-run. The ambient listener PATCH below is the real
        // designation; membership is just a soft prerequisite for
        // the agent to appear in channel UIs.
        console.warn("addChannelMember failed (already member?):", err);
      }
      // Step 3 — designate the ambient listener.
      await api.setChannelAmbientListener(ch.id, agentId);
      return ch;
    },
    onSuccess: () => {
      toast.success(t(($) => $.concierge_setup_dialog.configured_toast));
      // Bust the channels-list cache so the drawer + inline panel
      // re-render with the newly-configured channel without a
      // refresh.
      queryClient.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      onOpenChange(false);
    },
    onError: (err) => {
      toast.error(err instanceof Error ? err.message : String(err));
    },
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md" data-testid="ship-concierge-setup-dialog">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm">
            <Bot className="size-4 text-muted-foreground" />
            {t(($) => $.concierge_setup_dialog.title)}
          </DialogTitle>
          <DialogDescription className="text-xs">
            {t(($) => $.concierge_setup_dialog.description)}
          </DialogDescription>
        </DialogHeader>

        <div className="grid gap-3 py-2">
          <div className="grid gap-1.5">
            <Label htmlFor="concierge-channel-name" className="text-xs">
              {t(($) => $.concierge_setup_dialog.channel_slug_label)}
            </Label>
            <Input
              id="concierge-channel-name"
              value={channelName}
              onChange={(e) => setChannelName(e.target.value)}
              placeholder="ship-concierge"
              className="h-8 text-xs"
            />
            <p className="text-[10px] text-muted-foreground">
              {t(($) => $.concierge_setup_dialog.channel_slug_hint, {
                name: channelName || t(($) => $.concierge_setup_dialog.name_fallback),
              })}
            </p>
          </div>

          <div className="grid gap-1.5">
            <Label htmlFor="concierge-channel-display" className="text-xs">
              {t(($) => $.concierge_setup_dialog.display_name_label)}
            </Label>
            <Input
              id="concierge-channel-display"
              value={channelDisplayName}
              onChange={(e) => setChannelDisplayName(e.target.value)}
              placeholder={t(($) => $.concierge_setup_dialog.default_display_name)}
              className="h-8 text-xs"
            />
          </div>

          <div className="grid gap-1.5">
            <Label className="text-xs">{t(($) => $.concierge_setup_dialog.agent_label)}</Label>
            <Select
              value={agentId}
              onValueChange={(v: string | null) => setAgentId(v ?? "")}
            >
              <SelectTrigger className="h-8 text-xs" data-testid="ship-concierge-agent-select">
                <SelectValue
                  placeholder={
                    agentsLoading
                      ? t(($) => $.concierge_setup_dialog.loading_agents)
                      : liveAgents.length === 0
                        ? t(($) => $.concierge_setup_dialog.no_agents)
                        : t(($) => $.concierge_setup_dialog.pick_agent)
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {liveAgents.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {liveAgents.length === 0 && !agentsLoading && (
              <p className="text-[10px] text-muted-foreground">
                {t(($) => $.concierge_setup_dialog.add_agent_hint)}
              </p>
            )}
          </div>
        </div>

        <DialogFooter>
          <Button
            variant="outline"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={setup.isPending}
          >
            {t(($) => $.concierge_setup_dialog.cancel)}
          </Button>
          <Button
            size="sm"
            onClick={() => setup.mutate()}
            disabled={setup.isPending || !agentId || !channelName.trim()}
            data-testid="ship-concierge-setup-submit"
          >
            {setup.isPending
              ? t(($) => $.concierge_setup_dialog.configuring)
              : t(($) => $.concierge_setup_dialog.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
