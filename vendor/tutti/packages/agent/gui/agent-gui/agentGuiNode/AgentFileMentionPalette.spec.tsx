import { fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { setAgentGuiI18nTestLocale } from "../../i18n/testUtils";
import { AgentFileMentionPalette } from "./AgentFileMentionPalette";
import type { AgentMentionSearchState } from "./AgentMentionSearchController";
import type { AgentContextMentionItem } from "./agentRichText/agentFileMentionExtension";

vi.mock("../../i18n/index", async () => {
  const actual =
    await vi.importActual<typeof import("../../i18n/index")>(
      "../../i18n/index"
    );
  const labels: Record<string, string> = {
    "agentHost.agentGui.mentionFilterApp": "Apps",
    "agentHost.agentGui.mentionFilterFile": "Files",
    "agentHost.agentGui.mentionFilterSession": "Sessions",
    "agentHost.agentGui.mentionGroupApps": "Apps",
    "agentHost.agentGui.mentionEmptyMySessions": "暂无会话",
    "agentHost.agentGui.mentionGroupIssues": "任务",
    "agentHost.agentGui.mentionEmptyIssues": "暂无任务",
    "agentHost.agentGui.mentionFilterIssue": "任务",
    "agentHost.agentGui.fileMentionEnterFolder": "进入文件夹",
    "agentHost.agentGui.fileMentionSwitchCategory": "切换分类",
    "agentHost.agentGui.fileMentionNavigateHierarchy": "进入/返回文件夹",
    "agentHost.agentGui.fileMentionSwitchSelection": "切换选中",
    "agentHost.agentGui.contextPickerBrowseFileHint":
      "暂无已打开或 Agent 生成的文件，继续输入文件名可搜索本机文件",
    "agentHost.agentGui.contextPickerBrowseSessionHint":
      "输入内容以搜索我发起的 Agent 会话",
    "agentHost.agentGui.contextPickerLoadMoreLoading": "正在加载",
    "agentHost.agentGui.contextPickerLoadMoreRetry": "加载失败，重试",
    "agentHost.agentGui.mentionGroupOpenedFiles": "我打开的文件",
    "agentHost.agentGui.mentionGroupAgentGeneratedFiles":
      "近期 Agent 生成的文件",
    "agentHost.agentGui.mentionFolderBack": "返回",
    "agentHost.agentGui.mentionNoMatchingFiles": "没有匹配到文件",
    "agentHost.roomIssueNode.issueStatusNotStarted": "待开始",
    "agentHost.roomIssueNode.issueStatusRunning": "执行中",
    "agentHost.roomIssueNode.issueStatusInProgress": "执行中",
    "agentHost.roomIssueNode.issueStatusPendingAcceptance": "待验收",
    "agentHost.roomIssueNode.issueStatusCompleted": "已完成",
    "agentHost.roomIssueNode.issueStatusFailed": "失败",
    "agentHost.roomIssueNode.issueStatusCanceled": "已取消",
    "agentHost.workspaceAgentActivityStatusWorking": "运行中",
    "agentHost.workspaceAgentActivityStatusWaiting": "等待中",
    "agentHost.workspaceAgentActivityStatusIdle": "已完成",
    "agentHost.workspaceAgentActivityStatusEnd": "已完成",
    "agentHost.workspaceAgentActivityStatusCompleted": "已完成",
    "agentHost.workspaceAgentActivityStatusCanceled": "已取消",
    "agentHost.workspaceAgentActivityStatusFailed": "错误",
    "agentHost.agentGui.mentionAgentTargetAvailable": "可用",
    "agentHost.agentGui.mentionAgentTargetUnavailable": "不可用"
  };

  return {
    ...actual,
    translate: (key: string, options?: { count?: number }) => {
      if (key === "agentHost.agentGui.contextPickerExpandMore") {
        return `展开更多 ${options?.count ?? 0} 条`;
      }
      return labels[key] ?? key;
    }
  };
});

describe("AgentFileMentionPalette", () => {
  it("shows a file's relative path and workspace after its name", () => {
    const file = {
      kind: "file" as const,
      href: "/Users/test/project/tutti/src/index.ts",
      path: "/Users/test/project/tutti/src/index.ts",
      name: "index.ts",
      entryKind: "file" as const,
      directoryPath: "/Users/test/project/tutti/src",
      contextLabel: "project/tutti/src · Tutti"
    } satisfies AgentContextMentionItem;
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "index",
      mode: "results",
      filter: "file",
      categories: [],
      groups: [
        {
          id: "opened_files",
          items: [file],
          totalCount: 1,
          visibleCount: 1,
          hasMore: false
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey={null}
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    expect(screen.getByText("index.ts")).toBeVisible();
    expect(screen.getByText("project/tutti/src · Tutti")).toBeVisible();
  });

  it("shows unavailable agent targets but prevents selecting them", () => {
    const unavailableAgent = {
      kind: "agent-target" as const,
      href: "mention://agent-target/shared-agent:codex?workspaceId=room-1",
      workspaceId: "room-1",
      targetId: "shared-agent:codex",
      name: "Lin 的 Codex",
      agentProviderId: "codex",
      availabilityStatus: "unavailable"
    } satisfies AgentContextMentionItem;
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "",
      mode: "browse",
      filter: "agent",
      categories: [],
      groups: [
        {
          id: "member:user-lin",
          label: "Lin",
          items: [unavailableAgent],
          totalCount: 1,
          visibleCount: 1,
          hasMore: false
        }
      ],
      error: null
    };
    const onSelectItem = vi.fn();

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey={null}
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={onSelectItem}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    const row = screen.getByRole("option", { name: /Lin 的 Codex/ });
    expect(row).toBeDisabled();
    expect(screen.getByText("不可用")).toBeVisible();
    fireEvent.click(row);
    expect(onSelectItem).not.toHaveBeenCalled();
  });

  it("renders task mentions with Multica status labels and categories", () => {
    setAgentGuiI18nTestLocale("zh-CN");
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "",
      mode: "browse",
      filter: "issue",
      categories: [],
      groups: [
        {
          id: "issues",
          items: [
            {
              kind: "workspace-issue",
              href: "tsh://room/room-1/task/task-1",
              workspaceId: "room-1",
              targetId: "task-1",
              name: "写一个文件",
              title: "写一个文件",
              creatorName: "Alice",
              iconUrl: "tutti-asset://issue/default.png",
              status: "todo"
            },
            {
              kind: "workspace-issue",
              href: "tsh://room/room-1/task/task-2",
              workspaceId: "room-1",
              targetId: "task-2",
              name: "查询天气",
              title: "查询天气",
              creatorName: "Bob",
              status: "in_progress"
            },
            {
              kind: "workspace-issue",
              href: "tsh://room/room-1/task/task-3",
              workspaceId: "room-1",
              targetId: "task-3",
              name: "继续推进",
              title: "继续推进",
              creatorName: "Cathy",
              status: "in_progress"
            },
            {
              kind: "workspace-issue",
              href: "tsh://room/room-1/task/task-4",
              workspaceId: "room-1",
              targetId: "task-4",
              name: "整理输出",
              title: "整理输出",
              creatorName: "Dora",
              status: "in_review"
            },
            {
              kind: "workspace-issue",
              href: "tsh://room/room-1/task/task-5",
              workspaceId: "room-1",
              targetId: "task-5",
              name: "归档结果",
              title: "归档结果",
              creatorName: "Eve",
              status: "done"
            },
            {
              kind: "workspace-issue",
              href: "tsh://room/room-1/task/task-6",
              workspaceId: "room-1",
              targetId: "task-6",
              name: "重试任务",
              title: "重试任务",
              creatorName: "Frank",
              status: "blocked"
            },
            {
              kind: "workspace-issue",
              href: "tsh://room/room-1/task/task-7",
              workspaceId: "room-1",
              targetId: "task-7",
              name: "停止执行",
              title: "停止执行",
              creatorName: "Grace",
              status: "cancelled"
            }
          ],
          totalCount: 7,
          visibleCount: 7,
          hasMore: false
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey="issues:workspace-issue:task-1"
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    expect(screen.getByText("待办")).toBeVisible();
    expect(screen.getAllByText("进行中")).toHaveLength(2);
    expect(screen.getByText("审核中")).toBeVisible();
    expect(screen.getByText("已完成")).toBeVisible();
    expect(screen.getByText("已阻塞")).toBeVisible();
    expect(screen.getByText("已取消")).toBeVisible();
    const statusTags = Array.from(
      document.querySelectorAll(".multica-mention-task-status")
    );
    expect(statusTags.map((tag) => tag.getAttribute("data-category"))).toEqual([
      "unstarted",
      "started",
      "started",
      "started",
      "done",
      "started",
      "closed"
    ]);
    expect(screen.queryByText("空闲")).toBeNull();
    expect(screen.queryByText("已退出")).toBeNull();
  });

  it("renders session mention status tags with activity-core display statuses", () => {
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "",
      mode: "browse",
      filter: "session",
      categories: [],
      groups: [
        {
          id: "my_sessions",
          items: [
            {
              kind: "session",
              href: "tsh://room/room-1/session/session-1",
              workspaceId: "room-1",
              targetId: "session-1",
              name: "Alice & Codex 看看目录",
              title: "看看目录",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "running"
            },
            {
              kind: "session",
              href: "tsh://room/room-1/session/session-2",
              workspaceId: "room-1",
              targetId: "session-2",
              name: "Alice & Codex 等待确认",
              title: "等待确认",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "waiting"
            },
            {
              kind: "session",
              href: "tsh://room/room-1/session/session-3",
              workspaceId: "room-1",
              targetId: "session-3",
              name: "Alice & Codex 已完成当前轮次",
              title: "已完成当前轮次",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "idle"
            },
            {
              kind: "session",
              href: "tsh://room/room-1/session/session-4",
              workspaceId: "room-1",
              targetId: "session-4",
              name: "Alice & Codex 结束会话",
              title: "结束会话",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "completed"
            },
            {
              kind: "session",
              href: "tsh://room/room-1/session/session-5",
              workspaceId: "room-1",
              targetId: "session-5",
              name: "Alice & Codex 准备就绪",
              title: "准备就绪",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "ready"
            },
            {
              kind: "session",
              href: "tsh://room/room-1/session/session-6",
              workspaceId: "room-1",
              targetId: "session-6",
              name: "Alice & Codex 已结束",
              title: "已结束",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "end"
            },
            {
              kind: "session",
              href: "tsh://room/room-1/session/session-7",
              workspaceId: "room-1",
              targetId: "session-7",
              name: "Alice & Codex 已完成",
              title: "已完成",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "done"
            },
            {
              kind: "session",
              href: "tsh://room/room-1/session/session-8",
              workspaceId: "room-1",
              targetId: "session-8",
              name: "Alice & Codex 未知状态",
              title: "未知状态",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "unknown"
            },
            {
              kind: "session",
              href: "tsh://room/room-1/session/session-9",
              workspaceId: "room-1",
              targetId: "session-9",
              name: "Alice & Codex 执行失败",
              title: "执行失败",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "failed"
            }
          ],
          totalCount: 9,
          visibleCount: 9,
          hasMore: false
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey="my_sessions:session:session-1"
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    expect(screen.getByText("运行中")).toBeVisible();
    expect(screen.getByText("等待中")).toBeVisible();
    expect(screen.getAllByText("错误")).toHaveLength(1);
    const statusTags = Array.from(
      document.querySelectorAll('[data-agent-mention-status-tag="true"]')
    );
    expect(statusTags.map((tag) => tag.textContent)).toEqual([
      "运行中",
      "等待中",
      "已完成",
      "已完成",
      "已完成",
      "已完成",
      "已完成",
      "已完成",
      "错误"
    ]);
    expect(statusTags.map((tag) => tag.getAttribute("data-status"))).toEqual([
      "working",
      "waiting",
      "idle",
      "completed",
      "idle",
      "idle",
      "completed",
      "idle",
      "failed"
    ]);
    expect(statusTags.map((tag) => tag.getAttribute("data-tone"))).toEqual([
      "blue",
      "amber",
      "green",
      "green",
      "green",
      "green",
      "green",
      "green",
      "red"
    ]);
    const selectedOption = screen.getByRole("option", { selected: true });
    expect(selectedOption).toHaveClass(
      "rich-text-at-mention-palette__row-button"
    );
    expect(selectedOption).toHaveAttribute("data-highlighted");
    const sessionRow = selectedOption.querySelector(
      ".rich-text-at-mention-row--session"
    );
    expect(sessionRow).toHaveClass(
      "rich-text-at-mention-row",
      "rich-text-at-mention-row--session"
    );
    const sessionParticipant = sessionRow?.querySelector(
      ".rich-text-at-mention-row__session-participant"
    );
    expect(sessionParticipant?.parentElement).toHaveClass(
      "rich-text-at-mention-row__entity-text",
      "rich-text-at-mention-row__session-title"
    );
    expect(sessionParticipant).toHaveClass(
      "rich-text-at-mention-row__entity-name",
      "rich-text-at-mention-row__session-participant"
    );
    expect(statusTags[0]).toHaveClass("rich-text-at-mention-status");
    const userAvatarImage = selectedOption.querySelector(
      '[data-agent-mention-user-avatar="true"] img'
    );
    const avatarStack = selectedOption.querySelector(
      ".rich-text-at-mention-avatar-stack"
    );
    const agentAvatar = selectedOption.querySelector(
      '[data-agent-mention-agent-avatar="true"]'
    );
    expect(avatarStack).toHaveClass(
      "rich-text-at-mention-avatar-stack",
      "rich-text-at-mention-avatar-stack--agent-only"
    );
    expect(
      selectedOption.querySelector('[data-agent-mention-user-avatar="true"]')
    ).toBeNull();
    expect(agentAvatar).toHaveClass(
      "rich-text-at-mention-avatar",
      "rich-text-at-mention-avatar--agent"
    );
    expect(userAvatarImage).toBeNull();
    expect(selectedOption).toHaveTextContent("Codex");
    expect(selectedOption).not.toHaveTextContent("Alice & Codex");
  });

  it("omits the grouped initiator while preserving the shared Agent owner and suffix", () => {
    const initiatorLabel = "A session initiator with a very long display name";
    const ownerLabel = "A member with a very long display name";
    const agentLabel = "Codex (Shared)";
    const agentName = `${ownerLabel} · ${agentLabel}`;
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "",
      mode: "browse",
      filter: "session",
      categories: [],
      groups: [
        {
          id: "member:user-1",
          label: initiatorLabel,
          items: [
            {
              kind: "session",
              href: "mention://agent-session/session-1?workspaceId=room-1",
              workspaceId: "room-1",
              targetId: "session-1",
              agentTargetId: "shared-agent:shared-codex",
              name: "Build result",
              title: "Build result",
              scope: "collab_sessions",
              initiatorName: initiatorLabel,
              agentName,
              agentOwnerLabel: ownerLabel,
              agentLabel,
              agentIconUrl: "data:image/png;base64,agent",
              status: "completed"
            }
          ],
          totalCount: 1,
          visibleCount: 1,
          hasMore: false
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey={null}
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    const participantElement = document.querySelector(
      ".rich-text-at-mention-row__session-participant"
    );
    const entities = document.querySelectorAll(
      ".rich-text-at-mention-row__session-participant-entity"
    );
    const segments = document.querySelectorAll(
      ".rich-text-at-mention-row__session-participant-segment"
    );
    const separator = document.querySelector(
      ".rich-text-at-mention-row__session-participant-separator"
    );
    const suffix = document.querySelector(
      ".rich-text-at-mention-row__session-participant-suffix"
    );
    expect(participantElement).toHaveClass(
      "rich-text-at-mention-row__session-participant--structured"
    );
    expect(participantElement).toHaveAttribute("title", agentName);
    expect(entities).toHaveLength(1);
    expect(segments[0]).toHaveTextContent(ownerLabel);
    expect(separator).toBeNull();
    expect(suffix?.textContent).toBe(` · ${agentLabel}`);
    expect(participantElement).not.toHaveTextContent(initiatorLabel);
    expect(screen.getByText("Build result")).toBeVisible();
    expect(screen.getByText("已完成")).toBeVisible();
  });

  it("uses the session provider to resolve agent mention avatars", () => {
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "",
      mode: "browse",
      filter: "session",
      categories: [],
      groups: [
        {
          id: "my_sessions",
          items: [
            {
              kind: "session",
              href: "mention://agent-session/session-1?workspaceId=room-1",
              workspaceId: "room-1",
              targetId: "session-1",
              name: "Alice & Codex 看看目录",
              title: "看看目录",
              scope: "my_sessions",
              initiatorName: "Alice",
              agentName: "Codex",
              status: "working"
            }
          ],
          totalCount: 1,
          visibleCount: 1,
          hasMore: false
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey="my_sessions:session:session-1"
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    const agentAvatarImage = document.querySelector(
      '[data-agent-mention-agent-avatar="true"] img'
    );
    expect(agentAvatarImage).toHaveAttribute(
      "src",
      expect.stringContaining("codex-rounded")
    );
  });

  it("uses the target-projected icon for extension sessions", () => {
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "",
      mode: "browse",
      filter: "session",
      categories: [],
      groups: [
        {
          id: "my_sessions",
          items: [
            {
              kind: "session",
              href: "mention://agent-session/session-1?workspaceId=room-1",
              workspaceId: "room-1",
              targetId: "session-1",
              name: "Gemini CLI hi",
              title: "hi",
              scope: "my_sessions",
              initiatorName: "local",
              agentName: "Gemini CLI",
              agentIconUrl: "data:image/svg+xml;base64,gemini"
            }
          ],
          totalCount: 1,
          visibleCount: 1,
          hasMore: false
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey="my_sessions:session:session-1"
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    expect(
      document.querySelector('[data-agent-mention-agent-avatar="true"] img')
    ).toHaveAttribute("src", "data:image/svg+xml;base64,gemini");
  });

  it("hides file subgroup titles while searching on the file tab", () => {
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "quick",
      mode: "results",
      filter: "file",
      categories: [],
      groups: [
        {
          id: "opened_files",
          items: [
            {
              kind: "file",
              href: "/workspace/quickPhrases.ts",
              path: "/workspace/quickPhrases.ts",
              name: "quickPhrases.ts",
              entryKind: "unknown",
              directoryPath: "/workspace"
            }
          ],
          totalCount: 11,
          visibleCount: 1,
          hasMore: true
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey={null}
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    expect(screen.getByText("quickPhrases.ts")).toBeVisible();
    expect(screen.getByText("展开更多 10 条")).toBeVisible();
    expect(screen.queryByText("我打开的文件")).toBeNull();
    expect(screen.queryByText("近期 Agent 生成的文件")).toBeNull();
    expect(screen.queryByText("没有匹配到文件")).toBeNull();
    expect(screen.queryByTestId("agent-mention-group-divider")).toBeNull();
  });

  it("renders file mention rows with the palette file icon target", () => {
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "report",
      mode: "results",
      filter: "file",
      categories: [],
      groups: [
        {
          id: "opened_files",
          items: [
            {
              kind: "file",
              href: "/workspace/report.docx",
              path: "/workspace/report.docx",
              name: "report.docx",
              entryKind: "file",
              directoryPath: "/workspace",
              score: 99
            }
          ],
          totalCount: 1,
          visibleCount: 1,
          hasMore: false
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey="opened_files:file:/workspace/report.docx"
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    const fileRow = screen
      .getByText("report.docx")
      .closest('[data-agent-file-mention="true"]');

    expect(fileRow).toHaveAttribute("data-agent-file-visual-kind", "document");
    expect(
      fileRow?.querySelector(".agent-gui-node__mention-file-icon")
    ).not.toBeNull();
    expect(
      fileRow?.querySelector('[data-agent-mention-file-thumb="true"]')
    ).toBeNull();
  });

  it("renders agent generated folder back rows with a back navigation marker", () => {
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "",
      mode: "browse",
      filter: "file",
      categories: [],
      groups: [
        {
          id: "agent_generated_files",
          items: [
            {
              kind: "file",
              href: "",
              path: "/workspace/demo/static",
              name: "返回",
              entryKind: "unknown",
              directoryPath: "/workspace/demo",
              mentionNavigation: "agent-generated-folder-back"
            }
          ],
          totalCount: 1,
          visibleCount: 1,
          hasMore: false
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey="agent_generated_files:agent-generated-folder-back:/workspace/demo/static"
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    const backRow = screen
      .getByText("返回")
      .closest('[data-agent-file-mention="true"]');

    expect(backRow).toHaveAttribute(
      "data-agent-mention-navigation",
      "agent-generated-folder-back"
    );
    expect(backRow).toHaveAttribute("data-agent-file-visual-kind", "back");
    const backIcon = backRow?.querySelector(
      ".agent-gui-node__mention-file-icon.rich-text-at-mention-file-icon--glyph"
    );
    expect(backIcon).not.toBeNull();
    expect(backIcon?.querySelector("svg")).not.toBeNull();
  });

  it.each([
    ["agent generated", "agent_generated_files", "agent-generated-folder"],
    ["workspace", "opened_files", "workspace-folder"]
  ] as const)(
    "enters %s folders from the row arrow without selecting the row",
    (_label, groupId, mentionNavigation) => {
      const folderItem = {
        kind: "file" as const,
        href: "",
        path: "/workspace/demo/agentGuiNode",
        name: "agentGuiNode",
        entryKind: "directory",
        directoryPath: "/workspace/demo",
        mentionNavigation,
        childCount: 5
      } satisfies AgentContextMentionItem;
      const state: AgentMentionSearchState = {
        status: "ready",
        query: "",
        mode: "browse",
        filter: "file",
        categories: [],
        groups: [
          {
            id: groupId,
            items: [folderItem],
            totalCount: 1,
            visibleCount: 1,
            hasMore: false
          }
        ],
        error: null
      };
      const onNavigateIntoItem = vi.fn();
      const onSelectItem = vi.fn();

      render(
        <AgentFileMentionPalette
          state={state}
          highlightedKey={`${groupId}:file:/workspace/demo/agentGuiNode`}
          label="mention palette"
          loadingLabel="loading"
          emptyLabel="empty"
          errorLabel="error"
          tabHintLabel="hint"
          maxHeightPx={320}
          onHighlightChange={vi.fn()}
          onSelectItem={onSelectItem}
          onSelectCategory={vi.fn()}
          onSelectFilter={vi.fn()}
          onExpandGroup={vi.fn()}
          onNavigateIntoItem={onNavigateIntoItem}
        />
      );

      const folderRow = screen
        .getByText("agentGuiNode")
        .closest('[data-agent-file-mention="true"]');
      const enterButton = screen.getByRole("button", { name: "进入文件夹" });
      const fileCount = folderRow?.querySelector(
        ".rich-text-at-mention-row__file-count"
      );

      expect(folderRow).toHaveAttribute(
        "data-agent-mention-navigation",
        mentionNavigation
      );
      expect(fileCount).not.toBeNull();
      if (!fileCount) {
        throw new Error("Expected the folder child count");
      }
      expect(
        fileCount.compareDocumentPosition(enterButton) &
          Node.DOCUMENT_POSITION_FOLLOWING
      ).toBeTruthy();
      expect(enterButton).toHaveAttribute(
        "data-agent-mention-navigate-into",
        "true"
      );

      fireEvent.click(enterButton);

      expect(onNavigateIntoItem).toHaveBeenCalledWith(folderItem);
      expect(onSelectItem).not.toHaveBeenCalled();

      fireEvent.click(folderRow!);

      expect(onSelectItem).toHaveBeenCalledWith(folderItem);
    }
  );

  it("renders image mention rows with thumbnails instead of default file icons", () => {
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "diagram",
      mode: "results",
      filter: "file",
      categories: [],
      groups: [
        {
          id: "opened_files",
          items: [
            {
              kind: "file",
              href: "/workspace/assets/diagram.png",
              path: "/workspace/assets/diagram.png",
              name: "diagram.png",
              entryKind: "file",
              directoryPath: "/workspace/assets",
              thumbnailUrl: "data:image/png;base64,thumb"
            }
          ],
          totalCount: 1,
          visibleCount: 1,
          hasMore: false
        }
      ],
      error: null
    };

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey={null}
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={vi.fn()}
        onSelectItem={vi.fn()}
        onSelectCategory={vi.fn()}
        onSelectFilter={vi.fn()}
        onExpandGroup={vi.fn()}
      />
    );

    const fileRow = screen
      .getByText("diagram.png")
      .closest('[data-agent-file-mention="true"]');

    expect(
      fileRow?.querySelector('[data-agent-mention-file-thumb="true"] img')
    ).toHaveAttribute("src", "data:image/png;base64,thumb");
    expect(
      fileRow?.querySelector(".agent-gui-node__mention-file-icon")
    ).toBeNull();
  });

  it("runs the footer shortcut actions when the hint controls are clicked", () => {
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "",
      mode: "results",
      filter: "file",
      categories: [],
      groups: [
        {
          id: "opened_files",
          items: [
            {
              kind: "file",
              href: "/workspace/assets/demo.png",
              path: "/workspace/assets/demo.png",
              name: "demo.png",
              entryKind: "file",
              directoryPath: "/workspace/assets",
              score: 99
            }
          ],
          totalCount: 1,
          visibleCount: 1,
          hasMore: false
        }
      ],
      error: null
    };
    const onHighlightChange = vi.fn();
    const onSelectCategory = vi.fn();
    const onSelectFilter = vi.fn();

    render(
      <AgentFileMentionPalette
        state={state}
        highlightedKey="opened_files:file:/workspace/assets/demo.png"
        label="mention palette"
        loadingLabel="loading"
        emptyLabel="empty"
        errorLabel="error"
        tabHintLabel="hint"
        maxHeightPx={320}
        onHighlightChange={onHighlightChange}
        onSelectItem={vi.fn()}
        onSelectCategory={onSelectCategory}
        onSelectFilter={onSelectFilter}
        onExpandGroup={vi.fn()}
      />
    );

    const hint = screen.getByTestId("agent-gui-mention-palette-hint");
    expect(within(hint).getByText("切换分类").parentElement).toHaveAttribute(
      "data-tooltip",
      "切换分类"
    );

    fireEvent.click(within(hint).getByRole("button", { name: "Tab 切换分类" }));
    fireEvent.click(within(hint).getByRole("button", { name: "↑ 切换选中" }));
    fireEvent.click(within(hint).getByRole("button", { name: "↓ 切换选中" }));

    expect(onSelectCategory).toHaveBeenCalledWith("issue");
    expect(onSelectFilter).toHaveBeenCalledWith("issue");
    expect(onHighlightChange).toHaveBeenNthCalledWith(
      1,
      "opened_files:file:/workspace/assets/demo.png"
    );
    expect(onHighlightChange).toHaveBeenNthCalledWith(
      2,
      "opened_files:file:/workspace/assets/demo.png"
    );
  });

  it("renders dynamic task topic labels and isolated load-more states", () => {
    const onExpandGroup = vi.fn();
    const baseGroup = {
      id: "issue-topic:topic%2Fone",
      label: "Pinned topic",
      items: [
        {
          kind: "workspace-issue" as const,
          href: "mention://workspace-issue/issue-1?workspaceId=room-1&topicId=topic%2Fone",
          workspaceId: "room-1",
          targetId: "issue-1",
          name: "Fix pagination",
          title: "Fix pagination",
          status: "running"
        }
      ],
      totalCount: 11,
      visibleCount: 1,
      hasMore: true
    };
    const state: AgentMentionSearchState = {
      status: "ready",
      query: "",
      mode: "browse",
      filter: "issue",
      categories: [],
      groups: [{ ...baseGroup, expandStatus: "loading" }],
      error: null
    };
    const props = {
      highlightedKey: "expand:issue-topic:topic%2Fone",
      label: "mention palette",
      loadingLabel: "loading",
      emptyLabel: "empty",
      errorLabel: "error",
      tabHintLabel: "hint",
      maxHeightPx: 320,
      onHighlightChange: vi.fn(),
      onSelectItem: vi.fn(),
      onSelectCategory: vi.fn(),
      onSelectFilter: vi.fn(),
      onExpandGroup
    };
    const { rerender } = render(
      <AgentFileMentionPalette {...props} state={state} />
    );

    expect(screen.getByText("Pinned topic")).toBeVisible();
    expect(screen.getByRole("button", { name: "正在加载" })).toBeDisabled();
    expect(screen.getByText("Fix pagination")).toBeVisible();

    rerender(
      <AgentFileMentionPalette
        {...props}
        state={{
          ...state,
          groups: [{ ...baseGroup, expandStatus: "error" }]
        }}
      />
    );
    const retry = screen.getByRole("button", { name: "加载失败，重试" });
    expect(retry).toBeEnabled();
    fireEvent.click(retry);
    expect(onExpandGroup).toHaveBeenCalledWith("issue-topic:topic%2Fone");
  });
});
