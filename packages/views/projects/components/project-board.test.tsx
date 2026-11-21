import { describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { ScrollRestorationProvider } from "../../platform/scroll-restoration";
import { ProjectBoard, type ProjectBoardColumn } from "./project-board";

const columns: ProjectBoardColumn[] = Array.from({ length: 6 }, (_, index) => ({
  key: String(index),
  label: `Column ${index + 1}`,
  projects: [],
}));

function board() {
  return (
    <ProjectBoard
      columns={columns}
      pinnedIds={new Set()}
      mayDelete={() => false}
      onMove={vi.fn()}
      onUpdate={vi.fn()}
    />
  );
}

describe("ProjectBoard scroll restoration", () => {
  it("restores the column strip from its own capture key", () => {
    const { container } = renderWithI18n(
      <ScrollRestorationProvider
        adapter={{
          get: (key) =>
            key === "project-board"
              ? { top: 0, height: 800, left: 640 }
              : { top: 0, height: 800, left: 120 },
        }}
      >
        {board()}
      </ScrollRestorationProvider>,
    );

    const strip = container.querySelector<HTMLElement>("[data-board-scroller]")!;
    expect(strip.scrollLeft).toBe(640);
    expect(strip).toHaveAttribute("data-tab-scroll-root", "project-board");
    expect(strip.scrollTop).toBe(0);
  });

  it("starts at the left edge when this route has no saved entry", () => {
    const { container } = renderWithI18n(
      <ScrollRestorationProvider
        adapter={{
          get: (key) =>
            key === "board" ? { top: 0, height: 800, left: 640 } : undefined,
        }}
      >
        {board()}
      </ScrollRestorationProvider>,
    );

    expect(container.querySelector<HTMLElement>("[data-board-scroller]")!.scrollLeft).toBe(0);
  });
});
