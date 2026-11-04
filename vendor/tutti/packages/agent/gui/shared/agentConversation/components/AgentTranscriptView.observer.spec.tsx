import { useVirtualizer } from "@tanstack/react-virtual";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import { observable, runInAction } from "mobx";
import { observer } from "mobx-react-lite";
import { useRef, type FC } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useModelValue } from "../../engine/useEngineSelector";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import { AgentTranscriptView } from "./AgentTranscriptView";

vi.mock("../../../i18n/index", () => ({
  getActiveUiLanguage: () => "en",
  useTranslation: () => ({ t: (key: string) => key }),
  translate: (key: string) => key
}));

const VIEWPORT_HEIGHT = 480;
const ITEM_HEIGHT = 100;
/** Past the end of any list here: unmeasured rows take their estimated height, not ITEM_HEIGHT. */
const END = 1_000_000;
const LABELS = {
  thinkingLabel: "Thought process",
  toolCallsLabel: (count: number) => `Tool calls (${count})`,
  processing: "Planning next moves",
  turnSummary: "Changed files"
};

/** jsdom lays nothing out, so the real virtualizer gets the sizes a browser would give it. */
function stubLayout() {
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockImplementation(
    function (this: HTMLElement) {
      return this.dataset.testid === "agent-gui-timeline"
        ? VIEWPORT_HEIGHT
        : ITEM_HEIGHT;
    }
  );
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(800);
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(
    function (this: HTMLElement) {
      const height =
        this.dataset.testid === "agent-gui-timeline"
          ? VIEWPORT_HEIGHT
          : ITEM_HEIGHT;
      return { bottom: height, height, left: 0, right: 800, top: 0, width: 800, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
    }
  );
}

function scrollTo(element: HTMLElement, top: number) {
  act(() => {
    element.scrollTop = top;
    fireEvent.scroll(element);
  });
}

function createBodies(count: number) {
  return observable.box<readonly string[]>(
    Array.from({ length: count }, (_, index) => `body ${index}`),
    { deep: false }
  );
}
type Bodies = ReturnType<typeof createBodies>;

function commit(bodies: Bodies, change: (current: string[]) => void) {
  act(() => {
    runInAction(() => {
      const next = [...bodies.get()];
      change(next);
      bodies.set(next);
    });
  });
}

function Timeline({ bodies }: { bodies: readonly string[] }) {
  return (
    <div
      data-testid="agent-gui-timeline"
      style={{ height: `${VIEWPORT_HEIGHT}px`, overflow: "auto" }}
    >
      <AgentTranscriptView
        conversation={conversationOf(bodies)}
        labels={LABELS}
      />
    </div>
  );
}

const readers: [string, FC<{ model: Bodies }>][] = [
  [
    "useModelValue",
    function ThroughHook({ model }) {
      return <Timeline bodies={useModelValue(() => model.get(), [model])} />;
    }
  ],
  [
    "observer",
    observer(function ThroughObserver({ model }: { model: Bodies }) {
      return <Timeline bodies={model.get()} />;
    })
  ]
];

function mountedBodies(): string[] {
  return [...document.querySelectorAll("[data-agent-transcript-virtual-turn]")]
    .map((turn) => turn.textContent ?? "")
    .filter(Boolean);
}

describe("the transcript's virtual list under MobX", () => {
  let consoleError: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    stubLayout();
    consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe.each(readers)("with a commit read through %s", (_name, Reader) => {
    it("mounts a window of a long conversation", () => {
      render(<Reader model={createBodies(60)} />);
      expect(
        document.querySelector("[data-agent-transcript-virtualized='true']")
      ).toBeTruthy();
      const mounted = mountedBodies();
      expect(mounted.length).toBeGreaterThan(0);
      expect(mounted.length).toBeLessThan(60);
    });

    it("shows a streamed change to a mounted row, leaving the window alone", () => {
      const model = createBodies(60);
      render(<Reader model={model} />);
      const before = mountedBodies().length;
      const target = screen.getByText("body 1");
      expect(target).toBeTruthy();

      commit(model, (rows) => {
        rows[1] = "body 1 and more";
      });

      expect(screen.getByText("body 1 and more")).toBeTruthy();
      expect(screen.queryByText("body 1")).toBeNull();
      expect(mountedBodies()).toHaveLength(before);
    });

    it("shows the latest text of a row that changed while it was not mounted", () => {
      const model = createBodies(60);
      render(<Reader model={model} />);
      expect(screen.queryByText("body 59")).toBeNull();

      commit(model, (rows) => {
        rows[59] = "body 59 changed offscreen";
      });
      expect(screen.queryByText("body 59 changed offscreen")).toBeNull();

      scrollTo(screen.getByTestId("agent-gui-timeline"), END);

      expect(screen.getByText("body 59 changed offscreen")).toBeTruthy();
    });

    it("keeps a commit from changing a row scrolled out of the window", () => {
      const model = createBodies(60);
      render(<Reader model={model} />);
      const timeline = screen.getByTestId("agent-gui-timeline");
      scrollTo(timeline, END);
      expect(screen.queryByText("body 0")).toBeNull();

      commit(model, (rows) => {
        rows[0] = "body 0 changed";
      });
      expect(screen.queryByText("body 0 changed")).toBeNull();

      scrollTo(timeline, 0);
      expect(screen.getByText("body 0 changed")).toBeTruthy();
    });

    it("grows when a commit appends a turn", () => {
      const model = createBodies(60);
      render(<Reader model={model} />);
      scrollTo(screen.getByTestId("agent-gui-timeline"), END);

      commit(model, (rows) => {
        rows.push("body appended");
      });

      expect(screen.getByText("body appended")).toBeTruthy();
    });

    it("stops listening when unmounted", () => {
      const model = createBodies(60);
      const { unmount } = render(<Reader model={model} />);
      unmount();
      commit(model, (rows) => {
        rows[1] = "after unmount";
      });
      expect(screen.queryByText("after unmount")).toBeNull();
      expect(consoleError).not.toHaveBeenCalled();
    });

    it("reports no flushSync or update-in-render trouble while streaming", () => {
      const model = createBodies(60);
      render(<Reader model={model} />);
      for (let step = 0; step < 5; step++) {
        commit(model, (rows) => {
          rows[1] = `body 1 step ${step}`;
        });
      }
      scrollTo(screen.getByTestId("agent-gui-timeline"), END);
      commit(model, (rows) => {
        rows[59] = "body 59 streamed";
      });
      expect(screen.getByText("body 59 streamed")).toBeTruthy();
      expect(consoleError).not.toHaveBeenCalled();
    });
  });

  describe("with observer rows inside the virtualizer", () => {
    function setup(count: number) {
      const items = observable.map<number, string>(
        Array.from({ length: count }, (_, index) => [index, `item ${index}`]),
        { deep: false }
      );
      const renders = new Map<number, number>();
      let listRenders = 0;
      const Row = observer(function Row({ index }: { index: number }) {
        renders.set(index, (renders.get(index) ?? 0) + 1);
        return <p>{items.get(index)}</p>;
      });
      function List() {
        listRenders += 1;
        const scrollRef = useRef<HTMLDivElement>(null);
        const virtualizer = useVirtualizer({
          count,
          estimateSize: () => ITEM_HEIGHT,
          getScrollElement: () => scrollRef.current,
          overscan: 2,
          useFlushSync: true
        });
        return (
          <div data-testid="agent-gui-timeline" ref={scrollRef}>
            {virtualizer.getVirtualItems().map((item) => (
              <div
                data-index={item.index}
                key={item.key}
                ref={virtualizer.measureElement}
              >
                <Row index={item.index} />
              </div>
            ))}
          </div>
        );
      }
      render(<List />);
      return { items, listRenders: () => listRenders, renders };
    }
    const change = (items: Map<number, string>, index: number, text: string) =>
      act(() => {
        runInAction(() => items.set(index, text));
      });

    it("renders only the changed row, without its list", () => {
      const { items, listRenders, renders } = setup(100);
      expect(renders.has(1)).toBe(true);
      const list = listRenders();
      const one = renders.get(1)!;
      const zero = renders.get(0)!;

      change(items, 1, "item 1 changed");

      expect(screen.getByText("item 1 changed")).toBeTruthy();
      expect(renders.get(1)).toBe(one + 1);
      expect(renders.get(0)).toBe(zero);
      expect(listRenders()).toBe(list);
    });

    it("does not render a row outside the window for a change to it", () => {
      const { items, renders } = setup(100);
      expect(renders.has(90)).toBe(false);
      change(items, 90, "item 90 changed");
      expect(renders.has(90)).toBe(false);
    });

    it("drops a scrolled-out row's subscription, and reads the latest when it returns", () => {
      const { items, renders } = setup(100);
      const timeline = screen.getByTestId("agent-gui-timeline");
      scrollTo(timeline, END);
      expect(screen.queryByText("item 0")).toBeNull();
      const zero = renders.get(0)!;

      change(items, 0, "item 0 changed");
      expect(renders.get(0)).toBe(zero);

      scrollTo(timeline, 0);
      expect(screen.getByText("item 0 changed")).toBeTruthy();
    });
  });
});

function conversationOf(bodies: readonly string[]): AgentConversationVM {
  return {
    activity: {
      sessionId: "session-1"
    } as AgentConversationVM["activity"],
    workspaceRoot: "/workspace/demo",
    sourceDetail: {
      activity: { sessionId: "session-1" },
      session: normalizeAgentActivitySession({
        activeTurnId: null,
        latestTurnInteractions: [],
        pendingInteractions: [],
        workspaceId: "workspace-1",
        agentSessionId: "session-1",
        userId: "user-1",
        provider: "codex",
        providerSessionId: "provider-session-1",
        cwd: "/workspace/demo",
        title: "Codex",
        createdAtUnixMs: 1,
        updatedAtUnixMs: 10
      } as never),
      cwd: "/workspace/demo",
      workspaceRoot: "/workspace/demo",
      turns: bodies.map((_, index) => ({
        id: `turn-${index}`,
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
    rows: bodies.map((body, index) => ({
      kind: "message" as const,
      id: `row-${index}`,
      turnId: `turn-${index}`,
      speaker: "assistant" as const,
      messages: [
        {
          kind: "message-content" as const,
          id: `message-${index}`,
          turnId: `turn-${index}`,
          body,
          presentationKind: "content" as const,
          occurredAtUnixMs: index
        }
      ],
      thinking: [],
      occurredAtUnixMs: index
    }))
  } as unknown as AgentConversationVM;
}
