import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactElement } from "react";
import type { BatchUpdateIssuesResponse } from "@multica/core/types";
import { BatchOpsUnsupportedError } from "@multica/core/api";
import { renderWithI18n } from "../../test/i18n";
import { useBatchResultToast, type BatchResultToastOptions } from "./batch-result-toast";

const toast = vi.hoisted(() => ({ success: vi.fn(), warning: vi.fn(), error: vi.fn(), dismiss: vi.fn() }));
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
    const [title, options] = toast.warning.mock.calls[0]! as [string, { id: string; description: ReactElement }];
    expect(title).toBe("Updated 1, skipped 2 issues");
    // Toast content renders outside the app's providers.
    render(options.description);
    expect(screen.getByText("B")).toBeInTheDocument();
    expect(screen.getByText("Its project's workflow doesn't allow this change")).toBeInTheDocument();
    // An unknown code falls back to the server's message.
    expect(screen.getByText("server message")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Select only these 2" }));
    expect(onSelectSkipped).toHaveBeenCalledWith(["b", "c"]);
    expect(toast.dismiss).toHaveBeenCalledWith(options.id);
  });

  it("names the single skipped issue in the select action", () => {
    report({ updated: 1, results: [updated("a"), skipped("b", "not_found")] }, 2, { onSelectSkipped: vi.fn() });
    render((toast.warning.mock.calls[0]![1] as { description: ReactElement }).description);
    expect(screen.getByRole("button", { name: "Select only this issue" })).toBeInTheDocument();
  });

  it("summarises skips beyond the first three", () => {
    report(
      { updated: 0, results: ["a", "b", "c", "d", "e"].map((id) => skipped(id, "not_found")) },
      5,
    );
    const options = toast.warning.mock.calls[0]![1] as { description: ReactElement };
    render(options.description);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
    expect(screen.queryByText("D")).not.toBeInTheDocument();
    expect(screen.getByText("and 2 more")).toBeInTheDocument();
  });

  it("falls back to the count when the server sends no per-issue results", () => {
    expect(report({ updated: 2 }, 2)).toBe(true);
    expect(report({ updated: 1 }, 2)).toBe(false);
    expect(toast.warning).toHaveBeenCalledTimes(1);
  });

  it("warns that the result is unknown when the response could not be read", () => {
    expect(report({ updated: 0, unconfirmed: true }, 2)).toBe(false);
    expect(toast.success).not.toHaveBeenCalled();
    expect(toast.warning).toHaveBeenCalledWith(expect.stringMatching(/Couldn't confirm/));
  });

  it("explains an outdated server instead of showing a generic failure", () => {
    withReporter().reportError(new BatchOpsUnsupportedError());
    expect(toast.error).toHaveBeenCalledWith(expect.stringMatching(/server/i));
  });
});
