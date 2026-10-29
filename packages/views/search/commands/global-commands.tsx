"use client";

import { useMemo } from "react";
import { toast } from "sonner";
import {
  Archive,
  CheckCheck,
  FolderPlus,
  MessageCircleQuestion,
  MessageSquare,
  Palette,
  SquarePen,
  Zap,
} from "@multica/ui/icons";
import { useChatStore } from "@multica/core/chat";
import type { CommandBarContext } from "@multica/core/command-bar";
import { useArchiveAllReadInbox, useMarkAllInboxRead } from "@multica/core/inbox/mutations";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores";
import { useModalStore } from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import { useShortcut } from "@multica/core/shortcuts";
import { isFloatingChatRouteSuppressed } from "../../chat/floating-chat-visibility";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { usePalette } from "../palette-context";
import type { PaletteCommand } from "../types";

/**
 * Commands that exist everywhere. `primary` ones form the empty state's New
 * group; the rest surface once the user types.
 */
export function useGlobalCommands(contexts: readonly CommandBarContext[]): PaletteCommand[] {
  const { t } = useT("search");
  const { t: tInbox } = useT("inbox");
  const palette = usePalette();
  const p = useWorkspacePaths();
  const navigation = useNavigation();
  const createIssueShortcut = useShortcut("createIssue");
  const toggleChatShortcut = useShortcut("toggleChat");
  const markAllRead = useMarkAllInboxRead();
  const archiveAllRead = useArchiveAllReadInbox();

  const projectContext = contexts.find((c) => c.kind === "project");
  const projectId = projectContext?.kind === "project" ? projectContext.project.id : null;
  const inboxContext = contexts.find((c) => c.kind === "inbox");
  const inbox = inboxContext?.kind === "inbox" ? inboxContext : null;
  const { pathname } = navigation;

  return useMemo<PaletteCommand[]>(() => {
    const chatPath = p.chat();
    return [
      {
        id: "new-issue",
        label: t(($) => $.commands.new_issue),
        icon: SquarePen,
        keywords: ["new", "issue", "create", "add", "新建", "任务"],
        shortcut: createIssueShortcut,
        run: () => openCreateIssueWithPreference(projectId ? { project_id: projectId } : undefined),
        primary: true,
      },
      {
        id: "chat-with-agent",
        label: t(($) => $.commands.chat_with_agent),
        icon: MessageSquare,
        keywords: ["chat", "agent", "talk", "ask", "对话", "聊天", "智能体"],
        shortcut: toggleChatShortcut,
        run: () => {
          // ⌘J opens the floating chat where it is available; elsewhere (the
          // Chat page itself, or with the overlay turned off) go to Chat.
          const chat = useChatStore.getState();
          if (chat.floatingChatEnabled && !isFloatingChatRouteSuppressed(pathname, chatPath)) {
            chat.setOpen(true);
          } else {
            navigation.push(chatPath);
          }
        },
        primary: true,
      },
      {
        id: "new-project",
        label: t(($) => $.commands.new_project),
        icon: FolderPlus,
        keywords: ["new", "project", "create", "add", "新建", "项目"],
        run: () => useModalStore.getState().open("create-project"),
      },
      {
        id: "new-autopilot",
        label: t(($) => $.commands.new_autopilot),
        icon: Zap,
        keywords: ["new", "autopilot", "automation", "schedule", "create", "新建", "自动化"],
        run: () => palette.openHostDialog({ type: "new-autopilot" }),
      },
      {
        id: "inbox-mark-all-read",
        label: t(($) => $.inbox.mark_all_read),
        icon: CheckCheck,
        keywords: ["inbox", "read", "all", "收件箱", "已读"],
        run: () => {
          if (inbox) {
            inbox.markAllRead();
            return;
          }
          markAllRead.mutateAsync(undefined).catch((err: unknown) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : tInbox(($) => $.errors.mark_all_read_failed),
            ),
          );
        },
      },
      {
        id: "inbox-archive-all-read",
        label: t(($) => $.inbox.archive_all_read),
        icon: Archive,
        keywords: ["inbox", "archive", "read", "收件箱", "归档"],
        run: () => {
          if (inbox) {
            inbox.archiveAllRead();
            return;
          }
          archiveAllRead.mutateAsync(undefined).catch((err: unknown) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : tInbox(($) => $.errors.archive_all_read_failed),
            ),
          );
        },
      },
      {
        id: "theme",
        label: t(($) => $.commands.change_theme),
        icon: Palette,
        keywords: ["theme", "appearance", "dark", "light", "system", "mode", "主题", "深色", "浅色"],
        page: { type: "theme" },
      },
      {
        id: "feedback",
        label: t(($) => $.commands.send_feedback),
        icon: MessageCircleQuestion,
        keywords: ["feedback", "bug", "report", "support", "反馈"],
        run: () => useModalStore.getState().open("feedback"),
      },
    ];
  }, [
    archiveAllRead,
    createIssueShortcut,
    inbox,
    markAllRead,
    navigation,
    p,
    palette,
    pathname,
    projectId,
    t,
    tInbox,
    toggleChatShortcut,
  ]);
}
