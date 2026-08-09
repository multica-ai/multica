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
    expect(screen.getAllByText("When Choose a trigger for Choose what this applies to, if no conditions, Guide (let it continue), then No follow-up action.")).toHaveLength(4);
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
    expect(screen.getByRole("heading", { name: "Hooks" }).parentElement?.parentElement).not.toHaveClass("flex-wrap");
  });

  it("opens the stable family route and distinguishes Live with Draft changes", async () => {
    const onOpenHook = vi.fn();
    const base = createHookDraft();
    render(<HooksPage onOpenHook={onOpenHook} hooks={[{
      ...base,
      id: "draft-r3",
      family_id: "family-1",
      name: "Guard completion",
      lifecycle: {
        state: "live_with_draft",
        live_policy_id: "live-v4",
        live_version: 4,
        draft_id: "draft-r3",
        draft_revision: 3,
        live_unchanged_by_draft: true,
      },
    }]} />);

    expect(screen.getByText("Enforced · Draft changes")).toBeInTheDocument();
    screen.getByRole("button", { name: /Guard completion/ }).click();
    expect(onOpenHook).toHaveBeenCalledWith("family-1");
  });

  it("reports malformed list records without hiding valid Hooks", () => {
    const base = createHookDraft();
    render(<HooksPage
      onOpenHook={vi.fn()}
      hooks={[{ ...base, id: "valid", name: "Valid Hook" }]}
      partialErrors={[{ record_id: "broken", code: "hook_record_malformed" }]}
    />);

    expect(screen.getByText("Valid Hook")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("1 Hook could not be displayed");
  });

  it("renders a truncated safe summary with resolved labels on each card", () => {
    const base = createHookDraft();
    const { container } = render(<HooksPage
      onOpenHook={vi.fn()}
      directory={{ issue: [{ value: "private-issue-id", label: "Returns launch" }] }}
      hooks={[{
        ...base,
        id: "safe",
        name: "Safe Hook",
        events: ["before.task.complete"],
        bindings: [{ kind: "issue", value: "private-issue-id" }],
        actions: [{ type: "issue.comment", label: "Comment on issue", config: { body: "Private message" } }],
      }]}
    />);

    const summary = container.querySelector("span[title]");
    expect(summary).toHaveClass("truncate");
    expect(summary).toHaveTextContent("Returns launch");
    expect(summary).toHaveTextContent("<redacted>");
    expect(summary).not.toHaveTextContent("private-issue-id");
    expect(summary).not.toHaveTextContent("Private message");
  });
});
