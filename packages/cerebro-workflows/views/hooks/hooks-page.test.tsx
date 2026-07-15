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
    expect(screen.getAllByText("Trigger → 3 conditions → Block → 1 action")).toHaveLength(4);
    expect(screen.getByText("Off")).toBeInTheDocument();
    expect(screen.getByText("Dry run")).toBeInTheDocument();
    expect(screen.getByText("Enforced")).toBeInTheDocument();
    expect(screen.getByText("Managed")).toBeInTheDocument();
    expect(screen.getAllByText("Never")).toHaveLength(4);
  });
});
