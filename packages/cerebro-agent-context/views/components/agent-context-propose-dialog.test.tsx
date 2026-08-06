// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { Agent } from "@multica/core/types";

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("../../core/queries", () => ({
  agentContextVersionsOptions: () => ({ queryKey: ["versions"] }),
}));

vi.mock("../../core/mutations", () => ({
  useCreateAgentContextChangeRequest: () => ({
    isPending: false,
    mutateAsync: vi.fn(),
  }),
  useReviewAgentContextChangeRequest: () => ({
    isPending: false,
    mutateAsync: vi.fn(),
  }),
}));

vi.mock("./agent-context-config-fields", () => ({
  AgentContextConfigFields: () => <div data-testid="config-fields" />,
}));

import { AgentContextProposeDialog } from "./agent-context-propose-dialog";

const agent = {
  id: "agent-1",
  runtime_id: "runtime-1",
  instructions: "# Existing instructions",
  model: "",
  thinking_level: "",
  runtime_config: {},
} as Agent;

describe("AgentContextProposeDialog", () => {
  it("uses the supplied visual editor instead of a raw Instructions textarea", () => {
    const VisualEditor = ({
      defaultValue,
      onUpdate,
    }: {
      defaultValue?: string;
      onUpdate?: (value: string) => void;
    }) => (
      <button
        type="button"
        data-testid="visual-instructions-editor"
        onClick={() => onUpdate?.("# Updated instructions")}
      >
        {defaultValue}
      </button>
    );

    render(
      <AgentContextProposeDialog
        agent={agent}
        instructionsEditor={VisualEditor}
      />,
    );

    expect(screen.getByTestId("visual-instructions-editor")).toHaveTextContent(
      "Existing instructions",
    );
    expect(screen.queryByRole("textbox", { name: "Instructions" })).toBeNull();

    fireEvent.click(screen.getByTestId("visual-instructions-editor"));
    expect(screen.getByRole("button", { name: "Discard" })).toBeInTheDocument();
  });
});
