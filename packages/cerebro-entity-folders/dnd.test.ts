import { describe, expect, it } from "vitest";
import type { DragEvent } from "react";
import {
  ENTITY_FOLDER_DND_MIME,
  hasEntityFolderDragData,
  readEntityFolderDragData,
  setEntityFolderDragData,
} from "./dnd";

// Minimal DataTransfer stand-in — jsdom's is fine but we keep the test
// environment-agnostic (this package runs in vitest's node env).
function makeDataTransfer() {
  const store = new Map<string, string>();
  return {
    effectAllowed: "" as string,
    dropEffect: "" as string,
    get types() {
      return Array.from(store.keys());
    },
    setData(type: string, value: string) {
      store.set(type, value);
    },
    getData(type: string) {
      return store.get(type) ?? "";
    },
  };
}

function makeDragEvent() {
  const dataTransfer = makeDataTransfer();
  return { dataTransfer } as unknown as DragEvent & {
    dataTransfer: ReturnType<typeof makeDataTransfer>;
  };
}

describe("entity-folder drag-and-drop payload", () => {
  it("round-trips kind + itemId for the matching kind", () => {
    const e = makeDragEvent();
    setEntityFolderDragData(e, "skill", "skill-123");

    expect(e.dataTransfer.effectAllowed).toBe("move");
    expect(hasEntityFolderDragData(e)).toBe(true);
    expect(readEntityFolderDragData(e, "skill")).toBe("skill-123");
  });

  it("refuses a payload dragged across kinds (skill onto an autopilot folder)", () => {
    const e = makeDragEvent();
    setEntityFolderDragData(e, "skill", "skill-123");

    expect(readEntityFolderDragData(e, "autopilot")).toBeNull();
  });

  it("preserves an itemId that contains a colon", () => {
    const e = makeDragEvent();
    setEntityFolderDragData(e, "autopilot", "ap:with:colons");

    expect(readEntityFolderDragData(e, "autopilot")).toBe("ap:with:colons");
  });

  it("reports no payload for an unrelated drag", () => {
    const e = makeDragEvent();
    e.dataTransfer.setData("text/plain", "hello");

    expect(hasEntityFolderDragData(e)).toBe(false);
    expect(readEntityFolderDragData(e, "skill")).toBeNull();
  });

  it("uses a namespaced MIME type so it can't collide with plain text drags", () => {
    expect(ENTITY_FOLDER_DND_MIME).toContain("cerebro");
  });
});
