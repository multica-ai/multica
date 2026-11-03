// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { PendingSend } from "./drafts";
import { aiSubmission } from "./submission";

describe("reference submission ownership", () => {
  const request: PendingSend = { requestId: "same-request", text: "Review", attachments: ["file-1"], createdAt: 123,
    prepared: { text: "[Reviewer](mention://agent/agent-1)\n\nReview", prompt: "Review with quoted task context", references: [{ kind: "agent", id: "agent-1" }, { kind: "issue", id: "issue-1" }], agents: [{ id: "agent-1", name: "Reviewer" }] },
  };
  it("retries the same text, references, recipients and request identity", () => {
    const restored: PendingSend = JSON.parse(JSON.stringify(request));
    const first = aiSubmission(request);
    expect(aiSubmission(restored)).toEqual(first);
    expect(first).toMatchObject({ type: "send", request_id: "same-request", text: request.prepared!.text, prompt: request.prepared!.prompt, references: request.prepared!.references, agents: request.prepared!.agents, attachment_ids: ["file-1"] });
  });
  it("keeps context and recipients together when queued and restored", () => {
    const queued = { ...request, queued: true, queuePaused: true };
    const command = aiSubmission(queued);
    expect(command).toMatchObject({ type: "queue_enqueue", paused: true, item: { requestId: request.requestId, content: request.prepared!.text, prompt: request.prepared!.prompt, references: request.prepared!.references, agents: request.prepared!.agents, attachmentIds: request.attachments } });
    expect(aiSubmission(JSON.parse(JSON.stringify(queued)))).toEqual(command);
  });
});
