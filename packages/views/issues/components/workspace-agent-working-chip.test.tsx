// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkingAgentSummary } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  avatarAgentIds: undefined as readonly string[] | undefined,
  buttonVariant: undefined as string | undefined,
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) => `Agent ${id}`,
    getActorInitials: () => "AG",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("../../agents/components/agent-avatar-stack", () => ({
  AgentAvatarStack: ({ agentIds }: { agentIds: readonly string[] }) => {
    mockState.avatarAgentIds = agentIds;
    return <div data-testid="agent-avatar-stack">{agentIds.length}</div>;
  },
}));

// The real hover card renders its body only while open. Render it inline so the
// chip's own wiring to the hover body is observable: the MUL-5525 follow-up bug
// was in that wiring (`agents ?? []`), not in the body's rendering.
vi.mock("@multica/ui/components/ui/hover-card", () => ({
  HoverCard: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  HoverCardTrigger: ({ render }: { render: React.ReactElement }) => render,
  HoverCardContent: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="hover-content">{children}</div>
  ),
}));

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
  WorkingAgentsHoverContent,
  WorkspaceAgentWorkingChip,
  chipActivity,
  chipAppearance,
} from "./workspace-agent-working-chip";

function makeAgent(id: string, runningTaskCount = 1): WorkingAgentSummary {
  return { id, running_task_count: runningTaskCount };
}

const UNKNOWN_HOVER = "Agents working: not loaded yet";
const EMPTY_HOVER = "No agents working right now";

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockState.avatarAgentIds = undefined;
  mockState.buttonVariant = undefined;
});

describe("WorkspaceAgentWorkingChip", () => {
  it("counts exactly the agents the surface projection supplies", () => {
    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value="all"
        onToggle={() => {}}
        agents={[makeAgent("agent-1"), makeAgent("agent-2", 3), makeAgent("agent-3")]}
      />,
    );

    expect(
      screen.getByRole("button", { name: "3 agents working" }),
    ).toBeTruthy();
    expect(mockState.avatarAgentIds).toEqual([
      "agent-1",
      "agent-2",
      "agent-3",
    ]);
    expect(mockState.buttonVariant).toBe("brandSubtle");
  });

  // The whole point of MUL-5525: the chip must not invent a count of its own.
  // A surface whose filters leave no working rows has to read zero even while
  // other agents are busy elsewhere in the workspace.
  it("shows a known zero for a surface with no working rows", () => {
    renderWithI18n(
      <WorkspaceAgentWorkingChip value="all" onToggle={() => {}} agents={[]} />,
    );

    expect(
      screen.getByRole("button", { name: "0 agents working" }),
    ).toBeTruthy();
    expect(screen.queryByTestId("agent-avatar-stack")).toBeNull();
    expect(mockState.buttonVariant).toBe("outline");
    expect(screen.getByText(EMPTY_HOVER)).toBeTruthy();
  });

  it("renders an indeterminate label while the projection is unresolved", () => {
    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value="all"
        onToggle={() => {}}
        agents={undefined}
      />,
    );

    expect(
      screen.getByRole("button", { name: "Agents working: —" }),
    ).toBeTruthy();
    expect(screen.queryByTestId("agent-avatar-stack")).toBeNull();
  });

  // Regression: the chip passed `agents ?? []` to the hover body, so an
  // unresolved projection rendered "No agents working right now" — an assertion
  // of zero on no evidence, from the one surface with room to say otherwise.
  it("does not let the hover body downgrade an unresolved projection to zero", () => {
    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value="all"
        onToggle={() => {}}
        agents={undefined}
      />,
    );

    expect(screen.getByTestId("hover-content").textContent).toBe(UNKNOWN_HOVER);
    expect(screen.queryByText(EMPTY_HOVER)).toBeNull();
  });

  // The neutral tier's muted text is what reads as "idle", so it must not be
  // worn while the answer is still unknown.
  it("does not dim the chip while the projection is unresolved", () => {
    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value="all"
        onToggle={() => {}}
        agents={undefined}
      />,
    );

    const button = screen.getByRole("button", { name: "Agents working: —" });
    expect(mockState.buttonVariant).toBe("outline");
    expect(button.className).not.toContain("text-muted-foreground");
  });

  it("keeps the active filter visually selected after the final agent stops", () => {
    renderWithI18n(
      <WorkspaceAgentWorkingChip value="working" onToggle={() => {}} agents={[]} />,
    );

    expect(mockState.buttonVariant).toBe("brand");
  });
});

describe("WorkingAgentsHoverContent", () => {
  it("says the answer is not loaded yet when the projection is unresolved", () => {
    renderWithI18n(<WorkingAgentsHoverContent agents={undefined} />);

    expect(screen.getByText(UNKNOWN_HOVER)).toBeTruthy();
    expect(screen.queryByText(EMPTY_HOVER)).toBeNull();
  });

  it("says nobody is working only for a resolved empty projection", () => {
    renderWithI18n(<WorkingAgentsHoverContent agents={[]} />);

    expect(screen.getByText(EMPTY_HOVER)).toBeTruthy();
    expect(screen.queryByText(UNKNOWN_HOVER)).toBeNull();
  });

  it("lists the roster with each agent's active run count", () => {
    renderWithI18n(
      <WorkingAgentsHoverContent
        agents={[makeAgent("a1"), makeAgent("a2", 3)]}
      />,
    );

    expect(screen.getByText("2 active agents")).toBeTruthy();
    expect(screen.getByText("Agent a1")).toBeTruthy();
    expect(screen.getByText("1 run")).toBeTruthy();
    expect(screen.getByText("Agent a2")).toBeTruthy();
    expect(screen.getByText("3 runs")).toBeTruthy();
    expect(screen.queryByText(UNKNOWN_HOVER)).toBeNull();
    expect(screen.queryByText(EMPTY_HOVER)).toBeNull();
  });
});

describe("chipActivity", () => {
  it("keeps unresolved, resolved-empty and non-empty strictly apart", () => {
    expect(chipActivity(undefined)).toBe("unknown");
    expect(chipActivity([])).toBe("none");
    expect(chipActivity([makeAgent("a1")])).toBe("some");
  });
});

describe("WorkspaceAgentWorkingChip tri-state", () => {
  const agents = [makeAgent("agent-1"), makeAgent("agent-2")];

  it("describes each of the three states without renaming the button", () => {
    const expected = {
      all: "showing all issues",
      working: "showing only issues with agents working",
      not_working: "showing only issues without agents working",
    } as const;
    for (const mode of ["all", "working", "not_working"] as const) {
      cleanup();
      renderWithI18n(<WorkspaceAgentWorkingChip value={mode} onToggle={() => {}} agents={agents} />);
      // The count label is the accessible name in every state, so the number
      // does not change when the chip is clicked.
      const button = screen.getByRole("button", { name: "2 agents working" });
      expect(button.getAttribute("data-working-mode")).toBe(mode);
      const describedBy = button.getAttribute("aria-describedby");
      expect(describedBy).toBeTruthy();
      expect(document.getElementById(describedBy!)?.textContent).toBe(expected[mode]);
    }
  });

  it("marks each state with a distinct shape, not only colour", () => {
    const icon = (mode: "all" | "working" | "not_working") => {
      cleanup();
      renderWithI18n(<WorkspaceAgentWorkingChip value={mode} onToggle={() => {}} agents={agents} />);
      return document.querySelector("[data-state-icon]")?.getAttribute("data-state-icon") ?? null;
    };
    expect(icon("all")).toBeNull();
    expect(icon("working")).toBe("working");
    expect(icon("not_working")).toBe("not_working");
  });

  it("keeps the state glyph decorative and the accessible name unchanged", () => {
    renderWithI18n(<WorkspaceAgentWorkingChip value="not_working" onToggle={() => {}} agents={agents} />);
    const glyph = document.querySelector("[data-state-icon]")!;
    expect(glyph.getAttribute("aria-hidden")).toBe("true");
    expect(screen.getByRole("button", { name: "2 agents working" })).toBeTruthy();
  });

  it("shows the glyph even when the projection is unresolved", () => {
    renderWithI18n(<WorkspaceAgentWorkingChip value="working" onToggle={() => {}} agents={undefined} />);
    expect(document.querySelector('[data-state-icon="working"]')).toBeTruthy();
  });

  it("wears the filled brand tier in both filtered states", () => {
    for (const mode of ["working", "not_working"] as const) {
      cleanup();
      renderWithI18n(<WorkspaceAgentWorkingChip value={mode} onToggle={() => {}} agents={[]} />);
      expect(mockState.buttonVariant).toBe("brand");
    }
  });

  it("keeps the count when not_working is selected", () => {
    renderWithI18n(<WorkspaceAgentWorkingChip value="not_working" onToggle={() => {}} agents={agents} />);
    expect(screen.getByRole("button", { name: "2 agents working" })).toBeTruthy();
  });

  it("calls onToggle once per click and never owns the state itself", () => {
    const onToggle = vi.fn();
    renderWithI18n(<WorkspaceAgentWorkingChip value="all" onToggle={onToggle} agents={agents} />);
    fireEvent.click(screen.getByRole("button", { name: "2 agents working" }));
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});

describe("chipAppearance", () => {
  it("wears the filled brand tier while the filter is on", () => {
    expect(chipAppearance("working", "some").variant).toBe("brand");
  });

  it("wears the tint tier for activity without the filter", () => {
    expect(chipAppearance("all", "some").variant).toBe("brandSubtle");
  });

  it("wears the plain tier with muted text when nothing is running", () => {
    const appearance = chipAppearance("all", "none");
    expect(appearance.variant).toBe("outline");
    expect(appearance.className).toContain("text-muted-foreground");
  });

  it("stays neutral but undimmed while the projection is unknown", () => {
    const appearance = chipAppearance("all", "unknown");
    expect(appearance.variant).toBe("outline");
    expect(appearance.className).not.toContain("text-muted-foreground");
  });

  it("does not mute the active zero state", () => {
    const appearance = chipAppearance("working", "none");
    expect(appearance.variant).toBe("brand");
    expect(appearance.className).not.toContain("text-muted-foreground");
  });
});
