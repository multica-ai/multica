import { useState } from "react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { configStore } from "@multica/core/config";
import type { WebhookEventFilter } from "@multica/core/types";
import { screen, fireEvent, within } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { WebhookEventFilterSection } from "./webhook-event-filter-section";
import {
  draftsFromEventFilters,
  resolveEventFilterDrafts,
  type ResolvedEventFilters,
} from "./webhook-event-filter-draft";

// The editor is controlled by its drafts. The harness owns them the way the
// webhook dialog does and reports what a save would send after every change.
function Harness({
  initial = [],
  showErrors = false,
  onResolved,
}: {
  initial?: WebhookEventFilter[];
  showErrors?: boolean;
  onResolved?: (resolved: ResolvedEventFilters) => void;
}) {
  const [drafts, setDrafts] = useState(() => draftsFromEventFilters(initial));
  return (
    <WebhookEventFilterSection
      drafts={drafts}
      showErrors={showErrors}
      onChange={(next) => {
        setDrafts(next);
        onResolved?.(resolveEventFilterDrafts(next));
      }}
    />
  );
}

function renderEditor(props: Parameters<typeof Harness>[0] = {}, locale?: "zh-Hans" | "zh-Hant") {
  const onResolved = vi.fn<(resolved: ResolvedEventFilters) => void>();
  const result = renderWithI18n(<Harness {...props} onResolved={onResolved} />, locale ? { locale } : undefined);
  const lastResolved = () => onResolved.mock.calls.at(-1)?.[0];
  return { ...result, lastResolved };
}

const type = (el: HTMLElement, value: string) => fireEvent.change(el, { target: { value } });

describe("WebhookEventFilterSection", () => {
  beforeEach(() => configStore.getState().setWebhookPayloadConditionsSupported(true));
  afterEach(() => configStore.getState().setWebhookPayloadConditionsSupported(false));

  it("offers only event filters when an older backend cannot enforce payload conditions", () => {
    configStore.getState().setWebhookPayloadConditionsSupported(false);
    renderEditor({ initial: [{ event: "pull_request" }] });
    expect(screen.queryByRole("button", { name: "Add condition" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add rule" })).toBeInTheDocument();
  });

  // Regression cover: rules and conditions each went through a sub-form with
  // its own "Add", so text left in one was dropped on Save, and a saved rule
  // could only be removed and typed again. Every field is now edited in place.
  it("adds a rule as an editable card and focuses its event", () => {
    const { lastResolved } = renderEditor();

    fireEvent.click(screen.getByRole("button", { name: "Add rule" }));
    const event = screen.getByRole("textbox", { name: "Event" });
    expect(event).toHaveFocus();

    type(event, "workflow_run");
    type(screen.getByRole("textbox", { name: "Actions" }), "completed, failed");

    expect(lastResolved()).toMatchObject({
      valid: true,
      filters: [{ event: "workflow_run", actions: ["completed", "failed"] }],
    });
  });

  it("edits a saved rule in place and keeps its conditions", () => {
    const { lastResolved } = renderEditor({
      initial: [
        {
          event: "pull_request",
          actions: ["opened"],
          conditions: [{ path: "pull_request.base.ref", op: "equals", value: "main" }],
        },
      ],
    });

    const rule = screen.getByRole("group", { name: "pull_request" });
    type(within(rule).getByRole("textbox", { name: "Actions" }), "opened, reopened");

    expect(lastResolved()?.filters).toEqual([
      {
        event: "pull_request",
        actions: ["opened", "reopened"],
        conditions: [{ path: "pull_request.base.ref", op: "equals", value: "main" }],
      },
    ]);
  });

  it("adds a condition row to the rule it was added on", () => {
    const { lastResolved } = renderEditor({
      initial: [{ event: "issues", actions: ["labeled"] }, { event: "push" }],
    });

    fireEvent.click(
      within(screen.getByRole("group", { name: "issues" })).getByRole("button", {
        name: "Add condition",
      }),
    );
    const path = screen.getByRole("textbox", { name: "Payload field" });
    expect(path).toHaveFocus();
    type(path, "label.name");
    type(screen.getByRole("textbox", { name: "Value" }), "agent");

    expect(lastResolved()?.filters).toEqual([
      {
        event: "issues",
        actions: ["labeled"],
        conditions: [{ path: "label.name", op: "equals", value: "agent" }],
      },
      { event: "push" },
    ]);
  });

  it("shows saved values the way they have to be typed to keep their type", () => {
    renderEditor({
      initial: [
        {
          event: "pull_request",
          conditions: [
            { path: "pull_request.draft", op: "equals", value: false },
            { path: "pull_request.title", op: "contains", value: "false" },
            { path: "pull_request.base.ref", op: "in", values: ["main", "release"] },
          ],
        },
      ],
    });

    expect(screen.getAllByRole("textbox", { name: "Value" }).map((el) => (el as HTMLInputElement).value)).toEqual([
      "false",
      '"false"',
      "main, release",
    ]);
  });

  it("removes one condition and drops the key when none are left", () => {
    const { lastResolved } = renderEditor({
      initial: [{ event: "issues", conditions: [{ path: "label.name", op: "equals", value: "agent" }] }],
    });
    fireEvent.click(screen.getByRole("button", { name: "Remove condition" }));
    expect(lastResolved()?.filters).toEqual([{ event: "issues" }]);
  });

  it("drops the rule when remove is clicked", () => {
    const { lastResolved } = renderEditor({ initial: [{ event: "issues" }, { event: "workflow_run" }] });
    fireEvent.click(screen.getAllByRole("button", { name: "Remove rule" })[1]!);
    expect(lastResolved()?.filters).toEqual([{ event: "issues" }]);
  });

  it("marks nothing until a save is attempted", () => {
    renderEditor({ initial: [{ event: "" }] });
    expect(screen.getByRole("textbox", { name: "Event" })).not.toHaveAttribute("aria-invalid");
    expect(screen.queryByText("Enter an event name.")).not.toBeInTheDocument();
  });

  it("marks every incomplete field after a save attempt and focuses the first", () => {
    renderEditor({
      showErrors: true,
      initial: [
        { event: "" },
        { event: "issues", conditions: [{ path: "a..b", op: "equals", value: "x" }] },
      ],
    });

    const [emptyEvent] = screen.getAllByRole("textbox", { name: "Event" });
    expect(emptyEvent).toHaveAttribute("aria-invalid", "true");
    expect(emptyEvent).toHaveFocus();
    expect(emptyEvent).toHaveAccessibleDescription("Enter an event name.");
    expect(screen.getByRole("textbox", { name: "Payload field" })).toHaveAccessibleDescription(
      "Separate field names with dots, up to 256 characters.",
    );
  });

  it.each([
    [
      "zh-Hans",
      `https://multica.ai/docs/zh/autopilots#${encodeURIComponent("事件过滤")}`,
    ],
    [
      "zh-Hant",
      `https://multica.ai/docs/zh-Hant/autopilots#${encodeURIComponent("事件過濾")}`,
    ],
  ] as const)("links the %s event filter docs section", (locale, href) => {
    renderEditor({}, locale);
    expect(screen.getByRole("link")).toHaveAttribute("href", href);
  });
});
