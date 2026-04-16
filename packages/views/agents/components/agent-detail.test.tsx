import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, RuntimeDevice } from "@multica/core/types";

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid="agent-avatar">{actorId}</span>,
}));

vi.mock("./tabs/instructions-tab", () => ({
  InstructionsTab: () => <div data-testid="instructions-tab">Instructions</div>,
}));

vi.mock("./tabs/skills-tab", () => ({
  SkillsTab: () => <div data-testid="skills-tab">Skills</div>,
}));

vi.mock("./tabs/tasks-tab", () => ({
  TasksTab: () => <div data-testid="tasks-tab">Tasks</div>,
}));

vi.mock("./tabs/settings-tab", () => ({
  SettingsTab: () => <div data-testid="settings-tab">Settings</div>,
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open?: boolean; children: React.ReactNode }) => (open ? <div>{children}</div> : null),
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogDescription: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

import { AgentDetail } from "./agent-detail";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Build Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  owner_id: null,
  skills: [],
  created_at: "2026-04-13T00:00:00Z",
  updated_at: "2026-04-13T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const runtimeDevices: RuntimeDevice[] = [
  {
    id: "runtime-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Cloud Runtime",
    runtime_mode: "cloud",
    provider: "openai",
    status: "online",
    device_info: "",
    metadata: {},
    owner_id: null,
    last_seen_at: null,
    created_at: "2026-04-13T00:00:00Z",
    updated_at: "2026-04-13T00:00:00Z",
  },
];

describe("AgentDetail", () => {
  it("shows an explicit archive action and confirms before archiving", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const onArchive = vi.fn().mockResolvedValue(undefined);
    const onRestore = vi.fn().mockResolvedValue(undefined);

    render(
      <AgentDetail
        agent={baseAgent}
        runtimes={runtimeDevices}
        onUpdate={onUpdate}
        onArchive={onArchive}
        onRestore={onRestore}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Archive Agent" }));
    expect(screen.getByText("Archive agent?")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Archive" }));
    expect(onArchive).toHaveBeenCalledWith("agent-1");
  });

  it("shows restore action for archived agents", async () => {
    const user = userEvent.setup();
    const onUpdate = vi.fn().mockResolvedValue(undefined);
    const onArchive = vi.fn().mockResolvedValue(undefined);
    const onRestore = vi.fn().mockResolvedValue(undefined);

    render(
      <AgentDetail
        agent={{ ...baseAgent, archived_at: "2026-04-13T00:00:00Z" }}
        runtimes={runtimeDevices}
        onUpdate={onUpdate}
        onArchive={onArchive}
        onRestore={onRestore}
      />,
    );

    expect(screen.queryByRole("button", { name: "Archive Agent" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Restore" }));
    expect(onRestore).toHaveBeenCalledWith("agent-1");
  });
});
