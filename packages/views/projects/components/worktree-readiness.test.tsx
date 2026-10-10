// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, screen } from "@testing-library/react";
import type { WorktreeReadiness } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { WorktreeReadinessNotice } from "./worktree-readiness";

const measurement: WorktreeReadiness = {
  status: "blocked", reason_code: "untracked_limit",
  checked_at: "2026-10-06T00:00:00Z", expires_at: "2026-10-06T00:01:30Z",
  file_count: 234, total_bytes: 1258291200, max_files: 2000, max_bytes: 209715200,
  symlink_count: 0,
  largest_paths: [{ path: "debug", file_count: 234, total_bytes: 1258291200 }],
};
function clock() {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-10-06T00:00:00Z"));
}
afterEach(() => vi.useRealTimers());

describe("worktree readiness notice", () => {
  it("shows measured limits, largest paths, and cleanup without a retry action", () => {
    clock();
    renderWithI18n(<WorktreeReadinessNotice readiness={measurement} />);
    expect(screen.getByRole("status")).toHaveTextContent("Worktree cleanup required");
    expect(screen.getByText("Regular files: 234 / 2,000 limit")).toBeVisible();
    expect(screen.getByText(/Size: 1,258,291,200 bytes/)).toHaveTextContent("209,715,200 bytes (200 MiB) limit");
    expect(screen.getByText("debug")).toBeVisible();
    expect(screen.getByText(/Queued runs resume automatically/)).toBeVisible();
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
  it("clears the blocker and cleanup guidance when a fresh ready report arrives", () => {
    clock();
    const { rerender } = renderWithI18n(<WorktreeReadinessNotice readiness={measurement} />);
    rerender(<WorktreeReadinessNotice readiness={{ ...measurement, status: "ready", file_count: 0, total_bytes: 0, largest_paths: [] }} />);
    expect(screen.getByRole("status")).toHaveTextContent("Ready for worktree runs");
    expect(screen.queryByText(/Add the offending/)).not.toBeInTheDocument();
    expect(screen.queryByText("debug")).not.toBeInTheDocument();
  });
  it("expires cached ready data without requiring a successful fetch", () => {
    clock();
    renderWithI18n(<WorktreeReadinessNotice readiness={{ ...measurement, status: "ready" }} />);
    act(() => vi.advanceTimersByTime(91000));
    expect(screen.getByRole("status")).toHaveTextContent("unavailable");
    expect(screen.getByText("Last measured usage")).toBeVisible();
    expect(screen.getByText(/measurement has expired/)).toBeVisible();
  });
  it("treats a failed refresh as unavailable, preserving only last-measured usage", () => {
    clock();
    renderWithI18n(<WorktreeReadinessNotice readiness={{ ...measurement, status: "ready" }} unavailable />);
    expect(screen.getByRole("status")).toHaveTextContent("unavailable");
    expect(screen.getByText("Last measured usage")).toBeVisible();
  });
  it("does not reuse measurements for an unsaved configuration", () => {
    clock();
    renderWithI18n(<WorktreeReadinessNotice readiness={measurement} saved={false} />);
    expect(screen.getByRole("status")).toHaveTextContent("unchecked");
    expect(screen.getByText(/Save this configuration/)).toBeVisible();
    expect(screen.queryByText(/Regular files:/)).not.toBeInTheDocument();
  });
  it.each([undefined, { status: "checking" } as WorktreeReadiness, { status: "unavailable" } as WorktreeReadiness])("does not invent zero usage: %j", (readiness) => {
    renderWithI18n(<WorktreeReadinessNotice readiness={readiness} />);
    expect(screen.queryByText(/Regular files:/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Size:/)).not.toBeInTheDocument();
  });
  it("explains symlink-only blocks even below file and byte limits", () => {
    clock();
    renderWithI18n(<WorktreeReadinessNotice readiness={{ ...measurement, file_count: 0, total_bytes: 0, largest_paths: [], symlink_count: 1, reason_code: "untracked_symlinks" }} />);
    expect(screen.getByText(/Non-ignored symlinks: 1/)).toBeVisible();
  });
});
