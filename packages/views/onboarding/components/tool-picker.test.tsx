import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

vi.mock("../../agents/components/model-dropdown", () => ({ ModelDropdown: () => null }));
vi.mock("../../agents/components/inspector/thinking-prop-row", () => ({
  useThinkingLevels: () => ({ levels: [], allowClear: false }),
}));

import { EMPTY_TOOL_SELECTION, ToolPicker, type ToolSelection } from "./tool-picker";

const rt = (id: string, provider: string): AgentRuntime =>
  ({
    id,
    provider,
    status: "online",
    device_info: "MacBook · 1.0.0",
    metadata: { cli_version: "0.8.2-12-gabc" },
  }) as unknown as AgentRuntime;

function Harness({ runtimes, initial = EMPTY_TOOL_SELECTION }: { runtimes: AgentRuntime[]; initial?: ToolSelection }) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <output data-testid="picked">{value.runtimeId}</output>
      <ToolPicker
        runtimes={runtimes}
        detecting={false}
        canRescan={false}
        onRescan={vi.fn()}
        value={value}
        onChange={setValue}
      />
    </>
  );
}

const picked = () => screen.getByTestId("picked").textContent;

describe("ToolPicker", () => {
  it("keeps the top tool selected while tools are still registering", () => {
    const { rerender } = renderWithI18n(<Harness runtimes={[rt("dsh", "dsh")]} />);
    expect(picked()).toBe("dsh");

    rerender(<Harness runtimes={[rt("claude", "claude"), rt("dsh", "dsh")]} />);
    expect(picked()).toBe("claude");
  });

  it("never overrides a tool the member picked", () => {
    const { rerender } = renderWithI18n(<Harness runtimes={[rt("codex", "codex"), rt("dsh", "dsh")]} />);
    fireEvent.click(screen.getByRole("button", { name: /DeepSeek/ }));
    expect(picked()).toBe("dsh");

    rerender(<Harness runtimes={[rt("claude", "claude"), rt("codex", "codex"), rt("dsh", "dsh")]} />);
    expect(picked()).toBe("dsh");
  });

  it("keeps the selection it was handed when coming back to the step", () => {
    renderWithI18n(
      <Harness
        runtimes={[rt("claude", "claude"), rt("codex", "codex")]}
        initial={{ ...EMPTY_TOOL_SELECTION, runtimeId: "codex" }}
      />,
    );
    expect(picked()).toBe("codex");
  });

  it("describes a tool by its host and version, not Multica's CLI version", () => {
    renderWithI18n(<Harness runtimes={[rt("claude", "claude")]} />);
    expect(screen.getByText("MacBook · 1.0.0")).toBeInTheDocument();
    expect(screen.queryByText(/0\.8\.2/)).not.toBeInTheDocument();
  });
});
