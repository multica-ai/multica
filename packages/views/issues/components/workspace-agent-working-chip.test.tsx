// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceWorkingAgent } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  agents: [] as WorkspaceWorkingAgent[],
  requestedType: undefined as string | undefined,
  requestedMineRelation: undefined as string | undefined,
  avatarAgentIds: undefined as readonly string[] | undefined,
  buttonVariant: undefined as string | undefined,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/agents", () => ({
  workspaceWorkingAgentsOptions: (
    wsId: string,
    type?: string,
    mineRelation?: string,
  ) => {
    mockState.requestedType = type;
    mockState.requestedMineRelation = mineRelation;
    return {
      queryKey: [
        "workspaces",
        wsId,
        "working-agents",
        "list",
        type ?? "all",
        mineRelation ? `mine:${mineRelation}` : "workspace",
      ],
    };
  },
}));

vi.mock("../../agents/components/agent-avatar-stack", () => ({
  AgentAvatarStack: ({ agentIds }: { agentIds: readonly string[] }) => {
    mockState.avatarAgentIds = agentIds;
    return <div data-testid="agent-avatar-stack">{agentIds.length}</div>;
  },
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: () => ({ data: mockState.agents }),
  };
});

vi.mock("@multica/ui/components/ui/button", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/ui/components/ui/button")>(
      "@multica/ui/components/ui/button",
    );
  return {
    ...actual,
    Button: (props: React.ComponentProps<typeof actual.Button>) => {
      mockState.buttonVariant = props.variant ?? undefined;
      return <actual.Button {...props} />;
    },
  };
});

import {
  WorkspaceAgentWorkingChip,
  chipAppearance,
} from "./workspace-agent-working-chip";

function makeAgent(
  id: string,
  runningTaskCount = 1,
): WorkspaceWorkingAgent {
  return {
    id,
    name: `Agent ${id}`,
    avatar_url: null,
    running_task_count: runningTaskCount,
    issue_ids: [],
  };
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockState.agents = [];
  mockState.requestedType = undefined;
  mockState.requestedMineRelation = undefined;
  mockState.avatarAgentIds = undefined;
  mockState.buttonVariant = undefined;
});

describe("WorkspaceAgentWorkingChip", () => {
  it("shows every agent returned by the independent workspace API", () => {
    mockState.agents = [
      makeAgent("agent-1"),
      makeAgent("agent-2", 3),
      makeAgent("agent-3"),
    ];

    renderWithI18n(
      <WorkspaceAgentWorkingChip value={false} onToggle={() => {}} />,
    );

    expect(
      screen.getByRole("button", { name: "3 agents working" }),
    ).toBeTruthy();
    expect(mockState.avatarAgentIds).toEqual([
      "agent-1",
      "agent-2",
      "agent-3",
    ]);
    expect(mockState.requestedType).toBe("issue");
    expect(mockState.requestedMineRelation).toBeUndefined();
    expect(mockState.buttonVariant).toBe("brandSubtle");
  });

  it("requests the selected My Issues relation when the header is scoped", () => {
    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        mineRelation="involved"
      />,
    );

    expect(mockState.requestedType).toBe("issue");
    expect(mockState.requestedMineRelation).toBe("involved");
  });

  it("shows a known zero instead of an indeterminate Table value", () => {
    renderWithI18n(
      <WorkspaceAgentWorkingChip value={false} onToggle={() => {}} />,
    );

    expect(
      screen.getByRole("button", { name: "0 agents working" }),
    ).toBeTruthy();
    expect(screen.queryByTestId("agent-avatar-stack")).toBeNull();
    expect(mockState.buttonVariant).toBe("outline");
  });

  it("keeps the active filter visually selected after the final agent stops", () => {
    renderWithI18n(
      <WorkspaceAgentWorkingChip value onToggle={() => {}} />,
    );

    expect(mockState.buttonVariant).toBe("brand");
  });
});

describe("chipAppearance", () => {
  it("wears the filled brand tier while the filter is on", () => {
    expect(chipAppearance(true, true).variant).toBe("brand");
  });

  it("wears the tint tier for activity without the filter", () => {
    expect(chipAppearance(false, true).variant).toBe("brandSubtle");
  });

  it("wears the plain tier with muted text when nothing is running", () => {
    const appearance = chipAppearance(false, false);
    expect(appearance.variant).toBe("outline");
    expect(appearance.className).toContain("text-muted-foreground");
  });

  it("does not mute the active zero state", () => {
    const appearance = chipAppearance(true, false);
    expect(appearance.variant).toBe("brand");
    expect(appearance.className).not.toContain("text-muted-foreground");
  });
});
