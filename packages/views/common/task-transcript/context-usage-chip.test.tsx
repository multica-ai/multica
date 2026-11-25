// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_CONTEXT_USAGE_SETTINGS,
  useContextUsageSettingsStore,
} from "@multica/core/agents/stores";
import { renderWithI18n } from "../../test/i18n";
import { ContextUsageChip } from "./context-usage-chip";
import type { ContextEventSource } from "./context-usage";

function usage(used: number, at: string, limit = 200_000): ContextEventSource {
  return { type: "context", input: { used_tokens: used, limit_tokens: limit }, created_at: at };
}

beforeEach(() => {
  cleanup();
  // The feature ships off; every case below is about what it shows once on.
  useContextUsageSettingsStore.setState({ enabled: true, showOnIssueRow: true });
});

afterEach(() => {
  useContextUsageSettingsStore.setState(DEFAULT_CONTEXT_USAGE_SETTINGS);
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

  // Only Claude and Codex report a window, and occupancy without one is not
  // shown at all: a bare token count is not something a reader can act on.
  it("renders nothing for an occupancy with no window", () => {
    const { container } = renderWithI18n(
      <ContextUsageChip items={[{ type: "context", input: { used_tokens: 12_300 } }]} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps the history behind the chip, newest first", async () => {
    renderWithI18n(
      <ContextUsageChip
        items={[
          usage(10_000, "2026-11-20T01:00:00Z"),
          // An event with no readable figure in it adds no entry rather
          // than one with nothing to show.
          { type: "context", input: { phase: "compacted", pre_tokens: 180_000 } },
          usage(40_000, "2026-11-20T01:11:00Z"),
        ]}
      />,
    );

    await userEvent.click(screen.getByRole("button"));

    const entries = await screen.findAllByRole("listitem");
    expect(entries).toHaveLength(2);
    expect(entries[0]).toHaveTextContent("20%");
    expect(entries[1]).toHaveTextContent("5%");
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

  // Whether the chip opens must not depend on how many times the runtime
  // happened to report: a short turn reports once, and the reader still
  // expects the same affordance they get on a long one.
  it("opens on a single observation too", async () => {
    renderWithI18n(<ContextUsageChip items={[usage(10_000, "2026-11-20T01:00:00Z")]} />);

    await userEvent.click(screen.getByRole("button"));
    expect(await screen.findAllByRole("listitem")).toHaveLength(1);
  });

  it("stays a plain figure with no history and no way to fetch one", () => {
    renderWithI18n(<ContextUsageChip usage={{ usedTokens: 75_200, limitTokens: 258_400 }} />);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  // A finished run in an issue fetches no messages until a reader asks; the
  // chip is what asks, so it opens before it has anything to list.
  it("opens and asks for the history a surface has not loaded", async () => {
    const onOpenHistory = vi.fn();
    renderWithI18n(
      <ContextUsageChip
        usage={{ usedTokens: 75_200, limitTokens: 258_400 }}
        onOpenHistory={onOpenHistory}
      />,
    );

    await userEvent.click(screen.getByRole("button"));
    expect(onOpenHistory).toHaveBeenCalledTimes(1);
    expect(await screen.findByText("Loading the run's history...")).toBeInTheDocument();
  });
});

describe("ContextUsageChip settings", () => {
  it("renders nothing at all until the feature is turned on", () => {
    useContextUsageSettingsStore.setState(DEFAULT_CONTEXT_USAGE_SETTINGS);
    const { container } = renderWithI18n(
      <ContextUsageChip items={[usage(120_000, "2026-11-20T01:05:00Z")]} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("drops only the issue row when its own switch is off", () => {
    useContextUsageSettingsStore.setState({ enabled: true, showOnIssueRow: false });
    const items = [usage(120_000, "2026-11-20T01:05:00Z")];

    const row = renderWithI18n(<ContextUsageChip items={items} surface="issue_row" />);
    expect(row.container).toBeEmptyDOMElement();
    cleanup();

    renderWithI18n(<ContextUsageChip items={items} />);
    expect(screen.getByText("60%")).toBeInTheDocument();
  });
});
