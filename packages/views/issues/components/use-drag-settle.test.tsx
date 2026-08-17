/**
 * @vitest-environment jsdom
 */
import { describe, it, expect } from "vitest";
import { useEffect } from "react";
import { render, act } from "@testing-library/react";
import { useDragSettle } from "./use-drag-settle";

describe("useDragSettle", () => {
  // MUL-4985 defense-in-depth: the board/list/swimlane resync effect calls
  // `setColumns(buildColumns(...))` whenever its `groups` input changes
  // identity. When `groups` churns a fresh-but-content-equal value every render
  // (the cold-load failure mode), an unguarded setter allocated a new column
  // object each render and spun into "Maximum update depth exceeded". The
  // equality guard returns the previous reference for a content-equal rebuild,
  // so React bails and the loop cannot start.
  it("does not loop when a resync effect rebuilds a content-equal column map every render", () => {
    let renders = 0;
    function Harness() {
      renders++;
      const { columns, setColumns } = useDragSettle(() => ({ todo: ["a", "b"] }));
      // A fresh object of identical content on every render — what an unstable
      // `groups` fed through buildColumns produces.
      useEffect(() => {
        setColumns({ todo: ["a", "b"] });
      });
      return <div>{Object.keys(columns).join(",")}</div>;
    }

    expect(() => render(<Harness />)).not.toThrow();
    // A guarded setter settles in one or two renders; an unguarded loop is in
    // the hundreds before React throws. Bound generously to stay robust.
    expect(renders).toBeLessThan(10);
  });

  it("still applies a content-changed column update", () => {
    function Harness({ next }: { next: Record<string, string[]> }) {
      const { columns, setColumns } = useDragSettle(() => ({ todo: ["a"] }));
      useEffect(() => {
        setColumns(next);
      }, [next, setColumns]);
      return <div data-testid="cols">{(columns.todo ?? []).join(",")}</div>;
    }

    const { getByTestId, rerender } = render(<Harness next={{ todo: ["a"] }} />);
    expect(getByTestId("cols").textContent).toBe("a");

    act(() => {
      rerender(<Harness next={{ todo: ["a", "b"] }} />);
    });
    expect(getByTestId("cols").textContent).toBe("a,b");
  });
});
