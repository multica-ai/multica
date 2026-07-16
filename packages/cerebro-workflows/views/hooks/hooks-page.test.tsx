// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { createHookDraft } from "../../core/hook-types";
import { HooksPage } from "./hooks-page";

afterEach(cleanup);

describe("HooksPage", () => {
  it("shows one-line chains, last run, and all four states", () => {
    const base = createHookDraft();
    render(<HooksPage onOpenHook={vi.fn()} hooks={[
      { ...base, id: "off", name: "Off policy", mode: "off" },
      { ...base, id: "dry", name: "Dry policy", mode: "dry_run" },
      { ...base, id: "on", name: "Live policy", mode: "enforce" },
      { ...base, id: "managed", name: "Managed policy", mode: "managed" },
    ]} />);
    expect(screen.getAllByText("Choose trigger → 0 conditions → Allow → 0 actions")).toHaveLength(4);
    expect(screen.getByText("Off")).toBeInTheDocument();
    expect(screen.getByText("Dry run")).toBeInTheDocument();
    expect(screen.getByText("Enforced")).toBeInTheDocument();
    expect(screen.getByText("Managed")).toBeInTheDocument();
    expect(screen.getAllByText("Never")).toHaveLength(4);
  });

  it("uses the shared page header and a responsive list without a wide table", () => {
    const { container } = render(<HooksPage onOpenHook={vi.fn()} hooks={[]} />);
    expect(screen.getByRole("heading", { name: "Hooks" })).toBeInTheDocument();
    expect(container.querySelector("table")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New hook" })).toBeInTheDocument();
  });
});
