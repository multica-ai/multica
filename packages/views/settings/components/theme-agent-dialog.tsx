"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  BUILT_IN_THEMES,
  themeAgentPrompt,
  themeIdForDescription,
  useInstalledThemesStore,
  useThemeFolderStatus,
} from "@multica/core/appearance";
import { api, dispatchReasonCode } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useChatStore } from "@multica/core/chat";
import { upsertChatMessageToCaches } from "@multica/core/chat/message-cache";
import { chatKeys } from "@multica/core/chat/queries";
import { refetchChatSessionsAfterCommit } from "@multica/core/chat/session-list";
import { useCreateChatSession } from "@multica/core/chat/mutations";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { ActorAvatar } from "../../common/actor-avatar";
import { seedAcceptedPendingTask } from "../../chat/components/use-chat-controller";
import { canAssignAgent } from "../../issues/components/pickers/assignee-picker";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

/**
 * Ask an agent to build a theme: a description and an agent, sent as a new
 * chat whose first message carries the theme contract (themeAgentPrompt).
 * On Desktop the agent is pointed at the themes folder, and the theme is
 * switched to once it lands there and passes the checks.
 */
export function ThemeAgentDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [sending, setSending] = useState(false);
  return (
    <Dialog open={open} onOpenChange={(next) => !sending && onOpenChange(next)}>
      <DialogContent className="sm:max-w-lg" showCloseButton={!sending}>
        {open ? (
          <ThemeAgentForm
            sending={sending}
            setSending={setSending}
            close={() => onOpenChange(false)}
          />
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function ThemeAgentForm({
  sending,
  setSending,
  close,
}: {
  sending: boolean;
  setSending: (sending: boolean) => void;
  close: () => void;
}) {
  const { t } = useT("settings");
  const { t: tChat } = useT("chat");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const userId = useAuthStore((s) => s.user?.id);
  const lastAgentId = useChatStore((s) => s.selectedAgentId);
  const folder = useThemeFolderStatus((s) => s.path);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const createSession = useCreateChatSession();
  const [description, setDescription] = useState("");
  const [agentId, setAgentId] = useState<string | null>(null);

  const role = members.find((m) => m.user_id === userId)?.role;
  const available = agents.filter(
    (a) => !a.archived_at && canAssignAgent(a, userId, role),
  );
  const agent =
    available.find((a) => a.id === agentId) ??
    available.find((a) => a.id === lastAgentId) ??
    available[0];
  const ready = !!agent && description.trim().length > 0 && !sending;

  const submit = async () => {
    if (!agent || !ready) return;
    setSending(true);
    const taken = [
      ...BUILT_IN_THEMES,
      ...useInstalledThemesStore.getState().themes,
    ].map((theme) => theme.id);
    const id = themeIdForDescription(description, taken);
    const content = themeAgentPrompt({ description, id, folder });
    try {
      const session = await createSession.mutateAsync({
        agent_id: agent.id,
        title: t(($) => $.appearance.agent.chat_title, { id }),
      });
      const result = await api.sendChatMessage(session.id, content);
      upsertChatMessageToCaches(
        qc,
        session.id,
        {
          id: result.message_id,
          chat_session_id: session.id,
          role: "user",
          content,
          task_id: result.task_id,
          created_at: result.created_at,
        },
        { seedIfMissing: true },
      );
      seedAcceptedPendingTask(qc, session.id, {
        task_id: result.task_id,
        created_at: result.created_at,
        message_id: result.message_id,
        content,
        supports_queue: result.supports_queue,
        queued: result.queued,
      });
      void refetchChatSessionsAfterCommit(qc, chatKeys.sessions(wsId));
      if (folder) useThemeFolderStatus.setState({ awaiting: id });
      close();
      push(paths.chatSession(session.id));
    } catch (err) {
      const reason = dispatchReasonCode(err);
      toast.error(
        reason === "invocation_not_allowed"
          ? tChat(($) => $.input.send_blocked_toast)
          : reason === "agent_runtime_required"
            ? tChat(($) => $.input.runtime_required_toast)
            : reason === "runtime_access_denied"
              ? tChat(($) => $.input.runtime_access_denied_toast)
              : tChat(($) => $.input.send_failed_toast),
      );
    } finally {
      setSending(false);
    }
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t(($) => $.appearance.agent.title)}</DialogTitle>
        <DialogDescription>
          {folder
            ? t(($) => $.appearance.agent.description_desktop)
            : t(($) => $.appearance.agent.description_web)}
        </DialogDescription>
      </DialogHeader>
      <form
        className="space-y-4"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <div className="space-y-1.5">
          <Label htmlFor="theme-agent-description">
            {t(($) => $.appearance.agent.prompt_label)}
          </Label>
          <Textarea
            id="theme-agent-description"
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                event.preventDefault();
                void submit();
              }
            }}
            placeholder={t(($) => $.appearance.agent.prompt_placeholder)}
            className="min-h-24"
            maxLength={2000}
            autoFocus
          />
        </div>
        <div className="space-y-1.5">
          <Label htmlFor="theme-agent-agent">
            {t(($) => $.appearance.agent.agent_label)}
          </Label>
          {agent ? (
            <Select
              items={available.map((a) => ({ value: a.id, label: a.name }))}
              value={agent.id}
              onValueChange={(next) => {
                if (next) setAgentId(next);
              }}
            >
              <SelectTrigger id="theme-agent-agent" className="w-full">
                <SelectValue>
                  <span className="flex min-w-0 items-center gap-2">
                    <ActorAvatar
                      actorType="agent"
                      actorId={agent.id}
                      size="xs"
                    />
                    <span className="truncate">{agent.name}</span>
                  </span>
                </SelectValue>
              </SelectTrigger>
              <SelectContent>
                {available.map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    <span className="flex min-w-0 items-center gap-2">
                      <ActorAvatar actorType="agent" actorId={a.id} size="xs" />
                      <span className="truncate">{a.name}</span>
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          ) : (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.appearance.agent.no_agents)}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            disabled={sending}
            onClick={close}
          >
            {t(($) => $.appearance.import.cancel)}
          </Button>
          <Button type="submit" disabled={!ready}>
            {sending
              ? t(($) => $.appearance.agent.sending)
              : t(($) => $.appearance.agent.submit)}
          </Button>
        </DialogFooter>
      </form>
    </>
  );
}
