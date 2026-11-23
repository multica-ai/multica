import { beforeEach, describe, expect, it, vi } from "vitest";
import { DEFAULT_APPEARANCE, useAppearanceStore } from "@multica/core/appearance";
import { UI_EASE_OUT_CSS } from "@multica/ui/lib/motion";
import {
  createIssueDropAnimation,
  ISSUE_DROP_DURATION_MS,
} from "./drag-drop-animation";

type FakeAnimation = {
  onfinish: (() => void) | null;
  oncancel: (() => void) | null;
  cancel: ReturnType<typeof vi.fn>;
};

function makeFakeAnim(): FakeAnimation {
  return {
    onfinish: null,
    oncancel: null,
    cancel: vi.fn(),
  };
}

function makeDropArgs() {
  const activeNode = document.createElement("div");
  activeNode.style.opacity = "0.85";

  const overlayNode = document.createElement("div");
  const innerCard = document.createElement("div");
  innerCard.style.rotate = "1deg";
  innerCard.style.scale = "1.01";
  overlayNode.appendChild(innerCard);

  const outerAnim = makeFakeAnim();
  const innerAnim = makeFakeAnim();
  const outerAnimate = vi.fn(() => outerAnim as unknown as Animation);
  const innerAnimate = vi.fn(() => innerAnim as unknown as Animation);
  overlayNode.animate = outerAnimate;
  innerCard.animate = innerAnimate;

  return {
    activeNode,
    overlayNode,
    outerAnim,
    innerAnim,
    outerAnimate,
    innerAnimate,
    args: {
      active: {
        id: "issue-1",
        data: { current: {} },
        node: activeNode,
        rect: { top: 200, left: 300, bottom: 300, right: 560, width: 260, height: 100 },
      },
      dragOverlay: {
        node: overlayNode,
        rect: { top: 140, left: 180, bottom: 240, right: 440, width: 260, height: 100 },
      },
      transform: { x: 10, y: 20, scaleX: 1, scaleY: 1 },
      draggableNodes: new Map(),
      droppableContainers: new Map(),
      measuringConfiguration: {} as never,
    } as unknown as Parameters<ReturnType<typeof createIssueDropAnimation>>[0],
  };
}

beforeEach(() => {
  useAppearanceStore.setState({ ...DEFAULT_APPEARANCE });
});

describe("createIssueDropAnimation", () => {
  it("glides the overlay into the target slot and straightens the lifted card over 170ms UI_EASE_OUT_CSS", async () => {
    const skipRef = { current: false };
    const drop = createIssueDropAnimation(skipRef);
    const { activeNode, outerAnim, innerAnim, outerAnimate, innerAnimate, args } = makeDropArgs();

    const promise = drop(args);

    expect(activeNode.style.visibility).toBe("hidden");
    expect(activeNode.style.opacity).toBe("0.85");
    expect(outerAnimate).toHaveBeenCalledWith(
      [
        { transform: "translate3d(10px, 20px, 0) scaleX(1) scaleY(1)" },
        { transform: "translate3d(130px, 80px, 0) scaleX(1) scaleY(1)" },
      ],
      {
        duration: ISSUE_DROP_DURATION_MS,
        easing: UI_EASE_OUT_CSS,
        fill: "forwards",
      },
    );
    expect(innerAnimate).toHaveBeenCalledWith(
      [
        expect.objectContaining({
          rotate: "1deg",
          scale: "1.01",
        }),
        {
          rotate: "0deg",
          scale: "1",
          opacity: "1",
          boxShadow: "none",
        },
      ],
      {
        duration: ISSUE_DROP_DURATION_MS,
        easing: UI_EASE_OUT_CSS,
        fill: "forwards",
      },
    );

    outerAnim.onfinish?.();
    await promise;

    expect(activeNode.style.visibility).toBe("");
    expect(activeNode.style.opacity).toBe("0.85");
    expect(innerAnim.cancel).toHaveBeenCalledOnce();
  });

  it("skips animation immediately when the drag was cancelled or blocked", () => {
    const skipRef = { current: true };
    const drop = createIssueDropAnimation(skipRef);
    const { activeNode, outerAnimate, innerAnimate, args } = makeDropArgs();

    const result = drop(args);

    expect(result).toBeUndefined();
    expect(outerAnimate).not.toHaveBeenCalled();
    expect(innerAnimate).not.toHaveBeenCalled();
    expect(activeNode.style.visibility).toBe("");
  });

  it("skips animation immediately when reduced motion is enabled", () => {
    useAppearanceStore.getState().set({ reduceMotion: true });
    const skipRef = { current: false };
    const drop = createIssueDropAnimation(skipRef);
    const { activeNode, outerAnimate, innerAnimate, args } = makeDropArgs();

    const result = drop(args);

    expect(result).toBeUndefined();
    expect(outerAnimate).not.toHaveBeenCalled();
    expect(innerAnimate).not.toHaveBeenCalled();
    expect(activeNode.style.visibility).toBe("");
  });
});
