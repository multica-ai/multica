// @vitest-environment jsdom

import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { SettingsTab } from "./settings-tab";
import type { Agent, RuntimeDevice, MemberWithUser } from "@multica/core/types";

vi.mock("@multica/core/api", () => ({
  api: {},
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ upload: vi.fn(), uploading: false }),
}));

vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <div data-testid={`avatar-${actorId}`} />,
}));

vi.mock("../../../runtimes/components/provider-logo", () => ({
  ProviderLogo: ({ provider }: { provider: string }) => <span data-testid={`provider-logo-${provider}`} />,
}));

const runtime = (overrides: Partial<RuntimeDevice> = {}): RuntimeDevice => ({
  id: "rt-1",
  workspace_id: "ws-1",
  daemon_id: null,
  name: "Workstation",
  runtime_mode: "local",
  provider: "claude",
  status: "online",
  device_info: "macOS",
  metadata: {},
  owner_id: "user-1",
  last_seen_at: null,
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  ...overrides,
});

const agent = (runtimeIds: string[]): Agent => ({
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_ids: runtimeIds,
  runtimes: runtimeIds.map((id) => ({
    id,
    name: `Runtime ${id}`,
    status: "online",
    runtime_mode: "local",
    provider: "claude",
    device_info: "macOS",
    owner_id: "user-1",
    last_used_at: null,
  })),
  groups: [],
  name: "Test Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_env: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "private",
  status: "idle",
  max_concurrent_tasks: 6,
  owner_id: "user-1",
  skills: [],
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  archived_at: null,
  archived_by: null,
});

describe("SettingsTab", () => {
  it("renders a chip per assigned runtime", () => {
    const a = agent(["rt-1", "rt-2"]);
    render(
      <SettingsTab
        agent={a}
        runtimes={[runtime({ id: "rt-1", name: "Workstation" }), runtime({ id: "rt-2", name: "Laptop" })]}
        members={[] as MemberWithUser[]}
        currentUserId="user-1"
        onSave={vi.fn()}
      />,
    );
    expect(screen.getByText("Workstation")).toBeInTheDocument();
    expect(screen.getByText("Laptop")).toBeInTheDocument();
  });

  it("disables Save when all runtimes are removed", () => {
    const a = agent(["rt-1"]);
    render(
      <SettingsTab
        agent={a}
        runtimes={[runtime({ id: "rt-1" })]}
        members={[] as MemberWithUser[]}
        currentUserId="user-1"
        onSave={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByLabelText("Remove Workstation"));
    expect(screen.getByText(/at least one runtime/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /save/i })).toBeDisabled();
  });

  it("excludes already-assigned runtimes from the Add picker", () => {
    const a = agent(["rt-1"]);
    render(
      <SettingsTab
        agent={a}
        runtimes={[
          runtime({ id: "rt-1", name: "Workstation" }),
          runtime({ id: "rt-2", name: "Laptop" }),
        ]}
        members={[] as MemberWithUser[]}
        currentUserId="user-1"
        onSave={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /add runtime/i }));
    const popoverItems = screen.getAllByRole("menuitem");
    expect(popoverItems).toHaveLength(1);
    expect(popoverItems[0]).toHaveTextContent("Laptop");
  });
});
