// @vitest-environment jsdom
import { cleanup, fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
  beforeEach(() => { vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(600); });
  it("prefetches within the forty-row buffer before reaching the edge", () => {
    const list = Array.from({ length: 100 }, (_, index) => ({ ...items()[0]!, key: String(index), summary: `Prompt ${index}` }));
    const more = vi.fn();
    const { container, rerender } = render(<AgentMessageLocatorRail items={list} onPanelMore={more} onLocate={() => {}} />);
    fireEvent.mouseEnter(container.querySelector("button")!);
    const panel = container.querySelector<HTMLDivElement>(".agent-gui-message-locator__panel")!;
    Object.defineProperties(panel, { scrollHeight: { value: 2980 }, clientHeight: { value: 600 } });
    rerender(<AgentMessageLocatorRail items={[...list]} onPanelMore={more} onLocate={() => {}} />);
    more.mockClear();
    panel.scrollTop = 1500;
    fireEvent.scroll(panel);
    expect(more).toHaveBeenCalledWith("after");
  });
  it("keeps native wheel scrolling for an overflowing panel", () => {
    const { container, rerender } = render(<AgentMessageLocatorRail items={items()} onLocate={() => {}} />);
    fireEvent.mouseEnter(container.querySelector("button")!);
    const panel = container.querySelector<HTMLDivElement>(".agent-gui-message-locator__panel")!;
    Object.defineProperties(panel, { scrollHeight: { value: 1000 }, clientHeight: { value: 300 } });
    rerender(<AgentMessageLocatorRail items={items()} onLocate={() => {}} />);
    panel.scrollTop = 200;
    const wheel = new WheelEvent("wheel", { deltaY: 40, bubbles: true, cancelable: true });
    panel.dispatchEvent(wheel);
    expect(wheel.defaultPrevented).toBe(false);
    expect(panel.scrollTop).toBe(200);
  });
  it("does not treat directory hydration or history reload as a newly received reply", () => {
    const list = (hasAgentResponse: boolean, agentResponseKnown: boolean) => items().map((item, nodeIndex) => ({ ...item, nodeIndex, messageId: item.key, hasAgentResponse, agentResponseKnown }));
    const rail = (list: AgentMessageLocatorItem[]) => <AgentMessageLocatorRail items={list} onLocate={() => {}} />;
    const { container, rerender } = render(rail(list(false, false)));
    rerender(rail(list(true, false)));
    expect(container.querySelectorAll('[data-unread-agent-response="true"]')).toHaveLength(0);
    rerender(rail(list(true, true)));
    rerender(rail(list(false, false)));
    rerender(rail(list(true, true)));
    expect(container.querySelectorAll('[data-unread-agent-response="true"]')).toHaveLength(0);
  });

  it("continues marking a reply arriving after an observed unanswered message", () => {
    const list = (hasAgentResponse: boolean) => items().map((item) => ({ ...item, hasAgentResponse, agentResponseKnown: true }));
    const { container, rerender } = render(<AgentMessageLocatorRail items={list(false)} onLocate={() => {}} />);
    rerender(<AgentMessageLocatorRail items={list(true)} onLocate={() => {}} />);
    expect(container.querySelectorAll('[data-unread-agent-response="true"]')).toHaveLength(3);
  });
  it("keeps a large directory bounded while retaining the full logical count", () => {
    const list = Array.from({ length: 10000 }, (_, index) => ({ key: String(index), rowKey: String(index), rowIndex: index, turnGroupIndex: index, summary: `Prompt ${index}`, hasAgentResponse: false }));
    const onPanelMore = vi.fn();
    const { container } = render(<AgentMessageLocatorRail items={list} panelItems={list} onPanelMore={onPanelMore} onLocate={() => {}} />);
    expect(container.querySelector("nav")?.getAttribute("data-node-count")).toBe("10000");
    expect(container.querySelectorAll(".agent-gui-message-locator__tick").length).toBeLessThan(100);
    fireEvent.mouseEnter(container.querySelector("button")!);
    expect(container.querySelectorAll(".agent-gui-message-locator__panel-item").length).toBeGreaterThan(0);
    expect(container.querySelectorAll(".agent-gui-message-locator__panel-item").length).toBeLessThan(120);
    const panel = container.querySelector<HTMLDivElement>(".agent-gui-message-locator__panel")!;
    expect(panel.getAttribute("data-item-count")).toBe("10000");
    fireEvent.wheel(panel, { deltaY: -100 });
    expect(onPanelMore).toHaveBeenCalledWith("before");
    fireEvent.wheel(panel, { deltaY: 100 });
    expect(onPanelMore).toHaveBeenCalledWith("after");
    panel.scrollTop = 5000;
    fireEvent.scroll(panel);
    expect(panel.textContent).toContain("Prompt 170");
    expect(panel.querySelector("button")?.textContent).toBe("Prompt 129");
    expect(panel.querySelectorAll("button").length).toBeLessThan(120);
  });
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
