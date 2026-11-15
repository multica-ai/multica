import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { ReactElement } from "react";
import type { BatchUpdateIssuesResponse } from "@multica/core/types";
import { BatchOpsUnsupportedError } from "@multica/core/api";
import { renderWithI18n } from "../../test/i18n";
import { useBatchResultToast, type BatchResultToastOptions } from "./batch-result-toast";

const toast = vi.hoisted(() => ({ success: vi.fn(), warning: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

type Reporter = ReturnType<typeof useBatchResultToast>;

function withReporter(): Reporter {
  let reporter: Reporter | undefined;
  function Probe() {
    reporter = useBatchResultToast();
    return null;
  }
  renderWithI18n(<Probe />);
  return reporter!;
}

function report(response: BatchUpdateIssuesResponse, count: number, options?: BatchResultToastOptions) {
  return withReporter().report(response, count, options);
}

const updated = (id: string) => ({ issue_id: id, identifier: id.toUpperCase(), outcome: "updated" as const });
const skipped = (id: string, code: string) => ({
  issue_id: id,
  identifier: id.toUpperCase(),
  outcome: "skipped" as const,
  code,
  message: "server message",
});

beforeEach(() => {
  toast.success.mockClear();
  toast.warning.mockClear();
  toast.error.mockClear();
});

describe("useBatchResultToast", () => {
  it("reports success when every issue took the change", () => {
    expect(report({ updated: 2, results: [updated("a"), updated("b")] }, 2)).toBe(true);
    expect(toast.success).toHaveBeenCalledWith("Updated 2 issues");
    expect(toast.warning).not.toHaveBeenCalled();
  });

  it("stays silent on success when asked to", () => {
    expect(report({ updated: 1, results: [updated("a")] }, 1, { successMessage: null })).toBe(true);
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("names each skipped issue with its reason and offers to select only those", () => {
    const onSelectSkipped = vi.fn();
    const applied = report(
      {
        updated: 1,
        results: [updated("a"), skipped("b", "workflow_rejected"), skipped("c", "something_new")],
      },
      3,
      { onSelectSkipped },
    );

    expect(applied).toBe(false);
    expect(toast.success).not.toHaveBeenCalled();
    const [title, options] = toast.warning.mock.calls[0]! as [
      string,
      { description: ReactElement; action: { label: string; onClick: () => void } },
    ];
    expect(title).toBe("Updated 1, skipped 2 issues");
    renderWithI18n(options.description);
    expect(screen.getByText("B")).toBeInTheDocument();
    expect(screen.getByText("C")).toBeInTheDocument();
    // An unknown code falls back to the server's message.
    expect(screen.getByText("server message")).toBeInTheDocument();

    expect(options.action.label).toBe("Select only these 2");
    options.action.onClick();
    expect(onSelectSkipped).toHaveBeenCalledWith(["b", "c"]);
  });

  it("summarises skips beyond the first three", () => {
    report(
      { updated: 0, results: ["a", "b", "c", "d", "e"].map((id) => skipped(id, "not_found")) },
      5,
    );
    const options = toast.warning.mock.calls[0]![1] as { description: ReactElement; action?: unknown };
    expect(options.action).toBeUndefined();
    renderWithI18n(options.description);
    expect(screen.queryByText("D")).not.toBeInTheDocument();
    expect(screen.getByText("and 2 more")).toBeInTheDocument();
  });

  it("falls back to the count when the server sends no per-issue results", () => {
    expect(report({ updated: 2 }, 2)).toBe(true);
    expect(report({ updated: 1 }, 2)).toBe(false);
    expect(toast.warning).toHaveBeenCalledTimes(1);
  });

  it("explains an outdated server instead of showing a generic failure", () => {
    withReporter().reportError(new BatchOpsUnsupportedError());
    expect(toast.error).toHaveBeenCalledWith(expect.stringMatching(/server/i));
  });
});
