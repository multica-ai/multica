// CEREBRO-PATCH(tokens-tab-service-tokens): FIR-3754 workspace-authoritative
// visibility proof for the Settings → Tokens service-token management surface.
import { render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const flagState = vi.hoisted(() => ({ enabled: true }));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useWorkspaceEffectiveFlag: () => flagState.enabled,
}));

vi.mock("@multica/cerebro-service-tokens/views", () => ({
  ServiceTokensSection: () => <div>Service token management</div>,
}));

import { ServiceTokensWorkspaceGate } from "./tokens-tab";

describe("ServiceTokensWorkspaceGate", () => {
  beforeEach(() => {
    flagState.enabled = true;
  });

  it("shows management when the workspace-effective flag is ON", () => {
    render(<ServiceTokensWorkspaceGate />);
    expect(screen.getByText("Service token management")).toBeInTheDocument();
  });

  it("hides management when the workspace-effective flag is OFF", () => {
    flagState.enabled = false;
    render(<ServiceTokensWorkspaceGate />);
    expect(screen.queryByText("Service token management")).not.toBeInTheDocument();
  });
});
