import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import {
  ArtifactFilters,
  applyArtifactFilter,
  type ArtifactFilterState,
} from "./artifact-filters";

const empty: ArtifactFilterState = { q: "" };

describe("ArtifactFilters UI", () => {
  it("emits the typed query on input", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<ArtifactFilters value={empty} onChange={onChange} />);
    await user.type(screen.getByPlaceholderText(/search artifacts/i), "x");
    expect(onChange).toHaveBeenLastCalledWith({ q: "x" });
  });

  it("toggles a kind chip on click", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<ArtifactFilters value={empty} onChange={onChange} />);
    await user.click(screen.getByRole("button", { name: /report/i }));
    expect(onChange).toHaveBeenCalledWith({ q: "", kind: "report" });
  });

  it("toggles kind off when clicked again", async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(
      <ArtifactFilters
        value={{ q: "", kind: "plan" }}
        onChange={onChange}
      />,
    );
    await user.click(screen.getByRole("button", { name: /plan/i }));
    expect(onChange).toHaveBeenCalledWith({ q: "", kind: undefined });
  });

  it("renders scope chips only when showScope is true", () => {
    const { rerender } = render(
      <ArtifactFilters value={empty} onChange={() => {}} />,
    );
    expect(screen.queryByText("Workspace")).toBeNull();
    rerender(
      <ArtifactFilters value={empty} onChange={() => {}} showScope />,
    );
    expect(screen.getByText("Workspace")).toBeInTheDocument();
    expect(screen.getByText("Project")).toBeInTheDocument();
    expect(screen.getByText("Issue")).toBeInTheDocument();
  });
});

describe("applyArtifactFilter", () => {
  const items = [
    { id: "1", kind: "report", title: "API latency", body: "Findings" },
    { id: "2", kind: "plan", title: "Q2 plan", body: "Tasks" },
    { id: "3", kind: "decision", title: "Use Postgres", body: "ADR" },
  ];

  it("returns all items when filter is empty", () => {
    expect(applyArtifactFilter(items, empty)).toHaveLength(3);
  });

  it("filters by kind", () => {
    const out = applyArtifactFilter(items, { q: "", kind: "plan" });
    expect(out).toHaveLength(1);
    expect(out[0]?.id).toBe("2");
  });

  it("filters by query against title and body", () => {
    expect(applyArtifactFilter(items, { q: "latency" })).toHaveLength(1);
    expect(applyArtifactFilter(items, { q: "findings" })).toHaveLength(1);
    expect(applyArtifactFilter(items, { q: "ADR" })).toHaveLength(1);
  });

  it("combines kind + query", () => {
    expect(
      applyArtifactFilter(items, { q: "tasks", kind: "plan" }),
    ).toHaveLength(1);
    expect(
      applyArtifactFilter(items, { q: "latency", kind: "plan" }),
    ).toHaveLength(0);
  });
});
