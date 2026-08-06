import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { ReactElement } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AskUserQuestionMeta } from "@multica/core/types";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { issues: enIssues } };

const { mutateMock } = vi.hoisted(() => ({ mutateMock: vi.fn() }));

vi.mock("@multica/core/issues/mutations", () => ({
  useAnswerAskUserQuestion: () => ({ mutate: mutateMock, isPending: false }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) => (id === "u-target" ? "Alice" : id),
  }),
}));

import { AskUserQuestionCard } from "./ask-user-question-card";

function meta(overrides: Partial<AskUserQuestionMeta> = {}): AskUserQuestionMeta {
  return {
    target_user: "u-target",
    source_user: "a-agent",
    question: "Which cache?",
    options: [
      { label: "Redis", description: "distributed" },
      { label: "Local", description: "single-node" },
    ],
    answer: null,
    ...overrides,
  };
}

function renderCard(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>{ui}</QueryClientProvider>
    </I18nProvider>,
  );
}

beforeEach(() => vi.clearAllMocks());
afterEach(() => vi.restoreAllMocks());

describe("AskUserQuestionCard", () => {
  it("shows Confirm/Ignore for the target user and submits the selected option", () => {
    renderCard(
      <AskUserQuestionCard
        issueId="i1"
        commentId="c1"
        meta={meta()}
        currentUserId="u-target"
      />,
    );
    // Question + both option labels render.
    expect(screen.getByText("Which cache?")).toBeTruthy();
    expect(screen.getByText("Redis")).toBeTruthy();
    expect(screen.getByText("Local")).toBeTruthy();

    // Confirm is disabled until a selection is made.
    const confirm = screen.getByText("Confirm").closest("button")!;
    expect(confirm.disabled).toBe(true);

    // Select the second option, then confirm.
    fireEvent.click(screen.getByText("Local"));
    fireEvent.click(screen.getByText("Confirm").closest("button")!);
    expect(mutateMock).toHaveBeenCalledTimes(1);
    expect(mutateMock.mock.calls[0]![0]).toMatchObject({
      commentId: "c1",
      state: "submitted",
      selectedIndex: 1,
    });
  });

  it("ignores without a selection", () => {
    renderCard(
      <AskUserQuestionCard issueId="i1" commentId="c1" meta={meta()} currentUserId="u-target" />,
    );
    fireEvent.click(screen.getByText("Ignore").closest("button")!);
    expect(mutateMock.mock.calls[0]![0]).toMatchObject({ commentId: "c1", state: "ignored" });
  });

  it("is read-only for a non-target user (no action buttons)", () => {
    renderCard(
      <AskUserQuestionCard issueId="i1" commentId="c1" meta={meta()} currentUserId="someone-else" />,
    );
    expect(screen.queryByText("Confirm")).toBeNull();
    expect(screen.queryByText("Ignore")).toBeNull();
    // Question still visible in read-only mode.
    expect(screen.getByText("Which cache?")).toBeTruthy();
  });

  it("renders the terminal 'Selected' chip and no buttons once submitted", () => {
    renderCard(
      <AskUserQuestionCard
        issueId="i1"
        commentId="c1"
        meta={meta({ answer: { state: "submitted", selected_index: 0, answered_at: "2026-01-01T00:00:00Z" } })}
        currentUserId="u-target"
      />,
    );
    expect(screen.getByText("Selected")).toBeTruthy();
    expect(screen.queryByText("Confirm")).toBeNull();
    expect(screen.queryByText("Ignore")).toBeNull();
  });

  it("renders the terminal 'Ignored' chip once ignored", () => {
    renderCard(
      <AskUserQuestionCard
        issueId="i1"
        commentId="c1"
        meta={meta({ answer: { state: "ignored", answered_at: "2026-01-01T00:00:00Z" } })}
        currentUserId="u-target"
      />,
    );
    expect(screen.getByText("Ignored")).toBeTruthy();
    expect(screen.queryByText("Confirm")).toBeNull();
  });
  it("multi-select: checks several options and submits selectedIndices", () => {
    renderCard(
      <AskUserQuestionCard
        issueId="i1"
        commentId="c1"
        meta={meta({ multi_select: true })}
        currentUserId="u-target"
      />,
    );
    fireEvent.click(screen.getByText("Redis"));
    fireEvent.click(screen.getByText("Local"));
    fireEvent.click(screen.getByText("Confirm").closest("button")!);
    expect(mutateMock.mock.calls[0]![0]).toMatchObject({
      commentId: "c1",
      state: "submitted",
      selectedIndices: [0, 1],
    });
  });

  it("allow_custom: picking Other reveals input and submits customText", () => {
    renderCard(
      <AskUserQuestionCard
        issueId="i1"
        commentId="c1"
        meta={meta({ allow_custom: true })}
        currentUserId="u-target"
      />,
    );
    // "Other" row present; selecting it reveals the text input.
    fireEvent.click(screen.getByText("Other"));
    const input = document.querySelector('input[type="text"]') as HTMLInputElement;
    expect(input).toBeTruthy();
    fireEvent.change(input, { target: { value: "my custom answer" } });
    fireEvent.click(screen.getByText("Confirm").closest("button")!);
    expect(mutateMock.mock.calls[0]![0]).toMatchObject({
      commentId: "c1",
      state: "submitted",
      customText: "my custom answer",
    });
  });

  it("multi-select terminal: shows all chosen labels ticked", () => {
    renderCard(
      <AskUserQuestionCard
        issueId="i1"
        commentId="c1"
        meta={meta({
          multi_select: true,
          answer: { state: "submitted", selected_indices: [0, 1], answered_at: "2026-01-01T00:00:00Z" },
        })}
        currentUserId="u-target"
      />,
    );
    expect(screen.getByText("Selected")).toBeTruthy();
    expect(screen.queryByText("Confirm")).toBeNull();
  });
});
