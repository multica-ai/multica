"use client";

import { useState } from "react";
import { toast } from "sonner";
import { AGENT_DESCRIPTION_MAX_LENGTH } from "@multica/core/agents";
import {
  useCreateGlobalAgent,
  useUpdateGlobalAgent,
} from "@multica/core/global-agents";
import type {
  AgentConversationStarter,
  CreateGlobalAgentRequest,
  GlobalAgent,
  UpdateGlobalAgentRequest,
} from "@multica/core/types";
import { isImeComposing } from "@multica/core/utils";
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
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import { CharCounter } from "../../agents/components/char-counter";
import { ConversationStartersEditor } from "../../agents/components/conversation-starters-editor";
import { useT } from "../../i18n";

function sameStarters(
  a: AgentConversationStarter[],
  b: AgentConversationStarter[],
): boolean {
  return (
    a.length === b.length &&
    a.every(
      (item, index) =>
        item.label === b[index]?.label && item.prompt === b[index]?.prompt,
    )
  );
}

/**
 * Create or edit a global agent. Only the synced identity fields live here —
 * runtime, model, skills and access are chosen per workspace when the agent
 * is enabled there. Instructions are a plain textarea on purpose: the rich
 * editor resolves mentions and uploads against the current workspace, which
 * would not mean the same thing in the other workspaces this text reaches.
 */
export function GlobalAgentFormDialog({
  agent,
  onClose,
}: {
  /** The agent to edit; omit to create a new one. */
  agent?: GlobalAgent;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const createMutation = useCreateGlobalAgent();
  const updateMutation = useUpdateGlobalAgent();
  const isEdit = !!agent;

  const [name, setName] = useState(agent?.name ?? "");
  const [description, setDescription] = useState(agent?.description ?? "");
  const [instructions, setInstructions] = useState(agent?.instructions ?? "");
  const [avatarUrl, setAvatarUrl] = useState<string | null>(
    agent?.avatar_url ?? null,
  );
  const [starters, setStarters] = useState<AgentConversationStarter[]>(
    agent?.conversation_starters ?? [],
  );

  const pending = createMutation.isPending || updateMutation.isPending;
  const descriptionTooLong =
    [...description].length > AGENT_DESCRIPTION_MAX_LENGTH;
  const startersIncomplete = starters.some(
    (item) => !item.label.trim() || !item.prompt.trim(),
  );
  const canSubmit =
    !pending && !!name.trim() && !descriptionTooLong && !startersIncomplete;

  const handleSubmit = async () => {
    if (!canSubmit) return;
    const trimmedName = name.trim();
    try {
      if (agent) {
        // Send only what changed: every synced field fans out to all linked
        // workspace agents, so an untouched name must not re-run the
        // cross-workspace name checks.
        const patch: UpdateGlobalAgentRequest = {};
        if (trimmedName !== agent.name) patch.name = trimmedName;
        if (description !== agent.description) patch.description = description;
        if (instructions !== agent.instructions) patch.instructions = instructions;
        if (avatarUrl && avatarUrl !== agent.avatar_url) patch.avatar_url = avatarUrl;
        if (!sameStarters(starters, agent.conversation_starters)) {
          patch.conversation_starters = starters;
        }
        if (Object.keys(patch).length > 0) {
          await updateMutation.mutateAsync({ id: agent.id, ...patch });
        }
        toast.success(t(($) => $.global_agents.form.saved_toast));
      } else {
        const data: CreateGlobalAgentRequest = {
          name: trimmedName,
          description,
          instructions,
        };
        if (avatarUrl) data.avatar_url = avatarUrl;
        if (starters.length > 0) data.conversation_starters = starters;
        await createMutation.mutateAsync(data);
        toast.success(t(($) => $.global_agents.form.created_toast));
      }
      onClose();
    } catch (err) {
      toast.error(
        err instanceof Error
          ? err.message
          : isEdit
            ? t(($) => $.global_agents.form.save_failed_toast)
            : t(($) => $.global_agents.form.create_failed_toast),
      );
    }
  };

  const syncNote =
    isEdit && (agent?.links.length ?? 0) > 0
      ? t(($) => $.global_agents.form.sync_note)
      : null;

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <DialogContent className="flex max-h-[85vh] flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl">
        <DialogHeader className="border-b px-5 py-3">
          <DialogTitle>
            {isEdit
              ? t(($) => $.global_agents.form.edit_title)
              : t(($) => $.global_agents.form.create_title)}
          </DialogTitle>
          {syncNote ? (
            <DialogDescription className="text-caption">
              {syncNote}
            </DialogDescription>
          ) : null}
        </DialogHeader>

        <div className="min-h-0 flex-1 space-y-5 overflow-y-auto p-5">
          <div className="flex items-start gap-4">
            <AvatarUploadControl
              variant="agent"
              value={avatarUrl}
              name={name}
              size={64}
              ariaLabel={t(($) => $.global_agents.form.avatar_aria)}
              onUploaded={setAvatarUrl}
              onEmojiSelected={setAvatarUrl}
              onClear={isEdit ? undefined : () => setAvatarUrl(null)}
            />
            <div className="min-w-0 flex-1 space-y-3">
              <div>
                <Label
                  htmlFor="global-agent-name"
                  className="text-caption text-muted-foreground"
                >
                  {t(($) => $.global_agents.form.name_label)}
                </Label>
                <Input
                  id="global-agent-name"
                  autoFocus
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={t(($) => $.global_agents.form.name_placeholder)}
                  className="mt-1"
                  onKeyDown={(e) => {
                    if (isImeComposing(e)) return;
                    if (e.key === "Enter") void handleSubmit();
                  }}
                />
              </div>
              <div>
                <Label
                  htmlFor="global-agent-description"
                  className="text-caption text-muted-foreground"
                >
                  {t(($) => $.global_agents.form.description_label)}
                </Label>
                <Input
                  id="global-agent-description"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder={t(
                    ($) => $.global_agents.form.description_placeholder,
                  )}
                  maxLength={AGENT_DESCRIPTION_MAX_LENGTH}
                  className="mt-1"
                />
                <div className="mt-1">
                  <CharCounter
                    length={[...description].length}
                    max={AGENT_DESCRIPTION_MAX_LENGTH}
                  />
                </div>
              </div>
            </div>
          </div>

          <div>
            <Label
              htmlFor="global-agent-instructions"
              className="text-caption text-muted-foreground"
            >
              {t(($) => $.global_agents.form.instructions_label)}
            </Label>
            <Textarea
              id="global-agent-instructions"
              value={instructions}
              onChange={(e) => setInstructions(e.target.value)}
              placeholder={t(
                ($) => $.global_agents.form.instructions_placeholder,
              )}
              rows={8}
              className="mt-1 resize-y"
            />
          </div>

          <ConversationStartersEditor
            value={starters}
            onChange={setStarters}
            disabled={pending}
          />
        </div>

        <DialogFooter className="m-0 px-5 py-3">
          <Button variant="outline" onClick={onClose} disabled={pending}>
            {t(($) => $.global_agents.form.cancel)}
          </Button>
          <Button
            onClick={() => void handleSubmit()}
            disabled={!canSubmit}
            aria-busy={pending || undefined}
          >
            {isEdit
              ? pending
                ? t(($) => $.global_agents.form.saving)
                : t(($) => $.global_agents.form.save)
              : pending
                ? t(($) => $.global_agents.form.creating)
                : t(($) => $.global_agents.form.create)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
