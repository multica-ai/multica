// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { AgentQuestionCard, agentQuestionAnswered, agentQuestionOf, formatAgentQuestionAnswer } from "./agent-question-card";

const { toastError } = vi.hoisted(() => ({ toastError: vi.fn() }));
vi.mock("sonner", () => ({ toast: { error: toastError } }));

function questionEntry(over: Partial<TimelineEntry> = {}): TimelineEntry {
  return {
    type: "comment",
    id: "q-1",
    actor_type: "agent",
    actor_id: "agent-1",
    created_at: "2026-09-07T10:00:00Z",
    content: "**Flag** — Which flag name?",
    question_payload: {
      questions: [
        {
          question: "Which flag name?",
          header: "Flag",
          multi_select: false,
          options: [
            { label: "--dry-run", description: "Conventional" },
            { label: "--preview", description: "Friendlier" },
          ],
        },
      ],
    },
    ...over,
  };
}

const memberReply: TimelineEntry = {
  type: "comment",
  id: "r-1",
  actor_type: "member",
  actor_id: "user-1",
  created_at: "2026-09-07T10:05:00Z",
  parent_id: "q-1",
  content: "**Flag:** --dry-run",
};

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("AgentQuestionCard", () => {
  it("keeps Submit disabled until every question has an answer, then posts the formatted reply", async () => {
    const onSubmit = vi.fn().mockResolvedValue("r-1");
    const onAccepted = vi.fn();
    renderWithI18n(
      <AgentQuestionCard entry={questionEntry()} replies={[]} agentName="Walt" onSubmit={onSubmit} onAccepted={onAccepted} />,
    );

    expect(screen.getByText("Walt has a question")).toBeTruthy();
    expect(screen.getByText("Which flag name?")).toBeTruthy();
    expect(screen.getByText("Conventional")).toBeTruthy();
    const submit = screen.getByRole("button", { name: "Send answer" }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);

    fireEvent.click(screen.getByRole("radio", { name: /--dry-run/ }));
    await waitFor(() => expect(submit.disabled).toBe(false));

    fireEvent.click(submit);
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("**Flag:** --dry-run"));
    await waitFor(() => expect(onAccepted).toHaveBeenCalledWith("r-1"));
  });

  it("requires text for the Other choice and sends it in place of a label", async () => {
    const onSubmit = vi.fn().mockResolvedValue("r-2");
    renderWithI18n(<AgentQuestionCard entry={questionEntry()} replies={[]} agentName="Walt" onSubmit={onSubmit} />);

    const submit = screen.getByRole("button", { name: "Send answer" }) as HTMLButtonElement;
    fireEvent.click(screen.getByRole("radio", { name: "Other" }));
    // Picking Other alone is not an answer yet.
    expect(submit.disabled).toBe(true);

    fireEvent.change(screen.getByPlaceholderText("Write your own answer"), { target: { value: "--plan" } });
    await waitFor(() => expect(submit.disabled).toBe(false));
    fireEvent.click(submit);
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("**Flag:** --plan"));
  });

  it("joins multi-select choices and falls back to the question text without a header", async () => {
    const onSubmit = vi.fn().mockResolvedValue("r-3");
    const entry = questionEntry({
      question_payload: {
        questions: [
          {
            question: "Which sections?",
            multi_select: true,
            options: [{ label: "Intro" }, { label: "Outro" }, { label: "Appendix" }],
          },
        ],
      },
    });
    renderWithI18n(<AgentQuestionCard entry={entry} replies={[]} agentName="Walt" onSubmit={onSubmit} />);

    fireEvent.click(screen.getByRole("checkbox", { name: "Intro" }));
    fireEvent.click(screen.getByRole("checkbox", { name: "Appendix" }));
    const submit = screen.getByRole("button", { name: "Send answer" });
    await waitFor(() => expect((submit as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(submit);
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith("**Which sections?:** Intro, Appendix"));
  });

  it("surfaces a failed send and keeps the choice", async () => {
    const onSubmit = vi.fn().mockResolvedValue(false);
    renderWithI18n(<AgentQuestionCard entry={questionEntry()} replies={[]} agentName="Walt" onSubmit={onSubmit} />);

    fireEvent.click(screen.getByRole("radio", { name: /--preview/ }));
    const submit = screen.getByRole("button", { name: "Send answer" });
    await waitFor(() => expect((submit as HTMLButtonElement).disabled).toBe(false));
    fireEvent.click(submit);

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("Failed to send the answer"));
    expect((screen.getByRole("radio", { name: /--preview/ }) as HTMLInputElement).getAttribute("aria-checked")).toBe("true");
    expect((submit as HTMLButtonElement).disabled).toBe(false);
  });

  it("renders read-only with an Answered badge once a member replied in the thread", () => {
    renderWithI18n(
      <AgentQuestionCard entry={questionEntry()} replies={[memberReply]} agentName="Walt" onSubmit={vi.fn()} />,
    );

    expect(screen.getByText("Answered")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Send answer" })).toBeNull();
    for (const radio of screen.getAllByRole("radio")) {
      expect((radio as HTMLElement).getAttribute("aria-disabled") === "true" || (radio as HTMLButtonElement).disabled).toBe(true);
    }
  });

  it("renders localized copy", () => {
    renderWithI18n(<AgentQuestionCard entry={questionEntry()} replies={[]} agentName="Walt" onSubmit={vi.fn()} />, {
      locale: "zh-Hans",
    });
    expect(screen.getByRole("button", { name: "发送回答" })).toBeTruthy();
  });
});

describe("agent question helpers", () => {
  it("treats a missing or empty payload as an ordinary comment", () => {
    expect(agentQuestionOf({ question_payload: null })).toBeNull();
    expect(agentQuestionOf({ question_payload: undefined })).toBeNull();
    expect(agentQuestionOf({ question_payload: { questions: [] } })).toBeNull();
    expect(agentQuestionOf(questionEntry())).not.toBeNull();
  });

  it("counts only member replies as answers", () => {
    expect(agentQuestionAnswered([])).toBe(false);
    expect(agentQuestionAnswered([{ actor_type: "agent" }])).toBe(false);
    expect(agentQuestionAnswered([{ actor_type: "agent" }, { actor_type: "member" }])).toBe(true);
  });

  it("formats one line per question in payload order", () => {
    const payload = questionEntry().question_payload!;
    expect(formatAgentQuestionAnswer(payload, { "0:Which flag name?": { selected: ["--preview"], other: "" } })).toBe(
      "**Flag:** --preview",
    );
  });
});
