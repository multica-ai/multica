// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { AgentMessageLocatorRail } from "./AgentMessageLocatorRail";
import type { AgentMessageLocatorItem } from "./agentTranscriptModel";

const items = (): AgentMessageLocatorItem[] =>
  ["a", "b", "c"].map(
    (key, rowIndex) => ({
      hasAgentResponse: true,
      key,
      rowIndex,
      rowKey: key,
      summary: key,
      turnGroupIndex: rowIndex
    })
  );

describe("AgentMessageLocatorRail", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it("reads the rail's scroll position only when the selection can have moved", () => {
    const rail = (list: AgentMessageLocatorItem[]) => (
      <AgentMessageLocatorRail items={list} onLocate={() => {}} />
    );
    const { container, rerender } = render(rail(items()));
    const reads = vi.spyOn(Element.prototype, "scrollTop", "get");

    fireEvent.click(container.querySelectorAll("button")[1]!);
    expect(reads).toHaveBeenCalled();

    reads.mockClear();
    rerender(rail(items()));
    expect(reads).not.toHaveBeenCalled();
  });
});
