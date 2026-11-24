"use client";

/**
 * MermaidBlock — a fenced ```mermaid block rendered in place, inside the shared
 * DynamicBlock frame (MUL-7649). The diagram, its viewer and its layout cache
 * stay in MermaidDiagram; the frame takes the title bar, source view, copy,
 * fullscreen and the error panel. The editor's NodeView draws it too, with its
 * own editable source (`editing`, MUL-9760).
 */

import { useMemo, useRef, useState } from "react";
import {
  DYNAMIC_BLOCK_CHROME_PX,
  DYNAMIC_BLOCK_COLLAPSE_AT_PX,
  DynamicBlock,
  useValueWhileVisible,
  type DynamicBlockEditing,
} from "./dynamic-block";
import {
  MERMAID_SKELETON_HEIGHT_PX,
  MermaidDiagram,
  framedMermaidBodyHeightPx,
  type MermaidFrameBinding,
} from "./mermaid-diagram";

function blockHeightPx(bodyHeight: number): number {
  return DYNAMIC_BLOCK_CHROME_PX + Math.min(bodyHeight, DYNAMIC_BLOCK_COLLAPSE_AT_PX);
}

/** Reserved height before any render: the title bar plus the skeleton body. */
export const MERMAID_BLOCK_DEFAULT_RESERVED_PX = blockHeightPx(MERMAID_SKELETON_HEIGHT_PX);

/**
 * Height the near-viewport lazy shell reserves for this chart: its collapsed
 * block height when it already rendered in this session. NOT safe to call
 * during render; see framedMermaidBodyHeightPx.
 */
export function reservedMermaidBlockHeightPx(chart: string): number {
  return blockHeightPx(framedMermaidBodyHeightPx(chart));
}

export function MermaidBlock({
  chart: source,
  title,
  editing,
}: {
  chart: string;
  title?: string | null;
  /** Set by the editor's NodeView. */
  editing?: DynamicBlockEditing;
}) {
  const [viewerOpen, setViewerOpen] = useState(false);
  // While the editor shows the source, the hidden diagram is not redrawn on
  // every pause; it catches up when shown or opened in the viewer.
  const chart = useValueWhileVisible(
    source,
    !editing || editing.view === "preview" || viewerOpen,
  );
  const [error, setError] = useState<string | null>(null);
  const fullscreenButtonRef = useRef<HTMLButtonElement>(null);

  const frame = useMemo<MermaidFrameBinding>(
    () => ({
      viewerOpen,
      onViewerOpenChange: setViewerOpen,
      onErrorChange: setError,
      finalFocusRef: fullscreenButtonRef,
    }),
    [viewerOpen],
  );

  return (
    <DynamicBlock
      kind="mermaid"
      title={title}
      source={source}
      error={error == null ? null : { message: error }}
      // A diagram that did not draw has nothing to blow up.
      onFullscreen={error == null ? () => setViewerOpen(true) : undefined}
      fullscreenButtonRef={fullscreenButtonRef}
      editing={editing}
      preview={() => <MermaidDiagram chart={chart} frame={frame} />}
    />
  );
}
