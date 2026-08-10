import { dropLanding, type DropBlock } from "@multica/cerebro-editor-image";

interface DropNode {
  nodeSize: number;
}

interface DropEditor {
  state: {
    doc: {
      forEach: (callback: (node: DropNode, offset: number) => void) => void;
    };
  };
  view: {
    nodeDOM: (pos: number) => Node | null;
  };
}

export interface EditorDropLanding {
  position: number;
  lineY: number;
}

/** Resolve the visual landing line and exact top-level document position. */
export function editorImageDropLanding(
  editor: DropEditor,
  pointerY: number,
): EditorDropLanding | null {
  const blocks: DropBlock[] = [];
  const positions: Array<{ above: number; below: number }> = [];
  editor.state.doc.forEach((node, offset) => {
    const dom = editor.view.nodeDOM(offset) as
      | (Node & { getBoundingClientRect?: () => { top: number; bottom: number } })
      | null;
    const rect = dom?.getBoundingClientRect?.();
    if (!rect) return;
    blocks.push({ top: rect.top, bottom: rect.bottom });
    positions.push({ above: offset, below: offset + node.nodeSize });
  });
  const landing = dropLanding(pointerY, blocks);
  if (!landing) return null;
  const position = positions[landing.blockIndex];
  if (!position) return null;
  return {
    position: landing.side === "above" ? position.above : position.below,
    lineY: landing.lineY,
  };
}
