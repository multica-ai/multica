import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";
import { useRef } from "react";
import type { ChatMode } from "@multica/core/chat";
import { useChatResize } from "./use-chat-resize";

const state = {
  chatWidth: 380,
  chatHeight: 600,
  isExpanded: false,
  setChatSize: vi.fn((w: number, h: number) => {
    state.chatWidth = w;
    state.chatHeight = h;
    state.isExpanded = false;
  }),
  setExpanded: vi.fn((expanded: boolean) => {
    state.isExpanded = expanded;
  }),
};

vi.mock("@multica/core/chat", () => ({
  CHAT_MIN_W: 360,
  CHAT_MIN_H: 480,
  useChatStore: Object.assign(
    (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
    { getState: () => state },
  ),
}));

function Harness({
  mode,
  capture,
}: {
  mode: ChatMode;
  capture: (r: ReturnType<typeof useChatResize>) => void;
}) {
  const windowRef = useRef<HTMLDivElement>(null);
  capture(useChatResize(windowRef, mode));
  return (
    <div>
      <div ref={windowRef} />
    </div>
  );
}

function renderResize(mode: ChatMode) {
  let result!: ReturnType<typeof useChatResize>;
  render(<Harness mode={mode} capture={(r) => { result = r; }} />);
  return () => result;
}

describe("useChatResize", () => {
  beforeEach(() => {
    // jsdom performs no real layout — stub the container box the hook
    // measures via ResizeObserver so maxW/maxH aren't always 0.
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(1000);
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(800);
  });

  afterEach(() => {
    vi.restoreAllMocks();
    state.chatWidth = 380;
    state.chatHeight = 600;
    state.isExpanded = false;
    state.setChatSize.mockClear();
    state.setExpanded.mockClear();
  });

  it("floating mode caps height to the 90% breathing-room ratio", () => {
    const get = renderResize("floating");
    expect(get().renderWidth).toBe(380);
    expect(get().renderHeight).toBe(600);
    expect(get().isAtMax).toBe(false);
  });

  it.each(["floating", "docked-right"] as const)("%s fits a container narrower than the preferred minimum", (mode) => {
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(320);
    const get = renderResize(mode);
    expect(get().renderWidth).toBe(288);
    expect(state.chatWidth).toBe(380);
  });

  it("docked-right mode always fills the container height, ignoring chatHeight", () => {
    const get = renderResize("docked-right");
    expect(get().renderHeight).toBe(800); // full container height, not clamp(600, 480, 720)
    expect(get().renderWidth).toBe(380); // width still user-controlled
  });

  it("docked-right toggleExpand only changes width, leaving chatHeight untouched", () => {
    const get = renderResize("docked-right");
    get().toggleExpand();
    expect(state.setChatSize).toHaveBeenCalledWith(900, 600);
    expect(state.setExpanded).not.toHaveBeenCalled();
  });

  it("floating toggleExpand still uses setExpanded (no regression)", () => {
    const get = renderResize("floating");
    get().toggleExpand();
    expect(state.setExpanded).toHaveBeenCalledWith(true);
    expect(state.setChatSize).not.toHaveBeenCalled();
  });

  it("floating toggleExpand at max restores both width and height", () => {
    state.isExpanded = true;
    const get = renderResize("floating");
    get().toggleExpand();
    expect(state.setChatSize).toHaveBeenCalledWith(360, 480);
  });
});
