"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Archive,
  CircleDashed,
  FolderInput,
  Link2,
  MailOpen,
  MessageSquare,
  Pause,
  Pin,
  PinOff,
  Play,
  SquarePen,
  UserPlus,
} from "@multica/ui/icons";
import { isAgentRuntimeBound } from "@multica/core/agents";
import { useAuthStore } from "@multica/core/auth";
import type { CommandBarContext } from "@multica/core/command-bar";
import { useUpdateAutopilot } from "@multica/core/autopilots/mutations";
import { usePinChatAgent, useSetChatSessionPinned, useUnpinChatAgent } from "@multica/core/chat/mutations";
import { chatPinnedAgentsOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { canAssignAgentToIssue, useCurrentMember } from "@multica/core/permissions";
import { pinListOptions, useCreatePin, useDeletePin } from "@multica/core/pins";
import { getShortcut } from "@multica/core/shortcuts";
import { copyText } from "@multica/ui/lib/clipboard";
import { useRunAutopilotNow } from "../../autopilots/components/use-run-autopilot-now";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import type { PaletteCommand, PaletteCommandGroup } from "../types";
import { ContextHeading } from "./context-heading";

type ContextOf<K extends CommandBarContext["kind"]> = Extract<CommandBarContext, { kind: K }>;

export function useProjectCommandGroup(
  context: ContextOf<"project"> | null,
): PaletteCommandGroup | null {
  const { t } = useT("search");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { getShareableUrl } = useNavigation();
  const userId = useAuthStore((s) => s.user?.id ?? "");
  const { data: pins = [] } = useQuery({
    ...pinListOptions(wsId, userId),
    enabled: !!context && !!userId,
  });
  const createPin = useCreatePin();
  const deletePin = useDeletePin();

  return useMemo(() => {
    if (!context) return null;
    const { project } = context;
    const pinned = pins.some((pin) => pin.item_type === "project" && pin.item_id === project.id);
    const commands: PaletteCommand[] = [
      {
        id: "project-status",
        label: t(($) => $.project.change_status),
        icon: CircleDashed,
        keywords: ["status", "state", "状态"],
        page: { type: "project-status", project },
        crumb: project.title,
        primary: true,
      },
      {
        id: "project-new-issue",
        label: t(($) => $.project.new_issue),
        icon: SquarePen,
        keywords: ["new", "issue", "create", "新建", "任务"],
        run: () => openCreateIssueWithPreference({ project_id: project.id }),
        primary: true,
      },
      {
        id: "project-pin",
        label: pinned ? t(($) => $.project.unpin) : t(($) => $.project.pin),
        icon: pinned ? PinOff : Pin,
        keywords: ["pin", "sidebar", "固定"],
        run: () => {
          if (pinned) deletePin.mutate({ itemType: "project", itemId: project.id });
          else createPin.mutate({ item_type: "project", item_id: project.id });
        },
      },
      {
        id: "project-copy-link",
        label: t(($) => $.project.copy_link),
        icon: Link2,
        keywords: ["copy", "link", "share", "url", "复制", "链接"],
        run: () => {
          void copyText(getShareableUrl(p.projectDetail(project.id))).then((ok) => {
            if (ok) toast.success(t(($) => $.toast.link_copied));
          });
        },
        primary: true,
      },
    ];
    return {
      id: "context-project",
      heading: <ContextHeading label={t(($) => $.context.current_project)} subject={project.title} />,
      commands,
    };
  }, [context, createPin, deletePin, getShareableUrl, p, pins, t]);
}

export function useAgentCommandGroup(context: ContextOf<"agent"> | null): PaletteCommandGroup | null {
  const { t } = useT("search");
  const { t: tAgents } = useT("agents");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const navigation = useNavigation();
  const { userId, role } = useCurrentMember(wsId);
  const { data: pinnedAgents = [] } = useQuery({
    ...chatPinnedAgentsOptions(wsId),
    enabled: !!context,
  });
  const pinAgent = usePinChatAgent();
  const unpinAgent = useUnpinChatAgent();

  return useMemo(() => {
    if (!context) return null;
    const { agent } = context;
    if (agent.archived_at) return null;
    const allowed = canAssignAgentToIssue(agent, { userId, role }).allowed;
    const runtimeBound = isAgentRuntimeBound(agent);
    // Same gate as the agent page's own buttons: say why instead of opening
    // a chat or a form that cannot start a run.
    const guard = (proceed: () => void) => () => {
      if (!allowed) {
        toast.error(tAgents(($) => $.detail.dm_no_permission_toast));
        return;
      }
      if (!runtimeBound) {
        toast.error(tAgents(($) => $.detail.runtime_required_toast));
        return;
      }
      proceed();
    };
    const pinned = pinnedAgents.some((pin) => pin.agent_id === agent.id);
    const commands: PaletteCommand[] = [
      {
        id: "agent-chat",
        label: t(($) => $.agent.chat, { name: agent.name }),
        icon: MessageSquare,
        keywords: ["chat", "dm", "message", "talk", "对话", "聊天"],
        run: guard(() => navigation.push(p.chatWithAgent(agent.id))),
        primary: true,
      },
      {
        id: "agent-assign-issue",
        label: t(($) => $.agent.assign_issue, { name: agent.name }),
        icon: UserPlus,
        keywords: ["assign", "issue", "指派", "任务"],
        page: allowed && runtimeBound ? { type: "agent-assign-issue", agent } : undefined,
        crumb: agent.name,
        run: guard(() => {}),
        primary: true,
      },
      {
        id: "agent-new-issue",
        label: t(($) => $.agent.new_issue, { name: agent.name }),
        icon: SquarePen,
        keywords: ["new", "issue", "create", "新建", "任务"],
        run: guard(() =>
          useModalStore.getState().open("quick-create-issue", { agent_id: agent.id }),
        ),
      },
      {
        id: "agent-pin-chat",
        label: pinned ? t(($) => $.agent.unpin_in_chat) : t(($) => $.agent.pin_in_chat),
        icon: pinned ? PinOff : Pin,
        keywords: ["pin", "chat", "固定", "聊天"],
        run: () => {
          if (pinned) unpinAgent.mutate(agent.id);
          else pinAgent.mutate(agent.id);
        },
      },
    ];
    return {
      id: "context-agent",
      heading: <ContextHeading label={t(($) => $.context.current_agent)} subject={agent.name} />,
      commands,
    };
  }, [context, navigation, p, pinAgent, pinnedAgents, role, t, tAgents, unpinAgent, userId]);
}

export function useAutopilotCommandGroup(
  context: ContextOf<"autopilot"> | null,
): PaletteCommandGroup | null {
  const { t } = useT("search");
  const { runNow } = useRunAutopilotNow();
  const updateAutopilot = useUpdateAutopilot();

  return useMemo(() => {
    if (!context) return null;
    const { autopilot } = context;
    if (autopilot.status === "archived" || autopilot.can_write === false) return null;
    const active = autopilot.status === "active";
    const commands: PaletteCommand[] = [];
    if (active) {
      commands.push({
        id: "autopilot-run-now",
        label: t(($) => $.autopilot.run_now),
        icon: Play,
        keywords: ["run", "trigger", "now", "运行", "触发"],
        run: () => void runNow(autopilot.id),
        primary: true,
      });
    }
    commands.push({
      id: "autopilot-toggle",
      label: active ? t(($) => $.autopilot.pause) : t(($) => $.autopilot.resume),
      icon: active ? Pause : Play,
      keywords: ["pause", "resume", "enable", "disable", "暂停", "启用"],
      run: () =>
        updateAutopilot.mutate({ id: autopilot.id, status: active ? "paused" : "active" }),
      primary: true,
    });
    return {
      id: "context-autopilot",
      heading: (
        <ContextHeading label={t(($) => $.context.current_autopilot)} subject={autopilot.title} />
      ),
      commands,
    };
  }, [context, runNow, t, updateAutopilot]);
}

export function useInboxCommandGroup(context: ContextOf<"inbox"> | null): PaletteCommandGroup | null {
  const { t } = useT("search");
  return useMemo(() => {
    if (!context?.item) return null;
    const { item } = context;
    const commands: PaletteCommand[] = [
      {
        id: "inbox-archive",
        label: t(($) => $.inbox.archive),
        icon: Archive,
        keywords: ["archive", "done", "归档"],
        shortcut: getShortcut("archiveInboxItem"),
        run: () => context.archive(item.id),
        primary: true,
      },
    ];
    if (item.read) {
      commands.push({
        id: "inbox-mark-unread",
        label: t(($) => $.inbox.mark_unread),
        icon: MailOpen,
        keywords: ["unread", "未读"],
        run: () => context.markUnread(item.id),
        primary: true,
      });
    }
    return {
      id: "context-inbox",
      heading: <ContextHeading label={t(($) => $.context.inbox)} detail={item.title} />,
      commands,
    };
  }, [context, t]);
}

export function useChatCommandGroup(context: ContextOf<"chat"> | null): PaletteCommandGroup | null {
  const { t } = useT("search");
  const setPinned = useSetChatSessionPinned();
  return useMemo(() => {
    if (!context) return null;
    const { session } = context;
    if (session.status === "archived") return null;
    const pinned = !!session.pinned;
    const commands: PaletteCommand[] = [
      {
        id: "chat-link-project",
        label: t(($) => $.chat.link_project),
        icon: FolderInput,
        keywords: ["project", "context", "项目", "关联"],
        page: { type: "chat-project", session, changeProject: context.changeProject },
        crumb: session.title,
        primary: true,
      },
      {
        id: "chat-pin",
        label: pinned ? t(($) => $.chat.unpin) : t(($) => $.chat.pin),
        icon: pinned ? PinOff : Pin,
        keywords: ["pin", "固定"],
        run: () => setPinned.mutate({ sessionId: session.id, pinned: !pinned }),
        primary: true,
      },
      {
        id: "chat-archive",
        label: t(($) => $.chat.archive),
        icon: Archive,
        keywords: ["archive", "归档"],
        run: () => context.archive(session),
        primary: true,
      },
    ];
    return {
      id: "context-chat",
      heading: <ContextHeading label={t(($) => $.context.current_chat)} detail={session.title} />,
      commands,
    };
  }, [context, setPinned, t]);
}
