// @vitest-environment jsdom
// FIR-3646 — drag-and-drop round order in the inbox block and in the Manage
// rounds panel. dnd-kit is mocked (jsdom has no layout, so a real pointer drag
// never produces a drop target); the test captures onDragEnd and fires it, which
// is exactly what the browser hands the component after a drop.
import "@testing-library/jest-dom/vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { RoundManager, RoundsBlock } from "./rounds-block";
import type { RoundStatus } from "./schemas";

let lastOnDragEnd: ((event: unknown) => void) | null = null;

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children, onDragEnd }: { children: React.ReactNode; onDragEnd: (event: unknown) => void }) => {
    lastOnDragEnd = onDragEnd;
    return children;
  },
  PointerSensor: class {},
  useSensor: () => ({}),
  useSensors: () => [],
  closestCenter: vi.fn(),
}));

vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => children,
  verticalListSortingStrategy: {},
  // Real implementation — the assertions are about the resulting order.
  arrayMove: <T,>(items: T[], from: number, to: number): T[] => {
    const copy = items.slice();
    const [moved] = copy.splice(from, 1);
    copy.splice(to, 0, moved!);
    return copy;
  },
  useSortable: () => ({
    attributes: {},
    listeners: {},
    setNodeRef: vi.fn(),
    setActivatorNodeRef: vi.fn(),
    transform: null,
    transition: null,
    isDragging: false,
  }),
}));

vi.mock("@dnd-kit/utilities", () => ({ CSS: { Transform: { toString: () => undefined } } }));

beforeAll(() => {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn(),
  })) as unknown as typeof window.matchMedia;
});

afterEach(() => {
  cleanup();
  lastOnDragEnd = null;
});

const round = (id: string, name: string): RoundStatus => ({
  round: { id, workspace_id: "ws", owner_id: "owner", name, created_at: "", updated_at: "" },
  members: [],
  active_cycle: null,
});

const statuses = [round("r1", "Daily"), round("r2", "Weekly"), round("r3", "Monthly")];

const blockProps = {
  issueTitles: {},
  onStart: vi.fn(),
  onPause: vi.fn(),
  onSelectIssue: vi.fn(),
};

describe("RoundsBlock round order", () => {
  it("reports the dropped order when a round is dragged in the inbox block", () => {
    const onReorder = vi.fn();
    render(<RoundsBlock statuses={statuses} {...blockProps} onReorder={onReorder} />);

    expect(screen.getByRole("button", { name: "Drag to reorder Daily" })).toBeInTheDocument();
    lastOnDragEnd?.({ active: { id: "r3" }, over: { id: "r1" } });

    expect(onReorder).toHaveBeenCalledWith(["r3", "r1", "r2"]);
  });

  it("ignores a drop back onto the same round", () => {
    const onReorder = vi.fn();
    render(<RoundsBlock statuses={statuses} {...blockProps} onReorder={onReorder} />);

    lastOnDragEnd?.({ active: { id: "r2" }, over: { id: "r2" } });
    lastOnDragEnd?.({ active: { id: "r2" }, over: null });

    expect(onReorder).not.toHaveBeenCalled();
  });

  it("offers no grip when reordering is unavailable or there is a single round", () => {
    const { rerender } = render(<RoundsBlock statuses={statuses} {...blockProps} />);
    expect(screen.queryByRole("button", { name: /^Drag to reorder/ })).not.toBeInTheDocument();

    rerender(<RoundsBlock statuses={[round("r1", "Daily")]} {...blockProps} onReorder={vi.fn()} />);
    expect(screen.queryByRole("button", { name: /^Drag to reorder/ })).not.toBeInTheDocument();
  });
});

describe("Manage rounds panel round order", () => {
  it("reports the dropped order when a round is dragged in settings", () => {
    const onReorder = vi.fn();
    render(
      <RoundManager
        statuses={statuses}
        issueTitles={{}}
        onCreate={vi.fn()}
        onUpdate={vi.fn()}
        onDelete={vi.fn()}
        onRemoveMember={vi.fn()}
        onReorder={onReorder}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Manage rounds" }));
    expect(screen.getByRole("button", { name: "Drag to reorder Weekly" })).toBeInTheDocument();

    lastOnDragEnd?.({ active: { id: "r1" }, over: { id: "r3" } });

    expect(onReorder).toHaveBeenCalledWith(["r2", "r3", "r1"]);
  });
});
