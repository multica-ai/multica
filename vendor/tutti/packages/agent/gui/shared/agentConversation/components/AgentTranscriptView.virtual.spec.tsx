import {
  act,
  fireEvent,
  render,
  screen,
  waitFor
} from "@testing-library/react";
import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import { createRef, useReducer } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";

const virtualizerMockState = vi.hoisted(() => ({
  centerIndex: 10,
  getVirtualItemForOffset: vi.fn(),
  scrollOffset: 1_000,
  scrollRect: { height: 480, width: 800 },
  virtualIndexes: [100, 101, 102, 103, 104],
  containerRef: vi.fn(),
  refresh: () => {},
  isAtEnd: vi.fn(() => true),
  scrollToEnd: vi.fn(),
  scrollToOffset: vi.fn(),
  scrollToIndex: vi.fn(),
  instance: {
    shouldAdjustScrollPositionOnItemSizeChange: undefined as
      | undefined
      | (() => boolean)
  }
}));

vi.mock("../../../i18n/index", () => ({
  getActiveUiLanguage: () => "en",
  useTranslation: () => ({
    t: (key: string) => key
  }),
  translate: (key: string) => key
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: vi.fn(() => {
    const [, refresh] = useReducer((value: number) => value + 1, 0);
    virtualizerMockState.refresh = refresh;
    return Object.assign(virtualizerMockState.instance, {
      getTotalSize: () => 20000,
      getVirtualItems: () =>
        virtualizerMockState.virtualIndexes.map((index) => ({
          index,
          key: `virtual-${index}`,
          start: index * 100,
          size: 100
        })),
      getVirtualItemForOffset: virtualizerMockState.getVirtualItemForOffset,
      measureElement: vi.fn(),
      containerRef: virtualizerMockState.containerRef,
      isAtEnd: virtualizerMockState.isAtEnd,
      scrollOffset: virtualizerMockState.scrollOffset,
      scrollRect: virtualizerMockState.scrollRect,
      scrollToEnd: virtualizerMockState.scrollToEnd,
      scrollToOffset: virtualizerMockState.scrollToOffset,
      scrollToIndex: virtualizerMockState.scrollToIndex
    });
  })
}));

vi.mock("./agentTurnWorkSectionModel", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./agentTurnWorkSectionModel")>();
  return { ...actual, buildAgentTurnWorkSectionModel: vi.fn(actual.buildAgentTurnWorkSectionModel) };
});

import { useVirtualizer } from "@tanstack/react-virtual";
import type {
  AgentTranscriptAttachmentLocator,
  AgentTranscriptVirtualScrollController
} from "./AgentTranscriptView";
import { AgentTranscriptLayout as AgentTranscriptView } from "./AgentTranscriptView";
import { AgentTranscriptViewport } from "./AgentTranscriptViewport";
import { TranscriptPresentationModel } from "../../../state/TranscriptPresentationModel";
import { buildAgentTurnWorkSectionModel } from "./agentTurnWorkSectionModel";

const TRANSCRIPT_LABELS = {
  thinkingLabel: "Thought process",
  toolCallsLabel: (count: number) => `Tool calls (${count})`,
  processing: "Planning next moves",
  turnSummary: "Changed files"
};
const TRANSCRIPT_LABELS_WITH_LOCATOR = {
  ...TRANSCRIPT_LABELS,
  userMessageLocator: "User messages"
};

afterEach(() => {
  vi.restoreAllMocks();
});

describe("AgentTranscriptView virtual rendering", () => {
  beforeEach(() => {
    virtualizerMockState.centerIndex = 10;
    virtualizerMockState.getVirtualItemForOffset.mockReset();
    virtualizerMockState.getVirtualItemForOffset.mockImplementation(() => ({
      end: (virtualizerMockState.centerIndex + 1) * 100,
      index: virtualizerMockState.centerIndex,
      key: `virtual-${virtualizerMockState.centerIndex}`,
      lane: 0,
      size: 100,
      start: virtualizerMockState.centerIndex * 100
    }));
    virtualizerMockState.scrollOffset = 1_000;
    virtualizerMockState.scrollRect = { height: 480, width: 800 };
    virtualizerMockState.containerRef.mockClear();
    virtualizerMockState.isAtEnd.mockReset();
    virtualizerMockState.isAtEnd.mockReturnValue(true);
    virtualizerMockState.scrollToEnd.mockClear();
    virtualizerMockState.scrollToOffset.mockClear();
    virtualizerMockState.scrollToIndex.mockClear();
    virtualizerMockState.instance.shouldAdjustScrollPositionOnItemSizeChange =
      undefined;
  });

  it("builds only newly visible turn models when the virtual window grows", async () => {
    virtualizerMockState.virtualIndexes = [10];
    render(
      <div data-testid="agent-gui-timeline" style={{ overflow: "auto" }}>
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(40)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );
    await waitFor(() => expect(screen.getByText("turn 10 user row")).toBeTruthy());
    vi.mocked(buildAgentTurnWorkSectionModel).mockClear();

    virtualizerMockState.virtualIndexes = [10, 11];
    act(() => virtualizerMockState.refresh());

    expect(screen.getByText("turn 11 user row")).toBeTruthy();
    expect(buildAgentTurnWorkSectionModel).toHaveBeenCalledTimes(1);
    expect(vi.mocked(buildAgentTurnWorkSectionModel).mock.calls[0]?.[0].key).toBe("turn-11");
  });

  it("keeps structural projections when only viewport layout options change", () => {
    virtualizerMockState.virtualIndexes = [10];
    const conversation = conversationWithMultiRowTurns(40);
    const rendered = render(
      <div data-testid="agent-gui-timeline" style={{ overflow: "auto" }}>
        <AgentTranscriptView conversation={conversation} labels={TRANSCRIPT_LABELS} virtualListLayoutRevision={0} />
      </div>
    );
    vi.mocked(buildAgentTurnWorkSectionModel).mockClear();
    rendered.rerender(
      <div data-testid="agent-gui-timeline" style={{ overflow: "auto" }}>
        <AgentTranscriptView conversation={conversation} labels={TRANSCRIPT_LABELS} virtualListLayoutRevision={1} followEndMode="detached" />
      </div>
    );
    expect(useVirtualizer).toHaveBeenLastCalledWith(expect.objectContaining({ followOnAppend: false }));
    expect(buildAgentTurnWorkSectionModel).not.toHaveBeenCalled();
  });

  it("reuses visible turn content when virtual positions and the range change", async () => {
    virtualizerMockState.virtualIndexes = [10];
    const conversation = conversationWithMultiRowTurns(40);
    const model = TranscriptPresentationModel.fromConversation(conversation, false);
    const renderTurn = vi.fn((group: { key: string }) => <p>Content {group.key}</p>);
    render(
      <div data-testid="agent-gui-timeline" style={{ overflow: "auto" }}>
        <AgentTranscriptViewport
          model={model}
          virtualize
          renderTurnGroup={renderTurn}
        />
      </div>
    );
    await waitFor(() => expect(screen.getByText("Content turn-10")).toBeTruthy());
    renderTurn.mockClear();

    act(() => virtualizerMockState.refresh());
    expect(renderTurn).not.toHaveBeenCalled();

    virtualizerMockState.virtualIndexes = [10, 11];
    act(() => virtualizerMockState.refresh());
    expect(screen.getByText("Content turn-11")).toBeTruthy();
    expect(renderTurn).toHaveBeenCalledTimes(1);
    expect(renderTurn.mock.calls[0]?.[0].key).toBe("turn-11");
  });

  it("does not virtualize normal short conversations", () => {
    virtualizerMockState.virtualIndexes = [0];

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithRows(12)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(
      document.querySelector("[data-agent-transcript-virtualized='true']")
    ).toBeNull();
    expect(screen.getByText("virtual transcript row 0")).toBeTruthy();
    expect(screen.getByText("virtual transcript row 11")).toBeTruthy();
    expect(useVirtualizer).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: false }));
  });

  it("virtualizes by turn and keeps all rows from the visible turn mounted together", async () => {
    virtualizerMockState.virtualIndexes = [10];

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(40)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(useVirtualizer).toHaveBeenCalledWith(
      expect.objectContaining({
        anchorTo: "end",
        count: 40,
        directDomUpdates: true,
        directDomUpdatesMode: "transform",
        followOnAppend: true,
        scrollMargin: 0,
        scrollEndThreshold: 24
      })
    );
    const virtualizerOptions = vi.mocked(useVirtualizer).mock.calls.at(-1)?.[0];
    expect(virtualizerOptions?.getItemKey?.(10)).toBe("session-1\u0000turn-10");
    await waitFor(() => {
      expect(screen.getByText("turn 10 user row")).toBeTruthy();
      expect(screen.getByText("turn 10 assistant row")).toBeTruthy();
    });
    const virtualTurn = document.querySelector<HTMLElement>(
      "[data-agent-transcript-virtual-turn='turn-10']"
    );
    const virtualContainer = document.querySelector<HTMLElement>(
      "[data-agent-transcript-virtualized='true']"
    );
    expect(virtualContainer?.style.height).toBe("");
    expect(virtualizerMockState.containerRef).toHaveBeenCalledWith(
      virtualContainer
    );
    expect(virtualTurn?.style.paddingBottom).toBe("12px");
    expect(virtualTurn?.style.transform).toBe("");
    expect(
      virtualTurn?.querySelectorAll(":scope > .agent-gui-transcript-row")
    ).toHaveLength(2);
    expect(
      virtualTurn?.querySelector("[data-agent-turn-work-section]")
    ).toBeNull();
    expect(screen.queryByText("turn 9 user row")).toBeNull();
    expect(screen.queryByText("turn 11 assistant row")).toBeNull();
  });

  it("keeps exact Fork behavior inside a virtualized settled Turn", () => {
    virtualizerMockState.virtualIndexes = [10];
    const onForkThroughTurn = vi.fn();
    const baseConversation = conversationWithCollapsibleTurns(40);
    const conversation = {
      ...baseConversation,
      sourceDetail: {
        ...baseConversation.sourceDetail,
        session: normalizeAgentActivitySession({
          ...baseConversation.sourceDetail.session,
          lifecycleCapabilities: {
            fork: false,
            forkThroughTurn: true
          }
        })
      }
    };
    const { rerender } = render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversation}
          labels={TRANSCRIPT_LABELS}
          onForkThroughTurn={onForkThroughTurn}
        />
      </div>
    );

    const button = screen.getByRole("button", {
      name: "agentHost.agentGui.forkThroughTurn"
    });
    fireEvent.click(button);
    expect(onForkThroughTurn).toHaveBeenCalledWith("turn-10");

    rerender(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={{
            ...conversation,
            sourceDetail: {
              ...conversation.sourceDetail,
              session: normalizeAgentActivitySession({
                ...conversation.sourceDetail.session,
                pendingInteractions: [
                  {
                    agentSessionId: "session-1",
                    createdAtUnixMs: 4,
                    kind: "question",
                    requestId: "request-10",
                    status: "pending",
                    turnId: "turn-10",
                    updatedAtUnixMs: 4
                  }
                ]
              })
            }
          }}
          labels={TRANSCRIPT_LABELS}
          onForkThroughTurn={onForkThroughTurn}
        />
      </div>
    );
    expect(
      screen.getByRole("button", {
        name: "agentHost.agentGui.forkThroughTurn"
      })
    ).not.toBeDisabled();
  });

  it("preserves mutation anchoring but disables append following while detached", () => {
    virtualizerMockState.virtualIndexes = [10];

    const rendered = render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(40)}
          followEndMode="detached"
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(useVirtualizer).toHaveBeenLastCalledWith(
      expect.objectContaining({
        anchorTo: "end",
        followOnAppend: false
      })
    );

    rendered.rerender(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(40)}
          followEndMode="following"
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(useVirtualizer).toHaveBeenLastCalledWith(
      expect.objectContaining({
        anchorTo: "end",
        followOnAppend: true
      })
    );
  });

  it("positions fallback turns when no scroll parent is available", () => {
    render(
      <AgentTranscriptView
        conversation={conversationWithMultiRowTurns(40)}
        labels={TRANSCRIPT_LABELS}
      />
    );

    const fallbackTurns = [
      ...document.querySelectorAll<HTMLElement>(
        "[data-agent-transcript-virtual-turn]"
      )
    ];
    expect(fallbackTurns).toHaveLength(3);
    expect(fallbackTurns.map((turn) => turn.style.transform)).toEqual([
      "translateY(10360px)",
      "translateY(10640px)",
      "translateY(10920px)"
    ]);
  });

  it("exposes only the matching Session virtual scroll controller", () => {
    virtualizerMockState.virtualIndexes = [10];
    const virtualScrollController =
      createRef<AgentTranscriptVirtualScrollController>();

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(40)}
          virtualScrollControllerRef={virtualScrollController}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(virtualScrollController.current?.agentSessionId).toBe("session-1");
    expect(virtualScrollController.current?.enabled).toBe(true);
    expect(virtualScrollController.current?.isAtEnd()).toBe(true);
    virtualScrollController.current?.scrollToOffset(640, {
      behavior: "auto"
    });
    expect(virtualizerMockState.scrollToOffset).toHaveBeenCalledWith(640, {
      behavior: "auto"
    });
    virtualScrollController.current?.scrollToEnd({ behavior: "smooth" });
    expect(virtualizerMockState.scrollToEnd).toHaveBeenCalledWith({
      behavior: "smooth"
    });
  });

  it("remeasures scrollMargin when outer timeline layout changes", async () => {
    virtualizerMockState.virtualIndexes = [10];
    let virtualListTop = 120;
    const timeline = document.createElement("div");
    timeline.dataset.testid = "agent-gui-timeline";
    timeline.style.overflow = "auto";
    timeline.scrollTop = 40;
    document.body.appendChild(timeline);
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
      function (this: HTMLElement) {
        return {
          top: this === timeline ? 20 : virtualListTop
        } as DOMRect;
      }
    );
    const rendered = render(
      <AgentTranscriptView
        conversation={conversationWithMultiRowTurns(40)}
        labels={TRANSCRIPT_LABELS}
        virtualListLayoutRevision={0}
      />,
      { container: timeline }
    );

    await waitFor(() => {
      expect(
        vi.mocked(useVirtualizer).mock.calls.at(-1)?.[0].scrollMargin
      ).toBe(140);
    });

    virtualListTop = 152;
    rendered.rerender(
      <AgentTranscriptView
        conversation={conversationWithMultiRowTurns(40)}
        labels={TRANSCRIPT_LABELS}
        virtualListLayoutRevision={1}
      />
    );

    await waitFor(() => {
      expect(
        vi.mocked(useVirtualizer).mock.calls.at(-1)?.[0].scrollMargin
      ).toBe(172);
    });
    timeline.remove();
  });

  it("keeps completed turn disclosure interactive inside the virtual window", async () => {
    virtualizerMockState.virtualIndexes = [10];

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithCollapsibleTurns(40)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    await waitFor(() => {
      expect(screen.getByText("turn 10 assistant row")).toBeTruthy();
    });
    expect(
      screen.queryByRole("button", { name: "Thought process" })
    ).toBeNull();
    const timeline = screen.getByTestId("agent-gui-timeline");
    const header = document.querySelector<HTMLElement>(
      "[data-agent-turn-work-header='turn-10']"
    )!;
    timeline.scrollTop = 900;
    vi.spyOn(header, "getBoundingClientRect").mockImplementation(
      () => ({ top: 80 - (timeline.scrollTop - 900) }) as DOMRect
    );

    fireEvent.click(
      screen.getByRole("button", {
        name: "agentHost.agentGui.expandTurnWork"
      })
    );

    expect(vi.mocked(useVirtualizer).mock.calls.at(-1)?.[0]).toEqual(
      expect.objectContaining({
        anchorTo: "start",
        followOnAppend: false
      })
    );
    expect(
      virtualizerMockState.instance.shouldAdjustScrollPositionOnItemSizeChange?.()
    ).toBe(false);
    expect(timeline.style.getPropertyValue("overflow-anchor")).toBe("none");
    timeline.scrollTop = 940;
    fireEvent.scroll(timeline);
    expect(timeline.scrollTop).toBe(900);

    await flushCollapsibleRevealFrames();
    const reveal = screen
      .getByRole("button", { name: "Thought process" })
      .closest(".agent-collapsible-reveal");
    expect(
      document.querySelector("[data-agent-transcript-virtual-turn='turn-10']")
    ).toBeTruthy();
    expect(
      document.querySelector<HTMLElement>(
        "[data-agent-transcript-virtual-turn='turn-10']"
      )?.style.paddingBottom
    ).toBe("24px");
    fireEvent.transitionEnd(reveal as HTMLElement, {
      propertyName: "height"
    });

    await waitFor(() => {
      expect(vi.mocked(useVirtualizer).mock.calls.at(-1)?.[0]).toEqual(
        expect.objectContaining({ anchorTo: "end" })
      );
      expect(
        virtualizerMockState.instance.shouldAdjustScrollPositionOnItemSizeChange
      ).toBeUndefined();
    });
    timeline.scrollTop = 940;
    fireEvent.scroll(timeline);
    expect(timeline.scrollTop).toBe(940);
    expect(timeline.style.getPropertyValue("overflow-anchor")).toBe("");
  });

  it("pins a disclosure row in a non-virtual transcript", () => {
    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithCollapsibleTurns(2)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );
    const timeline = screen.getByTestId("agent-gui-timeline");
    const header = document.querySelector<HTMLElement>(
      "[data-agent-turn-work-header='turn-0']"
    )!;
    timeline.scrollTop = 200;
    vi.spyOn(header, "getBoundingClientRect").mockImplementation(
      () => ({ top: 40 - (timeline.scrollTop - 200) }) as DOMRect
    );

    fireEvent.click(
      screen.getAllByRole("button", {
        name: "agentHost.agentGui.expandTurnWork"
      })[0]!
    );
    timeline.scrollTop = 240;
    fireEvent.scroll(timeline);

    expect(timeline.scrollTop).toBe(200);
  });

  it("enables virtualization once the transcript reaches 30 turns", () => {
    virtualizerMockState.virtualIndexes = [29];

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithRows(30)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(useVirtualizer).toHaveBeenCalled();
  });

  it("keeps one complex turn in normal flow because virtualization cannot elide it", () => {
    virtualizerMockState.virtualIndexes = [0];

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithRows(1, {
            body: [
              "Complex turn intro.",
              "x".repeat(9000),
              "```ts",
              "const result = runLargeExample();",
              "```",
              "```json",
              '{"ok":true}',
              "```",
              "| File | Status |",
              "| --- | --- |",
              "| app.tsx | updated |",
              "![Preview](preview.png)"
            ].join("\n")
          })}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(
      document.querySelector("[data-agent-transcript-virtualized='true']")
    ).toBeNull();
    expect(
      document.querySelector("[data-agent-transcript-row='row-0']")
    ).toBeTruthy();
  });

  it("renders only the virtualized transcript window for long conversations", async () => {
    virtualizerMockState.virtualIndexes = [100, 101, 102, 103, 104];

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithRows(200)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(useVirtualizer).toHaveBeenCalled();
    await waitFor(() => {
      expect(screen.getByText("virtual transcript row 100")).toBeTruthy();
      expect(screen.getByText("virtual transcript row 104")).toBeTruthy();
    });
    expect(screen.queryByText("virtual transcript row 0")).toBeNull();
    expect(screen.queryByText("virtual transcript row 199")).toBeNull();
  });

  it("uses the timeline viewport when locating an unmounted virtualized message", async () => {
    virtualizerMockState.virtualIndexes = [10];

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <div data-slot="scroll-area-content">
          <AgentTranscriptView
            conversation={conversationWithMultiRowTurns(40)}
            labels={TRANSCRIPT_LABELS_WITH_LOCATOR}
          />
        </div>
      </div>
    );

    await waitFor(() => {
      const virtualizerOptions = vi
        .mocked(useVirtualizer)
        .mock.calls.at(-1)?.[0];
      expect(virtualizerOptions?.getScrollElement()).toBe(
        screen.getByTestId("agent-gui-timeline")
      );
    });

    fireEvent.click(
      screen
        .getByTestId("agent-message-locator")
        .querySelectorAll(".agent-gui-message-locator__tick")[0]!
    );

    expect(virtualizerMockState.scrollToIndex).toHaveBeenCalledWith(0, {
      align: "center"
    });
  });

  it("selects an unmounted locator item from virtual measurements", async () => {
    virtualizerMockState.centerIndex = 18;
    virtualizerMockState.virtualIndexes = [10];

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(40)}
          labels={TRANSCRIPT_LABELS_WITH_LOCATOR}
        />
      </div>
    );

    const timeline = screen.getByTestId("agent-gui-timeline");
    timeline.scrollTop = 1_000;
    const timelineQuerySelector = vi.spyOn(timeline, "querySelector");
    fireEvent.scroll(timeline);

    await waitFor(() => {
      const ticks = screen
        .getByTestId("agent-message-locator")
        .querySelectorAll(".agent-gui-message-locator__tick");
      expect(ticks[18]).toHaveAttribute("data-selected", "true");
      expect(ticks[10]).not.toHaveAttribute("data-selected");
    });
    expect(screen.queryByText("turn 18 user row")).toBeNull();
    expect(virtualizerMockState.getVirtualItemForOffset).toHaveBeenCalledWith(
      1_240
    );
    expect(
      timelineQuerySelector.mock.calls.some(([selector]) =>
        String(selector).includes("data-agent-transcript-row")
      )
    ).toBe(false);
  });

  it("ignores transient locator reversals but accepts a sustained direction change", async () => {
    virtualizerMockState.centerIndex = 10;
    virtualizerMockState.virtualIndexes = [10];
    const timeline = document.createElement("div");
    timeline.dataset.testid = "agent-gui-timeline";
    timeline.style.overflow = "auto";
    timeline.scrollTop = 1_000;
    document.body.appendChild(timeline);
    render(
      <AgentTranscriptView
        conversation={conversationWithMultiRowTurns(40)}
        labels={TRANSCRIPT_LABELS_WITH_LOCATOR}
      />,
      { container: timeline }
    );
    const selectedIndex = () =>
      [
        ...timeline.querySelectorAll(".agent-gui-message-locator__tick")
      ].findIndex((tick) => tick.getAttribute("data-selected") === "true");
    await waitFor(() => expect(selectedIndex()).toBe(10));

    virtualizerMockState.centerIndex = 5;
    timeline.scrollTop = 500;
    fireEvent.scroll(timeline);
    await waitFor(() => expect(selectedIndex()).toBe(5));

    virtualizerMockState.centerIndex = 6;
    fireEvent.scroll(timeline);
    await waitFor(() => expect(selectedIndex()).toBe(5));

    virtualizerMockState.centerIndex = 4;
    for (const scrollTop of [400, 300, 200]) {
      timeline.scrollTop = scrollTop;
      fireEvent.scroll(timeline);
      await new Promise((resolve) => requestAnimationFrame(resolve));
    }
    await waitFor(() => expect(selectedIndex()).toBe(4));

    virtualizerMockState.centerIndex = 5;
    for (const scrollTop of [300, 400]) {
      timeline.scrollTop = scrollTop;
      fireEvent.scroll(timeline);
      await new Promise((resolve) => requestAnimationFrame(resolve));
    }
    expect(selectedIndex()).toBe(4);
    timeline.scrollTop = 500;
    fireEvent.scroll(timeline);
    await waitFor(() => expect(selectedIndex()).toBe(5));

    fireEvent.wheel(timeline, { deltaY: -100 });
    virtualizerMockState.centerIndex = 6;
    timeline.scrollTop = 600;
    fireEvent.scroll(timeline);
    await new Promise((resolve) => requestAnimationFrame(resolve));
    expect(selectedIndex()).toBe(5);
    timeline.remove();
  });

  it("follows an explicit downward wheel reversal immediately", async () => {
    virtualizerMockState.centerIndex = 10;
    virtualizerMockState.virtualIndexes = [10];
    const timeline = document.createElement("div");
    timeline.dataset.testid = "agent-gui-timeline";
    timeline.style.overflow = "auto";
    timeline.scrollTop = 1_000;
    document.body.appendChild(timeline);
    render(
      <AgentTranscriptView
        conversation={conversationWithMultiRowTurns(40)}
        followEndMode="detached"
        labels={TRANSCRIPT_LABELS_WITH_LOCATOR}
      />,
      { container: timeline }
    );
    const selectedIndex = () =>
      [
        ...timeline.querySelectorAll(".agent-gui-message-locator__tick")
      ].findIndex((tick) => tick.getAttribute("data-selected") === "true");
    await waitFor(() => expect(selectedIndex()).toBe(10));

    fireEvent.wheel(timeline, { deltaY: -100 });
    virtualizerMockState.centerIndex = 5;
    timeline.scrollTop = 500;
    fireEvent.scroll(timeline);
    await waitFor(() => expect(selectedIndex()).toBe(5));

    fireEvent.wheel(timeline, { deltaY: 100 });
    virtualizerMockState.centerIndex = 6;
    timeline.scrollTop = 600;
    fireEvent.scroll(timeline);

    await waitFor(() => expect(selectedIndex()).toBe(6));
    timeline.remove();
  });

  it("follows a semantic return to the transcript end from measured positions", async () => {
    virtualizerMockState.centerIndex = 10;
    virtualizerMockState.virtualIndexes = [10];
    const timeline = document.createElement("div");
    timeline.dataset.testid = "agent-gui-timeline";
    timeline.style.overflow = "auto";
    timeline.scrollTop = 1_000;
    document.body.appendChild(timeline);
    const rendered = render(
      <AgentTranscriptView
        conversation={conversationWithMultiRowTurns(40)}
        followEndMode="detached"
        labels={TRANSCRIPT_LABELS_WITH_LOCATOR}
      />,
      { container: timeline }
    );
    const selectedIndex = () =>
      [
        ...timeline.querySelectorAll(".agent-gui-message-locator__tick")
      ].findIndex((tick) => tick.getAttribute("data-selected") === "true");
    await waitFor(() => expect(selectedIndex()).toBe(10));

    fireEvent.wheel(timeline, { deltaY: -100 });
    virtualizerMockState.centerIndex = 5;
    timeline.scrollTop = 500;
    fireEvent.scroll(timeline);
    await waitFor(() => expect(selectedIndex()).toBe(5));

    rendered.rerender(
      <AgentTranscriptView
        conversation={conversationWithMultiRowTurns(40)}
        followEndMode="following"
        labels={TRANSCRIPT_LABELS_WITH_LOCATOR}
      />
    );
    virtualizerMockState.centerIndex = 39;
    timeline.scrollTop = 3_900;
    fireEvent.scroll(timeline);

    await waitFor(() => expect(selectedIndex()).toBe(39));
    timeline.remove();
  });

  it("selects the preceding user message when the centered turn has no user row", async () => {
    virtualizerMockState.centerIndex = 18;
    virtualizerMockState.virtualIndexes = [18];
    const conversation = conversationWithMultiRowTurns(40);
    const sparseUserConversation: AgentConversationVM = {
      ...conversation,
      rows: conversation.rows.map((row) =>
        row.kind === "message" &&
        row.speaker === "user" &&
        Number(row.turnId?.replace("turn-", "")) > 9
          ? { ...row, speaker: "assistant" as const }
          : row
      )
    };

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={sparseUserConversation}
          labels={TRANSCRIPT_LABELS_WITH_LOCATOR}
        />
      </div>
    );

    const timeline = screen.getByTestId("agent-gui-timeline");
    timeline.scrollTop = 1_000;
    fireEvent.scroll(timeline);

    await waitFor(() => {
      const ticks = screen
        .getByTestId("agent-message-locator")
        .querySelectorAll(".agent-gui-message-locator__tick");
      expect(ticks).toHaveLength(10);
      expect(ticks[9]).toHaveAttribute("data-selected", "true");
    });
  });

  it("scrolls to an unmounted turn before locating its attachment", async () => {
    virtualizerMockState.virtualIndexes = [10];
    const locateAttachment = createRef<AgentTranscriptAttachmentLocator>();

    render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <div data-slot="scroll-area-content">
          <AgentTranscriptView
            conversation={conversationWithMultiRowTurns(40)}
            turnAttachments={[
              {
                id: "workflow:workflow-20",
                anchorTurnId: "turn-20",
                content: <div>Workflow 20</div>
              }
            ]}
            turnAttachmentLocatorRef={locateAttachment}
            labels={TRANSCRIPT_LABELS}
          />
        </div>
      </div>
    );

    await waitFor(() => expect(locateAttachment.current).not.toBeNull());
    locateAttachment.current?.("workflow:workflow-20");
    expect(virtualizerMockState.scrollToIndex).toHaveBeenCalledWith(20, {
      align: "center"
    });
  });

  it("hides and then positions a Fork boundary attachment in a virtualized Turn", async () => {
    virtualizerMockState.virtualIndexes = [39];
    const attachment = {
      id: "fork-lineage:operation-1",
      anchorTurnId: "turn-40",
      missingAnchorBehavior: "hide" as const,
      content: <div>Continued from task</div>
    };
    const rendered = render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(40)}
          turnAttachments={[attachment]}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(screen.queryByText("Continued from task")).toBeNull();

    virtualizerMockState.virtualIndexes = [40];
    rendered.rerender(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(41)}
          turnAttachments={[attachment]}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    await waitFor(() =>
      expect(screen.getByText("Continued from task")).toBeTruthy()
    );
    const virtualTurn = document.querySelector<HTMLElement>(
      "[data-agent-transcript-virtual-turn='turn-40']"
    );
    const assistantRow = screen
      .getByText("turn 40 assistant row")
      .closest<HTMLElement>("[data-agent-transcript-row]");
    const lineage = screen
      .getByText("Continued from task")
      .closest<HTMLElement>("[data-agent-transcript-attachment]");
    expect(virtualTurn).toContainElement(assistantRow);
    expect(virtualTurn).toContainElement(lineage);
    expect(
      assistantRow!.compareDocumentPosition(lineage!) &
        Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy();
  });

  it("keeps the timeline viewport bound across long-short-long switches", async () => {
    virtualizerMockState.virtualIndexes = [10];
    const { rerender } = render(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(40)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );
    const timeline = screen.getByTestId("agent-gui-timeline");

    await waitFor(() => {
      expect(
        vi.mocked(useVirtualizer).mock.calls.at(-1)?.[0].getScrollElement()
      ).toBe(timeline);
    });

    rerender(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithRows(2)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    await waitFor(() => {
      expect(
        vi.mocked(useVirtualizer).mock.calls.at(-1)?.[0].getScrollElement()
      ).toBe(timeline);
    });

    rerender(
      <div
        data-testid="agent-gui-timeline"
        style={{ height: "480px", overflow: "auto" }}
      >
        <AgentTranscriptView
          conversation={conversationWithMultiRowTurns(40)}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );

    expect(
      vi.mocked(useVirtualizer).mock.calls.at(-1)?.[0].getScrollElement()
    ).toBe(timeline);
  });

  it("does not reuse measurement keys across Sessions", () => {
    virtualizerMockState.virtualIndexes = [10];
    const firstConversation = conversationWithMultiRowTurns(40);
    const secondConversation = {
      ...firstConversation,
      sourceDetail: {
        ...firstConversation.sourceDetail,
        session: {
          ...firstConversation.sourceDetail.session,
          agentSessionId: "session-2"
        }
      }
    };
    const rendered = render(
      <div data-testid="agent-gui-timeline">
        <AgentTranscriptView
          conversation={firstConversation}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );
    const firstGetItemKey = vi
      .mocked(useVirtualizer)
      .mock.calls.at(-1)?.[0].getItemKey;

    rendered.rerender(
      <div data-testid="agent-gui-timeline">
        <AgentTranscriptView
          conversation={secondConversation}
          labels={TRANSCRIPT_LABELS}
        />
      </div>
    );
    const secondGetItemKey = vi
      .mocked(useVirtualizer)
      .mock.calls.at(-1)?.[0].getItemKey;

    expect(firstGetItemKey?.(10)).toBe("session-1\u0000turn-10");
    expect(secondGetItemKey?.(10)).toBe("session-2\u0000turn-10");
  });
});

function conversationWithRows(
  rowCount: number,
  options: { body?: string } = {}
): AgentConversationVM {
  const rows = Array.from({ length: rowCount }, (_, index) =>
    messageRow(index, options.body ? { body: options.body } : {})
  );
  return {
    activity: {
      id: "activity-1",
      sessionId: "session-1",
      agentName: "Codex",
      agentProvider: "codex",
      status: "working",
      title: "Codex",
      latestActivitySummary: "Working",
      sortTimeUnixMs: 10,
      changedFiles: [],
      userId: "user-1",
      userName: "Taylor",
      userAvatarUrl: ""
    },
    workspaceRoot: "/workspace/demo",
    sourceDetail: {
      activity: {
        id: "activity-1",
        sessionId: "session-1",
        agentName: "Codex",
        agentProvider: "codex",
        title: "Codex",
        latestActivitySummary: "Working",
        status: "working",
        sortTimeUnixMs: 10,
        changedFiles: [],
        userId: "user-1",
        userName: "Taylor",
        userAvatarUrl: ""
      },
      session: normalizeAgentActivitySession({
        ...{
          activeTurnId: null,
          latestTurnInteractions: [],
          pendingInteractions: []
        },
        workspaceId: "workspace-1",
        agentSessionId: "session-1",
        userId: "user-1",
        provider: "codex",
        providerSessionId: "provider-session-1",
        cwd: "/workspace/demo",
        title: "Codex",
        createdAtUnixMs: 1,
        updatedAtUnixMs: 10
      }),
      cwd: "/workspace/demo",
      workspaceRoot: "/workspace/demo",
      turns: rows.map((row) => ({
        id: row.turnId ?? row.id,
        userMessage: null,
        userMessages: [],
        agentMessages: [],
        toolCalls: [],
        toolCallCount: 0,
        hasFailedToolCall: false,
        agentItems: []
      })),
      showProcessingIndicator: false
    },
    rows
  };
}

function conversationWithMultiRowTurns(turnCount: number): AgentConversationVM {
  const rows = Array.from({ length: turnCount }, (_, index) => [
    messageRow(index, {
      idPrefix: "user-row",
      messagePrefix: "user-message",
      speaker: "user",
      body: `turn ${index} user row`
    }),
    messageRow(index, {
      idPrefix: "assistant-row",
      messagePrefix: "assistant-message",
      speaker: "assistant",
      body: `turn ${index} assistant row`
    })
  ]).flat();

  return {
    ...conversationWithRows(0),
    rows,
    sourceDetail: {
      ...conversationWithRows(0).sourceDetail,
      turns: Array.from({ length: turnCount }, (_, index) => ({
        id: `turn-${index}`,
        userMessage: null,
        userMessages: [],
        agentMessages: [],
        toolCalls: [],
        toolCallCount: 0,
        hasFailedToolCall: false,
        agentItems: []
      }))
    }
  };
}

function conversationWithCollapsibleTurns(
  turnCount: number
): AgentConversationVM {
  const conversation = conversationWithMultiRowTurns(turnCount);
  return {
    ...conversation,
    rows: conversation.rows.map((row) =>
      row.kind === "message" && row.speaker === "assistant"
        ? {
            ...row,
            messages: row.messages.map((message) => ({
              ...message,
              copyText: message.body,
              isTurnFinalText: true as const
            })),
            thinking: [
              {
                kind: "thinking-content" as const,
                id: `thinking-${row.turnId}`,
                turnId: row.turnId,
                body: `${row.turnId.replace("turn-", "turn ")} thinking`,
                occurredAtUnixMs: 2
              }
            ]
          }
        : row
    ),
    sourceDetail: {
      ...conversation.sourceDetail,
      sessionTurns: Array.from({ length: turnCount }, (_, index) => ({
        agentSessionId: "session-1",
        origin: "user_prompt" as const,
        phase: "settled" as const,
        providerForkBindingAvailable: true,
        outcome: "completed" as const,
        startedAtUnixMs: 1,
        settledAtUnixMs: 3,
        turnId: `turn-${index}`,
        updatedAtUnixMs: 3
      }))
    }
  };
}

async function flushCollapsibleRevealFrames(): Promise<void> {
  await flushAnimationFrame();
  await flushAnimationFrame();
}

async function flushAnimationFrame(): Promise<void> {
  await act(async () => {
    await new Promise<void>((resolve) => {
      requestAnimationFrame(() => resolve());
    });
  });
}

function messageRow(
  index: number,
  overrides: {
    idPrefix?: string;
    messagePrefix?: string;
    speaker?: "user" | "assistant";
    body?: string;
  } = {}
): AgentTranscriptRowVM {
  const speaker = overrides.speaker ?? "assistant";
  return {
    kind: "message",
    id: `${overrides.idPrefix ?? "row"}-${index}`,
    turnId: `turn-${index}`,
    speaker,
    messages: [
      {
        kind: "message-content",
        id: `${overrides.messagePrefix ?? "message"}-${index}`,
        turnId: `turn-${index}`,
        body: overrides.body ?? `virtual transcript row ${index}`,
        presentationKind: "content",
        occurredAtUnixMs: index
      }
    ],
    thinking: [],
    occurredAtUnixMs: index
  };
}
