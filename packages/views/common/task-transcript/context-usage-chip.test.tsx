// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { ContextUsageChip } from "./context-usage-chip";
import type { ContextEventSource } from "./context-usage";

function usage(used: number, at: string, limit = 200_000): ContextEventSource {
  return { type: "context", input: { used_tokens: used, limit_tokens: limit }, created_at: at };
}

beforeEach(() => {
  cleanup();
});

describe("ContextUsageChip", () => {
  it("renders nothing when the runtime reported no occupancy", () => {
    const { container } = renderWithI18n(
      <ContextUsageChip items={[{ type: "text" }, { type: "tool_use" }]} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("states the newest figure", () => {
    renderWithI18n(
      <ContextUsageChip
        items={[usage(10_000, "2026-11-20T01:00:00Z"), usage(120_000, "2026-11-20T01:05:00Z")]}
      />,
    );
    expect(screen.getByText("60%")).toBeInTheDocument();
    expect(screen.getByText("120K/200K")).toBeInTheDocument();
  });

  it("omits the percentage when no window was reported", () => {
    renderWithI18n(
      <ContextUsageChip items={[{ type: "context", input: { used_tokens: 12_300 } }]} />,
    );
    expect(screen.getByText("12.3K")).toBeInTheDocument();
    expect(screen.queryByText(/%/)).not.toBeInTheDocument();
  });

  it("keeps the history behind the chip, newest first", async () => {
    renderWithI18n(
      <ContextUsageChip
        items={[
          usage(10_000, "2026-11-20T01:00:00Z"),
          { type: "context", input: { phase: "compacted", pre_tokens: 180_000 } },
          usage(40_000, "2026-11-20T01:11:00Z"),
        ]}
      />,
    );

    await userEvent.click(screen.getByRole("button"));

    const entries = await screen.findAllByRole("listitem");
    expect(entries).toHaveLength(3);
    expect(entries[0]).toHaveTextContent("20%");
    expect(entries[1]).toHaveTextContent("Compacted the context window (180K before)");
    expect(entries[2]).toHaveTextContent("5%");
  });

  it("falls back to the figure a task row carries when no stream is loaded", () => {
    renderWithI18n(<ContextUsageChip usage={{ usedTokens: 75_200, limitTokens: 258_400 }} />);
    expect(screen.getByText("29%")).toBeInTheDocument();
    expect(screen.getByText("75.2K/258.4K")).toBeInTheDocument();
  });

  it("prefers the loaded stream over the row, which can only be older", () => {
    renderWithI18n(
      <ContextUsageChip
        items={[usage(120_000, "2026-11-20T01:05:00Z")]}
        usage={{ usedTokens: 10_000, limitTokens: 200_000 }}
      />,
    );
    expect(screen.getByText("60%")).toBeInTheDocument();
  });

  it("stays a plain figure when there is only one observation to show", () => {
    renderWithI18n(<ContextUsageChip items={[usage(10_000, "2026-11-20T01:00:00Z")]} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
