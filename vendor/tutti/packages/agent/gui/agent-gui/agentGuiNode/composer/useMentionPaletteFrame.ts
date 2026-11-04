import {
  useCallback,
  useLayoutEffect,
  useMemo,
  useState,
  type CSSProperties,
  type RefObject
} from "react";
import {
  MENTION_PALETTE_GAP_PX,
  MENTION_PALETTE_MAX_HEIGHT_PX,
  MENTION_PALETTE_MIN_HEIGHT_PX,
  MENTION_PALETTE_VIEWPORT_PADDING_PX,
  composerPaletteZIndex,
  resolveMentionPaletteZIndex,
  type MentionPaletteFrame
} from "./AgentComposerChrome";
import { resolveComposerPortalTarget } from "./composerPortalTarget";

export function useMentionPaletteFrame(
  inputShellRef: RefObject<HTMLDivElement | null>,
  showFileMentionPalette: boolean,
  viewportTopInset = MENTION_PALETTE_VIEWPORT_PADDING_PX
) {
  const [mentionPaletteFrame, setMentionPaletteFrame] =
    useState<MentionPaletteFrame | null>(null);
  const syncMentionPaletteFrame = useCallback((): void => {
    const anchor = inputShellRef.current;
    if (!anchor || typeof window === "undefined") {
      setMentionPaletteFrame(null);
      return;
    }

    const rect = (anchor.querySelector(".agent-gui-node__composer-prompt-input-area") ?? anchor).getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    const width = Math.max(
      0,
      Math.min(
        rect.width,
        viewportWidth - MENTION_PALETTE_VIEWPORT_PADDING_PX * 2
      )
    );
    const left = Math.max(
      MENTION_PALETTE_VIEWPORT_PADDING_PX,
      Math.min(
        rect.left,
        viewportWidth - MENTION_PALETTE_VIEWPORT_PADDING_PX - width
      )
    );
    const safeTop = Math.max(
      MENTION_PALETTE_VIEWPORT_PADDING_PX,
      viewportTopInset
    );
    const availableAbove = rect.top - MENTION_PALETTE_GAP_PX - safeTop;
    const height =
      Math.min(MENTION_PALETTE_MAX_HEIGHT_PX, Math.max(0, availableAbove));

    setMentionPaletteFrame({
      height,
      left,
      portalTarget: resolveComposerPortalTarget(anchor),
      top: Math.max(
        safeTop,
        Math.min(
          rect.top - MENTION_PALETTE_GAP_PX - height,
          viewportHeight - MENTION_PALETTE_VIEWPORT_PADDING_PX - height
        )
      ),
      width,
      zIndex: resolveMentionPaletteZIndex(anchor)
    });
  }, [viewportTopInset]);

  useLayoutEffect(() => {
    if (!showFileMentionPalette) {
      setMentionPaletteFrame(null);
      return;
    }

    syncMentionPaletteFrame();
    const anchor = inputShellRef.current;
    const resizeObserver =
      typeof ResizeObserver === "undefined"
        ? null
        : new ResizeObserver(syncMentionPaletteFrame);
    if (anchor) {
      resizeObserver?.observe(anchor);
      const prompt = anchor.querySelector(".agent-gui-node__composer-prompt-input-area");
      if (prompt) resizeObserver?.observe(prompt);
    }
    window.addEventListener("resize", syncMentionPaletteFrame);
    window.addEventListener("scroll", syncMentionPaletteFrame, {
      capture: true,
      passive: true
    });
    return () => {
      resizeObserver?.disconnect();
      window.removeEventListener("resize", syncMentionPaletteFrame);
      window.removeEventListener("scroll", syncMentionPaletteFrame, true);
    };
  }, [showFileMentionPalette, syncMentionPaletteFrame]);

  const mentionPaletteStyle = useMemo<CSSProperties>(
    () => ({
      position: "fixed",
      left: `${mentionPaletteFrame?.left ?? 0}px`,
      bottom: `calc(100% - ${(mentionPaletteFrame?.top ?? 0) + (mentionPaletteFrame?.height ?? 0)}px)`,
      width: `${mentionPaletteFrame?.width ?? 0}px`,
      maxWidth: `${mentionPaletteFrame?.width ?? 0}px`,
      minHeight: "0px",
      maxHeight: `${mentionPaletteFrame?.height ?? MENTION_PALETTE_MAX_HEIGHT_PX}px`,
      height: "auto",
      zIndex: composerPaletteZIndex
    }),
    [mentionPaletteFrame]
  );
  const mentionPaletteHeightPx =
    mentionPaletteFrame?.height ?? MENTION_PALETTE_MIN_HEIGHT_PX;

  return { mentionPaletteFrame, mentionPaletteHeightPx, mentionPaletteStyle };
}
