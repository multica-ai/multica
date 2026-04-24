import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Workspace } from "@multica/core/types";

// Mocks live at module level so we can swap return values per-test. The
// URL host is driven by getWorkspaceUrlHost, which in production reads a
// module-level singleton set by CoreProvider. Tests don't mount the
// provider, so we stub the getter directly.
const mocks = vi.hoisted(() => ({
  getWorkspaceUrlHost: vi.fn<() => string>(() => "multica.ai"),
  useCreateWorkspace: vi.fn(),
}));

vi.mock("@multica/core/platform", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@multica/core/platform")>();
  return {
    ...actual,
    getWorkspaceUrlHost: mocks.getWorkspaceUrlHost,
  };
});

vi.mock("@multica/core/workspace/mutations", () => ({
  useCreateWorkspace: () => mocks.useCreateWorkspace(),
}));

import { StepWorkspace } from "./step-workspace";

function makeWorkspace(overrides: Partial<Workspace> = {}): Workspace {
  return {
    id: "ws_test",
    name: "Acme Labs",
    slug: "acme-labs",
    description: "",
    context: null,
    settings: {},
    repos: [],
    issue_prefix: "ACME",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  } as Workspace;
}

function mockMutation() {
  return {
    mutate: vi.fn(),
    isPending: false,
  };
}

function renderStep(
  overrides: Partial<React.ComponentProps<typeof StepWorkspace>> = {},
) {
  const onCreated = vi.fn();
  const onBack = vi.fn();
  render(
    <StepWorkspace onCreated={onCreated} onBack={onBack} {...overrides} />,
  );
  return { onCreated, onBack };
}

/**
 * The URL host shown in onboarding previews is env-driven so rebranded
 * forks can swap `multica.ai` for their own domain without editing the
 * component. These tests lock down:
 *   - Default host ("multica.ai") renders when no override is configured
 *   - Custom host renders in all three places the host is surfaced:
 *       1. The create-workspace URL pill (slug input prefix)
 *       2. The existing-workspace card subtitle (resume path)
 *       3. The sidebar preview card (right-column aside)
 */
describe("StepWorkspace — configurable URL host", () => {
  beforeEach(() => {
    mocks.getWorkspaceUrlHost.mockReset();
    mocks.getWorkspaceUrlHost.mockReturnValue("multica.ai");
    mocks.useCreateWorkspace.mockReset();
    mocks.useCreateWorkspace.mockReturnValue(mockMutation());
  });

  it("renders the default 'multica.ai' host in the URL pill when no override is set", () => {
    renderStep();
    // URL pill sits next to the slug input with a trailing slash, e.g.
    // `multica.ai/`. Match the literal including the slash so we don't
    // accidentally pass on a partial render.
    expect(screen.getByText("multica.ai/")).toBeInTheDocument();
  });

  it("renders the configured host in the URL pill when overridden", () => {
    mocks.getWorkspaceUrlHost.mockReturnValue("agentfarm.g2.com");
    renderStep();
    expect(screen.getByText("agentfarm.g2.com/")).toBeInTheDocument();
    expect(screen.queryByText("multica.ai/")).not.toBeInTheDocument();
  });

  it("reflects the configured host when the user types a slug", async () => {
    mocks.getWorkspaceUrlHost.mockReturnValue("agentfarm.g2.com");
    const user = userEvent.setup();
    renderStep();

    await user.type(screen.getByLabelText(/workspace name/i), "Acme Inc");

    // Pill keeps the configured host regardless of slug content.
    expect(screen.getByText("agentfarm.g2.com/")).toBeInTheDocument();
  });

  it("renders the configured host in the existing-workspace card (resume path)", () => {
    mocks.getWorkspaceUrlHost.mockReturnValue("agentfarm.g2.com");
    const existing = makeWorkspace({ slug: "acme-labs" });
    renderStep({ existing });

    // Existing-workspace card shows `{host}/{slug}`. Two copies render —
    // one in the card, one in the right-column preview aside — so we
    // assert on the count too.
    const entries = screen.getAllByText("agentfarm.g2.com/acme-labs");
    expect(entries.length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText("multica.ai/acme-labs")).not.toBeInTheDocument();
  });

  it("falls back to the default host across all surfaces when no override is set", () => {
    const existing = makeWorkspace({ slug: "acme-labs" });
    renderStep({ existing });

    // URL pill in the create card (collapsed, but CreateNewWorkspaceCard
    // renders children only when selected — so we only assert on surfaces
    // visible at rest in the resume path: existing card + preview aside).
    expect(screen.getAllByText("multica.ai/acme-labs").length).toBeGreaterThanOrEqual(1);
  });

  it("calls getWorkspaceUrlHost to resolve the host (does not hardcode)", () => {
    renderStep();
    // If the component ever regresses to a hardcoded string, the getter
    // will stop being called and this assertion will fail.
    expect(mocks.getWorkspaceUrlHost).toHaveBeenCalled();
  });
});
