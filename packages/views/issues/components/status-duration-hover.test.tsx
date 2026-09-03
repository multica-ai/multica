import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { useQuery } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";
import { StatusDurationBody, StatusDurationHover } from "./status-duration-hover";

vi.mock("@tanstack/react-query", () => ({
  useQuery: vi.fn(),
}));

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  // The catalog resolves custom keys to human names. Stubbed so the suite can
  // assert the fallback path (an archived key the catalog no longer knows)
  // without standing up a real workspace catalog.
  useIssueStatuses: () => ({
    statuses: [],
    activeStatuses: [],
    categoryOf: (key: string) => key,
    colorOf: () => null,
    labelOf: (key: string) => key,
    entryOf: (key: string) =>
      key === "code_review" ? { name: "Code Review" } : undefined,
    inCategory: () => [],
    isLoaded: true,
  }),
}));

vi.mock("./status-icon", () => ({
  StatusIcon: ({ status }: { status: string }) => (
    <svg data-testid="status-icon" data-status={status} />
  ),
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueStatusDurationsOptions: (issueId: string) => ({
    queryKey: ["status-durations", issueId],
  }),
}));

const mockUseQuery = vi.mocked(useQuery);

type Entry = { status: string; seconds: number; current: boolean };

function mockDurations(
  state:
    | { phase: "pending" }
    | { phase: "error" }
    | {
        phase: "success";
        entries: Entry[];
        partial?: boolean;
        computedAt?: string;
      },
): void {
  mockUseQuery.mockImplementation(
    () =>
      ({
        data:
          state.phase === "success"
            ? {
                entries: state.entries,
                partial: state.partial ?? false,
                computed_at: state.computedAt ?? new Date().toISOString(),
              }
            : undefined,
        isPending: state.phase === "pending",
        isError: state.phase === "error",
      }) as unknown as ReturnType<typeof useQuery>,
  );
}

function renderBody(): void {
  renderWithI18n(<StatusDurationBody issueId="issue-1" wsId="workspace-1" />);
}

/** The duration cells, in render order. */
function durations(): string[] {
  return screen
    .getAllByTestId("status-icon")
    .map((icon) => icon.parentElement?.lastElementChild?.textContent ?? "");
}

beforeEach(() => {
  vi.clearAllMocks();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("StatusDurationBody", () => {
  it("renders one row per status with a compact duration", () => {
    mockDurations({
      phase: "success",
      entries: [
        { status: "backlog", seconds: 9, current: false },
        { status: "in_progress", seconds: 56 * 60, current: false },
        { status: "code_review", seconds: 11 * 86400, current: true },
      ],
    });
    renderBody();

    expect(screen.getAllByTestId("status-icon")).toHaveLength(3);
    expect(durations()).toEqual(["9s", "56min", "11d"]);
  });

  it("preserves server order, which is chronological rather than by size", () => {
    // The list has to read as the issue's history top-to-bottom. Re-sorting by
    // duration client-side would make rows jump as the current one grows.
    mockDurations({
      phase: "success",
      entries: [
        { status: "backlog", seconds: 9, current: false },
        { status: "in_progress", seconds: 11 * 86400, current: false },
        { status: "code_review", seconds: 60, current: true },
      ],
    });
    renderBody();

    expect(
      screen.getAllByTestId("status-icon").map((i) => i.dataset.status),
    ).toEqual(["backlog", "in_progress", "code_review"]);
  });

  it("resolves catalog names and falls back to the raw key for unknown ones", () => {
    // A status can be archived out of the catalog while issues that visited it
    // still report time against it. The row must survive with its raw key
    // rather than rendering blank.
    mockDurations({
      phase: "success",
      entries: [
        { status: "code_review", seconds: 60, current: false },
        { status: "retired_status", seconds: 60, current: true },
      ],
    });
    renderBody();

    expect(screen.getByText("Code Review")).toBeTruthy();
    expect(screen.getByText("retired_status")).toBeTruthy();
  });

  it("shows a skeleton while pending and never translation keys", () => {
    mockDurations({ phase: "pending" });
    renderBody();

    expect(screen.getByTestId("status-duration-skeleton")).toBeTruthy();
    expect(screen.queryByText(/status_duration/)).toBeNull();
  });

  it("degrades to one message for both error and empty responses", () => {
    mockDurations({ phase: "error" });
    const { unmount } = renderWithI18n(
      <StatusDurationBody issueId="issue-1" wsId="workspace-1" />,
    );
    expect(screen.getByText("No status history yet")).toBeTruthy();
    unmount();

    mockDurations({ phase: "success", entries: [] });
    renderBody();
    expect(screen.getByText("No status history yet")).toBeTruthy();
  });

  it("notes when the aggregate is a reconstruction rather than recorded history", () => {
    mockDurations({
      phase: "success",
      entries: [{ status: "backlog", seconds: 120, current: true }],
      partial: true,
    });
    renderBody();

    expect(
      screen.getByText(
        "No transitions recorded — showing time since the issue was created.",
      ),
    ).toBeTruthy();
  });

  it("omits the note when real history exists", () => {
    mockDurations({
      phase: "success",
      entries: [{ status: "backlog", seconds: 120, current: true }],
      partial: false,
    });
    renderBody();

    expect(screen.queryByText(/No transitions recorded/)).toBeNull();
  });

  it("adds request-latency drift to the current status only", () => {
    // The server closed the open segment 30s before this render. The current
    // row must already account for that on first paint; closed segments are
    // final and must not move.
    const computedAt = new Date(Date.now() - 30_000).toISOString();
    mockDurations({
      phase: "success",
      entries: [
        { status: "backlog", seconds: 100, current: false },
        { status: "code_review", seconds: 100, current: true },
      ],
      computedAt,
    });
    renderBody();

    expect(durations()).toEqual(["1min", "2min"]);
  });

  it("discards an implausible clock skew instead of adding it", () => {
    // A client clock hours behind the server would otherwise credit the
    // current status with hours it never held.
    const computedAt = new Date(Date.now() - 6 * 3600 * 1000).toISOString();
    mockDurations({
      phase: "success",
      entries: [{ status: "code_review", seconds: 60, current: true }],
      computedAt,
    });
    renderBody();

    expect(durations()).toEqual(["1min"]);
  });

  it("ignores a client clock ahead of the server rather than going backwards", () => {
    const computedAt = new Date(Date.now() + 60_000).toISOString();
    mockDurations({
      phase: "success",
      entries: [{ status: "code_review", seconds: 120, current: true }],
      computedAt,
    });
    renderBody();

    expect(durations()).toEqual(["2min"]);
  });

  it("survives a malformed computed_at", () => {
    mockDurations({
      phase: "success",
      entries: [{ status: "code_review", seconds: 120, current: true }],
      computedAt: "not-a-date",
    });
    renderBody();

    expect(durations()).toEqual(["2min"]);
  });

  it("localizes units", () => {
    mockDurations({
      phase: "success",
      entries: [
        { status: "backlog", seconds: 9, current: false },
        { status: "code_review", seconds: 11 * 86400, current: true },
      ],
    });
    renderWithI18n(<StatusDurationBody issueId="issue-1" wsId="workspace-1" />, {
      locale: "zh-Hans",
    });

    expect(durations()).toEqual(["9 秒", "11 天"]);
  });
});

describe("StatusDurationHover trigger", () => {
  function renderHover(disabled = false) {
    return renderWithI18n(
      <StatusDurationHover
        issueId="issue-1"
        wsId="workspace-1"
        disabled={disabled}
        delay={0}
      >
        <button type="button">Code Review</button>
      </StatusDurationHover>,
    );
  }

  it("renders its children as the trigger", () => {
    mockDurations({ phase: "success", entries: [] });
    renderHover();

    expect(screen.getByRole("button", { name: "Code Review" })).toBeTruthy();
  });

  it("does not fetch until the card is opened", () => {
    // The whole laziness claim: mounting an issue detail must not cost a
    // status-durations request. Base UI only mounts the popup subtree (where
    // the query lives) once the card opens, so a closed card issues nothing.
    mockDurations({ phase: "success", entries: [] });
    renderHover();

    expect(mockUseQuery).not.toHaveBeenCalled();
  });

  it("fetches and renders rows once hovered", async () => {
    const user = userEvent.setup();
    mockDurations({
      phase: "success",
      entries: [{ status: "code_review", seconds: 11 * 86400, current: true }],
    });
    renderHover();

    await user.hover(screen.getByRole("button", { name: "Code Review" }));

    await screen.findByText("Time in status");
    expect(mockUseQuery).toHaveBeenCalled();
    expect(screen.getByText("11d")).toBeTruthy();
  });

  it("stays shut while the status picker owns the row", async () => {
    // Two layers anchored to the same row read as a glitch, and a user who
    // just clicked to CHANGE the status is not asking how long it has been
    // there. Nothing should be fetched either.
    const user = userEvent.setup();
    mockDurations({ phase: "success", entries: [] });
    renderHover(true);

    await user.hover(screen.getByRole("button", { name: "Code Review" }));

    expect(screen.queryByText("Time in status")).toBeNull();
    expect(mockUseQuery).not.toHaveBeenCalled();
  });
});
