import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { RunFailureCard } from "./run-failure-card";

const items = [
  { seq: 1, type: "tool_use", tool: "Bash", input: { command: "make check" } },
  { seq: 2, type: "tool_result", tool: "Bash", output: "ok\nFAIL: db refused" },
];

describe("RunFailureCard", () => {
  it("renders the human reason", () => {
    render(<RunFailureCard failureReason="agent_error.provider_quota_limit" items={[]} />);
    expect(screen.getByText("Usage quota is used up")).toBeTruthy();
  });

  it("renders the last command and the tail of its output", () => {
    render(<RunFailureCard failureReason="timeout" items={items} />);
    expect(screen.getByText(/make check/)).toBeTruthy();
    expect(screen.getByText(/FAIL: db refused/)).toBeTruthy();
  });

  it("keeps the end of a long output, not the start", () => {
    const long = [
      { seq: 1, type: "tool_result", tool: "Bash", output: "START" + "x".repeat(5000) + "THE-REAL-ERROR" },
    ];
    render(<RunFailureCard failureReason="timeout" items={long} />);
    expect(screen.getByText(/THE-REAL-ERROR/)).toBeTruthy();
  });

  it("renders nothing when the run has no failure reason", () => {
    const { container } = render(<RunFailureCard failureReason="" items={items} />);
    expect(container.firstChild).toBeNull();
  });
});
