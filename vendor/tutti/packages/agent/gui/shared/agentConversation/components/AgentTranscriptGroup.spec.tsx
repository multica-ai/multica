import { StrictMode } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import { autorun, observable, runInAction } from "mobx";
import { expect, it, vi } from "vitest";
import { TranscriptPresentationModel } from "../../../state/TranscriptPresentationModel";
import type { AgentConversationVM } from "../contracts/agentConversationVM";
import { AgentTranscriptGroup } from "./AgentTranscriptGroup";

vi.mock("../../../i18n/index", () => ({
  getActiveUiLanguage: () => "en",
  useTranslation: () => ({ t: (key: string) => key }),
  translate: (key: string) => key
}));
vi.mock("./AgentMessageBlock", () => ({
  AgentMessageBlock: ({ row }: { row: { messages: { body: string }[] } }) => (
    <p>{row.messages.map((message) => message.body).join(" ")}</p>
  )
}));

type Row = AgentConversationVM["rows"][number];
const message = (id: string): Row => ({
  kind: "message", id, turnId: "t1", speaker: "user", occurredAtUnixMs: 1,
  messages: [{ kind: "message-content", id, turnId: "t1", body: id,
    presentationKind: "content", occurredAtUnixMs: 1 }],
  thinking: []
});
function fixture() {
  const activity = { sessionId: "s", agentProvider: "codex" } as AgentConversationVM["activity"];
  const base: AgentConversationVM = {
    activity, workspaceRoot: null, rows: [message("initial")],
    sourceDetail: {
      activity, cwd: "", workspaceRoot: "", turns: [], showProcessingIndicator: false,
      session: normalizeAgentActivitySession({ agentSessionId: "s", workspaceId: "w", provider: "codex", cwd: "", title: "Test",
        activeTurnId: null, latestTurnInteractions: [], pendingInteractions: [] })
    }
  };
  const parts = observable.box([base], { deep: false });
  const model = new TranscriptPresentationModel({ parts: () => parts.get(), base: () => base, turn: () => null }, "s", false);
  const stop = autorun(() => model.viewportGroups);
  const content = () => (
    <StrictMode>
      <AgentTranscriptGroup model={model} groupKey="t1" options={{
        labels: { processing: "Working", thinkingLabel: "Thinking", toolCallsLabel: String, turnSummary: "Summary" }
      }} onDisclosureMotionChange={() => {}} />
    </StrictMode>
  );
  return { base, parts, stop, content };
}
const rowElement = (text: string) => screen.getByText(text).closest("[data-agent-transcript-row]")!;
const finishAnimation = (element: Element) => {
  fireEvent.animationEnd(element);
  fireEvent(element, new Event("webkitAnimationEnd", { bubbles: true }));
};

it.each([false, true])("does not replay a new row's entrance after a virtual remount (animation ended=%s)", (ended) => {
  const { base, parts, stop, content } = fixture();
  const app = render(content());
  try {
    act(() => runInAction(() => parts.set([{ ...base, rows: [...base.rows, message("added")] }])));
    const added = rowElement("added");
    expect(added).toHaveAttribute("data-agent-transcript-row-enter", "true");
    finishAnimation(screen.getByText("added"));
    expect(added).toHaveAttribute("data-agent-transcript-row-enter", "true");
    if (ended) {
      finishAnimation(added);
      expect(added).not.toHaveAttribute("data-agent-transcript-row-enter");
    }
    app.rerender(null);
    app.rerender(content());
    expect(rowElement("added")).not.toHaveAttribute("data-agent-transcript-row-enter");
  } finally {
    app.unmount();
    stop();
  }
});

it("does not animate an already displayed message when a history window returns", () => {
  const { base, parts, stop, content } = fixture();
  const app = render(content());
  try {
    expect(rowElement("initial")).not.toHaveAttribute("data-agent-transcript-row-enter");
    act(() => runInAction(() => parts.set([{ ...base, rows: [message("other-window")] }])));
    act(() => runInAction(() => parts.set([base])));
    expect(rowElement("initial")).not.toHaveAttribute("data-agent-transcript-row-enter");
  } finally {
    app.unmount();
    stop();
  }
});
