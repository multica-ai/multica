import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import type { AgentTranscriptRowVM } from "../contracts/agentTranscriptRowVM";
import { transcriptRowKey, useEnteringTranscriptRows } from "./agentTranscriptModel";

function message(id: string, submitId?: string, speaker: "user" | "assistant" = "user"): AgentTranscriptRowVM {
  return {
    kind: "message", id, turnId: "turn", speaker,
    messages: [{ kind: "message-content", id, turnId: "turn", body: "Same text",
      presentationKind: "content", occurredAtUnixMs: 1,
      sourceTimelineItems: [{ id: 1, agentSessionId: "session", eventId: id,
        actorType: speaker, actorId: "user", itemType: "message", payload: { clientSubmitId: submitId } }] }],
    thinking: [], occurredAtUnixMs: 1
  };
}

it("does not fade in a durable user row again, but animates distinct submissions and assistant output", async () => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const container = document.body.appendChild(document.createElement("div"));
  const root = createRoot(container);
  let entering: ReadonlySet<string> = new Set();
  function Probe({ rows }: { rows: AgentTranscriptRowVM[] }) {
    entering = useEnteringTranscriptRows(rows.map(transcriptRowKey), rows);
    return null;
  }
  const render = (rows: AgentTranscriptRowVM[]) => act(async () => root.render(<Probe rows={rows} />));
  try {
    await render([]);
    await render([message("optimistic", "submit-a")]);
    expect([...entering]).toEqual(["optimistic"]);
    await render([message("durable", "submit-a")]);
    expect([...entering]).toEqual([]);
    await render([message("durable", "submit-a"), message("next", "submit-b")]);
    expect([...entering]).toEqual(["next"]);
    await render([message("durable", "submit-a"), message("next-confirmed", "submit-b"), message("reply", "submit-a", "assistant")]);
    expect([...entering]).toEqual(["reply"]);
    await render([message("history")]);
    expect([...entering]).toEqual(["history"]);
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});
