// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { PendingSend } from "./drafts";
import { aiSubmission,canReplaySubmission } from "./submission";

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

it("never replays an acknowledged mirror absent from the loaded body or an unproven executor history",()=>{
 const record={confirmed:false,lifecycle:true,generation:"g"};const reply={known_history:true,store_generation:"g"};
 expect(canReplaySubmission(record,reply)).toBe(true);
 expect(canReplaySubmission({...record,confirmed:true},reply)).toBe(false);
 expect(canReplaySubmission({...record,lifecycle:false},reply)).toBe(false);
 expect(canReplaySubmission(record,{...reply,store_generation:"other"})).toBe(false);
 expect(canReplaySubmission(record,{...reply,known_history:false})).toBe(false);
});
